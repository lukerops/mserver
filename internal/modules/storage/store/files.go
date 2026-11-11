package store

import (
	"context"
	"fmt"
	"path"

	"github.com/google/uuid"
)

// GetFile returns ErrNotFound if the file does not exist.
func (s *Store) GetFile(ctx context.Context, id uuid.UUID) (*File, error) {
	files, err := s.queryFiles(ctx, selectFile, id)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: file %s", ErrNotFound, id)
	}
	return &files[0], nil
}

// ListFiles returns the files of a location ordered by path. An empty page
// returns all of them.
func (s *Store) ListFiles(ctx context.Context, location uuid.UUID, page Page) ([]File, error) {
	args := []any{location}
	return s.queryFiles(ctx, paginate(selectFilesByPath, &args, page), args...)
}

// Siblings returns the other files in the same directory as f, ordered by
// path.
func (s *Store) Siblings(ctx context.Context, f *File) ([]Sibling, error) {
	rows, err := s.db.QueryContext(ctx, selectSiblings, f.LocationID, path.Dir(f.Path), f.ID)
	if err != nil {
		return nil, fmt.Errorf("store: siblings of %s: %w", f.ID, err)
	}
	defer rows.Close()

	var siblings []Sibling
	for rows.Next() {
		var sib Sibling
		if err := rows.Scan(&sib.FileID, &sib.Path); err != nil {
			return nil, fmt.Errorf("store: siblings of %s: %w", f.ID, err)
		}
		siblings = append(siblings, sib)
	}
	return siblings, rows.Err()
}

func (s *Store) queryFiles(ctx context.Context, query string, args ...any) ([]File, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query files: %w", err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var (
			f              File
			modTime, inode int64
		)
		if err := rows.Scan(&f.ID, &f.LocationID, &f.Path, &f.Size, &modTime, &inode); err != nil {
			return nil, fmt.Errorf("store: query files: %w", err)
		}
		f.ModTime = fromNanos(modTime)
		f.Inode = uint64(inode)
		files = append(files, f)
	}
	return files, rows.Err()
}
