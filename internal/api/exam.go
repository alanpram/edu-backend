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
	"github.com/edumaster/backend-go/internal/codegen"
	"github.com/edumaster/backend-go/internal/exam"
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
)

type examPackage struct {
	PaketID                int             `json:"paket_id"`
	PaketNama              string          `json:"paket_nama"`
	PaketTipe              string          `json:"paket_tipe"`
	JenisPaket             string          `json:"jenis_paket"`
	BankBatch              *string         `json:"bank_batch"`
	PaketHarga             float64         `json:"paket_harga"`
	IsActive               bool            `json:"is_active"`
	OrganizationID         *int            `json:"organization_id"`
	MaxAttempts            int             `json:"max_attempts"`
	DurationMinutes        int             `json:"duration_minutes"`
	AllowBacktrack         bool            `json:"allow_backtrack"`
	IsPembahasanVisible    bool            `json:"is_pembahasan_visible"`
	TanggalBukaPembahasan  *time.Time      `json:"tanggal_buka_pembahasan"`
	PassingMode            string          `json:"passing_mode"`
	PassingGrade           float64         `json:"passing_grade"`
	SkalaNilai             string          `json:"skala_nilai"`
	IntroKategoriMode      string          `json:"intro_kategori_mode"`
	FocusLockEnabled       bool            `json:"focus_lock_enabled"`
	FocusLockMaxViolations int             `json:"focus_lock_max_violations"`
	PaketJP                int             `json:"paket_jp"`
	PaketKomposisi         json.RawMessage `json:"paket_komposisi"`
	PaketBobotKategori     json.RawMessage `json:"paket_bobot_kategori"`
	PaketPassingKategori   json.RawMessage `json:"paket_passing_kategori"`
	PaketJumlahSoal        int             `json:"paket_jumlah_soal"`
	ModeSoal               string          `json:"mode_soal"`
	SelectionMode          string          `json:"selection_mode"`
}

type examAnswer struct {
	JawabanID      int             `json:"jawaban_id"`
	SessionID      int             `json:"session_id"`
	SoalID         int             `json:"soal_id"`
	Urutan         int             `json:"urutan"`
	SoalKategori   string          `json:"soal_kategori"`
	SoalKesulitan  string          `json:"soal_kesulitan"`
	SoalTipe       string          `json:"soal_tipe"`
	SoalPertanyaan string          `json:"soal_pertanyaan"`
	SoalGambarURL  *string         `json:"soal_gambar_url"`
	SoalGambarAlt  *string         `json:"soal_gambar_alt"`
	SoalOpsi       json.RawMessage `json:"soal_opsi"`
	SkorMaks       float64         `json:"skor_maks"`
	Jawaban        json.RawMessage `json:"jawaban"`
	IsRagu         bool            `json:"is_ragu"`
	SkorDidapat    *float64        `json:"skor_didapat"`
	IsBenar        *bool           `json:"is_benar"`
	DinilaiOleh    *int            `json:"dinilai_oleh"`
	DinilaiAt      *time.Time      `json:"dinilai_at"`
	DijawabAt      *time.Time      `json:"dijawab_at"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type Session struct {
	SessionID        int             `json:"session_id"`
	UserID           int             `json:"user_id"`
	PaketID          int             `json:"paket_id"`
	AttemptNumber    int             `json:"attempt_number"`
	FinalScore       *float64        `json:"final_score"`
	IsPassed         *bool           `json:"is_passed"`
	Status           string          `json:"status"`
	ScoreBreakdown   json.RawMessage `json:"score_breakdown"`
	ScoringConfig    json.RawMessage `json:"scoring_config"`
	CertificateJP    *int            `json:"certificate_jp"`
	CertificateCode  *string         `json:"certificate_code"`
	AutoSubmitted    bool            `json:"auto_submitted"`
	DisqualifyAlasan *string         `json:"disqualify_alasan"`
	StartedAt        time.Time       `json:"started_at"`
	ExpiresAt        time.Time       `json:"expires_at"`
	SubmittedAt      *time.Time      `json:"submitted_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	Paket            examPackage     `json:"paket"`
	Answers          []examAnswer    `json:"answers"`
}

const examPackageSelect = `p.paket_id, p.paket_nama, p.paket_tipe, p.jenis_paket, p.bank_batch,
	p.paket_harga, p.is_active, p.organization_id, p.max_attempts, p.duration_minutes,
	p.allow_backtrack, p.is_pembahasan_visible, p.tanggal_buka_pembahasan, p.passing_mode,
	p.passing_grade, p.skala_nilai, p.intro_kategori_mode, p.focus_lock_enabled,
	p.focus_lock_max_violations, p.paket_jp, p.paket_komposisi, p.paket_bobot_kategori,
	p.paket_passing_kategori, p.paket_jumlah_soal, p.mode_soal, p.selection_mode`

const examSessionSelect = `s.session_id, s.user_id, s.paket_id, s.attempt_number, s.final_score,
	s.is_passed, s.status, s.score_breakdown, s.scoring_config, s.certificate_jp,
	s.certificate_code, s.auto_submitted, s.disqualify_alasan, s.started_at, s.expires_at,
	s.submitted_at, s.updated_at, ` + examPackageSelect

func scanExamPackage(row pgx.Row, offset int, args ...any) (examPackage, error) {
	var p examPackage
	var komposisi, bobot, passing []byte
	err := row.Scan(append(args,
		&p.PaketID, &p.PaketNama, &p.PaketTipe, &p.JenisPaket, &p.BankBatch,
		&p.PaketHarga, &p.IsActive, &p.OrganizationID, &p.MaxAttempts, &p.DurationMinutes,
		&p.AllowBacktrack, &p.IsPembahasanVisible, &p.TanggalBukaPembahasan, &p.PassingMode,
		&p.PassingGrade, &p.SkalaNilai, &p.IntroKategoriMode, &p.FocusLockEnabled,
		&p.FocusLockMaxViolations, &p.PaketJP, &komposisi, &bobot, &passing,
		&p.PaketJumlahSoal, &p.ModeSoal, &p.SelectionMode,
	)...)
	if len(komposisi) > 0 {
		p.PaketKomposisi = json.RawMessage(komposisi)
	}
	if len(bobot) > 0 {
		p.PaketBobotKategori = json.RawMessage(bobot)
	}
	if len(passing) > 0 {
		p.PaketPassingKategori = json.RawMessage(passing)
	}
	return p, err
}

func (s *Server) loadExamPackage(ctx context.Context, id int) (examPackage, error) {
	var p examPackage
	var komposisi, bobot, passing []byte
	err := s.Pool.QueryRow(ctx, `SELECT `+examPackageSelect+` FROM paket_ujian p WHERE p.paket_id = $1`, id).Scan(
		&p.PaketID, &p.PaketNama, &p.PaketTipe, &p.JenisPaket, &p.BankBatch,
		&p.PaketHarga, &p.IsActive, &p.OrganizationID, &p.MaxAttempts, &p.DurationMinutes,
		&p.AllowBacktrack, &p.IsPembahasanVisible, &p.TanggalBukaPembahasan, &p.PassingMode,
		&p.PassingGrade, &p.SkalaNilai, &p.IntroKategoriMode, &p.FocusLockEnabled,
		&p.FocusLockMaxViolations, &p.PaketJP, &komposisi, &bobot, &passing,
		&p.PaketJumlahSoal, &p.ModeSoal, &p.SelectionMode,
	)
	if len(komposisi) > 0 {
		p.PaketKomposisi = json.RawMessage(komposisi)
	}
	if len(bobot) > 0 {
		p.PaketBobotKategori = json.RawMessage(bobot)
	}
	if len(passing) > 0 {
		p.PaketPassingKategori = json.RawMessage(passing)
	}
	return p, err
}

