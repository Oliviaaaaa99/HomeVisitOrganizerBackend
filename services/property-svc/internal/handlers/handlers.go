// Package handlers wires HTTP routes for property-svc.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/property-svc/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	pg         *pgxpool.Pool
	properties *store.Properties
	units      *store.Units
	notes      *store.Notes
}

// New constructs a Handlers from its dependencies.
func New(pg *pgxpool.Pool, properties *store.Properties, units *store.Units, notes *store.Notes) *Handlers {
	return &Handlers{pg: pg, properties: properties, units: units, notes: notes}
}

// Health is the liveness probe — returns 200 unconditionally.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready is the readiness probe — verifies Postgres connectivity.
func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	if err := h.pg.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"postgres": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"postgres": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, map[string]string{"error": code, "detail": detail})
}
