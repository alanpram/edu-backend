package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
)

type landingSection struct {
	SectionID       int             `json:"section_id"`
	SectionKey      string          `json:"section_key"`
	SectionEyebrow  *string         `json:"section_eyebrow"`
	SectionTitle    *string         `json:"section_title"`
	SectionSubtitle *string         `json:"section_subtitle"`
	SectionBody     *string         `json:"section_body"`
	SectionImageURL *string         `json:"section_image_url"`
	SectionCTALabel *string         `json:"section_cta_label"`
	SectionCTAURL   *string         `json:"section_cta_url"`
	SectionVisible  bool            `json:"section_visible"`
	SectionOrder    int             `json:"section_order"`
	SectionSettings json.RawMessage `json:"section_settings"`
}

type heroSlide struct {
	SlideID       int     `json:"slide_id"`
	SlideTitle    string  `json:"slide_title"`
	SlideSubtitle *string `json:"slide_subtitle"`
	SlideImageURL string  `json:"slide_image_url"`
	SlideImageAlt *string `json:"slide_image_alt"`
	SlideCTALabel *string `json:"slide_cta_label"`
	SlideCTAURL   *string `json:"slide_cta_url"`
	SlideOverlay  int     `json:"slide_overlay"`
	SlideOrder    int     `json:"slide_order"`
	SlideActive   bool    `json:"slide_active"`
}

type landingStat struct {
	StatID     int    `json:"stat_id"`
	StatValue  string `json:"stat_value"`
	StatLabel  string `json:"stat_label"`
	StatColor  string `json:"stat_color"`
	StatOrder  int    `json:"stat_order"`
	StatActive bool   `json:"stat_active"`
}

type landingFeature struct {
	FeatureID     int    `json:"feature_id"`
	FeatureIcon   string `json:"feature_icon"`
	FeatureTitle  string `json:"feature_title"`
	FeatureText   string `json:"feature_text"`
	FeatureOrder  int    `json:"feature_order"`
	FeatureActive bool   `json:"feature_active"`
}

type landingContent struct {
	Sections []landingSection
	Slides   []heroSlide
	Stats    []landingStat
	Features []landingFeature
	Site     map[string]any
}

func (s *Server) registerLanding(api chi.Router) {
	api.Route("/landing", func(r chi.Router) {
		r.Get("/", httpx.Wrap(s.landingPublic))
		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Authenticate)
			r.With(middleware.RequirePermission(s.Permissions, "admin.landing.view")).
				Get("/admin", httpx.Wrap(s.landingAdmin))
			r.With(middleware.RequirePermission(s.Permissions, "admin.landing.manage")).
				Put("/admin", httpx.Wrap(s.landingUpdate))
		})
	})
}

