package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/codegen"
	"github.com/edumaster/backend-go/internal/exam"
	"github.com/edumaster/backend-go/internal/httpx"
)

func (s *Server) uniquePackageAccessCode(ctx context.Context) (string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		code := codegen.PackageCode()
		var exists bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM paket_ujian WHERE paket_access_code = $1)`, code).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", httpx.NewError(http.StatusInternalServerError, "Gagal membuat kode akses unik", "CODE_GENERATION_FAILED", nil)
}

func validateManualSelection(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID int, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM soal_bank
		WHERE soal_id = ANY($1) AND organization_id = $2 AND soal_aktif = true`, ids, organizationID).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return httpx.Unprocessable("Sebagian soal pilihan tidak ditemukan, tidak aktif, atau milik organisasi lain.", "MANUAL_QUESTION_NOT_FOUND", nil)
	}
	return nil
}

func replaceManualSelection(ctx context.Context, tx pgx.Tx, paketID int, ids []int) error {
	if _, err := tx.Exec(ctx, `DELETE FROM paket_soal WHERE paket_id = $1`, paketID); err != nil {
		return err
	}
	for index, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO paket_soal (paket_id, soal_id, urutan) VALUES ($1, $2, $3)`, paketID, id, index+1); err != nil {
			return err
		}
	}
	return nil
}

func normalizeBankJenis(value string) string {
	if value == "ujian_real" {
		return "ujian_real"
	}
	return "tryout"
}

func (s *Server) businessCreatePackage(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	member, err := s.requireMembership(r.Context(), principal.UserID, nil)
	if err != nil {
		return err
	}
	if err := assertManager(member); err != nil {
		return err
	}
	if err := assertOrgActive(member); err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(bodyString(body, "paket_nama"))
	if len([]rune(name)) < 3 || len([]rune(name)) > 200 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "paket_nama", "message": "Nama paket wajib 3-200 karakter"}})
	}
	paketTipe := strings.ToLower(strings.TrimSpace(bodyString(body, "paket_tipe")))
	if paketTipe == "" {
		paketTipe = "cpns"
	}
	jenis := normalizeBankJenis(strings.ToLower(bodyString(body, "jenis_paket")))
	passingGrade := bodyFloat(body, "passing_grade", 0)
	skala := bodyStringDefault(body, "skala_nilai", "skala_100")
	if skala != "poin_mentah" {
		skala = "skala_100"
	}
	maxGrade := 100.0
	if skala == "poin_mentah" {
		maxGrade = 9999
	}
	if passingGrade < 0 || passingGrade > maxGrade {
		return httpx.Unprocessable(fmt.Sprintf("passing_grade harus 0-%.0f", maxGrade), "PASSING_GRADE_INVALID", nil)
	}
	duration := bodyInt(body, "duration_minutes", 90)
	if duration < 1 || duration > 1440 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "duration_minutes", "message": "Durasi harus 1-1440 menit"}})
	}
	maxAttempts := bodyInt(body, "max_attempts", 3)
	if maxAttempts < 0 || maxAttempts > 100 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "max_attempts", "message": "Maksimal attempt harus 0-100"}})
	}
	jumlahSoal := bodyInt(body, "paket_jumlah_soal", 25)
	if jumlahSoal < 1 || jumlahSoal > 500 {
		jumlahSoal = 25
	}
	selectionMode := "auto"
	if bodyString(body, "selection_mode") == "manual" {
		selectionMode = "manual"
	}
	manualIDs := bodyIntSlice(body, "manual_soal_ids")
	if selectionMode == "manual" {
		if err := validateManualSelection(r.Context(), s.Pool, member.Organization.OrganizationID, manualIDs); err != nil {
			return err
		}
	}
	accessCode := codegen.NormalizeAccessCode(bodyString(body, "paket_access_code"))
	if accessCode == "" {
		accessCode, err = s.uniquePackageAccessCode(r.Context())
		if err != nil {
			return err
		}
	} else {
		var exists bool
		if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM paket_ujian WHERE paket_access_code = $1)`, accessCode).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return httpx.Conflict("Kode akses paket sudah digunakan.", "PACKAGE_ACCESS_CODE_EXISTS")
		}
	}
	introMode := bodyStringDefault(body, "intro_kategori_mode", "always")
	if introMode != "first" && introMode != "never" {
		introMode = "always"
	}
	focusMax := bodyInt(body, "focus_lock_max_violations", 3)
	if focusMax < 1 {
		focusMax = 1
	}
	if focusMax > 20 {
		focusMax = 20
	}
	modeSoal := bodyStringDefault(body, "mode_soal", "acak")
	if modeSoal != "tetap" {
		modeSoal = "acak"
	}
	passingMode := bodyStringDefault(body, "passing_mode", "overall")
	if passingMode != "per_category" {
		passingMode = "overall"
	}

	emails := bodyStringSlice(body, "participant_emails")
	participants := map[string]any{"created": 0, "skipped": []string{}, "items": []any{}}
	var paketID int
	err = pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO paket_ujian (
				paket_nama, paket_deskripsi, paket_tipe, jenis_paket, bank_batch, paket_harga,
				passing_grade, duration_minutes, max_attempts, paket_jp, is_pembahasan_visible,
				tanggal_buka_pembahasan, paket_komposisi, mode_soal, paket_bobot_kategori,
				passing_mode, paket_passing_kategori, selection_mode, paket_jumlah_soal,
				allow_backtrack, intro_kategori_mode, skala_nilai, focus_lock_enabled,
				focus_lock_max_violations, is_active, paket_landing, paket_landing_urutan,
				organization_id, paket_access_code, created_at, updated_at
			) VALUES (
				$1,$2,$3,$4,$5,0,$6,$7,$8,$9,$10,NULL,$11::jsonb,$12,$13::jsonb,
				$14,$15::jsonb,$16,$17,$18,$19,$20,$21,$22,true,false,0,$23,$24,now(),now()
			) RETURNING paket_id`,
			name, bodyStringPtr(body, "paket_deskripsi"), paketTipe, jenis, fmt.Sprintf("org-%d", member.Organization.OrganizationID),
			passingGrade, duration, maxAttempts, bodyInt(body, "paket_jp", 8), bodyBool(body, "is_pembahasan_visible", true),
			jsonArgument(body["paket_komposisi"]), modeSoal, jsonArgument(body["paket_bobot_kategori"]),
			passingMode, jsonArgument(body["paket_passing_kategori"]), selectionMode, jumlahSoal,
			bodyBool(body, "allow_backtrack", true), introMode, skala, bodyBool(body, "focus_lock_enabled", true),
			focusMax, member.Organization.OrganizationID, accessCode,
		).Scan(&paketID); err != nil {
			return err
		}
		if selectionMode == "manual" && len(manualIDs) > 0 {
			if err := replaceManualSelection(r.Context(), tx, paketID, manualIDs); err != nil {
				return err
			}
		}
		if len(emails) > 0 {
			created, skipped, err := s.addParticipantEmails(r.Context(), tx, paketID, emails, principal.UserID)
			if err != nil {
				return err
			}
			participants["created"] = created
			participants["skipped"] = skipped
		}
		return nil
	})
	if err != nil {
		return err
	}
	paket, err := scanPaket(s.Pool.QueryRow(r.Context(), `SELECT `+packageSelect+` FROM paket_ujian p WHERE p.paket_id = $1`, paketID))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_PACKAGE_CREATE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "paket_id": paketID,
		"paket_tipe": paketTipe, "participants": participants["created"],
	})
	httpx.Created(w, map[string]any{"paket": paket, "access_code": accessCode, "participants": participants}, "Kelas/ujian privat berhasil dibuat.")
	return nil
}

func (s *Server) businessUpdatePackage(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	member, err := s.requireMembership(r.Context(), principal.UserID, nil)
	if err != nil {
		return err
	}
	if err := assertManager(member); err != nil {
		return err
	}
	if err := assertOrgActive(member); err != nil {
		return err
	}
	if _, err := s.requireOrgPackage(r.Context(), id, member.Organization.OrganizationID); err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	for _, key := range []string{"organization_id", "paket_landing", "paket_landing_urutan", "paket_harga", "paket_tipe", "jenis_paket", "bank_batch"} {
		delete(body, key)
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
	if value, exists := body["paket_access_code"]; exists {
		text, _ := value.(string)
		normalized := codegen.NormalizeAccessCode(text)
		if normalized != "" {
			var exists bool
			if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM paket_ujian WHERE paket_access_code = $1 AND paket_id <> $2)`, normalized, id).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return httpx.Conflict("Kode akses paket sudah digunakan.", "PACKAGE_ACCESS_CODE_EXISTS")
			}
			add("paket_access_code", "", normalized)
		}
	}
	for key, column := range map[string]string{
		"paket_nama": "paket_nama", "paket_deskripsi": "paket_deskripsi", "mode_soal": "mode_soal",
		"passing_mode": "passing_mode", "selection_mode": "selection_mode",
		"intro_kategori_mode": "intro_kategori_mode", "skala_nilai": "skala_nilai",
	} {
		if value, exists := body[key]; exists {
			if value == nil {
				add(column, "", nil)
				continue
			}
			text := strings.TrimSpace(bodyString(body, key))
			switch key {
			case "paket_nama":
				if len([]rune(text)) < 3 || len([]rune(text)) > 200 {
					return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "paket_nama", "message": "Nama paket wajib 3-200 karakter"}})
				}
			case "mode_soal":
				if text != "tetap" {
					text = "acak"
				}
			case "passing_mode":
				if text != "per_category" {
					text = "overall"
				}
			case "selection_mode":
				if text != "manual" {
					text = "auto"
				}
			case "intro_kategori_mode":
				if text != "first" && text != "never" {
					text = "always"
				}
			case "skala_nilai":
				if text != "poin_mentah" {
					text = "skala_100"
				}
			case "paket_deskripsi":
				if text == "" {
					add(column, "", nil)
					continue
				}
			}
			add(column, "", text)
		}
	}
	for _, key := range []string{"passing_grade", "duration_minutes", "max_attempts", "paket_jp", "paket_jumlah_soal", "focus_lock_max_violations"} {
		if _, exists := body[key]; exists {
			add(key, "", bodyInt(body, key, 0))
		}
	}
	for _, key := range []string{"is_pembahasan_visible", "allow_backtrack", "focus_lock_enabled"} {
		if _, exists := body[key]; exists {
			add(key, "", bodyBool(body, key, true))
		}
	}
	for _, key := range []string{"paket_komposisi", "paket_bobot_kategori", "paket_passing_kategori"} {
		if value, exists := body[key]; exists {
			add(key, "jsonb", jsonArgument(value))
		}
	}
	if raw, exists := body["tanggal_buka_pembahasan"]; exists {
		add("tanggal_buka_pembahasan", "", parseDatePtr(raw))
	}
	manualIDs := bodyIntSlice(body, "manual_soal_ids")
	if selectionMode, _ := body["selection_mode"].(string); selectionMode == "manual" {
		if err := validateManualSelection(r.Context(), s.Pool, member.Organization.OrganizationID, manualIDs); err != nil {
			return err
		}
	}
	err = pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
		if len(set) > 0 {
			set = append(set, "updated_at = now()")
			args = append(args, id)
			if _, err := tx.Exec(r.Context(), fmt.Sprintf(`UPDATE paket_ujian SET %s WHERE paket_id = $%d`, strings.Join(set, ", "), index), args...); err != nil {
				return err
			}
		}
		if selectionMode, _ := body["selection_mode"].(string); selectionMode == "manual" {
			return replaceManualSelection(r.Context(), tx, id, manualIDs)
		}
		return nil
	})
	if err != nil {
		return err
	}
	paket, err := scanPaket(s.Pool.QueryRow(r.Context(), `SELECT `+packageSelect+` FROM paket_ujian p WHERE p.paket_id = $1`, id))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_PACKAGE_UPDATE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "paket_id": id,
	})
	httpx.Success(w, paket, "Kelas/ujian privat berhasil diperbarui.")
	return nil
}

