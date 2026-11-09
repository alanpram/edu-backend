package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
)

type packageCount struct {
	SoalList     int `json:"soal_list"`
	ExamSessions int `json:"exam_sessions"`
	UserAccesses int `json:"user_accesses"`
	LmsMaterials int `json:"lms_materials"`
}

type manualQuestion struct {
	SoalID int `json:"soal_id"`
	Urutan int `json:"urutan"`
}

type Paket struct {
	PaketID                int              `json:"paket_id"`
	PaketNama              string           `json:"paket_nama"`
	PaketDeskripsi         *string          `json:"paket_deskripsi"`
	PaketTipe              string           `json:"paket_tipe"`
	JenisPaket             string           `json:"jenis_paket"`
	BankBatch              *string          `json:"bank_batch"`
	PaketHarga             float64          `json:"paket_harga"`
	PassingGrade           float64          `json:"passing_grade"`
	DurationMinutes        int              `json:"duration_minutes"`
	MaxAttempts            int              `json:"max_attempts"`
	PaketJP                int              `json:"paket_jp"`
	IsPembahasanVisible    bool             `json:"is_pembahasan_visible"`
	TanggalBukaPembahasan  *time.Time       `json:"tanggal_buka_pembahasan"`
	PaketKomposisi         json.RawMessage  `json:"paket_komposisi"`
	ModeSoal               string           `json:"mode_soal"`
	PaketBobotKategori     json.RawMessage  `json:"paket_bobot_kategori"`
	PassingMode            string           `json:"passing_mode"`
	PaketPassingKategori   json.RawMessage  `json:"paket_passing_kategori"`
	SelectionMode          string           `json:"selection_mode"`
	PaketJumlahSoal        int              `json:"paket_jumlah_soal"`
	AllowBacktrack         bool             `json:"allow_backtrack"`
	IntroKategoriMode      string           `json:"intro_kategori_mode"`
	SkalaNilai             string           `json:"skala_nilai"`
	FocusLockEnabled       bool             `json:"focus_lock_enabled"`
	FocusLockMaxViolations int              `json:"focus_lock_max_violations"`
	IsActive               bool             `json:"is_active"`
	PaketLanding           bool             `json:"paket_landing"`
	PaketLandingUrutan     int              `json:"paket_landing_urutan"`
	OrganizationID         *int             `json:"organization_id"`
	PaketAccessCode        *string          `json:"paket_access_code"`
	CreatedAt              time.Time        `json:"created_at"`
	UpdatedAt              time.Time        `json:"updated_at"`
	Count                  packageCount     `json:"_count"`
	ManualQuestions        []manualQuestion `json:"manual_questions"`
	BankPoolCount          int              `json:"bank_pool_count"`
}

const packageSelect = `
	p.paket_id, p.paket_nama, p.paket_deskripsi, p.paket_tipe, p.jenis_paket, p.bank_batch,
	p.paket_harga, p.passing_grade, p.duration_minutes, p.max_attempts, p.paket_jp,
	p.is_pembahasan_visible, p.tanggal_buka_pembahasan, p.paket_komposisi, p.mode_soal,
	p.paket_bobot_kategori, p.passing_mode, p.paket_passing_kategori, p.selection_mode,
	p.paket_jumlah_soal, p.allow_backtrack, p.intro_kategori_mode, p.skala_nilai,
	p.focus_lock_enabled, p.focus_lock_max_violations, p.is_active, p.paket_landing,
	p.paket_landing_urutan, p.organization_id, p.paket_access_code, p.created_at, p.updated_at,
	(SELECT count(*) FROM soal_bank s WHERE s.paket_id = p.paket_id) AS soal_list_count,
	(SELECT count(*) FROM exam_session e WHERE e.paket_id = p.paket_id) AS exam_session_count,
	(SELECT count(*) FROM user_package_access a WHERE a.paket_id = p.paket_id) AS access_count,
	(SELECT count(*) FROM lms_materi m WHERE m.paket_id = p.paket_id) AS lms_material_count,
	COALESCE((SELECT json_agg(json_build_object('soal_id', q.soal_id, 'urutan', q.urutan) ORDER BY q.urutan)
	          FROM paket_soal q WHERE q.paket_id = p.paket_id), '[]'::json) AS manual_questions,
	(SELECT count(*) FROM soal_bank s
	 WHERE s.soal_aktif = true
	   AND s.bank_jenis = CASE WHEN p.jenis_paket = 'ujian_real' THEN 'ujian_real' ELSE 'tryout' END
	   AND lower(s.bank_tipe) = lower(p.paket_tipe)
	   AND ((p.organization_id IS NULL AND s.organization_id IS NULL) OR (p.organization_id IS NOT NULL AND s.organization_id = p.organization_id))
	   AND (p.bank_batch IS NULL OR lower(s.bank_batch) = lower(p.bank_batch))
	) AS bank_pool_count`

