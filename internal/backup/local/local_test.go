package local

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lvbibir/opswright/internal/backup"
)

func TestFolder(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	f := &Folder{Dir: filepath.Join(dir, "sub")} // not made yet
	if err := f.Check(ctx); err != nil {
		t.Fatalf("check: %v", err)
	}
	name := backup.Name(time.Now(), "dev1")
	data := bytes.Repeat([]byte("k"), 10)
	if err := f.Put(ctx, name, data); err != nil {
		t.Fatalf("put: %v", err)
	}
	// A file that is not a backup's is not listed.
	if err := os.WriteFile(filepath.Join(f.Dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	objs, err := f.List(ctx)
	if err != nil || len(objs) != 1 || objs[0].Name != name || objs[0].Device != "dev1" {
		t.Fatalf("list %+v, %v", objs, err)
	}
	got, err := f.Get(ctx, name)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("get %q, %v", got, err)
	}
	// A name that is not a backup's is not found, and cannot leave the
	// folder.
	for _, bad := range []string{"../secrets", "notes.txt", name + "/x"} {
		if _, err := f.Get(ctx, bad); !errors.Is(err, backup.ErrNotFound) {
			t.Fatalf("get %q: %v", bad, err)
		}
	}
	if err := f.Delete(ctx, name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := f.Get(ctx, name); !errors.Is(err, backup.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if err := f.Delete(ctx, name); !errors.Is(err, backup.ErrNotFound) {
		t.Fatalf("double delete: %v", err)
	}
}

func TestFolderMissing(t *testing.T) {
	ctx := context.Background()
	f := &Folder{Dir: filepath.Join(t.TempDir(), "gone")}
	objs, err := f.List(ctx)
	if err != nil || len(objs) != 0 {
		t.Fatalf("list missing: %+v, %v", objs, err)
	}
	if _, err := f.Get(ctx, backup.Name(time.Now(), "dev1")); !errors.Is(err, backup.ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}
	if err := (&Folder{}).Check(ctx); err == nil {
		t.Fatal("no folder: check passed")
	}
}
