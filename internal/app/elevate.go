package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/session"
	"github.com/lvbibir/opswright/locales"
)

// elevateCommand is what a host's terminals run to become root
// (Host.TerminalSu). It is a login shell, so it starts in the target
// user's home: the cd to the terminal's folder is typed after it, not
// before.
const elevateCommand = "su -"

// elevateOptions is how the terminals of h become root as their shell
// starts, nil when they do not: Host.TerminalSu is off, the login user is
// root already, or no password is saved. A locked store, and a host
// without a password, leave the terminal as the login user's, as it is.
//
// The password is read from the encrypted store as each shell starts, so
// an edit, or an unlock since, takes effect. The server's processes are
// read only when the connection may run commands of its own
// (gatewayExecChannel): through a gateway that serves the terminal and
// nothing else they are not, and the shell goes on without the elevation
// rather than typing a password at a prompt that may not be su's.
func (a *App) elevateOptions(h assets.Host) *session.Elevate {
	if !h.TerminalSu || h.User == "root" {
		return nil
	}
	if ok, err := a.kc.Exists(keychain.HostElevate(h.ID)); err != nil || !ok {
		return nil
	}
	id, host := h.ID, h.Label()
	return &session.Elevate{
		Command: elevateCommand,
		Prompt:  h.SuPrompt(),
		Probe:   a.gatewayExecChannel(h),
		Password: func() []byte {
			b, err := a.kc.Get(keychain.HostElevate(id))
			if err != nil {
				return nil
			}
			return b
		},
		Failed: func(reason string) {
			a.post(func() {
				a.toast(func(m locales.Messages) ui.Toast {
					return ui.Toast{Title: m.ElevateFailed(host, elevateReason(m, reason)), Type: "error"}
				})
			})
		},
	}
}

// elevateReason says why a host's terminals did not become root, as the
// session reported it.
func elevateReason(m locales.Messages, reason string) string {
	switch reason {
	case "password":
		return m.ElevateNoPassword()
	case "timeout":
		return m.ElevateNoPrompt()
	case "not-su":
		return m.ElevateNotSu()
	}
	return m.ElevateRefused()
}
