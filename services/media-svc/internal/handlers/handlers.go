// Package handlers wires HTTP routes for media-svc.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/media-svc/internal/service"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/authx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	pg    *pgxpool.Pool
	media *service.Media
}

// New constructs a Handlers from its dependencies.
func New(pg *pgxpool.Pool, media *service.Media) *Handlers {
	return &Handlers{pg: pg, media: media}
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

func authedUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	sub := authx.UserIDFrom(r.Context())
	if sub == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing user")
		return uuid.Nil, false
	}
	id, err := uuid.Parse(sub)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_subject", err.Error())
		return uuid.Nil, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, map[string]string{"error": code, "detail": detail})
}
