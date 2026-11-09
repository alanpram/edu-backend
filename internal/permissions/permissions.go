// Package permissions mengelola matriks role-permission dinamis.
package permissions

import (
	"context"
	"sort"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edumaster/backend-go/internal/httpx"
)

var RoleOrder = []string{"SUPER_ADMIN", "INSTRUCTOR", "ASESOR", "STUDENT"}

type Definition struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	Group        string   `json:"group"`
	DefaultRoles []string `json:"-"`
}

func roles(values ...string) []string { return values }

var staff = roles("SUPER_ADMIN", "INSTRUCTOR", "ASESOR")
var editor = roles("SUPER_ADMIN", "INSTRUCTOR")
var super = roles("SUPER_ADMIN")
var all = roles("SUPER_ADMIN", "INSTRUCTOR", "ASESOR", "STUDENT")

var Catalog = []Definition{
	{"admin.dashboard", "Ringkasan Panel Admin", "Melihat halaman ringkasan dan statistik utama panel admin.", "Panel Admin", staff},
	{"admin.landing.view", "Lihat Landing Page CMS", "Membuka data hero, section, dan footer landing page.", "Konten & Pembelajaran", staff},
	{"admin.landing.manage", "Kelola Landing Page CMS", "Mengubah konten landing page, slideshow, dan media.", "Konten & Pembelajaran", editor},
	{"admin.packages.view", "Lihat Paket Ujian", "Melihat daftar paket, statistik peserta, dan detail paket.", "Konten & Pembelajaran", staff},
	{"admin.packages.manage", "Kelola Paket Ujian", "Membuat, mengubah, dan menghapus paket ujian.", "Konten & Pembelajaran", editor},
	{"admin.lms.view", "Lihat Kelas LMS", "Melihat kelas, modul, dan soal post-test dari panel admin.", "Konten & Pembelajaran", staff},
	{"admin.lms.manage", "Kelola Kelas LMS", "Membuat kelas, menyusun modul, dan mengatur post-test.", "Konten & Pembelajaran", editor},
	{"admin.media.manage", "Kelola Media & Upload", "Mengunggah dan menghapus gambar/media untuk konten.", "Konten & Pembelajaran", staff},
	{"admin.questions.view", "Lihat Bank Soal", "Melihat soal, opsi jawaban, dan pembahasan.", "Konten & Pembelajaran", staff},
	{"admin.questions.manage", "Kelola Bank Soal", "Membuat, mengubah, menghapus soal dan pembahasan.", "Konten & Pembelajaran", editor},
	{"admin.articles.view", "Lihat Artikel & SEO", "Melihat artikel dan metadata SEO di panel admin.", "Konten & Pembelajaran", staff},
	{"admin.articles.manage", "Kelola Artikel & SEO", "Menulis, menerbitkan, dan menghapus artikel.", "Konten & Pembelajaran", editor},
	{"admin.faq.view", "Lihat FAQ", "Melihat daftar FAQ portal.", "Konten & Pembelajaran", staff},
	{"admin.faq.manage", "Kelola FAQ", "Membuat, mengubah, dan menghapus FAQ.", "Konten & Pembelajaran", editor},
	{"admin.testimonials.view", "Lihat Testimoni", "Melihat testimoni peserta.", "Konten & Pembelajaran", staff},
	{"admin.testimonials.manage", "Kelola Testimoni", "Membuat, mengubah, dan menghapus testimoni.", "Konten & Pembelajaran", editor},
	{"admin.essays.view", "Lihat Antrean Esai", "Melihat jawaban esai yang menunggu penilaian.", "Penilaian", staff},
	{"admin.essays.grade", "Nilai Jawaban Esai", "Memberikan skor manual untuk jawaban esai peserta.", "Penilaian", staff},
	{"admin.coupons.view", "Lihat Kupon & Afiliasi", "Melihat kupon, penggunaan, dan performa afiliasi.", "Komersial", editor},
	{"admin.coupons.manage", "Kelola Kupon & Afiliasi", "Membuat, mengubah, dan menghapus kupon.", "Komersial", editor},
	{"admin.transactions.view", "Lihat Transaksi", "Melihat seluruh transaksi pembelian paket.", "Komersial", editor},
	{"admin.users.view", "Lihat Pengguna", "Melihat daftar akun peserta dan staf.", "Sistem", staff},
	{"admin.users.manage", "Kelola Pengguna", "Membuat akun, mengubah role, dan menonaktifkan akun.", "Sistem", super},
	{"admin.organizations.view", "Lihat Organisasi Business", "Melihat organisasi Business dan statusnya.", "Sistem", super},
	{"admin.organizations.manage", "Kelola Organisasi Business", "Menyetujui, menangguhkan, dan mengubah status organisasi.", "Sistem", super},
	{"admin.access.manage", "Kelola Akses Paket", "Memberikan dan mencabut akses paket milik peserta.", "Sistem", editor},
	{"admin.audit.view", "Lihat Audit Trail", "Melihat log aktivitas dan investigasi kecurangan.", "Sistem", editor},
	{"admin.permissions.manage", "Kelola Role & Permission", "Mengubah hak akses setiap role untuk seluruh fitur.", "Sistem", super},
	{"business.workspace", "Akses Workspace Business", "Membuka dashboard organisasi, paket privat, dan hasil peserta.", "Business", all},
	{"business.manage", "Kelola Organisasi Business", "Mengelola paket, soal, modul, dan anggota organisasi.", "Business", roles("STUDENT")},
	{"lms.learn", "Belajar di LMS", "Membuka kelas e-learning, modul, progress, dan post-test.", "Peserta", all},
	{"exam.take", "Mengikuti Ujian", "Memulai, menjawab, dan melihat hasil ujian.", "Peserta", all},
	{"discussion.participate", "Diskusi Soal", "Membaca dan menulis diskusi pada pembahasan soal.", "Peserta", all},
	{"leaderboard.view", "Lihat Leaderboard", "Melihat peringkat peserta per paket ujian.", "Peserta", all},
}