func scanPaket(row pgx.Row) (Paket, error) {
	var item Paket
	var komposisi, bobot, passing, manual []byte
	err := row.Scan(
		&item.PaketID, &item.PaketNama, &item.PaketDeskripsi, &item.PaketTipe, &item.JenisPaket, &item.BankBatch,
		&item.PaketHarga, &item.PassingGrade, &item.DurationMinutes, &item.MaxAttempts, &item.PaketJP,
		&item.IsPembahasanVisible, &item.TanggalBukaPembahasan, &komposisi, &item.ModeSoal,
		&bobot, &item.PassingMode, &passing, &item.SelectionMode,
		&item.PaketJumlahSoal, &item.AllowBacktrack, &item.IntroKategoriMode, &item.SkalaNilai,
		&item.FocusLockEnabled, &item.FocusLockMaxViolations, &item.IsActive, &item.PaketLanding,
		&item.PaketLandingUrutan, &item.OrganizationID, &item.PaketAccessCode, &item.CreatedAt, &item.UpdatedAt,
		&item.Count.SoalList, &item.Count.ExamSessions, &item.Count.UserAccesses, &item.Count.LmsMaterials,
		&manual, &item.BankPoolCount,
	)
	if err != nil {
		return item, err
	}
	if len(komposisi) > 0 {
		item.PaketKomposisi = json.RawMessage(komposisi)
	}
	if len(bobot) > 0 {
		item.PaketBobotKategori = json.RawMessage(bobot)
	}
	if len(passing) > 0 {
		item.PaketPassingKategori = json.RawMessage(passing)
	}
	item.ManualQuestions = []manualQuestion{}
	if len(manual) > 0 {
		_ = json.Unmarshal(manual, &item.ManualQuestions)
	}
	return item, nil
}

func (s *Server) registerPackages(api chi.Router) {
	api.Route("/packages", func(r chi.Router) {
		r.Use(s.Auth.OptionalAuthenticate)
		r.Get("/", httpx.Wrap(s.listPackages))
		r.Get("/{id}", httpx.Wrap(s.packageDetail))
		r.Get("/{id}/stats", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.Auth.Authenticate(middleware.RequirePermission(s.Permissions, "admin.packages.view")(httpx.Wrap(s.packageStats))).ServeHTTP(w, r)
		}))
		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Authenticate)
			r.Use(middleware.RequirePermission(s.Permissions, "admin.packages.manage"))
			r.Post("/", httpx.Wrap(s.createPackage))
			r.Put("/{id}", httpx.Wrap(s.updatePackage))
			r.Delete("/{id}", httpx.Wrap(s.deletePackage))
		})
	})
}

func (s *Server) canViewAllPackages(r *http.Request) bool {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		return false
	}
	allowed, err := s.Permissions.HasPermission(r.Context(), principal.UserRole, "admin.packages.view")
	return err == nil && allowed
}

