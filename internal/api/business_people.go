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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/codegen"
	"github.com/edumaster/backend-go/internal/exam"
	"github.com/edumaster/backend-go/internal/httpx"
)

func normalizeParticipantEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// rowQuerier menyatukan *pgxpool.Pool dan pgx.Tx agar penambahan peserta dapat
// ikut dalam transaksi pembuatan paket.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Server) addParticipantEmails(ctx context.Context, db rowQuerier, paketID int, emails []string, createdBy int) (int, []string, error) {
	created := 0
	skipped := []string{}
	seen := map[string]bool{}
	for _, raw := range emails {
		email := normalizeParticipantEmail(raw)
		if email == "" || seen[email] || !emailPattern.MatchString(email) {
			skipped = append(skipped, email)
			continue
		}
		seen[email] = true
		inserted := false
		for attempt := 0; attempt < 8; attempt++ {
			var participantID int
			err := db.QueryRow(ctx, `
				INSERT INTO package_participant (paket_id, participant_email, access_code, status, created_by, created_at, updated_at)
				VALUES ($1, $2, $3, 'UNUSED', $4, now(), now())
				ON CONFLICT (paket_id, participant_email) DO NOTHING
				RETURNING participant_id`, paketID, email, codegen.ParticipantCode(), createdBy).Scan(&participantID)
			if err == nil {
				created++
				inserted = true
				break
			}
			if errors.Is(err, pgx.ErrNoRows) {
				// Email sudah terdaftar pada paket ini.
				break
			}
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				// Tabrakan access_code; coba kode baru.
				continue
			}
			return created, skipped, err
		}
		if !inserted {
			skipped = append(skipped, email)
		}
	}
	return created, skipped, nil
}

func (s *Server) businessListParticipants(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
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
	paket, err := s.requireOrgPackage(r.Context(), id, member.Organization.OrganizationID)
	if err != nil {
		return err
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT pp.participant_id, pp.participant_email, pp.participant_nama, pp.access_code,
		       pp.status, pp.redeemed_at, pp.user_id, u.user_nama, u.user_email
		FROM package_participant pp LEFT JOIN "user" u ON u.user_id = pp.user_id
		WHERE pp.paket_id = $1 ORDER BY pp.participant_id ASC`, id)
	if err != nil {
		return err
	}
	type participantRow struct {
		ID        int
		Email     string
		Nama      *string
		Code      string
		Status    string
		Redeemed  *time.Time
		UserID    *int
		UserNama  *string
		UserEmail *string
	}
	participants := []participantRow{}
	for rows.Next() {
		var item participantRow
		if err := rows.Scan(&item.ID, &item.Email, &item.Nama, &item.Code, &item.Status,
			&item.Redeemed, &item.UserID, &item.UserNama, &item.UserEmail); err != nil {
			rows.Close()
			return err
		}
		participants = append(participants, item)
	}
	rows.Close()

	accessRows, err := s.Pool.Query(r.Context(), `
		SELECT a.user_id, a.access_id, a.sisa_attempt, a.expired_at, u.user_nama, u.user_email
		FROM user_package_access a JOIN "user" u ON u.user_id = a.user_id
		WHERE a.paket_id = $1 ORDER BY a.access_id ASC`, id)
	if err != nil {
		return err
	}
	type accessRow struct {
		UserID   int
		AccessID int
		Attempts int
		Expiry   time.Time
		Nama     string
		Email    string
	}
	accesses := []accessRow{}
	for accessRows.Next() {
		var item accessRow
		if err := accessRows.Scan(&item.UserID, &item.AccessID, &item.Attempts, &item.Expiry, &item.Nama, &item.Email); err != nil {
			accessRows.Close()
			return err
		}
		accesses = append(accesses, item)
	}
	accessRows.Close()
	accessByUser := map[int]accessRow{}
	for _, item := range accesses {
		accessByUser[item.UserID] = item
	}

	sessionRows, err := s.Pool.Query(r.Context(), `
		SELECT user_id, status, final_score, is_passed, submitted_at
		FROM exam_session WHERE paket_id = $1 ORDER BY session_id DESC`, id)
	if err != nil {
		return err
	}
	type sessionStat struct {
		Status    string
		Score     *float64
		Passed    *bool
		Submitted *time.Time
	}
	sessionMap := map[int][]sessionStat{}
	for sessionRows.Next() {
		var userID int
		var stat sessionStat
		if err := sessionRows.Scan(&userID, &stat.Status, &stat.Score, &stat.Passed, &stat.Submitted); err != nil {
			sessionRows.Close()
			return err
		}
		sessionMap[userID] = append(sessionMap[userID], stat)
	}
	sessionRows.Close()

	isElearning := paket.PaketTipe == "elearning"
	totalMaterials := 0
	progressMap := map[int]int{}
	if isElearning {
		_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM lms_materi WHERE paket_id = $1 AND is_active = true`, id).Scan(&totalMaterials)
		progressRows, err := s.Pool.Query(r.Context(), `
			SELECT p.user_id, count(*) FROM lms_progress p JOIN lms_materi m ON m.materi_id = p.materi_id
			WHERE m.paket_id = $1 GROUP BY p.user_id`, id)
		if err != nil {
			return err
		}
		for progressRows.Next() {
			var userID, count int
			if err := progressRows.Scan(&userID, &count); err == nil {
				progressMap[userID] = count
			}
		}
		progressRows.Close()
	}

	statsFor := func(userID *int) map[string]any {
		result := map[string]any{"attempts": 0, "last_status": nil, "last_submitted_at": nil, "best_score": nil, "best_is_passed": nil}
		if userID == nil {
			return result
		}
		items := sessionMap[*userID]
		result["attempts"] = len(items)
		if len(items) == 0 {
			return result
		}
		result["last_status"] = items[0].Status
		result["last_submitted_at"] = items[0].Submitted
		var best *sessionStat
		for index := range items {
			item := &items[index]
			if item.Status != "completed" || item.Score == nil {
				continue
			}
			if best == nil || *item.Score > *best.Score {
				best = item
			}
		}
		if best != nil {
			result["best_score"] = best.Score
			result["best_is_passed"] = best.Passed
		}
		return result
	}

	linked := map[int]bool{}
	items := []map[string]any{}
	for _, participant := range participants {
		if participant.UserID != nil {
			linked[*participant.UserID] = true
		}
		var accessPayload any
		if participant.UserID != nil {
			if access, exists := accessByUser[*participant.UserID]; exists {
				accessPayload = map[string]any{
					"access_id": access.AccessID, "sisa_attempt": access.Attempts, "expired_at": access.Expiry,
				}
			}
		}
		item := map[string]any{
			"participant_id": participant.ID, "participant_email": participant.Email,
			"participant_nama": participant.Nama, "access_code": participant.Code,
			"status": participant.Status, "redeemed_at": participant.Redeemed,
			"user_id": participant.UserID, "user_nama": participant.UserNama, "user_email": participant.UserEmail,
			"access": accessPayload,
		}
		for key, value := range statsFor(participant.UserID) {
			item[key] = value
		}
		if isElearning {
			item["total_materials"] = totalMaterials
			completed := 0
			if participant.UserID != nil {
				completed = progressMap[*participant.UserID]
			}
			item["completed_materials"] = completed
		}
		items = append(items, item)
	}
	legacy := []map[string]any{}
	for _, access := range accesses {
		if linked[access.UserID] {
			continue
		}
		item := map[string]any{
			"participant_id": nil, "participant_email": access.Email, "participant_nama": access.Nama,
			"access_code": nil, "status": "LEGACY", "redeemed_at": nil,
			"user_id": access.UserID, "user_nama": access.Nama, "user_email": access.Email,
			"access": map[string]any{
				"access_id": access.AccessID, "sisa_attempt": access.Attempts, "expired_at": access.Expiry,
			},
		}
		uid := access.UserID
		for key, value := range statsFor(&uid) {
			item[key] = value
		}
		if isElearning {
			item["total_materials"] = totalMaterials
			item["completed_materials"] = progressMap[access.UserID]
		}
		legacy = append(legacy, item)
	}
	redeemed := len(legacy)
	unused := 0
	for _, item := range participants {
		if item.Status == "REDEEMED" {
			redeemed++
		}
		if item.Status == "UNUSED" {
			unused++
		}
	}
	httpx.Success(w, map[string]any{
		"paket": map[string]any{
			"paket_id": paket.PaketID, "paket_nama": paket.PaketNama,
			"paket_tipe": paket.PaketTipe, "jenis_paket": paket.JenisPaket,
		},
		"is_elearning":    isElearning,
		"total_materials": totalMaterials,
		"summary": map[string]any{
			"total": len(participants) + len(legacy), "redeemed": redeemed, "unused": unused,
		},
		"items": append(items, legacy...),
	}, "")
	return nil
}

