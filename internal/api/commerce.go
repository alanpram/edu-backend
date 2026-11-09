package api

import (
	"context"
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
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
)

type Coupon struct {
	KuponID       int       `json:"kupon_id"`
	KuponKode     string    `json:"kupon_kode"`
	KuponDiskon   float64   `json:"kupon_diskon"`
	AffiliateName *string   `json:"affiliate_name"`
	KuponExpired  time.Time `json:"kupon_expired"`
	KuponAktif    bool      `json:"kupon_aktif"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Transaction struct {
	TransaksiID      int        `json:"transaksi_id"`
	UserID           int        `json:"user_id"`
	PaketID          int        `json:"paket_id"`
	KuponID          *int       `json:"kupon_id"`
	TotalBayar       float64    `json:"total_bayar"`
	Status           string     `json:"status"`
	PaymentProvider  *string    `json:"payment_provider"`
	PaymentReference *string    `json:"payment_reference"`
	PaidAt           *time.Time `json:"paid_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	Paket            map[string]any
	Kupon            any
	User             *auth.User
}

func round2(value float64) float64 {
	return math.Round((value+math.SmallestNonzeroFloat64)*100) / 100
}

func scanCoupon(row pgx.Row) (Coupon, error) {
	var item Coupon
	err := row.Scan(&item.KuponID, &item.KuponKode, &item.KuponDiskon, &item.AffiliateName,
		&item.KuponExpired, &item.KuponAktif, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

const couponColumns = `kupon_id, kupon_kode, kupon_diskon, affiliate_name, kupon_expired, kupon_aktif, created_at, updated_at`

func (s *Server) registerCoupons(api chi.Router) {
	api.Route("/coupons", func(r chi.Router) {
		r.Use(s.Auth.Authenticate)
		r.Post("/validate", httpx.Wrap(s.couponValidate))
		r.With(middleware.RequirePermission(s.Permissions, "admin.coupons.view")).
			Get("/performance", httpx.Wrap(s.couponPerformance))
		r.With(middleware.RequirePermission(s.Permissions, "admin.coupons.view")).
			Get("/", httpx.Wrap(s.couponList))
		r.With(middleware.RequirePermission(s.Permissions, "admin.coupons.manage")).
			Post("/", httpx.Wrap(s.couponCreate))
		r.With(middleware.RequirePermission(s.Permissions, "admin.coupons.manage")).
			Put("/{id}", httpx.Wrap(s.couponUpdate))
		r.With(middleware.RequirePermission(s.Permissions, "admin.coupons.manage")).
			Delete("/{id}", httpx.Wrap(s.couponRemove))
	})
}

func (s *Server) couponValidate(w http.ResponseWriter, r *http.Request) error {
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	code := strings.ToUpper(strings.TrimSpace(bodyString(body, "kupon_kode")))
	paketID := bodyInt(body, "paket_id", 0)
	if code == "" || paketID <= 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR",
			[]map[string]string{{"field": "kupon_kode", "message": "Kode kupon dan paket wajib diisi"}})
	}
	paket, err := s.lmsPackage(r.Context(), paketID)
	if err != nil {
		return err
	}
	if !paket.IsActive || paket.OrganizationID != nil {
		return httpx.NotFound("Paket ujian tidak ditemukan", "PACKAGE_NOT_FOUND")
	}
	coupon, err := scanCoupon(s.Pool.QueryRow(r.Context(), `SELECT `+couponColumns+` FROM kupon_diskon WHERE kupon_kode = $1`, code))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !coupon.KuponAktif) {
		return httpx.NotFound("Kode kupon tidak ditemukan atau tidak aktif", "COUPON_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if !coupon.KuponExpired.After(time.Now()) {
		return httpx.Unprocessable("Kode kupon sudah kedaluwarsa", "COUPON_EXPIRED", nil)
	}
	discount := math.Min(coupon.KuponDiskon, paket.PaketHarga)
	finalPrice := round2(math.Max(0, paket.PaketHarga-discount))
	httpx.Success(w, map[string]any{
		"kupon": map[string]any{
			"kupon_id": coupon.KuponID, "kupon_kode": coupon.KuponKode,
			"kupon_diskon": coupon.KuponDiskon, "kupon_expired": coupon.KuponExpired,
		},
		"paket": map[string]any{
			"paket_id": paket.PaketID, "paket_nama": paket.PaketNama, "paket_harga": paket.PaketHarga,
		},
		"diskon_diterapkan": round2(discount),
		"total_bayar":       finalPrice,
	}, "Kupon valid")
	return nil
}

