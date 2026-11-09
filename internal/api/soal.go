package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
)

type Pembahasan struct {
	PembahasanID    int       `json:"pembahasan_id"`
	SoalID          int       `json:"soal_id"`
	PembahasanTeks  string    `json:"pembahasan_teks"`
	PembahasanVideo *string   `json:"pembahasan_video"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Soal struct {
	SoalID         int             `json:"soal_id"`
	PaketID        *int            `json:"paket_id"`
	BankJenis      string          `json:"bank_jenis"`
	BankTipe       string          `json:"bank_tipe"`
	BankBatch      string          `json:"bank_batch"`
	SoalKategori   string          `json:"soal_kategori"`
	SoalKesulitan  string          `json:"soal_kesulitan"`
	SoalTipe       string          `json:"soal_tipe"`
	SoalPertanyaan string          `json:"soal_pertanyaan"`
	SoalGambarURL  *string         `json:"soal_gambar_url"`
	SoalGambarAlt  *string         `json:"soal_gambar_alt"`
	SoalOpsi       json.RawMessage `json:"soal_opsi"`
	SoalSkorMaks   float64         `json:"soal_skor_maks"`
	SoalAktif      bool            `json:"soal_aktif"`
	OrganizationID *int            `json:"organization_id"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	Pembahasan     *Pembahasan     `json:"pembahasan"`
}

const soalColumns = `s.soal_id, s.paket_id, s.bank_jenis, s.bank_tipe, s.bank_batch,
	s.soal_kategori, s.soal_kesulitan, s.soal_tipe, s.soal_pertanyaan, s.soal_gambar_url,
	s.soal_gambar_alt, s.soal_opsi, s.soal_skor_maks, s.soal_aktif, s.organization_id,
	s.created_at, s.updated_at, p.pembahasan_id, p.soal_id, p.pembahasan_teks,
	p.pembahasan_video, p.created_at, p.updated_at`

const soalJoin = ` FROM soal_bank s LEFT JOIN soal_pembahasan p ON p.soal_id = s.soal_id`

func scanSoal(row pgx.Row) (Soal, error) {
	var item Soal
	var opsi []byte
	var pembID, pembSoalID *int
	var pembTeks, pembVideo *string
	var pembCreated, pembUpdated *time.Time
	err := row.Scan(
		&item.SoalID, &item.PaketID, &item.BankJenis, &item.BankTipe, &item.BankBatch,
		&item.SoalKategori, &item.SoalKesulitan, &item.SoalTipe, &item.SoalPertanyaan,
		&item.SoalGambarURL, &item.SoalGambarAlt, &opsi,
		&item.SoalSkorMaks, &item.SoalAktif, &item.OrganizationID, &item.CreatedAt, &item.UpdatedAt,
		&pembID, &pembSoalID, &pembTeks, &pembVideo, &pembCreated, &pembUpdated,
	)
	if err != nil {
		return item, err
	}
	if len(opsi) > 0 {
		item.SoalOpsi = json.RawMessage(opsi)
	}
	if pembID != nil && pembTeks != nil && pembCreated != nil && pembUpdated != nil {
		item.Pembahasan = &Pembahasan{
			PembahasanID: *pembID, SoalID: *pembSoalID, PembahasanTeks: *pembTeks,
			PembahasanVideo: pembVideo, CreatedAt: *pembCreated, UpdatedAt: *pembUpdated,
		}
	}
	return item, nil
}

