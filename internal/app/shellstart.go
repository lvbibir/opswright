package app

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/layout"
	"github.com/lvbibir/opswright/locales"
	"golang.org/x/crypto/ssh"
)

// What an SSH terminal's shell is given each time it starts
// (session.Options.Start): the command the terminal was opened for, once
// (the shell in a container), else its host's startup commands
// (Host.Startup).

// shellStart is what a terminal's shell is given as it starts. The
// session asks from its own goroutine, hence mu.
type shellStart struct {
	mu sync.Mutex
	// once is typed at the next start only
	once string
	// dir is the folder the shell is taken to as it starts
	// (Host.TerminalDir), "" for its home
	dir string
	// startup is set when the host's startup commands are typed
	// (Host.Startup); cleared for a terminal opened without them
	startup bool
	// ran is set once a command given once was typed: the folders the
	// terminal then reports are not the server's
	ran bool
	// cwd is the folder the terminal last reported, at its connect number
	// cwdConn; prevCwd the one before, of an earlier connect (noteCwd)
	cwd, prevCwd      string
	cwdConn, prevConn int
}

// shellStartCmd is what c's shell is asked to run as it starts, on client.
func (a *App) shellStartCmd(c *conn) func(context.Context, *ssh.Client) string {
	st := c.start
	return func(ctx context.Context, client *ssh.Client) string {
		connects := c.s.Status().Connects
		st.mu.Lock()
		once, startup := st.once, st.startup
		st.once = ""
		if once != "" {
			st.ran = true
		}
		back, ran, dir := st.backDir(connects), st.ran, st.dir
		st.mu.Unlock()
		if once != "" {
			return once
		}
		cd := ""
		if returnDirWanted(a.returnDirOn.Load(), connects, back, ran) {
			cd = returnDirOn(ctx, client, back, dir)
		}
		var lines []string
		if startup {
			lines = a.startupLines(c)
		}
		return startLine(cd, lines)
	}
}

// startLine is what a starting shell is typed after the cd to its
// folder: the cd back to where it was, then its host's startup commands,
// each on its own line.
func startLine(cd string, startup []string) string {
	var parts []string
	for _, p := range append([]string{cd}, startup...) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\r")
}

// startupLines are the startup commands of c's host, read from the
// encrypted store as its shell starts, so that an edit takes effect at
// the next one; none when they cannot be read, which a toast tells.
func (a *App) startupLines(c *conn) []string {
	text, err := a.kc.Get(keychain.HostStartup(c.host.ID))
	if errors.Is(err, keychain.ErrNotFound) {
		return nil
	}
	if err == nil {
		err = assets.ValidateStartup(string(text))
	}
	if err != nil {
		host := c.host.Label()
		a.post(func() {
			a.toasts = append(a.toasts, func(m locales.Messages) ui.Toast {
				return ui.Toast{Title: m.StartupReadFailed(host), Type: "error"}
			})
		})
		return nil
	}
	return assets.StartupLines(string(text))
}

// openTerminal opens a terminal to a saved host in a new tab, prep
// preparing it before it starts.
func (a *App) openTerminal(hostID string, prep func(*conn)) (int, error) {
	c, err := a.makeConn(hostID, layout.Terminal, 0)
	if err != nil {
		return 0, err
	}
	if prep != nil {
		prep(c)
	}
	a.place(c, openAt{})
	return c.id, nil
}

// ConnectWithoutStartup opens a terminal to a saved host that does not
// type the host's startup commands: a way in should they keep its shell
// from starting.
func (a *App) ConnectWithoutStartup(hostID string) (int, error) {
	return a.openTerminal(hostID, func(c *conn) {
		if c.start != nil {
			c.start.startup = false
		}
	})
}
