package app

import (
	"errors"
	"os"
	"syscall"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/sshyard/internal/keychain"
	"github.com/lvbibir/sshyard/internal/theme"
	"github.com/lvbibir/sshyard/locales"
)

// unlockView replaces the app when its data folder could not be read at
// all: there is nothing to unlock and nothing else to show.
func (a *App) unlockView(c *ui.Context) {
	base := *c.Theme()
	base.Font = a.fonts.ui
	tok := theme.For(a.settings.Theme, base.Dark)
	c.SetTheme(tok.Apply(base))
	m := locales.In(c.Locale())
	a.unlock.mu.Lock()
	err := a.unlock.err
	busy := a.unlock.busy
	a.unlock.mu.Unlock()
	ui.Column(c).Fill().Background(tok.Canvas).TextColor(tok.Text).Children(func() {
		ui.Row(c.Key("locked-title")).Height(bandHeight(c)).FillWidth().DragWindow().Children(func() {
			ui.Spacer(c)
			a.windowButtons(c, m, tok)
		})
		ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Gap(theme.Space(4)).Children(func() {
			sheetTitle(c, m.SecretsUnlockFailed())
			if err != nil {
				ui.Text(c, unlockErrorText(m, err)).FontSize(theme.Body).TextColor(tok.Danger).MaxWidth(480)
			}
			ui.Text(c, m.SecretsUnlockSafety()).FontSize(theme.Footnote).TextColor(tok.TextMuted).MaxWidth(480)
			if pill(c, m.SecretsRetry(), true, false, busy) {
				a.retryPrepare()
			}
		})
	})
}

// Only known reasons and operation names enter the interface, never raw errors.
func unlockErrorText(m locales.Messages, err error) string {
	reason := unlockErrorReason(m, err)
	stage := m.SecretsStageOpen()
	var op *keychain.OpError
	if errors.As(err, &op) {
		switch op.Op {
		case "lock":
			stage = m.SecretsStageLock()
		case "master-cache":
			stage = m.SecretsStageMasterCache()
		case "master-password":
			stage = m.SecretsStageMasterPassword()
		case "master-password-write":
			stage = m.SecretsStageWriteMasterPassword()
		case "file-read":
			stage = m.SecretsStageReadFile()
		case "file-decrypt":
			stage = m.SecretsStageDecrypt()
		case "file-write":
			stage = m.SecretsStageWriteFile()
		}
	}
	return m.SecretsFailureDetail(stage, reason)
}

func unlockErrorReason(m locales.Messages, err error) string {
	reason := m.SecretsReasonSystem()
	switch {
	case errors.Is(err, os.ErrPermission):
		reason = m.SecretsReasonPermission()
	case errors.Is(err, os.ErrNotExist):
		reason = m.SecretsReasonNotFound()
	case errors.Is(err, syscall.ENOSPC):
		reason = m.SecretsReasonDiskFull()
	case errors.Is(err, keychain.ErrBusy):
		reason = m.SecretsReasonBusy()
	case errors.Is(err, keychain.ErrMasterMissing):
		reason = m.SecretsReasonMasterMissing()
	case errors.Is(err, keychain.ErrMasterInvalid):
		reason = m.SecretsReasonMasterInvalid()
	case errors.Is(err, keychain.ErrCorrupt):
		reason = m.SecretsReasonCorrupt()
	case errors.Is(err, keychain.ErrVersion):
		reason = m.SecretsReasonVersion()
	case errors.Is(err, keychain.ErrUnavailable):
		reason = m.SecretsReasonUnavailable()
	case errors.Is(err, keychain.ErrWrongPassword):
		reason = m.MasterPasswordWrong()
	case errors.Is(err, keychain.ErrPasswordSetup):
		reason = m.SecretsReasonPasswordSetup()
	}
	return reason
}
