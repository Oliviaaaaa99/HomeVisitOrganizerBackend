// Command server runs the user-svc HTTP API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/clients"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/handlers"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/service"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/store"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/authx"
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
	env := configx.String("ENV", "dev") // dev | staging | prod

	pgURL, err := configx.MustString("DATABASE_URL")
	if err != nil {
		return err
	}
	redisURL := configx.String("REDIS_URL", "redis://localhost:6379/0")

	jwtSecret, err := configx.MustString("JWT_SECRET")
	if err != nil {
		return err
	}
	accessTTL := configx.Duration("JWT_ACCESS_TTL", 1*time.Hour)
	refreshTTL := configx.Duration("REFRESH_TTL", 30*24*time.Hour)

	appleAudience := configx.String("APPLE_AUDIENCE", "")
	googleAudience := configx.String("GOOGLE_AUDIENCE", "")

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

	// Auth scaffolding
	users := store.NewUsers(pg)
	refresh := store.NewRefreshTokens(pg)
	jwtIssuer := authx.NewIssuer([]byte(jwtSecret), accessTTL)
	jwtVerifier := authx.NewVerifier([]byte(jwtSecret))

	idps := clients.NewRegistry()
	if env == "dev" {
		idps.Register("dev", clients.NewDevVerifier())
		slog.Info("dev id-token verifier enabled (provider=dev)")
	}
	if appleAudience != "" {
		v, err := clients.NewAppleVerifier(ctx, appleAudience)
		if err != nil {
			return err
		}
		idps.Register("apple", v)
		slog.Info("apple id-token verifier enabled", "audience", appleAudience)
	}
	if googleAudience != "" {
		v, err := clients.NewGoogleVerifier(ctx, googleAudience)
		if err != nil {
			return err
		}
		idps.Register("google", v)
		slog.Info("google id-token verifier enabled", "audience", googleAudience)
	}

	auth := service.NewAuth(idps, users, refresh, jwtIssuer, refreshTTL)

	// Optional S3 client for avatars. If S3_BUCKET isn't set, avatar endpoints
	// return 503 — useful so dev runs without LocalStack still come up cleanly.
	var s3Client *clients.S3
	if bucket := configx.String("S3_BUCKET", ""); bucket != "" {
		s3Cfg := clients.Config{
			Region:       configx.String("AWS_REGION", "us-east-1"),
			Bucket:       bucket,
			Endpoint:     configx.String("AWS_ENDPOINT_URL", ""),
			AccessKey:    configx.String("AWS_ACCESS_KEY_ID", ""),
			SecretKey:    configx.String("AWS_SECRET_ACCESS_KEY", ""),
			UsePathStyle: configx.String("AWS_S3_PATH_STYLE", "false") == "true",
			PresignTTL:   configx.Duration("PRESIGN_TTL", 5*time.Minute),
		}
		s3Client, err = clients.NewS3(ctx, s3Cfg)
		if err != nil {
			return err
		}
		slog.Info("avatar uploads enabled", "bucket", bucket)
	} else {
		slog.Info("S3_BUCKET not set — avatar endpoints disabled")
	}

	h := handlers.New(pg, rdb, auth, users, s3Client)

	r := chi.NewRouter()
	r.Use(httpx.RequestID, httpx.Logger, httpx.Recoverer)

	r.Get("/healthz", h.Health)
	r.Get("/readyz", h.Ready)

	r.Route("/v1", func(r chi.Router) {
		// Public
		r.Post("/auth/exchange", h.Exchange)
		r.Post("/auth/refresh", h.Refresh)

		// Authenticated
		r.Group(func(r chi.Router) {
			r.Use(authx.Middleware(jwtVerifier))
			r.Get("/users/me", h.Me)
			r.Patch("/users/me", h.UpdateMe)
			r.Post("/users/me/avatar:presign", h.PresignAvatar)
			r.Post("/users/me/avatar:commit", h.CommitAvatar)
			r.Delete("/users/me/avatar", h.DeleteAvatar)
		})
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
		slog.Info("listening", "addr", addr, "env", env)
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
	opts, err := redis.ParseURL(strings.TrimSpace(url))
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
