// Package securefs contains the filesystem trust-boundary primitives used by Kothar.
package securefs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/safeopen"
)

const MaxFile = 1 << 20

func cleanRelative(name string) error {
	if name == "" || filepath.IsAbs(name) || filepath.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe relative path")
	}
	return nil
}

// CheckPath rejects symlinks in every existing component and verifies the final type.
func CheckPath(base, name string, wantDir bool) (fs.FileInfo, error) {
	if err := cleanRelative(name); err != nil {
		return nil, err
	}
	cur := base
	for _, part := range strings.Split(name, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return nil, err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink prohibited: %s", name)
		}
		if cur != filepath.Join(base, name) && !fi.IsDir() {
			return nil, fmt.Errorf("non-directory path component: %s", name)
		}
	}
	fi, err := os.Lstat(cur)
	if err != nil {
		return nil, err
	}
	if wantDir != fi.IsDir() || (!wantDir && !fi.Mode().IsRegular()) {
		return nil, fmt.Errorf("unexpected file type: %s", name)
	}
	return fi, nil
}

func ReadFile(base, name string, max int64) ([]byte, fs.FileInfo, error) {
	fi, err := CheckPath(base, name, false)
	if err != nil {
		return nil, nil, err
	}
	if fi.Size() > max {
		return nil, nil, fmt.Errorf("file exceeds %d bytes", max)
	}
	f, err := safeopen.OpenBeneath(base, name)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > max {
		return nil, nil, fmt.Errorf("file exceeds %d bytes", max)
	}
	return data, fi, nil
}

// EnsureDir creates a directory tree one component at a time without following symlinks.
func EnsureDir(path string, mode fs.FileMode) error {
	path = filepath.Clean(path)
	cur := string(filepath.Separator)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("directory must be absolute")
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			if err = os.Mkdir(cur, mode); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe directory component: %s", cur)
		}
	}
	return nil
}

// AtomicWrite replaces a regular file without following the destination or directory symlinks.
func AtomicWrite(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := EnsureDir(dir, 0700); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil && (!fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("unsafe destination")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(dir, ".atomic-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
