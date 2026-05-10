// Package service holds ranking logic.
//
// We support two rankers behind a uniform interface:
//
//   - LLM-as-ranker (Claude Haiku): structured prompt → JSON ranking.
//     This is the default when ANTHROPIC_API_KEY is set.
//   - Rule-based scoring: deterministic Go function, used as the
//     fallback when no API key is configured AND when the LLM call
//     fails for any reason. Always available.
//
// Both produce the same RankedUnit shape, so the iOS UI doesn't care
// which path produced the result.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/ranking-svc/internal/clients"
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
	store  *store.Store
	claude *clients.Claude // nil = rule-based only
}

// NewRanker takes an optional Claude client. Pass nil to force the
// rule-based path (used when ANTHROPIC_API_KEY is not set).
func NewRanker(s *store.Store, c *clients.Claude) *Ranker {
	return &Ranker{store: s, claude: c}
}

// Compute returns ranked units, highest score first.
//
// Path:
//   - Claude-backed if a client is configured (ANTHROPIC_API_KEY set).
//   - Rule-based if Claude is nil OR if the LLM call fails for any
//     reason. We log + degrade silently — ranking should never fail
//     just because Anthropic is having a bad day.
func (r *Ranker) Compute(ctx context.Context, userID uuid.UUID) ([]RankedUnit, error) {
	prefs, err := r.store.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get prefs: %w", err)
	}
	units, err := r.store.ListUnitsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}
	if len(units) == 0 {
		return []RankedUnit{}, nil
	}

	if r.claude != nil {
		out, err := r.rankWithClaude(ctx, units, prefs)
		if err == nil {
			return out, nil
		}
		// Don't fail the request — the rule-based fallback is genuinely
		// usable. Log and continue.
		slog.Warn("claude rank failed, falling back to rules", "err", err)
	}
	return rankWithRules(units, prefs), nil
}

