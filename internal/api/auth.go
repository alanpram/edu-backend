package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/httpx"
)

type userWithPermissions struct {
	auth.User
	Permissions []string `json:"permissions"`
}

func (s *Server) registerAuth(api chi.Router) {
	api.Route("/auth", func(r chi.Router) {
		r.With(s.authLimit.Middleware).Post("/register", httpx.Wrap(s.register))
		r.With(s.authLimit.Middleware).Post("/login", httpx.Wrap(s.login))
		r.With(s.verifyLimit.Middleware).Post("/verify-email", httpx.Wrap(s.verifyEmail))
		r.With(s.verifyLimit.Middleware).Post("/resend-verification", httpx.Wrap(s.resendVerification))

		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Authenticate)
			r.Post("/logout", httpx.Wrap(s.logout))
			r.Get("/me", httpx.Wrap(s.me))
			r.Patch("/me", httpx.Wrap(s.updateProfile))
			r.Patch("/me/password", httpx.Wrap(s.changePassword))
		})
	})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) error {
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	v := &validation{}
	name := v.stringField(body, "user_nama", "Nama", 2, 150, true)
	email := v.email(body, "user_email")
	phone := v.phone(body, "user_phone", true)
	password := v.password(body, "user_password", "Password")
	confirm, _ := body["confirm_password"].(string)
	if confirm == "" {
		v.add("confirm_password", "Konfirmasi password wajib diisi")
	} else if confirm != password {
		v.add("confirm_password", "Konfirmasi password tidak cocok")
	}
	if err := v.err(); err != nil {
		return err
	}

	ip := auth.RequestIP(r)
	if err := s.Captcha.Verify(r.Context(), bodyString(body, "recaptcha_token"), ip, "register"); err != nil {
		return err
	}

	var existing int
	if err := s.Pool.QueryRow(r.Context(), `SELECT 1 FROM "user" WHERE user_email = $1`, email).Scan(&existing); err == nil {
		return httpx.Conflict("Email sudah terdaftar", "EMAIL_ALREADY_REGISTERED")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err := s.Pool.QueryRow(r.Context(), `SELECT 1 FROM "user" WHERE user_phone = $1`, phone).Scan(&existing); err == nil {
		return httpx.Conflict("Nomor HP sudah terdaftar", "PHONE_ALREADY_REGISTERED")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	passwordHash, err := auth.HashPassword(password, s.Config.BcryptRounds)
	if err != nil {
		return err
	}

	var user auth.User
	err = s.Pool.QueryRow(r.Context(), `
		INSERT INTO "user" (user_nama, user_email, user_phone, user_password, user_role, email_verified_at, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'STUDENT'::"Role", NULL, true, now(), now())
		RETURNING `+publicUserColumns,
		name, email, phone, passwordHash,
	).Scan(&user.UserID, &user.UserNama, &user.UserEmail, &user.UserPhone, &user.UserRole,
		&user.EmailVerifiedAt, &user.IsActive, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return err
	}

	verificationToken := auth.RandomHex(32)
	tokenHash := auth.HashToken(verificationToken)
	err = s.issueVerificationToken(r.Context(), user.UserID, tokenHash, time.Now().Add(s.Config.EmailVerificationTTL))
	if err != nil {
		return err
	}

	emailSent := false
	if _, sendErr := s.Mail.SendVerificationEmail(user.UserEmail, user.UserNama, verificationToken); sendErr != nil {
		// Akun tetap dibuat agar user bisa memakai tombol kirim ulang.
		_ = sendErr
	} else {
		emailSent = s.Config.MailEnabled && s.Config.Environment != "test"
	}
	s.Activity.Record(r.Context(), user.UserID, "REGISTER", ip, map[string]any{
		"email": user.UserEmail, "email_sent": emailSent,
	})

	httpx.Created(w, map[string]any{
		"user":                  user,
		"email_sent":            emailSent,
		"verification_required": true,
	}, "Registrasi berhasil. Cek email Anda untuk menyelesaikan verifikasi.")
	return nil
}

func (s *Server) issueVerificationToken(ctx context.Context, userID int, tokenHash string, expiresAt time.Time) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE email_verification_token SET used_at = now()
			WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO email_verification_token (user_id, token_hash, expires_at, created_at)
			VALUES ($1, $2, $3, now())`, userID, tokenHash, expiresAt)
		return err
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	v := &validation{}
	email := v.email(body, "user_email")
	password, _ := body["user_password"].(string)
	if password == "" {
		v.add("user_password", "Password wajib diisi")
	}
	if err := v.err(); err != nil {
		return err
	}
	ip := auth.RequestIP(r)
	if err := s.Captcha.Verify(r.Context(), bodyString(body, "recaptcha_token"), ip, "login"); err != nil {
		return err
	}

	var user auth.User
	var passwordHash string
	err = s.Pool.QueryRow(r.Context(), `
		SELECT `+publicUserColumns+`, user_password
		FROM "user" WHERE user_email = $1`, email,
	).Scan(&user.UserID, &user.UserNama, &user.UserEmail, &user.UserPhone, &user.UserRole,
		&user.EmailVerifiedAt, &user.IsActive, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt,
		&passwordHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.Unauthorized("Email atau password salah", "INVALID_CREDENTIALS")
		}
		return err
	}
	if !auth.VerifyPassword(password, passwordHash) {
		return httpx.Unauthorized("Email atau password salah", "INVALID_CREDENTIALS")
	}
	if user.EmailVerifiedAt == nil {
		return httpx.Forbidden("Email belum diverifikasi. Cek kotak masuk Anda atau minta tautan verifikasi baru.", "EMAIL_NOT_VERIFIED")
	}
	if !user.IsActive {
		return httpx.Forbidden("Akun dinonaktifkan. Hubungi administrator.", "ACCOUNT_INACTIVE")
	}

	token, err := auth.Sign(s.Config.JWTSecret, s.Config.JWTExpiresIn, user.UserID, user.UserRole)
	if err != nil {
		return err
	}
	if _, err := s.Pool.Exec(r.Context(), `
		UPDATE "user" SET current_token = $1, last_login_at = now(), updated_at = now()
		WHERE user_id = $2`, auth.HashToken(token), user.UserID); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), user.UserID, "LOGIN", ip, nil)

	permissionKeys, err := s.Permissions.KeysForRole(r.Context(), user.UserRole)
	if err != nil {
		return err
	}
	s.Auth.SetSessionCookies(w, token)
	httpx.Success(w, map[string]any{
		"token":      token,
		"token_type": "Bearer",
		"user":       userWithPermissions{User: user, Permissions: permissionKeys},
	}, "Login berhasil")
	return nil
}

func (s *Server) verifyEmail(w http.ResponseWriter, r *http.Request) error {
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	rawToken, _ := body["token"].(string)
	rawToken = strings.TrimSpace(rawToken)
	if len(rawToken) < 32 || len(rawToken) > 256 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "token", "message": "Token verifikasi tidak valid"}})
	}

	tokenHash := auth.HashToken(rawToken)
	var user auth.User
	err = pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
		var tokenID, userID int
		var usedAt *time.Time
		var expiresAt time.Time
		err := tx.QueryRow(r.Context(), `
			SELECT token_id, user_id, used_at, expires_at
			FROM email_verification_token WHERE token_hash = $1 FOR UPDATE`, tokenHash,
		).Scan(&tokenID, &userID, &usedAt, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.BadRequest("Tautan verifikasi tidak valid atau sudah kedaluwarsa. Minta tautan baru.", "INVALID_VERIFICATION_TOKEN")
		}
		if err != nil {
			return err
		}
		if usedAt != nil || !expiresAt.After(time.Now()) {
			return httpx.BadRequest("Tautan verifikasi tidak valid atau sudah kedaluwarsa. Minta tautan baru.", "INVALID_VERIFICATION_TOKEN")
		}
		result, err := tx.Exec(r.Context(), `
			UPDATE email_verification_token SET used_at = now()
			WHERE token_id = $1 AND used_at IS NULL AND expires_at > now()`, tokenID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return httpx.BadRequest("Tautan verifikasi tidak valid atau sudah kedaluwarsa. Minta tautan baru.", "INVALID_VERIFICATION_TOKEN")
		}
		err = tx.QueryRow(r.Context(), `
			UPDATE "user" SET email_verified_at = COALESCE(email_verified_at, now()), updated_at = now()
			WHERE user_id = $1
			RETURNING `+publicUserColumns, userID,
		).Scan(&user.UserID, &user.UserNama, &user.UserEmail, &user.UserPhone, &user.UserRole,
			&user.EmailVerifiedAt, &user.IsActive, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt)
		return err
	})
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), user.UserID, "EMAIL_VERIFIED", auth.RequestIP(r), nil)
	httpx.Success(w, user, "Email berhasil diverifikasi. Silakan login.")
	return nil
}

func (s *Server) resendVerification(w http.ResponseWriter, r *http.Request) error {
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	v := &validation{}
	email := v.email(body, "user_email")
	if err := v.err(); err != nil {
		return err
	}
	ip := auth.RequestIP(r)

	var userID int
	var name string
	var verifiedAt *time.Time
	err = s.Pool.QueryRow(r.Context(), `
		SELECT user_id, user_nama, email_verified_at FROM "user" WHERE user_email = $1`, email,
	).Scan(&userID, &name, &verifiedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && verifiedAt != nil) {
		httpx.Success(w, map[string]any{"sent": false}, "Jika email terdaftar dan belum terverifikasi, tautan verifikasi baru telah dikirim.")
		return nil
	}
	if err != nil {
		return err
	}

	token := auth.RandomHex(32)
	if err := s.issueVerificationToken(r.Context(), userID, auth.HashToken(token), time.Now().Add(s.Config.EmailVerificationTTL)); err != nil {
		return err
	}
	sent := false
	if _, sendErr := s.Mail.SendVerificationEmail(email, name, token); sendErr == nil {
		sent = s.Config.MailEnabled && s.Config.Environment != "test"
	}
	s.Activity.Record(r.Context(), userID, "RESEND_VERIFICATION", ip, map[string]any{"email_sent": sent})
	httpx.Success(w, map[string]any{"sent": sent}, "Jika email terdaftar dan belum terverifikasi, tautan verifikasi baru telah dikirim.")
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	if _, err := s.Pool.Exec(r.Context(), `
		UPDATE "user" SET current_token = NULL, updated_at = now() WHERE user_id = $1`, principal.UserID); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "LOGOUT", auth.RequestIP(r), nil)
	s.Auth.ClearSessionCookies(w)
	httpx.Success(w, nil, "Logout berhasil. Token sesi telah dicabut.")
	return nil
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	user, err := scanUser(s.Pool.QueryRow(r.Context(), `SELECT `+publicUserColumns+` FROM "user" WHERE user_id = $1`, principal.UserID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("User tidak ditemukan", "USER_NOT_FOUND")
		}
		return err
	}
	keys, err := s.Permissions.KeysForRole(r.Context(), user.UserRole)
	if err != nil {
		return err
	}
	httpx.Success(w, userWithPermissions{User: user, Permissions: keys}, "")
	return nil
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	v := &validation{}
	name := v.stringField(body, "user_nama", "Nama", 2, 150, true)
	if err := v.err(); err != nil {
		return err
	}
	user, err := scanUser(s.Pool.QueryRow(r.Context(), `
		UPDATE "user" SET user_nama = $1, updated_at = now()
		WHERE user_id = $2 RETURNING `+publicUserColumns, name, principal.UserID))
	if err != nil {
		return err
	}
	httpx.Success(w, user, "Profil berhasil diperbarui")
	return nil
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	oldPassword, _ := body["password_lama"].(string)
	if oldPassword == "" {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "password_lama", "message": "Password lama wajib diisi"}})
	}
	v := &validation{}
	newPassword := v.password(body, "password_baru", "Password baru")
	if err := v.err(); err != nil {
		return err
	}
	var hash string
	if err := s.Pool.QueryRow(r.Context(), `SELECT user_password FROM "user" WHERE user_id = $1`, principal.UserID).Scan(&hash); err != nil {
		return err
	}
	if !auth.VerifyPassword(oldPassword, hash) {
		return httpx.BadRequest("Password lama tidak sesuai", "INVALID_OLD_PASSWORD")
	}
	newHash, err := auth.HashPassword(newPassword, s.Config.BcryptRounds)
	if err != nil {
		return err
	}
	if _, err := s.Pool.Exec(r.Context(), `
		UPDATE "user" SET user_password = $1, current_token = NULL, updated_at = now()
		WHERE user_id = $2`, newHash, principal.UserID); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "CHANGE_PASSWORD", auth.RequestIP(r), nil)
	s.Auth.ClearSessionCookies(w)
	httpx.Success(w, nil, "Password berhasil diubah. Semua sesi telah dicabut, silakan login kembali.")
	return nil
}