var byKey = func() map[string]Definition {
	result := make(map[string]Definition, len(Catalog))
	for _, definition := range Catalog {
		result[definition.Key] = definition
	}
	return result
}()

func IsKnown(key string) bool {
	_, ok := byKey[key]
	return ok
}

type Service struct {
	Pool *pgxpool.Pool

	mu     sync.RWMutex
	cache  map[string]map[string]bool
	seeded bool
}

type MatrixRow struct {
	Key         string          `json:"key"`
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Group       string          `json:"group"`
	Roles       map[string]bool `json:"roles"`
}

type Update struct {
	Role          string `json:"role"`
	PermissionKey string `json:"permission_key"`
	Allowed       bool   `json:"allowed"`
}

func (s *Service) ensureSeeded(ctx context.Context) error {
	s.mu.RLock()
	seeded := s.seeded
	s.mu.RUnlock()
	if seeded {
		return nil
	}
	for _, definition := range Catalog {
		for _, role := range definition.DefaultRoles {
			_, err := s.Pool.Exec(ctx, `
				INSERT INTO role_permission (role, permission_key, allowed, updated_at)
				VALUES ($1::"Role", $2, true, now())
				ON CONFLICT (role, permission_key) DO NOTHING`, role, definition.Key)
			if err != nil {
				return err
			}
		}
	}
	s.mu.Lock()
	s.seeded = true
	s.mu.Unlock()
	return nil
}

func (s *Service) load(ctx context.Context) (map[string]map[string]bool, error) {
	if err := s.ensureSeeded(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	if s.cache != nil {
		cache := s.cache
		s.mu.RUnlock()
		return cache, nil
	}
	s.mu.RUnlock()

	rows, err := s.Pool.Query(ctx, `SELECT role, permission_key FROM role_permission WHERE allowed = true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cache := make(map[string]map[string]bool, len(RoleOrder))
	for _, role := range RoleOrder {
		cache[role] = map[string]bool{}
	}
	for rows.Next() {
		var role, key string
		if err := rows.Scan(&role, &key); err != nil {
			return nil, err
		}
		if _, ok := cache[role]; ok {
			cache[role][key] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cache = cache
	s.mu.Unlock()
	return cache, nil
}

func (s *Service) Invalidate() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
}

func (s *Service) KeysForRole(ctx context.Context, role string) ([]string, error) {
	if role == "SUPER_ADMIN" {
		keys := make([]string, 0, len(Catalog))
		for _, definition := range Catalog {
			keys = append(keys, definition.Key)
		}
		return keys, nil
	}
	cache, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(cache[role]))
	for key := range cache[role] {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func (s *Service) HasPermission(ctx context.Context, role, key string) (bool, error) {
	if role == "SUPER_ADMIN" {
		return true, nil
	}
	if !IsKnown(key) {
		return false, nil
	}
	cache, err := s.load(ctx)
	if err != nil {
		return false, err
	}
	return cache[role][key], nil
}

func (s *Service) Matrix(ctx context.Context) (map[string]any, error) {
	if err := s.ensureSeeded(ctx); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT role, permission_key, allowed FROM role_permission`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stored := map[string]bool{}
	for rows.Next() {
		var role, key string
		var allowed bool
		if err := rows.Scan(&role, &key, &allowed); err != nil {
			return nil, err
		}
		stored[role+":"+key] = allowed
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items := make([]MatrixRow, 0, len(Catalog))
	for _, definition := range Catalog {
		roleMap := make(map[string]bool, len(RoleOrder))
		for _, role := range RoleOrder {
			if role == "SUPER_ADMIN" {
				roleMap[role] = true
			} else {
				roleMap[role] = stored[role+":"+definition.Key]
			}
		}
		items = append(items, MatrixRow{
			Key:         definition.Key,
			Label:       definition.Label,
			Description: definition.Description,
			Group:       definition.Group,
			Roles:       roleMap,
		})
	}
	return map[string]any{"roles": RoleOrder, "permissions": items}, nil
}

func (s *Service) Update(ctx context.Context, updates []Update) (map[string]any, error) {
	if err := s.ensureSeeded(ctx); err != nil {
		return nil, err
	}
	normalized := make([]Update, 0, len(updates))
	for _, update := range updates {
		if !contains(RoleOrder, update.Role) {
			return nil, httpx.BadRequest("Role tidak dikenal: "+update.Role, "INVALID_ROLE")
		}
		if !IsKnown(update.PermissionKey) {
			return nil, httpx.BadRequest("Permission tidak dikenal: "+update.PermissionKey, "INVALID_PERMISSION")
		}
		if update.Role == "SUPER_ADMIN" {
			continue
		}
		normalized = append(normalized, update)
	}
	if len(normalized) == 0 {
		return s.Matrix(ctx)
	}

	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for _, update := range normalized {
			_, err := tx.Exec(ctx, `
				INSERT INTO role_permission (role, permission_key, allowed, updated_at)
				VALUES ($1::"Role", $2, $3, now())
				ON CONFLICT (role, permission_key)
				DO UPDATE SET allowed = EXCLUDED.allowed, updated_at = now()`,
				update.Role, update.PermissionKey, update.Allowed)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Invalidate()
	return s.Matrix(ctx)
}

func (s *Service) Reset(ctx context.Context) (map[string]any, error) {
	if _, err := s.Pool.Exec(ctx, `DELETE FROM role_permission`); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.seeded = false
	s.cache = nil
	s.mu.Unlock()
	return s.Matrix(ctx)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