func (s *Server) couponList(w http.ResponseWriter, r *http.Request) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"1=1"}
	args := []any{}
	index := 1
	if affiliate := strings.TrimSpace(r.URL.Query().Get("affiliate_name")); affiliate != "" {
		conditions = append(conditions, fmt.Sprintf("affiliate_name = $%d", index))
		args = append(args, affiliate)
		index++
	}
	if active := httpx.QueryBoolPtr(r, "is_active"); active != nil {
		conditions = append(conditions, fmt.Sprintf("kupon_aktif = $%d", index))
		args = append(args, *active)
		index++
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		conditions = append(conditions, fmt.Sprintf("(kupon_kode ILIKE $%d OR affiliate_name ILIKE $%d)", index, index))
		args = append(args, "%"+search+"%")
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM kupon_diskon WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	rows, err := s.Pool.Query(r.Context(), fmt.Sprintf(`SELECT `+couponColumns+` FROM kupon_diskon WHERE %s
		ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, index, index+1), append(args, limit, offset)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Coupon{}
	for rows.Next() {
		item, err := scanCoupon(rows)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return rows.Err()
}

func couponValidation(body map[string]any, partial bool) (map[string]any, map[string]string) {
	fields := map[string]string{}
	values := map[string]any{}
	if !partial || bodyHasKey(body, "kupon_kode") {
		code := strings.ToUpper(strings.TrimSpace(bodyString(body, "kupon_kode")))
		valid := len(code) >= 3 && len(code) <= 50
		for _, character := range code {
			if !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && character != '_' && character != '-' {
				valid = false
				break
			}
		}
		if !valid {
			fields["kupon_kode"] = "Kode hanya boleh berisi huruf, angka, _ dan -"
		} else {
			values["kupon_kode"] = code
		}
	}
	if !partial || bodyHasKey(body, "kupon_diskon") {
		value := bodyFloat(body, "kupon_diskon", 0)
		if value <= 0 || value > 100_000_000 {
			fields["kupon_diskon"] = "Diskon harus lebih dari 0"
		} else {
			values["kupon_diskon"] = value
		}
	}
	if !partial || bodyHasKey(body, "affiliate_name") {
		value := bodyStringPtr(body, "affiliate_name")
		if value != nil && len([]rune(*value)) > 150 {
			fields["affiliate_name"] = "Nama affiliate maksimal 150 karakter"
		} else {
			values["affiliate_name"] = value
		}
	}
	if !partial || bodyHasKey(body, "kupon_expired") {
		parsed, err := parseISODate(bodyString(body, "kupon_expired"))
		if err != nil {
			fields["kupon_expired"] = "Tanggal kedaluwarsa tidak valid"
		} else if !parsed.After(time.Now()) {
			fields["kupon_expired"] = "kupon_expired harus berada di masa depan"
		} else {
			values["kupon_expired"] = parsed
		}
	}
	if !partial || bodyHasKey(body, "kupon_aktif") {
		values["kupon_aktif"] = bodyBool(body, "kupon_aktif", true)
	}
	return values, fields
}

func (s *Server) couponCreate(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	values, fields := couponValidation(body, false)
	if len(fields) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(fields))
	}
	var exists bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM kupon_diskon WHERE kupon_kode = $1)`, values["kupon_kode"]).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return httpx.Conflict("Kode kupon sudah digunakan", "COUPON_CODE_EXISTS")
	}
	item, err := scanCoupon(s.Pool.QueryRow(r.Context(), `INSERT INTO kupon_diskon
		(kupon_kode, kupon_diskon, affiliate_name, kupon_expired, kupon_aktif, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,now(),now()) RETURNING `+couponColumns,
		values["kupon_kode"], values["kupon_diskon"], values["affiliate_name"], values["kupon_expired"], values["kupon_aktif"]))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "COUPON_CREATE", auth.RequestIP(r), map[string]any{
		"kupon_id": item.KuponID, "kupon_kode": item.KuponKode,
	})
	httpx.Created(w, item, "Kupon berhasil dibuat")
	return nil
}

