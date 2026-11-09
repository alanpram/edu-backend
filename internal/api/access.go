package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/codegen"
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
)

type accessPackage struct {
	PaketID         int            `json:"paket_id"`
	PaketNama       string         `json:"paket_nama"`
	PaketTipe       string         `json:"paket_tipe"`
	PaketHarga      float64        `json:"paket_harga"`
	MaxAttempts     int            `json:"max_attempts"`
	DurationMinutes int            `json:"duration_minutes"`
	PaketJumlahSoal int            `json:"paket_jumlah_soal"`
	IsActive        bool           `json:"is_active"`
	Count           map[string]int `json:"_count"`
}

type Access struct {
	AccessID    int           `json:"access_id"`
	UserID      int           `json:"user_id"`
	PaketID     int           `json:"paket_id"`
	SisaAttempt int           `json:"sisa_attempt"`
	ExpiredAt   time.Time     `json:"expired_at"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	Paket       accessPackage `json:"paket"`
}

type accessWithUser struct {
	Access
	User auth.User `json:"user"`
}

const accessSelect = `
	a.access_id, a.user_id, a.paket_id, a.sisa_attempt, a.expired_at, a.created_at, a.updated_at,
	p.paket_id, p.paket_nama, p.paket_tipe, p.paket_harga, p.max_attempts,
	p.duration_minutes, p.paket_jumlah_soal, p.is_active,
	(SELECT count(*) FROM soal_bank s WHERE s.paket_id = p.paket_id) AS soal_count`

func scanAccess(row pgx.Row) (Access, error) {
	var item Access
	var soalCount int
	err := row.Scan(
		&item.AccessID, &item.UserID, &item.PaketID, &item.SisaAttempt, &item.ExpiredAt, &item.CreatedAt, &item.UpdatedAt,
		&item.Paket.PaketID, &item.Paket.PaketNama, &item.Paket.PaketTipe, &item.Paket.PaketHarga,
		&item.Paket.MaxAttempts, &item.Paket.DurationMinutes, &item.Paket.PaketJumlahSoal, &item.Paket.IsActive,
		&soalCount,
	)
	item.Paket.Count = map[string]int{"soal_list": soalCount}
	return item, err
}

// assertOrganizationActive memastikan paket privat hanya dapat dipakai selama
// organisasi pemiliknya aktif.
func (s *Server) assertOrganizationActive(ctx context.Context, organizationID *int) error {
	if organizationID == nil {
		return nil
	}
	var status string
	if err := s.Pool.QueryRow(ctx, `
		SELECT organization_status FROM organization WHERE organization_id = $1`, *organizationID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Organisasi paket tidak ditemukan", "ORGANIZATION_NOT_FOUND")
		}
		return err
	}
	if status != "ACTIVE" {
		return httpx.Forbidden("Organisasi pemilik paket ini tidak aktif. Hubungi administrator.", "ORGANIZATION_INACTIVE")
	}
	return nil
}

// hasActivePackageAccess dipakai untuk fitur komunitas (mis. leaderboard)
// yang boleh diakses peserta pemegang kode tanpa harus menjadi anggota
// organisasi.
func (s *Server) hasActivePackageAccess(ctx context.Context, userID, paketID int) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_package_access a
			JOIN paket_ujian p ON p.paket_id = a.paket_id
			LEFT JOIN organization o ON o.organization_id = p.organization_id
			WHERE a.user_id = $1 AND a.paket_id = $2 AND a.expired_at > now()
			  AND (p.organization_id IS NULL OR o.organization_status = 'ACTIVE')
		)`, userID, paketID).Scan(&ok)
	return ok, err
}

func (s *Server) registerAccess(api chi.Router) {
	api.Route("/access", func(r chi.Router) {
		r.Use(s.Auth.Authenticate)
		r.Get("/me", httpx.Wrap(s.myAccess))
		r.With(s.redeemLimit.Middleware).Post("/redeem", httpx.Wrap(s.redeemAccessCode))
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequirePermission(s.Permissions, "admin.access.manage"))
			r.Get("/", httpx.Wrap(s.listAccess))
			r.Post("/", httpx.Wrap(s.grantAccess))
			r.Delete("/{id}", httpx.Wrap(s.revokeAccess))
		})
	})
}

