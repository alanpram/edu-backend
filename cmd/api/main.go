package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/edumaster/backend-go/internal/api"
	"github.com/edumaster/backend-go/internal/config"
	"github.com/edumaster/backend-go/internal/db"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[BOOTSTRAP FAILED] %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[BOOTSTRAP FAILED] %v", err)
	}

	var redisClient *redis.Client
	if cfg.RedisURL != "" {
		redisClient, err = db.OpenRedis(ctx, cfg.RedisURL)
		if err != nil {
			log.Printf("[WARN] Redis tidak tersedia, rate limit memakai memori proses: %v", err)
			redisClient = nil
		} else {
			log.Println("rate limit memakai Redis terpusat")
		}
	}

	server := api.New(cfg, pool, redisClient)
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		log.Printf("[%s] listening on http://localhost:%s%s", cfg.AppName, cfg.Port, cfg.APIPrefix)
		log.Printf("[%s] environment: %s", cfg.AppName, cfg.Environment)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[HTTP FAILED] %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutdown signal received")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	server.Shutdown(shutdownCtx)
}
