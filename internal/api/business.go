package api

import (
	"context"
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

type Organization struct {
	OrganizationID              int        `json:"organization_id"`
	OrganizationNama            string     `json:"organization_nama"`
	OrganizationSlug            string     `json:"organization_slug"`
	OrganizationEmail           string     `json:"organization_email"`
	OrganizationPhone           *string    `json:"organization_phone"`
	OrganizationAlamat          *string    `json:"organization_alamat"`
	OrganizationDeskripsi       *string    `json:"organization_deskripsi"`
	OrganizationKode            string     `json:"organization_kode"`
	OrganizationStatus          string     `json:"organization_status"`
	OrganizationRejectionAlasan *string    `json:"organization_rejection_alasan"`
	ApprovedBy                  *int       `json:"approved_by"`
	ApprovedAt                  *time.Time `json:"approved_at"`
	CreatedBy                   *int       `json:"created_by"`
	CreatedAt                   time.Time  `json:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at"`
}

type Membership struct {
	MemberID     int
	MemberRole   string
	MemberStatus string
	JoinedAt     time.Time
	Organization Organization
}

func scanOrganization(row pgx.Row, memberID *int, role, status *string, joinedAt *time.Time) (Membership, error) {
	var member Membership
	var memberIDValue *int
	var roleValue, statusValue *string
	var joined *time.Time
	err := row.Scan(
		&member.Organization.OrganizationID, &member.Organization.OrganizationNama,
		&member.Organization.OrganizationSlug, &member.Organization.OrganizationEmail,
		&member.Organization.OrganizationPhone, &member.Organization.OrganizationAlamat,
		&member.Organization.OrganizationDeskripsi, &member.Organization.OrganizationKode,
		&member.Organization.OrganizationStatus, &member.Organization.OrganizationRejectionAlasan,
		&member.Organization.ApprovedBy, &member.Organization.ApprovedAt,
		&member.Organization.CreatedBy, &member.Organization.CreatedAt, &member.Organization.UpdatedAt,
		&memberIDValue, &roleValue, &statusValue, &joined,
	)
	if memberIDValue != nil {
		member.MemberID = *memberIDValue
	}
	if roleValue != nil {
		member.MemberRole = *roleValue
	}
	if statusValue != nil {
		member.MemberStatus = *statusValue
	}
	if joined != nil {
		member.JoinedAt = *joined
	}
	if memberID != nil && memberIDValue != nil {
		*memberID = *memberIDValue
	}
	if role != nil && roleValue != nil {
		*role = *roleValue
	}
	if status != nil && statusValue != nil {
		*status = *statusValue
	}
	if joinedAt != nil && joined != nil {
		*joinedAt = *joined
	}
	return member, err
}

const organizationColumns = `o.organization_id, o.organization_nama, o.organization_slug, o.organization_email,
	o.organization_phone, o.organization_alamat, o.organization_deskripsi, o.organization_kode,
	o.organization_status, o.organization_rejection_alasan, o.approved_by, o.approved_at,
	o.created_by, o.created_at, o.updated_at`

func (s *Server) listMemberships(ctx context.Context, userID int) ([]Membership, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT `+organizationColumns+`, m.member_id, m.member_role, m.member_status, m.joined_at
		FROM organization_member m JOIN organization o ON o.organization_id = m.organization_id
		WHERE m.user_id = $1 AND m.member_status = 'ACTIVE'
		ORDER BY m.joined_at ASC, m.member_id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Membership{}
	for rows.Next() {
		member, err := scanOrganization(rows, nil, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		items = append(items, member)
	}
	return items, rows.Err()
}

func (s *Server) requireMembership(ctx context.Context, userID int, organizationID *int) (Membership, error) {
	if organizationID != nil {
		row := s.Pool.QueryRow(ctx, `
			SELECT `+organizationColumns+`, m.member_id, m.member_role, m.member_status, m.joined_at
			FROM organization_member m JOIN organization o ON o.organization_id = m.organization_id
			WHERE m.user_id = $1 AND m.organization_id = $2 AND m.member_status = 'ACTIVE'`,
			userID, *organizationID)
		member, err := scanOrganization(row, nil, nil, nil, nil)
		if errors.Is(err, pgx.ErrNoRows) {
			return Membership{}, httpx.Forbidden("Akun Anda belum terhubung ke organisasi. Masukkan kode organisasi terlebih dahulu.", "ORG_MEMBERSHIP_REQUIRED")
		}
		return member, err
	}
	items, err := s.listMemberships(ctx, userID)
	if err != nil {
		return Membership{}, err
	}
	if len(items) == 0 {
		return Membership{}, httpx.Forbidden("Akun Anda belum terhubung ke organisasi. Masukkan kode organisasi terlebih dahulu.", "ORG_MEMBERSHIP_REQUIRED")
	}
	for _, item := range items {
		if item.Organization.OrganizationStatus == "ACTIVE" {
			return item, nil
		}
	}
	return items[0], nil
}

func isManager(role string) bool {
	return role == "OWNER" || role == "ADMIN"
}

func isViewerRole(role string) bool {
	return role == "OWNER" || role == "ADMIN" || role == "VIEWER"
}

func assertManager(member Membership) error {
	if !isManager(member.MemberRole) {
		return httpx.Forbidden("Hanya owner atau admin organisasi yang dapat melakukan tindakan ini.", "ORG_MANAGER_REQUIRED")
	}
	return nil
}

func assertViewer(member Membership) error {
	if !isViewerRole(member.MemberRole) {
		return httpx.Forbidden("Peran Anda tidak memiliki akses ke laporan perusahaan.", "ORG_VIEWER_REQUIRED")
	}
	return nil
}

func assertOrgActive(member Membership) error {
	status := member.Organization.OrganizationStatus
	if status == "ACTIVE" {
		return nil
	}
	if status == "PENDING_APPROVAL" {
		return httpx.Forbidden("Organisasi masih menunggu persetujuan Superadmin.", "ORG_NOT_ACTIVE")
	}
	return httpx.Forbidden("Organisasi tidak aktif. Hubungi administrator EduMaster.", "ORG_NOT_ACTIVE")
}

func organizationForMember(member Membership) map[string]any {
	org := member.Organization
	payload := map[string]any{
		"organization_id":               org.OrganizationID,
		"organization_nama":             org.OrganizationNama,
		"organization_slug":             org.OrganizationSlug,
		"organization_email":            org.OrganizationEmail,
		"organization_phone":            org.OrganizationPhone,
		"organization_alamat":           org.OrganizationAlamat,
		"organization_deskripsi":        org.OrganizationDeskripsi,
		"organization_status":           org.OrganizationStatus,
		"organization_rejection_alasan": org.OrganizationRejectionAlasan,
		"approved_by":                   org.ApprovedBy,
		"approved_at":                   org.ApprovedAt,
		"created_by":                    org.CreatedBy,
		"created_at":                    org.CreatedAt,
		"updated_at":                    org.UpdatedAt,
	}
	if isManager(member.MemberRole) {
		payload["organization_kode"] = org.OrganizationKode
	}
	return payload
}

func (s *Server) registerBusiness(api chi.Router) {
	api.Route("/business", func(r chi.Router) {
		r.With(s.authLimit.Middleware).Post("/register", httpx.Wrap(s.businessRegister))
		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Authenticate)
			r.Use(middleware.RequirePermission(s.Permissions, "business.workspace"))
			r.Get("/me", httpx.Wrap(s.businessMe))
			r.Post("/join", httpx.Wrap(s.businessJoin))
			r.Post("/invites/accept", httpx.Wrap(s.acceptInvite))
			r.Get("/packages", httpx.Wrap(s.businessMyPackages))
			r.Get("/stats", httpx.Wrap(s.businessStats))
			r.Get("/results", httpx.Wrap(s.businessResults))
			r.Post("/packages/{id}/unlock", httpx.Wrap(s.businessUnlock))
		})
		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Authenticate)
			r.Use(middleware.RequirePermission(s.Permissions, "business.manage"))
			r.Get("/manage/packages", httpx.Wrap(s.businessListPackages))
			r.Post("/manage/packages", httpx.Wrap(s.businessCreatePackage))
			r.Put("/manage/packages/{id}", httpx.Wrap(s.businessUpdatePackage))
			r.Delete("/manage/packages/{id}", httpx.Wrap(s.businessRemovePackage))
			r.Post("/manage/uploads/images", httpx.Wrap(s.businessUploadImage))
			r.Get("/manage/questions", httpx.Wrap(s.businessListBankQuestions))
			r.Post("/manage/questions", httpx.Wrap(s.businessCreateBankQuestion))
			r.Put("/manage/questions/{id}", httpx.Wrap(s.businessUpdateQuestion))
			r.Delete("/manage/questions/{id}", httpx.Wrap(s.businessRemoveQuestion))
			r.Get("/manage/packages/{id}/questions", httpx.Wrap(s.businessListPackageQuestions))
			r.Post("/manage/packages/{id}/questions", httpx.Wrap(s.businessCreatePackageQuestion))
			r.Get("/manage/packages/{id}/participants", httpx.Wrap(s.businessListParticipants))
			r.Post("/manage/packages/{id}/participants", httpx.Wrap(s.businessAddParticipants))
			r.Delete("/manage/packages/{id}/participants/{participant_id}", httpx.Wrap(s.businessRemoveParticipant))
			r.Post("/manage/packages/{id}/participants/{participant_id}/regenerate", httpx.Wrap(s.businessRegenerateParticipant))
			r.Get("/manage/packages/{id}/modules", httpx.Wrap(s.businessListModules))
			r.Post("/manage/packages/{id}/modules", httpx.Wrap(s.businessCreateModule))
			r.Put("/manage/modules/{id}", httpx.Wrap(s.businessUpdateModule))
			r.Delete("/manage/modules/{id}", httpx.Wrap(s.businessRemoveModule))
			r.Get("/manage/members", httpx.Wrap(s.businessListMembers))
			r.Post("/manage/members/invite", httpx.Wrap(s.businessInviteMember))
			r.Patch("/manage/members/{id}", httpx.Wrap(s.businessUpdateMemberRole))
			r.Delete("/manage/members/{id}", httpx.Wrap(s.businessRemoveMember))
		})
		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Authenticate)
			r.Use(middleware.RequirePermission(s.Permissions, "admin.organizations.view"))
			r.Get("/admin/organizations", httpx.Wrap(s.businessAdminList))
		})
		r.Group(func(r chi.Router) {
			r.Use(s.Auth.Authenticate)
			r.Use(middleware.RequirePermission(s.Permissions, "admin.organizations.manage"))
			r.Patch("/admin/organizations/{id}/status", httpx.Wrap(s.businessAdminStatus))
		})
	})
}

