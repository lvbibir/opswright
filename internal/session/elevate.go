package session

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Elevate is how an SSH shell is made root as it starts: its command is
// typed once the shell has gone quiet, its password is typed when the
// terminal prints the prompt, and then the cd to Options.Dir and the
// Start command follow, so that the terminal, its startup commands and
// its tmux session all run as root.
//
// The command must be the only thing typed before the password: it is a
// program that reads one line from the terminal, so anything typed after
// it would be taken as the password. The password is typed, not sent as
// ordinary input (serializedInput.answer), and it is never typed at a
// prompt that is only like its own by coincidence when the server's
// processes can be read (Probe).
type Elevate struct {
	// Command is what makes the shell root, e.g. "su -".
	Command string
	// Prompt is what the shell prints while it waits for the password.
	Prompt string
	// Password is read as each shell starts, so that an edit, or an
	// unlock since, takes effect; nil, or an empty password, types none.
	Password func() []byte
	// Probe is whether the server's processes may be read: Linux, over
	// an exec channel the gateway allows. It keeps the password from
	// being typed at a prompt that is not the program's own, and it is
	// given up on for the rest of the connection when a read fails.
	Probe bool
	// Failed, when set, hears why no elevation happened, from the
	// session's own goroutine: "password" (none to type), "timeout" (the
	// prompt never came), "not-su" (the prompt was not the program's, so
	// nothing was typed) or "refused" (it did not become root).
	Failed func(reason string)
}

// The password prompt is waited for for elevateWait after the command is
// typed; the shell is given elevateAuth to become root, read every
// elevateEvery. Variables for the tests' sake.
var (
	elevateWait  = 8 * time.Second
	elevateAuth  = 3 * time.Second
	elevateEvery = 300 * time.Millisecond
)

// elevateProgram is the process naming the program whose prompt the
// password is for, as /proc names it while it waits for one line. A
// system whose su is named otherwise (BusyBox's busybox) refuses instead
// of being answered, and is left to the user to type.
const elevateProgram = "su"

// elevateTail bounds what is kept of the shell's output to match the
// prompt in, as login.feed bounds its own.
const elevateTail = 512

// elevateAsk waits for the password prompt of a starting shell. feed runs
// on the session's output goroutine and must not wait: it matches the
// tail of what the shell printed and closes matched once. The password is
// typed by elevate, on enterShell's goroutine, once the server's
// processes say the program is really the one asking.
type elevateAsk struct {
	e *Elevate
	// probe is whether the server's processes may be read, e.Probe
	// until a read fails. Only the driver touches it.
	probe bool

	mu      sync.Mutex
	tail    []byte
	stopped bool
	matched chan struct{}
	once    sync.Once
}

func newElevateAsk(e *Elevate) *elevateAsk {
	return &elevateAsk{e: e, probe: e.Probe, matched: make(chan struct{})}
}

// feed takes what the shell printed, as termWriter.print does.
func (a *elevateAsk) feed(p []byte) {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.tail = append(a.tail, p...)
	if n := len(a.tail); n > elevateTail {
		a.tail = append([]byte(nil), a.tail[n-elevateTail:]...)
	}
	text := ansiSeq.ReplaceAll(a.tail, nil)
	match := suPromptMatch(string(text), a.e.Prompt)
	a.mu.Unlock()
	if match {
		a.once.Do(func() { close(a.matched) })
	}
}

// stop ends it and lets its tail go, whether or not the prompt was seen.
func (a *elevateAsk) stop() {
	a.mu.Lock()
	a.stopped, a.tail = true, nil
	a.mu.Unlock()
}

// suPromptMatch reports whether text, the tail of what a terminal printed
// with its escape sequences removed, ends with prompt: the spaces around
// the prompt's end are ignored, so "Password:" finds the shell's
// "Password: ". An empty prompt matches nothing.
func suPromptMatch(text, prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	return strings.HasSuffix(strings.TrimRight(text, " \t"), prompt)
}

// failed says why no elevation happened, when anyone listens.
func (e *Elevate) failed(reason string) {
	if e.Failed != nil {
		e.Failed(reason)
	}
}

