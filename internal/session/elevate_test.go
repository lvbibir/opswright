package session

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lvbibir/opswright/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

func TestSuPromptMatch(t *testing.T) {
	for _, tc := range []struct {
		text, prompt string
		want         bool
	}{
		{text: "Password: ", prompt: "Password: ", want: true},
		{text: "Password:", prompt: "Password: ", want: true},
		{text: "Password: ", prompt: "Password:", want: true},
		{text: "Welcome\r\n$ Password: ", prompt: "Password: ", want: true},
		{text: "密码：", prompt: "密码：", want: true},
		{text: "user@host's password: ", prompt: "Password: ", want: false},
		{text: "Password: ", prompt: "", want: false},
		{text: "", prompt: "Password: ", want: false},
	} {
		if got := suPromptMatch(tc.text, tc.prompt); got != tc.want {
			t.Errorf("suPromptMatch(%q, %q) = %v, want %v", tc.text, tc.prompt, got, tc.want)
		}
	}
}

func TestParseFg(t *testing.T) {
	fg, err := parseFg("1000 0 su\n")
	if err != nil || fg.LoginUID != 1000 || fg.UID != 0 || fg.Comm != "su" {
		t.Fatalf("parseFg = %+v, %v", fg, err)
	}
	if fg, err := parseFg("1000 1000"); err != nil || fg.Comm != "" {
		t.Fatalf("no name: %+v, %v", fg, err)
	}
	for _, bad := range []string{"", "1000", "x 1000 su", "1000 -1 bash"} {
		if _, err := parseFg(bad); !errors.Is(err, ErrNoUser) {
			t.Errorf("parseFg(%q) = %v, want ErrNoUser", bad, err)
		}
	}
}

// TestElevateAskFeed: the ask matches the prompt at the end of what the
// shell printed, escape sequences and all, and does not match anything
// else.
func TestElevateAskFeed(t *testing.T) {
	a := newElevateAsk(&Elevate{Prompt: "Password: "})
	select {
	case <-a.matched:
		t.Fatal("matched before anything was printed")
	default:
	}
	a.feed([]byte("Welcome to the box\r\n$ "))
	select {
	case <-a.matched:
		t.Fatal("matched output that is not the prompt")
	default:
	}
	a.feed([]byte("\x1b[31mPassword: \x1b[0m"))
	select {
	case <-a.matched:
	default:
		t.Fatal("the prompt, escapes and all, did not match")
	}
	a.stop()
	a.feed([]byte("Password: ")) // no second close
}

// TestElevateAsksOnce: with no probe the password is typed once, right
// after the command, and never as ordinary input.
func TestElevateAsksOnce(t *testing.T) {
	sb := &syncBuf{}
	in := &serializedInput{WriteCloser: sb}
	a := newElevateAsk(&Elevate{Command: "su -", Prompt: "Password: ", Password: func() []byte { return []byte("s3cret") }})
	close(a.matched)
	(&Session{}).elevate(context.Background(), in, a)
	want := "su -\rs3cret\r"
	waitFor(t, "the command and the password", func() bool { return sb.String() == want })
	a.feed([]byte("Password: "))
	if sb.String() != want {
		t.Fatalf("typed again: %q", sb.String())
	}
}

// TestElevateWithoutAPrompt: a prompt that never comes types nothing and
// is told as such, so the shell goes on as the login user's.
func TestElevateWithoutAPrompt(t *testing.T) {
	old := elevateWait
	elevateWait = 40 * time.Millisecond
	defer func() { elevateWait = old }()
	var reasons reasonLog
	sb := &syncBuf{}
	a := newElevateAsk(&Elevate{Command: "su -", Prompt: "Password: ", Password: func() []byte { return []byte("s3cret") }, Failed: reasons.add})
	(&Session{}).elevate(context.Background(), &serializedInput{WriteCloser: sb}, a)
	if got := reasons.all(); len(got) != 1 || got[0] != "timeout" {
		t.Fatalf("reasons %v", got)
	}
	if sb.String() != "su -\r" {
		t.Fatalf("typed %q", sb.String())
	}
}

// TestElevateWithoutAPassword: nothing is typed at all, and no prompt is
// waited for.
func TestElevateWithoutAPassword(t *testing.T) {
	var reasons reasonLog
	sb := &syncBuf{}
	a := newElevateAsk(&Elevate{Command: "su -", Prompt: "Password: ", Password: func() []byte { return nil }, Failed: reasons.add})
	(&Session{}).elevate(context.Background(), &serializedInput{WriteCloser: sb}, a)
	if got := reasons.all(); len(got) != 1 || got[0] != "password" {
		t.Fatalf("reasons %v", got)
	}
	if sb.String() != "" {
		t.Fatalf("typed %q", sb.String())
	}
}

// TestElevateCancelled: a connection going drops the wait, and nothing
// is typed at the shell it left.
func TestElevateCancelled(t *testing.T) {
	old := elevateWait
	elevateWait = time.Minute
	defer func() { elevateWait = old }()
	ctx, cancel := context.WithCancel(context.Background())
	sb := &syncBuf{}
	a := newElevateAsk(&Elevate{Command: "su -", Prompt: "Password: ", Password: func() []byte { return []byte("s3cret") }})
	cancel()
	(&Session{}).elevate(ctx, &serializedInput{WriteCloser: sb}, a)
	if strings.Contains(sb.String(), "s3cret") {
		t.Fatalf("typed %q", sb.String())
	}
}

