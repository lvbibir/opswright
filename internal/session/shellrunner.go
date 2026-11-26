package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ShellRunner runs commands over a shell channel of a connection whose
// server allows no exec channel: a jump server that only routes to a
// host, where the terminal and nothing else is served. It types each
// command into the shell and reads its output back between a random mark,
// as cwdShell reads a directory, but on a shell of its own that stays
// open between commands. A caller gives a runner a connection of its own,
// so nothing it types reaches a user's terminal; a pty, if there is one,
// merges stdout and stderr into the one stream it reads.
type ShellRunner struct {
	sess *ssh.Session
	in   io.WriteCloser

	mu      sync.Mutex
	pending []byte // output read past the last answer
	chunks  chan []byte
	done    chan struct{}
	err     error
	once    sync.Once
}

// shellReadyTryWait and shellReadyTries bound a new shell's handshake: the
// preamble is sent again, after this long each time, until the shell
// answers, because a bastion that routes a connection may discard what is
// typed before its shell takes over. A shell that never answers (no shell,
// a login that hangs) leaves NewShellRunner an error with what it said.
const (
	shellReadyTries   = 4
	shellReadyTryWait = 4 * time.Second
)

// shellKillWait bounds waiting for the mark after interrupting a command,
// so that the shell is left clean for the next one.
const shellKillWait = 3 * time.Second

// shellPreamble quiets a shell the runner types into before it sends
// anything: errexit off, so one failing command does not end the shell
// before the mark, bracketed paste off, so readline (bash's line editor)
// writes no \x1b[?2004h/l and no carriage returns around a line, no echo,
// so what is typed is not read back, no onlcr, so a newline is one byte,
// noflsh, so the mark typed behind a command survives a Ctrl-C, and no
// prompt, so nothing between answers is the shell's own. bind is bash's;
// a shell without it has no readline to quiet.
const shellPreamble = `set +e 2>/dev/null; bind 'set enable-bracketed-paste off' 2>/dev/null; stty -echo -onlcr noflsh 2>/dev/null; PS1=; PS2=; PROMPT_COMMAND=; unset PROMPT_COMMAND 2>/dev/null`

// NewShellRunner opens a shell on client, quiets it, and returns a runner
// that types commands into it. The caller owns client: closing the runner
// closes only its channel.
func NewShellRunner(client *ssh.Client, cols, rows int) (*ShellRunner, error) {
	if client == nil {
		return nil, errors.New("session: no client")
	}
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	// A pty makes stty() work and is what a bastion's shell assumes;
	// whether the server gives one or not, the mark framing is the same.
	_ = sess.RequestPty("xterm", rows, cols, ssh.TerminalModes{ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200})
	in, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	out, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	if err := sess.Shell(); err != nil {
		sess.Close()
		return nil, err
	}
	r := &ShellRunner{sess: sess, in: in, chunks: make(chan []byte, 32), done: make(chan struct{})}
	go r.read(out)
	if err := r.ready(); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// read passes the shell's output to chunks, until it ends.
func (r *ShellRunner) read(out io.Reader) {
	defer close(r.chunks)
	buf := make([]byte, 8192)
	for {
		n, err := out.Read(buf)
		if n > 0 {
			p := append([]byte(nil), buf[:n]...)
			select {
			case r.chunks <- p:
			case <-r.done:
				return
			}
		}
		if err != nil {
			r.mu.Lock()
			r.err = err
			r.mu.Unlock()
			return
		}
	}
}

// ready quiets the shell and waits for it to answer a mark, so that a
// login banner, a prompt and any echo are behind us before the first
// command. The preamble is sent again until the shell answers, so that a
// bastion discarding what is typed before its shell takes over does not
// lose it.
func (r *ShellRunner) ready() error {
	tag := newTag()
	line := shellPreamble + `; printf '\n__%s__\n' ` + tag
	for i := 0; i < shellReadyTries; i++ {
		if _, err := io.WriteString(r.in, line+"\n"); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), shellReadyTryWait)
		_, err := r.readUntil(ctx, []byte("__"+tag+"__"))
		cancel()
		if err == nil {
			// What the shell printed while getting ready is not the first
			// command's.
			r.clear()
			return nil
		}
		r.mu.Lock()
		dead := r.err != nil
		r.mu.Unlock()
		if dead {
			return err
		}
	}
	return fmt.Errorf("session: the shell answered nothing; it said: %s", r.seen())
}