func (s *Server) loadLandingSections(ctx context.Context, publicOnly bool) ([]landingSection, error) {
	condition := ""
	if publicOnly {
		condition = " WHERE section_visible = true"
	}
	rows, err := s.Pool.Query(ctx, `SELECT section_id, section_key, section_eyebrow, section_title,
		section_subtitle, section_body, section_image_url, section_cta_label, section_cta_url,
		section_visible, section_order, section_settings FROM landing_section`+condition+`
		ORDER BY section_order ASC, section_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []landingSection{}
	for rows.Next() {
		var item landingSection
		var settings []byte
		if err := rows.Scan(&item.SectionID, &item.SectionKey, &item.SectionEyebrow, &item.SectionTitle,
			&item.SectionSubtitle, &item.SectionBody, &item.SectionImageURL, &item.SectionCTALabel,
			&item.SectionCTAURL, &item.SectionVisible, &item.SectionOrder, &settings); err != nil {
			return nil, err
		}
		if len(settings) > 0 {
			item.SectionSettings = json.RawMessage(settings)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadHeroSlides(ctx context.Context, publicOnly bool) ([]heroSlide, error) {
	condition := ""
	if publicOnly {
		condition = " WHERE slide_active = true"
	}
	rows, err := s.Pool.Query(ctx, `SELECT slide_id, slide_title, slide_subtitle, slide_image_url,
		slide_image_alt, slide_cta_label, slide_cta_url, slide_overlay, slide_order, slide_active
		FROM hero_slide`+condition+` ORDER BY slide_order ASC, slide_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []heroSlide{}
	for rows.Next() {
		var item heroSlide
		if err := rows.Scan(&item.SlideID, &item.SlideTitle, &item.SlideSubtitle, &item.SlideImageURL,
			&item.SlideImageAlt, &item.SlideCTALabel, &item.SlideCTAURL, &item.SlideOverlay,
			&item.SlideOrder, &item.SlideActive); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadLandingStats(ctx context.Context, publicOnly bool) ([]landingStat, error) {
	condition := ""
	if publicOnly {
		condition = " WHERE stat_active = true"
	}
	rows, err := s.Pool.Query(ctx, `SELECT stat_id, stat_value, stat_label, stat_color, stat_order, stat_active
		FROM landing_stat`+condition+` ORDER BY stat_order ASC, stat_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []landingStat{}
	for rows.Next() {
		var item landingStat
		if err := rows.Scan(&item.StatID, &item.StatValue, &item.StatLabel, &item.StatColor,
			&item.StatOrder, &item.StatActive); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadLandingFeatures(ctx context.Context, publicOnly bool) ([]landingFeature, error) {
	condition := ""
	if publicOnly {
		condition = " WHERE feature_active = true"
	}
	rows, err := s.Pool.Query(ctx, `SELECT feature_id, feature_icon, feature_title, feature_text, feature_order, feature_active
		FROM landing_feature`+condition+` ORDER BY feature_order ASC, feature_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []landingFeature{}
	for rows.Next() {
		var item landingFeature
		if err := rows.Scan(&item.FeatureID, &item.FeatureIcon, &item.FeatureTitle, &item.FeatureText,
			&item.FeatureOrder, &item.FeatureActive); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadSiteSettings(ctx context.Context) (map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `SELECT setting_key, setting_value FROM site_setting ORDER BY setting_key ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	site := map[string]any{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var value any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &value)
		}
		site[key] = value
	}
	return site, rows.Err()
}

func (s *Server) loadLandingContent(ctx context.Context, publicOnly bool) (landingContent, error) {
	sections, err := s.loadLandingSections(ctx, publicOnly)
	if err != nil {
		return landingContent{}, err
	}
	slides, err := s.loadHeroSlides(ctx, publicOnly)
	if err != nil {
		return landingContent{}, err
	}
	stats, err := s.loadLandingStats(ctx, publicOnly)
	if err != nil {
		return landingContent{}, err
	}
	features, err := s.loadLandingFeatures(ctx, publicOnly)
	if err != nil {
		return landingContent{}, err
	}
	site, err := s.loadSiteSettings(ctx)
	if err != nil {
		return landingContent{}, err
	}
	return landingContent{Sections: sections, Slides: slides, Stats: stats, Features: features, Site: site}, nil
}

func (s *Server) landingPublic(w http.ResponseWriter, r *http.Request) error {
	content, err := s.loadLandingContent(r.Context(), true)
	if err != nil {
		return err
	}
	sections := map[string]landingSection{}
	for _, section := range content.Sections {
		sections[section.SectionKey] = section
	}
	heroSection, hasHero := sections["hero"]
	heroMode := "text"
	if hasHero && len(heroSection.SectionSettings) > 0 && len(content.Slides) > 0 {
		var settings map[string]any
		_ = json.Unmarshal(heroSection.SectionSettings, &settings)
		if mode, _ := settings["mode"].(string); mode == "slideshow" {
			heroMode = "slideshow"
		}
	}

	packages := []Paket{}
	rows, err := s.Pool.Query(r.Context(), `SELECT `+packageSelect+` FROM paket_ujian p
		WHERE p.is_active = true AND p.paket_landing = true
		ORDER BY p.paket_landing_urutan ASC, p.paket_id DESC LIMIT 6`)
	if err != nil {
		return err
	}
	for rows.Next() {
		item, err := scanPaket(rows)
		if err != nil {
			rows.Close()
			return err
		}
		packages = append(packages, item)
	}
	rows.Close()

	articles := []Artikel{}
	articleRows, err := s.Pool.Query(r.Context(), `SELECT `+artikelColumns+` FROM artikel
		WHERE artikel_aktif = true AND artikel_landing = true
		ORDER BY artikel_landing_urutan ASC, artikel_published_at DESC LIMIT 3`)
	if err != nil {
		return err
	}
	for articleRows.Next() {
		item, err := scanArtikel(articleRows)
		if err != nil {
			articleRows.Close()
			return err
		}
		articles = append(articles, item)
	}
	articleRows.Close()

	testimonials := []Testimoni{}
	testimoniRows, err := s.Pool.Query(r.Context(), `SELECT testimoni_id, testimoni_nama, testimoni_peran,
		testimoni_isi, testimoni_rating, testimoni_foto_url, testimoni_urutan, testimoni_aktif,
		testimoni_landing, testimoni_landing_urutan, created_at, updated_at FROM testimoni
		WHERE testimoni_aktif = true AND testimoni_landing = true
		ORDER BY testimoni_landing_urutan ASC, testimoni_urutan ASC, testimoni_id DESC LIMIT 3`)
	if err != nil {
		return err
	}
	for testimoniRows.Next() {
		item, err := scanTestimoni(testimoniRows)
		if err != nil {
			testimoniRows.Close()
			return err
		}
		testimonials = append(testimonials, item)
	}
	testimoniRows.Close()

	faqs := []Faq{}
	faqRows, err := s.Pool.Query(r.Context(), `SELECT faq_id, faq_pertanyaan, faq_jawaban, faq_kategori,
		faq_urutan, faq_aktif, faq_landing, faq_landing_urutan, created_at, updated_at FROM faq
		WHERE faq_aktif = true AND faq_landing = true
		ORDER BY faq_landing_urutan ASC, faq_urutan ASC LIMIT 4`)
	if err != nil {
		return err
	}
	for faqRows.Next() {
		item, err := scanFaq(faqRows)
		if err != nil {
			faqRows.Close()
			return err
		}
		faqs = append(faqs, item)
	}
	faqRows.Close()

	hero := map[string]any{"mode": heroMode, "section": nil, "slides": content.Slides}
	if hasHero {
		hero["section"] = heroSection
	}
	httpx.Success(w, map[string]any{
		"hero":         hero,
		"sections":     sections,
		"stats":        content.Stats,
		"features":     content.Features,
		"packages":     packages,
		"articles":     articles,
		"testimonials": testimonials,
		"faqs":         faqs,
		"site":         content.Site,
	}, "")
	return nil
}

func (s *Server) landingAdmin(w http.ResponseWriter, r *http.Request) error {
	content, err := s.loadLandingContent(r.Context(), false)
	if err != nil {
		return err
	}
	httpx.Success(w, map[string]any{
		"sections": content.Sections,
		"slides":   content.Slides,
		"stats":    content.Stats,
		"features": content.Features,
		"site":     content.Site,
	}, "")
	return nil
}

func landingRaw(value any) any {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func (s *Server) landingUpdate(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	sections := bodySlice(body, "sections")
	slides := bodySlice(body, "slides")
	stats := bodySlice(body, "stats")
	features := bodySlice(body, "features")
	if len(sections) > 30 || len(slides) > 12 || len(stats) > 12 || len(features) > 16 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR",
			[]map[string]string{{"field": "sections", "message": "Jumlah konten melebihi batas"}})
	}
	site, _ := body["site"].(map[string]any)
	if site == nil {
		site = map[string]any{}
	}
	err = pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
		for _, table := range []string{"landing_section", "hero_slide", "landing_stat", "landing_feature", "site_setting"} {
			if _, err := tx.Exec(r.Context(), `DELETE FROM `+table); err != nil {
				return err
			}
		}
		for index, raw := range sections {
			item, ok := raw.(map[string]any)
			if !ok {
				return httpx.BadRequest("Data section tidak valid", "INVALID_LANDING_SECTION")
			}
			key := strings.TrimSpace(bodyString(item, "section_key"))
			if key == "" || len([]rune(key)) > 60 {
				return httpx.BadRequest("section_key wajib diisi", "INVALID_LANDING_SECTION")
			}
			order := bodyInt(item, "section_order", index)
			visible := bodyBool(item, "section_visible", true)
			if _, err := tx.Exec(r.Context(), `INSERT INTO landing_section
				(section_key, section_eyebrow, section_title, section_subtitle, section_body, section_image_url,
				 section_cta_label, section_cta_url, section_visible, section_order, section_settings, created_at, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,now(),now())`,
				key, bodyStringPtr(item, "section_eyebrow"), bodyStringPtr(item, "section_title"),
				bodyStringPtr(item, "section_subtitle"), bodyStringPtr(item, "section_body"),
				bodyStringPtr(item, "section_image_url"), bodyStringPtr(item, "section_cta_label"),
				bodyStringPtr(item, "section_cta_url"), visible, order,
				landingRaw(item["section_settings"])); err != nil {
				return err
			}
		}
		for index, raw := range slides {
			item, ok := raw.(map[string]any)
			if !ok {
				return httpx.BadRequest("Data slide tidak valid", "INVALID_LANDING_SLIDE")
			}
			title := strings.TrimSpace(bodyString(item, "slide_title"))
			image := strings.TrimSpace(bodyString(item, "slide_image_url"))
			if title == "" || image == "" {
				return httpx.BadRequest("Judul dan gambar slide wajib diisi", "INVALID_LANDING_SLIDE")
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO hero_slide
				(slide_title, slide_subtitle, slide_image_url, slide_image_alt, slide_cta_label, slide_cta_url,
				 slide_overlay, slide_order, slide_active, created_at, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now(),now())`,
				title, bodyStringPtr(item, "slide_subtitle"), image, bodyStringPtr(item, "slide_image_alt"),
				bodyStringPtr(item, "slide_cta_label"), bodyStringPtr(item, "slide_cta_url"),
				bodyInt(item, "slide_overlay", 55), bodyInt(item, "slide_order", index),
				bodyBool(item, "slide_active", true)); err != nil {
				return err
			}
		}
		for index, raw := range stats {
			item, ok := raw.(map[string]any)
			if !ok {
				return httpx.BadRequest("Data statistik tidak valid", "INVALID_LANDING_STAT")
			}
			value := strings.TrimSpace(bodyString(item, "stat_value"))
			label := strings.TrimSpace(bodyString(item, "stat_label"))
			if value == "" || label == "" {
				return httpx.BadRequest("Nilai dan label statistik wajib diisi", "INVALID_LANDING_STAT")
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO landing_stat
				(stat_value, stat_label, stat_color, stat_order, stat_active, created_at, updated_at)
				VALUES ($1,$2,$3,$4,$5,now(),now())`,
				value, label, bodyStringDefault(item, "stat_color", "text-blue-400"),
				bodyInt(item, "stat_order", index), bodyBool(item, "stat_active", true)); err != nil {
				return err
			}
		}
		for index, raw := range features {
			item, ok := raw.(map[string]any)
			if !ok {
				return httpx.BadRequest("Data fitur tidak valid", "INVALID_LANDING_FEATURE")
			}
			title := strings.TrimSpace(bodyString(item, "feature_title"))
			text := strings.TrimSpace(bodyString(item, "feature_text"))
			if title == "" || text == "" {
				return httpx.BadRequest("Judul dan teks fitur wajib diisi", "INVALID_LANDING_FEATURE")
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO landing_feature
				(feature_icon, feature_title, feature_text, feature_order, feature_active, created_at, updated_at)
				VALUES ($1,$2,$3,$4,$5,now(),now())`,
				bodyStringDefault(item, "feature_icon", "shield"), title, text,
				bodyInt(item, "feature_order", index), bodyBool(item, "feature_active", true)); err != nil {
				return err
			}
		}
		for key, value := range site {
			if _, err := tx.Exec(r.Context(), `INSERT INTO site_setting (setting_key, setting_value, updated_at)
				VALUES ($1,$2::jsonb,now()) ON CONFLICT (setting_key) DO UPDATE SET setting_value = EXCLUDED.setting_value, updated_at = now()`,
				key, landingRaw(value)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	content, err := s.loadLandingContent(r.Context(), false)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "LANDING_UPDATE", auth.RequestIP(r), map[string]any{
		"sections": len(content.Sections), "slides": len(content.Slides), "stats": len(content.Stats), "features": len(content.Features),
	})
	httpx.Success(w, map[string]any{
		"sections": content.Sections, "slides": content.Slides, "stats": content.Stats,
		"features": content.Features, "site": content.Site,
	}, "Landing page berhasil diperbarui")
	return nil
}
