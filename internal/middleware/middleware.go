// Package middleware berisi middleware HTTP lintas domain.
package middleware

import (
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/permissions"
)

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// CORS hanya mengizinkan origin eksplisit. Wildcard sengaja tidak didukung
// karena cookie sesi memerlukan Access-Control-Allow-Credentials.
func CORS(origins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		if origin != "" && origin != "*" {
			allowed[origin] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" && allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, X-CSRF-Token")
				w.Header().Set("Access-Control-Expose-Headers", "X-CSRF-Token")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP menaruh IP klien yang sudah tervalidasi ke context. Header proxy
// hanya dipercaya bila TRUST_PROXY=true; nilai yang diambil adalah entri
// terakhir X-Forwarded-For (paling dekat dengan proxy) agar entri palsu di
// depan tidak dapat dipakai untuk mem-bypass rate limit.
func ClientIP(trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := auth.RemoteIP(r.RemoteAddr)
			if trustProxy {
				if forwarded := forwardedIP(r); forwarded != "" {
					ip = forwarded
				}
			}
			next.ServeHTTP(w, r.WithContext(auth.WithClientIP(r.Context(), ip)))
		})
	}
}

func forwardedIP(r *http.Request) string {
	values := []string{}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		values = append(values, strings.Split(forwarded, ",")...)
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		values = append(values, realIP)
	}
	for index := len(values) - 1; index >= 0; index-- {
		candidate := strings.TrimSpace(values[index])
		if parsed := net.ParseIP(candidate); parsed != nil {
			return parsed.String()
		}
	}
	return ""
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.RequestURI(), recorder.status, time.Since(start).Round(time.Millisecond))
	})
}

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("[PANIC] %v", recovered)
				httpx.WriteError(w, httpx.NewError(http.StatusInternalServerError, "Terjadi kesalahan internal pada server", "INTERNAL_SERVER_ERROR", nil))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func RequirePermission(service *permissions.Service, key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.PrincipalFrom(r.Context())
			if !ok {
				httpx.WriteError(w, httpx.Unauthorized("", "UNAUTHORIZED"))
				return
			}
			allowed, err := service.HasPermission(r.Context(), principal.UserRole, key)
			if err != nil {
				httpx.WriteError(w, err)
				return
			}
			if !allowed {
				label := key
				for _, definition := range permissions.Catalog {
					if definition.Key == key {
						label = definition.Label
						break
					}
				}
				httpx.WriteError(w, httpx.NewError(
					http.StatusForbidden,
					"Anda tidak memiliki izin untuk: "+label+".",
					"PERMISSION_FORBIDDEN",
					map[string]any{"permission": key},
				))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequireRoles(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.PrincipalFrom(r.Context())
			if !ok {
				httpx.WriteError(w, httpx.Unauthorized("", "UNAUTHORIZED"))
				return
			}
			for _, role := range roles {
				if principal.UserRole == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			httpx.WriteError(w, httpx.Forbidden("Endpoint ini hanya dapat diakses oleh: "+strings.Join(roles, ", "), "ROLE_FORBIDDEN"))
		})
	}
}
