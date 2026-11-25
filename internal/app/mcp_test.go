package app

import (
	"net"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/settings"
)

// clickRowControl clicks over the settings row labeled label, right of
// the label and down past its note, until done holds: the row's control
// carries the same accessible label, which Find gives to the row's text
// first.
func clickRowControl(t *testing.T, tt *ui.Tester, label string, done func() bool) {
	t.Helper()
	r := mustFind(t, tt, label)
	for y := r.Y + r.H/2; y < r.Y+48; y += 8 {
		for x := float32(1090); x > r.X+r.W; x -= 6 {
			tt.ClickAt(x, y)
			tt.Frame()
			if done() {
				return
			}
		}
	}
	t.Fatalf("no control on the row %q", label)
}

// freePort is a port nothing listened on a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestMCPAsksBeforeTurningOn(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1100, 2600)
	s := a.Settings()
	if s.MCPEnabled || s.MCPPort != settings.MCPPortDefault {
		t.Fatalf("defaults %v %d", s.MCPEnabled, s.MCPPort)
	}
	s.MCPPort = freePort(t)
	if err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	a.openSettings(prefAgents)
	tt.Frame()
	if a.mcp.srv != nil {
		t.Fatalf("off: srv %v", a.mcp.srv)
	}

	// Cancel turns nothing on.
	a.mcpAsk = &mcpAsk{open: true}
	tt.Frame()
	mustClick(t, tt, "Cancel")
	tt.Frame()
	if a.mcpAsk != nil || a.Settings().MCPEnabled {
		t.Fatal("cancel turned it on")
	}

	clickRowControl(t, tt, "Allow AI agents to connect", func() bool { return a.mcpAsk != nil || a.Settings().MCPEnabled })
	if a.mcpAsk == nil || a.Settings().MCPEnabled {
		t.Fatal("the switch did not ask first")
	}
	mustClick(t, tt, "Turn On (3)")
	tt.Frame()
	if a.Settings().MCPEnabled || a.mcpAsk == nil {
		t.Fatal("confirmed before the wait")
	}
	tt.Advance(time.Second)
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.Frame()
	mustClick(t, tt, "I Understand, Turn On")
	tt.Frame()
	if !a.Settings().MCPEnabled || a.mcp.srv == nil || a.mcp.err != nil {
		t.Fatalf("not on: %v %v", a.mcp.srv, a.mcp.err)
	}
	tok, err := a.kc.Get(keychain.MCPToken)
	if err != nil || string(tok) != a.mcp.token || len(tok) < 40 {
		t.Fatalf("token %q %v", tok, err)
	}

	clickRowControl(t, tt, "Allow AI agents to connect", func() bool { return !a.Settings().MCPEnabled })
	if a.Settings().MCPEnabled || a.mcp.srv != nil || a.mcpAsk != nil {
		t.Fatal("turning off did not stop it")
	}
}

func TestMCPBackupKeepsDeviceSwitch(t *testing.T) {
	cur, in := settings.Default(), settings.Default()
	in.MCPEnabled, in.MCPPort = true, 50000
	if out := mergeSettings(cur, in); out.MCPEnabled || out.MCPPort != cur.MCPPort {
		t.Fatalf("a backup changed MCP: %v %d", out.MCPEnabled, out.MCPPort)
	}
}

func TestMCPPortNormalize(t *testing.T) {
	for _, p := range []int{0, 80, 70000} {
		s := settings.Settings{MCPPort: p}
		s.Normalize()
		if s.MCPPort != settings.MCPPortDefault {
			t.Errorf("port %d kept as %d", p, s.MCPPort)
		}
	}
}

func TestMCPEnableWaitsForMasterPassword(t *testing.T) {
	a, tt, _ := newLockedTestApp(t)
	a.settings.MCPPort = freePort(t)
	a.settings.MCPEnabled = true
	// The endpoint needs the access token from the locked store, so
	// nothing starts until the master password opens it.
	a.syncMCP()
	tt.Frame()
	if a.mcp.srv != nil {
		t.Fatal("the MCP endpoint started while the store was locked")
	}
	setMasterPassword(t, a, tt, "correct horse")
	a.syncMCP()
	waitFor(t, tt, "the MCP endpoint started", func() bool { return a.mcp.srv != nil })
}