func (s *Server) businessAddParticipants(w http.ResponseWriter, r *http.Request) error {
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
	values := bodySlice(body, "participants")
	if len(values) == 0 || len(values) > 200 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "participants", "message": "Minimal 1 dan maksimal 200 peserta"}})
	}
	emails := make([]string, 0, len(values))
	for _, raw := range values {
		if item, ok := raw.(map[string]any); ok {
			if email := bodyString(item, "email"); email != "" {
				emails = append(emails, email)
			}
		}
	}
	created, skipped, err := s.addParticipantEmails(r.Context(), s.Pool, id, emails, principal.UserID)
	if err != nil {
		return err
	}
	if created > 0 {
		s.Activity.Record(r.Context(), principal.UserID, "ORG_PARTICIPANT_ADD", auth.RequestIP(r), map[string]any{
			"organization_id": member.Organization.OrganizationID, "paket_id": id, "jumlah": created,
		})
	}
	httpx.Created(w, map[string]any{
		"paket_id": id, "created": created, "skipped": skipped, "items": []any{},
	}, "Peserta berhasil ditambahkan.")
	return nil
}

func (s *Server) businessRemoveParticipant(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	participantID, err := httpx.IDParam(r, "participant_id")
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
	var status, email string
	if err := s.Pool.QueryRow(r.Context(), `SELECT status, participant_email FROM package_participant WHERE participant_id = $1 AND paket_id = $2`, participantID, id).Scan(&status, &email); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Peserta paket tidak ditemukan", "PARTICIPANT_NOT_FOUND")
		}
		return err
	}
	if status != "UNUSED" {
		return httpx.Conflict("Peserta sudah menebus kode. Cabut aksesnya melalui menu akses bila diperlukan.", "PARTICIPANT_ALREADY_REDEEMED")
	}
	tag, err := s.Pool.Exec(r.Context(), `DELETE FROM package_participant WHERE participant_id = $1 AND status = 'UNUSED'`, participantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.Conflict("Peserta sudah menebus kode. Cabut aksesnya melalui menu akses bila diperlukan.", "PARTICIPANT_ALREADY_REDEEMED")
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_PARTICIPANT_REMOVE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "paket_id": id,
		"participant_id": participantID, "participant_email": email,
	})
	httpx.Success(w, map[string]any{"participant_id": participantID}, "Peserta berhasil dihapus.")
	return nil
}

