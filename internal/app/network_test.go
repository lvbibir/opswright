package app

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/layout"
	"github.com/lvbibir/opswright/internal/netproxy"
	"github.com/lvbibir/opswright/internal/session"
	"github.com/lvbibir/opswright/internal/sshtest"
	"github.com/lvbibir/opswright/locales"
)

func startServer(t *testing.T) *sshtest.Server {
	t.Helper()
	s, err := sshtest.Start("127.0.0.1:0", "demo", "secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestGatewayTerminalAndFiles(t *testing.T) {
	bastion, db := startServer(t), startServer(t)
	bastion.DirectTCPIP = true
	db.SFTPRoot = t.TempDir()
	os.WriteFile(filepath.Join(db.SFTPRoot, "inside.txt"), []byte("x"), 0o644)
	a, tt := newTestApp(t)
	gw := addGateway(t, a, bastion, "bastion-1", "secret", true)
	h := addHost(t, a, db, "db", "secret")
	h.GatewayID = gw.ID
	if _, err := a.Store().Update(h); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tip := a.hostTip(locales.In("en"), h); !strings.HasSuffix(tip, "via bastion-1") {
		t.Errorf("tooltip %q", tip)
	}
	a.Connect(h.ID, openAt{})
	c := a.focused()
	// Every hop's key is trusted on sight, the gateway's first.
	waitFor(t, tt, "connected through the gateway", func() bool { return c.s.Status().State == session.Connected })
	if len(a.known.For(bastion.Addr)) == 0 || len(a.known.For(db.Addr)) == 0 {
		t.Fatal("hop keys not saved")
	}
	if bastion.Tunnels() < 1 || db.Logins() != 1 {
		t.Errorf("tunnels %d, logins %d", bastion.Tunnels(), db.Logins())
	}
	// A files pane goes the same way, on the same connection.
	if _, err := a.OpenFiles(h.ID, openAt{pane: c.id, edge: layout.Right}, c); err != nil {
		t.Fatal(err)
	}
	fc := a.focused()
	waitFor(t, tt, "files through the gateway", func() bool { return fc.fs.remote.client != nil })
	waitFor(t, tt, "remote listing", func() bool { return tt.HasText("inside.txt") })
	// The gateway drops: the session comes back through it.
	bastion.Drop()
	waitFor(t, tt, "reconnected", func() bool { st := c.s.Status(); return st.State == session.Connected && st.Connects == 2 })
	if bastion.Logins() < 2 {
		t.Errorf("bastion logins %d", bastion.Logins())
	}
}

func TestGatewayErrorsAndStatus(t *testing.T) {
	a, _ := newTestApp(t)
	m := locales.In("en")
	cr, _ := a.Store().SaveCredential(assets.Credential{Name: "pw", Kind: assets.CredPassword, User: "ops"})
	gw, _ := a.Store().SaveGateway(assets.Gateway{Name: "bastion-1", Address: "10.0.0.1", User: "ops", CredentialID: cr.ID})
	h, _ := a.Store().Add(assets.Host{Name: "db", Address: "10.0.0.2", User: "demo", Auth: assets.AuthAsk, GatewayID: gw.ID})
	st := session.Status{State: session.Connecting, Hop: "bastion-1"}
	if got := a.statusText(m, h, st, time.Now()); got != "Connecting to bastion-1…" {
		t.Errorf("connecting: %q", got)
	}
	err := &session.HopError{Index: 0, ID: gw.ID, Name: "bastion-1", Jump: true, Err: errors.New("ssh: unable to authenticate")}
	if got := a.errText(m, h, err); !strings.HasPrefix(got, "Jump host bastion-1: Authentication failed for ops@") {
		t.Errorf("gateway auth: %q", got)
	}
	err = &session.HopError{Index: 1, ID: h.ID, Name: "db", Err: errors.New("ssh: unable to authenticate")}
	if got := a.errText(m, h, err); !strings.HasPrefix(got, "Authentication failed for demo@") {
		t.Errorf("target auth: %q", got)
	}
	// A host naming a gateway that does not exist is refused on save.
	a.openEditor(&h)
	a.form.gateway = "nope"
	if a.SaveForm(m) || a.form.err != m.ErrGatewayUnknown() {
		t.Errorf("unknown gateway saved: err %q", a.form.err)
	}
	if got, _ := a.Store().Get(h.ID); got.GatewayID != gw.ID {
		t.Errorf("gateway changed: %q", got.GatewayID)
	}
}

// TestGatewayLoginName: a routed gateway logs in with the composed name;
// the credential's own user must not replace it.
func TestGatewayLoginName(t *testing.T) {
	const login = "liuzhijie@sysadm@192.168.8.40"
	srv, err := sshtest.Start("127.0.0.1:0", login, "secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	a, tt := newTestApp(t)
	a.known.Add(srv.Addr, srv.HostKey.PublicKey())
	cr, err := a.store.SaveCredential(assets.Credential{Name: "bastion", Kind: assets.CredPassword, User: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.kc.Set(keychain.Cred(cr.ID, keychain.Password), []byte("secret")); err != nil {
		t.Fatal(err)
	}
	host, port := splitAddr(t, srv.Addr)
	gw, err := a.store.SaveGateway(assets.Gateway{Name: "bastion", Address: host, Port: port, User: "liuzhijie", CredentialID: cr.ID})
	if err != nil {
		t.Fatal(err)
	}
	h, err := a.store.Add(assets.Host{Name: "zabbix", Address: "192.168.8.40", User: "sysadm", Auth: assets.AuthAsk, GatewayID: gw.ID})
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.Connect(h.ID, openAt{})
	if err != nil {
		t.Fatal(err)
	}
	c := a.conns[id]
	waitFor(t, tt, "connected", func() bool {
		st := c.s.Status()
		if st.State == session.Failed {
			t.Fatalf("failed: %v", st.Err)
		}
		return st.State == session.Connected
	})
}

func TestProxySheetAndEditor(t *testing.T) {
	a, tt := newTestApp(t)
	m := locales.In("en")
	mustClick(t, tt, "Settings")
	tt.Frame()
	mustClick(t, tt, "Proxies")
	tt.Frame()
	if !tt.HasText(m.NoProxies()) {
		t.Fatalf("settings: %q", tt.Texts())
	}
	a.openProxyForm(nil, nil) // the "New Proxy" button, below the fold
	tt.Frame()
	f := a.proxyForm
	f.name, f.address, f.user = "office", "127.0.0.1", "pu"
	f.kind = 1
	tt.Frame()
	f.port, f.password = "3128", "proxy-secret"
	mustClick(t, tt, "Save")
	if a.proxyForm != nil {
		t.Fatalf("not saved: %q", f.err)
	}
	ps := a.Store().Proxies()
	if len(ps) != 1 || ps[0].Kind != assets.ProxyHTTP || ps[0].Port != 3128 || ps[0].User != "pu" {
		t.Fatalf("proxies %+v", ps)
	}
	px := ps[0]
	if b, err := a.kc.Get(keychain.ProxyPassword(px.ID)); err != nil || string(b) != "proxy-secret" {
		t.Errorf("keychain: %q %v", b, err)
	}
	if b, _ := os.ReadFile(filepath.Join(a.p.DataDir, "hosts.json")); strings.Contains(string(b), "proxy-secret") {
		t.Error("proxy password in hosts.json")
	}
	tt.Frame()
	if !tt.HasText("office") || !tt.HasText("HTTP · 127.0.0.1:3128 · pu") {
		t.Errorf("proxy row: %q", tt.Texts())
	}
	// The host editor makes one in place, and comes back with it chosen.
	mustClick(t, tt, "Close Settings")
	a.openEditor(nil)
	a.form.netOpen = true
	tt.Frame()
	a.form.name, a.form.address, a.form.user = "web", "10.0.0.5", "root"
	hf := a.form
	hf.proxy = proxyNew // "New proxy…" chosen in the select
	tt.Frame()
	if a.proxyForm == nil || a.editing || hf.proxy != "" {
		t.Fatalf("New proxy…: sheet %v, editing %v, proxy %q", a.proxyForm != nil, a.editing, hf.proxy)
	}
	tt.Frame()
	if a.form != hf {
		t.Fatal("the editor was dropped behind the proxy sheet")
	}
	a.proxyForm.name, a.proxyForm.address = "socks", "127.0.0.1"
	mustClick(t, tt, "Save")
	tt.Frame()
	if p, _ := a.Store().Proxy(hf.proxy); !a.editing || a.form != hf || p.Name != "socks" {
		t.Fatalf("back to the editor: editing %v, proxy %q", a.editing, hf.proxy)
	}
	if !a.SaveForm(m) {
		t.Fatal(hf.err)
	}
	h := a.Store().List()[0]
	socks, _ := a.Store().Proxy(h.ProxyID)
	if socks.Name != "socks" || socks.Kind != assets.ProxySOCKS5 || socks.Port != 1080 {
		t.Errorf("host proxy %+v", socks)
	}
	if !a.kc.Has(keychain.ProxyPassword(px.ID)) || a.kc.Has(keychain.ProxyPassword(socks.ID)) {
		t.Error("keychain entries")
	}
	// One in use is not deleted, and says who uses it.
	err := a.DeleteProxy(socks.ID)
	if err == nil || !strings.Contains(a.inUseText(m, err), "web") {
		t.Errorf("deleted a proxy in use: %v", err)
	}
	if err := a.DeleteProxy(px.ID); err != nil || a.kc.Has(keychain.ProxyPassword(px.ID)) {
		t.Errorf("delete: %v", err)
	}
	// The editor shows the network section open for it.
	a.openEditor(&h)
	tt.Frame()
	if !a.form.netOpen || !tt.HasText("Network") || a.form.proxy != socks.ID {
		t.Errorf("editor network: open %v, proxy %q", a.form.netOpen, a.form.proxy)
	}
}

func TestConnectThroughProxy(t *testing.T) {
	srv := startServer(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go (&netproxy.Server{User: "pu", Password: "pp", Dial: (&net.Dialer{}).DialContext}).Serve(l)
	a, tt := newTestApp(t)
	m := locales.In("en")
	host, port := splitAddr(t, l.Addr().String())
	px, err := a.Store().SaveProxy(assets.Proxy{Name: "office", Kind: assets.ProxySOCKS5, Address: host, Port: port, User: "pu"})
	if err != nil {
		t.Fatal(err)
	}
	a.kc.Set(keychain.ProxyPassword(px.ID), []byte("wrong"))
	h := addHost(t, a, srv, "box", "secret")
	h.ProxyID = px.ID
	a.Store().Update(h)
	a.known.Add(srv.Addr, srv.HostKey.PublicKey())
	a.Connect(h.ID, openAt{})
	c := a.focused()
	waitFor(t, tt, "proxy failure", func() bool { return c.s.Status().State == session.Failed })
	tt.Frame()
	want := "Proxy authentication failed at office. Check the proxy’s username and password in Settings."
	if got := a.statusText(m, c.host, c.s.Status(), time.Now()); got != want || a.prompting {
		t.Fatalf("status %q, prompting %v", got, a.prompting)
	}
	a.kc.Set(keychain.ProxyPassword(px.ID), []byte("pp"))
	c.s.Reconnect()
	waitFor(t, tt, "connected through the proxy", func() bool { return c.s.Status().State == session.Connected })
}

// TestPoolKeyFollowsGateway: editing a gateway (or its credential) gives
// the host a new connection, not the stale shared one.
func TestPoolKeyFollowsGateway(t *testing.T) {
	old, fresh, db := startServer(t), startServer(t), startServer(t)
	old.DirectTCPIP, fresh.DirectTCPIP = true, true
	a, tt := newTestApp(t)
	gw := addGateway(t, a, old, "bastion", "secret", true)
	h := addHost(t, a, db, "db", "secret")
	h.GatewayID = gw.ID
	h, _ = a.Store().Update(h)
	for _, s := range []*sshtest.Server{old, fresh, db} {
		a.known.Add(s.Addr, s.HostKey.PublicKey())
	}
	key := a.poolKey(h)
	a.Connect(h.ID, openAt{})
	c := a.focused()
	waitFor(t, tt, "connected via old", func() bool { return c.s.Status().State == session.Connected })

	// The gateway now points elsewhere: a new pane goes there.
	gw.Address, gw.Port = splitAddr(t, fresh.Addr)
	if gw, _ = a.Store().SaveGateway(gw); a.poolKey(h) == key {
		t.Fatal("pool key ignores the gateway's address")
	}
	a.Connect(h.ID, openAt{})
	c2 := a.focused()
	waitFor(t, tt, "connected via fresh", func() bool { return c2.s.Status().State == session.Connected })
	if fresh.Tunnels() != 1 || old.Tunnels() != 1 {
		t.Errorf("tunnels old %d fresh %d", old.Tunnels(), fresh.Tunnels())
	}
	// Its credential's user is part of it too.
	key = a.poolKey(h)
	cr, _ := a.Store().Credential(gw.CredentialID)
	cr.User = "other"
	a.Store().SaveCredential(cr)
	if a.poolKey(h) == key {
		t.Error("pool key ignores the gateway's credential user")
	}
	// Its capabilities are too: exec off vs on reconnects.
	key = a.poolKey(h)
	gw.Exec = true
	gw, _ = a.Store().SaveGateway(gw)
	if a.poolKey(h) == key {
		t.Error("pool key ignores the gateway's capabilities")
	}
}

// TestGatewayHopDownMessage: a host the gateway cannot reach is named,
// with the gateway it was tried through, in plain words.
func TestGatewayHopDownMessage(t *testing.T) {
	bastion, db := startServer(t), startServer(t)
	bastion.DirectTCPIP = true
	a, tt := newTestApp(t)
	m := locales.In("en")
	gw := addGateway(t, a, bastion, "bastion-1", "secret", true)
	h := addHost(t, a, db, "db", "secret")
	h.GatewayID = gw.ID
	h, _ = a.Store().Update(h)
	a.known.Add(bastion.Addr, bastion.HostKey.PublicKey())
	db.Close()
	a.Connect(h.ID, openAt{})
	c := a.focused()
	want := "Cannot reach db through bastion-1: " + m.NetRefused()
	waitFor(t, tt, "hop error", func() bool {
		st := c.s.Status()
		return st.Err != nil && a.errText(m, h, st.Err) == want
	})
}
