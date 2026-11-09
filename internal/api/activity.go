package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
)

type activityRow struct {
	LogID     int             `json:"log_id"`
	UserID    int             `json:"user_id"`
	Aktivitas string          `json:"aktivitas"`
	IPAddress *string         `json:"ip_address"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
	User      activityUser    `json:"user"`
}

type activityUser struct {
	UserID    int    `json:"user_id"`
	UserNama  string `json:"user_nama"`
	UserEmail string `json:"user_email"`
	UserRole  string `json:"user_role"`
}

func (s *Server) registerActivity(api chi.Router) {
	api.Route("/activity-logs", func(r chi.Router) {
		r.Use(s.Auth.Authenticate)
		r.Get("/me", httpx.Wrap(s.myActivityLogs))
		r.With(middleware.RequirePermission(s.Permissions, "admin.audit.view")).Get("/", httpx.Wrap(s.listActivityLogs))
	})
}

func (s *Server) myActivityLogs(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	return s.activityLogs(w, r, &principal.UserID)
}

func (s *Server) listActivityLogs(w http.ResponseWriter, r *http.Request) error {
	var userID *int
	if value := httpx.QueryInt(r, "user_id", 0); value > 0 {
		userID = &value
	}
	return s.activityLogs(w, r, userID)
}

func (s *Server) activityLogs(w http.ResponseWriter, r *http.Request, forcedUserID *int) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"1=1"}
	args := []any{}
	index := 1

	if forcedUserID != nil {
		conditions = append(conditions, fmt.Sprintf("l.user_id = $%d", index))
		args = append(args, *forcedUserID)
		index++
	}
	if activity := strings.TrimSpace(r.URL.Query().Get("aktivitas")); activity != "" {
		conditions = append(conditions, fmt.Sprintf("l.aktivitas ILIKE $%d", index))
		args = append(args, "%"+activity+"%")
		index++
	}
	if from := parseDate(r.URL.Query().Get("from")); from != nil {
		conditions = append(conditions, fmt.Sprintf("l.created_at >= $%d", index))
		args = append(args, *from)
		index++
	}
	if to := parseDate(r.URL.Query().Get("to")); to != nil {
		conditions = append(conditions, fmt.Sprintf("l.created_at <= $%d", index))
		args = append(args, *to)
		index++
	}
	where := strings.Join(conditions, " AND ")

	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM activity_log l WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`
		SELECT l.log_id, l.user_id, l.aktivitas, l.ip_address, l.metadata, l.created_at,
		       u.user_id, u.user_nama, u.user_email, u.user_role
		FROM activity_log l JOIN "user" u ON u.user_id = l.user_id
		WHERE %s ORDER BY l.created_at DESC LIMIT $%d OFFSET $%d`, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]activityRow, 0, limit)
	for rows.Next() {
		var item activityRow
		var metadata []byte
		if err := rows.Scan(&item.LogID, &item.UserID, &item.Aktivitas, &item.IPAddress, &metadata, &item.CreatedAt,
			&item.User.UserID, &item.User.UserNama, &item.User.UserEmail, &item.User.UserRole); err != nil {
			return err
		}
		if len(metadata) > 0 {
			item.Metadata = json.RawMessage(metadata)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return nil
}

func parseDate(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		if date, err2 := time.Parse("2006-01-02", value); err2 == nil {
			return &date
		}
		return nil
	}
	return &parsed
}
