package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/exam"
	"github.com/edumaster/backend-go/internal/httpx"
)

type pembahasanVisibility struct {
	Visible   bool       `json:"visible"`
	IsEnabled bool       `json:"is_enabled"`
	OpenAt    *time.Time `json:"open_at"`
}

func visibilityOf(paket examPackage) pembahasanVisibility {
	open := paket.TanggalBukaPembahasan == nil || !paket.TanggalBukaPembahasan.After(time.Now())
	return pembahasanVisibility{
		Visible:   paket.IsPembahasanVisible && open,
		IsEnabled: paket.IsPembahasanVisible,
		OpenAt:    paket.TanggalBukaPembahasan,
	}
}

func plainOptions(raw json.RawMessage) any {
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

func fullOptions(raw json.RawMessage) any {
	options := exam.ParseOptions(rawJSONToAny(raw))
	if len(options) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(options))
	for _, option := range options {
		item := map[string]any{"label": option.Label, "teks": option.Teks}
		if option.GambarURL != "" {
			item["gambar_url"] = option.GambarURL
		}
		if option.GambarAlt != "" {
			item["gambar_alt"] = option.GambarAlt
		}
		if option.IsCorrect {
			item["is_correct"] = true
		}
		if option.Skor != nil {
			item["skor"] = *option.Skor
		}
		result = append(result, item)
	}
	return result
}

func (s *Server) reviewExam(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	isStaff, _ := s.Permissions.HasPermission(r.Context(), principal.UserRole, "admin.essays.view")
	session, err := s.loadSession(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Sesi ujian tidak ditemukan", "SESSION_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if !isStaff && session.UserID != principal.UserID {
		return httpx.Forbidden("Anda tidak memiliki akses ke sesi ini.", "SESSION_FORBIDDEN")
	}
	if session.Status == "ongoing" {
		return httpx.Conflict("Selesaikan ujian terlebih dahulu sebelum membuka pembahasan.", "SESSION_STILL_ONGOING")
	}
	if session.Status == "disqualified" {
		return httpx.Forbidden("Sesi didiskualifikasi sehingga pembahasan tidak dapat dibuka.", "SESSION_DISQUALIFIED")
	}
	visibility := visibilityOf(session.Paket)
	canView := isStaff || visibility.Visible

	pembahasan := map[int]Pembahasan{}
	if canView && len(session.Answers) > 0 {
		ids := make([]int, 0, len(session.Answers))
		for _, answer := range session.Answers {
			ids = append(ids, answer.SoalID)
		}
		rows, err := s.Pool.Query(r.Context(), `
			SELECT pembahasan_id, soal_id, pembahasan_teks, pembahasan_video, created_at, updated_at
			FROM soal_pembahasan WHERE soal_id = ANY($1)`, ids)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item Pembahasan
			if err := rows.Scan(&item.PembahasanID, &item.SoalID, &item.PembahasanTeks,
				&item.PembahasanVideo, &item.CreatedAt, &item.UpdatedAt); err != nil {
				rows.Close()
				return err
			}
			pembahasan[item.SoalID] = item
		}
		rows.Close()
	}

	var breakdown map[string]any
	if len(session.ScoreBreakdown) > 0 {
		_ = json.Unmarshal(session.ScoreBreakdown, &breakdown)
	}
	passingMode := session.Paket.PassingMode
	skalaNilai := session.Paket.SkalaNilai
	pending := 0
	if breakdown != nil {
		if value, ok := breakdown["passing_mode"].(string); ok {
			passingMode = value
		}
		if value, ok := breakdown["skala_nilai"].(string); ok {
			skalaNilai = value
		}
		if value, ok := breakdown["pending_essay"].(float64); ok {
			pending = int(value)
		}
	}

	questions := make([]map[string]any, 0, len(session.Answers))
	for _, answer := range session.Answers {
		var explanation any
		if item, exists := pembahasan[answer.SoalID]; exists && canView {
			explanation = map[string]any{
				"pembahasan_teks":  item.PembahasanTeks,
				"pembahasan_video": item.PembahasanVideo,
			}
		}
		options := plainOptions(answer.SoalOpsi)
		if canView {
			options = fullOptions(answer.SoalOpsi)
		}
		questions = append(questions, map[string]any{
			"soal_id":         answer.SoalID,
			"urutan":          answer.Urutan,
			"soal_kategori":   answer.SoalKategori,
			"soal_kesulitan":  answer.SoalKesulitan,
			"soal_tipe":       answer.SoalTipe,
			"soal_pertanyaan": answer.SoalPertanyaan,
			"soal_gambar_url": answer.SoalGambarURL,
			"soal_gambar_alt": answer.SoalGambarAlt,
			"soal_opsi":       options,
			"jawaban":         rawJSONToAny(answer.Jawaban),
			"is_ragu":         answer.IsRagu,
			"is_benar":        answer.IsBenar,
			"skor_didapat":    answer.SkorDidapat,
			"skor_maks":       answer.SkorMaks,
			"dinilai_at":      answer.DinilaiAt,
			"pembahasan":      explanation,
		})
	}

	message := "Pembahasan tersedia."
	if !canView {
		message = "Pembahasan belum dapat dibuka untuk paket ini."
	}
	httpx.Success(w, map[string]any{
		"session": map[string]any{
			"session_id":      session.SessionID,
			"paket_id":        session.PaketID,
			"paket_nama":      session.Paket.PaketNama,
			"user_id":         session.UserID,
			"attempt_number":  session.AttemptNumber,
			"status":          session.Status,
			"final_score":     session.FinalScore,
			"is_passed":       session.IsPassed,
			"passing_grade":   session.Paket.PassingGrade,
			"passing_mode":    exam.NormalizePassingMode(passingMode),
			"skala_nilai":     exam.NormalizeFinalScoreMode(skalaNilai),
			"pending_essay":   pending,
			"score_breakdown": breakdown,
			"started_at":      session.StartedAt,
			"submitted_at":    session.SubmittedAt,
		},
		"pembahasan": map[string]any{
			"available": canView,
			"enabled":   visibility.IsEnabled,
			"open_at":   visibility.OpenAt,
			"message":   message,
		},
		"questions": questions,
	}, "")
	return nil
}