func (s *Server) businessMe(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	memberships, err := s.listMemberships(r.Context(), principal.UserID)
	if err != nil {
		return err
	}
	if len(memberships) == 0 {
		httpx.Success(w, map[string]any{"membership": nil, "memberships": []any{}}, "")
		return nil
	}
	primary := memberships[0]
	for _, item := range memberships {
		if item.Organization.OrganizationStatus == "ACTIVE" {
			primary = item
			break
		}
	}
	payload := func(item Membership) map[string]any {
		return map[string]any{
			"organization_id": item.Organization.OrganizationID,
			"member_role":     item.MemberRole,
			"joined_at":       item.JoinedAt,
			"organization":    organizationForMember(item),
			"permissions": map[string]bool{
				"manage":       isManager(item.MemberRole),
				"view_results": isViewerRole(item.MemberRole),
			},
		}
	}
	all := make([]map[string]any, 0, len(memberships))
	for _, item := range memberships {
		all = append(all, map[string]any{
			"organization_id": item.Organization.OrganizationID,
			"member_role":     item.MemberRole,
			"organization":    organizationForMember(item),
		})
	}
	httpx.Success(w, map[string]any{"membership": payload(primary), "memberships": all}, "")
	return nil
}

func (s *Server) requireOrgPackage(ctx context.Context, paketID, organizationID int) (Paket, error) {
	item, err := scanPaket(s.Pool.QueryRow(ctx, `SELECT `+packageSelect+` FROM paket_ujian p WHERE p.paket_id = $1`, paketID))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (item.OrganizationID == nil || *item.OrganizationID != organizationID)) {
		return Paket{}, httpx.NotFound("Paket privat tidak ditemukan", "PACKAGE_NOT_FOUND")
	}
	return item, err
}

