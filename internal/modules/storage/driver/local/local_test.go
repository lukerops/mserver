package local

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lukerops/mserver/internal/modules/storage/driver"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newDriver(t *testing.T, dir string) driver.Driver {
	t.Helper()
	d, err := New(map[string]string{OptionDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestNewValidatesOptions(t *testing.T) {
	for _, options := range []map[string]string{
		{},
		{OptionDir: "relative/dir"},
		{OptionDir: "/tmp", "other": "x"},
	} {
		if _, err := New(options); !errors.Is(err, driver.ErrInvalidOptions) {
			t.Errorf("New(%v): got %v, want ErrInvalidOptions", options, err)
		}
	}
}

func TestWalk(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(dir, "Show/Season 01/ep1.mkv"), "video")
	writeFile(t, filepath.Join(dir, "Show/Season 01/ep1.srt"), "subtitle")
	writeFile(t, filepath.Join(dir, "movie.mkv"), "movie")
	writeFile(t, filepath.Join(outside, "secret"), "secret")

	// A symlink inside the root is followed; one escaping it is ignored.
	if err := os.Symlink("movie.mkv", filepath.Join(dir, "link.mkv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}

	var paths []string
	err := newDriver(t, dir).Walk(t.Context(), func(info driver.FileInfo) error {
		paths = append(paths, info.Path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Show/Season 01/ep1.mkv", "Show/Season 01/ep1.srt", "link.mkv", "movie.mkv"}
	slices.Sort(paths)
	if !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestWalkFailsOnMissingRoot(t *testing.T) {
	d := newDriver(t, filepath.Join(t.TempDir(), "missing"))
	if err := d.Check(t.Context()); err == nil {
		t.Error("Check succeeded on a missing directory")
	}
	if err := d.Walk(t.Context(), func(driver.FileInfo) error { return nil }); err == nil {
		t.Error("Walk succeeded on a missing directory")
	}
}

func TestOpen(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(dir, "Show/ep1.mkv"), "video")
	writeFile(t, filepath.Join(outside, "secret"), "secret")
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	d := newDriver(t, dir)

	f, err := d.Open(t.Context(), "Show/ep1.mkv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Seek(1, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil || string(b) != "ideo" {
		t.Fatalf("read %q, %v", b, err)
	}
	if info, err := f.Stat(); err != nil || info.Size() != 5 {
		t.Fatalf("Stat = %v, %v", info, err)
	}

	for _, p := range []string{"missing.mkv", "Show", "escape", "../" + filepath.Base(outside) + "/secret"} {
		if f, err := d.Open(t.Context(), p); err == nil {
			f.Close()
			t.Errorf("Open(%q) succeeded", p)
		}
	}
	if _, err := d.Open(t.Context(), "Show"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open of a directory: got %v, want fs.ErrNotExist", err)
	}
	if _, err := d.Open(t.Context(), "missing.mkv"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open of a missing file: got %v, want fs.ErrNotExist", err)
	}
}
