package app

import (
	"context"
	"io"
	"time"

	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/session"
	"mvdan.cc/sh/v3/syntax"
)

// commandRunner runs one AI command on a host's connection, and can bound
// it in time: remote.Runner over an exec channel, or a shell runner over
// a shell when the gateway refuses exec.
type commandRunner interface {
	RunWithTimeout(ctx context.Context, cmd, dir string, stdout, stderr io.Writer, timeout time.Duration) (int, error)
	BeginTurn()
}

// shellCommandRunner runs commands over a shell runner (a gateway that
// routes and refuses the exec channel). There is one stream: a pty merges
// stderr into it, so stderr is ignored.
type shellCommandRunner struct{ r *session.ShellRunner }

func (shellCommandRunner) BeginTurn() {}

func (s shellCommandRunner) RunWithTimeout(ctx context.Context, cmd, dir string, stdout, stderr io.Writer, timeout time.Duration) (int, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	code, err := s.r.Run(ctx, shellCommand(cmd, dir), stdout)
	if err != nil && ctx.Err() != nil {
		return -1, ctx.Err()
	}
	return code, err
}

// shellCommand is the line typed into a shell to run cmd in dir: a child
// sh, quoted, so the outer shell does not read the command, and stdin from
// /dev/null so it cannot swallow the runner's mark.
func shellCommand(cmd, dir string) string {
	inner := cmd
	if dir != "" {
		inner = "cd -- " + shellArg(dir) + " && " + cmd
	}
	return "sh -c " + shellArg(inner) + " </dev/null"
}

// shellArg is s as one sh word; a string it cannot quote is left as it
// is, not dropped.
func shellArg(s string) string {
	q, err := syntax.Quote(s, syntax.LangBash)
	if err != nil {
		return s
	}
	return q
}

// dialShellRunner dials a connection of its own for h and opens a shell
// runner on it, for a connection that allows no exec channel. The
// connection is h's own pool entry under a key of its own, so nothing the
// runner types reaches the terminal's connection. release closes the
// runner and lets the connection go.
func (a *App) dialShellRunner(ctx context.Context, h assets.Host, c *conn) (commandRunner, func(), error) {
	cfg := a.hostConfig(h, c, func(ctx context.Context) (session.Credentials, error) {
		return a.credentials(c, h)
	})
	cfg.Key = a.poolKey(h) + "|shell"
	client, release, err := a.pool.Acquire(ctx, cfg.Key, cfg)
	if err != nil {
		return nil, nil, err
	}
	r, err := session.NewShellRunner(client, shellCols, shellRows)
	if err != nil {
		release()
		return nil, nil, err
	}
	return shellCommandRunner{r}, func() { r.Close(); release() }, nil
}

// A runner's shell needs a size for its pty; it draws no terminal, so any
// reasonable one does.
const (
	shellCols = 160
	shellRows = 48
)

// joinRelease runs a then b.
func joinRelease(a, b func()) func() { return func() { a(); b() } }