func (s *Server) businessRegenerateParticipant(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	participantID, err := httpx.IDParam(r, "participant_id")
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
	var status string
	if err := s.Pool.QueryRow(r.Context(), `SELECT status FROM package_participant WHERE participant_id = $1 AND paket_id = $2`, participantID, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Peserta paket tidak ditemukan", "PARTICIPANT_NOT_FOUND")
		}
		return err
	}
	if status != "UNUSED" {
		return httpx.Conflict("Kode tidak dapat dibuat ulang karena peserta sudah menebusnya.", "PARTICIPANT_ALREADY_REDEEMED")
	}
	var updated map[string]any
	for attempt := 0; attempt < 8; attempt++ {
		var participantIDOut int
		var email string
		var nama *string
		var code string
		var redeemed *time.Time
		err := s.Pool.QueryRow(r.Context(), `
			UPDATE package_participant SET access_code = $1, updated_at = now()
			WHERE participant_id = $2 AND status = 'UNUSED'
			RETURNING participant_id, participant_email, participant_nama, access_code, redeemed_at`,
			codegen.ParticipantCode(), participantID).Scan(&participantIDOut, &email, &nama, &code, &redeemed)
		if err == nil {
			updated = map[string]any{
				"participant_id": participantIDOut, "participant_email": email,
				"participant_nama": nama, "access_code": code, "status": "UNUSED", "redeemed_at": redeemed,
			}
			break
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.Conflict("Kode tidak dapat dibuat ulang karena peserta sudah menebusnya.", "PARTICIPANT_ALREADY_REDEEMED")
		}
	}
	if updated == nil {
		return httpx.NewError(http.StatusInternalServerError, "Gagal membuat kode peserta baru", "CODE_GENERATION_FAILED", nil)
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_PARTICIPANT_REGENERATE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "paket_id": id, "participant_id": participantID,
	})
	httpx.Success(w, updated, "Kode peserta berhasil dibuat ulang.")
	return nil
}

func (s *Server) businessListMembers(w http.ResponseWriter, r *http.Request) error {
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
	conditions := []string{"m.organization_id = $1", "m.member_status = 'ACTIVE'"}
	args := []any{member.Organization.OrganizationID}
	index := 2
	if role := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("role"))); role != "" {
		conditions = append(conditions, fmt.Sprintf("m.member_role = $%d", index))
		args = append(args, role)
		index++
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("(u.user_nama ILIKE $%d OR u.user_email ILIKE $%d)", index, index))
		args = append(args, "%"+search+"%")
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM organization_member m JOIN "user" u ON u.user_id = m.user_id WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`
		SELECT m.member_id, m.organization_id, m.user_id, m.member_role, m.member_status,
		       m.invited_by, m.joined_at, m.created_at, m.updated_at,
		       u.user_id, u.user_nama, u.user_email, u.user_phone, u.created_at
		FROM organization_member m JOIN "user" u ON u.user_id = m.user_id
		WHERE %s ORDER BY m.joined_at ASC, m.member_id ASC LIMIT $%d OFFSET $%d`, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var memberID, orgID, userID int
		var role, status string
		var invitedBy *int
		var joined, created, updated time.Time
		var uID int
		var uNama, uEmail string
		var uPhone *string
		var uCreated time.Time
		if err := rows.Scan(&memberID, &orgID, &userID, &role, &status, &invitedBy, &joined,
			&created, &updated, &uID, &uNama, &uEmail, &uPhone, &uCreated); err != nil {
			return err
		}
		items = append(items, map[string]any{
			"member_id": memberID, "organization_id": orgID, "user_id": userID, "member_role": role,
			"member_status": status, "invited_by": invitedBy, "joined_at": joined,
			"created_at": created, "updated_at": updated,
			"user": map[string]any{
				"user_id": uID, "user_nama": uNama, "user_email": uEmail,
				"user_phone": uPhone, "created_at": uCreated,
			},
		})
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)),
		withExtras(map[string]any{"organization": organizationForMember(member), "member_role": member.MemberRole}))
	return nil
}