func (s *Server) loadSession(ctx context.Context, sessionID int) (Session, error) {
	var session Session
	var scoreBreakdown, scoringConfig []byte
	var komposisi, bobot, passing []byte
	err := s.Pool.QueryRow(ctx, `SELECT `+examSessionSelect+`
		FROM exam_session s JOIN paket_ujian p ON p.paket_id = s.paket_id
		WHERE s.session_id = $1`, sessionID).Scan(
		&session.SessionID, &session.UserID, &session.PaketID, &session.AttemptNumber, &session.FinalScore,
		&session.IsPassed, &session.Status, &scoreBreakdown, &scoringConfig, &session.CertificateJP,
		&session.CertificateCode, &session.AutoSubmitted, &session.DisqualifyAlasan, &session.StartedAt,
		&session.ExpiresAt, &session.SubmittedAt, &session.UpdatedAt,
		&session.Paket.PaketID, &session.Paket.PaketNama, &session.Paket.PaketTipe, &session.Paket.JenisPaket,
		&session.Paket.BankBatch, &session.Paket.PaketHarga, &session.Paket.IsActive, &session.Paket.OrganizationID,
		&session.Paket.MaxAttempts, &session.Paket.DurationMinutes, &session.Paket.AllowBacktrack,
		&session.Paket.IsPembahasanVisible, &session.Paket.TanggalBukaPembahasan, &session.Paket.PassingMode,
		&session.Paket.PassingGrade, &session.Paket.SkalaNilai, &session.Paket.IntroKategoriMode,
		&session.Paket.FocusLockEnabled, &session.Paket.FocusLockMaxViolations, &session.Paket.PaketJP,
		&komposisi, &bobot, &passing, &session.Paket.PaketJumlahSoal, &session.Paket.ModeSoal, &session.Paket.SelectionMode,
	)
	if err != nil {
		return session, err
	}
	if len(scoreBreakdown) > 0 {
		session.ScoreBreakdown = json.RawMessage(scoreBreakdown)
	}
	if len(scoringConfig) > 0 {
		session.ScoringConfig = json.RawMessage(scoringConfig)
	}
	if len(komposisi) > 0 {
		session.Paket.PaketKomposisi = json.RawMessage(komposisi)
	}
	if len(bobot) > 0 {
		session.Paket.PaketBobotKategori = json.RawMessage(bobot)
	}
	if len(passing) > 0 {
		session.Paket.PaketPassingKategori = json.RawMessage(passing)
	}
	if err := s.loadAnswers(ctx, &session); err != nil {
		return session, err
	}
	return session, nil
}

