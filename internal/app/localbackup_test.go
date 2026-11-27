package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lvbibir/opswright/internal/backup"
	"github.com/lvbibir/opswright/internal/backup/local"
	"github.com/lvbibir/opswright/internal/settings"
	"github.com/lvbibir/opswright/internal/vault"
)

// localObjects are the backups in dir.
func localObjects(t *testing.T, dir string) []backup.Object {
	t.Helper()
	objs, err := (&local.Folder{Dir: dir}).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return objs
}

// TestLocalBackupWrites: a manual local backup lands in the folder as a
// sealed file the master password opens, as an export is.
func TestLocalBackupWrites(t *testing.T) {
	a, tt, _ := backupApp(t)
	fillStore(t, a)
	dir := t.TempDir()
	a.settings.LocalBackup.Dir = dir
	a.runLocalBackup(true)
	waitForIn(t, tt, slowWait, "local backup", func() bool { return !a.localBusy })
	objs := localObjects(t, dir)
	if len(objs) != 1 {
		t.Fatalf("wrote %d backups", len(objs))
	}
	data, err := os.ReadFile(filepath.Join(dir, objs[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"hunter2", "sk-test"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("%q readable in the local backup", secret)
		}
	}
	plain, err := vault.Open(data, []byte(testBackupPassword))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	p, err := unpackBackup(plain)
	if err != nil || len(p.Assets.Hosts) != 1 || len(p.Secrets) == 0 {
		t.Fatalf("payload %+v, %v", p, err)
	}
}

// TestLocalBackupRetention: after a backup only this device's newest keep
// stay; another device's backups are never pruned.
func TestLocalBackupRetention(t *testing.T) {
	dir := t.TempDir()
	r := &local.Folder{Dir: dir}
	ctx := context.Background()
	base := time.Now().Truncate(time.Second)
	for i := 0; i < 4; i++ {
		name := backup.Name(base.Add(time.Duration(i)*time.Hour), "dev1")
		if err := putBackup(ctx, r, name, []byte("x"), "dev1", 2); err != nil {
			t.Fatal(err)
		}
	}
	if objs := localObjects(t, dir); len(objs) != 2 {
		t.Fatalf("kept %d backups", len(objs))
	}
	if err := r.Put(ctx, backup.Name(base.Add(-time.Hour), "dev2"), []byte("y")); err != nil {
		t.Fatal(err)
	}
	if err := putBackup(ctx, r, backup.Name(base.Add(10*time.Hour), "dev1"), []byte("x"), "dev1", 1); err != nil {
		t.Fatal(err)
	}
	objs := localObjects(t, dir)
	if len(objs) != 2 {
		t.Fatalf("after other device: %d backups", len(objs))
	}
	for _, o := range objs {
		if o.Device == "dev1" && !o.Time.Equal(base.Add(10*time.Hour)) {
			t.Fatalf("wrong dev1 backup kept: %v", o.Time)
		}
	}
}

// TestLocalBackupDue: a scheduled backup is due when there is none of this
// device's, or the newest is older than the interval.
func TestLocalBackupDue(t *testing.T) {
	a, _, _ := backupApp(t)
	dir := t.TempDir()
	a.settings.LocalBackup.Dir = dir
	a.settings.LocalBackup.Interval = settings.LocalDaily
	now := time.Now().Truncate(time.Second)
	if !a.localBackupDue(now) {
		t.Fatal("no backup yet should be due")
	}
	if err := (&local.Folder{Dir: dir}).Put(context.Background(), backup.Name(now.Add(-time.Hour), a.deviceID()), []byte("x")); err != nil {
		t.Fatal(err)
	}
	if a.localBackupDue(now) {
		t.Fatal("an hour-old backup is not due daily")
	}
	if !a.localBackupDue(now.Add(25 * time.Hour)) {
		t.Fatal("a day-old backup is due")
	}
}

// TestAutoLocalBackup: the scheduled backup runs when due and on, and does
// nothing just after one or while off.
func TestAutoLocalBackup(t *testing.T) {
	a, tt, _ := backupApp(t)
	fillStore(t, a)
	dir := t.TempDir()
	now := time.Now()
	a.settings.LocalBackup.Dir = dir // on, daily, by default
	a.autoLocalBackup(now)
	waitForIn(t, tt, slowWait, "scheduled backup", func() bool { return !a.localBusy })
	if n := len(localObjects(t, dir)); n != 1 {
		t.Fatalf("scheduled %d backups", n)
	}
	if a.localBackupDue(now) {
		t.Fatal("still due just after a backup")
	}
	a.autoLocalBackup(now)
	if a.localBusy {
		t.Fatal("a second backup started before the interval")
	}
	a.settings.LocalBackup.Enabled = false
	a.autoLocalBackup(now.Add(48 * time.Hour))
	if a.localBusy {
		t.Fatal("backed up while off")
	}
}
