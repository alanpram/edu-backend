package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
)

func (s *Server) registerUsers(api chi.Router) {
	api.Route("/users", func(r chi.Router) {
		r.Use(s.Auth.Authenticate)
		r.With(middleware.RequirePermission(s.Permissions, "admin.users.view")).Get("/", httpx.Wrap(s.listUsers))
		r.With(middleware.RequirePermission(s.Permissions, "admin.users.manage")).Post("/", httpx.Wrap(s.createUser))
		r.With(middleware.RequirePermission(s.Permissions, "admin.users.manage")).Patch("/{id}/role", httpx.Wrap(s.updateUserRole))
		r.With(middleware.RequirePermission(s.Permissions, "admin.users.manage")).Patch("/{id}/status", httpx.Wrap(s.updateUserStatus))
	})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"1=1"}
	args := []any{}
	index := 1

	if role := strings.TrimSpace(r.URL.Query().Get("user_role")); role != "" {
		if !isRole(role) {
			return httpx.BadRequest("Role tidak valid", "INVALID_ROLE")
		}
		conditions = append(conditions, fmt.Sprintf("user_role = $%d::\"Role\"", index))
		args = append(args, role)
		index++
	}
	if active := httpx.QueryBoolPtr(r, "is_active"); active != nil {
		conditions = append(conditions, fmt.Sprintf("is_active = $%d", index))
		args = append(args, *active)
		index++
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("(user_nama ILIKE $%d OR user_email ILIKE $%d)", index, index))
		args = append(args, "%"+search+"%")
		index++
	}

	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM "user" WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`SELECT %s FROM "user" WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		publicUserColumns, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	items := make([]auth.User, 0, limit)
	for rows.Next() {
		var user auth.User
		if err := rows.Scan(&user.UserID, &user.UserNama, &user.UserEmail, &user.UserPhone, &user.UserRole,
			&user.EmailVerifiedAt, &user.IsActive, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return err
		}
		items = append(items, user)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return nil
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	v := &validation{}
	name := v.stringField(body, "user_nama", "Nama", 2, 150, true)
	email := v.email(body, "user_email")
	phone := v.phone(body, "user_phone", false)
	password := v.password(body, "user_password", "Password")
	role := bodyString(body, "user_role")
	if !isRole(role) {
		v.add("user_role", "Role tidak valid")
	}
	if err := v.err(); err != nil {
		return err
	}

	var exists int
	if err := s.Pool.QueryRow(r.Context(), `SELECT 1 FROM "user" WHERE user_email = $1`, email).Scan(&exists); err == nil {
		return httpx.Conflict("Email sudah terdaftar", "EMAIL_ALREADY_REGISTERED")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if phone != "" {
		if err := s.Pool.QueryRow(r.Context(), `SELECT 1 FROM "user" WHERE user_phone = $1`, phone).Scan(&exists); err == nil {
			return httpx.Conflict("Nomor HP sudah terdaftar", "PHONE_ALREADY_REGISTERED")
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}

	hash, err := auth.HashPassword(password, s.Config.BcryptRounds)
	if err != nil {
		return err
	}
	var phoneArg any
	if phone != "" {
		phoneArg = phone
	}
	user, err := scanUser(s.Pool.QueryRow(r.Context(), `
		INSERT INTO "user" (user_nama, user_email, user_phone, user_password, user_role, email_verified_at, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5::"Role", now(), true, now(), now())
		RETURNING `+publicUserColumns, name, email, phoneArg, hash, role))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "USER_CREATE", auth.RequestIP(r), map[string]any{
		"target_user_id": user.UserID, "user_role": user.UserRole,
	})
	httpx.Created(w, user, "User berhasil dibuat")
	return nil
}

func (s *Server) updateUserRole(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	targetID, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	role := bodyString(body, "user_role")
	if !isRole(role) {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "user_role", "message": "Role tidak valid"}})
	}
	if targetID == principal.UserID && role != "SUPER_ADMIN" {
		return httpx.BadRequest("Anda tidak dapat menurunkan role akun sendiri.", "CANNOT_CHANGE_OWN_ROLE")
	}

	var currentRole string
	if err := s.Pool.QueryRow(r.Context(), `SELECT user_role FROM "user" WHERE user_id = $1`, targetID).Scan(&currentRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("User tidak ditemukan", "USER_NOT_FOUND")
		}
		return err
	}
	if currentRole == "SUPER_ADMIN" && role != "SUPER_ADMIN" {
		var count int
		if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM "user" WHERE user_role = 'SUPER_ADMIN'`).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return httpx.Conflict("Tidak dapat menurunkan SUPER_ADMIN terakhir.", "LAST_SUPER_ADMIN")
		}
	}
	user, err := scanUser(s.Pool.QueryRow(r.Context(), `
		UPDATE "user" SET user_role = $1::"Role", updated_at = now() WHERE user_id = $2
		RETURNING `+publicUserColumns, role, targetID))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "USER_ROLE_UPDATE", auth.RequestIP(r), map[string]any{
		"target_user_id": targetID, "previous_role": currentRole, "new_role": role,
	})
	httpx.Success(w, user, "Role user berhasil diperbarui")
	return nil
}

func (s *Server) updateUserStatus(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	targetID, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	active, ok := body["is_active"].(bool)
	if !ok {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "is_active", "message": "Status wajib diisi"}})
	}
	if targetID == principal.UserID && !active {
		return httpx.BadRequest("Anda tidak dapat menonaktifkan akun sendiri.", "CANNOT_DEACTIVATE_SELF")
	}
	var currentRole string
	if err := s.Pool.QueryRow(r.Context(), `SELECT user_role FROM "user" WHERE user_id = $1`, targetID).Scan(&currentRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("User tidak ditemukan", "USER_NOT_FOUND")
		}
		return err
	}
	if currentRole == "SUPER_ADMIN" && !active {
		var count int
		if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM "user" WHERE user_role = 'SUPER_ADMIN'`).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return httpx.Conflict("Tidak dapat menonaktifkan SUPER_ADMIN terakhir.", "LAST_SUPER_ADMIN")
		}
	}
	user, err := scanUser(s.Pool.QueryRow(r.Context(), `
		UPDATE "user" SET is_active = $1,
			current_token = CASE WHEN $1 THEN current_token ELSE NULL END,
			updated_at = now()
		WHERE user_id = $2 RETURNING `+publicUserColumns, active, targetID))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "USER_STATUS_UPDATE", auth.RequestIP(r), map[string]any{
		"target_user_id": targetID, "is_active": active,
	})
	message := "User diaktifkan"
	if !active {
		message = "User dinonaktifkan dan sesi dicabut"
	}
	httpx.Success(w, user, message)
	return nil
}