func (s *Server) hasCompletedExam(ctx context.Context, userID, paketID int) (bool, error) {
	var exists bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM exam_session
			WHERE user_id = $1 AND paket_id = $2 AND status IN ('submitted', 'completed')
		)`, userID, paketID).Scan(&exists)
	return exists, err
}

func (s *Server) completedQuestionIDs(ctx context.Context, userID, paketID int) ([]int, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT a.soal_id FROM exam_answer a
		JOIN exam_session s ON s.session_id = a.session_id
		WHERE s.user_id = $1 AND s.paket_id = $2 AND s.status IN ('submitted', 'completed')`, userID, paketID)
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

func (s *Server) packagePembahasan(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "paketId")
	if err != nil {
		return err
	}
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 100))
	paket, err := s.loadExamPackage(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Paket ujian tidak ditemukan", "PACKAGE_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	isStaff, _ := s.Permissions.HasPermission(r.Context(), principal.UserRole, "admin.packages.view")
	visibility := visibilityOf(paket)
	if !isStaff {
		if !visibility.Visible {
			return httpx.NewError(http.StatusForbidden, "Pembahasan paket ini belum dibuka.", "PEMBAHASAN_LOCKED",
				map[string]any{"enabled": visibility.IsEnabled, "open_at": visibility.OpenAt})
		}
		completed, err := s.hasCompletedExam(r.Context(), principal.UserID, id)
		if err != nil {
			return err
		}
		if !completed {
			return httpx.Forbidden("Anda harus menyelesaikan minimal satu ujian pada paket ini untuk membuka pembahasan.", "PEMBAHASAN_REQUIRES_ATTEMPT")
		}
	}

	conditions := []string{"s.soal_aktif = true", "p.pembahasan_id IS NOT NULL"}
	args := []any{}
	index := 1
	if isStaff {
		jenis := "tryout"
		if paket.JenisPaket == "ujian_real" {
			jenis = "ujian_real"
		}
		conditions = append(conditions, fmt.Sprintf("s.bank_jenis = $%d", index))
		args = append(args, jenis)
		index++
		conditions = append(conditions, fmt.Sprintf("s.bank_tipe = $%d", index))
		args = append(args, strings.ToLower(paket.PaketTipe))
		index++
		if paket.OrganizationID != nil {
			conditions = append(conditions, fmt.Sprintf("s.organization_id = $%d", index))
			args = append(args, *paket.OrganizationID)
			index++
		} else if paket.BankBatch != nil {
			conditions = append(conditions, fmt.Sprintf("s.bank_batch = $%d", index))
			args = append(args, strings.ToLower(*paket.BankBatch))
			index++
		}
	} else {
		ids, err := s.completedQuestionIDs(r.Context(), principal.UserID, id)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			httpx.Success(w, map[string]any{
				"paket": map[string]any{"paket_id": paket.PaketID, "paket_nama": paket.PaketNama, "paket_tipe": paket.PaketTipe},
				"items": []any{}, "total": 0,
			}, "", httpx.WithMeta(httpx.Meta(0, page, limit)))
			return nil
		}
		conditions = append(conditions, fmt.Sprintf("s.soal_id = ANY($%d)", index))
		args = append(args, ids)
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM soal_bank s JOIN soal_pembahasan p ON p.soal_id = s.soal_id WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`SELECT %s %s WHERE %s ORDER BY s.soal_id ASC LIMIT $%d OFFSET $%d`,
		soalColumns, soalJoin, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		item, err := scanSoal(rows)
		if err != nil {
			return err
		}
		items = append(items, map[string]any{
			"soal_id":         item.SoalID,
			"soal_kategori":   item.SoalKategori,
			"soal_kesulitan":  item.SoalKesulitan,
			"soal_tipe":       item.SoalTipe,
			"soal_pertanyaan": item.SoalPertanyaan,
			"soal_gambar_url": item.SoalGambarURL,
			"soal_gambar_alt": item.SoalGambarAlt,
			"soal_opsi":       fullOptions(item.SoalOpsi),
			"pembahasan":      item.Pembahasan,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.Success(w, map[string]any{
		"paket": map[string]any{"paket_id": paket.PaketID, "paket_nama": paket.PaketNama, "paket_tipe": paket.PaketTipe},
		"items": items,
		"total": total,
	}, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return nil
}

func packageAccessSummary(paket examPackage) string {
	return fmt.Sprintf("%s (%s)", paket.PaketNama, paket.PaketTipe)
}
