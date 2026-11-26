package app

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

// TestElevateOptions: the terminals become root only when the host says
// so, the login user is not root already and a password is saved; the
// server's processes are read only where the connection may run commands
// of its own.
func TestElevateOptions(t *testing.T) {
	a, _ := newTestApp(t)
	h, err := a.store.Add(assets.Host{Name: "web", Address: "10.0.0.1", User: "demo", Auth: assets.AuthAsk, TerminalSu: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.elevateOptions(h); got != nil {
		t.Fatalf("no password saved, yet %+v", got)
	}
	if err := a.kc.Set(keychain.HostElevate(h.ID), []byte("rootpw")); err != nil {
		t.Fatal(err)
	}
	got := a.elevateOptions(h)
	if got == nil || got.Command != "su -" || got.Prompt != assets.DefaultSuPrompt || !got.Probe {
		t.Fatalf("options %+v", got)
	}
	if pass := got.Password(); string(pass) != "rootpw" {
		t.Fatalf("password %q", pass)
	}
	h.TerminalSu = false
	if a.elevateOptions(h) != nil {
		t.Error("a host not set to elevate did")
	}
	h.TerminalSu = true
	h.User = "root"
	if a.elevateOptions(h) != nil {
		t.Error("a host whose login user is root elevates")
	}
	h.User = "demo"

	// A routed gateway that serves the terminal and nothing else allows
	// no exec channel: the /proc probe would read the gateway, so it is
	// dropped and the prompt alone is waited for.
	cr, err := a.Store().SaveCredential(assets.Credential{Name: "bastion-cred", Kind: assets.CredPassword, User: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	gw, err := a.Store().SaveGateway(assets.Gateway{Name: "bastion", Address: "10.0.0.9", User: "demo", CredentialID: cr.ID})
	if err != nil {
		t.Fatal(err)
	}
	h.GatewayID = gw.ID
	if _, err := a.store.Update(h); err != nil {
		t.Fatal(err)
	}
	if got := a.elevateOptions(h); got == nil || got.Probe {
		t.Fatalf("a gateway without exec: %+v", got)
	}
	gw.DirectTCPIP = true
	if _, err := a.Store().SaveGateway(gw); err != nil {
		t.Fatal(err)
	}
	if got := a.elevateOptions(h); got == nil || !got.Probe {
		t.Fatalf("a forwarding gateway: %+v", got)
	}
}

// TestElevateTyped: su - is typed before the cd and the startup commands,
// its password is typed once and never echoed, and the terminal then runs
// as root.
func TestElevateTyped(t *testing.T) {
	log := &suShellLog{}
	srv := startMetricSrv(t, func(ssh.Channel, string) int { return 1 })
	srv.Shell = log.shell("rootpw")
	a, tt := newTestApp(t)
	h := addHost(t, a, srv, "box", "secret")
	a.known.Add(srv.Addr, srv.HostKey.PublicKey())
	if err := a.kc.Set(keychain.HostElevate(h.ID), []byte("rootpw")); err != nil {
		t.Fatal(err)
	}
	if err := a.kc.Set(keychain.HostStartup(h.ID), []byte("source env")); err != nil {
		t.Fatal(err)
	}
	h.TerminalSu, h.Startup, h.TerminalDir = true, true, "/srv"
	if _, err := a.Store().Update(h); err != nil {
		t.Fatal(err)
	}
	a.Connect(h.ID, openAt{})
	cn := a.focused()
	waitFor(t, tt, "root", func() bool { return strings.Contains(cn.s.Term.ScreenText(), "root#") })
	screen := cn.s.Term.ScreenText()
	if strings.Contains(screen, "rootpw") {
		t.Fatalf("the password is on screen: %q", screen)
	}
	if !strings.Contains(screen, "Password:") {
		t.Fatalf("the prompt was not asked: %q", screen)
	}
	want := []string{"password", "cd '/srv'", "source env"}
	waitFor(t, tt, "everything typed", func() bool { return len(log.lines()) >= len(want) })
	if got := log.lines(); !equal(got, want) {
		t.Fatalf("typed %q, want %q", got, want)
	}
	time.Sleep(dirQuietForTest)
	tt.Frame()
	if got := log.lines(); !equal(got, want) {
		t.Fatalf("typed again: %q", got)
	}
}

// TestElevateEditor: the editor keeps the su password in the encrypted
// store only, writes it when typed, keeps it when the field is left
// blank, and deletes it when the elevation is turned off. Deleting the
// host deletes it too.
func TestElevateEditor(t *testing.T) {
	a, _ := newTestApp(t)
	a.openEditor(nil)
	f := a.form
	f.name, f.address, f.user = "web", "10.0.0.1", "demo"
	f.terminalSu, f.suPass = true, "rootpw"
	if !a.SaveForm(en) {
		t.Fatal(f.err)
	}
	h := a.store.List()[0]
	key := keychain.HostElevate(h.ID)
	if v, err := a.kc.Get(key); err != nil || !h.TerminalSu || string(v) != "rootpw" {
		t.Fatalf("saved %v %q %v", h.TerminalSu, v, err)
	}
	noSecrets(t, a.p.DataDir, "rootpw")

	// A blank field keeps the saved password.
	a.openEditor(&h)
	if !a.form.suPassOrig || a.form.suPass != "" {
		t.Fatalf("editor %+v", a.form)
	}
	a.form.suPrompt = "Password:"
	if !a.SaveForm(en) {
		t.Fatal(a.form.err)
	}
	if v, err := a.kc.Get(key); err != nil || string(v) != "rootpw" {
		t.Fatalf("kept %q %v", v, err)
	}
	if h, _ = a.store.Get(h.ID); h.TerminalSuPrompt != "Password:" {
		t.Fatalf("prompt %q", h.TerminalSuPrompt)
	}

	// A prompt a terminal could not be waited on is refused.
	a.openEditor(&h)
	a.form.suPrompt = "bad\tprompt"
	if a.SaveForm(en) || a.form.err != en.TerminalSuInvalid() {
		t.Fatalf("invalid prompt saved: %q", a.form.err)
	}
	a.form.suPrompt = ""
	a.form.suPass = "a\nb"
	if a.SaveForm(en) || a.form.err != en.SuPasswordInvalid() {
		t.Fatalf("invalid password saved: %q", a.form.err)
	}

	// Turning the elevation off deletes the password.
	a.form.suPass = ""
	a.form.terminalSu = false
	if !a.SaveForm(en) {
		t.Fatal(a.form.err)
	}
	if h, _ = a.store.Get(h.ID); h.TerminalSu || a.kc.Has(key) {
		t.Fatal("turned off, the password stayed")
	}

	// A host deleted goes with its password.
	if err := a.kc.Set(key, []byte("rootpw")); err != nil {
		t.Fatal(err)
	}
	h, _ = a.store.Get(h.ID)
	h.TerminalSu = true
	if _, err := a.store.Update(h); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteHost(h.ID); err != nil {
		t.Fatal(err)
	}
	if a.kc.Has(key) {
		t.Fatal("deleted host kept its su password")
	}
}

// TestElevateBackup: a backup carries a host's su password, and a restore
// without it deletes the one the host had here.
func TestElevateBackup(t *testing.T) {
	a, _ := newTestApp(t)
	h, err := a.store.Add(assets.Host{Name: "web", Address: "10.0.0.1", User: "demo", Auth: assets.AuthAgent, TerminalSu: true})
	if err != nil {
		t.Fatal(err)
	}
	key := keychain.HostElevate(h.ID)
	if err := a.kc.Set(key, []byte("rootpw")); err != nil {
		t.Fatal(err)
	}
	p, err := a.collectBackup(true)
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Secrets[key]) != "rootpw" {
		t.Fatal("the su password is absent from the backup")
	}
	b, _ := newTestApp(t)
	if err := b.restoreBackup(en, p); err != nil {
		t.Fatal(err)
	}
	if v, err := b.kc.Get(key); err != nil || string(v) != "rootpw" {
		t.Fatalf("restored %q %v", v, err)
	}
	p.Secrets[key] = []byte("bad\npassword")
	if err := b.restoreBackup(en, p); err == nil {
		t.Fatal("a password that cannot be typed was restored")
	}
	delete(p.Secrets, key)
	if err := b.restoreBackup(en, p); err != nil {
		t.Fatal(err)
	}
	if b.kc.Has(key) {
		t.Fatal("a restore without the password kept the old one")
	}
}

// TestElevateNeedsUnlock: a host whose terminals become root asks for the
// store to be unlocked before it connects, as its login secrets would.
func TestElevateNeedsUnlock(t *testing.T) {
	a, tt, _ := newLockedTestApp(t)
	setMasterPassword(t, a, tt, "correct horse")
	a.LockApp()
	h := assets.Host{ID: "h1", Address: "localhost", Port: 22, User: "test", Auth: assets.AuthAgent}
	if a.unlocked() || a.connectionNeedsSecrets(h) {
		t.Fatal("an agent host asked for an unlock")
	}
	h.TerminalSu = true
	if !a.connectionNeedsSecrets(h) {
		t.Fatal("a host becoming root connected locked")
	}
}

// suShellLog is a shell that becomes root with su -, echoing what it is
// told so that a test can see what was typed and in what order. The
// password is never echoed, as a terminal with echo off does not.
type suShellLog struct {
	mu     sync.Mutex
	typed  []string
	passwd int
}

func (l *suShellLog) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.typed...)
}

func (l *suShellLog) add(s string) {
	l.mu.Lock()
	l.typed = append(l.typed, s)
	l.mu.Unlock()
}

func (l *suShellLog) shell(pw string) sshtest.Shell {
	return func(ch ssh.Channel, _ sshtest.PTY, _ <-chan sshtest.PTY) int {
		var line []byte
		buf := make([]byte, 256)
		waiting, root := false, false
		prompt := func() {
			if root {
				io.WriteString(ch, "root# ")
				return
			}
			io.WriteString(ch, "user$ ")
		}
		io.WriteString(ch, "Welcome\r\n")
		prompt()
		for {
			n, err := ch.Read(buf)
			if err != nil {
				return 0
			}
			for _, b := range buf[:n] {
				if b != '\r' && b != '\n' {
					line = append(line, b)
					continue
				}
				cmd := strings.TrimSpace(string(line))
				line = line[:0]
				io.WriteString(ch, "\r\n")
				switch {
				case waiting:
					waiting = false
					l.add("password")
					l.mu.Lock()
					l.passwd++
					l.mu.Unlock()
					if cmd != pw {
						io.WriteString(ch, "su: Authentication failure\r\n")
						prompt()
						continue
					}
					root = true
					prompt()
				case cmd == "su -":
					waiting = true
					io.WriteString(ch, "Password: ")
				case cmd != "":
					l.add(cmd)
					io.WriteString(ch, "you said: "+cmd+"\r\n")
					prompt()
				default:
					prompt()
				}
			}
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
