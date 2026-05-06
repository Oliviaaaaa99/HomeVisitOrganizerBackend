// Command server runs the property-svc HTTP API.
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

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/property-svc/internal/handlers"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/property-svc/internal/store"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/authx"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/configx"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/dbx"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/httpx"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/logx"
	"github.com/go-chi/chi/v5"
)

const serviceName = "property-svc"

func main() {
	log := logx.New(serviceName, slog.LevelInfo)
	slog.SetDefault(log)

	if err := run(); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	addr := configx.String("HTTP_ADDR", ":8082")
	pgURL, err := configx.MustString("DATABASE_URL")
	if err != nil {
		return err
	}
	jwtSecret, err := configx.MustString("JWT_SECRET")
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pg, err := dbx.Connect(ctx, dbx.DefaultConfig(pgURL))
	if err != nil {
		return err
	}
	defer pg.Close()

	properties := store.NewProperties(pg)
	units := store.NewUnits(pg)
	notes := store.NewNotes(pg)
	jwtVerifier := authx.NewVerifier([]byte(jwtSecret))

	h := handlers.New(pg, properties, units, notes)

	r := chi.NewRouter()
	r.Use(httpx.RequestID, httpx.Logger, httpx.Recoverer)

	r.Get("/healthz", h.Health)
	r.Get("/readyz", h.Ready)

	r.Route("/v1", func(r chi.Router) {
		r.Use(authx.Middleware(jwtVerifier))
		r.Post("/properties", h.CreateProperty)
		r.Get("/properties", h.ListProperties)
		r.Route("/properties/{id}", func(r chi.Router) {
			r.Get("/", h.GetProperty)
			r.Patch("/", h.UpdateProperty)
			r.Delete("/", h.ArchiveProperty)
			r.Post("/units", h.CreateUnit)
			r.Post("/notes", h.CreateNote)
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
