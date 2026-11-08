package sqlite

import (
	"context"
	"fmt"
	"path"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/storage/store"
)

func (s *Store) ApplyScan(ctx context.Context, location uuid.UUID, c store.ScanChanges) error {
	return s.WithTx(ctx, func(ctx context.Context) error {
		conn := s.conn(ctx)
		for _, id := range c.Removed {
			if _, err := conn.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, id); err != nil {
				return fmt.Errorf("store: remove file %s: %w", id, err)
			}
		}
		for id, info := range c.Updated {
			if _, err := conn.ExecContext(ctx,
				`UPDATE files SET path = ?, dir = ?, size = ?, mod_time = ?, inode = ? WHERE id = ?`,
				info.Path, path.Dir(info.Path), info.Size, nanos(info.ModTime), int64(info.Inode), id,
			); err != nil {
				return fmt.Errorf("store: update %s: %w", info.Path, err)
			}
		}
		for id, info := range c.Added {
			if _, err := conn.ExecContext(ctx, `
				INSERT INTO files (id, location_id, path, dir, size, mod_time, inode)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				id, location, info.Path, path.Dir(info.Path),
				info.Size, nanos(info.ModTime), int64(info.Inode),
			); err != nil {
				return fmt.Errorf("store: add %s: %w", info.Path, err)
			}
		}
		return nil
	})
}
