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
	"github.com/edumaster/backend-go/internal/middleware"
)

type leaderboardEntry struct {
	Rank          int       `json:"rank"`
	UserID        int       `json:"user_id"`
	UserNama      string    `json:"user_nama"`
	BestScore     float64   `json:"best_score"`
	SubmittedAt   time.Time `json:"submitted_at"`
	TotalAttempts int       `json:"total_attempts"`
}

const leaderboardCTE = `
	WITH best AS (
		SELECT DISTINCT ON (es.user_id) es.user_id, es.session_id, es.final_score, es.submitted_at
		FROM exam_session es
		WHERE es.paket_id = $1 AND es.status = 'completed' AND es.final_score IS NOT NULL
		ORDER BY es.user_id, es.final_score DESC, es.submitted_at ASC
	), attempt_totals AS (
		SELECT es.user_id, count(*)::int AS total_attempts
		FROM exam_session es
		WHERE es.paket_id = $1 AND es.status = 'completed'
		GROUP BY es.user_id
	), ranked AS (
		SELECT b.user_id, u.user_nama, b.final_score AS best_score, b.submitted_at, a.total_attempts,
			(ROW_NUMBER() OVER (ORDER BY b.final_score DESC, b.submitted_at ASC, b.user_id ASC))::int AS rank
		FROM best b JOIN "user" u ON u.user_id = b.user_id
		JOIN attempt_totals a ON a.user_id = b.user_id
	)`

func (s *Server) registerLeaderboard(api chi.Router) {
	api.With(s.Auth.Authenticate, middleware.RequirePermission(s.Permissions, "leaderboard.view")).
		Get("/leaderboard/{paketId}", httpx.Wrap(s.leaderboardGet))
}

func scanLeaderboardRow(row pgx.Row) (leaderboardEntry, error) {
	var item leaderboardEntry
	err := row.Scan(&item.UserID, &item.UserNama, &item.BestScore, &item.SubmittedAt, &item.TotalAttempts, &item.Rank)
	return item, err
}

func (s *Server) leaderboardGet(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	paketID, err := httpx.IDParam(r, "paketId")
	if err != nil {
		return err
	}
	paket, err := s.lmsPackage(r.Context(), paketID)
	if err != nil {
		return err
	}
	if paket.OrganizationID != nil {
		if err := s.assertOrganizationActive(r.Context(), paket.OrganizationID); err != nil {
			return err
		}
		if !s.canPermission(r, "admin.packages.view") {
			hasAccess, err := s.hasActivePackageAccess(r.Context(), principal.UserID, paketID)
			if err != nil {
				return err
			}
			if !hasAccess {
				member, err := s.requireMembership(r.Context(), principal.UserID, paket.OrganizationID)
				if err != nil {
					return err
				}
				if member.MemberStatus != "ACTIVE" || member.Organization.OrganizationStatus != "ACTIVE" {
					return httpx.Forbidden("Leaderboard ini hanya tersedia untuk anggota organisasi.", "ORG_MEMBERSHIP_REQUIRED")
				}
			}
		}
	}
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 50))
	rows, err := s.Pool.Query(r.Context(), leaderboardCTE+`
		SELECT user_id, user_nama, best_score, submitted_at, total_attempts, rank
		FROM ranked ORDER BY rank ASC LIMIT $2 OFFSET $3`, paketID, limit, offset)
	if err != nil {
		return err
	}
	entries := []leaderboardEntry{}
	for rows.Next() {
		item, err := scanLeaderboardRow(rows)
		if err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	var current *leaderboardEntry
	for index := range entries {
		if entries[index].UserID == principal.UserID {
			value := entries[index]
			current = &value
			break
		}
	}
	if current == nil {
		var value leaderboardEntry
		err := s.Pool.QueryRow(r.Context(), leaderboardCTE+`
			SELECT user_id, user_nama, best_score, submitted_at, total_attempts, rank
			FROM ranked WHERE user_id = $2`, paketID, principal.UserID).
			Scan(&value.UserID, &value.UserNama, &value.BestScore, &value.SubmittedAt, &value.TotalAttempts, &value.Rank)
		if err == nil {
			current = &value
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(DISTINCT user_id)::int FROM exam_session
		WHERE paket_id = $1 AND status = 'completed' AND final_score IS NOT NULL`, paketID).Scan(&total); err != nil {
		return err
	}
	httpx.Success(w, map[string]any{
		"paket": map[string]any{
			"paket_id": paket.PaketID, "paket_nama": paket.PaketNama, "paket_tipe": paket.PaketTipe,
		},
		"entries":            entries,
		"current_user":       current,
		"total_participants": total,
	}, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return nil
}

type Discussion struct {
	DiskusiID int       `json:"diskusi_id"`
	SoalID    int       `json:"soal_id"`
	UserID    int       `json:"user_id"`
	Komentar  string    `json:"komentar"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      map[string]any
}

func (s *Server) registerDiscussions(api chi.Router) {
	api.With(s.Auth.Authenticate, middleware.RequirePermission(s.Permissions, "discussion.participate")).
		Get("/packages/{paketId}/soal/{soalId}/discussions", httpx.Wrap(s.discussionList))
	api.With(s.Auth.Authenticate, middleware.RequirePermission(s.Permissions, "discussion.participate")).
		Post("/packages/{paketId}/soal/{soalId}/discussions", httpx.Wrap(s.discussionCreate))
	api.With(s.Auth.Authenticate, middleware.RequirePermission(s.Permissions, "discussion.participate")).
		Delete("/packages/{paketId}/soal/{soalId}/discussions/{diskusiId}", httpx.Wrap(s.discussionRemove))
}

func (s *Server) assertDiscussionAccess(ctx context.Context, userID int, paketID, soalID int) error {
	var questionPaket *int
	err := s.Pool.QueryRow(ctx, `SELECT paket_id FROM soal_bank WHERE soal_id = $1`, soalID).Scan(&questionPaket)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (questionPaket == nil || *questionPaket != paketID)) {
		return httpx.NotFound("Soal tidak ditemukan pada paket yang dimaksud.", "QUESTION_NOT_IN_PACKAGE")
	}
	if err != nil {
		return err
	}
	return nil
}

func (s *Server) assertDiscussionAccessForRequest(r *http.Request, userID int, paketID, soalID int) error {
	if err := s.assertDiscussionAccess(r.Context(), userID, paketID, soalID); err != nil {
		return err
	}
	if s.canPermission(r, "admin.questions.view") {
		return nil
	}
	var hasAccess bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM user_package_access
		WHERE user_id = $1 AND paket_id = $2 AND expired_at > now())`, userID, paketID).Scan(&hasAccess); err != nil {
		return err
	}
	if hasAccess {
		return nil
	}
	var completed bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM exam_session
		WHERE user_id = $1 AND paket_id = $2 AND status = 'completed')`, userID, paketID).Scan(&completed); err != nil {
		return err
	}
	if completed {
		return nil
	}
	return httpx.Forbidden("Anda harus memiliki akses aktif atau telah menyelesaikan ujian pada paket ini untuk berdiskusi.", "DISCUSSION_ACCESS_DENIED")
}