func (s *Server) businessRemovePackage(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	member, err := s.requireMembership(r.Context(), principal.UserID, nil)
	if err != nil {
		return err
	}
	if err := assertManager(member); err != nil {
		return err
	}
	if err := assertOrgActive(member); err != nil {
		return err
	}
	if _, err := s.requireOrgPackage(r.Context(), id, member.Organization.OrganizationID); err != nil {
		return err
	}
	tag, err := s.Pool.Exec(r.Context(), `UPDATE paket_ujian SET is_active = false, updated_at = now() WHERE paket_id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Paket privat tidak ditemukan", "PACKAGE_NOT_FOUND")
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_PACKAGE_DELETE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "paket_id": id,
	})
	httpx.Success(w, map[string]any{"paket_id": id, "is_active": false}, "Kelas/ujian privat dinonaktifkan.")
	return nil
}

func (s *Server) businessListModules(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	member, err := s.requireMembership(r.Context(), principal.UserID, nil)
	if err != nil {
		return err
	}
	if err := assertManager(member); err != nil {
		return err
	}
	if err := assertOrgActive(member); err != nil {
		return err
	}
	paket, err := s.requireOrgPackage(r.Context(), id, member.Organization.OrganizationID)
	if err != nil {
		return err
	}
	if paket.PaketTipe != "elearning" {
		return httpx.BadRequest("Modul hanya tersedia untuk kelas e-learning.", "PACKAGE_NOT_ELEARNING")
	}
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM lms_materi WHERE paket_id = $1`, id).Scan(&total); err != nil {
		return err
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT materi_id, paket_id, materi_judul, materi_deskripsi, materi_tipe, materi_url,
		       materi_konten, materi_urutan, is_wajib, is_active, created_at, updated_at,
		       (SELECT count(*) FROM lms_progress p WHERE p.materi_id = lms_materi.materi_id) AS progress_count
		FROM lms_materi WHERE paket_id = $1 ORDER BY materi_urutan ASC, materi_id ASC LIMIT $2 OFFSET $3`, id, limit, offset)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var materiID, paketID, urutan, progressCount int
		var judul string
		var deskripsi, url, konten *string
		var tipe string
		var wajib, active bool
		var created, updated time.Time
		if err := rows.Scan(&materiID, &paketID, &judul, &deskripsi, &tipe, &url, &konten, &urutan,
			&wajib, &active, &created, &updated, &progressCount); err != nil {
			return err
		}
		items = append(items, map[string]any{
			"materi_id": materiID, "paket_id": paketID, "materi_judul": judul,
			"materi_deskripsi": deskripsi, "materi_tipe": tipe, "materi_url": url,
			"materi_konten": konten, "materi_urutan": urutan, "is_wajib": wajib,
			"is_active": active, "created_at": created, "updated_at": updated,
			"_count": map[string]int{"progress": progressCount},
		})
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)),
		withExtras(map[string]any{"paket": paket}))
	return nil
}

func (s *Server) businessCreateModule(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	member, err := s.requireMembership(r.Context(), principal.UserID, nil)
	if err != nil {
		return err
	}
	if err := assertManager(member); err != nil {
		return err
	}
	if err := assertOrgActive(member); err != nil {
		return err
	}
	paket, err := s.requireOrgPackage(r.Context(), id, member.Organization.OrganizationID)
	if err != nil {
		return err
	}
	if paket.PaketTipe != "elearning" {
		return httpx.BadRequest("Modul hanya tersedia untuk kelas e-learning.", "PACKAGE_NOT_ELEARNING")
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	judul := strings.TrimSpace(bodyString(body, "materi_judul"))
	if len([]rune(judul)) < 3 || len([]rune(judul)) > 200 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "materi_judul", "message": "Judul modul wajib 3-200 karakter"}})
	}
	tipe := bodyStringDefault(body, "materi_tipe", "teks")
	if tipe != "teks" && tipe != "pdf" && tipe != "video" && tipe != "tugas" && tipe != "dokumen" {
		tipe = "teks"
	}
	var materiID int
	var created, updated time.Time
	if err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO lms_materi (paket_id, materi_judul, materi_deskripsi, materi_tipe, materi_url,
			materi_konten, materi_urutan, is_wajib, is_active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now(),now())
		RETURNING materi_id, created_at, updated_at`,
		id, judul, bodyStringPtr(body, "materi_deskripsi"), tipe, bodyStringPtr(body, "materi_url"),
		bodyStringPtr(body, "materi_konten"), bodyInt(body, "materi_urutan", 0),
		bodyBool(body, "is_wajib", true), bodyBool(body, "is_active", true),
	).Scan(&materiID, &created, &updated); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_MODULE_CREATE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "paket_id": id, "materi_id": materiID,
	})
	httpx.Created(w, map[string]any{
		"materi_id": materiID, "paket_id": id, "materi_judul": judul,
		"materi_deskripsi": bodyStringPtr(body, "materi_deskripsi"), "materi_tipe": tipe,
		"materi_url": bodyStringPtr(body, "materi_url"), "materi_konten": bodyStringPtr(body, "materi_konten"),
		"materi_urutan": bodyInt(body, "materi_urutan", 0), "is_wajib": bodyBool(body, "is_wajib", true),
		"is_active": bodyBool(body, "is_active", true), "created_at": created, "updated_at": updated,
	}, "Modul kelas berhasil dibuat.")
	return nil
}

