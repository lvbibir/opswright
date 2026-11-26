//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/creack/pty"
	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/layout"
	"github.com/lvbibir/opswright/internal/session"
	"github.com/lvbibir/opswright/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

// bashShell runs a real /bin/bash on a pty for a session's shell channel,
// as a bastion that routes to a host gives (readline and all).
func bashShell(ch ssh.Channel, _ sshtest.PTY, resize <-chan sshtest.PTY) int {
	ptmx, err := pty.Start(exec.Command("/bin/bash"))
	if err != nil {
		return 127
	}
	defer ptmx.Close()
	go func() {
		for p := range resize {
			pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(p.Cols), Rows: uint16(p.Rows)})
		}
	}()
	go io.Copy(ptmx, ch)
	io.Copy(ch, ptmx)
	return 0
}

// TestRoutedGatewayRunsCommandsOverShell: a gateway that routes and refuses
// the exec channel runs the AI's commands over a shell runner of its own,
// and refuses file access.
func TestRoutedGatewayRunsCommandsOverShell(t *testing.T) {
	srv, err := sshtest.Start("127.0.0.1:0", "demo", "secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	srv.Exec = nil // no exec channel, as a bastion that only routes
	srv.Shell = bashShell
	a, tt := newTestApp(t)
	a.known.Add(srv.Addr, srv.HostKey.PublicKey())
	cr, err := a.store.SaveCredential(assets.Credential{Name: "gw", Kind: assets.CredPassword, User: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.kc.Set(keychain.Cred(cr.ID, keychain.Password), []byte("secret")); err != nil {
		t.Fatal(err)
	}
	host, port := splitAddr(t, srv.Addr)
	gw, err := a.store.SaveGateway(assets.Gateway{Name: "gw", Address: host, Port: port, User: "demo", CredentialID: cr.ID, Login: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := a.store.Add(assets.Host{Name: "target", Address: "10.0.0.9", User: "root", Auth: assets.AuthAsk, GatewayID: gw.ID})
	if err != nil {
		t.Fatal(err)
	}
	if a.gatewayExecChannel(h) || a.gatewaySFTP(h) {
		t.Fatal("a routed gateway without exec or sftp reports either")
	}
	consentHost(t, a, h.ID)
	id, err := a.Connect(h.ID, openAt{})
	if err != nil {
		t.Fatal(err)
	}
	c := a.conns[id]
	waitFor(t, tt, "connected", func() bool { return c.s.Status().State == session.Connected })

	// The exec-based tool panes are refused on this host, the others not.
	for _, k := range []layout.Kind{layout.Monitor, layout.Usage, layout.History, layout.Docker} {
		if a.toolCanRun(k) {
			t.Fatalf("%v allowed on a gateway without exec", k)
		}
	}
	for _, k := range []layout.Kind{layout.HostInfo, layout.Snippets, layout.Logs, layout.Broadcast} {
		if !a.toolCanRun(k) {
			t.Fatalf("%v refused on a gateway without exec", k)
		}
	}

	// Its latency comes from its session: an SSH keepalive round trip
	// through the gateway, not a direct probe that cannot reach the host.
	a.PingAll()
	waitFor(t, tt, "latency through the gateway", func() bool {
		r, ok := a.pingOf(h)
		return ok && r.ok
	})

	tools := newHostTools(a, c)
	defer tools.end()
	out, code, err := tools.Call(context.Background(), "run_command", json.RawMessage(`{"command":"echo hello"}`))
	if err != nil || code != 0 || !strings.Contains(out, "hello") {
		t.Fatalf("run_command over shell: %q code %d err %v", out, code, err)
	}
	if out, code, err := tools.Call(context.Background(), "list_dir", json.RawMessage(`{"path":"/"}`)); err != nil || code != 1 || !strings.Contains(out, "file access") {
		t.Fatalf("list_dir: %q code %d err %v", out, code, err)
	}
}