func (s *Server) businessListPackages(w http.ResponseWriter, r *http.Request) error {
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
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"p.organization_id = $1"}
	args := []any{member.Organization.OrganizationID}
	index := 2
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tipe"))) {
	case "lms":
		conditions = append(conditions, "p.paket_tipe = 'elearning'")
	case "exam":
		conditions = append(conditions, "p.paket_tipe <> 'elearning'")
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("p.paket_nama ILIKE $%d", index))
		args = append(args, "%"+search+"%")
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM paket_ujian p WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`SELECT %s FROM paket_ujian p WHERE %s ORDER BY p.created_at DESC, p.paket_id DESC LIMIT $%d OFFSET $%d`,
		packageSelect, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Paket{}
	for rows.Next() {
		item, err := scanPaket(rows)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)),
		withExtras(map[string]any{
			"organization": organizationForMember(member),
			"member_role":  member.MemberRole,
		}))
	return nil
}

func (s *Server) businessMyPackages(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	member, err := s.requireMembership(r.Context(), principal.UserID, nil)
	if err != nil {
		return err
	}
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	canManage := isManager(member.MemberRole)
	extra := map[string]any{"organization": organizationForMember(member), "member_role": member.MemberRole, "pending_approval": false}
	if member.Organization.OrganizationStatus == "PENDING_APPROVAL" {
		extra["pending_approval"] = true
		httpx.Success(w, []any{}, "", httpx.WithMeta(httpx.Meta(0, page, limit)), withExtras(extra))
		return nil
	}
	if err := assertOrgActive(member); err != nil {
		return err
	}
	conditions := []string{"p.organization_id = $1", "p.is_active = true"}
	args := []any{member.Organization.OrganizationID}
	index := 2
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("p.paket_nama ILIKE $%d", index))
		args = append(args, "%"+search+"%")
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM paket_ujian p WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`SELECT %s FROM paket_ujian p WHERE %s ORDER BY p.created_at DESC, p.paket_id DESC LIMIT $%d OFFSET $%d`,
		packageSelect, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	accessRows, err := s.Pool.Query(r.Context(), `SELECT paket_id, access_id, expired_at, sisa_attempt FROM user_package_access WHERE user_id = $1`, principal.UserID)
	if err != nil {
		rows.Close()
		return err
	}
	type accessInfo struct {
		ID       int
		Expiry   time.Time
		Attempts int
	}
	accessMap := map[int]accessInfo{}
	for accessRows.Next() {
		var paketID int
		var info accessInfo
		if err := accessRows.Scan(&paketID, &info.ID, &info.Expiry, &info.Attempts); err == nil {
			accessMap[paketID] = info
		}
	}
	accessRows.Close()
	items := []map[string]any{}
	for rows.Next() {
		paket, err := scanPaket(rows)
		if err != nil {
			rows.Close()
			return err
		}
		payload := map[string]any{}
		encoded, _ := json.Marshal(paket)
		_ = json.Unmarshal(encoded, &payload)
		if !canManage {
			delete(payload, "paket_access_code")
		}
		if access, exists := accessMap[paket.PaketID]; exists {
			active := access.Expiry.After(time.Now())
			payload["access"] = map[string]any{
				"access_id": access.ID, "expired_at": access.Expiry,
				"sisa_attempt": access.Attempts, "active": active,
			}
			payload["locked"] = !active
		} else {
			payload["access"] = nil
			payload["locked"] = true
		}
		items = append(items, payload)
	}
	rows.Close()
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)), withExtras(extra))
	return nil
}

