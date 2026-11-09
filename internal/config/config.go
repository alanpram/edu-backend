// Package config memuat konfigurasi environment untuk EDUMASTER API (Go).
package config

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config adalah konfigurasi runtime yang tervalidasi saat startup.
type Config struct {
	Environment string
	Port        string
	AppName     string
	APIPrefix   string
	CORSOrigins []string
	// TrustProxy hanya boleh true bila API berada di belakang reverse proxy
	// tepercaya. Default false agar X-Forwarded-For tidak dapat dipalsukan.
	TrustProxy bool

	DatabaseURL string
	RedisURL    string

	JWTSecret    string
	JWTExpiresIn time.Duration
	BcryptRounds int

	CookieName     string
	CSRFCookieName string
	CookieSecure   bool
	CookieSameSite string
	CookieDomain   string

	FreeAccessDays    int
	PaidAccessDays    int
	PrivateAccessDays int
	PaymentProvider   string

	FrontendURL            string
	CertificateVerifyBase  string
	CertificateIssuerName  string
	CertificateIssuerTitle string
	EmailVerificationTTL   time.Duration
	MailEnabled            bool
	MailHost               string
	MailPort               int
	MailUsername           string
	MailPassword           string
	MailEncryption         string
	MailFromAddress        string
	MailFromName           string
	MailReplyTo            string
	RecaptchaEnabled       bool
	RecaptchaSecretKey     string
	RecaptchaVerifyURL     string
	RecaptchaMinScore      float64
	UploadDir              string
	UploadMaxMB            int64
	IsProduction           bool
}

