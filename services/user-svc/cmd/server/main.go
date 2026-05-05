// Command server runs the user-svc HTTP API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/handlers"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/configx"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/dbx"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/httpx"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/logx"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

const serviceName = "user-svc"

func main() {
	log := logx.New(serviceName, slog.LevelInfo)
	slog.SetDefault(log)

	if err := run(); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	addr := configx.String("HTTP_ADDR", ":8080")
	pgURL, err := configx.MustString("DATABASE_URL")
	if err != nil {
		return err
	}
	redisURL := configx.String("REDIS_URL", "redis://localhost:6379/0")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pg, err := dbx.Connect(ctx, dbx.DefaultConfig(pgURL))
	if err != nil {
		return err
	}
	defer pg.Close()

	rdb, err := redisClient(ctx, redisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()

	r := chi.NewRouter()
	r.Use(httpx.RequestID, httpx.Logger, httpx.Recoverer)

	h := handlers.New(pg, rdb)
	r.Get("/healthz", h.Health)
	r.Get("/readyz", h.Ready)

	r.Route("/v1", func(r chi.Router) {
		r.Get("/users/me", h.Me)
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return srv.Shutdown(shutdownCtx)
}

func redisClient(ctx context.Context, url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	c := redis.NewClient(opts)
	if err := c.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}