func normalizeBusinessBank(value string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func (s *Server) businessBankConditions(r *http.Request, organizationID int) (string, []any, int) {
	conditions := []string{"s.organization_id = $1", "s.organization_id IS NOT NULL"}
	args := []any{organizationID}
	index := 2
	for _, filter := range []struct{ query, column string }{
		{"bank_jenis", "s.bank_jenis"}, {"bank_tipe", "s.bank_tipe"}, {"bank_batch", "s.bank_batch"},
		{"kesulitan", "s.soal_kesulitan"}, {"tipe", "s.soal_tipe"},
	} {
		if value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(filter.query))); value != "" {
			conditions = append(conditions, fmt.Sprintf("%s = $%d", filter.column, index))
			args = append(args, value)
			index++
		}
	}
	if kategori := strings.TrimSpace(r.URL.Query().Get("kategori")); kategori != "" {
		conditions = append(conditions, fmt.Sprintf("s.soal_kategori ILIKE $%d", index))
		args = append(args, kategori)
		index++
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("s.soal_pertanyaan ILIKE $%d", index))
		args = append(args, "%"+search+"%")
		index++
	}
	if active := httpx.QueryBoolPtr(r, "aktif"); active != nil {
		conditions = append(conditions, fmt.Sprintf("s.soal_aktif = $%d", index))
		args = append(args, *active)
		index++
	}
	return strings.Join(conditions, " AND "), args, index
}