func (s *Server) businessInviteMember(w http.ResponseWriter, r *http.Request) error {
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
	v := &validation{}
	email := v.email(body, "email")
	role := strings.ToUpper(bodyString(body, "role"))
	if role != "ADMIN" && role != "VIEWER" && role != "PARTICIPANT" {
		v.add("role", "Peran undangan harus ADMIN, VIEWER, atau PARTICIPANT")
	}
	if err := v.err(); err != nil {
		return err
	}
	if role == "ADMIN" && member.MemberRole != "OWNER" {
		return httpx.Forbidden("Hanya owner organisasi yang dapat mengundang admin baru.", "OWNER_REQUIRED")
	}
	var existingUserID *int
	_ = s.Pool.QueryRow(r.Context(), `SELECT user_id FROM "user" WHERE lower(user_email) = $1`, email).Scan(&existingUserID)
	if existingUserID != nil {
		var exists bool
		_ = s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM organization_member WHERE organization_id = $1 AND user_id = $2 AND member_status = 'ACTIVE')`, member.Organization.OrganizationID, *existingUserID).Scan(&exists)
		if exists {
			return httpx.Conflict("Pengguna sudah menjadi anggota organisasi.", "MEMBER_EXISTS")
		}
	}
	token := codegen.RandomToken()
	tokenHash := codegen.HashToken(token)
	expires := time.Now().Add(7 * 24 * time.Hour)
	var inviteID int
	var domainEmail string
	var domainRole string
	var inviteStatus *string
	var acceptedAt *time.Time
	var invitedBy *int
	var createdAt time.Time
	err = s.Pool.QueryRow(r.Context(), `
		INSERT INTO organization_invite (organization_id, invite_email, invite_role, invite_token_hash,
			invite_expires_at, invited_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (invite_token_hash) DO NOTHING
		RETURNING invite_id`, member.Organization.OrganizationID, email, role, tokenHash, expires, principal.UserID).Scan(&inviteID)
	if err != nil {
		return err
	}
	_ = s.Pool.QueryRow(r.Context(), `SELECT invite_email, invite_role, invite_accepted_at, invited_by, created_at FROM organization_invite WHERE invite_id = $1`, inviteID).Scan(&domainEmail, &domainRole, &acceptedAt, &invitedBy, &createdAt)
	_ = inviteStatus
	sent, _ := s.Mail.Send(email, "Undangan bergabung organisasi "+member.Organization.OrganizationNama,
		fmt.Sprintf("Halo,\n\nAnda diundang bergabung ke organisasi %s sebagai %s.\n\nBuka tautan berikut untuk menerima undangan:\n%s/business/invite?token=%s\n\nSalam,\nTim EduMaster",
			member.Organization.OrganizationNama, role, s.Config.FrontendURL, token),
		fmt.Sprintf("<p>Halo,</p><p>Anda diundang bergabung ke organisasi <strong>%s</strong> sebagai <strong>%s</strong>.</p><p><a href=\"%s/business/invite?token=%s\">Terima undangan</a></p>",
			member.Organization.OrganizationNama, role, s.Config.FrontendURL, token))
	invite := map[string]any{
		"invite_id": inviteID, "organization_id": member.Organization.OrganizationID,
		"invite_email": domainEmail, "invite_role": domainRole, "invite_expires_at": expires,
		"invite_accepted_at": acceptedAt, "invited_by": invitedBy, "created_at": createdAt,
	}
	result := map[string]any{"invite": invite, "email_sent": sent}
	if !sent {
		result["token_preview"] = token
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_INVITE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "invite_id": inviteID, "email": email, "role": role, "email_sent": sent,
	})
	if sent {
		httpx.Created(w, result, "Undangan berhasil dikirim melalui email.")
	} else {
		httpx.Created(w, result, "Undangan dibuat, tetapi email gagal dikirim. Gunakan token pratinjau.")
	}
	return nil
}

func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(bodyString(body, "token"))
	if token == "" {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "token", "message": "Token undangan wajib diisi"}})
	}
	var inviteID, orgID int
	var inviteEmail, role string
	var expires time.Time
	var accepted *time.Time
	var orgStatus string
	err = s.Pool.QueryRow(r.Context(), `
		SELECT i.invite_id, i.organization_id, i.invite_email, i.invite_role, i.invite_expires_at,
		       i.invite_accepted_at, o.organization_status
		FROM organization_invite i JOIN organization o ON o.organization_id = i.organization_id
		WHERE i.invite_token_hash = $1`, codegen.HashToken(token)).Scan(&inviteID, &orgID, &inviteEmail, &role, &expires, &accepted, &orgStatus)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (accepted != nil || !expires.After(time.Now()))) {
		return httpx.BadRequest("Undangan tidak valid atau sudah kedaluwarsa.", "INVALID_INVITE_TOKEN")
	}
	if err != nil {
		return err
	}
	if !strings.EqualFold(principal.UserEmail, inviteEmail) {
		return httpx.Forbidden("Undangan ini ditujukan untuk alamat email lain.", "INVITE_EMAIL_MISMATCH")
	}
	if orgStatus != "ACTIVE" {
		return httpx.Forbidden("Organisasi tidak aktif. Hubungi administrator EduMaster.", "ORG_NOT_ACTIVE")
	}
	var currentRole *string
	_ = s.Pool.QueryRow(r.Context(), `SELECT member_role FROM organization_member WHERE organization_id = $1 AND user_id = $2`, orgID, principal.UserID).Scan(&currentRole)
	if currentRole != nil && *currentRole == "OWNER" {
		return httpx.Conflict("Owner organisasi tidak dapat menerima undangan dengan peran lain.", "OWNER_INVITE_CONFLICT")
	}
	err = pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO organization_member (organization_id, user_id, member_role, member_status, joined_at, created_at, updated_at)
			VALUES ($1,$2,$3,'ACTIVE',now(),now(),now())
			ON CONFLICT (organization_id, user_id) DO UPDATE
			SET member_role = EXCLUDED.member_role, member_status = 'ACTIVE', updated_at = now()`,
			orgID, principal.UserID, role); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `UPDATE organization_invite SET invite_accepted_at = now() WHERE invite_id = $1`, inviteID)
		return err
	})
	if err != nil {
		return err
	}
	var member Membership
	row := s.Pool.QueryRow(r.Context(), `
		SELECT `+organizationColumns+`, m.member_id, m.member_role, m.member_status, m.joined_at
		FROM organization_member m JOIN organization o ON o.organization_id = m.organization_id
		WHERE m.organization_id = $1 AND m.user_id = $2`, orgID, principal.UserID)
	member, err = scanOrganization(row, nil, nil, nil, nil)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_INVITE_ACCEPT", auth.RequestIP(r), map[string]any{
		"organization_id": orgID, "invite_id": inviteID,
	})
	httpx.Success(w, map[string]any{"organization": organizationForMember(member), "member_role": role}, "Undangan berhasil diterima.")
	return nil
}

