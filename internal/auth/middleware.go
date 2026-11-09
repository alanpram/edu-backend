package auth

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edumaster/backend-go/internal/httpx"
)

type User struct {
	UserID          int        `json:"user_id"`
	UserNama        string     `json:"user_nama"`
	UserEmail       string     `json:"user_email"`
	UserPhone       *string    `json:"user_phone"`
	UserRole        string     `json:"user_role"`
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
	IsActive        bool       `json:"is_active"`
	LastLoginAt     *time.Time `json:"last_login_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Principal struct {
	UserID    int
	UserNama  string
	UserEmail string
	UserRole  string
}

type contextKey string

const principalKey contextKey = "principal"

type clientIPKey struct{}

// WithClientIP menyimpan IP klien hasil resolusi middleware. Handler tidak
// boleh membaca X-Forwarded-For langsung karena header itu mudah dipalsukan.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey, principal)
}

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey).(Principal)
	return principal, ok
}

type Middleware struct {
	Pool           *pgxpool.Pool
	Secret         string
	CookieName     string
	CSRFCookieName string
	CookieSecure   bool
	CookieSameSite string
	CookieDomain   string
	TokenTTL       time.Duration
}

func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := m.resolve(r)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		m.ExposeCSRF(w, r)
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
	})
}

func (m *Middleware) OptionalAuthenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal, err := m.resolve(r); err == nil {
			m.ExposeCSRF(w, r)
			r = r.WithContext(WithPrincipal(r.Context(), principal))
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) resolve(r *http.Request) (Principal, error) {
	token, fromCookie, err := m.tokenFromRequest(r)
	if err != nil {
		return Principal{}, err
	}
	if fromCookie && isUnsafeMethod(r.Method) {
		if err := m.verifyCSRF(r); err != nil {
			return Principal{}, err
		}
	}
	claims, err := Verify(m.Secret, token)
	if err != nil {
		return Principal{}, err
	}
	userID := 0
	for _, ch := range claims.Sub {
		if ch < '0' || ch > '9' {
			return Principal{}, httpx.Unauthorized("Token tidak valid", "INVALID_TOKEN")
		}
		userID = userID*10 + int(ch-'0')
	}
	if userID <= 0 {
		return Principal{}, httpx.Unauthorized("Token tidak valid", "INVALID_TOKEN")
	}

	var user User
	var currentToken *string
	err = m.Pool.QueryRow(r.Context(), `
		SELECT user_id, user_nama, user_email, user_phone, user_role,
		       email_verified_at, is_active, last_login_at, created_at, updated_at, current_token
		FROM "user" WHERE user_id = $1`, userID,
	).Scan(
		&user.UserID, &user.UserNama, &user.UserEmail, &user.UserPhone, &user.UserRole,
		&user.EmailVerifiedAt, &user.IsActive, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt, &currentToken,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Principal{}, httpx.Unauthorized("Akun tidak ditemukan", "USER_NOT_FOUND")
		}
		return Principal{}, err
	}
	if !user.IsActive {
		return Principal{}, httpx.Forbidden("Akun dinonaktifkan. Hubungi administrator.", "ACCOUNT_INACTIVE")
	}
	if currentToken == nil || *currentToken != HashToken(token) {
		return Principal{}, httpx.Unauthorized(
			"Sesi Anda telah berakhir karena akun digunakan pada perangkat lain.",
			"SESSION_REPLACED",
		)
	}
	return Principal{
		UserID:    user.UserID,
		UserNama:  user.UserNama,
		UserEmail: user.UserEmail,
		UserRole:  user.UserRole,
	}, nil
}

// RequestIP mengembalikan IP klien yang sudah diresolusi middleware. Bila
// middleware tidak dipasang (mis. unit test), fallback ke RemoteAddr.
func RequestIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok && ip != "" {
		return ip
	}
	return RemoteIP(r.RemoteAddr)
}

// RemoteIP mengambil host dari alamat RemoteAddr tanpa port.
func RemoteIP(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}
