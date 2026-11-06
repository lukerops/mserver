package sqlite

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/lukerops/mserver/internal/modules/storage/api"
)

const fileColumns = `id, location_id, path, size, mod_time, inode`

func scanFile(row rowScanner) (api.File, error) {
	var (
		f                   api.File
		id, locationID      string
		modTimeNanos, inode int64
	)
	if err := row.Scan(&id, &locationID, &f.Path, &f.Size, &modTimeNanos, &inode); err != nil {
		return api.File{}, err
	}
	var err error
	if f.ID, err = api.ParseFileID(id); err != nil {
		return api.File{}, err
	}
	if f.LocationID, err = api.ParseLocationID(locationID); err != nil {
		return api.File{}, err
	}
	f.ModTime = time.Unix(0, modTimeNanos).UTC()
	f.Inode = uint64(inode)
	return f, nil
}

func (s *Store) GetFile(ctx context.Context, id api.FileID) (api.File, error) {
	row := s.conn(ctx).QueryRowContext(ctx, `SELECT `+fileColumns+` FROM files WHERE id = ?`, id.String())
	f, err := scanFile(row)
	if err != nil {
		return api.File{}, notFound(err, "file "+id.String())
	}
	return f, nil
}

func (s *Store) ListFiles(ctx context.Context, loc api.LocationID) ([]api.File, error) {
	return s.queryFiles(ctx,
		`SELECT `+fileColumns+` FROM files WHERE location_id = ? ORDER BY path`, loc.String())
}

func (s *Store) Siblings(ctx context.Context, f api.File) ([]api.Sibling, error) {
	files, err := s.queryFiles(ctx,
		`SELECT `+fileColumns+` FROM files WHERE location_id = ? AND dir = ? AND id <> ? ORDER BY path`,
		f.LocationID.String(), path.Dir(f.Path), f.ID.String(),
	)
	if err != nil {
		return nil, err
	}
	siblings := make([]api.Sibling, len(files))
	for i, sib := range files {
		siblings[i] = api.Sibling{FileID: sib.ID, Path: sib.Path}
	}
	return siblings, nil
}

func (s *Store) queryFiles(ctx context.Context, query string, args ...any) ([]api.File, error) {
	rows, err := s.conn(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query files: %w", err)
	}
	defer rows.Close()

	var files []api.File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, fmt.Errorf("store: query files: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}
