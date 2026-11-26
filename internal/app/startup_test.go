package app

import (
	"strings"
	"testing"
	"time"

	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/session"
	"github.com/lvbibir/opswright/locales"
	"golang.org/x/crypto/ssh"
)

// startOnce is the command c's shell is to be given once, "" for none.
func startOnce(c *conn) string {
	if c.start == nil {
		return ""
	}
	c.start.mu.Lock()
	defer c.start.mu.Unlock()
	return c.start.once
}

// setStartOnce gives c's shell cmd once.
func setStartOnce(c *conn, cmd string) {
	if c.start == nil {
		c.start = &shellStart{}
	}
	c.start.mu.Lock()
	c.start.once = cmd
	c.start.mu.Unlock()
}

// dirQuietForTest outlasts the wait for a new shell's prompt.
const dirQuietForTest = 800 * time.Millisecond

// TestStartLine: the cd back, then the startup commands, each typed on
// its own line.
func TestStartLine(t *testing.T) {
	for _, tc := range []struct {
		cd      string
		startup []string
		want    string
	}{
		{want: ""},
		{startup: []string{"export A=1", "source env"}, want: "export A=1\rsource env"},
		{cd: "cd /srv", startup: []string{"ls"}, want: "cd /srv\rls"},
		{cd: "cd /srv", want: "cd /srv"},
	} {
		if got := startLine(tc.cd, tc.startup); got != tc.want {
			t.Errorf("startLine(%q, %q) = %q, want %q", tc.cd, tc.startup, got, tc.want)
		}
	}
}

// TestHostStartupTyped: a terminal types its host's startup commands in
// the shell; one opened without them types none.
func TestHostStartupTyped(t *testing.T) {
	srv := startMetricSrv(t, func(_ ssh.Channel, _ string) int { return 0 })
	a, tt := newTestApp(t)
	m := locales.In("en")
	h := addHost(t, a, srv, "box", "secret")
	a.known.Add(srv.Addr, srv.HostKey.PublicKey())
	if err := a.kc.Set(keychain.HostStartup(h.ID), []byte("export A=1\nsource env")); err != nil {
		t.Fatal(err)
	}
	h.Startup = true
	if _, err := a.Store().Update(h); err != nil {
		t.Fatal(err)
	}
	a.Connect(h.ID, openAt{})
	cn := a.focused()
	waitFor(t, tt, "the commands", func() bool {
		return strings.Contains(cn.s.Term.ScreenText(), "you said: source env")
	})
	screen := cn.s.Term.ScreenText()
	first, second := strings.Index(screen, "you said: export A=1"), strings.Index(screen, "you said: source env")
	if first < 0 || second < first {
		t.Fatalf("typed out of order: %q", screen)
	}

	a.ConnectWithoutStartup(h.ID)
	plain := a.focused()
	waitFor(t, tt, "connected", func() bool { return plain.s.Status().State == session.Connected })
	time.Sleep(dirQuietForTest)
	tt.Frame()
	if strings.Contains(plain.s.Term.ScreenText(), "export A=1") {
		t.Errorf("typed startup commands: %q", plain.s.Term.ScreenText())
	}
	var labels []string
	for _, e := range a.hostEntries(m, h) {
		labels = append(labels, e.Label)
	}
	if !strings.Contains(strings.Join(labels, "|"), m.ConnectWithoutStartup()) {
		t.Errorf("menu %q", labels)
	}

	// Commands gone from the store are typed no more, and told.
	if err := a.kc.Set(keychain.HostStartup(h.ID), []byte("bad\tline")); err != nil {
		t.Fatal(err)
	}
	a.Connect(h.ID, openAt{})
	bad := a.focused()
	waitFor(t, tt, "the warning", func() bool { return tt.HasText(m.StartupReadFailed("box")) })
	waitFor(t, tt, "connected", func() bool { return bad.s.Status().State == session.Connected })
	time.Sleep(dirQuietForTest)
	tt.Frame()
	if strings.Contains(bad.s.Term.ScreenText(), "you said: bad") {
		t.Errorf("typed invalid commands: %q", bad.s.Term.ScreenText())
	}
}