func (s *Server) listPackages(w http.ResponseWriter, r *http.Request) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	canViewAll := s.canViewAllPackages(r)
	conditions := []string{"1=1"}
	args := []any{}
	index := 1

	if !canViewAll {
		conditions = append(conditions, "p.organization_id IS NULL", "p.is_active = true")
	} else if active := httpx.QueryBoolPtr(r, "is_active"); active != nil {
		conditions = append(conditions, fmt.Sprintf("p.is_active = $%d", index))
		args = append(args, *active)
		index++
	}
	for _, filter := range []struct{ query, column string }{
		{"paket_tipe", "p.paket_tipe"}, {"paket_tipe_not", "p.paket_tipe"}, {"jenis_paket", "p.jenis_paket"},
	} {
		if value := strings.TrimSpace(r.URL.Query().Get(filter.query)); value != "" {
			operator := "="
			if filter.query == "paket_tipe_not" {
				operator = "<>"
			}
			conditions = append(conditions, fmt.Sprintf("%s %s $%d", filter.column, operator, index))
			args = append(args, strings.ToLower(value))
			index++
		}
	}
	if batch := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("bank_batch"))); batch != "" {
		conditions = append(conditions, fmt.Sprintf("lower(p.bank_batch) = $%d", index))
		args = append(args, batch)
		index++
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("(p.paket_nama ILIKE $%d OR p.paket_deskripsi ILIKE $%d)", index, index))
		args = append(args, "%"+search+"%")
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM paket_ujian p WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`SELECT %s FROM paket_ujian p WHERE %s ORDER BY p.created_at DESC LIMIT $%d OFFSET $%d`,
		packageSelect, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]Paket, 0, limit)
	for rows.Next() {
		item, err := scanPaket(rows)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return nil
}