func (s *Server) businessUpdateMemberRole(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	targetID, err := httpx.IDParam(r, "id")
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
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	role := strings.ToUpper(bodyString(body, "role"))
	if role != "ADMIN" && role != "VIEWER" && role != "PARTICIPANT" {
		return httpx.BadRequest("Peran tidak valid.", "INVALID_MEMBER_ROLE")
	}
	var targetRole string
	if err := s.Pool.QueryRow(r.Context(), `SELECT member_role FROM organization_member WHERE organization_id = $1 AND user_id = $2 AND member_status = 'ACTIVE'`,
		member.Organization.OrganizationID, targetID).Scan(&targetRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Anggota tidak ditemukan", "MEMBER_NOT_FOUND")
		}
		return err
	}
	if targetRole == "OWNER" {
		return httpx.Forbidden("Peran owner tidak dapat diubah.", "OWNER_IMMUTABLE")
	}
	if member.MemberRole == "ADMIN" && (targetRole == "ADMIN" || role == "ADMIN") {
		return httpx.Forbidden("Hanya owner yang dapat mengubah atau mengangkat admin.", "OWNER_REQUIRED")
	}
	tag, err := s.Pool.Exec(r.Context(), `UPDATE organization_member SET member_role = $1, updated_at = now() WHERE organization_id = $2 AND user_id = $3`,
		role, member.Organization.OrganizationID, targetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Anggota tidak ditemukan", "MEMBER_NOT_FOUND")
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_MEMBER_ROLE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "target_user_id": targetID, "role": role,
	})
	httpx.Success(w, map[string]any{"user_id": targetID, "member_role": role}, "Peran anggota diperbarui.")
	return nil
}

func (s *Server) businessRemoveMember(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	targetID, err := httpx.IDParam(r, "id")
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
	var targetRole string
	if err := s.Pool.QueryRow(r.Context(), `SELECT member_role FROM organization_member WHERE organization_id = $1 AND user_id = $2 AND member_status = 'ACTIVE'`,
		member.Organization.OrganizationID, targetID).Scan(&targetRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Anggota tidak ditemukan", "MEMBER_NOT_FOUND")
		}
		return err
	}
	if targetRole == "OWNER" {
		return httpx.Forbidden("Owner organisasi tidak dapat dikeluarkan.", "OWNER_IMMUTABLE")
	}
	if member.MemberRole == "ADMIN" && targetRole == "ADMIN" {
		return httpx.Forbidden("Hanya owner yang dapat mengeluarkan admin.", "OWNER_REQUIRED")
	}
	if targetID == principal.UserID {
		return httpx.BadRequest("Anda tidak dapat mengeluarkan diri sendiri.", "SELF_REMOVE")
	}
	if _, err := s.Pool.Exec(r.Context(), `UPDATE organization_member SET member_status = 'REMOVED', updated_at = now() WHERE organization_id = $1 AND user_id = $2`,
		member.Organization.OrganizationID, targetID); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_MEMBER_REMOVE", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "target_user_id": targetID,
	})
	httpx.Success(w, nil, "Anggota dikeluarkan dari organisasi.")
	return nil
}

func (s *Server) businessResults(w http.ResponseWriter, r *http.Request) error {
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
	conditions := []string{"p.organization_id = $1"}
	args := []any{member.Organization.OrganizationID}
	index := 2
	if paketID := httpx.QueryInt(r, "paket_id", 0); paketID > 0 {
		conditions = append(conditions, fmt.Sprintf("s.paket_id = $%d", index))
		args = append(args, paketID)
		index++
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("(u.user_nama ILIKE $%d OR u.user_email ILIKE $%d)", index, index))
		args = append(args, "%"+search+"%")
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM exam_session s JOIN paket_ujian p ON p.paket_id = s.paket_id JOIN "user" u ON u.user_id = s.user_id WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`
		SELECT s.session_id, s.user_id, s.paket_id, s.attempt_number, s.status, s.final_score,
		       s.is_passed, s.started_at, s.submitted_at, s.expires_at, s.auto_submitted,
		       s.disqualify_alasan, s.certificate_code, s.certificate_jp, s.updated_at,
		       u.user_id, u.user_nama, u.user_email,
		       p.paket_id, p.paket_nama, p.paket_tipe, p.jenis_paket, p.passing_grade
		FROM exam_session s JOIN paket_ujian p ON p.paket_id = s.paket_id JOIN "user" u ON u.user_id = s.user_id
		WHERE %s ORDER BY s.started_at DESC, s.session_id DESC LIMIT $%d OFFSET $%d`, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var sessionID, userID, paketID, attempt, uID, pID int
		var status, uNama, uEmail, pNama, pTipe, pJenis string
		var finalScore *float64
		var isPassed *bool
		var started, expires, updated time.Time
		var submitted *time.Time
		var auto bool
		var alasan, certCode *string
		var certJP *int
		var passing float64
		if err := rows.Scan(&sessionID, &userID, &paketID, &attempt, &status, &finalScore, &isPassed,
			&started, &submitted, &expires, &auto, &alasan, &certCode, &certJP, &updated,
			&uID, &uNama, &uEmail, &pID, &pNama, &pTipe, &pJenis, &passing); err != nil {
			return err
		}
		items = append(items, map[string]any{
			"session_id": sessionID, "user_id": userID, "paket_id": paketID, "attempt_number": attempt,
			"status": status, "final_score": finalScore, "is_passed": isPassed, "started_at": started,
			"submitted_at": submitted, "expires_at": expires, "auto_submitted": auto,
			"disqualify_alasan": alasan, "certificate_code": certCode, "certificate_jp": certJP,
			"updated_at": updated,
			"user":       map[string]any{"user_id": uID, "user_nama": uNama, "user_email": uEmail},
			"paket": map[string]any{
				"paket_id": pID, "paket_nama": pNama, "paket_tipe": pTipe,
				"jenis_paket": pJenis, "passing_grade": passing,
			},
		})
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)),
		withExtras(map[string]any{"organization": organizationForMember(member), "member_role": member.MemberRole}))
	return nil
}