func (s *Server) couponUpdate(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	existing, err := scanCoupon(s.Pool.QueryRow(r.Context(), `SELECT `+couponColumns+` FROM kupon_diskon WHERE kupon_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Kupon tidak ditemukan", "COUPON_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	values, fields := couponValidation(body, true)
	if len(fields) > 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", validationDetails(fields))
	}
	if code, ok := values["kupon_kode"].(string); ok && code != existing.KuponKode {
		var duplicate bool
		if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM kupon_diskon WHERE kupon_kode = $1)`, code).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate {
			return httpx.Conflict("Kode kupon sudah digunakan", "COUPON_CODE_EXISTS")
		}
	}
	columns := []string{"kupon_kode", "kupon_diskon", "affiliate_name", "kupon_expired", "kupon_aktif"}
	set, args, index := updateSet(values, columns)
	if len(set) > 0 {
		set = append(set, "updated_at = now()")
		args = append(args, id)
		if _, err := s.Pool.Exec(r.Context(), fmt.Sprintf(`UPDATE kupon_diskon SET %s WHERE kupon_id = $%d`, strings.Join(set, ", "), index), args...); err != nil {
			return err
		}
	}
	item, err := scanCoupon(s.Pool.QueryRow(r.Context(), `SELECT `+couponColumns+` FROM kupon_diskon WHERE kupon_id = $1`, id))
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "COUPON_UPDATE", auth.RequestIP(r), map[string]any{"kupon_id": id})
	httpx.Success(w, item, "Kupon berhasil diperbarui")
	return nil
}

func (s *Server) couponRemove(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	var code string
	err = s.Pool.QueryRow(r.Context(), `DELETE FROM kupon_diskon WHERE kupon_id = $1 RETURNING kupon_kode`, id).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Kupon tidak ditemukan", "COUPON_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "COUPON_DELETE", auth.RequestIP(r), map[string]any{"kupon_id": id, "kupon_kode": code})
	httpx.Success(w, nil, "Kupon berhasil dihapus")
	return nil
}

func (s *Server) couponPerformance(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT c.affiliate_name, c.kupon_id, c.kupon_kode, count(t.transaksi_id), COALESCE(sum(t.total_bayar), 0)
		FROM kupon_diskon c JOIN transaksi t ON t.kupon_id = c.kupon_id AND t.status = 'success'
		GROUP BY c.affiliate_name, c.kupon_id, c.kupon_kode
		ORDER BY COALESCE(sum(t.total_bayar), 0) DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	affiliates := map[string]map[string]any{}
	order := []string{}
	for rows.Next() {
		var affiliate *string
		var couponID, transactions int
		var code string
		var revenue float64
		if err := rows.Scan(&affiliate, &couponID, &code, &transactions, &revenue); err != nil {
			return err
		}
		name := "UNASSIGNED"
		if affiliate != nil && *affiliate != "" {
			name = *affiliate
		}
		current, exists := affiliates[name]
		if !exists {
			current = map[string]any{
				"affiliate_name": name, "total_transactions": 0, "total_revenue": 0.0,
				"coupons_used": []map[string]any{},
			}
			affiliates[name] = current
			order = append(order, name)
		}
		current["total_transactions"] = current["total_transactions"].(int) + transactions
		current["total_revenue"] = round2(current["total_revenue"].(float64) + revenue)
		current["coupons_used"] = append(current["coupons_used"].([]map[string]any), map[string]any{
			"kupon_id": couponID, "kupon_kode": code, "transactions": transactions, "revenue": round2(revenue),
		})
	}
	result := []map[string]any{}
	for _, name := range order {
		result = append(result, affiliates[name])
	}
	httpx.Success(w, result, "")
	return rows.Err()
}

