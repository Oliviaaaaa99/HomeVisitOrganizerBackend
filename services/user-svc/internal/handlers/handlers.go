// Package handlers wires HTTP routes for user-svc.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	pg  *pgxpool.Pool
	rdb *redis.Client
}

// New constructs a Handlers from its dependencies.
func New(pg *pgxpool.Pool, rdb *redis.Client) *Handlers {
	return &Handlers{pg: pg, rdb: rdb}
}

// Health is the liveness probe — returns 200 unconditionally.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready is the readiness probe — verifies Postgres and Redis connectivity.
func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	resp := map[string]string{"postgres": "ok", "redis": "ok"}
	status := http.StatusOK

	if err := h.pg.Ping(r.Context()); err != nil {
		resp["postgres"] = err.Error()
		status = http.StatusServiceUnavailable
	}
	if err := h.rdb.Ping(r.Context()).Err(); err != nil {
		resp["redis"] = err.Error()
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, resp)
}

// Me returns the authenticated user. Stub for now — auth wiring lands in M1.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error": "not_implemented",
		"hint":  "auth not wired yet — coming in M1",
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