func (s *Server) businessStats(w http.ResponseWriter, r *http.Request) error {
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
	orgID := member.Organization.OrganizationID
	var members, participants, packages, sessions, scored, passed int
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM organization_member WHERE organization_id = $1 AND member_status = 'ACTIVE'`, orgID).Scan(&members)
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM package_participant pp JOIN paket_ujian p ON p.paket_id = pp.paket_id WHERE p.organization_id = $1`, orgID).Scan(&participants)
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM paket_ujian WHERE organization_id = $1 AND is_active = true`, orgID).Scan(&packages)
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM exam_session s JOIN paket_ujian p ON p.paket_id = s.paket_id WHERE p.organization_id = $1`, orgID).Scan(&sessions)
	var average *float64
	_ = s.Pool.QueryRow(r.Context(), `
		SELECT count(*), count(*) FILTER (WHERE is_passed = true), avg(final_score)
		FROM exam_session s JOIN paket_ujian p ON p.paket_id = s.paket_id
		WHERE p.organization_id = $1 AND s.final_score IS NOT NULL`, orgID).Scan(&scored, &passed, &average)
	averageScore := 0.0
	if average != nil {
		averageScore = exam.Round2(*average)
	}
	passRate := 0.0
	if scored > 0 {
		passRate = math.Round(float64(passed)/float64(scored)*1000) / 10
	}
	httpx.Success(w, map[string]any{
		"organization": organizationForMember(member), "member_role": member.MemberRole,
		"members": members, "participants": participants, "packages": packages, "sessions": sessions,
		"scored_sessions": scored, "passed_sessions": passed, "pass_rate": passRate,
		"average_score": averageScore,
	}, "")
	return nil
}

func (s *Server) businessAdminList(w http.ResponseWriter, r *http.Request) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"1=1"}
	args := []any{}
	index := 1
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		conditions = append(conditions, fmt.Sprintf("o.organization_status = $%d", index))
		args = append(args, strings.ToUpper(status))
		index++
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("(o.organization_nama ILIKE $%d OR o.organization_email ILIKE $%d)", index, index))
		args = append(args, "%"+search+"%")
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM organization o WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	query := fmt.Sprintf(`
		SELECT `+organizationColumns+`,
		       (SELECT count(*) FROM organization_member m WHERE m.organization_id = o.organization_id) AS member_count,
		       (SELECT count(*) FROM paket_ujian p WHERE p.organization_id = o.organization_id) AS package_count
		FROM organization o WHERE %s ORDER BY o.created_at DESC, o.organization_id DESC LIMIT $%d OFFSET $%d`, where, index, index+1)
	args = append(args, limit, offset)
	rows, err := s.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var org Organization
		var memberCount, packageCount int
		if err := rows.Scan(&org.OrganizationID, &org.OrganizationNama, &org.OrganizationSlug,
			&org.OrganizationEmail, &org.OrganizationPhone, &org.OrganizationAlamat,
			&org.OrganizationDeskripsi, &org.OrganizationKode, &org.OrganizationStatus,
			&org.OrganizationRejectionAlasan, &org.ApprovedBy, &org.ApprovedAt, &org.CreatedBy,
			&org.CreatedAt, &org.UpdatedAt, &memberCount, &packageCount); err != nil {
			return err
		}
		items = append(items, map[string]any{
			"organization_id": org.OrganizationID, "organization_nama": org.OrganizationNama,
			"organization_slug": org.OrganizationSlug, "organization_email": org.OrganizationEmail,
			"organization_phone": org.OrganizationPhone, "organization_alamat": org.OrganizationAlamat,
			"organization_deskripsi": org.OrganizationDeskripsi, "organization_kode": org.OrganizationKode,
			"organization_status":           org.OrganizationStatus,
			"organization_rejection_alasan": org.OrganizationRejectionAlasan,
			"approved_by":                   org.ApprovedBy, "approved_at": org.ApprovedAt, "created_by": org.CreatedBy,
			"created_at": org.CreatedAt, "updated_at": org.UpdatedAt,
			"_count": map[string]int{"members": memberCount, "packages": packageCount},
		})
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return nil
}