// Load membaca .env (jika ada) lalu environment proses. Environment proses
// selalu menang agar deployment container dapat melakukan override.
func Load() (*Config, error) {
	_ = loadDotEnv(".env")
	if _, err := os.Stat(".env"); err != nil {
		_ = loadDotEnv(filepath.Join("..", "backend", ".env"))
	}

	cfg := &Config{
		Environment:            env("NODE_ENV", "development"),
		Port:                   env("PORT", "4000"),
		AppName:                env("APP_NAME", "EDUMASTER API"),
		APIPrefix:              env("API_PREFIX", "/api"),
		CORSOrigins:            normalizeOrigins(splitCSV(env("CORS_ORIGIN", "http://localhost:3000"))),
		TrustProxy:             envBool("TRUST_PROXY", false),
		DatabaseURL:            env("DATABASE_URL", ""),
		RedisURL:               env("REDIS_URL", ""),
		JWTSecret:              env("JWT_SECRET", ""),
		BcryptRounds:           envInt("BCRYPT_ROUNDS", 12),
		CookieName:             env("COOKIE_NAME", "edumaster_session"),
		CSRFCookieName:         env("CSRF_COOKIE_NAME", "edumaster_csrf"),
		CookieSameSite:         env("COOKIE_SAMESITE", "lax"),
		CookieDomain:           env("COOKIE_DOMAIN", ""),
		FreeAccessDays:         envInt("FREE_ACCESS_DAYS", 365),
		PaidAccessDays:         envInt("PAID_ACCESS_DAYS", 365),
		PrivateAccessDays:      envInt("PRIVATE_ACCESS_DAYS", 365),
		PaymentProvider:        env("PAYMENT_PROVIDER", "mock"),
		FrontendURL:            strings.TrimRight(env("FRONTEND_URL", "http://localhost:3000"), "/"),
		CertificateVerifyBase:  strings.TrimRight(env("CERTIFICATE_VERIFY_BASE_URL", "http://localhost:3000"), "/"),
		CertificateIssuerName:  env("CERTIFICATE_ISSUER_NAME", "EduMaster Academy"),
		CertificateIssuerTitle: env("CERTIFICATE_ISSUER_TITLE", "Authorized Issuer"),
		MailEnabled:            envBool("MAIL_ENABLED", true),
		MailHost:               env("MAIL_HOST", ""),
		MailPort:               envInt("MAIL_PORT", 587),
		MailUsername:           env("MAIL_USERNAME", ""),
		MailPassword:           env("MAIL_PASSWORD", ""),
		MailEncryption:         env("MAIL_ENCRYPTION", "tls"),
		MailFromAddress:        env("MAIL_FROM_ADDRESS", "no-reply@edumaster.id"),
		MailFromName:           env("MAIL_FROM_NAME", "EduMaster"),
		MailReplyTo:            env("MAIL_REPLY_TO", ""),
		RecaptchaEnabled:       envBool("RECAPTCHA_ENABLED", true),
		RecaptchaSecretKey:     env("RECAPTCHA_SECRET_KEY", ""),
		RecaptchaVerifyURL:     env("RECAPTCHA_VERIFY_URL", "https://www.google.com/recaptcha/api/siteverify"),
		RecaptchaMinScore:      envFloat("RECAPTCHA_MIN_SCORE", 0.5),
		UploadDir:              env("UPLOAD_DIR", "uploads"),
		UploadMaxMB:            int64(envInt("UPLOAD_MAX_MB", 5)),
	}

	cfg.IsProduction = cfg.Environment == "production"
	cfg.CookieSecure = envBool("COOKIE_SECURE", cfg.IsProduction)

	expires, err := parseDuration(env("JWT_EXPIRES_IN", "12h"))
	if err != nil {
		return nil, fmt.Errorf("JWT_EXPIRES_IN tidak valid: %w", err)
	}
	cfg.JWTExpiresIn = expires

	ttl, err := parseDuration(fmt.Sprintf("%dh", envInt("EMAIL_VERIFICATION_TTL_HOURS", 24)))
	if err != nil {
		return nil, fmt.Errorf("EMAIL_VERIFICATION_TTL_HOURS tidak valid: %w", err)
	}
	cfg.EmailVerificationTTL = ttl

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL wajib diisi")
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET minimal 32 karakter")
	}
	if cfg.BcryptRounds < 8 || cfg.BcryptRounds > 15 {
		return nil, fmt.Errorf("BCRYPT_ROUNDS harus di antara 8 dan 15")
	}
	if len(cfg.CORSOrigins) == 0 {
		return nil, fmt.Errorf("CORS_ORIGIN wajib diisi")
	}
	for _, origin := range cfg.CORSOrigins {
		if origin == "*" {
			return nil, fmt.Errorf("CORS_ORIGIN tidak boleh '*' karena autentikasi cookie memerlukan origin eksplisit")
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, fmt.Errorf("CORS_ORIGIN tidak valid: %s", origin)
		}
	}
	switch strings.ToLower(strings.TrimSpace(cfg.CookieSameSite)) {
	case "lax", "strict", "none":
	default:
		return nil, fmt.Errorf("COOKIE_SAMESITE harus lax, strict, atau none")
	}
	if strings.EqualFold(cfg.CookieSameSite, "none") && !cfg.CookieSecure {
		return nil, fmt.Errorf("COOKIE_SAMESITE=none memerlukan COOKIE_SECURE=true")
	}

	return cfg, nil
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}
	return value
}

func envFloat(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(env(key, strconv.FormatFloat(fallback, 'f', -1, 64)), 64)
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(env(key, strconv.FormatBool(fallback)))
	return value != "false" && value != "0" && value != "no" && value != "off"
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// normalizeOrigins menyelaraskan bentuk origin dengan nilai header Origin yang
// dikirim browser (tanpa trailing slash).
func normalizeOrigins(origins []string) []string {
	result := make([]string, 0, len(origins))
	for _, origin := range origins {
		result = append(result, strings.TrimRight(origin, "/"))
	}
	return result
}

// parseDuration mendukung format Go (12h, 30m) dan hari (7d).
func parseDuration(value string) (time.Duration, error) {
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}
