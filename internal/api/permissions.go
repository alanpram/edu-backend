package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
	"github.com/edumaster/backend-go/internal/permissions"
)

func (s *Server) registerPermissions(api chi.Router) {
	api.Route("/permissions", func(r chi.Router) {
		r.Use(s.Auth.Authenticate)
		r.Get("/me", httpx.Wrap(s.permissionMe))
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequirePermission(s.Permissions, "admin.permissions.manage"))
			r.Get("/", httpx.Wrap(s.permissionMatrix))
			r.Put("/", httpx.Wrap(s.permissionUpdate))
			r.Post("/reset", httpx.Wrap(s.permissionReset))
		})
	})
}

func (s *Server) permissionMe(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	keys, err := s.Permissions.KeysForRole(r.Context(), principal.UserRole)
	if err != nil {
		return err
	}
	httpx.Success(w, map[string]any{"role": principal.UserRole, "permissions": keys}, "")
	return nil
}

func (s *Server) permissionMatrix(w http.ResponseWriter, r *http.Request) error {
	result, err := s.Permissions.Matrix(r.Context())
	if err != nil {
		return err
	}
	httpx.Success(w, result, "")
	return nil
}

func (s *Server) permissionUpdate(w http.ResponseWriter, r *http.Request) error {
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	rawUpdates := bodySlice(body, "updates")
	if len(rawUpdates) == 0 || len(rawUpdates) > 500 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "updates", "message": "Minimal satu perubahan permission"}})
	}
	updates := make([]permissions.Update, 0, len(rawUpdates))
	for _, raw := range rawUpdates {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		allowed, _ := item["allowed"].(bool)
		updates = append(updates, permissions.Update{
			Role:          bodyString(item, "role"),
			PermissionKey: bodyString(item, "permission_key"),
			Allowed:       allowed,
		})
	}
	result, err := s.Permissions.Update(r.Context(), updates)
	if err != nil {
		return err
	}
	httpx.Success(w, result, "Hak akses role berhasil diperbarui.")
	return nil
}

func (s *Server) permissionReset(w http.ResponseWriter, r *http.Request) error {
	result, err := s.Permissions.Reset(r.Context())
	if err != nil {
		return err
	}
	httpx.Success(w, result, "Hak akses role berhasil dikembalikan ke default.")
	return nil
}