func (s *Server) businessAdminStatus(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	status := strings.ToUpper(bodyString(body, "status"))
	if status != "ACTIVE" && status != "REJECTED" && status != "SUSPENDED" && status != "PENDING_APPROVAL" {
		return httpx.BadRequest("Status organisasi tidak valid.", "INVALID_ORG_STATUS")
	}
	reason := bodyStringPtr(body, "reason")
	var orgID int
	var nama, email string
	if err := s.Pool.QueryRow(r.Context(), `SELECT organization_id, organization_nama, organization_email FROM organization WHERE organization_id = $1`, id).Scan(&orgID, &nama, &email); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.NotFound("Organisasi tidak ditemukan", "ORGANIZATION_NOT_FOUND")
		}
		return err
	}
	if _, err := s.Pool.Exec(r.Context(), `
		UPDATE organization SET organization_status = $1, organization_rejection_alasan = $2,
			approved_by = $3, approved_at = CASE WHEN $1 = 'ACTIVE' THEN now() ELSE approved_at END,
			updated_at = now()
		WHERE organization_id = $4`, status, reason, principal.UserID, id); err != nil {
		return err
	}
	subject := "Status organisasi Anda diperbarui"
	switch status {
	case "ACTIVE":
		subject = "Organisasi Anda telah disetujui"
	case "REJECTED":
		subject = "Pendaftaran organisasi ditolak"
	case "SUSPENDED":
		subject = "Organisasi Anda dinonaktifkan"
	}
	message := "Status organisasi: " + status
	if reason != nil && *reason != "" {
		message += ". Alasan: " + *reason
	}
	_, _ = s.Mail.Send(email, subject+" - EduMaster Business",
		"Halo "+nama+",\n\n"+message+"\n\nSalam,\nTim EduMaster",
		"<p>Halo <strong>"+nama+"</strong>,</p><p>"+message+"</p><p>Salam,<br/>Tim EduMaster</p>")
	s.Activity.Record(r.Context(), principal.UserID, "ORG_STATUS_UPDATE", auth.RequestIP(r), map[string]any{
		"organization_id": id, "status": status,
	})
	httpx.Success(w, map[string]any{"organization_id": id, "organization_status": status}, "Status organisasi diperbarui menjadi "+status+".")
	return nil
}

func (s *Server) businessRegister(w http.ResponseWriter, r *http.Request) error {
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	if err := s.Captcha.Verify(r.Context(), bodyString(body, "recaptcha_token"), auth.RequestIP(r), "business_register"); err != nil {
		return err
	}
	v := &validation{}
	name := v.stringField(body, "user_nama", "Nama", 3, 100, true)
	email := v.email(body, "user_email")
	phone := v.phone(body, "user_phone", true)
	password := v.password(body, "user_password", "Password")
	orgName := v.stringField(body, "organization_nama", "Nama organisasi", 3, 150, true)
	orgEmail := email
	if raw := strings.TrimSpace(bodyString(body, "organization_email")); raw != "" {
		orgEmail = strings.ToLower(raw)
		if !emailPattern.MatchString(orgEmail) {
			v.add("organization_email", "Format email organisasi tidak valid")
		}
	}
	if err := v.err(); err != nil {
		return err
	}
	var existing bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM "user" WHERE lower(user_email) = $1)`, email).Scan(&existing); err != nil {
		return err
	}
	if existing {
		return httpx.Conflict("Email sudah terdaftar", "EMAIL_ALREADY_REGISTERED")
	}
	if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM "user" WHERE user_phone = $1)`, phone).Scan(&existing); err != nil {
		return err
	}
	if existing {
		return httpx.Conflict("Nomor HP sudah terdaftar", "PHONE_ALREADY_REGISTERED")
	}
	if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM organization WHERE lower(organization_email) = $1)`, orgEmail).Scan(&existing); err != nil {
		return err
	}
	if existing {
		return httpx.Conflict("Email organisasi sudah digunakan.", "ORGANIZATION_EMAIL_REGISTERED")
	}
	passwordHash, err := auth.HashPassword(password, s.Config.BcryptRounds)
	if err != nil {
		return err
	}
	baseSlug := codegen.Slugify(orgName)
	if baseSlug == "" {
		baseSlug = "organisasi"
	}
	slug := baseSlug
	for attempt := 1; attempt <= 20; attempt++ {
		if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM organization WHERE organization_slug = $1)`, slug).Scan(&existing); err != nil {
			return err
		}
		if !existing {
			break
		}
		slug = fmt.Sprintf("%s-%d", baseSlug, attempt+1)
	}
	var orgCode string
	for attempt := 0; attempt < 10; attempt++ {
		orgCode = codegen.OrganizationCode()
		if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM organization WHERE organization_kode = $1)`, orgCode).Scan(&existing); err != nil {
			return err
		}
		if !existing {
			break
		}
	}
	var userID, orgID int
	err = pgx.BeginFunc(r.Context(), s.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO "user" (user_nama, user_email, user_phone, user_password, user_role, created_at, updated_at)
			VALUES ($1,$2,$3,$4,'STUDENT',now(),now()) RETURNING user_id`,
			name, email, phone, passwordHash).Scan(&userID); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO organization (organization_nama, organization_slug, organization_email, organization_phone,
				organization_alamat, organization_deskripsi, organization_kode, organization_status, created_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'PENDING_APPROVAL',$8,now(),now()) RETURNING organization_id`,
			orgName, slug, orgEmail, bodyStringPtr(body, "organization_phone"), bodyStringPtr(body, "organization_alamat"),
			bodyStringPtr(body, "organization_deskripsi"), orgCode, userID).Scan(&orgID); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO organization_member (organization_id, user_id, member_role, member_status, joined_at, created_at, updated_at)
			VALUES ($1,$2,'OWNER','ACTIVE',now(),now(),now())`, orgID, userID)
		return err
	})
	if err != nil {
		return err
	}
	token := codegen.RandomToken()
	expires := time.Now().Add(s.Config.EmailVerificationTTL)
	_, _ = s.Pool.Exec(r.Context(), `
		INSERT INTO email_verification_token (user_id, token_hash, expires_at, created_at)
		VALUES ($1,$2,$3,now())`, userID, codegen.HashToken(token), expires)
	sent, _ := s.Mail.SendVerificationEmail(email, name, token)
	user, err := scanUser(s.Pool.QueryRow(r.Context(), `SELECT `+publicUserColumns+` FROM "user" WHERE user_id = $1`, userID))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), userID, "BUSINESS_REGISTER", auth.RequestIP(r), map[string]any{
		"organization_id": orgID, "email_sent": sent,
	})
	httpx.Created(w, map[string]any{
		"user": user,
		"organization": map[string]any{
			"organization_id": orgID, "organization_nama": orgName, "organization_status": "PENDING_APPROVAL",
		},
		"email_sent": sent, "verification_required": true,
	}, "Pendaftaran business berhasil. Verifikasi email Anda, lalu tunggu persetujuan Superadmin.")
	return nil
}

