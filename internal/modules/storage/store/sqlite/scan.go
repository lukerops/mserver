package sqlite

import (
	"context"
	"fmt"
	"path"

	"github.com/lukerops/mserver/internal/modules/storage/api"
	"github.com/lukerops/mserver/internal/modules/storage/store"
)

func (s *Store) ApplyScan(ctx context.Context, loc api.LocationID, c store.ScanChanges) error {
	return s.WithTx(ctx, func(ctx context.Context) error {
		conn := s.conn(ctx)
		for _, id := range c.Removed {
			if _, err := conn.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, id.String()); err != nil {
				return fmt.Errorf("store: remove file %s: %w", id, err)
			}
		}
		for id, info := range c.Updated {
			if _, err := conn.ExecContext(ctx,
				`UPDATE files SET path = ?, dir = ?, size = ?, mod_time = ?, inode = ? WHERE id = ?`,
				info.Path, path.Dir(info.Path), info.Size, info.ModTime.UnixNano(), int64(info.Inode), id.String(),
			); err != nil {
				return fmt.Errorf("store: update %s: %w", info.Path, err)
			}
		}
		for id, info := range c.Added {
			if _, err := conn.ExecContext(ctx, `
				INSERT INTO files (id, location_id, path, dir, size, mod_time, inode)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				id.String(), loc.String(), info.Path, path.Dir(info.Path),
				info.Size, info.ModTime.UnixNano(), int64(info.Inode),
			); err != nil {
				return fmt.Errorf("store: add %s: %w", info.Path, err)
			}
		}
		return nil
	})
}