func (s *Server) registerSoal(api chi.Router) {
	api.Route("/soal", func(r chi.Router) {
		r.Use(s.Auth.Authenticate)
		r.Use(middleware.RequirePermission(s.Permissions, "admin.questions.view"))
		r.Get("/paket/{id}/pembahasan", httpx.Wrap(s.listPembahasanByPackage))
		r.Get("/banks", httpx.Wrap(s.listQuestionBanks))
		r.Get("/", httpx.Wrap(s.listSoal))
		r.With(middleware.RequirePermission(s.Permissions, "admin.questions.manage")).Post("/", httpx.Wrap(s.createSoal))
		r.With(middleware.RequirePermission(s.Permissions, "admin.questions.manage")).Post("/import", httpx.Wrap(s.importSoal))
		r.Get("/{id}", httpx.Wrap(s.soalDetail))
		r.With(middleware.RequirePermission(s.Permissions, "admin.questions.manage")).Put("/{id}", httpx.Wrap(s.updateSoal))
		r.With(middleware.RequirePermission(s.Permissions, "admin.questions.manage")).Delete("/{id}", httpx.Wrap(s.removeSoal))
		r.Get("/{id}/pembahasan", httpx.Wrap(s.getPembahasan))
		r.With(middleware.RequirePermission(s.Permissions, "admin.questions.manage")).Put("/{id}/pembahasan", httpx.Wrap(s.upsertPembahasan))
	})
}

func (s *Server) listSoal(w http.ResponseWriter, r *http.Request) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 100))
	conditions := []string{"1=1"}
	args := []any{}
	index := 1
	if paketID := httpx.QueryInt(r, "paket_id", 0); paketID > 0 {
		conditions = append(conditions, fmt.Sprintf("s.paket_id = $%d", index))
		args = append(args, paketID)
		index++
	}
	for queryKey, column := range map[string]string{
		"bank_jenis": "s.bank_jenis", "bank_tipe": "s.bank_tipe", "bank_batch": "s.bank_batch",
		"soal_kategori": "s.soal_kategori", "soal_kesulitan": "s.soal_kesulitan", "soal_tipe": "s.soal_tipe",
	} {
		if value := strings.TrimSpace(r.URL.Query().Get(queryKey)); value != "" {
			operator := "="
			if queryKey == "soal_kategori" {
				operator = "ILIKE"
			}
			conditions = append(conditions, fmt.Sprintf("%s %s $%d", column, operator, index))
			if queryKey == "bank_tipe" || queryKey == "bank_batch" {
				value = strings.ToLower(value)
			}
			args = append(args, value)
			index++
		}
	}
	if active := httpx.QueryBoolPtr(r, "soal_aktif"); active != nil {
		conditions = append(conditions, fmt.Sprintf("s.soal_aktif = $%d", index))
		args = append(args, *active)
		index++
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("s.soal_pertanyaan ILIKE $%d", index))
		args = append(args, "%"+search+"%")
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM soal_bank s WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`SELECT %s %s WHERE %s ORDER BY s.soal_id DESC LIMIT $%d OFFSET $%d`,
		soalColumns, soalJoin, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Soal{}
	for rows.Next() {
		item, err := scanSoal(rows)
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

func (s *Server) listQuestionBanks(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT bank_jenis, bank_tipe, bank_batch, count(*)
		FROM soal_bank WHERE soal_aktif = true
		GROUP BY bank_jenis, bank_tipe, bank_batch
		ORDER BY bank_jenis ASC, bank_tipe ASC, bank_batch ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var jenis, tipe, batch string
		var count int
		if err := rows.Scan(&jenis, &tipe, &batch, &count); err != nil {
			return err
		}
		items = append(items, map[string]any{
			"bank_jenis": jenis, "bank_tipe": tipe, "bank_batch": batch, "jumlah_soal": count,
		})
	}
	httpx.Success(w, items, "")
	return nil
}