// TestHostStartupEditor: the editor keeps startup commands in the
// encrypted store only, writes them when changed, deletes them when
// cleared, and copies them to a clone; deleting the host deletes them.
func TestHostStartupEditor(t *testing.T) {
	a, _ := newTestApp(t)
	a.openEditor(nil)
	f := a.form
	f.name, f.address, f.user = "web", "10.0.0.1", "demo"
	f.startup = "echo started-here\n\n"
	if !a.SaveForm(en) {
		t.Fatal(f.err)
	}
	h := a.store.List()[0]
	key := keychain.HostStartup(h.ID)
	if v, err := a.kc.Get(key); err != nil || !h.Startup || string(v) != "echo started-here" {
		t.Fatalf("saved %v %q %v", h.Startup, v, err)
	}
	noSecrets(t, a.p.DataDir, "started-here")

	a.openEditor(&h)
	if a.form.startup != "echo started-here" {
		t.Fatalf("editor shows %q", a.form.startup)
	}
	if changes := startupChanges(a.form, h); changes != nil {
		t.Fatalf("unchanged commands rewritten: %v", changes)
	}
	a.form.startup = "echo\tbad"
	if a.SaveForm(en) || a.form.err != en.StartupInvalid() {
		t.Fatalf("invalid commands saved: %q", a.form.err)
	}

	a.openClone(h)
	a.form.name = "web-2"
	if !a.SaveForm(en) {
		t.Fatal(a.form.err)
	}
	var clone assets.Host
	for _, x := range a.store.List() {
		if x.Name == "web-2" {
			clone = x
		}
	}
	if v, err := a.kc.Get(keychain.HostStartup(clone.ID)); err != nil || !clone.Startup || string(v) != "echo started-here" {
		t.Fatalf("clone %v %q %v", clone.Startup, v, err)
	}

	a.openEditor(&h)
	a.form.startup = " \n"
	if !a.SaveForm(en) {
		t.Fatal(a.form.err)
	}
	h, _ = a.store.Get(h.ID)
	if h.Startup || a.kc.Has(key) {
		t.Fatal("cleared commands kept")
	}
	if err := a.DeleteHost(clone.ID); err != nil {
		t.Fatal(err)
	}
	if a.kc.Has(keychain.HostStartup(clone.ID)) {
		t.Fatal("deleted host kept its commands")
	}
}

// TestHostStartupBackup: a backup carries a host's startup commands, and
// a restore without them deletes those the host had here.
func TestHostStartupBackup(t *testing.T) {
	a, _ := newTestApp(t)
	h, err := a.store.Add(assets.Host{Name: "web", Address: "10.0.0.1", User: "demo", Auth: assets.AuthAgent, Startup: true})
	if err != nil {
		t.Fatal(err)
	}
	key := keychain.HostStartup(h.ID)
	if err := a.kc.Set(key, []byte("echo restored")); err != nil {
		t.Fatal(err)
	}
	p, err := a.collectBackup(true)
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Secrets[key]) != "echo restored" {
		t.Fatal("commands absent from backup")
	}
	b, _ := newTestApp(t)
	if err := b.restoreBackup(en, p); err != nil {
		t.Fatal(err)
	}
	if v, err := b.kc.Get(key); err != nil || string(v) != "echo restored" {
		t.Fatalf("restored %q %v", v, err)
	}
	p.Secrets[key] = []byte("echo\tbad")
	if err := b.restoreBackup(en, p); err == nil {
		t.Fatal("invalid commands restored")
	}
	delete(p.Secrets, key)
	if err := b.restoreBackup(en, p); err != nil {
		t.Fatal(err)
	}
	if b.kc.Has(key) {
		t.Fatal("restore without commands kept the old ones")
	}
}

// TestHostStartupNeedsUnlock: a host with startup commands asks for the
// store to be unlocked before it connects, as its login secrets would.
func TestHostStartupNeedsUnlock(t *testing.T) {
	a, tt, _ := newLockedTestApp(t)
	setMasterPassword(t, a, tt, "correct horse")
	a.LockApp()
	h := assets.Host{ID: "h1", Address: "localhost", Port: 22, User: "test", Auth: assets.AuthAgent}
	if a.unlocked() || a.connectionNeedsSecrets(h) {
		t.Fatal("an agent host asked for an unlock")
	}
	h.Startup = true
	if !a.connectionNeedsSecrets(h) {
		t.Fatal("startup commands connected locked")
	}
}
