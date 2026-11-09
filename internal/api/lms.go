package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
)

type lmsMaterialData struct {
	MateriID      int
	PaketID       int
	Judul         string
	Deskripsi     *string
	Tipe          string
	URL           *string
	Konten        *string
	Urutan        int
	Wajib         bool
	Active        bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
	ProgressCount int
}

type lmsAccessData struct {
	AccessID    int
	UserID      int
	PaketID     int
	SisaAttempt int
	ExpiredAt   time.Time
	CreatedAt   time.Time
}

type lmsAttemptData struct {
	SessionID       int        `json:"session_id"`
	AttemptNumber   int        `json:"attempt_number"`
	Status          string     `json:"status"`
	FinalScore      *float64   `json:"final_score"`
	IsPassed        *bool      `json:"is_passed"`
	CertificateCode *string    `json:"certificate_code"`
	StartedAt       time.Time  `json:"started_at"`
	SubmittedAt     *time.Time `json:"submitted_at"`
}

func (s *Server) registerLMS(api chi.Router) {
	api.Route("/lms", func(r chi.Router) {
		r.Use(s.Auth.Authenticate)
		r.With(middleware.RequirePermission(s.Permissions, "lms.learn")).
			Get("/courses", httpx.Wrap(s.lmsMyCourses))
		r.With(middleware.RequirePermission(s.Permissions, "lms.learn")).
			Get("/courses/{paketId}", httpx.Wrap(s.lmsCourseDetail))
		r.With(middleware.RequirePermission(s.Permissions, "lms.learn")).
			Put("/materials/{id}/progress", httpx.Wrap(s.lmsSetProgress))

		r.With(middleware.RequirePermission(s.Permissions, "admin.lms.manage")).
			Post("/courses", httpx.Wrap(s.lmsCreateCourse))
		r.With(middleware.RequirePermission(s.Permissions, "admin.lms.view")).
			Get("/materials", httpx.Wrap(s.lmsAdminMaterials))
		r.With(middleware.RequirePermission(s.Permissions, "admin.lms.manage")).
			Post("/materials", httpx.Wrap(s.lmsCreateMaterial))
		r.With(middleware.RequirePermission(s.Permissions, "admin.lms.manage")).
			Put("/materials/{id}", httpx.Wrap(s.lmsUpdateMaterial))
		r.With(middleware.RequirePermission(s.Permissions, "admin.lms.manage")).
			Delete("/materials/{id}", httpx.Wrap(s.lmsRemoveMaterial))
	})
}

