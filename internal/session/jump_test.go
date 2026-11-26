package session

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/lvbibir/opswright/internal/netproxy"
	"github.com/lvbibir/opswright/internal/sshtest"
)

func knownAll(t *testing.T, ss ...*sshtest.Server) *KnownHosts {
	t.Helper()
	kh, err := OpenKnownHosts(filepath.Join(t.TempDir(), "known_hosts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ss {
		if err := kh.Add(s.Addr, s.HostKey.PublicKey()); err != nil {
			t.Fatal(err)
		}
	}
	return kh
}

func jumpCfg(kh *KnownHosts, bastion, target *sshtest.Server, bastionPW string) Config {
	bastion.DirectTCPIP = true
	return Config{
		Name: "db", ID: "db-id", Addr: target.Addr, User: "demo", Credentials: password("secret"), HostKey: kh.Callback(),
		Jumps: []Hop{{Name: "bastion-1", ID: "b1", Addr: bastion.Addr, User: "demo", Credentials: password(bastionPW), HostKey: kh.Callback()}},
	}
}

func TestJumpChainShellAndProgress(t *testing.T) {
	b, db := server(t), server(t)
	kh := knownAll(t, b, db)
	cfg := jumpCfg(kh, b, db, "secret")
	hops := make(chan string, 4)
	cfg.Progress = func(h string) { hops <- h }
	s := New(cfg, Options{AutoReconnect: true, Backoff: 20 * time.Millisecond}, 80, 24)
	defer s.Close()
	s.Start()
	waitState(t, s, Connected)
	if a, c := <-hops, <-hops; a != "bastion-1" || c != "db" {
		t.Fatalf("progress %q, %q", a, c)
	}
	if b.Tunnels() != 1 || db.Logins() != 1 || b.Logins() != 1 {
		t.Fatalf("tunnels %d, logins %d/%d", b.Tunnels(), b.Logins(), db.Logins())
	}
	s.Write([]byte("hi\r"))
	waitText(t, s, "you said: hi")
	// The middle hop drops: the whole chain is dialed again.
	b.Drop()
	waitFor(t, "reconnected through the chain", func() bool { st := s.Status(); return st.State == Connected && st.Connects == 2 })
	if b.Logins() != 2 || db.Logins() != 2 {
		t.Fatalf("after drop: logins %d/%d", b.Logins(), db.Logins())
	}
	if s.Status().Hop != "" {
		t.Errorf("hop %q after connecting", s.Status().Hop)
	}
}

func TestJumpErrorsNameTheHop(t *testing.T) {
	b, db := server(t), server(t)
	// A jump host refuses the password.
	_, err := Dial(context.Background(), jumpCfg(knownAll(t, b, db), b, db, "nope"))
	var he *HopError
	if !errors.As(err, &he) || he.Index != 0 || he.ID != "b1" || Classify(err) != KindAuth {
		t.Fatalf("bad jump password: %v", err)
	}
	// A hop whose key changed is refused there, and named: at the last
	// hop for the target, at the first for the jump host. A key merely
	// unseen is trusted, so a change is what stops a chain.
	kh := knownAll(t, b)
	kh.Add(db.Addr, hostKey(t)) // not the key db offers now
	_, err = Dial(context.Background(), jumpCfg(kh, b, db, "secret"))
	var changed *HostKeyChangedError
	if !errors.As(err, &he) || he.Index != 1 || !errors.As(err, &changed) || changed.Host != db.Addr {
		t.Fatalf("changed target key: %v", err)
	}
	kh = knownAll(t, db)
	kh.Add(b.Addr, hostKey(t)) // not the key the jump host offers now
	_, err = Dial(context.Background(), jumpCfg(kh, b, db, "secret"))
	if !errors.As(err, &he) || he.Index != 0 || !errors.As(err, &changed) || changed.Host != b.Addr {
		t.Fatalf("changed jump key: %v", err)
	}
	// The target cannot be reached from the jump host.
	cfg := jumpCfg(knownAll(t, b, db), b, db, "secret")
	cfg.Addr = "127.0.0.1:1"
	_, err = Dial(context.Background(), cfg)
	if !errors.As(err, &he) || he.Index != 1 {
		t.Fatalf("unreachable target: %v", err)
	}
}

func TestDialThroughProxy(t *testing.T) {
	srv := server(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ps := &netproxy.Server{User: "pu", Password: "pp", Dial: (&net.Dialer{}).DialContext, Conns: &netproxy.Registry{}}
	go ps.Serve(l)
	for _, kind := range []netproxy.Kind{netproxy.SOCKS5, netproxy.HTTP} {
		pw := "pp"
		cfg := Config{Addr: srv.Addr, User: "demo", Credentials: password("secret"), HostKey: trusted(t, srv).Callback(),
			Proxy: &netproxy.Dialer{Kind: kind, Addr: l.Addr().String(), User: "pu", Password: func(context.Context) (string, error) { return pw, nil }}}
		c, err := Dial(context.Background(), cfg)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		c.Close()
		pw = "wrong"
		_, err = Dial(context.Background(), cfg)
		if Classify(err) != KindProxyAuth || Classify(err).Retryable() {
			t.Fatalf("%s wrong proxy password: %v (%v)", kind, err, Classify(err))
		}
	}
	// A proxy with jump hosts: only the first hop goes through it.
	b := server(t)
	cfg := jumpCfg(knownAll(t, b, srv), b, srv, "secret")
	cfg.Proxy = &netproxy.Dialer{Kind: netproxy.SOCKS5, Addr: l.Addr().String(), User: "pu", Password: func(context.Context) (string, error) { return "pp", nil }}
	c, err := Dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if b.Tunnels() != 1 {
		t.Fatalf("jump tunnels %d", b.Tunnels())
	}
	// Nothing listening at the proxy.
	cfg.Proxy.Addr = "127.0.0.1:1"
	if _, err := Dial(context.Background(), cfg); Classify(err) != KindProxy {
		t.Fatalf("no proxy: %v (%v)", err, Classify(err))
	}
}
