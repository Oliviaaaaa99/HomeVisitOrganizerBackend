// Package service holds the business-logic layer for media-svc.
package service

import (
	"context"
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/media-svc/internal/clients"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/media-svc/internal/store"
	"github.com/google/uuid"
)

// Per-unit quotas, from PRD §6.1.
const (
	MaxPhotos      = 10
	MaxShortVideos = 3
	MaxLongVideos  = 3
)

// Retention TTLs, from PRD §6.5.
var retentionByPropertyKind = map[string]time.Duration{
	"rental":   365 * 24 * time.Hour,     // 1 year
	"for_sale": 2 * 365 * 24 * time.Hour, // 2 years
}

// Media is the orchestrator for the media flow: presign upload URLs, commit
// rows after upload, soft-delete.
type Media struct {
	owner *store.Ownership
	media *store.Media
	s3    *clients.S3
}

// NewMedia wires the service.
func NewMedia(owner *store.Ownership, media *store.Media, s3 *clients.S3) *Media {
	return &Media{owner: owner, media: media, s3: s3}
}

// PresignItem is a single upload slot the client requested.
type PresignItem struct {
	MediaType string `json:"media_type"` // "photo" | "video_short" | "video_long"
}

// PresignedUpload is what we hand back to the client per slot.
type PresignedUpload struct {
	S3Key     string    `json:"s3_key"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	MediaType string    `json:"media_type"`
}

// Errors surfaced by Presign / Commit.
var (
	ErrUnitNotOwned     = errors.New("unit not owned")
	ErrQuotaExceeded    = errors.New("quota exceeded")
	ErrInvalidType      = errors.New("invalid media_type")
	ErrInvalidExtension = errors.New("invalid file extension")
)

// Presign returns one signed PUT URL per requested item, after enforcing
// quotas against the unit's existing media.
func (m *Media) Presign(ctx context.Context, unitID, userID uuid.UUID, items []PresignItem) ([]PresignedUpload, error) {
	if len(items) == 0 {
		return []PresignedUpload{}, nil
	}
	owner, err := m.owner.FindUnitForUser(ctx, unitID, userID)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrUnitNotOwned
	}

	requestedByType := map[string]int{}
	for _, it := range items {
		if !validMediaType(it.MediaType) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidType, it.MediaType)
		}
		requestedByType[it.MediaType]++
	}

	for kind, want := range requestedByType {
		existing, err := m.media.CountByType(ctx, unitID, kind)
		if err != nil {
			return nil, err
		}
		if existing+want > capFor(kind) {
			return nil, fmt.Errorf("%w: %s would exceed cap %d (have %d, want %d)",
				ErrQuotaExceeded, kind, capFor(kind), existing, want)
		}
	}

	ttl := m.s3.PresignTTL()
	exp := time.Now().UTC().Add(ttl)
	out := make([]PresignedUpload, 0, len(items))
	for _, it := range items {
		key := mediaKey(userID, unitID, it.MediaType)
		url, err := m.s3.PresignPut(ctx, key)
		if err != nil {
			return nil, err
		}
		out = append(out, PresignedUpload{
			S3Key:     key,
			URL:       url,
			ExpiresAt: exp,
			MediaType: it.MediaType,
		})
	}
	return out, nil
}

// CommitItem is one media item the client claims to have uploaded.
type CommitItem struct {
	S3Key     string   `json:"s3_key"`
	MediaType string   `json:"media_type"`
	DurationS *float64 `json:"duration_s,omitempty"`
	Caption   *string  `json:"caption,omitempty"`
}

// Committed is the persisted record we return for each successful commit.
type Committed struct {
	ID        string    `json:"id"`
	S3Key     string    `json:"s3_key"`
	MediaType string    `json:"media_type"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Commit verifies each S3 key exists, then inserts a media_assets row per item
// with the right retention TTL based on the parent property's kind.
func (m *Media) Commit(ctx context.Context, unitID, userID uuid.UUID, items []CommitItem) ([]Committed, error) {
	if len(items) == 0 {
		return []Committed{}, nil
	}
	owner, err := m.owner.FindUnitForUser(ctx, unitID, userID)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, ErrUnitNotOwned
	}
	ttl, ok := retentionByPropertyKind[owner.PropertyKind]
	if !ok {
		// Defensive: fall back to the shorter TTL.
		ttl = retentionByPropertyKind["rental"]
	}
	expiresAt := time.Now().UTC().Add(ttl)

	out := make([]Committed, 0, len(items))
	for _, it := range items {
		if !validMediaType(it.MediaType) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidType, it.MediaType)
		}
		// Verify the upload landed.
		if err := m.s3.HeadObject(ctx, it.S3Key); err != nil {
			return nil, fmt.Errorf("verify upload %q: %w", it.S3Key, err)
		}
		row, err := m.media.Insert(ctx, store.CommitInput{
			UnitID:    unitID,
			UserID:    userID,
			MediaType: it.MediaType,
			S3Key:     it.S3Key,
			DurationS: it.DurationS,
			Caption:   it.Caption,
			ExpiresAt: expiresAt,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, Committed{
			ID:        row.ID.String(),
			S3Key:     row.S3Key,
			MediaType: row.MediaType,
			ExpiresAt: row.ExpiresAt,
		})
	}
	return out, nil
}

// UpdatedMedia is the persisted row we return after a caption edit.
type UpdatedMedia struct {
	ID        string  `json:"id"`
	S3Key     string  `json:"s3_key"`
	MediaType string  `json:"media_type"`
	Caption   *string `json:"caption,omitempty"`
}

// UpdateCaption sets/clears the caption on a media row owned by userID. Empty
// string clears it. Returns nil if the row doesn't exist or isn't owned.
func (m *Media) UpdateCaption(ctx context.Context, mediaID, userID uuid.UUID, caption string) (*UpdatedMedia, error) {
	row, err := m.media.UpdateCaption(ctx, mediaID, userID, caption)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &UpdatedMedia{
		ID:        row.ID.String(),
		S3Key:     row.S3Key,
		MediaType: row.MediaType,
		Caption:   row.Caption,
	}, nil
}

// SoftDelete removes a media row (S3 object stays — retention sweeper purges later).
func (m *Media) SoftDelete(ctx context.Context, mediaID, userID uuid.UUID) (bool, error) {
	return m.media.SoftDelete(ctx, mediaID, userID)
}

func validMediaType(t string) bool {
	switch t {
	case "photo", "video_short", "video_long":
		return true
	}
	return false
}

func capFor(t string) int {
	switch t {
	case "photo":
		return MaxPhotos
	case "video_short":
		return MaxShortVideos
	case "video_long":
		return MaxLongVideos
	}
	return 0
}

// mediaKey builds an S3 key namespaced by (user, unit) so a single user-data
// purge can prefix-list and delete cleanly.
func mediaKey(userID, unitID uuid.UUID, mediaType string) string {
	id := uuid.New().String()
	return path.Join("media", userID.String(), unitID.String(), id+"."+extFor(mediaType))
}

func extFor(t string) string {
	switch t {
	case "photo":
		return "jpg"
	case "video_short", "video_long":
		return "mp4"
	}
	return "bin"
}
