// Command server runs the media-svc HTTP API.
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

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/media-svc/internal/clients"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/media-svc/internal/handlers"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/media-svc/internal/service"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/media-svc/internal/store"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/authx"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/configx"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/dbx"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/httpx"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/logx"
	"github.com/go-chi/chi/v5"
)

const serviceName = "media-svc"

func main() {
	log := logx.New(serviceName, slog.LevelInfo)
	slog.SetDefault(log)

	if err := run(); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	addr := configx.String("HTTP_ADDR", ":8083")
	pgURL, err := configx.MustString("DATABASE_URL")
	if err != nil {
		return err
	}
	jwtSecret, err := configx.MustString("JWT_SECRET")
	if err != nil {
		return err
	}

	// Tigris (Fly's managed S3-compatible storage) sets BUCKET_NAME and
	// AWS_ENDPOINT_URL_S3; our LocalStack dev setup uses S3_BUCKET and
	// AWS_ENDPOINT_URL. Accept either so the same binary works in both.
	s3Cfg := clients.Config{
		Region:       configx.String("AWS_REGION", "us-east-1"),
		Bucket:       configx.StringFirst("hvo-media-dev", "BUCKET_NAME", "S3_BUCKET"),
		Endpoint:     configx.StringFirst("", "AWS_ENDPOINT_URL_S3", "AWS_ENDPOINT_URL"),
		AccessKey:    configx.String("AWS_ACCESS_KEY_ID", ""),
		SecretKey:    configx.String("AWS_SECRET_ACCESS_KEY", ""),
		UsePathStyle: configx.String("AWS_S3_PATH_STYLE", "false") == "true",
		PresignTTL:   configx.Duration("PRESIGN_TTL", 5*time.Minute),
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pg, err := dbx.Connect(ctx, dbx.DefaultConfig(pgURL))
	if err != nil {
		return err
	}
	defer pg.Close()

	s3, err := clients.NewS3(ctx, s3Cfg)
	if err != nil {
		return err
	}
	if configx.String("S3_AUTO_CREATE_BUCKET", "false") == "true" {
		if err := s3.EnsureBucket(ctx); err != nil {
			return err
		}
		slog.Info("ensured s3 bucket", "bucket", s3Cfg.Bucket)
	}

	// Apply CORS to the bucket on every boot. Tigris virtual-hosted URLs
	// (where browser uploads land) don't inherit the org-level CORS, so we
	// have to set it bucket-side. Idempotent — PutBucketCors fully replaces
	// the existing policy. Best-effort: if it fails (transient network, etc.)
	// we keep booting; the worst case is one cold start where web uploads
	// don't work and they retry.
	if err := s3.EnsureBucketCors(ctx); err != nil {
		slog.Warn("EnsureBucketCors failed — web uploads may be blocked by browser CORS until next boot", "err", err)
	} else {
		slog.Info("ensured s3 bucket CORS", "bucket", s3Cfg.Bucket)
	}

	owner := store.NewOwnership(pg)
	media := store.NewMedia(pg)
	svc := service.NewMedia(owner, media, s3)
	jwtVerifier := authx.NewVerifier([]byte(jwtSecret))
	h := handlers.New(pg, svc)

	r := chi.NewRouter()
	r.Use(httpx.CORS(configx.String("CORS_ALLOWED_ORIGIN", "*")), httpx.RequestID, httpx.Logger, httpx.Recoverer)

	r.Get("/healthz", h.Health)
	r.Get("/readyz", h.Ready)

	r.Route("/v1", func(r chi.Router) {
		r.Use(authx.Middleware(jwtVerifier))
		// Verb-style sub-paths (`...:presign` / `...:commit`) follow Google's
		// AIP-136 — clearer than POST /presigns and easier to scan than a
		// query param.
		r.Post("/units/{id}/media:presign", h.Presign)
		r.Post("/units/{id}/media:commit", h.Commit)
		r.Get("/units/{id}/media", h.List)
		r.Patch("/media/{id}", h.Update)
		r.Delete("/media/{id}", h.Delete)
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
		slog.Info("listening", "addr", addr, "bucket", s3Cfg.Bucket)
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