func rankWithRules(units []*store.UnitForRanking, prefs *store.Preferences) []RankedUnit {
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
	return out
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

// claudeRankResponse is the JSON shape we ask Claude to return. Keeping
// it loose (string IDs, plain reasons) is intentional: the model is more
// reliable when it doesn't have to infer numeric formats or enums.
type claudeRankResponse struct {
	Items []claudeRankItem `json:"items"`
}

type claudeRankItem struct {
	UnitID  string                  `json:"unit_id"`
	Score   float64                 `json:"score"`
	Reasons []claudeRankReasonEntry `json:"reasons"`
}

type claudeRankReasonEntry struct {
	Sign    string `json:"sign"`
	Message string `json:"message"`
}

// rankWithClaude builds a structured prompt, asks Claude to rank, parses
// the response, and joins the result back to the original units. Returns
// an error so the caller can decide to fall back.
func (r *Ranker) rankWithClaude(ctx context.Context, units []*store.UnitForRanking, prefs *store.Preferences) ([]RankedUnit, error) {
	system := `You are an apartment-search assistant. Given a user's preferences and a list of candidate apartments, rank the apartments and explain your reasoning.

Return STRICT JSON in this exact shape — no prose around it, no code fences:

{"items":[{"unit_id":"<uuid>","score":<0-10 number>,"reasons":[{"sign":"pro|con","message":"<plain English, 1 sentence>"}]}]}

Scoring rules:
- 0 = terrible match, 10 = perfect match. Use the full range.
- A unit the user already shortlisted should score noticeably higher.
- A unit the user marked rejected should score very low.
- Hard-constraint misses (over budget, below minimum bedroom count) lower the score; meeting them does not raise it dramatically.
- 2-4 reasons per unit. Each reason is one specific, concrete sentence — refer to actual numbers from the data, not generic advice.
- The final list should be sorted by score, highest first.`

	user := buildClaudePrompt(units, prefs)

	raw, err := r.claude.Complete(ctx, system, user, 4096)
	if err != nil {
		return nil, fmt.Errorf("claude call: %w", err)
	}
	jsonText := extractJSON(raw)
	var parsed claudeRankResponse
	if err := json.Unmarshal([]byte(jsonText), &parsed); err != nil {
		return nil, fmt.Errorf("decode claude json: %w (raw=%q)", err, raw)
	}

	// Index units by id so we can join Claude's output back to the rich
	// metadata we won't ask it to echo.
	byID := make(map[string]*store.UnitForRanking, len(units))
	for _, u := range units {
		byID[u.UnitID.String()] = u
	}

	out := make([]RankedUnit, 0, len(parsed.Items))
	for _, it := range parsed.Items {
		u, ok := byID[it.UnitID]
		if !ok {
			continue // Claude hallucinated a unit_id; skip rather than crash.
		}
		score := it.Score
		if score < 0 {
			score = 0
		}
		if score > 10 {
			score = 10
		}
		reasons := make([]Reason, 0, len(it.Reasons))
		for _, rsn := range it.Reasons {
			sign := rsn.Sign
			if sign != "pro" && sign != "con" {
				continue
			}
			reasons = append(reasons, Reason{Sign: sign, Message: rsn.Message})
		}
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

	// If Claude dropped some units (rare), append rule-based scores for
	// the missing ones at the bottom so the user still sees them.
	seen := make(map[string]struct{}, len(out))
	for _, ru := range out {
		seen[ru.UnitID] = struct{}{}
	}
	for _, u := range units {
		if _, ok := seen[u.UnitID.String()]; ok {
			continue
		}
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

// buildClaudePrompt serializes prefs + units into a compact, deterministic
// text block. We don't trust Claude to handle JSON-in-JSON consistently;
// readable text + ids gives the cleanest results.
func buildClaudePrompt(units []*store.UnitForRanking, prefs *store.Preferences) string {
	var sb strings.Builder
	sb.WriteString("USER PREFERENCES:\n")
	if prefs == nil {
		sb.WriteString("- (no preferences saved yet)\n")
	} else {
		if prefs.WorkAddress != nil {
			fmt.Fprintf(&sb, "- Work address: %s\n", *prefs.WorkAddress)
		}
		if prefs.BudgetMinCents != nil {
			fmt.Fprintf(&sb, "- Budget min: $%d\n", *prefs.BudgetMinCents/100)
		}
		if prefs.BudgetMaxCents != nil {
			fmt.Fprintf(&sb, "- Budget max: $%d\n", *prefs.BudgetMaxCents/100)
		}
		if prefs.MinBeds != nil {
			fmt.Fprintf(&sb, "- Minimum bedrooms: %d\n", *prefs.MinBeds)
		}
		if prefs.MinBaths != nil {
			fmt.Fprintf(&sb, "- Minimum bathrooms: %.1f\n", *prefs.MinBaths)
		}
		if prefs.MinSqft != nil {
			fmt.Fprintf(&sb, "- Minimum sqft: %d\n", *prefs.MinSqft)
		}
	}
	sb.WriteString("\nCANDIDATE APARTMENTS:\n")
	for _, u := range units {
		fmt.Fprintf(&sb, "\n--- unit_id: %s\n", u.UnitID.String())
		fmt.Fprintf(&sb, "  Address: %s\n", u.Address)
		fmt.Fprintf(&sb, "  Listing kind: %s\n", u.Kind)
		fmt.Fprintf(&sb, "  Type: %s\n", u.UnitType)
		if u.UnitLabel != nil {
			fmt.Fprintf(&sb, "  Label: %s\n", *u.UnitLabel)
		}
		fmt.Fprintf(&sb, "  User-set status: %s\n", u.Status)
		if u.PriceCents != nil {
			suffix := ""
			if u.Kind == "rental" {
				suffix = "/mo"
			}
			fmt.Fprintf(&sb, "  Price: $%d%s\n", *u.PriceCents/100, suffix)
		}
		if u.Sqft != nil {
			fmt.Fprintf(&sb, "  Sqft: %d\n", *u.Sqft)
		}
		if u.Beds != nil {
			fmt.Fprintf(&sb, "  Beds: %d\n", *u.Beds)
		}
		if u.Baths != nil {
			fmt.Fprintf(&sb, "  Baths: %.1f\n", *u.Baths)
		}
		if u.PriceCents != nil && u.Sqft != nil && *u.Sqft > 0 {
			ppsqft := float64(*u.PriceCents) / 100.0 / float64(*u.Sqft)
			fmt.Fprintf(&sb, "  $/sqft: %.2f\n", ppsqft)
		}
	}
	sb.WriteString("\nReturn the JSON now.")
	return sb.String()
}

// extractJSON strips markdown code fences if Claude wrapped the JSON in
// one. We told it not to, but models are fallible — be lenient.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	return s
}
