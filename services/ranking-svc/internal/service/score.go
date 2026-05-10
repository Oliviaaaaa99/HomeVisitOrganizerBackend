// Package service holds ranking logic. v1 is rule-based; v2 (next PR)
// will hand the same inputs to Claude for explainable ranking.
package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/ranking-svc/internal/store"
	"github.com/google/uuid"
)

// Reason is one line of "why this unit got this score" — shown to the user
// so the ranking isn't a black box. Sign tells us if it's a pro or con.
type Reason struct {
	Sign    string `json:"sign"` // "pro" | "con"
	Message string `json:"message"`
}

// RankedUnit is one entry in the response. Score is in [0, 10].
type RankedUnit struct {
	UnitID     string   `json:"unit_id"`
	PropertyID string   `json:"property_id"`
	Address    string   `json:"address"`
	UnitLabel  *string  `json:"unit_label,omitempty"`
	UnitType   string   `json:"unit_type"`
	Score      float64  `json:"score"`
	Reasons    []Reason `json:"reasons"`
}

// Ranker computes scores for all of a user's non-archived units.
type Ranker struct {
	store *store.Store
}

func NewRanker(s *store.Store) *Ranker {
	return &Ranker{store: s}
}

// Compute returns ranked units, highest score first.
//
// v1 scoring: start at 5.0 (neutral). Apply additive bumps for matches
// against the user's preferences and signals like shortlisted status; apply
// penalties for hard-constraint violations. Range is clamped to [0, 10].
//
// Each contribution comes with a Reason so the iOS UI can show "why".
// When v2 lands, Claude will rewrite the same set of contributions into
// natural-language pros/cons — the rule-based scaffold gives us a
// deterministic baseline to compare against.
func (r *Ranker) Compute(ctx context.Context, userID uuid.UUID) ([]RankedUnit, error) {
	prefs, err := r.store.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get prefs: %w", err)
	}
	units, err := r.store.ListUnitsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}

	out := make([]RankedUnit, 0, len(units))
	for _, u := range units {
		score, reasons := scoreUnit(u, prefs)
		out = append(out, RankedUnit{
			UnitID:     u.UnitID.String(),
			PropertyID: u.PropertyID.String(),
			Address:    u.Address,
			UnitLabel:  u.UnitLabel,
			UnitType:   u.UnitType,
			Score:      score,
			Reasons:    reasons,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

func scoreUnit(u *store.UnitForRanking, prefs *store.Preferences) (float64, []Reason) {
	score := 5.0
	reasons := []Reason{}

	// Already shortlisted? The user gave us strong positive signal.
	if u.Status == "shortlisted" {
		score += 1.5
		reasons = append(reasons, Reason{Sign: "pro", Message: "You've shortlisted this one"})
	}
	// Rejected stays in the list (still useful for comparison) but heavily downweighted.
	if u.Status == "rejected" {
		score -= 3.0
		reasons = append(reasons, Reason{Sign: "con", Message: "You marked this rejected"})
	}

	// Hard constraints.
	if prefs != nil {
		if u.PriceCents != nil {
			if prefs.BudgetMaxCents != nil && *u.PriceCents > *prefs.BudgetMaxCents {
				score -= 2.0
				reasons = append(reasons, Reason{
					Sign:    "con",
					Message: fmt.Sprintf("Over budget ($%d/mo vs $%d max)", *u.PriceCents/100, *prefs.BudgetMaxCents/100),
				})
			} else if prefs.BudgetMaxCents != nil {
				score += 0.5
				reasons = append(reasons, Reason{Sign: "pro", Message: "Within budget"})
			}
		}
		if u.Beds != nil && prefs.MinBeds != nil {
			if *u.Beds < *prefs.MinBeds {
				score -= 1.5
				reasons = append(reasons, Reason{
					Sign:    "con",
					Message: fmt.Sprintf("Only %d bedroom — you wanted %d+", *u.Beds, *prefs.MinBeds),
				})
			} else if *u.Beds == *prefs.MinBeds {
				reasons = append(reasons, Reason{Sign: "pro", Message: "Meets bedroom count"})
			} else {
				score += 0.3
				reasons = append(reasons, Reason{Sign: "pro", Message: "Extra bedroom over your minimum"})
			}
		}
		if u.Sqft != nil && prefs.MinSqft != nil && *u.Sqft >= *prefs.MinSqft {
			score += 0.4
		}
	}

	// Soft signal: $/sqft is reasonable. Anything under $5/sqft (rentals)
	// or under $1000/sqft (for_sale) is a small bump. Coarse but useful
	// before we have market comps.
	if u.PriceCents != nil && u.Sqft != nil && *u.Sqft > 0 {
		dollarPerSqft := float64(*u.PriceCents) / 100.0 / float64(*u.Sqft)
		if u.Kind == "rental" && dollarPerSqft < 5 {
			score += 0.5
			reasons = append(reasons, Reason{
				Sign:    "pro",
				Message: fmt.Sprintf("Strong $/sqft ($%.2f)", dollarPerSqft),
			})
		}
		if u.Kind == "for_sale" && dollarPerSqft < 1000 {
			score += 0.5
			reasons = append(reasons, Reason{
				Sign:    "pro",
				Message: fmt.Sprintf("Strong $/sqft ($%.0f)", dollarPerSqft),
			})
		}
	}

	// Clamp.
	if score < 0 {
		score = 0
	}
	if score > 10 {
		score = 10
	}
	return score, reasons
}
