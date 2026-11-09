// Package api merangkai router dan handler HTTP EDUMASTER.
package api

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/edumaster/backend-go/internal/activity"
	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/config"
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
	"github.com/edumaster/backend-go/internal/permissions"
	"github.com/edumaster/backend-go/internal/services"
)

type Server struct {
	Config      *config.Config
	Pool        *pgxpool.Pool
	Redis       *redis.Client
	Auth        *auth.Middleware
	Permissions *permissions.Service
	Activity    *activity.Service
	Captcha     *services.Captcha
	Mail        *services.Mail
	StartedAt   time.Time
	UploadDir   string

	apiLimit    *middleware.Limiter
	authLimit   *middleware.Limiter
	verifyLimit *middleware.Limiter
	certLimit   *middleware.Limiter
	redeemLimit *middleware.Limiter
}

func New(cfg *config.Config, pool *pgxpool.Pool, redisClient *redis.Client) *Server {
	uploadDir, _ := filepath.Abs(cfg.UploadDir)
	_ = os.MkdirAll(uploadDir, 0o755)

	var limiterStore middleware.Store
	if redisClient != nil {
		limiterStore = middleware.NewRedisStore(redisClient)
	}
	limiter := func(name string, limit int, window time.Duration, message string) *middleware.Limiter {
		return middleware.NewLimiterWithStore(name, limit, window, message, limiterStore)
	}

	return &Server{
		Config: cfg,
		Pool:   pool,
		Redis:  redisClient,
		Auth: &auth.Middleware{
			Pool:           pool,
			Secret:         cfg.JWTSecret,
			CookieName:     cfg.CookieName,
			CSRFCookieName: cfg.CSRFCookieName,
			CookieSecure:   cfg.CookieSecure,
			CookieSameSite: cfg.CookieSameSite,
			CookieDomain:   cfg.CookieDomain,
			TokenTTL:       cfg.JWTExpiresIn,
		},
		Permissions: &permissions.Service{Pool: pool},
		Activity:    &activity.Service{Pool: pool},
		Captcha:     &services.Captcha{Config: cfg},
		Mail:        &services.Mail{Config: cfg},
		StartedAt:   time.Now(),
		UploadDir:   uploadDir,
		apiLimit:    limiter("api", 300, time.Minute, "Terlalu banyak permintaan. Silakan coba lagi."),
		authLimit:   limiter("auth", 30, 15*time.Minute, "Terlalu banyak percobaan autentikasi. Coba lagi dalam 15 menit."),
		verifyLimit: limiter("verify", 8, 15*time.Minute, "Terlalu banyak permintaan verifikasi. Coba lagi dalam 15 menit."),
		certLimit:   limiter("cert", 60, 15*time.Minute, "Terlalu banyak permintaan verifikasi sertifikat. Coba lagi nanti."),
		redeemLimit: limiter("redeem", 20, 10*time.Minute, "Terlalu banyak percobaan kode akses. Coba lagi nanti."),
	}
}

func (s *Server) Router() http.Handler {
	httpx.SetPathParam(chi.URLParam)

	router := chi.NewRouter()
	router.Use(middleware.Recoverer)
	router.Use(middleware.SecurityHeaders)
	router.Use(middleware.ClientIP(s.Config.TrustProxy))
	router.Use(middleware.CORS(s.Config.CORSOrigins))
	router.Use(middleware.Logger)

	router.Get("/", httpx.Wrap(s.root))
	router.Handle("/uploads/*", s.uploadsHandler())

	router.Route(s.Config.APIPrefix, func(api chi.Router) {
		api.Use(s.apiLimit.Middleware)
		api.Get("/health", httpx.Wrap(s.health))
		s.registerAuth(api)
		s.registerPermissions(api)
		s.registerUsers(api)
		s.registerActivity(api)
		s.registerPackages(api)
		s.registerAccess(api)
		s.registerSoal(api)
		s.registerExams(api)
		s.registerLMS(api)
		s.registerBusiness(api)
		s.registerLanding(api)
		s.registerContent(api)
		s.registerCoupons(api)
		s.registerTransactions(api)
		s.registerCertificates(api)
		s.registerLeaderboard(api)
		s.registerDiscussions(api)
		s.registerUploads(api)
	})

	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, httpx.NotFound("Endpoint "+r.Method+" "+r.URL.RequestURI()+" tidak ditemukan", "ENDPOINT_NOT_FOUND"))
	})
	return router
}

// uploadsHandler menyajikan berkas media tanpa directory listing. Nama berkas
// memakai UUID sehingga cache jangka panjang aman dipakai.
func (s *Server) uploadsHandler() http.Handler {
	files := http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.UploadDir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/uploads/" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
		files.ServeHTTP(w, r)
	})
}

func (s *Server) root(w http.ResponseWriter, _ *http.Request) error {
	httpx.Success(w, map[string]any{
		"name":        s.Config.AppName,
		"docs":        "Lihat backend/API_DOCUMENTATION.md",
		"api_prefix":  s.Config.APIPrefix,
		"environment": s.Config.Environment,
	}, s.Config.AppName+" berjalan")
	return nil
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) error {
	httpx.Success(w, map[string]any{
		"status":         "ok",
		"timestamp":      time.Now().UTC().Format(time.RFC3339Nano),
		"uptime_seconds": int(time.Since(s.StartedAt).Seconds()),
	}, "EDUMASTER API is running")
	return nil
}

// Shutdown tersedia untuk pemanggilan graceful shutdown dari main.
func (s *Server) Shutdown(ctx context.Context) {
	if s.Redis != nil {
		if err := s.Redis.Close(); err != nil {
			log.Printf("Redis ditutup dengan error: %v", err)
		} else {
			log.Println("koneksi Redis ditutup")
		}
	}
	done := make(chan struct{})
	go func() {
		s.Pool.Close()
		close(done)
	}()
	select {
	case <-done:
		log.Println("database pool ditutup")
	case <-ctx.Done():
		log.Println("timeout menutup database pool")
	}
}