func (s *Server) lmsPackage(ctx context.Context, paketID int) (Paket, error) {
	paket, err := scanPaket(s.Pool.QueryRow(ctx, `SELECT `+packageSelect+` FROM paket_ujian p WHERE p.paket_id = $1`, paketID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Paket{}, httpx.NotFound("Paket tidak ditemukan", "PACKAGE_NOT_FOUND")
	}
	return paket, err
}

func (s *Server) lmsAccess(ctx context.Context, userID, paketID int) (lmsAccessData, error) {
	var item lmsAccessData
	err := s.Pool.QueryRow(ctx, `
		SELECT access_id, user_id, paket_id, sisa_attempt, expired_at, created_at
		FROM user_package_access WHERE user_id = $1 AND paket_id = $2`, userID, paketID).
		Scan(&item.AccessID, &item.UserID, &item.PaketID, &item.SisaAttempt, &item.ExpiredAt, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, httpx.Forbidden("Anda belum memiliki akses ke kelas ini. Tambahkan paket terlebih dahulu.", "PACKAGE_ACCESS_REQUIRED")
	}
	return item, err
}

func (s *Server) lmsMaterials(ctx context.Context, userID, paketID int, includeInactive bool) ([]lmsMaterialData, error) {
	condition := "m.is_active = true"
	if includeInactive {
		condition = "1=1"
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT m.materi_id, m.paket_id, m.materi_judul, m.materi_deskripsi, m.materi_tipe,
		       m.materi_url, m.materi_konten, m.materi_urutan, m.is_wajib, m.is_active,
		       m.created_at, m.updated_at,
		       (SELECT completed_at FROM lms_progress ip WHERE ip.materi_id = m.materi_id AND ip.user_id = $2 LIMIT 1) AS completed_at,
		       (SELECT count(*) FROM lms_progress pc WHERE pc.materi_id = m.materi_id) AS progress_count
		FROM lms_materi m WHERE m.paket_id = $1 AND `+condition+`
		ORDER BY m.materi_urutan ASC, m.materi_id ASC`, paketID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []lmsMaterialData{}
	for rows.Next() {
		var item lmsMaterialData
		if err := rows.Scan(&item.MateriID, &item.PaketID, &item.Judul, &item.Deskripsi, &item.Tipe,
			&item.URL, &item.Konten, &item.Urutan, &item.Wajib, &item.Active,
			&item.CreatedAt, &item.UpdatedAt, &item.CompletedAt, &item.ProgressCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) lmsAttempts(ctx context.Context, userID, paketID int) ([]lmsAttemptData, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT session_id, attempt_number, status, final_score, is_passed, certificate_code, started_at, submitted_at
		FROM exam_session WHERE user_id = $1 AND paket_id = $2
		ORDER BY started_at DESC LIMIT 5`, userID, paketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []lmsAttemptData{}
	for rows.Next() {
		var item lmsAttemptData
		if err := rows.Scan(&item.SessionID, &item.AttemptNumber, &item.Status, &item.FinalScore,
			&item.IsPassed, &item.CertificateCode, &item.StartedAt, &item.SubmittedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func lmsProgressSummary(materials []lmsMaterialData) map[string]any {
	required := 0
	completedAll := 0
	completedRequired := 0
	for _, material := range materials {
		completed := material.CompletedAt != nil
		if completed {
			completedAll++
		}
		if material.Wajib {
			required++
			if completed {
				completedRequired++
			}
		}
	}
	percent := 0
	if required == 0 {
		if completedAll > 0 {
			percent = 100
		}
	} else {
		percent = int(math.Round(float64(completedRequired) / float64(required) * 100))
	}
	return map[string]any{
		"total_materials":     len(materials),
		"required_materials":  required,
		"completed_materials": completedAll,
		"completed_required":  completedRequired,
		"percent":             percent,
		"post_test_unlocked":  required > 0 && completedRequired == required,
	}
}

func lmsCertificateCode(paketID int, attempt lmsAttemptData) string {
	if attempt.CertificateCode != nil && *attempt.CertificateCode != "" {
		return *attempt.CertificateCode
	}
	return fmt.Sprintf("EDU-%d-%d-%d", paketID, attempt.SessionID, attempt.AttemptNumber)
}

func lmsAccessPayload(access lmsAccessData) map[string]any {
	return map[string]any{
		"access_id":      access.AccessID,
		"expired_at":     access.ExpiredAt,
		"sisa_attempt":   access.SisaAttempt,
		"access_expired": !access.ExpiredAt.After(time.Now()),
	}
}

func lmsPostTest(paket Paket, access lmsAccessData, materials []lmsMaterialData, attempts []lmsAttemptData) map[string]any {
	progress := lmsProgressSummary(materials)
	unlocked := progress["post_test_unlocked"].(bool)
	expired := !access.ExpiredAt.After(time.Now())
	hasAttempts := paket.MaxAttempts == 0 || access.SisaAttempt > 0

	var reason any
	switch {
	case expired:
		reason = "ACCESS_EXPIRED"
	case len(materials) == 0:
		reason = "MATERIALS_EMPTY"
	case !unlocked:
		reason = "MATERIALS_INCOMPLETE"
	case !hasAttempts:
		reason = "NO_ATTEMPTS_LEFT"
	default:
		reason = nil
	}

	var latestAttempt any
	if len(attempts) > 0 {
		latestAttempt = attempts[0]
	}
	var certificate any
	for _, attempt := range attempts {
		if attempt.Status == "completed" && attempt.IsPassed != nil && *attempt.IsPassed {
			issued := attempt.SubmittedAt
			if issued == nil {
				issued = &attempt.StartedAt
			}
			certificate = map[string]any{
				"code":           lmsCertificateCode(paket.PaketID, attempt),
				"session_id":     attempt.SessionID,
				"attempt_number": attempt.AttemptNumber,
				"final_score":    attempt.FinalScore,
				"issued_at":      issued,
			}
			break
		}
	}

	var attemptsLeft any
	if paket.MaxAttempts != 0 {
		attemptsLeft = access.SisaAttempt
	}
	var thresholds any
	if len(paket.PaketPassingKategori) > 0 {
		_ = json.Unmarshal(paket.PaketPassingKategori, &thresholds)
	}
	return map[string]any{
		"can_start":           reason == nil,
		"unlocked":            unlocked,
		"reason":              reason,
		"attempts_left":       attemptsLeft,
		"max_attempts":        paket.MaxAttempts,
		"passing_mode":        paket.PassingMode,
		"passing_grade":       paket.PassingGrade,
		"category_thresholds": thresholds,
		"total_questions":     paket.PaketJumlahSoal,
		"latest_attempt":      latestAttempt,
		"certificate":         certificate,
	}
}

func lmsCourseSummary(paket Paket, access lmsAccessData, materials []lmsMaterialData, attempts []lmsAttemptData) map[string]any {
	return map[string]any{
		"paket":           paket,
		"access":          lmsAccessPayload(access),
		"progress":        lmsProgressSummary(materials),
		"materials_count": len(materials),
		"post_test":       lmsPostTest(paket, access, materials, attempts),
	}
}

func (s *Server) lmsMyCourses(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	rows, err := s.Pool.Query(r.Context(), `
		SELECT a.access_id, a.user_id, a.paket_id, a.sisa_attempt, a.expired_at, a.created_at
		FROM user_package_access a JOIN paket_ujian p ON p.paket_id = a.paket_id
		WHERE a.user_id = $1 AND p.paket_tipe = 'elearning' AND p.is_active = true
		  AND (p.organization_id IS NULL OR EXISTS (
			SELECT 1 FROM organization o
			WHERE o.organization_id = p.organization_id AND o.organization_status = 'ACTIVE'
		  ))
		ORDER BY a.created_at DESC`, principal.UserID)
	if err != nil {
		return err
	}
	accesses := []lmsAccessData{}
	for rows.Next() {
		var item lmsAccessData
		if err := rows.Scan(&item.AccessID, &item.UserID, &item.PaketID, &item.SisaAttempt,
			&item.ExpiredAt, &item.CreatedAt); err != nil {
			rows.Close()
			return err
		}
		accesses = append(accesses, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	items := []map[string]any{}
	for _, access := range accesses {
		paket, err := s.lmsPackage(r.Context(), access.PaketID)
		if err != nil {
			if appErr, ok := err.(*httpx.Error); ok && appErr.Status == http.StatusNotFound {
				continue
			}
			return err
		}
		materials, err := s.lmsMaterials(r.Context(), principal.UserID, access.PaketID, false)
		if err != nil {
			return err
		}
		attempts, err := s.lmsAttempts(r.Context(), principal.UserID, access.PaketID)
		if err != nil {
			return err
		}
		items = append(items, lmsCourseSummary(paket, access, materials, attempts))
	}
	httpx.Success(w, items, "")
	return nil
}

func (s *Server) lmsCourseDetail(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	paketID, err := httpx.IDParam(r, "paketId")
	if err != nil {
		return err
	}
	access, err := s.lmsAccess(r.Context(), principal.UserID, paketID)
	if err != nil {
		return err
	}
	paket, err := s.lmsPackage(r.Context(), paketID)
	if err != nil {
		return err
	}
	if !paket.IsActive || paket.PaketTipe != "elearning" {
		return httpx.NotFound("Kelas e-learning tidak ditemukan", "LMS_COURSE_NOT_FOUND")
	}
	if err := s.assertOrganizationActive(r.Context(), paket.OrganizationID); err != nil {
		return err
	}
	if !access.ExpiredAt.After(time.Now()) {
		return httpx.Forbidden("Masa aktif kelas sudah berakhir. Perpanjang paket untuk melanjutkan belajar.", "ACCESS_EXPIRED")
	}
	materials, err := s.lmsMaterials(r.Context(), principal.UserID, paketID, false)
	if err != nil {
		return err
	}
	attempts, err := s.lmsAttempts(r.Context(), principal.UserID, paketID)
	if err != nil {
		return err
	}
	summary := lmsCourseSummary(paket, access, materials, attempts)

	previousRequiredIncomplete := false
	materialPayload := make([]map[string]any, 0, len(materials))
	for _, material := range materials {
		completed := material.CompletedAt != nil
		unlocked := !previousRequiredIncomplete
		if material.Wajib && !completed {
			previousRequiredIncomplete = true
		}
		url := material.URL
		konten := material.Konten
		if !unlocked {
			url = nil
			konten = nil
		}
		var lockReason any
		if !unlocked {
			lockReason = "MODULE_LOCKED"
		}
		materialPayload = append(materialPayload, map[string]any{
			"materi_id":        material.MateriID,
			"materi_judul":     material.Judul,
			"materi_deskripsi": material.Deskripsi,
			"materi_tipe":      material.Tipe,
			"materi_url":       url,
			"materi_konten":    konten,
			"materi_urutan":    material.Urutan,
			"is_wajib":         material.Wajib,
			"completed":        completed,
			"completed_at":     material.CompletedAt,
			"unlocked":         unlocked,
			"locked_reason":    lockReason,
		})
	}
	summary["materials"] = materialPayload
	httpx.Success(w, summary, "")
	return nil
}

func (s *Server) lmsSetProgress(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	materiID, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	completed := bodyBool(body, "completed", false)

	var paketID int
	var paketTipe string
	var isActive, paketActive bool
	var organizationID *int
	if err := s.Pool.QueryRow(r.Context(), `
		SELECT m.paket_id, p.paket_tipe, m.is_active, p.is_active, p.organization_id
		FROM lms_materi m JOIN paket_ujian p ON p.paket_id = m.paket_id
		WHERE m.materi_id = $1`, materiID).Scan(&paketID, &paketTipe, &isActive, &paketActive, &organizationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Materi tidak ditemukan", "LMS_MATERIAL_NOT_FOUND")
		}
		return err
	}
	if !isActive || !paketActive {
		return httpx.NotFound("Materi tidak ditemukan", "LMS_MATERIAL_NOT_FOUND")
	}
	if paketTipe != "elearning" {
		return httpx.BadRequest("Materi ini bukan bagian dari kelas e-learning.", "LMS_MATERIAL_INVALID")
	}
	if err := s.assertOrganizationActive(r.Context(), organizationID); err != nil {
		return err
	}

	access, err := s.lmsAccess(r.Context(), principal.UserID, paketID)
	if err != nil {
		return err
	}
	if !access.ExpiredAt.After(time.Now()) {
		return httpx.Forbidden("Masa aktif kelas sudah berakhir. Perpanjang paket untuk melanjutkan belajar.", "ACCESS_EXPIRED")
	}

	materials, err := s.lmsMaterials(r.Context(), principal.UserID, paketID, false)
	if err != nil {
		return err
	}
	if completed {
		var locking *lmsMaterialData
		for index := range materials {
			material := &materials[index]
			if material.MateriID == materiID {
				break
			}
			if material.Wajib && material.CompletedAt == nil {
				locking = material
				break
			}
		}
		if locking != nil {
			return httpx.NewError(http.StatusForbidden,
				fmt.Sprintf("Selesaikan Modul %d terlebih dahulu sebelum membuka modul ini.", locking.Urutan),
				"LMS_MODULE_LOCKED", map[string]any{
					"materi_id": locking.MateriID, "materi_judul": locking.Judul, "materi_urutan": locking.Urutan,
				})
		}
		if _, err := s.Pool.Exec(r.Context(), `
			INSERT INTO lms_progress (user_id, materi_id, completed_at, created_at)
			VALUES ($1,$2,now(),now())
			ON CONFLICT (user_id, materi_id) DO UPDATE SET completed_at = now()`, principal.UserID, materiID); err != nil {
			return err
		}
	} else {
		if _, err := s.Pool.Exec(r.Context(), `DELETE FROM lms_progress WHERE user_id = $1 AND materi_id = $2`,
			principal.UserID, materiID); err != nil {
			return err
		}
	}
	s.Activity.Record(r.Context(), principal.UserID,
		map[bool]string{true: "LMS_MATERIAL_COMPLETE", false: "LMS_MATERIAL_REOPEN"}[completed],
		auth.RequestIP(r), map[string]any{"paket_id": paketID, "materi_id": materiID})

	var required, completedRequired int
	if err := s.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM lms_materi WHERE paket_id = $1 AND is_active = true AND is_wajib = true`, paketID).Scan(&required); err != nil {
		return err
	}
	if err := s.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM lms_progress p JOIN lms_materi m ON m.materi_id = p.materi_id
		WHERE p.user_id = $1 AND m.paket_id = $2 AND m.is_active = true AND m.is_wajib = true`,
		principal.UserID, paketID).Scan(&completedRequired); err != nil {
		return err
	}
	percent := 0
	if required > 0 {
		percent = int(math.Round(float64(completedRequired) / float64(required) * 100))
	}
	message := "Materi dibuka kembali."
	if completed {
		message = "Materi ditandai selesai."
	}
	httpx.Success(w, map[string]any{
		"materi_id": materiID,
		"completed": completed,
		"progress": map[string]any{
			"required_materials": required,
			"completed_required": completedRequired,
			"percent":            percent,
			"post_test_unlocked": required > 0 && completedRequired == required,
		},
	}, message)
	return nil
}

func lmsNormalizeMaterialType(value string) string {
	switch value {
	case "teks", "pdf", "video", "tugas", "dokumen":
		return value
	default:
		return ""
	}
}

func validateLmsURL(value string) bool {
	if value == "" {
		return true
	}
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "/")
}

func lmsReadMaterialBody(body map[string]any, partial bool) (map[string]any, map[string]string) {
	errorsByField := map[string]string{}
	values := map[string]any{}
	if !partial || bodyHasKey(body, "materi_judul") {
		judul := strings.TrimSpace(bodyString(body, "materi_judul"))
		length := len([]rune(judul))
		if length < 3 || length > 200 {
			errorsByField["materi_judul"] = "Judul materi minimal 3 karakter dan maksimal 200 karakter"
		} else {
			values["materi_judul"] = judul
		}
	}
	if !partial || bodyHasKey(body, "materi_deskripsi") {
		deskripsi := bodyStringPtr(body, "materi_deskripsi")
		if deskripsi != nil && len([]rune(*deskripsi)) > 1000 {
			errorsByField["materi_deskripsi"] = "Deskripsi materi maksimal 1000 karakter"
		} else {
			values["materi_deskripsi"] = deskripsi
		}
	}
	if !partial || bodyHasKey(body, "materi_tipe") {
		tipe := "teks"
		if !partial {
			tipe = bodyStringDefault(body, "materi_tipe", "teks")
		} else if raw := bodyString(body, "materi_tipe"); raw != "" {
			tipe = raw
		}
		normalized := lmsNormalizeMaterialType(tipe)
		if normalized == "" {
			errorsByField["materi_tipe"] = "Tipe materi tidak valid"
		} else {
			values["materi_tipe"] = normalized
		}
	}
	if !partial || bodyHasKey(body, "materi_url") {
		url := bodyStringPtr(body, "materi_url")
		if url != nil && !validateLmsURL(strings.TrimSpace(*url)) {
			errorsByField["materi_url"] = "URL harus diawali http://, https://, atau path internal /"
		} else {
			values["materi_url"] = url
		}
	}
	if !partial || bodyHasKey(body, "materi_konten") {
		konten := bodyStringPtr(body, "materi_konten")
		if konten != nil && len([]rune(*konten)) > 50000 {
			errorsByField["materi_konten"] = "Konten materi maksimal 50000 karakter"
		} else {
			values["materi_konten"] = konten
		}
	}
	if !partial || bodyHasKey(body, "materi_urutan") {
		urutan := bodyInt(body, "materi_urutan", 0)
		if urutan < 0 || urutan > 9999 {
			errorsByField["materi_urutan"] = "Urutan materi harus 0-9999"
		} else {
			values["materi_urutan"] = urutan
		}
	}
	if !partial || bodyHasKey(body, "is_wajib") {
		values["is_wajib"] = bodyBool(body, "is_wajib", true)
	}
	if !partial || bodyHasKey(body, "is_active") {
		values["is_active"] = bodyBool(body, "is_active", true)
	}
	return values, errorsByField
}

func validationDetails(errorsByField map[string]string) []map[string]string {
	details := make([]map[string]string, 0, len(errorsByField))
	for field, message := range errorsByField {
		details = append(details, map[string]string{"field": field, "message": message})
	}
	return details
}

func (s *Server) lmsAdminMaterials(w http.ResponseWriter, r *http.Request) error {
	paketID := httpx.QueryInt(r, "paket_id", 0)
	if paketID <= 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR",
			[]map[string]string{{"field": "paket_id", "message": "paket_id wajib berupa bilangan positif"}})
	}
	paket, err := s.lmsPackage(r.Context(), paketID)
	if err != nil {
		return err
	}
	if paket.PaketTipe != "elearning" {
		return httpx.BadRequest("Materi LMS hanya dapat dikelola untuk paket tipe elearning.", "PACKAGE_NOT_ELEARNING")
	}
	materials, err := s.lmsMaterials(r.Context(), 0, paketID, true)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(materials))
	for _, material := range materials {
		items = append(items, map[string]any{
			"materi_id":        material.MateriID,
			"paket_id":         material.PaketID,
			"materi_judul":     material.Judul,
			"materi_deskripsi": material.Deskripsi,
			"materi_tipe":      material.Tipe,
			"materi_url":       material.URL,
			"materi_konten":    material.Konten,
			"materi_urutan":    material.Urutan,
			"is_wajib":         material.Wajib,
			"is_active":        material.Active,
			"created_at":       material.CreatedAt,
			"updated_at":       material.UpdatedAt,
			"_count":           map[string]int{"progress": material.ProgressCount},
		})
	}
	httpx.Success(w, map[string]any{"paket": paket, "materials": items}, "")
	return nil
}

func (s *Server) lmsCreateCourse(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	judul := strings.TrimSpace(bodyString(body, "paket_nama"))
	errorsByField := map[string]string{}
	if length := len([]rune(judul)); length < 3 || length > 200 {
		errorsByField["paket_nama"] = "Nama kelas minimal 3 karakter dan maksimal 200 karakter"
	}
	if deskripsi := bodyStringPtr(body, "paket_deskripsi"); deskripsi != nil && len([]rune(*deskripsi)) > 2000 {
		errorsByField["paket_deskripsi"] = "Deskripsi maksimal 2000 karakter"
	}
	modules := bodySlice(body, "modules")
	if len(modules) > 100 {
		errorsByField["modules"] = "Maksimal 100 modul per kelas"
	}
	type moduleInput struct {
		Judul     string
		Deskripsi *string
		Tipe      string
		URL       *string
		Konten    *string
		Urutan    int
		Wajib     bool
		Active    bool
	}
	moduleInputs := []moduleInput{}
	for index, raw := range modules {
		moduleBody, ok := raw.(map[string]any)
		if !ok {
			errorsByField[fmt.Sprintf("modules.%d", index)] = "Modul tidak valid"
			continue
		}
		values, fieldErrors := lmsReadMaterialBody(moduleBody, false)
		if len(fieldErrors) > 0 {
			for field, message := range fieldErrors {
				errorsByField[fmt.Sprintf("modules.%d.%s", index, field)] = message
			}
			continue
		}
		moduleInputs = append(moduleInputs, moduleInput{
			Judul: values["materi_judul"].(string), Deskripsi: values["materi_deskripsi"].(*string),
			Tipe: values["materi_tipe"].(string), URL: values["materi_url"].(*string),
			Konten: values["materi_konten"].(*string), Urutan: values["materi_urutan"].(int),
			Wajib: values["is_wajib"].(bool), Active: values["is_active"].(bool),
		})
	}
	if len(errorsByField) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(errorsByField))
	}

	harga := bodyInt(body, "paket_harga", 0)
	jp := bodyInt(body, "paket_jp", 8)
	passing := bodyInt(body, "passing_grade", 0)
	duration := bodyInt(body, "duration_minutes", 60)
	maxAttempts := bodyInt(body, "max_attempts", 3)
	jumlahSoal := bodyInt(body, "paket_jumlah_soal", 10)
	if harga < 0 || harga > 1_000_000_000 {
		errorsByField["paket_harga"] = "Harga tidak valid"
	}
	if jp < 1 || jp > 999 {
		errorsByField["paket_jp"] = "JP harus 1-999"
	}
	if passing < 0 || passing > 9999 {
		errorsByField["passing_grade"] = "Passing grade harus 0-9999"
	}
	if duration < 1 || duration > 1440 {
		errorsByField["duration_minutes"] = "Durasi harus 1-1440 menit"
	}
	if maxAttempts < 0 || maxAttempts > 1000 {
		errorsByField["max_attempts"] = "Maksimal attempt harus 0-1000"
	}
	if jumlahSoal < 1 || jumlahSoal > 500 {
		errorsByField["paket_jumlah_soal"] = "Jumlah soal harus 1-500"
	}
	mode := bodyStringDefault(body, "mode_soal", "acak")
	if mode != "acak" && mode != "tetap" {
		errorsByField["mode_soal"] = "Mode soal harus acak atau tetap"
	}
	if len(errorsByField) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(errorsByField))
	}

	var paketID int
	var bankBatch *string
	err = pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO paket_ujian (paket_nama, paket_deskripsi, paket_tipe, jenis_paket, bank_batch,
				paket_harga, passing_grade, duration_minutes, max_attempts, paket_jp, is_pembahasan_visible,
				tanggal_buka_pembahasan, paket_komposisi, mode_soal, paket_bobot_kategori, passing_mode,
				paket_passing_kategori, selection_mode, paket_jumlah_soal, allow_backtrack, intro_kategori_mode,
				skala_nilai, focus_lock_enabled, focus_lock_max_violations, is_active, paket_landing,
				paket_landing_urutan, organization_id, paket_access_code, created_at, updated_at)
			VALUES ($1,$2,'elearning','tryout',$3,$4,$5,$6,$7,$8,$9,NULL,NULL,$10,NULL,'jumlah_soal',
				NULL,'otomatis',$11,true,'semua','skala_100',false,3,true,false,0,NULL,NULL,now(),now())
			RETURNING paket_id`,
			judul, bodyStringPtr(body, "paket_deskripsi"), bodyStringPtr(body, "bank_batch"), harga,
			passing, duration, maxAttempts, jp, bodyBool(body, "is_pembahasan_visible", true), mode, jumlahSoal).
			Scan(&paketID); err != nil {
			return err
		}
		batch := bodyString(body, "bank_batch")
		if batch == "" {
			batch = fmt.Sprintf("lms-%d", paketID)
			if _, err := tx.Exec(r.Context(), `UPDATE paket_ujian SET bank_batch = $1, updated_at = now() WHERE paket_id = $2`,
				batch, paketID); err != nil {
				return err
			}
		}
		bankBatch = &batch
		batchValue := batch
		bankBatch = &batchValue
		for index, module := range moduleInputs {
			urutan := module.Urutan
			if _, exists := modules[index].(map[string]any)["materi_urutan"]; !exists || urutan == 0 {
				urutan = index + 1
			}
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO lms_materi (paket_id, materi_judul, materi_deskripsi, materi_tipe, materi_url,
					materi_konten, materi_urutan, is_wajib, is_active, created_at, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now(),now())`,
				paketID, module.Judul, module.Deskripsi, module.Tipe, module.URL, module.Konten,
				urutan, module.Wajib, module.Active); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	paket, err := s.lmsPackage(r.Context(), paketID)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "LMS_COURSE_CREATE", auth.RequestIP(r), map[string]any{
		"paket_id": paketID, "modules": len(moduleInputs), "bank_batch": bankBatch,
	})
	httpx.Created(w, paket, "Kelas LMS berhasil dibuat")
	return nil
}

