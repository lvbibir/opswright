// Package local keeps backups in a folder on this computer. A backup is
// a sealed file here as in any other place: Put writes what it is given,
// List knows only the files named as backups, and nothing else in the
// folder is touched.
package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/lvbibir/opswright/internal/backup"
)

// Folder is a folder on this computer that keeps backups.
type Folder struct {
	// Dir is the folder; it is made when a backup is first written.
	Dir string
}

var _ backup.Remote = (*Folder)(nil)

// path is the file of a backup name in the folder; a name that is not a
// backup's, or that would leave the folder, is ErrNotFound.
func (f *Folder) path(name string) (string, error) {
	if _, _, ok := backup.Parse(name); !ok || strings.ContainsAny(name, `/\`) || f.Dir == "" {
		return "", backup.ErrNotFound
	}
	return filepath.Join(f.Dir, name), nil
}

// Check makes the folder and writes a probe file in it, so that a folder
// that cannot be written is told before a backup is attempted.
func (f *Folder) Check(ctx context.Context) error {
	if f.Dir == "" {
		return errors.New("backup: no folder")
	}
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	fd, err := os.CreateTemp(f.Dir, ".probe-*")
	if err != nil {
		return err
	}
	name := fd.Name()
	fd.Close()
	return os.Remove(name)
}

// List is the backups in the folder, as ObjectOf reads their names; any
// other file is ignored.
func (f *Folder) List(ctx context.Context) ([]backup.Object, error) {
	ents, err := os.ReadDir(f.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var objs []backup.Object
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if o, ok := backup.ObjectOf(e.Name(), info.Size()); ok {
			objs = append(objs, o)
		}
	}
	return objs, nil
}

// Get reads one backup, up to MaxSize.
func (f *Folder) Get(ctx context.Context, name string) ([]byte, error) {
	p, err := f.path(name)
	if err != nil {
		return nil, err
	}
	fd, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, backup.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	return backup.ReadAll(fd)
}

// Put writes a new backup, making the folder when needed, atomically.
func (f *Folder) Put(ctx context.Context, name string, data []byte) error {
	p, err := f.path(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(f.Dir, ".tmp-*")
	if err != nil {
		return err
	}
	t := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(t)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(t)
		return err
	}
	if err := os.Chmod(t, 0o600); err != nil {
		os.Remove(t)
		return err
	}
	return os.Rename(t, p)
}

// Delete removes one backup.
func (f *Folder) Delete(ctx context.Context, name string) error {
	p, err := f.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); errors.Is(err, os.ErrNotExist) {
		return backup.ErrNotFound
	} else if err != nil {
		return err
	}
	return nil
}