func scanDiscussion(row pgx.Row) (Discussion, error) {
	var item Discussion
	var userID int
	var userNama, userRole string
	err := row.Scan(&item.DiskusiID, &item.SoalID, &item.UserID, &item.Komentar, &item.CreatedAt,
		&item.UpdatedAt, &userID, &userNama, &userRole)
	if err != nil {
		return item, err
	}
	item.User = map[string]any{"user_id": userID, "user_nama": userNama, "user_role": userRole}
	return item, nil
}

func (s *Server) discussionList(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	paketID, err := httpx.IDParam(r, "paketId")
	if err != nil {
		return err
	}
	soalID, err := httpx.IDParam(r, "soalId")
	if err != nil {
		return err
	}
	if err := s.assertDiscussionAccessForRequest(r, principal.UserID, paketID, soalID); err != nil {
		return err
	}
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM diskusi_soal WHERE soal_id = $1`, soalID).Scan(&total); err != nil {
		return err
	}
	rows, err := s.Pool.Query(r.Context(), `SELECT d.diskusi_id, d.soal_id, d.user_id, d.komentar, d.created_at,
		d.updated_at, u.user_id, u.user_nama, u.user_role
		FROM diskusi_soal d JOIN "user" u ON u.user_id = d.user_id
		WHERE d.soal_id = $1 ORDER BY d.created_at ASC LIMIT $2 OFFSET $3`, soalID, limit, offset)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Discussion{}
	for rows.Next() {
		item, err := scanDiscussion(rows)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return rows.Err()
}

func (s *Server) discussionCreate(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	paketID, err := httpx.IDParam(r, "paketId")
	if err != nil {
		return err
	}
	soalID, err := httpx.IDParam(r, "soalId")
	if err != nil {
		return err
	}
	if err := s.assertDiscussionAccessForRequest(r, principal.UserID, paketID, soalID); err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	komentar := strings.TrimSpace(bodyString(body, "komentar"))
	if len([]rune(komentar)) < 2 || len([]rune(komentar)) > 2000 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR",
			[]map[string]string{{"field": "komentar", "message": "Komentar harus 2-2000 karakter"}})
	}
	item, err := scanDiscussion(s.Pool.QueryRow(r.Context(), `WITH inserted AS (
		INSERT INTO diskusi_soal (soal_id, user_id, komentar, created_at, updated_at)
		VALUES ($1,$2,$3,now(),now()) RETURNING diskusi_id, soal_id, user_id, komentar, created_at, updated_at
	) SELECT i.diskusi_id, i.soal_id, i.user_id, i.komentar, i.created_at, i.updated_at,
		u.user_id, u.user_nama, u.user_role FROM inserted i JOIN "user" u ON u.user_id = i.user_id`,
		soalID, principal.UserID, komentar))
	if err != nil {
		return err
	}
	httpx.Created(w, item, "Komentar berhasil ditambahkan")
	return nil
}

func (s *Server) discussionRemove(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	paketID, err := httpx.IDParam(r, "paketId")
	if err != nil {
		return err
	}
	soalID, err := httpx.IDParam(r, "soalId")
	if err != nil {
		return err
	}
	diskusiID, err := httpx.IDParam(r, "diskusiId")
	if err != nil {
		return err
	}
	if err := s.assertDiscussionAccess(r.Context(), principal.UserID, paketID, soalID); err != nil {
		return err
	}
	var ownerID int
	err = s.Pool.QueryRow(r.Context(), `SELECT user_id FROM diskusi_soal WHERE diskusi_id = $1 AND soal_id = $2`, diskusiID, soalID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Diskusi tidak ditemukan", "DISCUSSION_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if !s.canPermission(r, "admin.questions.view") && ownerID != principal.UserID {
		return httpx.Forbidden("Anda hanya dapat menghapus komentar milik sendiri.", "DISCUSSION_FORBIDDEN")
	}
	if _, err := s.Pool.Exec(r.Context(), `DELETE FROM diskusi_soal WHERE diskusi_id = $1`, diskusiID); err != nil {
		return err
	}
	httpx.Success(w, nil, "Komentar berhasil dihapus")
	return nil
}