func (s *Server) lmsCreateMaterial(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	paketID := bodyInt(body, "paket_id", 0)
	if paketID <= 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR",
			[]map[string]string{{"field": "paket_id", "message": "paket_id wajib berupa bilangan positif"}})
	}
	values, fieldErrors := lmsReadMaterialBody(body, false)
	if len(fieldErrors) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(fieldErrors))
	}
	paket, err := s.lmsPackage(r.Context(), paketID)
	if err != nil {
		return err
	}
	if paket.PaketTipe != "elearning" {
		return httpx.BadRequest("Materi LMS hanya dapat ditambahkan ke paket tipe elearning.", "PACKAGE_NOT_ELEARNING")
	}
	var materiID int
	var created, updated time.Time
	if err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO lms_materi (paket_id, materi_judul, materi_deskripsi, materi_tipe, materi_url,
			materi_konten, materi_urutan, is_wajib, is_active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now(),now())
		RETURNING materi_id, created_at, updated_at`,
		paketID, values["materi_judul"], values["materi_deskripsi"], values["materi_tipe"],
		values["materi_url"], values["materi_konten"], values["materi_urutan"],
		values["is_wajib"], values["is_active"]).Scan(&materiID, &created, &updated); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "LMS_MATERIAL_CREATE", auth.RequestIP(r), map[string]any{
		"paket_id": paketID, "materi_id": materiID,
	})
	httpx.Created(w, map[string]any{
		"materi_id": materiID, "paket_id": paketID,
		"materi_judul": values["materi_judul"], "materi_deskripsi": values["materi_deskripsi"],
		"materi_tipe": values["materi_tipe"], "materi_url": values["materi_url"],
		"materi_konten": values["materi_konten"], "materi_urutan": values["materi_urutan"],
		"is_wajib": values["is_wajib"], "is_active": values["is_active"],
		"created_at": created, "updated_at": updated,
	}, "Materi LMS berhasil dibuat")
	return nil
}

func (s *Server) lmsUpdateMaterial(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	materiID, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	var currentPaketID int
	if err := s.Pool.QueryRow(r.Context(), `SELECT paket_id FROM lms_materi WHERE materi_id = $1`, materiID).Scan(&currentPaketID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Materi tidak ditemukan", "LMS_MATERIAL_NOT_FOUND")
		}
		return err
	}
	targetPaketID := currentPaketID
	if bodyHasKey(body, "paket_id") {
		targetPaketID = bodyInt(body, "paket_id", currentPaketID)
		if targetPaketID != currentPaketID {
			paket, err := s.lmsPackage(r.Context(), targetPaketID)
			if err != nil {
				return err
			}
			if paket.PaketTipe != "elearning" {
				return httpx.BadRequest("Materi LMS hanya dapat dipindahkan ke paket tipe elearning.", "PACKAGE_NOT_ELEARNING")
			}
		}
	}
	values, fieldErrors := lmsReadMaterialBody(body, true)
	if len(fieldErrors) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(fieldErrors))
	}
	set := []string{}
	args := []any{}
	index := 1
	if targetPaketID != currentPaketID {
		set = append(set, fmt.Sprintf("paket_id = $%d", index))
		args = append(args, targetPaketID)
		index++
	}
	for _, column := range []string{"materi_judul", "materi_deskripsi", "materi_tipe", "materi_url",
		"materi_konten", "materi_urutan", "is_wajib", "is_active"} {
		if value, exists := values[column]; exists {
			set = append(set, fmt.Sprintf("%s = $%d", column, index))
			args = append(args, value)
			index++
		}
	}
	if len(set) > 0 {
		set = append(set, "updated_at = now()")
		args = append(args, materiID)
		if _, err := s.Pool.Exec(r.Context(), fmt.Sprintf(`UPDATE lms_materi SET %s WHERE materi_id = $%d`,
			strings.Join(set, ", "), index), args...); err != nil {
			return err
		}
	}
	var item lmsMaterialData
	if err := s.Pool.QueryRow(r.Context(), `
		SELECT materi_id, paket_id, materi_judul, materi_deskripsi, materi_tipe, materi_url,
		       materi_konten, materi_urutan, is_wajib, is_active, created_at, updated_at,
		       NULL::timestamp, 0
		FROM lms_materi WHERE materi_id = $1`, materiID).Scan(&item.MateriID, &item.PaketID, &item.Judul,
		&item.Deskripsi, &item.Tipe, &item.URL, &item.Konten, &item.Urutan, &item.Wajib, &item.Active,
		&item.CreatedAt, &item.UpdatedAt, &item.CompletedAt, &item.ProgressCount); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "LMS_MATERIAL_UPDATE", auth.RequestIP(r), map[string]any{
		"paket_id": item.PaketID, "materi_id": materiID,
	})
	httpx.Success(w, map[string]any{
		"materi_id": item.MateriID, "paket_id": item.PaketID, "materi_judul": item.Judul,
		"materi_deskripsi": item.Deskripsi, "materi_tipe": item.Tipe, "materi_url": item.URL,
		"materi_konten": item.Konten, "materi_urutan": item.Urutan, "is_wajib": item.Wajib,
		"is_active": item.Active, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt,
	}, "Materi LMS berhasil diperbarui")
	return nil
}

func (s *Server) lmsRemoveMaterial(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	materiID, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	var paketID int
	if err := s.Pool.QueryRow(r.Context(), `SELECT paket_id FROM lms_materi WHERE materi_id = $1`, materiID).Scan(&paketID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Materi tidak ditemukan", "LMS_MATERIAL_NOT_FOUND")
		}
		return err
	}
	if _, err := s.Pool.Exec(r.Context(), `DELETE FROM lms_materi WHERE materi_id = $1`, materiID); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "LMS_MATERIAL_DELETE", auth.RequestIP(r), map[string]any{
		"paket_id": paketID, "materi_id": materiID,
	})
	httpx.Success(w, nil, "Materi LMS berhasil dihapus")
	return nil
}