func (s *Server) packageDetail(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	item, err := scanPaket(s.Pool.QueryRow(r.Context(), `SELECT `+packageSelect+` FROM paket_ujian p WHERE p.paket_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Paket ujian tidak ditemukan", "PACKAGE_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	canViewAll := s.canViewAllPackages(r)
	if !canViewAll && (!item.IsActive || item.OrganizationID != nil) {
		return httpx.NotFound("Paket ujian tidak ditemukan", "PACKAGE_NOT_FOUND")
	}
	httpx.Success(w, item, "")
	return nil
}

func (s *Server) packageStats(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	item, err := scanPaket(s.Pool.QueryRow(r.Context(), `SELECT `+packageSelect+` FROM paket_ujian p WHERE p.paket_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Paket ujian tidak ditemukan", "PACKAGE_NOT_FOUND")
	}
	if err != nil {
		return err
	}

	stats := map[string]any{"paket": item}
	var total, completed, submitted, ongoing, disqualified, scored, passed, totalAccess, activeAccess, participants int
	var avg, highest, lowest *float64
	err = s.Pool.QueryRow(r.Context(), `
		SELECT
			count(*) AS total,
			count(*) FILTER (WHERE status = 'completed') AS completed,
			count(*) FILTER (WHERE status = 'submitted') AS submitted,
			count(*) FILTER (WHERE status = 'ongoing') AS ongoing,
			count(*) FILTER (WHERE status = 'disqualified') AS disqualified,
			count(*) FILTER (WHERE final_score IS NOT NULL) AS scored,
			count(*) FILTER (WHERE is_passed = true) AS passed,
			avg(final_score), max(final_score), min(final_score),
			(SELECT count(DISTINCT user_id) FROM exam_session WHERE paket_id = $1) AS participants
		FROM exam_session WHERE paket_id = $1`, id,
	).Scan(&total, &completed, &submitted, &ongoing, &disqualified, &scored, &passed, &avg, &highest, &lowest, &participants)
	if err != nil {
		return err
	}
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM user_package_access WHERE paket_id = $1`, id).Scan(&totalAccess)
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM user_package_access WHERE paket_id = $1 AND expired_at > now()`, id).Scan(&activeAccess)

	type recent struct {
		SessionID     int        `json:"session_id"`
		UserID        int        `json:"user_id"`
		AttemptNumber int        `json:"attempt_number"`
		Status        string     `json:"status"`
		FinalScore    *float64   `json:"final_score"`
		IsPassed      *bool      `json:"is_passed"`
		StartedAt     time.Time  `json:"started_at"`
		SubmittedAt   *time.Time `json:"submitted_at"`
		User          struct {
			UserNama  string `json:"user_nama"`
			UserEmail string `json:"user_email"`
		} `json:"user"`
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT e.session_id, e.user_id, e.attempt_number, e.status, e.final_score, e.is_passed,
		       e.started_at, e.submitted_at, u.user_nama, u.user_email
		FROM exam_session e JOIN "user" u ON u.user_id = e.user_id
		WHERE e.paket_id = $1 ORDER BY e.started_at DESC LIMIT 12`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	recentSessions := []recent{}
	for rows.Next() {
		var row recent
		if err := rows.Scan(&row.SessionID, &row.UserID, &row.AttemptNumber, &row.Status, &row.FinalScore, &row.IsPassed,
			&row.StartedAt, &row.SubmittedAt, &row.User.UserNama, &row.User.UserEmail); err != nil {
			return err
		}
		recentSessions = append(recentSessions, row)
	}
	passRate := 0.0
	if scored > 0 {
		passRate = float64(passed) / float64(scored) * 100
	}
	stats["total_participants"] = participants
	stats["total_sessions"] = total
	stats["completed_sessions"] = completed
	stats["submitted_sessions"] = submitted
	stats["ongoing_sessions"] = ongoing
	stats["disqualified_sessions"] = disqualified
	stats["scored_sessions"] = scored
	stats["passed_sessions"] = passed
	stats["average_score"] = avg
	stats["highest_score"] = highest
	stats["lowest_score"] = lowest
	stats["total_accesses"] = totalAccess
	stats["active_accesses"] = activeAccess
	stats["pass_rate"] = passRate
	stats["recent_sessions"] = recentSessions
	httpx.Success(w, stats, "")
	return nil
}

func (s *Server) createPackage(w http.ResponseWriter, r *http.Request) error {
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	v := &validation{}
	name := v.stringField(body, "paket_nama", "Nama paket", 3, 200, true)
	tipe := strings.ToLower(v.stringField(body, "paket_tipe", "Tipe paket", 2, 50, true))
	jenis := bodyString(body, "jenis_paket")
	if jenis == "" {
		jenis = "tryout"
	}
	if jenis != "tryout" && jenis != "ujian_real" {
		v.add("jenis_paket", "Jenis paket tidak valid")
	}
	if err := v.err(); err != nil {
		return err
	}
	selectionMode := bodyString(body, "selection_mode")
	if selectionMode == "" {
		selectionMode = "auto"
	}
	manualIDs := bodyIntSlice(body, "manual_soal_ids")
	if selectionMode == "manual" && len(manualIDs) == 0 {
		return httpx.Unprocessable("Mode pilih soal sendiri memerlukan minimal satu soal.", "MANUAL_QUESTION_SELECTION_REQUIRED", nil)
	}
	if len(manualIDs) > 0 {
		if err := s.validateManualQuestions(r, nil, tipe, jenis, nil, manualIDs); err != nil {
			return err
		}
	}

	passingMode := bodyString(body, "passing_mode")
	if passingMode == "" {
		passingMode = "overall"
	}
	if err := validatePassingConfig(body, passingMode); err != nil {
		return err
	}
	modeSoal := bodyString(body, "mode_soal")
	if modeSoal == "" {
		modeSoal = "acak"
	}
	if jenis == "ujian_real" {
		modeSoal = "acak"
	}

	params := packageParams{
		nama: name, deskripsi: bodyStringPtr(body, "paket_deskripsi"), tipe: tipe, jenis: jenis,
		batch: bodyStringPtr(body, "bank_batch"), harga: bodyFloat(body, "paket_harga", 0),
		passingGrade: bodyFloat(body, "passing_grade", 0), duration: bodyInt(body, "duration_minutes", 90),
		maxAttempts: bodyInt(body, "max_attempts", 3), jp: bodyInt(body, "paket_jp", 8),
		pembahasanVisible: bodyBool(body, "is_pembahasan_visible", true),
		tanggalBuka:       parseDatePtr(body["tanggal_buka_pembahasan"]),
		komposisi:         bodyMap(body, "paket_komposisi"), modeSoal: modeSoal,
		bobot: bodyMap(body, "paket_bobot_kategori"), passingMode: passingMode,
		passingKategori: bodyMap(body, "paket_passing_kategori"), selectionMode: selectionMode,
		jumlahSoal: bodyInt(body, "paket_jumlah_soal", 25), allowBacktrack: bodyBool(body, "allow_backtrack", true),
		introMode:  bodyStringDefault(body, "intro_kategori_mode", "always"),
		skalaNilai: bodyStringDefault(body, "skala_nilai", "skala_100"),
		focusLock:  bodyBool(body, "focus_lock_enabled", true), focusMax: bodyInt(body, "focus_lock_max_violations", 3),
		active: bodyBool(body, "is_active", true), landing: bodyBool(body, "paket_landing", false),
		landingOrder:   bodyInt(body, "paket_landing_urutan", 0),
		organizationID: bodyIntPtr(body, "organization_id"), accessCode: bodyStringPtr(body, "paket_access_code"),
	}
	if selectionMode == "manual" {
		params.jumlahSoal = len(manualIDs)
	}
	if params.introMode != "always" && params.introMode != "first" && params.introMode != "never" {
		params.introMode = "always"
	}
	if params.focusMax < 1 {
		params.focusMax = 1
	}
	if params.focusMax > 20 {
		params.focusMax = 20
	}

	item, err := s.insertPackage(r, params)
	if err != nil {
		return err
	}
	if selectionMode == "manual" {
		if err := s.replaceManualQuestions(r, item.PaketID, manualIDs); err != nil {
			return err
		}
		item, _ = scanPaket(s.Pool.QueryRow(r.Context(), `SELECT `+packageSelect+` FROM paket_ujian p WHERE p.paket_id = $1`, item.PaketID))
	}
	httpx.Created(w, item, "Paket ujian berhasil dibuat")
	return nil
}

type packageParams struct {
	nama, tipe, jenis, modeSoal, passingMode, selectionMode, introMode, skalaNilai string
	deskripsi, batch, accessCode                                                   *string
	harga, passingGrade                                                            float64
	duration, maxAttempts, jp, jumlahSoal, focusMax, landingOrder                  int
	pembahasanVisible, allowBacktrack, focusLock, active, landing                  bool
	tanggalBuka                                                                    *time.Time
	komposisi, bobot, passingKategori                                              map[string]any
	organizationID                                                                 *int
}

func (s *Server) insertPackage(r *http.Request, p packageParams) (Paket, error) {
	return scanPaket(s.Pool.QueryRow(r.Context(), `
		INSERT INTO paket_ujian (
			paket_nama, paket_deskripsi, paket_tipe, jenis_paket, bank_batch, paket_harga, passing_grade,
			duration_minutes, max_attempts, paket_jp, is_pembahasan_visible, tanggal_buka_pembahasan,
			paket_komposisi, mode_soal, paket_bobot_kategori, passing_mode, paket_passing_kategori,
			selection_mode, paket_jumlah_soal, allow_backtrack, intro_kategori_mode, skala_nilai,
			focus_lock_enabled, focus_lock_max_violations, is_active, paket_landing, paket_landing_urutan,
			organization_id, paket_access_code, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14,$15::jsonb,$16,$17::jsonb,
			$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,now(),now()
		) RETURNING `+packageSelect,
		p.nama, p.deskripsi, p.tipe, p.jenis, p.batch, p.harga, p.passingGrade,
		p.duration, p.maxAttempts, p.jp, p.pembahasanVisible, p.tanggalBuka,
		jsonArgument(p.komposisi), p.modeSoal, jsonArgument(p.bobot), p.passingMode, jsonArgument(p.passingKategori),
		p.selectionMode, p.jumlahSoal, p.allowBacktrack, p.introMode, p.skalaNilai,
		p.focusLock, p.focusMax, p.active, p.landing, p.landingOrder,
		p.organizationID, p.accessCode,
	))
}

func (s *Server) updatePackage(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	existing, err := scanPaket(s.Pool.QueryRow(r.Context(), `SELECT `+packageSelect+` FROM paket_ujian p WHERE p.paket_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Paket ujian tidak ditemukan", "PACKAGE_NOT_FOUND")
	}
	if err != nil {
		return err
	}

	set := []string{}
	args := []any{}
	index := 1
	add := func(column, cast string, value any) {
		placeholder := fmt.Sprintf("$%d", index)
		if cast != "" {
			placeholder += "::" + cast
		}
		set = append(set, column+" = "+placeholder)
		args = append(args, value)
		index++
	}
	stringFields := map[string]string{
		"paket_nama": "paket_nama", "paket_deskripsi": "paket_deskripsi", "paket_tipe": "paket_tipe",
		"jenis_paket": "jenis_paket", "bank_batch": "bank_batch", "mode_soal": "mode_soal",
		"passing_mode": "passing_mode", "selection_mode": "selection_mode", "intro_kategori_mode": "intro_kategori_mode",
		"skala_nilai": "skala_nilai", "paket_access_code": "paket_access_code",
	}
	for bodyKey, column := range stringFields {
		if value, exists := body[bodyKey]; exists {
			if value == nil {
				add(column, "", nil)
			} else {
				text, _ := value.(string)
				text = strings.TrimSpace(text)
				if bodyKey == "paket_tipe" || bodyKey == "jenis_paket" || bodyKey == "bank_batch" {
					text = strings.ToLower(text)
				}
				if text == "" {
					add(column, "", nil)
				} else {
					add(column, "", text)
				}
			}
		}
	}
	for bodyKey, column := range map[string]string{
		"paket_harga": "paket_harga", "passing_grade": "passing_grade",
	} {
		if _, exists := body[bodyKey]; exists {
			add(column, "", bodyFloat(body, bodyKey, 0))
		}
	}
	for bodyKey, column := range map[string]string{
		"duration_minutes": "duration_minutes", "max_attempts": "max_attempts", "paket_jp": "paket_jp",
		"paket_jumlah_soal": "paket_jumlah_soal", "focus_lock_max_violations": "focus_lock_max_violations",
		"paket_landing_urutan": "paket_landing_urutan",
	} {
		if _, exists := body[bodyKey]; exists {
			add(column, "", bodyInt(body, bodyKey, 0))
		}
	}
	for bodyKey, column := range map[string]string{
		"is_pembahasan_visible": "is_pembahasan_visible", "allow_backtrack": "allow_backtrack",
		"focus_lock_enabled": "focus_lock_enabled", "is_active": "is_active",
		"paket_landing": "paket_landing",
	} {
		if _, exists := body[bodyKey]; exists {
			add(column, "", bodyBool(body, bodyKey, false))
		}
	}
	for bodyKey, column := range map[string]string{
		"paket_komposisi": "paket_komposisi", "paket_bobot_kategori": "paket_bobot_kategori",
		"paket_passing_kategori": "paket_passing_kategori",
	} {
		if value, exists := body[bodyKey]; exists {
			if value == nil {
				add(column, "jsonb", nil)
			} else {
				add(column, "jsonb", jsonArgument(bodyMap(body, bodyKey)))
			}
		}
	}
	if value, exists := body["tanggal_buka_pembahasan"]; exists {
		if value == nil {
			add("tanggal_buka_pembahasan", "", nil)
		} else {
			add("tanggal_buka_pembahasan", "", parseDatePtr(value))
		}
	}
	if value, exists := body["organization_id"]; exists {
		if value == nil {
			add("organization_id", "", nil)
		} else {
			add("organization_id", "", bodyInt(body, "organization_id", 0))
		}
	}
	if len(set) == 0 {
		httpx.Success(w, existing, "Paket ujian berhasil diperbarui")
		return nil
	}
	set = append(set, "updated_at = now()")
	args = append(args, id)
	query := fmt.Sprintf(`UPDATE paket_ujian SET %s WHERE paket_id = $%d RETURNING %s`, strings.Join(set, ", "), index, packageSelect)
	item, err := scanPaket(s.Pool.QueryRow(r.Context(), query, args...))
	if err != nil {
		return err
	}
	if rawIDs, exists := body["manual_soal_ids"]; exists && rawIDs != nil {
		ids := toIntSlice(rawIDs)
		if err := s.validateManualQuestions(r, &id, item.PaketTipe, item.JenisPaket, item.OrganizationID, ids); err != nil {
			return err
		}
		if err := s.replaceManualQuestions(r, id, ids); err != nil {
			return err
		}
		item, _ = scanPaket(s.Pool.QueryRow(r.Context(), `SELECT `+packageSelect+` FROM paket_ujian p WHERE p.paket_id = $1`, id))
	}
	httpx.Success(w, item, "Paket ujian berhasil diperbarui")
	return nil
}