func (s *Server) loadAnswers(ctx context.Context, session *Session) error {
	rows, err := s.Pool.Query(ctx, `
		SELECT jawaban_id, session_id, soal_id, urutan, soal_kategori, soal_kesulitan, soal_tipe,
		       soal_pertanyaan, soal_gambar_url, soal_gambar_alt, soal_opsi, skor_maks, jawaban,
		       is_ragu, skor_didapat, is_benar, dinilai_oleh, dinilai_at, dijawab_at, created_at, updated_at
		FROM exam_answer WHERE session_id = $1 ORDER BY urutan ASC`, session.SessionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	session.Answers = []examAnswer{}
	for rows.Next() {
		var item examAnswer
		var opsi, jawaban []byte
		if err := rows.Scan(&item.JawabanID, &item.SessionID, &item.SoalID, &item.Urutan,
			&item.SoalKategori, &item.SoalKesulitan, &item.SoalTipe, &item.SoalPertanyaan,
			&item.SoalGambarURL, &item.SoalGambarAlt, &opsi,
			&item.SkorMaks, &jawaban, &item.IsRagu, &item.SkorDidapat, &item.IsBenar,
			&item.DinilaiOleh, &item.DinilaiAt, &item.DijawabAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return err
		}
		if len(opsi) > 0 {
			item.SoalOpsi = json.RawMessage(opsi)
		}
		if len(jawaban) > 0 {
			item.Jawaban = json.RawMessage(jawaban)
		}
		session.Answers = append(session.Answers, item)
	}
	return rows.Err()
}

func remainingSeconds(expiresAt time.Time) int {
	remaining := int(math.Floor(expiresAt.Sub(time.Now()).Seconds()))
	if remaining < 0 {
		return 0
	}
	return remaining
}

func rawJSONToAny(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

func sanitizedOptions(raw json.RawMessage) any {
	options := exam.ParseOptions(rawJSONToAny(raw))
	if len(options) == 0 {
		return nil
	}
	result := make([]map[string]string, 0, len(options))
	for _, option := range options {
		item := map[string]string{"label": option.Label, "teks": option.Teks}
		if option.GambarURL != "" {
			item["gambar_url"] = option.GambarURL
		}
		if option.GambarAlt != "" {
			item["gambar_alt"] = option.GambarAlt
		}
		result = append(result, item)
	}
	return result
}

func (s *Server) buildStartPayload(session Session, resumed bool, warnings []string) map[string]any {
	now := time.Now()
	questions := make([]map[string]any, 0, len(session.Answers))
	for _, answer := range session.Answers {
		questions = append(questions, map[string]any{
			"soal_id":         answer.SoalID,
			"urutan":          answer.Urutan,
			"soal_kategori":   answer.SoalKategori,
			"soal_kesulitan":  answer.SoalKesulitan,
			"soal_tipe":       answer.SoalTipe,
			"soal_pertanyaan": answer.SoalPertanyaan,
			"soal_gambar_url": answer.SoalGambarURL,
			"soal_gambar_alt": answer.SoalGambarAlt,
			"soal_opsi":       sanitizedOptions(answer.SoalOpsi),
			"jawaban":         rawJSONToAny(answer.Jawaban),
			"is_ragu":         answer.IsRagu,
			"dijawab_at":      answer.DijawabAt,
		})
	}
	if warnings == nil {
		warnings = []string{}
	}
	introMode := session.Paket.IntroKategoriMode
	if introMode != "first" && introMode != "never" {
		introMode = "always"
	}
	focusMax := session.Paket.FocusLockMaxViolations
	if focusMax < 1 {
		focusMax = 1
	}
	if focusMax > 20 {
		focusMax = 20
	}
	return map[string]any{
		"session": map[string]any{
			"session_id":                session.SessionID,
			"paket_id":                  session.PaketID,
			"paket_nama":                session.Paket.PaketNama,
			"attempt_number":            session.AttemptNumber,
			"status":                    session.Status,
			"started_at":                session.StartedAt,
			"expires_at":                session.ExpiresAt,
			"server_time":               now,
			"remaining_seconds":         remainingSeconds(session.ExpiresAt),
			"duration_minutes":          session.Paket.DurationMinutes,
			"allow_backtrack":           session.Paket.AllowBacktrack,
			"is_pembahasan_visible":     session.Paket.IsPembahasanVisible,
			"passing_mode":              exam.NormalizePassingMode(session.Paket.PassingMode),
			"passing_grade":             session.Paket.PassingGrade,
			"skala_nilai":               exam.NormalizeFinalScoreMode(session.Paket.SkalaNilai),
			"intro_kategori_mode":       introMode,
			"focus_lock_enabled":        session.Paket.FocusLockEnabled,
			"focus_lock_max_violations": focusMax,
			"total_questions":           len(session.Answers),
		},
		"questions": questions,
		"resumed":   resumed,
		"warnings":  warnings,
	}
}

func (s *Server) buildResultSummary(session Session) map[string]any {
	var breakdown map[string]any
	if len(session.ScoreBreakdown) > 0 {
		_ = json.Unmarshal(session.ScoreBreakdown, &breakdown)
	}
	passingMode := session.Paket.PassingMode
	passingGrade := session.Paket.PassingGrade
	skalaNilai := session.Paket.SkalaNilai
	var thresholds any = rawJSONToAny(session.Paket.PaketPassingKategori)
	failed := []string{}
	pending := 0
	isFinal := session.Status == "completed"
	if breakdown != nil {
		if value, ok := breakdown["passing_mode"].(string); ok {
			passingMode = value
		}
		if value, ok := breakdown["passing_grade"].(float64); ok {
			passingGrade = value
		}
		if value, ok := breakdown["skala_nilai"].(string); ok {
			skalaNilai = value
		}
		if value, exists := breakdown["category_thresholds"]; exists {
			thresholds = value
		}
		if values, ok := breakdown["failed_categories"].([]any); ok {
			for _, value := range values {
				if text, ok := value.(string); ok {
					failed = append(failed, text)
				}
			}
		}
		if value, ok := breakdown["pending_essay"].(float64); ok {
			pending = int(value)
		}
		if value, ok := breakdown["is_final"].(bool); ok {
			isFinal = value
		}
	}
	return map[string]any{
		"session_id":          session.SessionID,
		"paket_id":            session.PaketID,
		"paket_nama":          session.Paket.PaketNama,
		"attempt_number":      session.AttemptNumber,
		"status":              session.Status,
		"final_score":         session.FinalScore,
		"is_passed":           session.IsPassed,
		"passing_mode":        exam.NormalizePassingMode(passingMode),
		"passing_grade":       passingGrade,
		"skala_nilai":         exam.NormalizeFinalScoreMode(skalaNilai),
		"category_thresholds": thresholds,
		"failed_categories":   failed,
		"is_final":            isFinal,
		"pending_essay":       pending,
		"score_breakdown":     breakdown,
		"started_at":          session.StartedAt,
		"submitted_at":        session.SubmittedAt,
		"expires_at":          session.ExpiresAt,
		"auto_submitted":      session.AutoSubmitted,
		"disqualify_alasan":   session.DisqualifyAlasan,
	}
}

func (s *Server) registerExams(api chi.Router) {
	api.Route("/exams", func(r chi.Router) {
		r.Use(s.Auth.Authenticate)
		r.With(middleware.RequirePermission(s.Permissions, "exam.take")).Post("/start", httpx.Wrap(s.startExam))
		r.With(middleware.RequirePermission(s.Permissions, "exam.take")).Get("/my-attempts", httpx.Wrap(s.myAttempts))
		r.With(middleware.RequirePermission(s.Permissions, "admin.essays.view")).Get("/grading-queue", httpx.Wrap(s.gradingQueue))
		r.With(middleware.RequirePermission(s.Permissions, "exam.take")).Get("/{id}", httpx.Wrap(s.getExamSession))
		r.With(middleware.RequirePermission(s.Permissions, "exam.take")).Put("/{id}/answers", httpx.Wrap(s.saveExamAnswers))
		r.With(middleware.RequirePermission(s.Permissions, "exam.take")).Post("/{id}/submit", httpx.Wrap(s.submitExam))
		r.With(middleware.RequirePermission(s.Permissions, "exam.take")).Post("/{id}/disqualify", httpx.Wrap(s.disqualifyExam))
		r.With(middleware.RequirePermission(s.Permissions, "exam.take")).Get("/{id}/review", httpx.Wrap(s.reviewExam))
		r.With(middleware.RequirePermission(s.Permissions, "admin.essays.grade")).Post("/{id}/grade-essay", httpx.Wrap(s.gradeEssays))
	})
	api.With(s.Auth.Authenticate).Get("/packages/{paketId}/pembahasan", httpx.Wrap(s.packagePembahasan))
}

type AnswerInput struct {
	SoalID     int
	Jawaban    any
	HasJawaban bool
	IsRagu     *bool
}

func parseAnswerInputs(body map[string]any) ([]AnswerInput, error) {
	values := bodySlice(body, "answers")
	result := make([]AnswerInput, 0, len(values))
	seen := map[int]bool{}
	for _, raw := range values {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		soalID := bodyInt(item, "soal_id", 0)
		if soalID <= 0 {
			return nil, httpx.BadRequest("soal_id tidak valid", "VALIDATION_ERROR")
		}
		if seen[soalID] {
			return nil, httpx.BadRequest(fmt.Sprintf("soal_id %d dikirim lebih dari sekali.", soalID), "DUPLICATE_ANSWER")
		}
		seen[soalID] = true
		jawaban, hasJawaban := item["jawaban"]
		var ragu *bool
		if value, ok := item["is_ragu"].(bool); ok {
			ragu = &value
		}
		result = append(result, AnswerInput{SoalID: soalID, Jawaban: jawaban, HasJawaban: hasJawaban, IsRagu: ragu})
	}
	return result, nil
}

func (s *Server) startExam(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	paketID := bodyInt(body, "paket_id", 0)
	if paketID <= 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "paket_id", "message": "paket_id wajib diisi"}})
	}
	paket, err := s.loadExamPackage(r.Context(), paketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Paket ujian tidak ditemukan atau tidak aktif", "PACKAGE_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if !paket.IsActive {
		return httpx.NotFound("Paket ujian tidak ditemukan atau tidak aktif", "PACKAGE_NOT_FOUND")
	}
	if err := s.assertOrganizationActive(r.Context(), paket.OrganizationID); err != nil {
		return err
	}

	var existingID int
	var existingExpiry time.Time
	err = s.Pool.QueryRow(r.Context(), `
		SELECT session_id, expires_at FROM exam_session
		WHERE user_id = $1 AND paket_id = $2 AND status = 'ongoing'
		ORDER BY session_id DESC LIMIT 1`, principal.UserID, paketID).Scan(&existingID, &existingExpiry)
	if err == nil {
		if existingExpiry.After(time.Now()) {
			session, err := s.loadSession(r.Context(), existingID)
			if err != nil {
				return err
			}
			s.Activity.Record(r.Context(), principal.UserID, "EXAM_RESUME", auth.RequestIP(r), map[string]any{"session_id": existingID, "paket_id": paketID})
			httpx.Success(w, s.buildStartPayload(session, true, nil), "Melanjutkan sesi ujian yang sedang berjalan.")
			return nil
		}
		if _, err := s.finalizeSession(r.Context(), existingID, true, false); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	var accessID, sisaAttempt int
	err = s.Pool.QueryRow(r.Context(), `
		SELECT access_id, sisa_attempt FROM user_package_access
		WHERE user_id = $1 AND paket_id = $2 AND expired_at > now()
		ORDER BY access_id DESC LIMIT 1`, principal.UserID, paketID).Scan(&accessID, &sisaAttempt)
	if errors.Is(err, pgx.ErrNoRows) {
		if paket.OrganizationID != nil {
			return httpx.Forbidden("Masukkan kode akses kelas/ujian privat dari admin organisasi Anda.", "PACKAGE_ACCESS_CODE_REQUIRED")
		}
		if paket.PaketHarga > 0 {
			return httpx.Forbidden("Anda belum memiliki akses aktif ke paket ini. Silakan lakukan pembelian.", "PACKAGE_ACCESS_REQUIRED")
		}
		expiredAt := time.Now().Add(time.Duration(s.Config.FreeAccessDays) * 24 * time.Hour)
		err = s.Pool.QueryRow(r.Context(), `
			INSERT INTO user_package_access (user_id, paket_id, sisa_attempt, expired_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, now(), now())
			ON CONFLICT (user_id, paket_id) DO UPDATE
			SET sisa_attempt = EXCLUDED.sisa_attempt, expired_at = EXCLUDED.expired_at, updated_at = now()
			RETURNING access_id, sisa_attempt`, principal.UserID, paketID, paket.MaxAttempts, expiredAt).Scan(&accessID, &sisaAttempt)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if paket.MaxAttempts > 0 && sisaAttempt <= 0 {
		return httpx.Forbidden("Kuota percobaan untuk paket ini sudah habis.", "NO_ATTEMPTS_LEFT")
	}
	if paket.PaketTipe == "elearning" {
		if err := s.assertPostTestUnlocked(r.Context(), principal.UserID, paketID); err != nil {
			return err
		}
	}

	questions, warnings, err := s.selectExamQuestions(r.Context(), paket)
	if err != nil {
		return err
	}
	if len(questions) == 0 {
		return httpx.Unprocessable("Bank soal paket ini kosong.", "EMPTY_QUESTION_BANK", nil)
	}
	if err := validateScoringForQuestions(paket, questions); err != nil {
		return err
	}
	scoringConfig := buildScoringSnapshot(paket)
	configJSON, _ := json.Marshal(scoringConfig)

	sessionID := 0
	err = pgx.BeginTxFunc(r.Context(), s.Pool, pgx.TxOptions{IsoLevel: pgx.Serializable}, func(tx pgx.Tx) error {
		var ongoingID int
		if err := tx.QueryRow(r.Context(), `
			SELECT session_id FROM exam_session WHERE user_id = $1 AND paket_id = $2 AND status = 'ongoing' LIMIT 1`,
			principal.UserID, paketID).Scan(&ongoingID); err == nil {
			return httpx.Conflict("Masih ada sesi ujian yang berjalan untuk paket ini.", "ONGOING_SESSION_EXISTS")
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if paket.MaxAttempts > 0 {
			tag, err := tx.Exec(r.Context(), `
				UPDATE user_package_access SET sisa_attempt = sisa_attempt - 1, updated_at = now()
				WHERE access_id = $1 AND sisa_attempt > 0 AND expired_at > now()`, accessID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return httpx.Forbidden("Kuota percobaan habis atau masa aktif paket telah berakhir.", "NO_ATTEMPTS_LEFT")
			}
		}
		var attemptCount int
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM exam_session WHERE user_id = $1 AND paket_id = $2`, principal.UserID, paketID).Scan(&attemptCount); err != nil {
			return err
		}
		startedAt := time.Now()
		expiresAt := startedAt.Add(time.Duration(paket.DurationMinutes) * time.Minute)
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO exam_session (user_id, paket_id, attempt_number, status, started_at, expires_at, scoring_config, updated_at)
			VALUES ($1, $2, $3, 'ongoing', $4, $5, $6::jsonb, now())
			RETURNING session_id`, principal.UserID, paketID, attemptCount+1, startedAt, expiresAt, string(configJSON),
		).Scan(&sessionID); err != nil {
			return err
		}
		for index, question := range questions {
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO exam_answer (session_id, soal_id, urutan, soal_kategori, soal_kesulitan, soal_tipe,
					soal_pertanyaan, soal_gambar_url, soal_gambar_alt, soal_opsi, skor_maks, created_at, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,now(),now())`,
				sessionID, question.SoalID, index+1, question.SoalKategori, question.SoalKesulitan,
				question.SoalTipe, question.SoalPertanyaan, question.SoalGambarURL, question.SoalGambarAlt,
				jsonArgument(question.SoalOpsi), question.SkorMaks); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	session, err := s.loadSession(r.Context(), sessionID)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "EXAM_START", auth.RequestIP(r), map[string]any{
		"session_id": sessionID, "paket_id": paketID, "attempt_number": session.AttemptNumber, "total_questions": len(session.Answers),
	})
	httpx.Created(w, s.buildStartPayload(session, false, warnings), "Sesi ujian berhasil dibuat.")
	return nil
}

type questionSnapshot struct {
	SoalID         int
	SoalKategori   string
	SoalKesulitan  string
	SoalTipe       string
	SoalPertanyaan string
	SoalGambarURL  *string
	SoalGambarAlt  *string
	SoalOpsi       json.RawMessage
	SkorMaks       float64
}

func (s *Server) selectExamQuestions(ctx context.Context, paket examPackage) ([]questionSnapshot, []string, error) {
	bankJenis := "tryout"
	if paket.JenisPaket == "ujian_real" {
		bankJenis = "ujian_real"
	}
	mode := "acak"
	if bankJenis != "ujian_real" && paket.ModeSoal == "tetap" {
		mode = "tetap"
	}

	if paket.SelectionMode == "manual" {
		rows, err := s.Pool.Query(ctx, `SELECT soal_id FROM paket_soal WHERE paket_id = $1 ORDER BY urutan ASC`, paket.PaketID)
		if err != nil {
			return nil, nil, err
		}
		ids := []int{}
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, nil, err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if len(ids) == 0 {
			return nil, nil, httpx.Unprocessable("Paket ini memakai mode pilih soal sendiri, tetapi belum ada soal yang dipilih.", "MANUAL_QUESTION_SELECTION_REQUIRED", nil)
		}
		questions, err := s.loadQuestionSnapshots(ctx, ids, paket.OrganizationID, false)
		if err != nil {
			return nil, nil, err
		}
		if len(questions) != len(ids) {
			return nil, nil, httpx.Unprocessable("Sebagian soal pilihan sudah tidak tersedia. Hubungi admin untuk memperbarui pilihan soal.", "MANUAL_QUESTION_NOT_FOUND", nil)
		}
		return questions, nil, nil
	}

	if paket.PaketTipe == "elearning" {
		ids, err := s.questionIDs(ctx, `paket_id = $1 AND soal_aktif = true`, paket.PaketID)
		if err != nil {
			return nil, nil, err
		}
		if len(ids) > 0 {
			questions, err := s.loadQuestionSnapshots(ctx, ids, paket.OrganizationID, true)
			if err != nil {
				return nil, nil, err
			}
			metas := toQuestionMeta(questions)
			selection := exam.SelectQuestions(metas, rawJSONToAny(paket.PaketKomposisi), paket.PaketJumlahSoal, mode, fmt.Sprint(paket.PaketID))
			if len(selection.IDs) == 0 {
				return nil, nil, httpx.Unprocessable("Tidak ada soal yang dapat dipilih dari bank soal.", "EMPTY_QUESTION_BANK", nil)
			}
			byID := map[int]questionSnapshot{}
			for _, question := range questions {
				byID[question.SoalID] = question
			}
			ordered := []questionSnapshot{}
			for _, id := range selection.IDs {
				ordered = append(ordered, byID[id])
			}
			return ordered, selection.Warnings, nil
		}
	}

	conditions := []string{"s.soal_aktif = true", "s.bank_jenis = $1", "s.bank_tipe = $2"}
	args := []any{bankJenis, strings.ToLower(paket.PaketTipe)}
	index := 3
	if paket.OrganizationID != nil {
		conditions = append(conditions, fmt.Sprintf("s.organization_id = $%d", index))
		args = append(args, *paket.OrganizationID)
		index++
	} else if paket.BankBatch != nil {
		conditions = append(conditions, fmt.Sprintf("s.bank_batch = $%d", index))
		args = append(args, strings.ToLower(*paket.BankBatch))
		index++
	}
	where := strings.Join(conditions, " AND ")
	metaRows, err := s.Pool.Query(ctx, `SELECT s.soal_id, s.soal_kategori, s.soal_kesulitan FROM soal_bank s WHERE `+where, args...)
	if err != nil {
		return nil, nil, err
	}
	metas := []exam.QuestionMeta{}
	for metaRows.Next() {
		var meta exam.QuestionMeta
		if err := metaRows.Scan(&meta.SoalID, &meta.SoalKategori, &meta.SoalKesulitan); err != nil {
			metaRows.Close()
			return nil, nil, err
		}
		metas = append(metas, meta)
	}
	metaRows.Close()
	if len(metas) == 0 {
		return nil, nil, httpx.Unprocessable(fmt.Sprintf("Belum ada soal aktif di bank %s untuk tipe \"%s\".", bankJenis, paket.PaketTipe), "EMPTY_QUESTION_BANK", nil)
	}
	selection := exam.SelectQuestions(metas, rawJSONToAny(paket.PaketKomposisi), paket.PaketJumlahSoal, mode, fmt.Sprint(paket.PaketID))
	if len(selection.IDs) == 0 {
		return nil, nil, httpx.Unprocessable("Tidak ada soal yang dapat dipilih dari bank soal.", "EMPTY_QUESTION_BANK", nil)
	}
	questions, err := s.loadQuestionSnapshots(ctx, selection.IDs, paket.OrganizationID, false)
	if err != nil {
		return nil, nil, err
	}
	if len(questions) != len(selection.IDs) {
		return nil, nil, httpx.Unprocessable("Sebagian soal tidak lagi tersedia. Silakan coba lagi.", "QUESTION_SELECTION_FAILED", nil)
	}
	return questions, selection.Warnings, nil
}

func (s *Server) questionIDs(ctx context.Context, where string, args ...any) ([]int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT soal_id FROM soal_bank WHERE `+where+` ORDER BY soal_id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Server) loadQuestionSnapshots(ctx context.Context, ids []int, organizationID *int, preserveOrder bool) ([]questionSnapshot, error) {
	if len(ids) == 0 {
		return []questionSnapshot{}, nil
	}
	args := []any{ids}
	query := `SELECT soal_id, soal_kategori, soal_kesulitan, soal_tipe, soal_pertanyaan,
		soal_gambar_url, soal_gambar_alt, soal_opsi, soal_skor_maks
		FROM soal_bank WHERE soal_id = ANY($1) AND soal_aktif = true`
	if organizationID != nil {
		query += ` AND organization_id = $2`
		args = append(args, *organizationID)
	}
	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[int]questionSnapshot{}
	for rows.Next() {
		var item questionSnapshot
		var opsi []byte
		if err := rows.Scan(&item.SoalID, &item.SoalKategori, &item.SoalKesulitan, &item.SoalTipe,
			&item.SoalPertanyaan, &item.SoalGambarURL, &item.SoalGambarAlt, &opsi, &item.SkorMaks); err != nil {
			return nil, err
		}
		if len(opsi) > 0 {
			item.SoalOpsi = json.RawMessage(opsi)
		}
		byID[item.SoalID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]questionSnapshot, 0, len(ids))
	for _, id := range ids {
		if item, ok := byID[id]; ok {
			result = append(result, item)
		}
	}
	return result, nil
}

func toQuestionMeta(questions []questionSnapshot) []exam.QuestionMeta {
	result := make([]exam.QuestionMeta, 0, len(questions))
	for _, question := range questions {
		result = append(result, exam.QuestionMeta{SoalID: question.SoalID, SoalKategori: question.SoalKategori, SoalKesulitan: question.SoalKesulitan})
	}
	return result
}

func buildScoringSnapshot(paket examPackage) map[string]any {
	return map[string]any{
		"passing_mode":        exam.NormalizePassingMode(paket.PassingMode),
		"passing_grade":       paket.PassingGrade,
		"weights":             exam.ParseWeights(rawJSONToAny(paket.PaketBobotKategori)),
		"category_thresholds": exam.ParseThresholds(rawJSONToAny(paket.PaketPassingKategori)),
		"final_score_mode":    exam.NormalizeFinalScoreMode(paket.SkalaNilai),
	}
}

func validateScoringForQuestions(paket examPackage, questions []questionSnapshot) error {
	passingGranularityMax := 100.0
	if exam.NormalizeFinalScoreMode(paket.SkalaNilai) == "poin_mentah" {
		passingGranularityMax = 9999
	}
	if paket.PassingGrade < 0 || paket.PassingGrade > passingGranularityMax {
		return httpx.Unprocessable(fmt.Sprintf("Passing grade keseluruhan harus berada di antara 0 dan %.0f.", passingGranularityMax), "PASSING_GRADE_INVALID", nil)
	}
	if exam.NormalizePassingMode(paket.PassingMode) != "per_category" {
		return nil
	}
	thresholds := exam.ParseThresholds(rawJSONToAny(paket.PaketPassingKategori))
	if len(thresholds) == 0 {
		return httpx.Unprocessable("Mode per kategori memerlukan minimal satu ambang poin kategori.", "PASSING_CATEGORY_REQUIRED", nil)
	}
	maxByCategory := map[string]float64{}
	categories := []string{}
	for _, question := range questions {
		category := question.SoalKategori
		if _, exists := maxByCategory[category]; !exists {
			categories = append(categories, category)
		}
		maxByCategory[category] += exam.QuestionMaxScore(question.SoalTipe, rawJSONToAny(question.SoalOpsi), question.SkorMaks)
	}
	for _, category := range categories {
		threshold, exists := thresholds[category]
		if !exists {
			return httpx.Unprocessable("Ambang kategori belum diisi untuk: "+category+".", "PASSING_CATEGORY_INCOMPLETE", nil)
		}
		if threshold > maxByCategory[category] {
			return httpx.Unprocessable("Ambang kategori melebihi poin maksimum yang tersedia. Periksa skor maksimum soal atau ambang.", "PASSING_THRESHOLD_EXCEEDS_MAX", nil)
		}
	}
	return nil
}

func (s *Server) assertPostTestUnlocked(ctx context.Context, userID, paketID int) error {
	var required, completed int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM lms_materi WHERE paket_id = $1 AND is_active = true AND is_wajib = true`, paketID).Scan(&required); err != nil {
		return err
	}
	if required == 0 {
		return httpx.Unprocessable("Kelas ini belum memiliki materi wajib. Hubungi admin untuk melengkapi materi.", "LMS_MATERIALS_EMPTY", nil)
	}
	if err := s.Pool.QueryRow(ctx, `
		SELECT count(*) FROM lms_progress p JOIN lms_materi m ON m.materi_id = p.materi_id
		WHERE p.user_id = $1 AND m.paket_id = $2 AND m.is_active = true AND m.is_wajib = true`, userID, paketID).Scan(&completed); err != nil {
		return err
	}
	if completed < required {
		return httpx.Forbidden("Selesaikan seluruh materi wajib sebelum mengerjakan post-test.", "LMS_MATERIALS_INCOMPLETE")
	}
	return nil
}

func (s *Server) getExamSession(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	session, err := s.loadSession(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Sesi ujian tidak ditemukan", "SESSION_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	canViewOther, _ := s.Permissions.HasPermission(r.Context(), principal.UserRole, "admin.essays.view")
	if !canViewOther && session.UserID != principal.UserID {
		return httpx.Forbidden("Anda tidak memiliki akses ke sesi ujian ini.", "SESSION_FORBIDDEN")
	}
	if session.Status == "ongoing" && !session.ExpiresAt.After(time.Now()) {
		if _, err := s.finalizeSession(r.Context(), id, true, false); err != nil {
			return err
		}
		session, err = s.loadSession(r.Context(), id)
		if err != nil {
			return err
		}
	}
	if session.Status == "ongoing" {
		payload := s.buildStartPayload(session, true, nil)
		payload["review"] = nil
		httpx.Success(w, payload, "")
		return nil
	}
	httpx.Success(w, map[string]any{
		"session": map[string]any{
			"session_id": session.SessionID, "paket_id": session.PaketID, "paket_nama": session.Paket.PaketNama,
			"attempt_number": session.AttemptNumber, "status": session.Status, "started_at": session.StartedAt,
			"expires_at": session.ExpiresAt, "server_time": time.Now(), "remaining_seconds": 0,
		},
		"questions": []any{}, "resumed": false, "warnings": []string{},
		"review": s.buildResultSummary(session),
	}, "")
	return nil
}

func (s *Server) saveExamAnswers(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	inputs, err := parseAnswerInputs(body)
	if err != nil {
		return err
	}
	if len(inputs) == 0 || len(inputs) > 500 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "answers", "message": "Minimal 1 dan maksimal 500 jawaban"}})
	}
	session, err := s.loadSession(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Sesi ujian tidak ditemukan", "SESSION_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if session.UserID != principal.UserID {
		return httpx.Forbidden("Anda tidak memiliki akses ke sesi ujian ini.", "SESSION_FORBIDDEN")
	}
	if session.Status != "ongoing" {
		return httpx.Conflict(fmt.Sprintf("Sesi ujian berstatus %s dan tidak dapat diubah.", session.Status), "SESSION_NOT_ONGOING")
	}
	if !session.ExpiresAt.After(time.Now()) {
		if _, err := s.finalizeSession(r.Context(), id, true, false); err != nil {
			return err
		}
		return httpx.NewError(http.StatusConflict, "Waktu ujian telah berakhir. Jawaban terakhir telah dikumpulkan otomatis.", "SESSION_EXPIRED", map[string]any{"session_id": id})
	}
	existing := map[int]examAnswer{}
	for _, answer := range session.Answers {
		existing[answer.SoalID] = answer
	}
	unknown := []int{}
	type update struct {
		soalID   int
		jawaban  any
		isRagu   bool
		answered bool
	}
	updates := []update{}
	for _, input := range inputs {
		answer, exists := existing[input.SoalID]
		if !exists {
			unknown = append(unknown, input.SoalID)
			continue
		}
		jawaban := input.Jawaban
		has := input.HasJawaban
		if !has {
			jawaban = rawJSONToAny(answer.Jawaban)
			has = answer.Jawaban != nil
		}
		ragu := answer.IsRagu
		if input.IsRagu != nil {
			ragu = *input.IsRagu
		}
		updates = append(updates, update{input.SoalID, jawaban, ragu, has})
	}
	if len(unknown) > 0 {
		return httpx.BadRequest("Beberapa soal tidak termasuk dalam sesi ujian ini.", "QUESTION_NOT_IN_SESSION")
	}
	err = pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
		for _, item := range updates {
			var answeredAt any
			if item.answered && item.jawaban != nil {
				answeredAt = time.Now()
			}
			if _, err := tx.Exec(r.Context(), `
				UPDATE exam_answer SET jawaban = $1::jsonb, is_ragu = $2, dijawab_at = $3, updated_at = now()
				WHERE session_id = $4 AND soal_id = $5`,
				jsonArgument(item.jawaban), item.isRagu, answeredAt, id, item.soalID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	session, err = s.loadSession(r.Context(), id)
	if err != nil {
		return err
	}
	answered := 0
	for _, answer := range session.Answers {
		if answer.Jawaban != nil {
			answered++
		}
	}
	httpx.Success(w, map[string]any{
		"saved": len(updates), "answered_count": answered, "total_questions": len(session.Answers),
		"remaining_seconds": remainingSeconds(session.ExpiresAt), "server_time": time.Now(),
	}, "Jawaban berhasil disimpan.")
	return nil
}

func (s *Server) submitExam(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	inputs, err := parseAnswerInputs(body)
	if err != nil {
		return err
	}
	autoSubmit := bodyBool(body, "auto_submit", false)
	session, err := s.loadSession(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Sesi ujian tidak ditemukan", "SESSION_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if session.UserID != principal.UserID {
		return httpx.Forbidden("Anda tidak memiliki akses ke sesi ujian ini.", "SESSION_FORBIDDEN")
	}
	if session.Status == "disqualified" {
		return httpx.Conflict("Sesi ujian telah didiskualifikasi.", "SESSION_DISQUALIFIED")
	}
	if session.Status != "ongoing" {
		httpx.Success(w, mergeMap(s.buildResultSummary(session), map[string]any{"already_submitted": true}), "Ujian sudah pernah dikumpulkan.")
		return nil
	}
	if len(inputs) > 0 {
		existing := map[int]examAnswer{}
		for _, answer := range session.Answers {
			existing[answer.SoalID] = answer
		}
		err = pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
			for _, input := range inputs {
				answer, exists := existing[input.SoalID]
				if !exists {
					continue
				}
				jawaban := input.Jawaban
				if !input.HasJawaban {
					jawaban = rawJSONToAny(answer.Jawaban)
				}
				ragu := answer.IsRagu
				if input.IsRagu != nil {
					ragu = *input.IsRagu
				}
				if _, err := tx.Exec(r.Context(), `
					UPDATE exam_answer SET jawaban = $1::jsonb, is_ragu = $2,
						dijawab_at = CASE WHEN $1::jsonb IS NULL THEN NULL ELSE now() END, updated_at = now()
					WHERE session_id = $3 AND soal_id = $4`,
					jsonArgument(jawaban), ragu, id, input.SoalID); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	isAuto := autoSubmit || !session.ExpiresAt.After(time.Now())
	finalized, err := s.finalizeSession(r.Context(), id, isAuto, false)
	if err != nil {
		return err
	}
	fresh, err := s.loadSession(r.Context(), id)
	if err != nil {
		return err
	}
	message := "Ujian selesai dan telah dinilai."
	if finalized.AlreadyFinalized {
		message = "Ujian sudah pernah dikumpulkan."
	} else if fresh.Status == "submitted" {
		message = "Ujian dikumpulkan. Menunggu penilaian esai oleh asesor."
	}
	s.Activity.Record(r.Context(), principal.UserID, map[bool]string{true: "EXAM_AUTO_SUBMIT", false: "EXAM_SUBMIT"}[isAuto], auth.RequestIP(r), map[string]any{
		"session_id": id, "paket_id": fresh.PaketID, "final_score": fresh.FinalScore,
	})
	httpx.Success(w, mergeMap(s.buildResultSummary(fresh), map[string]any{"already_submitted": finalized.AlreadyFinalized}), message)
	return nil
}

func (s *Server) disqualifyExam(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	alasan := bodyString(body, "alasan")
	if len(alasan) < 3 || len(alasan) > 500 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "alasan", "message": "Alasan wajib 3-500 karakter"}})
	}
	session, err := s.loadSession(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Sesi ujian tidak ditemukan", "SESSION_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if session.UserID != principal.UserID {
		return httpx.Forbidden("Anda tidak memiliki akses ke sesi ujian ini.", "SESSION_FORBIDDEN")
	}
	if session.Status != "ongoing" {
		return httpx.Conflict(fmt.Sprintf("Sesi ujian berstatus %s dan tidak dapat diubah.", session.Status), "SESSION_NOT_ONGOING")
	}
	tag, err := s.Pool.Exec(r.Context(), `
		UPDATE exam_session SET status = 'disqualified', final_score = 0, is_passed = false,
			disqualify_alasan = $1, submitted_at = now(), updated_at = now()
		WHERE session_id = $2 AND status = 'ongoing'`, alasan, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return httpx.Conflict("Sesi ujian sudah selesai sebelum diskualifikasi.", "SESSION_NOT_ONGOING")
	}
	session, err = s.loadSession(r.Context(), id)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "EXAM_DISQUALIFIED", auth.RequestIP(r), map[string]any{"session_id": id, "paket_id": session.PaketID, "alasan": alasan})
	httpx.Success(w, s.buildResultSummary(session), "Sesi ujian didiskualifikasi.")
	return nil
}

type FinalizeOutcome struct {
	AlreadyFinalized bool
	Session          Session
}

func (s *Server) finalizeSession(ctx context.Context, sessionID int, autoSubmitted bool, preserveSubmittedAt bool) (FinalizeOutcome, error) {
	session, err := s.loadSession(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return FinalizeOutcome{}, httpx.NotFound("Sesi ujian tidak ditemukan", "SESSION_NOT_FOUND")
	}
	if err != nil {
		return FinalizeOutcome{}, err
	}
	if session.Status == "disqualified" {
		return FinalizeOutcome{}, httpx.Conflict("Sesi ujian telah didiskualifikasi dan tidak dapat dinilai.", "SESSION_DISQUALIFIED")
	}
	if session.Status != "ongoing" && !preserveSubmittedAt {
		return FinalizeOutcome{AlreadyFinalized: true, Session: session}, nil
	}

	snapshot := map[string]any{}
	if len(session.ScoringConfig) > 0 {
		_ = json.Unmarshal(session.ScoringConfig, &snapshot)
	}
	weights := any(rawJSONToAny(session.Paket.PaketBobotKategori))
	thresholds := any(rawJSONToAny(session.Paket.PaketPassingKategori))
	passingMode := session.Paket.PassingMode
	passingGrade := session.Paket.PassingGrade
	skala := session.Paket.SkalaNilai
	if value, exists := snapshot["weights"]; exists {
		weights = value
	}
	if value, exists := snapshot["category_thresholds"]; exists {
		thresholds = value
	}
	if value, ok := snapshot["passing_mode"].(string); ok {
		passingMode = value
	}
	if value, ok := snapshot["passing_grade"].(float64); ok {
		passingGrade = value
	}
	if value, ok := snapshot["final_score_mode"].(string); ok {
		skala = value
	}

	answers := make([]exam.AnswerForScoring, 0, len(session.Answers))
	for _, answer := range session.Answers {
		answers = append(answers, exam.AnswerForScoring{
			SoalKategori: answer.SoalKategori, SoalTipe: answer.SoalTipe,
			SoalOpsi: rawJSONToAny(answer.SoalOpsi), SkorMaks: answer.SkorMaks,
			Jawaban: rawJSONToAny(answer.Jawaban), SkorDidapat: answer.SkorDidapat,
		})
	}
	result := exam.CalculateFinalScore(exam.CalculateInput{
		Answers: answers, Weights: weights, PassingMode: passingMode,
		PassingGrade: passingGrade, CategoryThresholds: thresholds, FinalScoreMode: skala,
	})
	status := "completed"
	if result.PendingEssay > 0 {
		status = "submitted"
	}
	var certificateJP *int
	if status == "completed" && result.IsPassed {
		value := session.Paket.PaketJP
		certificateJP = &value
	}
	breakdownJSON, _ := json.Marshal(result.Breakdown)

	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for _, answer := range session.Answers {
			if answer.SoalTipe == "essay" && answer.SkorDidapat == nil {
				continue
			}
			questionResult := exam.ScoreQuestion(exam.AnswerForScoring{
				SoalKategori: answer.SoalKategori, SoalTipe: answer.SoalTipe,
				SoalOpsi: rawJSONToAny(answer.SoalOpsi), SkorMaks: answer.SkorMaks,
				Jawaban: rawJSONToAny(answer.Jawaban), SkorDidapat: answer.SkorDidapat,
			})
			if _, err := tx.Exec(ctx, `
				UPDATE exam_answer SET skor_didapat = $1, is_benar = $2, updated_at = now()
				WHERE jawaban_id = $3`, questionResult.Earned, questionResult.IsCorrect, answer.JawabanID); err != nil {
				return err
			}
		}
		if preserveSubmittedAt {
			_, err := tx.Exec(ctx, `
				UPDATE exam_session SET final_score = $1, is_passed = $2, score_breakdown = $3::jsonb,
					status = $4, certificate_jp = $5, updated_at = now()
				WHERE session_id = $6`,
				result.FinalScore, result.IsPassed, string(breakdownJSON), status, certificateJP, sessionID)
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE exam_session SET final_score = $1, is_passed = $2, score_breakdown = $3::jsonb,
				status = $4, submitted_at = now(), auto_submitted = $5, certificate_jp = $6, updated_at = now()
			WHERE session_id = $7 AND status = 'ongoing'`,
			result.FinalScore, result.IsPassed, string(breakdownJSON), status, autoSubmitted, certificateJP, sessionID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errFinalizeRace
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errFinalizeRace) {
			fresh, loadErr := s.loadSession(ctx, sessionID)
			if loadErr != nil {
				return FinalizeOutcome{}, loadErr
			}
			return FinalizeOutcome{AlreadyFinalized: true, Session: fresh}, nil
		}
		return FinalizeOutcome{}, err
	}
	if status == "completed" && result.IsPassed {
		_ = s.issueCertificateCode(ctx, sessionID)
	}
	fresh, err := s.loadSession(ctx, sessionID)
	if err != nil {
		return FinalizeOutcome{}, err
	}
	return FinalizeOutcome{AlreadyFinalized: false, Session: fresh}, nil
}

var errFinalizeRace = errors.New("finalize race")

func (s *Server) issueCertificateCode(ctx context.Context, sessionID int) error {
	for retry := 0; retry < 5; retry++ {
		candidate := codegen.CertificateCode()
		tag, err := s.Pool.Exec(ctx, `
			UPDATE exam_session SET certificate_code = $1, updated_at = now()
			WHERE session_id = $2 AND certificate_code IS NULL`, candidate, sessionID)
		if err != nil {
			continue
		}
		if tag.RowsAffected() == 1 {
			return nil
		}
		return nil
	}
	return nil
}

func (s *Server) gradeEssays(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	session, err := s.loadSession(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Sesi ujian tidak ditemukan", "SESSION_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if session.Status == "ongoing" {
		return httpx.Conflict("Ujian masih berlangsung dan belum dapat dinilai.", "SESSION_NOT_SUBMITTED")
	}
	if session.Status == "disqualified" {
		return httpx.Conflict("Sesi didiskualifikasi.", "SESSION_DISQUALIFIED")
	}
	essayAnswers := map[int]examAnswer{}
	for _, answer := range session.Answers {
		if answer.SoalTipe == "essay" {
			essayAnswers[answer.SoalID] = answer
		}
	}
	if len(essayAnswers) == 0 {
		return httpx.BadRequest("Sesi ini tidak memiliki soal esai.", "NO_ESSAY_ANSWERS")
	}
	rawGrades := bodySlice(body, "grades")
	if len(rawGrades) == 0 || len(rawGrades) > 200 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "grades", "message": "Minimal 1 dan maksimal 200 nilai"}})
	}
	seen := map[int]bool{}
	for _, raw := range rawGrades {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		soalID := bodyInt(item, "soal_id", 0)
		answer, exists := essayAnswers[soalID]
		if !exists {
			return httpx.BadRequest(fmt.Sprintf("soal_id %d bukan soal esai pada sesi ini.", soalID), "INVALID_ESSAY_QUESTION")
		}
		if seen[soalID] {
			return httpx.BadRequest(fmt.Sprintf("soal_id %d dinilai lebih dari sekali.", soalID), "DUPLICATE_GRADE")
		}
		seen[soalID] = true
		score := bodyFloat(item, "skor_didapat", -1)
		if score < 0 {
			return httpx.BadRequest("skor_didapat tidak valid", "VALIDATION_ERROR")
		}
		if score > answer.SkorMaks {
			return httpx.Unprocessable(fmt.Sprintf("Skor untuk soal %d melebihi skor maksimum (%.2f).", soalID, answer.SkorMaks), "SCORE_EXCEEDS_MAX", nil)
		}
	}
	for _, raw := range rawGrades {
		item, _ := raw.(map[string]any)
		soalID := bodyInt(item, "soal_id", 0)
		score := bodyFloat(item, "skor_didapat", 0)
		if _, err := s.Pool.Exec(r.Context(), `
			UPDATE exam_answer SET skor_didapat = $1, is_benar = NULL, dinilai_oleh = $2,
				dinilai_at = now(), updated_at = now()
			WHERE session_id = $3 AND soal_id = $4`, score, principal.UserID, id, soalID); err != nil {
			return err
		}
	}
	outcome, err := s.finalizeSession(r.Context(), id, false, true)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ESSAY_GRADED", auth.RequestIP(r), map[string]any{
		"session_id": id, "paket_id": session.PaketID, "graded_questions": len(rawGrades), "pending_essay": outcome.Session.Answers,
	})
	httpx.Success(w, mergeMap(s.buildResultSummary(outcome.Session), map[string]any{
		"graded": len(rawGrades), "pending_essay": pendingEssayCount(outcome.Session),
	}), "")
	return nil
}

func pendingEssayCount(session Session) int {
	count := 0
	for _, answer := range session.Answers {
		if answer.SoalTipe == "essay" && answer.SkorDidapat == nil {
			count++
		}
	}
	return count
}

func (s *Server) myAttempts(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"s.user_id = $1"}
	args := []any{principal.UserID}
	index := 2
	if paketID := httpx.QueryInt(r, "paket_id", 0); paketID > 0 {
		conditions = append(conditions, fmt.Sprintf("s.paket_id = $%d", index))
		args = append(args, paketID)
		index++
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		conditions = append(conditions, fmt.Sprintf("s.status = $%d", index))
		args = append(args, status)
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM exam_session s WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`
		SELECT s.session_id, s.user_id, s.paket_id, s.attempt_number, s.final_score, s.is_passed,
		       s.status, s.score_breakdown, s.auto_submitted, s.disqualify_alasan, s.started_at,
		       s.expires_at, s.submitted_at, s.updated_at, p.paket_id, p.paket_nama, p.paket_tipe,
		       p.paket_jp, p.passing_grade, p.duration_minutes, p.is_pembahasan_visible,
		       p.tanggal_buka_pembahasan, p.allow_backtrack,
		       (SELECT count(*) FROM exam_answer a WHERE a.session_id = s.session_id) AS answer_count
		FROM exam_session s JOIN paket_ujian p ON p.paket_id = s.paket_id
		WHERE %s ORDER BY s.started_at DESC LIMIT $%d OFFSET $%d`, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var sessionID, userID, paketID, attempt, answerCount int
		var finalScore *float64
		var isPassed *bool
		var status string
		var breakdown []byte
		var auto bool
		var alasan *string
		var started, expires, updated time.Time
		var submitted *time.Time
		var pID int
		var pNama, pTipe string
		var pJP, pDuration int
		var passing float64
		var visible, backtrack bool
		var openAt *time.Time
		if err := rows.Scan(&sessionID, &userID, &paketID, &attempt, &finalScore, &isPassed, &status,
			&breakdown, &auto, &alasan, &started, &expires, &submitted, &updated, &pID, &pNama, &pTipe,
			&pJP, &passing, &pDuration, &visible, &openAt, &backtrack, &answerCount); err != nil {
			return err
		}
		item := map[string]any{
			"session_id": sessionID, "user_id": userID, "paket_id": paketID, "attempt_number": attempt,
			"final_score": finalScore, "is_passed": isPassed, "status": status,
			"score_breakdown": rawJSONToAny(json.RawMessage(breakdown)), "auto_submitted": auto,
			"disqualify_alasan": alasan, "started_at": started, "expires_at": expires,
			"submitted_at": submitted, "updated_at": updated,
			"paket": map[string]any{
				"paket_id": pID, "paket_nama": pNama, "paket_tipe": pTipe, "paket_jp": pJP,
				"passing_grade": passing, "duration_minutes": pDuration, "is_pembahasan_visible": visible,
				"tanggal_buka_pembahasan": openAt, "allow_backtrack": backtrack,
			},
			"_count": map[string]int{"answers": answerCount},
		}
		items = append(items, item)
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return nil
}

func (s *Server) gradingQueue(w http.ResponseWriter, r *http.Request) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"s.status = 'submitted'", `EXISTS (SELECT 1 FROM exam_answer a WHERE a.session_id = s.session_id AND a.soal_tipe = 'essay' AND a.skor_didapat IS NULL)`}
	args := []any{}
	index := 1
	if paketID := httpx.QueryInt(r, "paket_id", 0); paketID > 0 {
		conditions = append(conditions, fmt.Sprintf("s.paket_id = $%d", index))
		args = append(args, paketID)
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM exam_session s WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`
		SELECT s.session_id, s.user_id, s.paket_id, s.attempt_number, s.final_score, s.is_passed,
		       s.status, s.submitted_at, s.started_at,
		       u.user_id, u.user_nama, u.user_email, p.paket_id, p.paket_nama
		FROM exam_session s
		JOIN "user" u ON u.user_id = s.user_id
		JOIN paket_ujian p ON p.paket_id = s.paket_id
		WHERE %s ORDER BY s.submitted_at ASC LIMIT $%d OFFSET $%d`, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var sessionID, userID, paketID, attempt, uID, pID int
		var finalScore *float64
		var isPassed *bool
		var status, uNama, uEmail, pNama string
		var submitted, started *time.Time
		if err := rows.Scan(&sessionID, &userID, &paketID, &attempt, &finalScore, &isPassed, &status,
			&submitted, &started, &uID, &uNama, &uEmail, &pID, &pNama); err != nil {
			return err
		}
		answerRows, err := s.Pool.Query(r.Context(), `
			SELECT jawaban_id, soal_id, urutan, soal_pertanyaan, soal_gambar_url, soal_gambar_alt,
			       jawaban, skor_maks, skor_didapat
			FROM exam_answer WHERE session_id = $1 AND soal_tipe = 'essay' ORDER BY urutan ASC`, sessionID)
		if err != nil {
			return err
		}
		answers := []map[string]any{}
		for answerRows.Next() {
			var jawabanID, soalID, urutan int
			var pertanyaan string
			var gambarURL, gambarAlt *string
			var jawaban []byte
			var maxScore float64
			var score *float64
			if err := answerRows.Scan(&jawabanID, &soalID, &urutan, &pertanyaan, &gambarURL, &gambarAlt,
				&jawaban, &maxScore, &score); err != nil {
				answerRows.Close()
				return err
			}
			answers = append(answers, map[string]any{
				"jawaban_id": jawabanID, "soal_id": soalID, "urutan": urutan,
				"soal_pertanyaan": pertanyaan, "soal_gambar_url": gambarURL, "soal_gambar_alt": gambarAlt,
				"jawaban": rawJSONToAny(json.RawMessage(jawaban)), "skor_maks": maxScore, "skor_didapat": score,
			})
		}
		answerRows.Close()
		items = append(items, map[string]any{
			"session_id": sessionID, "user_id": userID, "paket_id": paketID, "attempt_number": attempt,
			"final_score": finalScore, "is_passed": isPassed, "status": status,
			"submitted_at": submitted, "started_at": started,
			"user":    map[string]any{"user_id": uID, "user_nama": uNama, "user_email": uEmail},
			"paket":   map[string]any{"paket_id": pID, "paket_nama": pNama},
			"answers": answers,
		})
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return nil
}

func mergeMap(base, extra map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range extra {
		result[key] = value
	}
	return result
}