// seen is what the shell has printed but not yet answered, for an error.
func (r *ShellRunner) seen() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.pending
	if len(b) > 400 {
		b = b[len(b)-400:]
	}
	return strconv.QuoteToASCII(string(b))
}

// Run types body into the shell and returns its exit status, streaming
// what it prints to out. A cancelled ctx interrupts the command (Ctrl-C)
// and returns ctx.Err() once the shell is clean again.
func (r *ShellRunner) Run(ctx context.Context, body string, out io.Writer) (int, error) {
	tag := newTag()
	if _, err := io.WriteString(r.in, body+"\n"); err != nil {
		return -1, err
	}
	// The mark is a separate, later line so that a Ctrl-C, which abandons
	// the rest of the line it interrupts, cannot take it with it.
	if _, err := io.WriteString(r.in, `printf '\n__%s__ %s\n' `+tag+` $?`+"\n"); err != nil {
		return -1, err
	}
	mark := []byte("\n__" + tag + "__ ")
	before, err := r.readUntil(ctx, mark)
	if err != nil {
		r.interrupt(mark, out)
		return -1, err
	}
	if out != nil {
		out.Write(before)
	}
	line, err := r.readLine(ctx)
	if err != nil {
		return -1, err
	}
	code, aerr := strconv.Atoi(strings.TrimSpace(string(line)))
	if aerr != nil {
		return -1, errors.New("session: the shell's status was unreadable")
	}
	return code, nil
}

// interrupt stops the command a cancelled Run left running, then reads to
// its mark so the shell is at a clean prompt for the next command.
func (r *ShellRunner) interrupt(mark []byte, out io.Writer) {
	io.WriteString(r.in, "\x03")
	ctx, cancel := context.WithTimeout(context.Background(), shellKillWait)
	defer cancel()
	if before, err := r.readUntil(ctx, mark); err == nil {
		if out != nil {
			out.Write(before)
		}
		r.readLine(ctx)
	}
	r.clear()
}

func (r *ShellRunner) clear() {
	r.mu.Lock()
	r.pending = nil
	r.mu.Unlock()
}

// readUntil returns the bytes before mark and leaves what follows in
// pending, waiting for output as it comes. On ctx.Done or the shell
// ending it returns an error, the mark if it had arrived after all.
func (r *ShellRunner) readUntil(ctx context.Context, mark []byte) ([]byte, error) {
	cut := func(p []byte) ([]byte, []byte, bool) {
		i := bytes.Index(p, mark)
		if i < 0 {
			return nil, nil, false
		}
		return append([]byte(nil), p[:i]...), p[i+len(mark):], true
	}
	return r.wait(ctx, cut)
}

// readLine returns the buffered line before the next newline.
func (r *ShellRunner) readLine(ctx context.Context) ([]byte, error) {
	cut := func(p []byte) ([]byte, []byte, bool) {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			return nil, nil, false
		}
		return append([]byte(nil), p[:i]...), p[i+1:], true
	}
	return r.wait(ctx, cut)
}

// wait buffers output until cut finds what it wants, and returns the part
// before it, keeping the rest.
func (r *ShellRunner) wait(ctx context.Context, cut func([]byte) ([]byte, []byte, bool)) ([]byte, error) {
	for {
		r.mu.Lock()
		before, rest, ok := cut(r.pending)
		if ok {
			r.pending = rest
		}
		r.mu.Unlock()
		if ok {
			return before, nil
		}
		select {
		case <-ctx.Done():
			return r.cutOr(ctx.Err(), cut)
		case chunk, ok := <-r.chunks:
			if !ok {
				return r.cutOr(r.ended(), cut)
			}
			r.mu.Lock()
			r.pending = append(r.pending, chunk...)
			r.mu.Unlock()
		}
	}
}

// cutOr is wait's last look after ctx ended or the shell did: output that
// arrived just then still counts.
func (r *ShellRunner) cutOr(err error, cut func([]byte) ([]byte, []byte, bool)) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if before, rest, ok := cut(r.pending); ok {
		r.pending = rest
		return before, nil
	}
	return nil, err
}

func (r *ShellRunner) ended() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	return io.EOF
}

// Close ends the runner's shell.
func (r *ShellRunner) Close() error {
	r.once.Do(func() {
		close(r.done)
		r.sess.Close()
	})
	return nil
}