func (s *Server) businessJoin(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	code := codegen.NormalizeAccessCode(bodyString(body, "organization_kode"))
	if code == "" {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "organization_kode", "message": "Kode organisasi wajib diisi"}})
	}
	var org Organization
	err = s.Pool.QueryRow(r.Context(), `SELECT `+organizationColumns+` FROM organization o WHERE o.organization_kode = $1`, code).Scan(
		&org.OrganizationID, &org.OrganizationNama, &org.OrganizationSlug, &org.OrganizationEmail,
		&org.OrganizationPhone, &org.OrganizationAlamat, &org.OrganizationDeskripsi, &org.OrganizationKode,
		&org.OrganizationStatus, &org.OrganizationRejectionAlasan, &org.ApprovedBy, &org.ApprovedAt,
		&org.CreatedBy, &org.CreatedAt, &org.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Kode organisasi tidak ditemukan", "ORGANIZATION_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if org.OrganizationStatus != "ACTIVE" {
		return httpx.Forbidden("Organisasi tidak aktif. Hubungi administrator EduMaster.", "ORG_NOT_ACTIVE")
	}
	var role string
	err = s.Pool.QueryRow(r.Context(), `SELECT member_role FROM organization_member WHERE organization_id = $1 AND user_id = $2 AND member_status = 'ACTIVE'`, org.OrganizationID, principal.UserID).Scan(&role)
	if err == nil {
		member := Membership{MemberRole: role, MemberStatus: "ACTIVE", Organization: org}
		httpx.Success(w, map[string]any{"organization": organizationForMember(member), "member_role": role}, "Berhasil bergabung ke organisasi.")
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err := s.Pool.Exec(r.Context(), `
		INSERT INTO organization_member (organization_id, user_id, member_role, member_status, joined_at, created_at, updated_at)
		VALUES ($1,$2,'PARTICIPANT','ACTIVE',now(),now(),now())
		ON CONFLICT (organization_id, user_id) DO UPDATE
		SET member_status = 'ACTIVE', updated_at = now()`, org.OrganizationID, principal.UserID); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_JOIN", auth.RequestIP(r), map[string]any{
		"organization_id": org.OrganizationID, "member_role": "PARTICIPANT",
	})
	httpx.Success(w, map[string]any{"organization": organizationForMember(Membership{MemberRole: "PARTICIPANT", Organization: org}), "member_role": "PARTICIPANT"}, "Berhasil bergabung ke organisasi.")
	return nil
}

func (s *Server) businessUnlock(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	member, err := s.requireMembership(r.Context(), principal.UserID, nil)
	if err != nil {
		return err
	}
	if err := assertOrgActive(member); err != nil {
		return err
	}
	paket, err := s.requireOrgPackage(r.Context(), id, member.Organization.OrganizationID)
	if err != nil {
		return err
	}
	if !paket.IsActive {
		return httpx.NotFound("Paket privat tidak ditemukan atau tidak aktif", "PACKAGE_NOT_FOUND")
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	code := codegen.NormalizeAccessCode(bodyString(body, "paket_access_code"))
	if paket.PaketAccessCode == nil || code != codegen.NormalizeAccessCode(*paket.PaketAccessCode) {
		return httpx.Forbidden("Kode akses kelas/ujian salah. Hubungi admin organisasi Anda.", "INVALID_PACKAGE_ACCESS_CODE")
	}
	expiredAt := time.Now().Add(time.Duration(s.Config.PrivateAccessDays) * 24 * time.Hour)
	var accessID, attempts int
	var expiry time.Time
	if err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO user_package_access (user_id, paket_id, sisa_attempt, expired_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,now(),now())
		ON CONFLICT (user_id, paket_id) DO UPDATE
		SET sisa_attempt = EXCLUDED.sisa_attempt, expired_at = EXCLUDED.expired_at, updated_at = now()
		RETURNING access_id, sisa_attempt, expired_at`, principal.UserID, id, paket.MaxAttempts, expiredAt).Scan(&accessID, &attempts, &expiry); err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "ORG_PACKAGE_UNLOCK", auth.RequestIP(r), map[string]any{
		"organization_id": member.Organization.OrganizationID, "paket_id": id,
	})
	payload := map[string]any{}
	encoded, _ := json.Marshal(paket)
	_ = json.Unmarshal(encoded, &payload)
	delete(payload, "paket_access_code")
	httpx.Success(w, map[string]any{
		"access": map[string]any{
			"access_id": accessID, "user_id": principal.UserID, "paket_id": id,
			"sisa_attempt": attempts, "expired_at": expiry,
		},
		"paket": payload,
	}, "Kelas/ujian privat berhasil dibuka.")
	return nil
}