func (s *Server) businessUpdateModule(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	member, err := s.requireMembership(r.Context(), principal.UserID, nil)
	if err != nil {
		return err
	}
	if err := assertManager(member); err != nil {
		return err
	}
	if err := assertOrgActive(member); err != nil {
		return err
	}
	var paketID int
	if err := s.Pool.QueryRow(r.Context(), `SELECT paket_id FROM lms_materi WHERE materi_id = $1`, id).Scan(&paketID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Materi tidak ditemukan", "LMS_MATERIAL_NOT_FOUND")
		}
		return err
	}
	if _, err := s.requireOrgPackage(r.Context(), paketID, member.Organization.OrganizationID); err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	delete(body, "paket_id")
	set := []string{}
	args := []any{}
	index := 1
	for key, column := range map[string]string{
		"materi_judul": "materi_judul", "materi_deskripsi": "materi_deskripsi", "materi_tipe": "materi_tipe",
		"materi_url": "materi_url", "materi_konten": "materi_konten",
	} {
		if value, exists := body[key]; exists {
			if value == nil {
				set = append(set, fmt.Sprintf("%s = $%d", column, index))
				args = append(args, nil)
			} else {
				set = append(set, fmt.Sprintf("%s = $%d", column, index))
				args = append(args, strings.TrimSpace(bodyString(body, key)))
			}
			index++
		}
	}
	for _, key := range []string{"materi_urutan"} {
		if _, exists := body[key]; exists {
			set = append(set, fmt.Sprintf("%s = $%d", key, index))
			args = append(args, bodyInt(body, key, 0))
			index++
		}
	}
	for _, key := range []string{"is_wajib", "is_active"} {
		if _, exists := body[key]; exists {
			set = append(set, fmt.Sprintf("%s = $%d", key, index))
			args = append(args, bodyBool(body, key, true))
			index++
		}
	}
	if len(set) == 0 {
		var materiID, urutan int
		var judul, tipe string
		var deskripsi, url, konten *string
		var wajib, active bool
		var created, updated time.Time
		if err := s.Pool.QueryRow(r.Context(), `
			SELECT materi_id, paket_id, materi_judul, materi_deskripsi, materi_tipe, materi_url,
				materi_konten, materi_urutan, is_wajib, is_active, created_at, updated_at
			FROM lms_materi WHERE materi_id = $1`, id).Scan(&materiID, &paketID, &judul, &deskripsi, &tipe,
			&url, &konten, &urutan, &wajib, &active, &created, &updated); err != nil {
			return err
		}
		httpx.Success(w, map[string]any{
			"materi_id": materiID, "paket_id": paketID, "materi_judul": judul, "materi_deskripsi": deskripsi,
			"materi_tipe": tipe, "materi_url": url, "materi_konten": konten, "materi_urutan": urutan,
			"is_wajib": wajib, "is_active": active, "created_at": created, "updated_at": updated,
		}, "Modul kelas berhasil diperbarui")
		return nil
	}
	set = append(set, "updated_at = now()")
	args = append(args, id)
	var materiID, urutan int
	var judul, tipe string
	var deskripsi, url, konten *string
	var wajib, active bool
	var created, updated time.Time
	if err := s.Pool.QueryRow(r.Context(), fmt.Sprintf(`
		UPDATE lms_materi SET %s WHERE materi_id = $%d
		RETURNING materi_id, paket_id, materi_judul, materi_deskripsi, materi_tipe, materi_url,
			materi_konten, materi_urutan, is_wajib, is_active, created_at, updated_at`,
		strings.Join(set, ", "), index), args...).Scan(&materiID, &paketID, &judul, &deskripsi, &tipe,
		&url, &konten, &urutan, &wajib, &active, &created, &updated); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_MODULE_UPDATE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "paket_id": paketID, "materi_id": id,
	})
	httpx.Success(w, map[string]any{
		"materi_id": materiID, "paket_id": paketID, "materi_judul": judul, "materi_deskripsi": deskripsi,
		"materi_tipe": tipe, "materi_url": url, "materi_konten": konten, "materi_urutan": urutan,
		"is_wajib": wajib, "is_active": active, "created_at": created, "updated_at": updated,
	}, "Modul kelas berhasil diperbarui")
	return nil
}

func (s *Server) businessRemoveModule(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	member, err := s.requireMembership(r.Context(), principal.UserID, nil)
	if err != nil {
		return err
	}
	if err := assertManager(member); err != nil {
		return err
	}
	if err := assertOrgActive(member); err != nil {
		return err
	}
	var paketID int
	if err := s.Pool.QueryRow(r.Context(), `SELECT paket_id FROM lms_materi WHERE materi_id = $1`, id).Scan(&paketID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Materi tidak ditemukan", "LMS_MATERIAL_NOT_FOUND")
		}
		return err
	}
	if _, err := s.requireOrgPackage(r.Context(), paketID, member.Organization.OrganizationID); err != nil {
		return err
	}
	if _, err := s.Pool.Exec(r.Context(), `DELETE FROM lms_materi WHERE materi_id = $1`, id); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_MODULE_DELETE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "paket_id": paketID, "materi_id": id,
	})
	httpx.Success(w, nil, "Modul kelas berhasil dihapus.")
	return nil
}

func scoringMaxForMode(paket Paket) float64 {
	if exam.NormalizeFinalScoreMode(paket.SkalaNilai) == "poin_mentah" {
		return 9999
	}
	return 100
}
