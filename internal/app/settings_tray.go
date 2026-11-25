package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/settings"
	"github.com/lvbibir/opswright/locales"
)

func (a *App) trayPreferences(c *ui.Context, m locales.Messages, g *settingsGroup, s *settings.Settings, changed *bool) {
	g.row(m.TrayClick(), m.TrayClickNote(), func() {
		cur := m.TrayClickPanel()
		if s.TrayClick == settings.TrayClickWindow {
			cur = m.TrayClickWindow()
		}
		if formSelect(c.Key("trayclick"), &cur, []string{m.TrayClickPanel(), m.TrayClickWindow()}).Label(m.TrayClick()).Width(180).Changed() {
			s.TrayClick = settings.TrayClickPanel
			if cur == m.TrayClickWindow() {
				s.TrayClick = settings.TrayClickWindow
			}
			*changed = true
		}
	})
}
