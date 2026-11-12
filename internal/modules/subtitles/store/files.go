package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// GetSubtitleFile returns ErrNotFound if the file is not known.
func (s *Store) GetSubtitleFile(ctx context.Context, id uuid.UUID) (*SubtitleFile, error) {
	var f SubtitleFile
	err := s.db.QueryRowContext(ctx, selectSubtitleFile, id).
		Scan(&f.FileID, &f.LocationID, &f.Path, &f.Dir, &f.Format)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: subtitle file %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get subtitle file %s: %w", id, err)
	}
	return &f, nil
}

// SaveSubtitleFile inserts or replaces a subtitle file.
func (s *Store) SaveSubtitleFile(ctx context.Context, f *SubtitleFile) error {
	if _, err := s.db.ExecContext(ctx, upsertSubtitleFile, f.FileID, f.LocationID, f.Path, f.Dir, f.Format); err != nil {
		return fmt.Errorf("store: save subtitle file %s: %w", f.FileID, err)
	}
	return nil
}

// DeleteSubtitleFile removes a subtitle file; removing an unknown file is
// not an error.
func (s *Store) DeleteSubtitleFile(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.ExecContext(ctx, deleteSubtitleFile, id); err != nil {
		return fmt.Errorf("store: delete subtitle file %s: %w", id, err)
	}
	return nil
}

// ListSubtitleFiles returns the subtitle files of a directory of a
// location, ordered by path.
func (s *Store) ListSubtitleFiles(ctx context.Context, location uuid.UUID, dir string) ([]SubtitleFile, error) {
	rows, err := s.db.QueryContext(ctx, selectSubtitleFiles, location, dir)
	if err != nil {
		return nil, fmt.Errorf("store: list subtitle files: %w", err)
	}
	defer rows.Close()

	var files []SubtitleFile
	for rows.Next() {
		var f SubtitleFile
		if err := rows.Scan(&f.FileID, &f.LocationID, &f.Path, &f.Dir, &f.Format); err != nil {
			return nil, fmt.Errorf("store: list subtitle files: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}