// elevate types e's command into the shell in and answers its password
// prompt once, saying through Failed why it could not. It runs on
// enterShell's goroutine, so it may wait.
func (s *Session) elevate(ctx context.Context, in *serializedInput, a *elevateAsk) {
	e := a.e
	pass := []byte(nil)
	if e.Password != nil {
		pass = e.Password()
	}
	if len(pass) == 0 {
		a.stop()
		e.failed("password")
		return
	}
	defer clear(pass)
	// A shell that already runs as root is left alone: the prompt would
	// never come, and nothing has to be typed.
	if a.probe {
		switch fg, err := s.probeForeground(ctx); {
		case err != nil:
			a.probe = false
		case fg.LoginUID == 0:
			a.stop()
			return // already root: nothing to do
		}
	}
	if err := in.send([]byte(e.Command + "\r")); err != nil {
		a.stop()
		return
	}
	select {
	case <-a.matched:
	case <-time.After(elevateWait):
		a.stop()
		e.failed("timeout")
		return
	case <-ctx.Done():
		a.stop()
		return
	}
	// The prompt must be the program's own, not one that happens to end
	// the same way: a password typed at a shell prompt is echoed, and
	// what a terminal echoes is logged.
	if a.probe {
		switch fg, err := s.probeForeground(ctx); {
		case err != nil:
			a.probe = false
		case !strings.Contains(fg.Comm, elevateProgram):
			a.stop()
			e.failed("not-su")
			return
		}
	}
	answer := append(append([]byte(nil), pass...), '\r')
	in.answer(answer)
	clear(answer)
	a.stop()
	// It was typed once, and it is never typed again: when the server
	// cannot be read, whether it worked is the terminal's to show.
	if a.probe && !s.awaitRoot(ctx) {
		e.failed("refused")
	}
}

// awaitRoot reports whether the shell has become root, reading the server
// until it has, or elevateAuth has passed.
func (s *Session) awaitRoot(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, elevateAuth)
	defer cancel()
	for {
		if fg, err := s.probeForeground(ctx); err == nil && fg.UID == 0 {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(elevateEvery):
		}
	}
}

// fgBody prints, from Linux's /proc, the effective uid of the shell
// tagged tag and of the process in the foreground of its terminal, and
// that process's name: "<shell uid> <foreground uid> <foreground name>".
// Of no terminal, or a leader gone, the shell is the foreground.
func fgBody(tag string) string {
	return shellBody(tag, `eu() { while read -r k a b r; do [ "$k" = Uid: ] && { echo "$b"; return; }; done <"$1/status"; } 2>/dev/null; `+
		`g=$(sed "s/.*) //" "$d/stat" 2>/dev/null | cut -d" " -f6); f=$p/$g; `+
		`{ [ "${g:-0}" -gt 0 ] 2>/dev/null && [ -r "$f/status" ]; } || f=$d; `+
		`l=$(eu "$d"); u=$(eu "$f"); [ -n "$l" ] && [ -n "$u" ] || exit 1; `+
		`echo "$l $u $(cat "$f/comm" 2>/dev/null)"; exit 0`)
}

// termFg is who runs a terminal and what it runs in the foreground.
type termFg struct {
	LoginUID int
	UID      int
	Comm     string
}

// parseFg reads what fgBody printed.
func parseFg(out string) (termFg, error) {
	f := strings.Fields(out)
	if len(f) < 2 {
		return termFg{}, ErrNoUser
	}
	login, err1 := strconv.Atoi(f[0])
	uid, err2 := strconv.Atoi(f[1])
	if err1 != nil || err2 != nil || login < 0 || uid < 0 {
		return termFg{}, ErrNoUser
	}
	fg := termFg{LoginUID: login, UID: uid}
	if len(f) > 2 {
		fg.Comm = f[2]
	}
	return fg, nil
}

// probeForeground reads from the server who runs the session's terminal
// and what it runs in the foreground, through the same probe shell as
// ProbeUser. It reads Linux's /proc, so it fails on other servers.
func (s *Session) probeForeground(ctx context.Context) (termFg, error) {
	out, found, err := s.probeShell(ctx, fgBody)
	if err != nil {
		return termFg{}, err
	}
	if !found {
		return termFg{}, ErrNoUser
	}
	return parseFg(out)
}
