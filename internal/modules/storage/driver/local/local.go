// Package local implements the storage driver for directories on the local
// filesystem. Walks are confined to the location directory with os.Root,
// so symlinks cannot escape it.
package local

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/lukerops/mserver/internal/modules/storage/api"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
)

const (
	// Name is the driver name used in location configuration.
	Name = "local"
	// OptionDir is the absolute directory of the location.
	OptionDir = "dir"
)

// Driver accesses a directory on the local filesystem.
type Driver struct {
	dir string
}

var _ driver.Driver = (*Driver)(nil)

// New creates a local driver. The directory is only accessed when used, so
// a location whose disk is not mounted can still be loaded.
func New(options map[string]string) (driver.Driver, error) {
	for key := range options {
		if key != OptionDir {
			return nil, fmt.Errorf("%w: local: unknown option %q", api.ErrInvalidArgument, key)
		}
	}

	dir := options[OptionDir]
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("%w: local: option %q must be an absolute path", api.ErrInvalidArgument, OptionDir)
	}
	return &Driver{dir: filepath.Clean(dir)}, nil
}

func (d *Driver) Root() string { return Name + ":" + filepath.ToSlash(d.dir) }

func (d *Driver) LocalPath(p string) string {
	return filepath.Join(d.dir, filepath.FromSlash(p))
}

// open opens the directory on every operation, so a remounted disk is
// picked up without any bookkeeping.
func (d *Driver) open() (*os.Root, error) {
	root, err := os.OpenRoot(d.dir)
	if err != nil {
		return nil, fmt.Errorf("local: %w", err)
	}
	return root, nil
}

func (d *Driver) Check(context.Context) error {
	root, err := d.open()
	if err != nil {
		return err
	}
	return root.Close()
}

func (d *Driver) Walk(ctx context.Context, fn func(driver.FileInfo) error) error {
	root, err := d.open()
	if err != nil {
		return err
	}
	defer root.Close()

	err = fs.WalkDir(root.FS(), ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		// Stat through the root follows symlinks only while they stay
		// inside it; broken or escaping symlinks are ignored.
		info, err := root.Stat(filepath.FromSlash(p))
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		return fn(driver.FileInfo{
			Path:    p,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Inode:   inodeOf(info),
		})
	})
	if err != nil {
		return fmt.Errorf("local: walk %s: %w", d.dir, err)
	}
	return nil
}