const transactionSelect = `
	t.transaksi_id, t.user_id, t.paket_id, t.kupon_id, t.total_bayar, t.status,
	t.payment_provider, t.payment_reference, t.paid_at, t.created_at, t.updated_at,
	p.paket_id, p.paket_nama, p.paket_tipe, p.paket_harga, p.max_attempts,
	c.kupon_id, c.kupon_kode, c.kupon_diskon, c.affiliate_name,
	u.user_id, u.user_nama, u.user_email, u.user_phone, u.user_role, u.email_verified_at,
	u.is_active, u.last_login_at, u.created_at, u.updated_at`

func scanTransactionWithUser(row pgx.Row) (Transaction, error) {
	var item Transaction
	var pID int
	var pNama, pTipe string
	var pHarga float64
	var pMax int
	var cID *int
	var cKode *string
	var cDiskon *float64
	var cAffiliate *string
	var user auth.User
	err := row.Scan(&item.TransaksiID, &item.UserID, &item.PaketID, &item.KuponID, &item.TotalBayar,
		&item.Status, &item.PaymentProvider, &item.PaymentReference, &item.PaidAt, &item.CreatedAt, &item.UpdatedAt,
		&pID, &pNama, &pTipe, &pHarga, &pMax,
		&cID, &cKode, &cDiskon, &cAffiliate,
		&user.UserID, &user.UserNama, &user.UserEmail, &user.UserPhone, &user.UserRole, &user.EmailVerifiedAt,
		&user.IsActive, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return item, err
	}
	item.Paket = map[string]any{
		"paket_id": pID, "paket_nama": pNama, "paket_tipe": pTipe, "paket_harga": pHarga, "max_attempts": pMax,
	}
	if cID != nil {
		item.Kupon = map[string]any{
			"kupon_id": *cID, "kupon_kode": cKode, "kupon_diskon": cDiskon, "affiliate_name": cAffiliate,
		}
	}
	item.User = &user
	return item, nil
}

func (t Transaction) payload(includeUser bool) map[string]any {
	payload := map[string]any{
		"transaksi_id": t.TransaksiID, "user_id": t.UserID, "paket_id": t.PaketID, "kupon_id": t.KuponID,
		"total_bayar": t.TotalBayar, "status": t.Status, "payment_provider": t.PaymentProvider,
		"payment_reference": t.PaymentReference, "paid_at": t.PaidAt,
		"created_at": t.CreatedAt, "updated_at": t.UpdatedAt,
		"paket": t.Paket, "kupon": t.Kupon,
	}
	if includeUser && t.User != nil {
		payload["user"] = t.User
	}
	return payload
}

