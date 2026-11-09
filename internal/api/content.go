package api

import (
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

type Faq struct {
	FaqID            int       `json:"faq_id"`
	FaqPertanyaan    string    `json:"faq_pertanyaan"`
	FaqJawaban       string    `json:"faq_jawaban"`
	FaqKategori      string    `json:"faq_kategori"`
	FaqUrutan        int       `json:"faq_urutan"`
	FaqAktif         bool      `json:"faq_aktif"`
	FaqLanding       bool      `json:"faq_landing"`
	FaqLandingUrutan int       `json:"faq_landing_urutan"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Testimoni struct {
	TestimoniID            int       `json:"testimoni_id"`
	TestimoniNama          string    `json:"testimoni_nama"`
	TestimoniPeran         *string   `json:"testimoni_peran"`
	TestimoniIsi           string    `json:"testimoni_isi"`
	TestimoniRating        int       `json:"testimoni_rating"`
	TestimoniFotoURL       *string   `json:"testimoni_foto_url"`
	TestimoniUrutan        int       `json:"testimoni_urutan"`
	TestimoniAktif         bool      `json:"testimoni_aktif"`
	TestimoniLanding       bool      `json:"testimoni_landing"`
	TestimoniLandingUrutan int       `json:"testimoni_landing_urutan"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type Artikel struct {
	ArtikelID            int       `json:"artikel_id"`
	ArtikelSlug          string    `json:"artikel_slug"`
	ArtikelJudul         string    `json:"artikel_judul"`
	ArtikelExcerpt       string    `json:"artikel_excerpt"`
	ArtikelIsi           string    `json:"artikel_isi"`
	ArtikelKategori      string    `json:"artikel_kategori"`
	ArtikelPenulis       string    `json:"artikel_penulis"`
	ArtikelReadMinutes   int       `json:"artikel_read_minutes"`
	ArtikelFotoURL       *string   `json:"artikel_foto_url"`
	ArtikelFotoAlt       *string   `json:"artikel_foto_alt"`
	ArtikelPublishedAt   time.Time `json:"artikel_published_at"`
	ArtikelAktif         bool      `json:"artikel_aktif"`
	ArtikelLanding       bool      `json:"artikel_landing"`
	ArtikelLandingUrutan int       `json:"artikel_landing_urutan"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (s *Server) registerContent(api chi.Router) {
	api.Route("/faqs", func(r chi.Router) {
		r.With(s.Auth.OptionalAuthenticate).Get("/", httpx.Wrap(s.faqList))
		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Authenticate)
			r.With(middleware.RequirePermission(s.Permissions, "admin.faq.view")).
				Get("/{id}", httpx.Wrap(s.faqDetail))
			r.With(middleware.RequirePermission(s.Permissions, "admin.faq.manage")).
				Post("/", httpx.Wrap(s.faqCreate))
			r.With(middleware.RequirePermission(s.Permissions, "admin.faq.manage")).
				Put("/{id}", httpx.Wrap(s.faqUpdate))
			r.With(middleware.RequirePermission(s.Permissions, "admin.faq.manage")).
				Delete("/{id}", httpx.Wrap(s.faqRemove))
		})
	})

	api.Route("/testimonials", func(r chi.Router) {
		r.With(s.Auth.OptionalAuthenticate).Get("/", httpx.Wrap(s.testimoniList))
		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Authenticate)
			r.With(middleware.RequirePermission(s.Permissions, "admin.testimonials.view")).
				Get("/{id}", httpx.Wrap(s.testimoniDetail))
			r.With(middleware.RequirePermission(s.Permissions, "admin.testimonials.manage")).
				Post("/", httpx.Wrap(s.testimoniCreate))
			r.With(middleware.RequirePermission(s.Permissions, "admin.testimonials.manage")).
				Put("/{id}", httpx.Wrap(s.testimoniUpdate))
			r.With(middleware.RequirePermission(s.Permissions, "admin.testimonials.manage")).
				Delete("/{id}", httpx.Wrap(s.testimoniRemove))
		})
	})

	api.Route("/articles", func(r chi.Router) {
		r.With(s.Auth.OptionalAuthenticate).Get("/", httpx.Wrap(s.articleList))
		r.Get("/categories", httpx.Wrap(s.articleCategories))
		r.With(s.Auth.OptionalAuthenticate).Get("/slug/{slug}", httpx.Wrap(s.articleDetail))
		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Authenticate)
			r.With(middleware.RequirePermission(s.Permissions, "admin.articles.view")).
				Get("/{id}", httpx.Wrap(s.articleAdminDetail))
			r.With(middleware.RequirePermission(s.Permissions, "admin.articles.manage")).
				Post("/", httpx.Wrap(s.articleCreate))
			r.With(middleware.RequirePermission(s.Permissions, "admin.articles.manage")).
				Put("/{id}", httpx.Wrap(s.articleUpdate))
			r.With(middleware.RequirePermission(s.Permissions, "admin.articles.manage")).
				Delete("/{id}", httpx.Wrap(s.articleRemove))
		})
	})
}

func (s *Server) canPermission(r *http.Request, permission string) bool {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		return false
	}
	allowed, err := s.Permissions.HasPermission(r.Context(), principal.UserRole, permission)
	return err == nil && allowed
}

func scanFaq(row pgx.Row) (Faq, error) {
	var item Faq
	err := row.Scan(&item.FaqID, &item.FaqPertanyaan, &item.FaqJawaban, &item.FaqKategori, &item.FaqUrutan,
		&item.FaqAktif, &item.FaqLanding, &item.FaqLandingUrutan, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Server) faqList(w http.ResponseWriter, r *http.Request) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"1=1"}
	args := []any{}
	index := 1
	if s.canPermission(r, "admin.faq.view") {
		if active := httpx.QueryBoolPtr(r, "faq_aktif"); active != nil {
			conditions = append(conditions, fmt.Sprintf("faq_aktif = $%d", index))
			args = append(args, *active)
			index++
		}
	} else {
		conditions = append(conditions, "faq_aktif = true")
	}
	if kategori := strings.TrimSpace(r.URL.Query().Get("faq_kategori")); kategori != "" {
		conditions = append(conditions, fmt.Sprintf("faq_kategori = $%d", index))
		args = append(args, kategori)
		index++
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("(faq_pertanyaan ILIKE $%d OR faq_jawaban ILIKE $%d)", index, index))
		args = append(args, "%"+search+"%")
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM faq WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`SELECT faq_id, faq_pertanyaan, faq_jawaban, faq_kategori, faq_urutan,
		faq_aktif, faq_landing, faq_landing_urutan, created_at, updated_at
		FROM faq WHERE %s ORDER BY faq_urutan ASC, faq_id ASC LIMIT $%d OFFSET $%d`, where, index, index+1)
	rows, err := s.Pool.Query(r.Context(), query, append(args, limit, offset)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Faq{}
	for rows.Next() {
		item, err := scanFaq(rows)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return rows.Err()
}

func (s *Server) faqDetail(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	item, err := scanFaq(s.Pool.QueryRow(r.Context(), `SELECT faq_id, faq_pertanyaan, faq_jawaban, faq_kategori,
		faq_urutan, faq_aktif, faq_landing, faq_landing_urutan, created_at, updated_at FROM faq WHERE faq_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("FAQ tidak ditemukan", "FAQ_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	httpx.Success(w, item, "")
	return nil
}

func faqValidation(body map[string]any, partial bool) (map[string]any, map[string]string) {
	fields := map[string]string{}
	values := map[string]any{}
	if !partial || bodyHasKey(body, "faq_pertanyaan") {
		value := strings.TrimSpace(bodyString(body, "faq_pertanyaan"))
		if len([]rune(value)) < 5 || len([]rune(value)) > 500 {
			fields["faq_pertanyaan"] = "Pertanyaan harus 5-500 karakter"
		} else {
			values["faq_pertanyaan"] = value
		}
	}
	if !partial || bodyHasKey(body, "faq_jawaban") {
		value := strings.TrimSpace(bodyString(body, "faq_jawaban"))
		if len([]rune(value)) < 10 || len([]rune(value)) > 10000 {
			fields["faq_jawaban"] = "Jawaban harus 10-10000 karakter"
		} else {
			values["faq_jawaban"] = value
		}
	}
	if !partial || bodyHasKey(body, "faq_kategori") {
		value := strings.TrimSpace(bodyString(body, "faq_kategori"))
		if value == "" {
			value = "Umum"
		}
		if len([]rune(value)) < 2 || len([]rune(value)) > 100 {
			fields["faq_kategori"] = "Kategori harus 2-100 karakter"
		} else {
			values["faq_kategori"] = value
		}
	}
	for _, key := range []string{"faq_urutan", "faq_landing_urutan"} {
		if !partial || bodyHasKey(body, key) {
			value := bodyInt(body, key, 0)
			if value < 0 || value > 9999 {
				fields[key] = "Urutan harus 0-9999"
			} else {
				values[key] = value
			}
		}
	}
	for _, key := range []string{"faq_aktif", "faq_landing"} {
		if !partial || bodyHasKey(body, key) {
			values[key] = bodyBool(body, key, key == "faq_aktif")
		}
	}
	return values, fields
}

func (s *Server) faqCreate(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	values, fields := faqValidation(body, false)
	if len(fields) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(fields))
	}
	var item Faq
	err = s.Pool.QueryRow(r.Context(), `
		INSERT INTO faq (faq_pertanyaan, faq_jawaban, faq_kategori, faq_urutan, faq_aktif, faq_landing, faq_landing_urutan, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,now(),now())
		RETURNING faq_id, faq_pertanyaan, faq_jawaban, faq_kategori, faq_urutan, faq_aktif, faq_landing, faq_landing_urutan, created_at, updated_at`,
		values["faq_pertanyaan"], values["faq_jawaban"], values["faq_kategori"], values["faq_urutan"],
		values["faq_aktif"], values["faq_landing"], values["faq_landing_urutan"]).
		Scan(&item.FaqID, &item.FaqPertanyaan, &item.FaqJawaban, &item.FaqKategori, &item.FaqUrutan,
			&item.FaqAktif, &item.FaqLanding, &item.FaqLandingUrutan, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "FAQ_CREATE", auth.RequestIP(r), map[string]any{"faq_id": item.FaqID})
	httpx.Created(w, item, "FAQ berhasil dibuat")
	return nil
}

func (s *Server) faqUpdate(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	values, fields := faqValidation(body, true)
	if len(fields) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(fields))
	}
	set, args, index := updateSet(values, []string{"faq_pertanyaan", "faq_jawaban", "faq_kategori", "faq_urutan", "faq_aktif", "faq_landing", "faq_landing_urutan"})
	if len(set) > 0 {
		set = append(set, "updated_at = now()")
		args = append(args, id)
		command, err := s.Pool.Exec(r.Context(), fmt.Sprintf(`UPDATE faq SET %s WHERE faq_id = $%d`, strings.Join(set, ", "), index), args...)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 {
			return httpx.NotFound("FAQ tidak ditemukan", "FAQ_NOT_FOUND")
		}
	}
	var item Faq
	err = s.Pool.QueryRow(r.Context(), `SELECT faq_id, faq_pertanyaan, faq_jawaban, faq_kategori, faq_urutan,
		faq_aktif, faq_landing, faq_landing_urutan, created_at, updated_at FROM faq WHERE faq_id = $1`, id).
		Scan(&item.FaqID, &item.FaqPertanyaan, &item.FaqJawaban, &item.FaqKategori, &item.FaqUrutan,
			&item.FaqAktif, &item.FaqLanding, &item.FaqLandingUrutan, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("FAQ tidak ditemukan", "FAQ_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "FAQ_UPDATE", auth.RequestIP(r), map[string]any{"faq_id": id})
	httpx.Success(w, item, "FAQ berhasil diperbarui")
	return nil
}

func (s *Server) faqRemove(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	command, err := s.Pool.Exec(r.Context(), `DELETE FROM faq WHERE faq_id = $1`, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return httpx.NotFound("FAQ tidak ditemukan", "FAQ_NOT_FOUND")
	}
	s.Activity.Record(r.Context(), principal.UserID, "FAQ_DELETE", auth.RequestIP(r), map[string]any{"faq_id": id})
	httpx.Success(w, nil, "FAQ berhasil dihapus")
	return nil
}

func scanTestimoni(row pgx.Row) (Testimoni, error) {
	var item Testimoni
	err := row.Scan(&item.TestimoniID, &item.TestimoniNama, &item.TestimoniPeran, &item.TestimoniIsi,
		&item.TestimoniRating, &item.TestimoniFotoURL, &item.TestimoniUrutan, &item.TestimoniAktif,
		&item.TestimoniLanding, &item.TestimoniLandingUrutan, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Server) testimoniList(w http.ResponseWriter, r *http.Request) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"1=1"}
	args := []any{}
	index := 1
	if s.canPermission(r, "admin.testimonials.view") {
		if active := httpx.QueryBoolPtr(r, "testimoni_aktif"); active != nil {
			conditions = append(conditions, fmt.Sprintf("testimoni_aktif = $%d", index))
			args = append(args, *active)
			index++
		}
	} else {
		conditions = append(conditions, "testimoni_aktif = true")
	}
	if rating := httpx.QueryInt(r, "testimoni_rating", 0); rating > 0 {
		conditions = append(conditions, fmt.Sprintf("testimoni_rating = $%d", index))
		args = append(args, rating)
		index++
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("(testimoni_nama ILIKE $%d OR testimoni_peran ILIKE $%d OR testimoni_isi ILIKE $%d)", index, index, index))
		args = append(args, "%"+search+"%")
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM testimoni WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`SELECT testimoni_id, testimoni_nama, testimoni_peran, testimoni_isi, testimoni_rating,
		testimoni_foto_url, testimoni_urutan, testimoni_aktif, testimoni_landing, testimoni_landing_urutan, created_at, updated_at
		FROM testimoni WHERE %s ORDER BY testimoni_urutan ASC, testimoni_id DESC LIMIT $%d OFFSET $%d`, where, index, index+1)
	rows, err := s.Pool.Query(r.Context(), query, append(args, limit, offset)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Testimoni{}
	for rows.Next() {
		item, err := scanTestimoni(rows)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return rows.Err()
}

func (s *Server) testimoniDetail(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	item, err := scanTestimoni(s.Pool.QueryRow(r.Context(), `SELECT testimoni_id, testimoni_nama, testimoni_peran,
		testimoni_isi, testimoni_rating, testimoni_foto_url, testimoni_urutan, testimoni_aktif,
		testimoni_landing, testimoni_landing_urutan, created_at, updated_at FROM testimoni WHERE testimoni_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Testimoni tidak ditemukan", "TESTIMONI_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	httpx.Success(w, item, "")
	return nil
}

func testimoniValidation(body map[string]any, partial bool) (map[string]any, map[string]string) {
	fields := map[string]string{}
	values := map[string]any{}
	if !partial || bodyHasKey(body, "testimoni_nama") {
		value := strings.TrimSpace(bodyString(body, "testimoni_nama"))
		if len([]rune(value)) < 2 || len([]rune(value)) > 150 {
			fields["testimoni_nama"] = "Nama harus 2-150 karakter"
		} else {
			values["testimoni_nama"] = value
		}
	}
	if !partial || bodyHasKey(body, "testimoni_peran") {
		value := bodyStringPtr(body, "testimoni_peran")
		if value != nil && len([]rune(*value)) > 150 {
			fields["testimoni_peran"] = "Peran maksimal 150 karakter"
		} else {
			values["testimoni_peran"] = value
		}
	}
	if !partial || bodyHasKey(body, "testimoni_isi") {
		value := strings.TrimSpace(bodyString(body, "testimoni_isi"))
		if len([]rune(value)) < 10 || len([]rune(value)) > 5000 {
			fields["testimoni_isi"] = "Isi testimoni harus 10-5000 karakter"
		} else {
			values["testimoni_isi"] = value
		}
	}
	if !partial || bodyHasKey(body, "testimoni_rating") {
		value := bodyInt(body, "testimoni_rating", 5)
		if value < 1 || value > 5 {
			fields["testimoni_rating"] = "Rating harus 1-5"
		} else {
			values["testimoni_rating"] = value
		}
	}
	if !partial || bodyHasKey(body, "testimoni_foto_url") {
		value := bodyStringPtr(body, "testimoni_foto_url")
		if value != nil && len([]rune(*value)) > 500 {
			fields["testimoni_foto_url"] = "URL foto maksimal 500 karakter"
		} else {
			values["testimoni_foto_url"] = value
		}
	}
	for _, key := range []string{"testimoni_urutan", "testimoni_landing_urutan"} {
		if !partial || bodyHasKey(body, key) {
			value := bodyInt(body, key, 0)
			if value < 0 || value > 9999 {
				fields[key] = "Urutan harus 0-9999"
			} else {
				values[key] = value
			}
		}
	}
	for _, key := range []string{"testimoni_aktif", "testimoni_landing"} {
		if !partial || bodyHasKey(body, key) {
			values[key] = bodyBool(body, key, key == "testimoni_aktif")
		}
	}
	return values, fields
}

func (s *Server) testimoniCreate(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	values, fields := testimoniValidation(body, false)
	if len(fields) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(fields))
	}
	var item Testimoni
	err = s.Pool.QueryRow(r.Context(), `
		INSERT INTO testimoni (testimoni_nama, testimoni_peran, testimoni_isi, testimoni_rating, testimoni_foto_url,
			testimoni_urutan, testimoni_aktif, testimoni_landing, testimoni_landing_urutan, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now(),now())
		RETURNING testimoni_id, testimoni_nama, testimoni_peran, testimoni_isi, testimoni_rating,
			testimoni_foto_url, testimoni_urutan, testimoni_aktif, testimoni_landing, testimoni_landing_urutan,
			created_at, updated_at`,
		values["testimoni_nama"], values["testimoni_peran"], values["testimoni_isi"], values["testimoni_rating"],
		values["testimoni_foto_url"], values["testimoni_urutan"], values["testimoni_aktif"],
		values["testimoni_landing"], values["testimoni_landing_urutan"]).
		Scan(&item.TestimoniID, &item.TestimoniNama, &item.TestimoniPeran, &item.TestimoniIsi, &item.TestimoniRating,
			&item.TestimoniFotoURL, &item.TestimoniUrutan, &item.TestimoniAktif, &item.TestimoniLanding,
			&item.TestimoniLandingUrutan, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "TESTIMONI_CREATE", auth.RequestIP(r), map[string]any{"testimoni_id": item.TestimoniID})
	httpx.Created(w, item, "Testimoni berhasil dibuat")
	return nil
}

func (s *Server) testimoniUpdate(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	values, fields := testimoniValidation(body, true)
	if len(fields) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(fields))
	}
	columns := []string{"testimoni_nama", "testimoni_peran", "testimoni_isi", "testimoni_rating", "testimoni_foto_url",
		"testimoni_urutan", "testimoni_aktif", "testimoni_landing", "testimoni_landing_urutan"}
	set, args, index := updateSet(values, columns)
	exists := true
	if len(set) > 0 {
		set = append(set, "updated_at = now()")
		args = append(args, id)
		command, err := s.Pool.Exec(r.Context(), fmt.Sprintf(`UPDATE testimoni SET %s WHERE testimoni_id = $%d`, strings.Join(set, ", "), index), args...)
		if err != nil {
			return err
		}
		exists = command.RowsAffected() > 0
	} else {
		var dummy int
		err := s.Pool.QueryRow(r.Context(), `SELECT 1 FROM testimoni WHERE testimoni_id = $1`, id).Scan(&dummy)
		exists = !errors.Is(err, pgx.ErrNoRows)
	}
	if !exists {
		return httpx.NotFound("Testimoni tidak ditemukan", "TESTIMONI_NOT_FOUND")
	}
	item, err := scanTestimoni(s.Pool.QueryRow(r.Context(), `SELECT testimoni_id, testimoni_nama, testimoni_peran,
		testimoni_isi, testimoni_rating, testimoni_foto_url, testimoni_urutan, testimoni_aktif,
		testimoni_landing, testimoni_landing_urutan, created_at, updated_at FROM testimoni WHERE testimoni_id = $1`, id))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "TESTIMONI_UPDATE", auth.RequestIP(r), map[string]any{"testimoni_id": id})
	httpx.Success(w, item, "Testimoni berhasil diperbarui")
	return nil
}

func (s *Server) testimoniRemove(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	command, err := s.Pool.Exec(r.Context(), `DELETE FROM testimoni WHERE testimoni_id = $1`, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return httpx.NotFound("Testimoni tidak ditemukan", "TESTIMONI_NOT_FOUND")
	}
	s.Activity.Record(r.Context(), principal.UserID, "TESTIMONI_DELETE", auth.RequestIP(r), map[string]any{"testimoni_id": id})
	httpx.Success(w, nil, "Testimoni berhasil dihapus")
	return nil
}

func updateSet(values map[string]any, order []string) ([]string, []any, int) {
	set := []string{}
	args := []any{}
	index := 1
	for _, column := range order {
		if value, exists := values[column]; exists {
			set = append(set, fmt.Sprintf("%s = $%d", column, index))
			args = append(args, value)
			index++
		}
	}
	return set, args, index
}

func scanArtikel(row pgx.Row) (Artikel, error) {
	var item Artikel
	err := row.Scan(&item.ArtikelID, &item.ArtikelSlug, &item.ArtikelJudul, &item.ArtikelExcerpt,
		&item.ArtikelIsi, &item.ArtikelKategori, &item.ArtikelPenulis, &item.ArtikelReadMinutes,
		&item.ArtikelFotoURL, &item.ArtikelFotoAlt, &item.ArtikelPublishedAt, &item.ArtikelAktif,
		&item.ArtikelLanding, &item.ArtikelLandingUrutan, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

const artikelColumns = `artikel_id, artikel_slug, artikel_judul, artikel_excerpt, artikel_isi,
	artikel_kategori, artikel_penulis, artikel_read_minutes, artikel_foto_url, artikel_foto_alt,
	artikel_published_at, artikel_aktif, artikel_landing, artikel_landing_urutan, created_at, updated_at`

func (s *Server) articleList(w http.ResponseWriter, r *http.Request) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"1=1"}
	args := []any{}
	index := 1
	if s.canPermission(r, "admin.articles.view") {
		if active := httpx.QueryBoolPtr(r, "artikel_aktif"); active != nil {
			conditions = append(conditions, fmt.Sprintf("artikel_aktif = $%d", index))
			args = append(args, *active)
			index++
		}
	} else {
		conditions = append(conditions, "artikel_aktif = true")
	}
	if kategori := strings.TrimSpace(r.URL.Query().Get("artikel_kategori")); kategori != "" {
		conditions = append(conditions, fmt.Sprintf("artikel_kategori = $%d", index))
		args = append(args, kategori)
		index++
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("(artikel_judul ILIKE $%d OR artikel_excerpt ILIKE $%d OR artikel_isi ILIKE $%d)", index, index, index))
		args = append(args, "%"+search+"%")
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM artikel WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`SELECT %s FROM artikel WHERE %s ORDER BY artikel_published_at DESC, artikel_id DESC LIMIT $%d OFFSET $%d`,
		artikelColumns, where, index, index+1)
	rows, err := s.Pool.Query(r.Context(), query, append(args, limit, offset)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Artikel{}
	for rows.Next() {
		item, err := scanArtikel(rows)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return rows.Err()
}

func (s *Server) articleCategories(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.Pool.Query(r.Context(), `SELECT artikel_kategori, count(*) FROM artikel WHERE artikel_aktif = true GROUP BY artikel_kategori ORDER BY artikel_kategori ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var kategori string
		var jumlah int
		if err := rows.Scan(&kategori, &jumlah); err != nil {
			return err
		}
		items = append(items, map[string]any{"artikel_kategori": kategori, "jumlah": jumlah})
	}
	httpx.Success(w, items, "")
	return rows.Err()
}

func (s *Server) articleDetail(w http.ResponseWriter, r *http.Request) error {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	item, err := scanArtikel(s.Pool.QueryRow(r.Context(), `SELECT `+artikelColumns+` FROM artikel WHERE artikel_slug = $1`, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Artikel tidak ditemukan", "ARTICLE_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if !item.ArtikelAktif && !s.canPermission(r, "admin.articles.view") {
		return httpx.NotFound("Artikel tidak ditemukan", "ARTICLE_NOT_FOUND")
	}
	httpx.Success(w, item, "")
	return nil
}

func (s *Server) articleAdminDetail(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	item, err := scanArtikel(s.Pool.QueryRow(r.Context(), `SELECT `+artikelColumns+` FROM artikel WHERE artikel_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Artikel tidak ditemukan", "ARTICLE_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	httpx.Success(w, item, "")
	return nil
}

func articleValidation(body map[string]any, partial bool) (map[string]any, map[string]string) {
	fields := map[string]string{}
	values := map[string]any{}
	if !partial || bodyHasKey(body, "artikel_slug") {
		if raw := strings.TrimSpace(bodyString(body, "artikel_slug")); raw != "" {
			slug := codegen.Slugify(raw)
			if len(slug) < 3 || len(slug) > 160 {
				fields["artikel_slug"] = "Slug tidak valid"
			} else {
				values["artikel_slug"] = slug
			}
		}
	}
	if !partial || bodyHasKey(body, "artikel_judul") {
		value := strings.TrimSpace(bodyString(body, "artikel_judul"))
		if len([]rune(value)) < 5 || len([]rune(value)) > 200 {
			fields["artikel_judul"] = "Judul harus 5-200 karakter"
		} else {
			values["artikel_judul"] = value
		}
	}
	if !partial || bodyHasKey(body, "artikel_excerpt") {
		value := strings.TrimSpace(bodyString(body, "artikel_excerpt"))
		if len([]rune(value)) < 10 || len([]rune(value)) > 500 {
			fields["artikel_excerpt"] = "Ringkasan harus 10-500 karakter"
		} else {
			values["artikel_excerpt"] = value
		}
	}
	if !partial || bodyHasKey(body, "artikel_isi") {
		value := strings.TrimSpace(bodyString(body, "artikel_isi"))
		if len([]rune(value)) < 20 || len([]rune(value)) > 50000 {
			fields["artikel_isi"] = "Isi artikel harus 20-50000 karakter"
		} else {
			values["artikel_isi"] = value
		}
	}
	if !partial || bodyHasKey(body, "artikel_kategori") {
		value := strings.TrimSpace(bodyString(body, "artikel_kategori"))
		if len([]rune(value)) < 2 || len([]rune(value)) > 100 {
			fields["artikel_kategori"] = "Kategori harus 2-100 karakter"
		} else {
			values["artikel_kategori"] = value
		}
	}
	if !partial || bodyHasKey(body, "artikel_penulis") {
		value := strings.TrimSpace(bodyString(body, "artikel_penulis"))
		if value == "" {
			value = "Tim EduMaster"
		}
		if len([]rune(value)) < 2 || len([]rune(value)) > 150 {
			fields["artikel_penulis"] = "Penulis harus 2-150 karakter"
		} else {
			values["artikel_penulis"] = value
		}
	}
	if !partial || bodyHasKey(body, "artikel_read_minutes") {
		value := bodyInt(body, "artikel_read_minutes", 4)
		if value < 1 || value > 120 {
			fields["artikel_read_minutes"] = "Waktu baca harus 1-120 menit"
		} else {
			values["artikel_read_minutes"] = value
		}
	}
	for _, key := range []string{"artikel_foto_url", "artikel_foto_alt"} {
		if !partial || bodyHasKey(body, key) {
			value := bodyStringPtr(body, key)
			if value != nil && len([]rune(*value)) > map[string]int{"artikel_foto_url": 1200, "artikel_foto_alt": 300}[key] {
				fields[key] = "Nilai terlalu panjang"
			} else {
				values[key] = value
			}
		}
	}
	if !partial || bodyHasKey(body, "artikel_published_at") {
		if value, exists := body["artikel_published_at"]; exists && value != nil {
			parsed, err := parseISODate(bodyString(body, "artikel_published_at"))
			if err != nil {
				fields["artikel_published_at"] = "Tanggal publikasi tidak valid"
			} else {
				values["artikel_published_at"] = parsed
			}
		} else if !partial {
			values["artikel_published_at"] = time.Now()
		}
	}
	for _, key := range []string{"artikel_aktif", "artikel_landing"} {
		if !partial || bodyHasKey(body, key) {
			values[key] = bodyBool(body, key, key == "artikel_aktif")
		}
	}
	if !partial || bodyHasKey(body, "artikel_landing_urutan") {
		value := bodyInt(body, "artikel_landing_urutan", 0)
		if value < 0 || value > 9999 {
			fields["artikel_landing_urutan"] = "Urutan harus 0-9999"
		} else {
			values["artikel_landing_urutan"] = value
		}
	}
	return values, fields
}

func parseISODate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid date")
}

func (s *Server) articleCreate(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	values, fields := articleValidation(body, false)
	if len(fields) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(fields))
	}
	baseSlug, _ := values["artikel_slug"].(string)
	if baseSlug == "" {
		baseSlug = codegen.Slugify(fmt.Sprint(values["artikel_judul"]))
	}
	if baseSlug == "" {
		return httpx.BadRequest("Slug artikel tidak valid", "INVALID_ARTICLE_SLUG")
	}
	slug := baseSlug
	for attempt := 2; ; attempt++ {
		var exists bool
		if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM artikel WHERE artikel_slug = $1)`, slug).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			break
		}
		slug = fmt.Sprintf("%s-%d", baseSlug, attempt)
	}
	values["artikel_slug"] = slug
	var item Artikel
	err = s.Pool.QueryRow(r.Context(), `
		INSERT INTO artikel (artikel_slug, artikel_judul, artikel_excerpt, artikel_isi, artikel_kategori,
			artikel_penulis, artikel_read_minutes, artikel_foto_url, artikel_foto_alt, artikel_published_at,
			artikel_aktif, artikel_landing, artikel_landing_urutan, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now(),now())
		RETURNING `+artikelColumns,
		slug, values["artikel_judul"], values["artikel_excerpt"], values["artikel_isi"], values["artikel_kategori"],
		values["artikel_penulis"], values["artikel_read_minutes"], values["artikel_foto_url"], values["artikel_foto_alt"],
		values["artikel_published_at"], values["artikel_aktif"], values["artikel_landing"], values["artikel_landing_urutan"]).
		Scan(&item.ArtikelID, &item.ArtikelSlug, &item.ArtikelJudul, &item.ArtikelExcerpt, &item.ArtikelIsi,
			&item.ArtikelKategori, &item.ArtikelPenulis, &item.ArtikelReadMinutes, &item.ArtikelFotoURL,
			&item.ArtikelFotoAlt, &item.ArtikelPublishedAt, &item.ArtikelAktif, &item.ArtikelLanding,
			&item.ArtikelLandingUrutan, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ARTICLE_CREATE", auth.RequestIP(r), map[string]any{
		"artikel_id": item.ArtikelID, "artikel_slug": item.ArtikelSlug,
	})
	httpx.Created(w, item, "Artikel berhasil dibuat")
	return nil
}

func (s *Server) articleUpdate(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	var existing Artikel
	existing, err = scanArtikel(s.Pool.QueryRow(r.Context(), `SELECT `+artikelColumns+` FROM artikel WHERE artikel_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Artikel tidak ditemukan", "ARTICLE_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	values, fields := articleValidation(body, true)
	if len(fields) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(fields))
	}
	nextSlug, hasSlug := values["artikel_slug"].(string)
	if !hasSlug && bodyHasKey(body, "artikel_judul") {
		if judul, ok := values["artikel_judul"].(string); ok && judul != existing.ArtikelJudul {
			nextSlug = codegen.Slugify(judul)
			hasSlug = nextSlug != ""
		}
	}
	if hasSlug && nextSlug != existing.ArtikelSlug {
		var duplicate bool
		if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM artikel WHERE artikel_slug = $1 AND artikel_id <> $2)`, nextSlug, id).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate {
			return httpx.Conflict("Slug artikel sudah digunakan", "ARTICLE_SLUG_EXISTS")
		}
		values["artikel_slug"] = nextSlug
	}
	columns := []string{"artikel_slug", "artikel_judul", "artikel_excerpt", "artikel_isi", "artikel_kategori",
		"artikel_penulis", "artikel_read_minutes", "artikel_foto_url", "artikel_foto_alt", "artikel_published_at",
		"artikel_aktif", "artikel_landing", "artikel_landing_urutan"}
	set, args, index := updateSet(values, columns)
	if len(set) > 0 {
		set = append(set, "updated_at = now()")
		args = append(args, id)
		if _, err := s.Pool.Exec(r.Context(), fmt.Sprintf(`UPDATE artikel SET %s WHERE artikel_id = $%d`, strings.Join(set, ", "), index), args...); err != nil {
			return err
		}
	}
	item, err := scanArtikel(s.Pool.QueryRow(r.Context(), `SELECT `+artikelColumns+` FROM artikel WHERE artikel_id = $1`, id))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ARTICLE_UPDATE", auth.RequestIP(r), map[string]any{
		"artikel_id": id, "artikel_slug": item.ArtikelSlug,
	})
	httpx.Success(w, item, "Artikel berhasil diperbarui")
	return nil
}

func (s *Server) articleRemove(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	var slug string
	err = s.Pool.QueryRow(r.Context(), `DELETE FROM artikel WHERE artikel_id = $1 RETURNING artikel_slug`, id).Scan(&slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Artikel tidak ditemukan", "ARTICLE_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ARTICLE_DELETE", auth.RequestIP(r), map[string]any{
		"artikel_id": id, "artikel_slug": slug,
	})
	httpx.Success(w, nil, "Artikel berhasil dihapus")
	return nil
}