func (s *Server) soalDetail(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	item, err := scanSoal(s.Pool.QueryRow(r.Context(), `SELECT `+soalColumns+soalJoin+` WHERE s.soal_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Soal tidak ditemukan", "QUESTION_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	httpx.Success(w, item, "")
	return nil
}

// normalizeMediaURL only accepts uploaded media or absolute HTTP(S) URLs.
// This keeps question images tied to our upload pipeline while still allowing
// intentionally hosted diagrams. Protocol-relative and script URLs are rejected.
func normalizeMediaURL(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", true
	}
	if len([]rune(value)) > 2048 {
		return "", false
	}
	if strings.HasPrefix(value, "/uploads/") && !strings.HasPrefix(value, "//") {
		return value, true
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", false
	}
	return value, true
}

func normalizeQuestionMediaFields(body map[string]any) error {
	if raw, exists := body["soal_gambar_url"]; exists {
		value := ""
		if raw != nil {
			value, _ = raw.(string)
		}
		normalized, ok := normalizeMediaURL(value)
		if !ok {
			return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{
				"field": "soal_gambar_url", "message": "URL gambar harus /uploads/... atau http(s)://...",
			}})
		}
		if normalized == "" {
			body["soal_gambar_url"] = nil
		} else {
			body["soal_gambar_url"] = normalized
		}
	}
	if raw, exists := body["soal_gambar_alt"]; exists {
		value := ""
		if raw != nil {
			value, _ = raw.(string)
		}
		value = strings.TrimSpace(value)
		if len([]rune(value)) > 200 {
			return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{
				"field": "soal_gambar_alt", "message": "Teks alternatif maksimal 200 karakter",
			}})
		}
		if value == "" {
			body["soal_gambar_alt"] = nil
		} else {
			body["soal_gambar_alt"] = value
		}
	}
	return nil
}

func validateOptions(tipe string, options []any) error {
	if tipe == "essay" {
		return nil
	}
	if len(options) < 2 {
		return httpx.Unprocessable("Soal exact/gradasi wajib memiliki minimal 2 opsi.", "INVALID_QUESTION_OPTIONS", nil)
	}
	for index, raw := range options {
		item, _ := raw.(map[string]any)
		imageURL, ok := normalizeMediaURL(bodyString(item, "gambar_url"))
		if !ok {
			return httpx.Unprocessable("Gambar opsi tidak valid.", "INVALID_QUESTION_OPTIONS", []map[string]string{{
				"field": fmt.Sprintf("soal_opsi[%d].gambar_url", index), "message": "Gunakan /uploads/... atau http(s)://...",
			}})
		}
		alt := strings.TrimSpace(bodyString(item, "gambar_alt"))
		if len([]rune(alt)) > 200 {
			return httpx.Unprocessable("Teks alternatif opsi terlalu panjang.", "INVALID_QUESTION_OPTIONS", []map[string]string{{
				"field": fmt.Sprintf("soal_opsi[%d].gambar_alt", index), "message": "Maksimal 200 karakter",
			}})
		}
		if imageURL == "" {
			delete(item, "gambar_url")
		} else {
			item["gambar_url"] = imageURL
		}
		if alt == "" {
			delete(item, "gambar_alt")
		} else {
			item["gambar_alt"] = alt
		}
		if bodyString(item, "teks") == "" && imageURL == "" {
			return httpx.Unprocessable("Teks atau gambar opsi wajib diisi.", "INVALID_QUESTION_OPTIONS", []map[string]string{{
				"field": fmt.Sprintf("soal_opsi[%d].teks", index), "message": "Isi teks atau lampirkan gambar opsi",
			}})
		}
	}
	if tipe == "exact" {
		correct := 0
		for _, raw := range options {
			item, _ := raw.(map[string]any)
			if item["is_correct"] == true {
				correct++
			}
		}
		if correct != 1 {
			return httpx.Unprocessable("Soal exact wajib memiliki tepat satu opsi dengan is_correct: true.", "INVALID_QUESTION_OPTIONS", nil)
		}
	}
	if tipe == "gradasi" {
		hasScore := false
		for _, raw := range options {
			item, _ := raw.(map[string]any)
			if bodyFloat(item, "skor", 0) > 0 {
				hasScore = true
			}
		}
		if !hasScore {
			return httpx.Unprocessable("Soal gradasi wajib memiliki minimal satu opsi dengan skor > 0.", "INVALID_QUESTION_OPTIONS", nil)
		}
	}
	return nil
}

func normalizeQuestionOptions(raw any) []any {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(values))
	for index, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		label := bodyString(item, "label")
		if label == "" {
			label = string(rune('A' + index))
		}
		normalized := map[string]any{"label": strings.ToUpper(label), "teks": bodyString(item, "teks")}
		if imageURL := bodyString(item, "gambar_url"); imageURL != "" {
			normalized["gambar_url"] = imageURL
		}
		if imageAlt := bodyString(item, "gambar_alt"); imageAlt != "" {
			normalized["gambar_alt"] = imageAlt
		}
		if item["is_correct"] == true {
			normalized["is_correct"] = true
		}
		if score, exists := item["skor"]; exists && score != nil {
			normalized["skor"] = bodyFloat(item, "skor", 0)
		}
		result = append(result, normalized)
	}
	return result
}

func (s *Server) questionPayload(r *http.Request) (map[string]any, error) {
	body, err := requireBody(r)
	if err != nil {
		return nil, err
	}
	tipe := bodyString(body, "soal_tipe")
	if tipe == "" {
		tipe = "exact"
	}
	if tipe != "exact" && tipe != "gradasi" && tipe != "essay" {
		return nil, httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "soal_tipe", "message": "Tipe soal tidak valid"}})
	}
	if err := normalizeQuestionMediaFields(body); err != nil {
		return nil, err
	}
	options := normalizeQuestionOptions(body["soal_opsi"])
	if err := validateOptions(tipe, options); err != nil {
		return nil, err
	}
	if tipe == "essay" {
		body["soal_opsi"] = nil
	} else {
		body["soal_opsi"] = options
	}
	return body, nil
}

func (s *Server) createSoal(w http.ResponseWriter, r *http.Request) error {
	body, err := s.questionPayload(r)
	if err != nil {
		return err
	}
	v := &validation{}
	kategori := v.stringField(body, "soal_kategori", "Kategori", 1, 50, true)
	kesulitan := bodyString(body, "soal_kesulitan")
	if kesulitan == "" {
		kesulitan = "sedang"
	}
	pertanyaan := v.stringField(body, "soal_pertanyaan", "Pertanyaan", 0, 20000, false)
	gambarURL := bodyString(body, "soal_gambar_url")
	if len([]rune(pertanyaan)) < 5 && gambarURL == "" {
		v.add("soal_pertanyaan", "Pertanyaan minimal 5 karakter atau lampirkan gambar")
	}
	paketID := bodyIntPtr(body, "paket_id")
	bankJenis := bodyString(body, "bank_jenis")
	if bankJenis != "ujian_real" {
		bankJenis = "tryout"
	}
	bankTipe := strings.ToLower(bodyString(body, "bank_tipe"))
	bankBatch := strings.ToLower(bodyString(body, "bank_batch"))
	if paketID != nil {
		var pJenis, pTipe string
		var pBatch *string
		if err := s.Pool.QueryRow(r.Context(), `SELECT jenis_paket, paket_tipe, bank_batch FROM paket_ujian WHERE paket_id = $1`, *paketID).Scan(&pJenis, &pTipe, &pBatch); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.NotFound("Paket ujian tidak ditemukan", "PACKAGE_NOT_FOUND")
			}
			return err
		}
		if pJenis == "ujian_real" {
			pJenis = "ujian_real"
		} else {
			pJenis = "tryout"
		}
		if bodyString(body, "bank_jenis") != "" && bankJenis != pJenis {
			return httpx.Unprocessable("bank_jenis soal tidak cocok dengan jenis paket asal.", "BANK_SOURCE_MISMATCH", nil)
		}
		if bankTipe != "" && bankTipe != strings.ToLower(pTipe) {
			return httpx.Unprocessable("bank_tipe soal tidak cocok dengan tipe paket asal.", "BANK_SOURCE_MISMATCH", nil)
		}
		bankJenis, bankTipe = pJenis, strings.ToLower(pTipe)
		if pBatch != nil {
			if bankBatch != "" && bankBatch != strings.ToLower(*pBatch) {
				return httpx.Unprocessable("bank_batch soal tidak cocok dengan batch paket asal.", "BANK_SOURCE_MISMATCH", nil)
			}
			bankBatch = strings.ToLower(*pBatch)
		}
	}
	if bankTipe == "" {
		v.add("bank_tipe", "bank_tipe wajib diisi jika soal tidak dikaitkan ke paket asal")
	}
	if err := v.err(); err != nil {
		return err
	}
	if bankBatch == "" {
		bankBatch = "umum"
	}
	tipe := bodyString(body, "soal_tipe")
	skor := bodyFloat(body, "soal_skor_maks", 1)
	if skor <= 0 || skor > 1000 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "soal_skor_maks", "message": "Skor maksimal harus 1-1000"}})
	}
	item, err := scanSoal(s.Pool.QueryRow(r.Context(), `
		INSERT INTO soal_bank (paket_id, bank_jenis, bank_tipe, bank_batch, soal_kategori, soal_kesulitan,
			soal_tipe, soal_pertanyaan, soal_gambar_url, soal_gambar_alt, soal_opsi, soal_skor_maks,
			soal_aktif, organization_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,NULL,now(),now())
		RETURNING soal_id, paket_id, bank_jenis, bank_tipe, bank_batch, soal_kategori, soal_kesulitan,
			soal_tipe, soal_pertanyaan, soal_gambar_url, soal_gambar_alt, soal_opsi, soal_skor_maks,
			soal_aktif, organization_id, created_at, updated_at,
			NULL::int, NULL::int, NULL::text, NULL::text, NULL::timestamptz, NULL::timestamptz`,
		paketID, bankJenis, bankTipe, bankBatch, kategori, kesulitan, tipe, pertanyaan,
		body["soal_gambar_url"], body["soal_gambar_alt"], jsonArgument(body["soal_opsi"]), skor,
		bodyBool(body, "soal_aktif", true)))
	if err != nil {
		return err
	}
	httpx.Created(w, item, "Soal berhasil dibuat")
	return nil
}

func (s *Server) updateSoal(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	existing, err := scanSoal(s.Pool.QueryRow(r.Context(), `SELECT `+soalColumns+soalJoin+` WHERE s.soal_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Soal tidak ditemukan", "QUESTION_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	body, err := s.questionPayload(r)
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
	for key, column := range map[string]string{
		"bank_jenis": "bank_jenis", "bank_tipe": "bank_tipe", "bank_batch": "bank_batch",
		"soal_kategori": "soal_kategori", "soal_kesulitan": "soal_kesulitan", "soal_tipe": "soal_tipe",
		"soal_pertanyaan": "soal_pertanyaan",
	} {
		if value, exists := body[key]; exists {
			text, _ := value.(string)
			text = strings.TrimSpace(text)
			if key == "bank_tipe" || key == "bank_batch" || key == "bank_jenis" {
				text = strings.ToLower(text)
			}
			if text == "" && key == "bank_batch" {
				text = "umum"
			}
			add(column, "", text)
		}
	}
	if value, exists := body["paket_id"]; exists {
		if value == nil {
			add("paket_id", "", nil)
		} else {
			add("paket_id", "", bodyInt(body, "paket_id", 0))
		}
	}
	if value, exists := body["soal_gambar_url"]; exists {
		add("soal_gambar_url", "", value)
	}
	if value, exists := body["soal_gambar_alt"]; exists {
		add("soal_gambar_alt", "", value)
	}
	if _, exists := body["soal_opsi"]; exists {
		add("soal_opsi", "jsonb", jsonArgument(body["soal_opsi"]))
	}
	if _, exists := body["soal_skor_maks"]; exists {
		add("soal_skor_maks", "", bodyFloat(body, "soal_skor_maks", existing.SoalSkorMaks))
	}
	if _, exists := body["soal_aktif"]; exists {
		add("soal_aktif", "", bodyBool(body, "soal_aktif", existing.SoalAktif))
	}
	if len(set) == 0 {
		httpx.Success(w, existing, "Soal berhasil diperbarui")
		return nil
	}
	set = append(set, "updated_at = now()")
	args = append(args, id)
	query := fmt.Sprintf(`UPDATE soal_bank SET %s WHERE soal_id = $%d RETURNING soal_id, paket_id, bank_jenis, bank_tipe,
		bank_batch, soal_kategori, soal_kesulitan, soal_tipe, soal_pertanyaan, soal_gambar_url,
		soal_gambar_alt, soal_opsi, soal_skor_maks, soal_aktif, organization_id, created_at, updated_at,
		(SELECT pembahasan_id FROM soal_pembahasan WHERE soal_id = $%d),
		(SELECT soal_id FROM soal_pembahasan WHERE soal_id = $%d),
		(SELECT pembahasan_teks FROM soal_pembahasan WHERE soal_id = $%d),
		(SELECT pembahasan_video FROM soal_pembahasan WHERE soal_id = $%d),
		(SELECT created_at FROM soal_pembahasan WHERE soal_id = $%d),
		(SELECT updated_at FROM soal_pembahasan WHERE soal_id = $%d)`,
		strings.Join(set, ", "), index, index, index, index, index, index, index)
	item, err := scanSoal(s.Pool.QueryRow(r.Context(), query, args...))
	if err != nil {
		return err
	}
	httpx.Success(w, item, "Soal berhasil diperbarui")
	return nil
}

func (s *Server) removeSoal(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.Pool.Exec(r.Context(), `UPDATE soal_bank SET soal_aktif = false, updated_at = now() WHERE soal_id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Soal tidak ditemukan", "QUESTION_NOT_FOUND")
	}
	httpx.Success(w, map[string]any{"soal_id": id, "soal_aktif": false}, "Soal dinonaktifkan (soft delete).")
	return nil
}

func (s *Server) getPembahasan(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	item, err := scanSoal(s.Pool.QueryRow(r.Context(), `SELECT `+soalColumns+soalJoin+` WHERE s.soal_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Soal tidak ditemukan", "QUESTION_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	httpx.Success(w, item.Pembahasan, "")
	return nil
}

func (s *Server) upsertPembahasan(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	v := &validation{}
	teks := v.stringField(body, "pembahasan_teks", "Pembahasan", 5, 20000, true)
	if err := v.err(); err != nil {
		return err
	}
	video := bodyStringPtr(body, "pembahasan_video")
	var item Pembahasan
	err = s.Pool.QueryRow(r.Context(), `
		INSERT INTO soal_pembahasan (soal_id, pembahasan_teks, pembahasan_video, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (soal_id) DO UPDATE
		SET pembahasan_teks = EXCLUDED.pembahasan_teks,
		    pembahasan_video = EXCLUDED.pembahasan_video,
		    updated_at = now()
		RETURNING pembahasan_id, soal_id, pembahasan_teks, pembahasan_video, created_at, updated_at`,
		id, teks, video,
	).Scan(&item.PembahasanID, &item.SoalID, &item.PembahasanTeks, &item.PembahasanVideo, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return err
	}
	httpx.Success(w, item, "Pembahasan berhasil disimpan")
	return nil
}

func (s *Server) listPembahasanByPackage(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	var jenis, tipe string
	var batch *string
	if err := s.Pool.QueryRow(r.Context(), `SELECT jenis_paket, paket_tipe, bank_batch FROM paket_ujian WHERE paket_id = $1`, id).Scan(&jenis, &tipe, &batch); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Paket ujian tidak ditemukan", "PACKAGE_NOT_FOUND")
		}
		return err
	}
	if jenis != "ujian_real" {
		jenis = "tryout"
	}
	conditions := []string{"s.soal_aktif = true", "p.pembahasan_id IS NOT NULL", "s.bank_jenis = $1", "s.bank_tipe = $2"}
	args := []any{jenis, strings.ToLower(tipe)}
	index := 3
	if batch != nil {
		conditions = append(conditions, fmt.Sprintf("s.bank_batch = $%d", index))
		args = append(args, strings.ToLower(*batch))
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
	items := []Soal{}
	for rows.Next() {
		item, err := scanSoal(rows)
		if err != nil {
			return err
		}
		// sanitizeOptions: admin hanya butuh label/teks pada daftar pembahasan.
		var options []map[string]any
		if len(item.SoalOpsi) > 0 {
			_ = json.Unmarshal(item.SoalOpsi, &options)
			sanitized := make([]map[string]any, 0, len(options))
			for _, option := range options {
				sanitized = append(sanitized, map[string]any{"label": option["label"], "teks": option["teks"]})
			}
			encoded, _ := json.Marshal(sanitized)
			item.SoalOpsi = encoded
		}
		items = append(items, item)
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return nil
}

func (s *Server) importSoal(w http.ResponseWriter, r *http.Request) error {
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	bankTipe := strings.ToLower(bodyString(body, "bank_tipe"))
	bankBatch := strings.ToLower(bodyString(body, "bank_batch"))
	if bankTipe == "" {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "bank_tipe", "message": "bank_tipe wajib diisi"}})
	}
	rows := bodySlice(body, "soal")
	if len(rows) == 0 || len(rows) > 500 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "soal", "message": "Minimal 1 dan maksimal 500 soal per import"}})
	}
	prepared := make([]map[string]any, 0, len(rows))
	rowErrors := []map[string]any{}
	for index, raw := range rows {
		item, ok := raw.(map[string]any)
		if !ok {
			rowErrors = append(rowErrors, map[string]any{"baris": index + 1, "pesan": "Baris tidak valid"})
			continue
		}
		tipe := bodyString(item, "soal_tipe")
		if tipe == "" {
			tipe = "exact"
		}
		if err := normalizeQuestionMediaFields(item); err != nil {
			rowErrors = append(rowErrors, map[string]any{"baris": index + 1, "pesan": err.Error()})
			continue
		}
		options := normalizeQuestionOptions(item["soal_opsi"])
		if err := validateOptions(tipe, options); err != nil {
			rowErrors = append(rowErrors, map[string]any{"baris": index + 1, "pesan": err.Error()})
			continue
		}
		category := bodyString(item, "soal_kategori")
		question := bodyString(item, "soal_pertanyaan")
		if category == "" || (len([]rune(question)) < 5 && bodyString(item, "soal_gambar_url") == "") {
			rowErrors = append(rowErrors, map[string]any{"baris": index + 1, "pesan": "soal_kategori dan pertanyaan/gambar wajib valid"})
			continue
		}
		jenis := bodyString(item, "bank_jenis")
		if jenis != "ujian_real" {
			jenis = "tryout"
		}
		batch := strings.ToLower(bodyString(item, "bank_batch"))
		if batch == "" {
			batch = bankBatch
		}
		if batch == "" {
			batch = "umum"
		}
		prepared = append(prepared, map[string]any{
			"paket_id": bodyIntPtr(item, "paket_id"), "bank_jenis": jenis, "bank_tipe": bankTipe,
			"bank_batch": batch, "soal_kategori": category, "soal_kesulitan": bodyStringDefault(item, "soal_kesulitan", "sedang"),
			"soal_tipe": tipe, "soal_pertanyaan": question,
			"soal_gambar_url": item["soal_gambar_url"], "soal_gambar_alt": item["soal_gambar_alt"],
			"soal_opsi": jsonArgument(options), "soal_skor_maks": bodyFloat(item, "soal_skor_maks", 1),
			"soal_aktif": bodyBool(item, "soal_aktif", true),
		})
	}
	if len(rowErrors) > 0 {
		return httpx.Unprocessable(fmt.Sprintf("%d baris soal tidak valid. Perbaiki lalu unggah ulang.", len(rowErrors)), "IMPORT_ROWS_INVALID", rowErrors)
	}
	err = pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
		for _, row := range prepared {
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO soal_bank (paket_id, bank_jenis, bank_tipe, bank_batch, soal_kategori, soal_kesulitan,
					soal_tipe, soal_pertanyaan, soal_gambar_url, soal_gambar_alt, soal_opsi, soal_skor_maks,
					soal_aktif, organization_id, created_at, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,NULL,now(),now())`,
				row["paket_id"], row["bank_jenis"], row["bank_tipe"], row["bank_batch"], row["soal_kategori"],
				row["soal_kesulitan"], row["soal_tipe"], row["soal_pertanyaan"], row["soal_gambar_url"],
				row["soal_gambar_alt"], row["soal_opsi"], row["soal_skor_maks"], row["soal_aktif"]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	httpx.Created(w, map[string]any{"inserted": len(prepared), "bank_tipe": bankTipe}, fmt.Sprintf("%d soal berhasil diimpor", len(prepared)))
	return nil
}
