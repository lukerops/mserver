package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// GetItem returns ErrNotFound if the item is not known.
func (s *Store) GetItem(ctx context.Context, id uuid.UUID) (*Item, error) {
	var it Item
	err := s.db.QueryRowContext(ctx, selectItem, id).
		Scan(&it.ID, &it.Type, &it.FileID, &it.LocationID, &it.Path, &it.Dir)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: item %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get item %s: %w", id, err)
	}
	return &it, nil
}

// SaveItem inserts or replaces an item.
func (s *Store) SaveItem(ctx context.Context, it *Item) error {
	if _, err := s.db.ExecContext(ctx, upsertItem, it.ID, it.Type, it.FileID, it.LocationID, it.Path, it.Dir); err != nil {
		return fmt.Errorf("store: save item %s: %w", it.ID, err)
	}
	return nil
}

// DeleteItem removes an item; removing an unknown item is not an error.
func (s *Store) DeleteItem(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.ExecContext(ctx, deleteItem, id); err != nil {
		return fmt.Errorf("store: delete item %s: %w", id, err)
	}
	return nil
}

// ListItems returns the items of a directory of a location, ordered by
// path.
func (s *Store) ListItems(ctx context.Context, location uuid.UUID, dir string) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, selectItems, location, dir)
	if err != nil {
		return nil, fmt.Errorf("store: list items: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Type, &it.FileID, &it.LocationID, &it.Path, &it.Dir); err != nil {
			return nil, fmt.Errorf("store: list items: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