// TestElevateProbe: the password is typed only when the server's
// foreground process is the one asking, and the shell is left alone when
// it is not; the prompt alone is not enough.
func TestElevateProbe(t *testing.T) {
	for _, tc := range []struct {
		name string
		// comm is what the server's foreground looks like while the
		// prompt is printed: "su" is the program really asking, anything
		// else is a prompt that only ends the same way.
		comm string
		off  bool
		want string // what the reason is, "" for elevated
	}{
		{name: "su asking", comm: "su"},
		{name: "some other prompt", comm: "bash", want: "not-su"},
		{name: "processes unreadable", off: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fg := &fakeProbe{}
			fg.set(1000, 1000, "bash")
			fg.off = tc.off
			srv := server(t)
			srv.Exec = fg.exec
			srv.Shell = suShell("s3cret", fg, tc.comm)
			var reasons reasonLog
			raised := &Elevate{
				Command: "su -", Prompt: "Password: ", Probe: true,
				Password: func() []byte { return []byte("s3cret") },
				Failed:   reasons.add,
			}
			s := New(Config{Addr: srv.Addr, User: "demo", Credentials: password("secret"), HostKey: trusted(t, srv).Callback()},
				Options{Elevate: raised}, 80, 24)
			defer s.Close()
			s.Start()
			waitState(t, s, Connected)
			if tc.want == "" {
				// su - succeeded in the fake shell: it shows root's prompt.
				waitText(t, s, "root#")
			} else {
				waitFor(t, "the reason "+tc.want, func() bool { return len(reasons.all()) > 0 })
				if got := reasons.all(); got[0] != tc.want {
					t.Fatalf("reasons %v, want %s", got, tc.want)
				}
			}
			time.Sleep(50 * time.Millisecond)
			screen := s.Term.ScreenText()
			if strings.Contains(screen, "s3cret") {
				t.Fatalf("the password is on screen: %q", screen)
			}
		})
	}
}

// fakeProbe answers a fake server's /proc probes with what its fixture
// says runs in the terminal's foreground, and says a probe fails when it
// is off, as a server without /proc does.
type fakeProbe struct {
	mu         sync.Mutex
	login, uid int
	comm       string
	off        bool
}

func (f *fakeProbe) set(login, uid int, comm string) {
	f.mu.Lock()
	f.login, f.uid, f.comm = login, uid, comm
	f.mu.Unlock()
}

func (f *fakeProbe) answer() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.off {
		return "", false
	}
	return strconv.Itoa(f.login) + " " + strconv.Itoa(f.uid) + " " + f.comm, true
}

// probeKind says which probe an app request is, by what it reads.
func probeKind(req string) string {
	switch {
	case strings.Contains(req, `cat "$f/comm"`):
		return "fg"
	case strings.Contains(req, `nm "$l"`):
		return "user"
	case strings.Contains(req, "$(pwd)"):
		return "cwd"
	}
	return ""
}

// exec serves the app's probes: the probe shell kept open (exec sh), and
// one run on a channel of its own.
func (f *fakeProbe) exec(ch ssh.Channel, cmd string) int {
	if cmd == "exec sh" {
		r := bufio.NewReader(ch)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return 0
			}
			if mark := probeMark(line); mark != "" {
				f.answerProbe(ch, mark)
			}
		}
	}
	if probeKind(cmd) == "" {
		return 1
	}
	out, ok := f.answer()
	if !ok {
		return 1
	}
	io.WriteString(ch, out+"\n")
	return 0
}

// probeMark is the mark of a probe typed into the probe shell.
func probeMark(line string) string {
	const at = `printf '%s %s %s %s\n' `
	i := strings.Index(line, at)
	if i < 0 {
		return ""
	}
	rest := line[i+len(at):]
	j := strings.IndexByte(rest, ' ')
	if j <= 0 {
		return ""
	}
	return rest[:j]
}

// answerProbe answers a probe typed into the probe shell, in the marks'
// own format: "<mark> <status> <output> <mark>".
func (f *fakeProbe) answerProbe(ch ssh.Channel, mark string) {
	out, ok := f.answer()
	if !ok {
		fmt.Fprintf(ch, "%s 1  %s\n", mark, mark)
		return
	}
	fmt.Fprintf(ch, "%s 0 %s %s\n", mark, out, mark)
}

// suShell is a shell that becomes root with su -: it echoes what is
// typed, answers "su -" with the password prompt, and takes the next line
// as the password without echoing it, as a terminal with echo off does.
// While it waits for the password the probe fixture names asking as the
// terminal's foreground process. A wrong password is refused, once.
func suShell(pw string, probe *fakeProbe, asking string) sshtest.Shell {
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
				cmd := string(line)
				line = line[:0]
				io.WriteString(ch, "\r\n")
				switch {
				case waiting:
					waiting = false
					if cmd != pw {
						probe.set(1000, 1000, "bash")
						io.WriteString(ch, "su: Authentication failure\r\n")
						prompt()
						continue
					}
					root = true
					probe.set(0, 0, "bash")
					prompt()
				case cmd == "su -":
					waiting = true
					probe.set(1000, 1000, asking)
					io.WriteString(ch, "Password: ")
				case cmd != "":
					io.WriteString(ch, "you said: "+cmd+"\r\n")
					prompt()
				default:
					prompt()
				}
			}
		}
	}
}

// reasonLog is why the elevation failed, as the session reported it from
// its own goroutine.
type reasonLog struct {
	mu  sync.Mutex
	got []string
}

func (l *reasonLog) add(r string) {
	l.mu.Lock()
	l.got = append(l.got, r)
	l.mu.Unlock()
}

func (l *reasonLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.got...)
}

// syncBuf is a writer several goroutines write, for the input the session
// serializes.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *syncBuf) Close() error { return nil }
