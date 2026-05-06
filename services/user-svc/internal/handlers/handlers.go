// Package handlers wires HTTP routes for user-svc.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/service"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/store"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/authx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	pg    *pgxpool.Pool
	rdb   *redis.Client
	auth  *service.Auth
	users *store.Users
}

// New constructs a Handlers from its dependencies.
func New(pg *pgxpool.Pool, rdb *redis.Client, auth *service.Auth, users *store.Users) *Handlers {
	return &Handlers{pg: pg, rdb: rdb, auth: auth, users: users}
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

type meResponse struct {
	ID        string  `json:"id"`
	Provider  string  `json:"provider"`
	EmailHash *string `json:"email_hash,omitempty"`
	CreatedAt string  `json:"created_at"`
}

// Me returns the authenticated user. Requires auth middleware in front.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	uidStr := authx.UserIDFrom(r.Context())
	if uidStr == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return
	}
	uid, err := uuid.Parse(uidStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_subject"})
		return
	}
	user, err := h.users.FindByID(r.Context(), uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "lookup_failed"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user_gone"})
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		ID:        user.ID.String(),
		Provider:  user.Provider,
		EmailHash: user.EmailHash,
		CreatedAt: user.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
