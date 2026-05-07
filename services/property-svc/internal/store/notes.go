package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Note mirrors a row in the notes table.
type Note struct {
	ID         uuid.UUID
	PropertyID uuid.UUID
	Body       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Notes is the data-access layer for the notes table.
type Notes struct {
	pool *pgxpool.Pool
}

// NewNotes wraps a pgxpool.
func NewNotes(pool *pgxpool.Pool) *Notes {
	return &Notes{pool: pool}
}

// Create inserts a note attached to a property.
func (n *Notes) Create(ctx context.Context, propertyID uuid.UUID, body string) (*Note, error) {
	const q = `
		INSERT INTO notes (property_id, body)
		VALUES ($1, $2)
		RETURNING id, property_id, body, created_at, updated_at`
	row := n.pool.QueryRow(ctx, q, propertyID, body)
	var note Note
	if err := row.Scan(&note.ID, &note.PropertyID, &note.Body, &note.CreatedAt, &note.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create note: %w", err)
	}
	return &note, nil
}

// Update overwrites the note body. Returns nil if the user doesn't own the
// parent property.
func (n *Notes) Update(ctx context.Context, id, userID uuid.UUID, body string) (*Note, error) {
	const q = `
		UPDATE notes SET body = $3, updated_at = now()
		WHERE id = $1
		  AND property_id IN (SELECT id FROM properties WHERE user_id = $2)
		RETURNING id, property_id, body, created_at, updated_at`
	row := n.pool.QueryRow(ctx, q, id, userID, body)
	var note Note
	if err := row.Scan(&note.ID, &note.PropertyID, &note.Body, &note.CreatedAt, &note.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update note: %w", err)
	}
	return &note, nil
}

// Delete removes a note. Returns true if a row matched (and thus belonged to the user).
func (n *Notes) Delete(ctx context.Context, id, userID uuid.UUID) (bool, error) {
	const q = `
		DELETE FROM notes
		WHERE id = $1
		  AND property_id IN (SELECT id FROM properties WHERE user_id = $2)`
	tag, err := n.pool.Exec(ctx, q, id, userID)
	if err != nil {
		return false, fmt.Errorf("delete note: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListByProperty returns notes of a property, newest first.
func (n *Notes) ListByProperty(ctx context.Context, propertyID uuid.UUID) ([]*Note, error) {
	const q = `SELECT id, property_id, body, created_at, updated_at FROM notes WHERE property_id = $1 ORDER BY created_at DESC`
	rows, err := n.pool.Query(ctx, q, propertyID)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()
	var out []*Note
	for rows.Next() {
		var note Note
		if err := rows.Scan(&note.ID, &note.PropertyID, &note.Body, &note.CreatedAt, &note.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &note)
	}
	return out, rows.Err()
}