func (s *Server) myAccess(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	rows, err := s.Pool.Query(r.Context(), `
		SELECT `+accessSelect+`
		FROM user_package_access a JOIN paket_ujian p ON p.paket_id = a.paket_id
		WHERE a.user_id = $1 ORDER BY a.created_at DESC`, principal.UserID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Access{}
	for rows.Next() {
		item, err := scanAccess(rows)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	_ = rows.Err()
	httpx.Success(w, items, "")
	return nil
}

func (s *Server) redeemAccessCode(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	rawCode, _ := body["kode"].(string)
	rawCode = strings.TrimSpace(rawCode)
	if len(rawCode) < 4 || len(rawCode) > 50 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "kode", "message": "Kode akses tidak valid"}})
	}
	code := codegen.NormalizeAccessCode(rawCode)
	ip := auth.RequestIP(r)

	var result map[string]any
	err = pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
		var participantID, paketID int
		var participantEmail, status string
		var paketNama, paketTipe, jenisPaket, orgStatus string
		var organizationID *int
		var organizationNama *string
		var maxAttempts int
		var isActive bool
		err := tx.QueryRow(r.Context(), `
			SELECT pp.participant_id, pp.paket_id, pp.participant_email, pp.status,
			       p.paket_nama, p.paket_tipe, p.jenis_paket, p.is_active, p.organization_id, p.max_attempts,
			       o.organization_nama, o.organization_status
			FROM package_participant pp
			JOIN paket_ujian p ON p.paket_id = pp.paket_id
			LEFT JOIN organization o ON o.organization_id = p.organization_id
			WHERE pp.access_code = $1
			FOR UPDATE OF pp`, code,
		).Scan(&participantID, &paketID, &participantEmail, &status, &paketNama, &paketTipe, &jenisPaket,
			&isActive, &organizationID, &maxAttempts, &organizationNama, &orgStatus)
		if err == nil {
			if !isActive || organizationID == nil || orgStatus != "ACTIVE" {
				return httpx.BadRequest("Kode akses tidak valid atau sudah tidak berlaku.", "INVALID_PACKAGE_CODE")
			}
			if status == "REDEEMED" {
				return httpx.BadRequest("Kode sudah pernah dipakai.", "CODE_ALREADY_USED")
			}
			if status != "UNUSED" {
				return httpx.BadRequest("Kode akses tidak valid atau sudah tidak berlaku.", "INVALID_PACKAGE_CODE")
			}
			var userEmail string
			if err := tx.QueryRow(r.Context(), `SELECT user_email FROM "user" WHERE user_id = $1`, principal.UserID).Scan(&userEmail); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return httpx.NotFound("User tidak ditemukan", "USER_NOT_FOUND")
				}
				return err
			}
			if !strings.EqualFold(userEmail, participantEmail) {
				return httpx.Forbidden("Kode ini terdaftar untuk email lain. Masuk dengan email yang didaftarkan admin.", "CODE_EMAIL_MISMATCH")
			}
			tag, err := tx.Exec(r.Context(), `
				UPDATE package_participant
				SET status = 'REDEEMED', user_id = $1, redeemed_at = now()
				WHERE participant_id = $2 AND status = 'UNUSED'`, principal.UserID, participantID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return httpx.BadRequest("Kode sudah pernah dipakai.", "CODE_ALREADY_USED")
			}

			sisaAttempt := maxAttempts
			expiredAt := time.Now().Add(time.Duration(s.Config.PrivateAccessDays) * 24 * time.Hour)
			var accessID int
			var grantedAttempts int
			var grantedExpiry time.Time
			err = tx.QueryRow(r.Context(), `
				INSERT INTO user_package_access (user_id, paket_id, sisa_attempt, expired_at, created_at, updated_at)
				VALUES ($1, $2, $3, $4, now(), now())
				ON CONFLICT (user_id, paket_id) DO UPDATE
				SET sisa_attempt = EXCLUDED.sisa_attempt, expired_at = EXCLUDED.expired_at, updated_at = now()
				RETURNING access_id, sisa_attempt, expired_at`,
				principal.UserID, paketID, sisaAttempt, expiredAt,
			).Scan(&accessID, &grantedAttempts, &grantedExpiry)
			if err != nil {
				return err
			}
			orgName := ""
			if organizationNama != nil {
				orgName = *organizationNama
			}
			result = map[string]any{
				"paket": map[string]any{
					"paket_id": paketID, "paket_nama": paketNama, "paket_tipe": paketTipe,
					"jenis_paket": jenisPaket, "organization_nama": orgName,
				},
				"sisa_attempt": grantedAttempts,
				"expired_at":   grantedExpiry,
				"via":          "participant",
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// Fallback kode per-paket lama.
		var isOrgActive bool
		err = tx.QueryRow(r.Context(), `
			SELECT p.paket_id, p.paket_nama, p.paket_tipe, p.jenis_paket, p.max_attempts,
			       p.organization_id, o.organization_nama,
			       (p.is_active AND p.organization_id IS NOT NULL AND o.organization_status = 'ACTIVE') AS valid
			FROM paket_ujian p LEFT JOIN organization o ON o.organization_id = p.organization_id
			WHERE p.paket_access_code = $1`, code,
		).Scan(&paketID, &paketNama, &paketTipe, &jenisPaket, &maxAttempts, &organizationID, &organizationNama, &isOrgActive)
		if errors.Is(err, pgx.ErrNoRows) || !isOrgActive {
			return httpx.BadRequest("Kode akses tidak valid atau sudah tidak berlaku.", "INVALID_PACKAGE_CODE")
		}
		if err != nil {
			return err
		}
		sisaAttempt := maxAttempts
		expiredAt := time.Now().Add(time.Duration(s.Config.PrivateAccessDays) * 24 * time.Hour)
		var accessID, grantedAttempts int
		var grantedExpiry time.Time
		err = tx.QueryRow(r.Context(), `
			INSERT INTO user_package_access (user_id, paket_id, sisa_attempt, expired_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, now(), now())
			ON CONFLICT (user_id, paket_id) DO UPDATE
			SET sisa_attempt = EXCLUDED.sisa_attempt, expired_at = EXCLUDED.expired_at, updated_at = now()
			RETURNING access_id, sisa_attempt, expired_at`,
			principal.UserID, paketID, sisaAttempt, expiredAt,
		).Scan(&accessID, &grantedAttempts, &grantedExpiry)
		if err != nil {
			return err
		}
		orgName := ""
		if organizationNama != nil {
			orgName = *organizationNama
		}
		result = map[string]any{
			"paket": map[string]any{
				"paket_id": paketID, "paket_nama": paketNama, "paket_tipe": paketTipe,
				"jenis_paket": jenisPaket, "organization_nama": orgName,
			},
			"sisa_attempt": grantedAttempts,
			"expired_at":   grantedExpiry,
			"via":          "package",
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "PACKAGE_CODE_REDEEM", ip, result)
	httpx.Created(w, result, "Kode berhasil ditebus — kelas/ujian masuk ke daftar Anda")
	return nil
}

