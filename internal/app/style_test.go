package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/sshyard/internal/settings"
	"github.com/lvbibir/sshyard/locales"
)

func TestStickySheets(t *testing.T) {
	a, _ := newTestApp(t)
	m := locales.In("en")
	for _, s := range []struct {
		name  string
		open  func()
		view  func(*ui.Context, locales.Messages)
		shown *bool
	}{
		{"backup", a.openExport, a.backupView, &a.backingUp},
		{"master password", a.openMasterPassword, a.masterPasswordView, &a.masterPwOpen},
		{"backup location", func() { a.openTargetForm(nil) }, a.targetView, &a.targetEditing},
		{"remote backups", func() {
			a.remotes, a.listingRemote = &remoteList{target: settings.BackupTarget{ID: "nas", Name: "NAS", Kind: settings.BackupWebDAV}}, true
		}, a.remoteListView, &a.listingRemote},
	} {
		s.open()
		tt := ui.NewTester(func(c *ui.Context) { s.view(c, m) }, 900, 700)
		// The first field takes typing once the sheet settles.
		for range 3 {
			tt.Frame()
		}
		tt.ClickAt(8, 8)
		tt.Frame()
		if !*s.shown {
			t.Fatalf("%s closed on a click outside it", s.name)
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		if *s.shown {
			t.Fatalf("%s stays open on Escape", s.name)
		}
	}
	if a.backup != nil || a.masterPw != nil || a.target != nil || a.remotes != nil {
		t.Fatal("a sheet closed on Escape kept its form")
	}
}