func (s *Server) businessListBankQuestions(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	member, err := s.requireMembership(r.Context(), principal.UserID, nil)
	if err != nil {
		return err
	}
	if err := assertViewer(member); err != nil {
		return err
	}
	if err := assertOrgActive(member); err != nil {
		return err
	}
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	where, args, index := s.businessBankConditions(r, member.Organization.OrganizationID)
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
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return nil
}

func (s *Server) createBusinessQuestion(ctx context.Context, actorID, organizationID int, paketID *int, body map[string]any) (Soal, error) {
	tipe := bodyString(body, "soal_tipe")
	if tipe == "" {
		tipe = "exact"
	}
	if tipe != "exact" && tipe != "gradasi" && tipe != "essay" {
		return Soal{}, httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "soal_tipe", "message": "Tipe soal tidak valid"}})
	}
	if err := normalizeQuestionMediaFields(body); err != nil {
		return Soal{}, err
	}
	options := normalizeQuestionOptions(body["soal_opsi"])
	if err := validateOptions(tipe, options); err != nil {
		return Soal{}, err
	}
	category := strings.TrimSpace(bodyString(body, "soal_kategori"))
	question := strings.TrimSpace(bodyString(body, "soal_pertanyaan"))
	if category == "" || (len([]rune(question)) < 5 && bodyString(body, "soal_gambar_url") == "") {
		return Soal{}, httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "soal_pertanyaan", "message": "Kategori dan pertanyaan/gambar wajib valid"}})
	}
	bankJenis := "tryout"
	if bodyString(body, "bank_jenis") == "ujian_real" {
		bankJenis = "ujian_real"
	}
	bankTipe := strings.ToLower(strings.TrimSpace(bodyString(body, "bank_tipe")))
	bankBatch := strings.ToLower(strings.TrimSpace(bodyString(body, "bank_batch")))
	if paketID != nil {
		var jenis, tipePaket string
		var batch *string
		if err := s.Pool.QueryRow(ctx, `SELECT jenis_paket, paket_tipe, bank_batch FROM paket_ujian WHERE paket_id = $1`, *paketID).Scan(&jenis, &tipePaket, &batch); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Soal{}, httpx.NotFound("Paket privat tidak ditemukan", "PACKAGE_NOT_FOUND")
			}
			return Soal{}, err
		}
		if jenis != "ujian_real" {
			jenis = "tryout"
		}
		bankJenis, bankTipe = jenis, strings.ToLower(tipePaket)
		if batch != nil {
			bankBatch = strings.ToLower(*batch)
		}
	}
	if bankTipe == "" {
		return Soal{}, httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "bank_tipe", "message": "bank_tipe wajib diisi"}})
	}
	if bankBatch == "" {
		bankBatch = fmt.Sprintf("org-%d", organizationID)
	}
	kesulitan := strings.ToLower(bodyStringDefault(body, "soal_kesulitan", "sedang"))
	if kesulitan != "mudah" && kesulitan != "sedang" && kesulitan != "sulit" {
		kesulitan = "sedang"
	}
	score := bodyFloat(body, "soal_skor_maks", 1)
	if score <= 0 || score > 1000 {
		return Soal{}, httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "soal_skor_maks", "message": "Skor maksimal harus 1-1000"}})
	}
	var optionsValue any
	if tipe != "essay" {
		optionsValue = options
	}
	item, err := scanSoal(s.Pool.QueryRow(ctx, `
		INSERT INTO soal_bank (paket_id, bank_jenis, bank_tipe, bank_batch, soal_kategori, soal_kesulitan,
			soal_tipe, soal_pertanyaan, soal_gambar_url, soal_gambar_alt, soal_opsi, soal_skor_maks,
			soal_aktif, organization_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,now(),now())
		RETURNING soal_id, paket_id, bank_jenis, bank_tipe, bank_batch, soal_kategori, soal_kesulitan,
			soal_tipe, soal_pertanyaan, soal_gambar_url, soal_gambar_alt, soal_opsi, soal_skor_maks,
			soal_aktif, organization_id, created_at, updated_at,
			NULL::int, NULL::int, NULL::text, NULL::text, NULL::timestamptz, NULL::timestamptz`,
		paketID, bankJenis, bankTipe, bankBatch, category, kesulitan, tipe, question,
		body["soal_gambar_url"], body["soal_gambar_alt"], jsonArgument(optionsValue), score,
		bodyBool(body, "soal_aktif", true), organizationID))
	return item, err
}