func (s *Server) transactionByID(ctx context.Context, id int) (Transaction, error) {
	item, err := scanTransactionWithUser(s.Pool.QueryRow(ctx, `SELECT `+transactionSelect+`
		FROM transaksi t JOIN paket_ujian p ON p.paket_id = t.paket_id
		LEFT JOIN kupon_diskon c ON c.kupon_id = t.kupon_id
		JOIN "user" u ON u.user_id = t.user_id
		WHERE t.transaksi_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, httpx.NotFound("Transaksi tidak ditemukan", "TRANSACTION_NOT_FOUND")
	}
	return item, err
}

func (s *Server) registerTransactions(api chi.Router) {
	api.Route("/transactions", func(r chi.Router) {
		r.Use(s.Auth.Authenticate)
		r.Post("/", httpx.Wrap(s.transactionCreate))
		r.Get("/me", httpx.Wrap(s.transactionMine))
		r.With(middleware.RequirePermission(s.Permissions, "admin.transactions.view")).
			Get("/", httpx.Wrap(s.transactionAdminList))
		r.Get("/{id}", httpx.Wrap(s.transactionDetail))
		r.Post("/{id}/pay", httpx.Wrap(s.transactionPay))
	})
}

func (s *Server) settleTransaction(ctx context.Context, transactionID int, provider string, reference *string, days, attempts int, unlimited bool) (Transaction, error) {
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var status string
		var userID, paketID int
		if err := tx.QueryRow(ctx, `SELECT status, user_id, paket_id FROM transaksi WHERE transaksi_id = $1 FOR UPDATE`, transactionID).
			Scan(&status, &userID, &paketID); err != nil {
			return err
		}
		if status == "success" {
			return nil
		}
		if status != "pending" {
			return httpx.Conflict(fmt.Sprintf("Transaksi berstatus %s tidak dapat dibayar.", status), "TRANSACTION_NOT_PAYABLE")
		}
		if _, err := tx.Exec(ctx, `UPDATE transaksi SET status = 'success', paid_at = now(),
			payment_provider = $1, payment_reference = COALESCE($2, payment_reference), updated_at = now()
			WHERE transaksi_id = $3`, provider, reference, transactionID); err != nil {
			return err
		}
		attemptsToGrant := attempts
		if unlimited {
			attemptsToGrant = 0
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_package_access (user_id, paket_id, sisa_attempt, expired_at, created_at, updated_at)
			VALUES ($1, $2, $3, now() + ($4 * interval '1 day'), now(), now())
			ON CONFLICT (user_id, paket_id) DO UPDATE SET
				sisa_attempt = CASE WHEN $5 THEN 0 ELSE user_package_access.sisa_attempt + $6 END,
				expired_at = GREATEST(user_package_access.expired_at, now()) + ($4 * interval '1 day'),
				updated_at = now()`,
			userID, paketID, attemptsToGrant, days, unlimited, attempts); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Transaction{}, err
	}
	return s.transactionByID(ctx, transactionID)
}

func mockCharge(transactionID int, total float64) map[string]any {
	reference := fmt.Sprintf("MOCK-%d-%d-%s", transactionID, time.Now().UnixMilli(), codegen.RandomToken()[:8])
	return map[string]any{
		"provider": "mock", "reference": reference,
		"payment_url":  "mock://payment/" + reference,
		"expires_at":   time.Now().Add(24 * time.Hour),
		"instructions": "Provider mock aktif. Panggil POST /api/transactions/{transaksi_id}/pay untuk menyimulasikan pembayaran berhasil.",
	}
}

