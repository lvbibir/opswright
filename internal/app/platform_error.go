package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/sshyard/locales"
)

// ReportError displays an already localized platform error without blocking
// window closing or quitting, including before storage is ready; UI thread only.
func (a *App) ReportError(message string) {
	a.toast(func(locales.Messages) ui.Toast { return ui.Toast{Title: message, Type: "error"} })
}