func (s *Server) deletePackage(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	item, err := scanPaket(s.Pool.QueryRow(r.Context(), `
		UPDATE paket_ujian SET is_active = false, updated_at = now()
		WHERE paket_id = $1 RETURNING `+packageSelect, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Paket ujian tidak ditemukan", "PACKAGE_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	httpx.Success(w, item, "Paket ujian dinonaktifkan (soft delete).")
	return nil
}

func (s *Server) validateManualQuestions(r *http.Request, packageID *int, tipe, jenis string, organizationID *int, ids []int) error {
	if len(ids) == 0 {
		return httpx.Unprocessable("Mode pilih soal sendiri memerlukan minimal satu soal.", "MANUAL_QUESTION_SELECTION_REQUIRED", nil)
	}
	if len(ids) > 2000 {
		return httpx.Unprocessable("Terlalu banyak soal manual.", "VALIDATION_ERROR", nil)
	}
	expectedJenis := "tryout"
	if jenis == "ujian_real" {
		expectedJenis = "ujian_real"
	}
	for _, soalID := range ids {
		var active bool
		var bankJenis, bankTipe string
		var pkgID *int
		var soalOrg *int
		err := s.Pool.QueryRow(r.Context(), `
			SELECT soal_aktif, bank_jenis, bank_tipe, paket_id, organization_id
			FROM soal_bank WHERE soal_id = $1`, soalID,
		).Scan(&active, &bankJenis, &bankTipe, &pkgID, &soalOrg)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.Unprocessable("Sebagian soal yang dipilih tidak ditemukan atau bukan milik organisasi ini.", "MANUAL_QUESTION_NOT_FOUND", map[string]any{"missing": []int{soalID}})
		}
		if err != nil {
			return err
		}
		if !active {
			return httpx.Unprocessable("Soal yang dipilih sudah nonaktif. Aktifkan kembali atau pilih soal lain.", "MANUAL_QUESTION_INACTIVE", map[string]any{"soal_id": soalID})
		}
		if bankJenis != expectedJenis || strings.ToLower(bankTipe) != strings.ToLower(tipe) {
			return httpx.Unprocessable("Soal yang dipilih berasal dari bank yang tidak sesuai dengan paket.", "MANUAL_QUESTION_BANK_MISMATCH", map[string]any{"soal_id": soalID})
		}
		if organizationID != nil {
			if soalOrg == nil || *soalOrg != *organizationID {
				return httpx.Unprocessable("Sebagian soal yang dipilih tidak ditemukan atau bukan milik organisasi ini.", "MANUAL_QUESTION_NOT_FOUND", map[string]any{"missing": []int{soalID}})
			}
		}
		if strings.ToLower(tipe) == "elearning" && pkgID != nil && packageID != nil && *pkgID != *packageID {
			return httpx.Unprocessable("Soal post-test harus berasal dari kelas yang sama.", "MANUAL_QUESTION_COURSE_MISMATCH", map[string]any{"soal_id": soalID})
		}
	}
	return nil
}

func (s *Server) replaceManualQuestions(r *http.Request, paketID int, ids []int) error {
	return pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `DELETE FROM paket_soal WHERE paket_id = $1`, paketID); err != nil {
			return err
		}
		for index, soalID := range ids {
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO paket_soal (paket_id, soal_id, urutan, created_at)
				VALUES ($1, $2, $3, now())`, paketID, soalID, index+1); err != nil {
				return err
			}
		}
		return nil
	})
}

func validatePassingConfig(body map[string]any, passingMode string) error {
	if passingMode != "per_category" {
		return nil
	}
	thresholds := bodyMap(body, "paket_passing_kategori")
	if len(thresholds) == 0 {
		return httpx.Unprocessable("Mode per kategori memerlukan minimal satu ambang poin kategori.", "PASSING_CATEGORY_REQUIRED", nil)
	}
	return nil
}

func jsonArgument(value any) any {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func bodyStringDefault(body map[string]any, key, fallback string) string {
	if value := bodyString(body, key); value != "" {
		return value
	}
	return fallback
}

func bodyIntSlice(body map[string]any, key string) []int {
	return toIntSlice(body[key])
}

func toIntSlice(raw any) []int {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]int, 0, len(values))
	seen := map[int]bool{}
	for _, value := range values {
		number := 0
		switch typed := value.(type) {
		case float64:
			number = int(typed)
		case json.Number:
			parsed, _ := typed.Int64()
			number = int(parsed)
		case int:
			number = typed
		}
		if number > 0 && !seen[number] {
			seen[number] = true
			result = append(result, number)
		}
	}
	return result
}

func parseDatePtr(value any) *time.Time {
	if value == nil {
		return nil
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		if date, err := time.Parse("2006-01-02", text); err == nil {
			return &date
		}
		return nil
	}
	return &parsed
}