func (s *Server) transactionCreate(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	body, err := requireBody(r)
	if err != nil {
		return err
	}
	paketID := bodyInt(body, "paket_id", 0)
	if paketID <= 0 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR",
			[]map[string]string{{"field": "paket_id", "message": "Paket wajib dipilih"}})
	}
	paket, err := s.lmsPackage(r.Context(), paketID)
	if err != nil {
		return err
	}
	if !paket.IsActive {
		return httpx.NotFound("Paket ujian tidak ditemukan atau tidak aktif", "PACKAGE_NOT_FOUND")
	}
	if paket.OrganizationID != nil {
		return httpx.Forbidden("Paket privat tidak dapat dibeli dari katalog publik. Masuk ke dashboard business atau gunakan kode akses organisasi.", "PRIVATE_PACKAGE_NOT_PURCHASABLE")
	}
	var pendingID int
	err = s.Pool.QueryRow(r.Context(), `SELECT transaksi_id FROM transaksi WHERE user_id = $1 AND paket_id = $2 AND status = 'pending' ORDER BY created_at DESC LIMIT 1`,
		principal.UserID, paketID).Scan(&pendingID)
	if err == nil {
		return httpx.NewError(http.StatusConflict,
			"Masih ada transaksi pending untuk paket ini. Selesaikan atau tunggu kedaluwarsa.",
			"PENDING_TRANSACTION_EXISTS", map[string]any{"transaksi_id": pendingID})
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	var coupon *Coupon
	code := strings.ToUpper(strings.TrimSpace(bodyString(body, "kupon_kode")))
	if code != "" {
		item, err := scanCoupon(s.Pool.QueryRow(r.Context(), `SELECT `+couponColumns+` FROM kupon_diskon WHERE kupon_kode = $1`, code))
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !item.KuponAktif) {
			return httpx.Unprocessable("Kode kupon tidak valid", "COUPON_NOT_FOUND", nil)
		}
		if err != nil {
			return err
		}
		if !item.KuponExpired.After(time.Now()) {
			return httpx.Unprocessable("Kode kupon sudah kedaluwarsa", "COUPON_EXPIRED", nil)
		}
		coupon = &item
	}
	discount := 0.0
	var couponID any
	if coupon != nil {
		discount = math.Min(coupon.KuponDiskon, paket.PaketHarga)
		couponID = coupon.KuponID
	} else {
		couponID = nil
	}
	total := round2(math.Max(0, paket.PaketHarga-discount))
	// Validasi provider dilakukan sebelum insert agar transaksi berbayar tidak
	// tertinggal dalam status pending ketika provider belum siap.
	if total > 0 {
		if s.Config.IsProduction {
			return httpx.NewError(http.StatusServiceUnavailable, "Payment provider mock tidak boleh dipakai di production. Konfigurasikan Midtrans atau Xendit.", "MOCK_PAYMENT_DISABLED_IN_PRODUCTION", nil)
		}
		if s.Config.PaymentProvider != "mock" {
			return httpx.NewError(http.StatusNotImplemented, fmt.Sprintf("Payment provider %q belum dikonfigurasi pada fondasi ini.", s.Config.PaymentProvider), "PAYMENT_PROVIDER_NOT_CONFIGURED", nil)
		}
	}
	var transactionID int
	if err := s.Pool.QueryRow(r.Context(), `INSERT INTO transaksi (user_id, paket_id, kupon_id, total_bayar, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,'pending',now(),now()) RETURNING transaksi_id`,
		principal.UserID, paketID, couponID, total).Scan(&transactionID); err != nil {
		return err
	}

	if total == 0 {
		settled, err := s.settleTransaction(r.Context(), transactionID, "free", nil, s.Config.PaidAccessDays, paket.MaxAttempts, paket.MaxAttempts == 0)
		if err != nil {
			return err
		}
		s.Activity.Record(r.Context(), principal.UserID, "PAYMENT_SUCCESS", auth.RequestIP(r), map[string]any{
			"transaksi_id": transactionID, "paket_id": paketID, "total_bayar": 0, "provider": "free",
		})
		httpx.Created(w, map[string]any{"transaction": settled.payload(false), "payment": nil}, "Transaksi berhasil dibuat")
		return nil
	}

	payment := mockCharge(transactionID, total)
	reference := payment["reference"].(string)
	if _, err := s.Pool.Exec(r.Context(), `UPDATE transaksi SET payment_provider = 'mock', payment_reference = $1, updated_at = now() WHERE transaksi_id = $2`,
		reference, transactionID); err != nil {
		return err
	}
	transaction, err := s.transactionByID(r.Context(), transactionID)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "TRANSACTION_CREATE", auth.RequestIP(r), map[string]any{
		"transaksi_id": transactionID, "paket_id": paketID, "total_bayar": total, "kupon_id": couponID,
	})
	httpx.Created(w, map[string]any{"transaction": transaction.payload(false), "payment": payment}, "Transaksi berhasil dibuat")
	return nil
}

