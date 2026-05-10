// Package handlers wires HTTP routes for ranking-svc.
package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/ranking-svc/internal/service"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/ranking-svc/internal/store"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/authx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handlers struct {
	pg     *pgxpool.Pool
	store  *store.Store
	ranker *service.Ranker
}

func New(pg *pgxpool.Pool, s *store.Store, r *service.Ranker) *Handlers {
	return &Handlers{pg: pg, store: s, ranker: r}
}

func (h *Handlers) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	if err := h.pg.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"postgres": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"postgres": "ok"})
}

type prefsResponse struct {
	WorkAddress    *string  `json:"work_address,omitempty"`
	WorkLat        *float64 `json:"work_lat,omitempty"`
	WorkLng        *float64 `json:"work_lng,omitempty"`
	BudgetMinCents *int64   `json:"budget_min_cents,omitempty"`
	BudgetMaxCents *int64   `json:"budget_max_cents,omitempty"`
	MinBeds        *int     `json:"min_beds,omitempty"`
	MinBaths       *float64 `json:"min_baths,omitempty"`
	MinSqft        *int     `json:"min_sqft,omitempty"`
	WeightPrice    *int     `json:"weight_price,omitempty"`
	WeightSize     *int     `json:"weight_size,omitempty"`
	WeightCommute  *int     `json:"weight_commute,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
}

func toPrefsResponse(p *store.Preferences) prefsResponse {
	if p == nil {
		return prefsResponse{}
	}
	return prefsResponse{
		WorkAddress:    p.WorkAddress,
		WorkLat:        p.WorkLat,
		WorkLng:        p.WorkLng,
		BudgetMinCents: p.BudgetMinCents,
		BudgetMaxCents: p.BudgetMaxCents,
		MinBeds:        p.MinBeds,
		MinBaths:       p.MinBaths,
		MinSqft:        p.MinSqft,
		WeightPrice:    p.WeightPrice,
		WeightSize:     p.WeightSize,
		WeightCommute:  p.WeightCommute,
		UpdatedAt:      p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// GetPreferences handles GET /v1/preferences.
func (h *Handlers) GetPreferences(w http.ResponseWriter, r *http.Request) {
	uid, ok := authedUUID(w, r)
	if !ok {
		return
	}
	p, err := h.store.Get(r.Context(), uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load_failed"})
		return
	}
	// Returning the empty shape (all nil) when no row exists keeps the
	// client simple — it can render its form against the same response
	// shape regardless of whether prefs have ever been saved.
	writeJSON(w, http.StatusOK, toPrefsResponse(p))
}

type prefsRequest struct {
	WorkAddress    *string  `json:"work_address,omitempty"`
	WorkLat        *float64 `json:"work_lat,omitempty"`
	WorkLng        *float64 `json:"work_lng,omitempty"`
	BudgetMinCents *int64   `json:"budget_min_cents,omitempty"`
	BudgetMaxCents *int64   `json:"budget_max_cents,omitempty"`
	MinBeds        *int     `json:"min_beds,omitempty"`
	MinBaths       *float64 `json:"min_baths,omitempty"`
	MinSqft        *int     `json:"min_sqft,omitempty"`
	WeightPrice    *int     `json:"weight_price,omitempty"`
	WeightSize     *int     `json:"weight_size,omitempty"`
	WeightCommute  *int     `json:"weight_commute,omitempty"`
}

// UpsertPreferences handles PUT /v1/preferences.
func (h *Handlers) UpsertPreferences(w http.ResponseWriter, r *http.Request) {
	uid, ok := authedUUID(w, r)
	if !ok {
		return
	}
	var req prefsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	p, err := h.store.Upsert(r.Context(), store.UpsertInput{
		UserID:         uid,
		WorkAddress:    req.WorkAddress,
		WorkLat:        req.WorkLat,
		WorkLng:        req.WorkLng,
		BudgetMinCents: req.BudgetMinCents,
		BudgetMaxCents: req.BudgetMaxCents,
		MinBeds:        req.MinBeds,
		MinBaths:       req.MinBaths,
		MinSqft:        req.MinSqft,
		WeightPrice:    req.WeightPrice,
		WeightSize:     req.WeightSize,
		WeightCommute:  req.WeightCommute,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save_failed"})
		return
	}
	writeJSON(w, http.StatusOK, toPrefsResponse(p))
}

type rankResponse struct {
	Items []service.RankedUnit `json:"items"`
}

// ComputeRanking handles POST /v1/rankings:compute. v1 is rule-based; v2
// will hand the same inputs to Claude and return the same shape.
func (h *Handlers) ComputeRanking(w http.ResponseWriter, r *http.Request) {
	uid, ok := authedUUID(w, r)
	if !ok {
		return
	}
	items, err := h.ranker.Compute(r.Context(), uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "compute_failed"})
		return
	}
	writeJSON(w, http.StatusOK, rankResponse{Items: items})
}

func authedUUID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	sub := authx.UserIDFrom(r.Context())
	if sub == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return uuid.Nil, false
	}
	uid, err := uuid.Parse(sub)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_subject"})
		return uuid.Nil, false
	}
	return uid, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
