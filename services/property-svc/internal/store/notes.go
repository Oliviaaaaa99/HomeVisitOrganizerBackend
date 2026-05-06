package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Note mirrors a row in the notes table.
type Note struct {
	ID         uuid.UUID
	PropertyID uuid.UUID
	Body       string
	CreatedAt  time.Time
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
		RETURNING id, property_id, body, created_at`
	row := n.pool.QueryRow(ctx, q, propertyID, body)
	var note Note
	if err := row.Scan(&note.ID, &note.PropertyID, &note.Body, &note.CreatedAt); err != nil {
		return nil, fmt.Errorf("create note: %w", err)
	}
	return &note, nil
}

// ListByProperty returns notes of a property, newest first.
func (n *Notes) ListByProperty(ctx context.Context, propertyID uuid.UUID) ([]*Note, error) {
	const q = `SELECT id, property_id, body, created_at FROM notes WHERE property_id = $1 ORDER BY created_at DESC`
	rows, err := n.pool.Query(ctx, q, propertyID)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()
	var out []*Note
	for rows.Next() {
		var note Note
		if err := rows.Scan(&note.ID, &note.PropertyID, &note.Body, &note.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &note)
	}
	return out, rows.Err()
}