func (s *Server) transactionPay(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	transaction, err := s.transactionByID(r.Context(), id)
	if err != nil {
		return err
	}
	if transaction.UserID != principal.UserID {
		return httpx.Forbidden("Anda tidak memiliki akses ke transaksi ini.", "TRANSACTION_FORBIDDEN")
	}
	if transaction.Status == "success" {
		httpx.Success(w, transaction.payload(true), "Pembayaran berhasil. Akses paket aktif.")
		return nil
	}
	if transaction.Status != "pending" {
		return httpx.Conflict(fmt.Sprintf("Transaksi berstatus %s tidak dapat dibayar.", transaction.Status), "TRANSACTION_NOT_PAYABLE")
	}
	if s.Config.IsProduction {
		return httpx.NewError(http.StatusServiceUnavailable, "Penyelesaian pembayaran mock dinonaktifkan di production. Gunakan webhook payment gateway.", "MOCK_PAYMENT_DISABLED_IN_PRODUCTION", nil)
	}
	if s.Config.PaymentProvider != "mock" {
		return httpx.NewError(http.StatusNotImplemented, "Penyelesaian pembayaran harus dilakukan melalui webhook payment gateway.", "WEBHOOK_REQUIRED", nil)
	}
	paket, err := s.lmsPackage(r.Context(), transaction.PaketID)
	if err != nil {
		return err
	}
	settled, err := s.settleTransaction(r.Context(), id, "mock", nil, s.Config.PaidAccessDays, paket.MaxAttempts, paket.MaxAttempts == 0)
	if err != nil {
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "PAYMENT_SUCCESS", auth.RequestIP(r), map[string]any{
		"transaksi_id": id, "paket_id": transaction.PaketID, "total_bayar": transaction.TotalBayar, "provider": "mock",
	})
	httpx.Success(w, settled.payload(true), "Pembayaran berhasil. Akses paket aktif.")
	return nil
}

func (s *Server) transactionMine(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM transaksi WHERE user_id = $1`, principal.UserID).Scan(&total); err != nil {
		return err
	}
	rows, err := s.Pool.Query(r.Context(), `SELECT `+transactionSelect+`
		FROM transaksi t JOIN paket_ujian p ON p.paket_id = t.paket_id
		LEFT JOIN kupon_diskon c ON c.kupon_id = t.kupon_id
		JOIN "user" u ON u.user_id = t.user_id
		WHERE t.user_id = $1 ORDER BY t.created_at DESC LIMIT $2 OFFSET $3`, principal.UserID, limit, offset)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		item, err := scanTransactionWithUser(rows)
		if err != nil {
			return err
		}
		items = append(items, item.payload(false))
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return rows.Err()
}

func (s *Server) transactionAdminList(w http.ResponseWriter, r *http.Request) error {
	page, limit, offset := httpx.Pagination(httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "limit", 20))
	conditions := []string{"1=1"}
	args := []any{}
	index := 1
	if userID := httpx.QueryInt(r, "user_id", 0); userID > 0 {
		conditions = append(conditions, fmt.Sprintf("t.user_id = $%d", index))
		args = append(args, userID)
		index++
	}
	if paketID := httpx.QueryInt(r, "paket_id", 0); paketID > 0 {
		conditions = append(conditions, fmt.Sprintf("t.paket_id = $%d", index))
		args = append(args, paketID)
		index++
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		conditions = append(conditions, fmt.Sprintf("t.status = $%d", index))
		args = append(args, status)
		index++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM transaksi t WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	rows, err := s.Pool.Query(r.Context(), fmt.Sprintf(`SELECT `+transactionSelect+`
		FROM transaksi t JOIN paket_ujian p ON p.paket_id = t.paket_id
		LEFT JOIN kupon_diskon c ON c.kupon_id = t.kupon_id
		JOIN "user" u ON u.user_id = t.user_id
		WHERE %s ORDER BY t.created_at DESC LIMIT $%d OFFSET $%d`, where, index, index+1),
		append(args, limit, offset)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		item, err := scanTransactionWithUser(rows)
		if err != nil {
			return err
		}
		items = append(items, item.payload(true))
	}
	httpx.Success(w, items, "", httpx.WithMeta(httpx.Meta(total, page, limit)))
	return rows.Err()
}

func (s *Server) transactionDetail(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		return err
	}
	item, err := s.transactionByID(r.Context(), id)
	if err != nil {
		return err
	}
	if item.UserID != principal.UserID && !s.canPermission(r, "admin.transactions.view") {
		return httpx.Forbidden("Anda tidak memiliki akses ke transaksi ini.", "TRANSACTION_FORBIDDEN")
	}
	httpx.Success(w, item.payload(true), "")
	return nil
}