// businessUploadImage reuses the hardened image pipeline while enforcing the
// same organization-manager and active-organization checks as the rest of the
// business workspace. Students with business.manage cannot upload arbitrary
// media through this route.
func (s *Server) businessUploadImage(w http.ResponseWriter, r *http.Request) error {
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
	return s.uploadImage(w, r)
}

func (s *Server) businessCreateBankQuestion(w http.ResponseWriter, r *http.Request) error {
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
	item, err := s.createBusinessQuestion(r.Context(), principal.UserID, member.Organization.OrganizationID, nil, body)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_QUESTION_CREATE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "soal_id": item.SoalID, "source": "bank",
	})
	httpx.Created(w, item, "Soal bank berhasil dibuat.")
	return nil
}

func (s *Server) businessListPackageQuestions(w http.ResponseWriter, r *http.Request) error {
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
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"s.paket_id = $1", "s.organization_id = $2"}
	args := []any{id, member.Organization.OrganizationID}
	index := 3
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
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)),
		withExtras(map[string]any{"paket_id": id}))
	return nil
}

func (s *Server) businessCreatePackageQuestion(w http.ResponseWriter, r *http.Request) error {
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
	item, err := s.createBusinessQuestion(r.Context(), principal.UserID, member.Organization.OrganizationID, &id, body)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_QUESTION_CREATE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "paket_id": id, "soal_id": item.SoalID,
	})
	httpx.Created(w, item, "Soal privat berhasil dibuat.")
	return nil
}

