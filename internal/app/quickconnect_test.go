package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Licoy/sshyard/internal/layout"
	"github.com/Licoy/sshyard/internal/session"
	"github.com/Licoy/sshyard/locales"
	"github.com/egoist/mygo/ui"
)

func TestQuickConnect(t *testing.T) {
	srv := startServer(t)
	a, tt := unlockedTestApp(t)
	m := locales.In("en")
	before, err := os.ReadFile(filepath.Join(a.p.DataDir, "secrets.enc"))
	if err != nil {
		t.Fatal(err)
	}
	a.known.Add(srv.Addr, srv.HostKey.PublicKey())

	// ⌘K (Ctrl+Shift+K) opens it, and so does the + menu.
	tt.Key(appMods, ui.KeyK)
	tt.Frame()
	if a.quick == nil {
		t.Fatal("shortcut did not open quick connect")
	}
	a.quick, a.quicking = nil, false
	tt.Frame()
	mustClick(t, tt, "Add")
	mustClick(t, tt, "Quick Connect…")
	tt.Frame()
	if a.quick == nil {
		t.Fatal("menu did not open quick connect")
	}
	// Errors stay in the sheet.
	a.quick.target = "ssh -i key demo@h"
	if a.QuickConnect(m) || a.quick.err != "-i is not supported here." {
		t.Errorf("option: %q", a.quick.err)
	}
	a.quick.target = "demo@" + srv.Addr
	a.quick.auth = a.quickChoices(m)[0].label
	a.quick.password = "secret"
	if !a.QuickConnect(m) {
		t.Fatal(a.quick.err)
	}
	c := a.focused()
	waitFor(t, tt, "connected", func() bool { return c != nil && c.s.Status().State == session.Connected })
	tt.Frame()
	if !tt.HasText("Temporary") || a.store.Len() != 0 || len(a.temps) != 1 {
		t.Fatalf("temp tab: %d hosts, %d temps, %q", a.store.Len(), len(a.temps), tt.Texts())
	}
	a.SplitFocused(layout.Right)
	second := a.focused()
	waitFor(t, tt, "split connected", func() bool { return second.s.Status().State == session.Connected })
	a.CloseSession(c.id)
	tt.Frame()
	if len(a.temps) != 1 {
		t.Fatal("temp dropped while a pane still used it")
	}
	a.CloseSession(second.id)
	tt.Frame()
	after, err := os.ReadFile(filepath.Join(a.p.DataDir, "secrets.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.temps) != 0 || len(a.typed) != 0 || a.store.Len() != 0 || string(after) != string(before) {
		t.Fatalf("left behind: %d temps, %d typed, %d hosts, encrypted store changed %v", len(a.temps), len(a.typed), a.store.Len(), string(after) != string(before))
	}

	// Save as Host turns a temporary tab into a saved host.
	a.OpenQuickConnect()
	a.quick.target = "ssh -p " + strings.Split(srv.Addr, ":")[1] + " demo@127.0.0.1"
	a.quick.password = "secret"
	if !a.QuickConnect(m) {
		t.Fatal(a.quick.err)
	}
	c = a.focused()
	waitFor(t, tt, "connected again", func() bool { return c.s.Status().State == session.Connected })
	a.SaveTempAs(c.id)
	tt.Frame()
	if !tt.HasText("Save as Host") {
		t.Fatalf("%q", tt.Texts())
	}
	a.form.name = "kept"
	if !a.SaveForm(m) {
		t.Fatal(a.form.err)
	}
	if a.store.Len() != 1 || len(a.temps) != 0 || isTemp(c.host.ID) {
		t.Fatalf("promote: %d hosts, %d temps, pane on %s", a.store.Len(), len(a.temps), c.host.ID)
	}
	tt.Frame()
	if tt.HasText("Temporary") {
		t.Error("badge still shown")
	}
}