func (s *Server) listAccess(w http.ResponseWriter, r *http.Request) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"1=1"}
	args := []any{}
	index := 1
	if userID := httpx.QueryInt(r, "user_id", 0); userID > 0 {
		conditions = append(conditions, fmt.Sprintf("a.user_id = $%d", index))
		args = append(args, userID)
		index++
	}
	if paketID := httpx.QueryInt(r, "paket_id", 0); paketID > 0 {
		conditions = append(conditions, fmt.Sprintf("a.paket_id = $%d", index))
		args = append(args, paketID)
		index++
	}
	if expired := httpx.QueryBoolPtr(r, "only_expired"); expired != nil && *expired {
		conditions = append(conditions, "a.expired_at < now()")
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM user_package_access a WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`
		SELECT %s, u.user_id, u.user_nama, u.user_email, u.user_phone, u.user_role,
		       u.email_verified_at, u.is_active, u.last_login_at, u.created_at, u.updated_at
		FROM user_package_access a
		JOIN paket_ujian p ON p.paket_id = a.paket_id
		JOIN "user" u ON u.user_id = a.user_id
		WHERE %s ORDER BY a.created_at DESC LIMIT $%d OFFSET $%d`, accessSelect, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []accessWithUser{}
	for rows.Next() {
		var item accessWithUser
		var soalCount int
		if err := rows.Scan(
			&item.AccessID, &item.UserID, &item.PaketID, &item.SisaAttempt, &item.ExpiredAt, &item.CreatedAt, &item.UpdatedAt,
			&item.Paket.PaketID, &item.Paket.PaketNama, &item.Paket.PaketTipe, &item.Paket.PaketHarga,
			&item.Paket.MaxAttempts, &item.Paket.DurationMinutes, &item.Paket.PaketJumlahSoal, &item.Paket.IsActive,
			&soalCount,
			&item.User.UserID, &item.User.UserNama, &item.User.UserEmail, &item.User.UserPhone, &item.User.UserRole,
			&item.User.EmailVerifiedAt, &item.User.IsActive, &item.User.LastLoginAt, &item.User.CreatedAt, &item.User.UpdatedAt,
		); err != nil {
			return err
		}
		item.Paket.Count = map[string]int{"soal_list": soalCount}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return nil
}

func (s *Server) grantAccess(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	userID := bodyInt(body, "user_id", 0)
	paketID := bodyInt(body, "paket_id", 0)
	expiredAt := parseDatePtr(body["expired_at"])
	if userID <= 0 || paketID <= 0 || expiredAt == nil {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "expired_at", "message": "user_id, paket_id, dan expired_at wajib valid"}})
	}
	if !expiredAt.After(time.Now()) {
		return httpx.BadRequest("expired_at harus berada di masa depan", "INVALID_EXPIRED_AT")
	}
	var exists int
	if err := s.Pool.QueryRow(r.Context(), `SELECT 1 FROM "user" WHERE user_id = $1`, userID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("User tidak ditemukan", "USER_NOT_FOUND")
		}
		return err
	}
	var maxAttempts int
	if err := s.Pool.QueryRow(r.Context(), `SELECT max_attempts FROM paket_ujian WHERE paket_id = $1`, paketID).Scan(&maxAttempts); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Paket ujian tidak ditemukan", "PACKAGE_NOT_FOUND")
		}
		return err
	}
	sisaAttempt := maxAttempts
	if raw, exists := body["sisa_attempt"]; exists && raw != nil {
		sisaAttempt = bodyInt(body, "sisa_attempt", maxAttempts)
		if sisaAttempt < 0 || sisaAttempt > 1000 {
			return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "sisa_attempt", "message": "sisa_attempt harus 0-1000"}})
		}
	}
	access, err := scanAccess(s.Pool.QueryRow(r.Context(), `
		INSERT INTO user_package_access (user_id, paket_id, sisa_attempt, expired_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, now(), now())
		ON CONFLICT (user_id, paket_id) DO UPDATE
		SET sisa_attempt = EXCLUDED.sisa_attempt, expired_at = EXCLUDED.expired_at, updated_at = now()
		RETURNING access_id, user_id, paket_id, sisa_attempt, expired_at, created_at, updated_at,
		(SELECT p.paket_id FROM paket_ujian p WHERE p.paket_id = $2),
		(SELECT p.paket_nama FROM paket_ujian p WHERE p.paket_id = $2),
		(SELECT p.paket_tipe FROM paket_ujian p WHERE p.paket_id = $2),
		(SELECT p.paket_harga FROM paket_ujian p WHERE p.paket_id = $2),
		(SELECT p.max_attempts FROM paket_ujian p WHERE p.paket_id = $2),
		(SELECT p.duration_minutes FROM paket_ujian p WHERE p.paket_id = $2),
		(SELECT p.paket_jumlah_soal FROM paket_ujian p WHERE p.paket_id = $2),
		(SELECT p.is_active FROM paket_ujian p WHERE p.paket_id = $2),
		(SELECT count(*) FROM soal_bank s WHERE s.paket_id = $2)`, userID, paketID, sisaAttempt, *expiredAt))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ACCESS_GRANT", auth.RequestIP(r), map[string]any{
		"target_user_id": userID, "paket_id": paketID, "sisa_attempt": sisaAttempt, "expired_at": *expiredAt,
	})
	httpx.Created(w, access, "Akses paket berhasil diberikan")
	return nil
}

func (s *Server) revokeAccess(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	var userID, paketID int
	err = s.Pool.QueryRow(r.Context(), `DELETE FROM user_package_access WHERE access_id = $1 RETURNING user_id, paket_id`, id).Scan(&userID, &paketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Akses paket tidak ditemukan", "ACCESS_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ACCESS_REVOKE", auth.RequestIP(r), map[string]any{
		"target_user_id": userID, "paket_id": paketID,
	})
	httpx.Success(w, nil, "Akses paket berhasil dicabut")
	return nil
}