func (s *Server) businessUpdateQuestion(w http.ResponseWriter, r *http.Request) error {
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
	var organizationID *int
	if err := s.Pool.QueryRow(r.Context(), `SELECT organization_id FROM soal_bank WHERE soal_id = $1`, id).Scan(&organizationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Soal privat tidak ditemukan", "QUESTION_NOT_FOUND")
		}
		return err
	}
	if organizationID == nil || *organizationID != member.Organization.OrganizationID {
		return httpx.NotFound("Soal privat tidak ditemukan", "QUESTION_NOT_FOUND")
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	tipe := bodyStringDefault(body, "soal_tipe", "exact")
	if err := normalizeQuestionMediaFields(body); err != nil {
		return err
	}
	if _, exists := body["soal_opsi"]; exists {
		options := normalizeQuestionOptions(body["soal_opsi"])
		if err := validateOptions(tipe, options); err != nil {
			return err
		}
	}
	delete(body, "organization_id")
	delete(body, "paket_id")
	set := []string{}
	args := []any{}
	index := 1
	for key, column := range map[string]string{
		"soal_kategori": "soal_kategori", "soal_kesulitan": "soal_kesulitan", "soal_tipe": "soal_tipe",
		"soal_pertanyaan": "soal_pertanyaan", "bank_jenis": "bank_jenis", "bank_tipe": "bank_tipe", "bank_batch": "bank_batch",
	} {
		if value, exists := body[key]; exists {
			text, _ := value.(string)
			text = strings.TrimSpace(text)
			if key == "bank_jenis" || key == "bank_tipe" || key == "bank_batch" {
				text = strings.ToLower(text)
			}
			set = append(set, fmt.Sprintf("%s = $%d", column, index))
			args = append(args, text)
			index++
		}
	}
	if value, exists := body["soal_gambar_url"]; exists {
		set = append(set, fmt.Sprintf("soal_gambar_url = $%d", index))
		args = append(args, value)
		index++
	}
	if value, exists := body["soal_gambar_alt"]; exists {
		set = append(set, fmt.Sprintf("soal_gambar_alt = $%d", index))
		args = append(args, value)
		index++
	}
	if value, exists := body["soal_opsi"]; exists {
		set = append(set, fmt.Sprintf("soal_opsi = $%d::jsonb", index))
		args = append(args, jsonArgument(value))
		index++
	}
	if _, exists := body["soal_skor_maks"]; exists {
		set = append(set, fmt.Sprintf("soal_skor_maks = $%d", index))
		args = append(args, bodyFloat(body, "soal_skor_maks", 1))
		index++
	}
	if _, exists := body["soal_aktif"]; exists {
		set = append(set, fmt.Sprintf("soal_aktif = $%d", index))
		args = append(args, bodyBool(body, "soal_aktif", true))
		index++
	}
	set = append(set, "updated_at = now()")
	args = append(args, id)
	item, err := scanSoal(s.Pool.QueryRow(r.Context(), fmt.Sprintf(`
		UPDATE soal_bank SET %s WHERE soal_id = $%d
		RETURNING soal_id, paket_id, bank_jenis, bank_tipe, bank_batch, soal_kategori, soal_kesulitan,
			soal_tipe, soal_pertanyaan, soal_gambar_url, soal_gambar_alt, soal_opsi, soal_skor_maks,
			soal_aktif, organization_id, created_at, updated_at,
			(SELECT pembahasan_id FROM soal_pembahasan WHERE soal_id = $%d),
			(SELECT soal_id FROM soal_pembahasan WHERE soal_id = $%d),
			(SELECT pembahasan_teks FROM soal_pembahasan WHERE soal_id = $%d),
			(SELECT pembahasan_video FROM soal_pembahasan WHERE soal_id = $%d),
			(SELECT created_at FROM soal_pembahasan WHERE soal_id = $%d),
			(SELECT updated_at FROM soal_pembahasan WHERE soal_id = $%d)`,
		strings.Join(set, ", "), index, index, index, index, index, index, index), args...))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_QUESTION_UPDATE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "soal_id": id,
	})
	httpx.Success(w, item, "Soal privat berhasil diperbarui")
	return nil
}

func (s *Server) businessRemoveQuestion(w http.ResponseWriter, r *http.Request) error {
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
	var organizationID *int
	if err := s.Pool.QueryRow(r.Context(), `SELECT organization_id FROM soal_bank WHERE soal_id = $1`, id).Scan(&organizationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Soal privat tidak ditemukan", "QUESTION_NOT_FOUND")
		}
		return err
	}
	if organizationID == nil || *organizationID != member.Organization.OrganizationID {
		return httpx.NotFound("Soal privat tidak ditemukan", "QUESTION_NOT_FOUND")
	}
	if _, err := s.Pool.Exec(r.Context(), `UPDATE soal_bank SET soal_aktif = false, updated_at = now() WHERE soal_id = $1`, id); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_QUESTION_DELETE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "soal_id": id,
	})
	httpx.Success(w, map[string]any{"soal_id": id, "soal_aktif": false}, "Soal privat dinonaktifkan")
	return nil
}
