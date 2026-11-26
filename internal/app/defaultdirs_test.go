package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lvbibir/opswright/internal/sshtest"
)

func TestHostDefaultDirs(t *testing.T) {
	skipWindowsSFTP(t)
	srv, err := sshtest.Start("127.0.0.1:0", "demo", "secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.SFTPRoot = root
	os.Mkdir(filepath.Join(root, "www"), 0o755)
	a, tt := newTestApp(t)
	os.Mkdir(filepath.Join(a.localHome(), "proj"), 0o755)
	h := addHost(t, a, srv, "box", "secret")
	a.known.Add(srv.Addr, srv.HostKey.PublicKey())
	showLocal(a)

	// The host's default folders start its Files panes and its terminal.
	h.TerminalDir, h.RemoteDir, h.LocalDir = "~/www", "~/www", "~/proj"
	if _, err := a.Store().Update(h); err != nil {
		t.Fatal(err)
	}
	a.selected = h.ID
	tt.Frame()
	mustClick(t, tt, "Open Files")
	fc := a.focused()
	waitFor(t, tt, "files listed", func() bool { return fc.fs.remote.client != nil && fc.fs.local.loaded })
	if fc.fs.remote.dir != filepath.Join(root, "www") || fc.fs.local.dir != filepath.Join(a.localHome(), "proj") {
		t.Errorf("files start in %q and %q", fc.fs.remote.dir, fc.fs.local.dir)
	}
	a.Connect(h.ID, openAt{})
	tc := a.focused()
	waitFor(t, tt, "the cd", func() bool { return strings.Contains(tc.s.Term.ScreenText(), "you said: cd ~/'www'") })

	// Folders that cannot be listed start at home.
	h.RemoteDir, h.LocalDir = "/nowhere/at/all", "~/missing"
	if _, err := a.Store().Update(h); err != nil {
		t.Fatal(err)
	}
	fc2, err := a.makeConn(h.ID, fc.kind, 0)
	if err != nil {
		t.Fatal(err)
	}
	a.place(fc2, openAt{})
	waitFor(t, tt, "files listed at home", func() bool { return fc2.fs.remote.client != nil && fc2.fs.local.loaded })
	if fc2.fs.remote.dir != root || fc2.fs.local.dir != a.localHome() || fc2.fs.remote.err != "" || fc2.fs.local.err != "" {
		t.Errorf("fallback in %q (%s) and %q (%s)", fc2.fs.remote.dir, fc2.fs.remote.err, fc2.fs.local.dir, fc2.fs.local.err)
	}
}

func TestDefaultDirStarts(t *testing.T) {
	for dir, want := range map[string]string{"~": "/home/u", "~/www": "/home/u/www", "www/x/..": "/home/u/www", "/srv//app/": "/srv/app"} {
		if got := remoteStart("/home/u", dir); got != want {
			t.Errorf("remoteStart(%q) = %q, want %q", dir, got, want)
		}
	}
	home := t.TempDir()
	abs := filepath.Join(home, "abs")
	for dir, want := range map[string]string{"~": home, "~/proj": filepath.Join(home, "proj"),
		"~" + string(filepath.Separator) + "p": filepath.Join(home, "p"), "rel": filepath.Join(home, "rel"), abs: abs} {
		if got := localStart(home, dir); got != want {
			t.Errorf("localStart(%q) = %q, want %q", dir, got, want)
		}
	}
}
