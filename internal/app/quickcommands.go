package app

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/icons"
	"github.com/lvbibir/opswright/internal/theme"
	"github.com/lvbibir/opswright/locales"
)

// Quick commands is the snippets picker the app shortcut (Ctrl+M) opens in
// the middle of the window: a field, and the entries matching what it
// holds. Up and Down choose one, Enter or a click runs it as the snippets
// pane's own action does, per the entry's mode.

// openQuickCommands shows the quick commands window, empty.
func (a *App) openQuickCommands() {
	a.quickCmd = pickerState{open: true, gen: a.quickCmd.gen + 1}
}

// quickCommandsView is the quick commands window: a field, and the entries
// matching what it holds. Up and Down choose one, Enter or a click runs it.
func (a *App) quickCommandsView(c *ui.Context, m locales.Messages) {
	p := &a.quickCmd
	if !p.open {
		return
	}
	tok := currentTokens(c)
	sheet(ui.Modal(c, &p.open, func() {
		// Its own shortcut closes it, as it opened it.
		if a.pressed(c, actQuickCommands) {
			p.open = false
		}
		ui.Column(c).Gap(10).Width(560).Children(func() {
			var hits []snippetHit
			ui.Row(c).Gap(10).AlignItems(ui.Center).FillWidth().Children(func() {
				ui.Icon(c, icons.Search.SVG).Size(16, 16).Shrink(0).TextColor(tok.TextMuted)
				in := formTextInput(c.Key("qcmd:q"), &p.query).Placeholder(m.SnippetSearch()).Label(m.QuickCommands()).
					AutoFocus().Grow(1)
				p.hold.apply(c, p.gen, in)
				in.OnTextKey(func(k ui.TextKey) bool {
					if k.Mods != 0 {
						return false
					}
					switch k.Key {
					case ui.KeyDown:
						p.sel++
					case ui.KeyUp:
						p.sel--
					default:
						return false
					}
					p.reveal = true
					return true
				})
				hits = a.snippetHits(m, a.store.Snippets(), p.query)
				if in.Changed() {
					p.sel = 0
				}
				p.sel = max(0, min(p.sel, len(hits)-1))
				if in.Submitted() && len(hits) > 0 {
					a.runPickedSnippet(m, hits[p.sel].n.ID, hits[p.sel].n.SendEnter)
					p.open = false
				}
			})
			ui.Box(c).Height(1).FillWidth().Background(tok.Line)
			if len(hits) == 0 {
				note := m.SnippetEmpty()
				if len(a.store.Snippets()) == 0 {
					note = m.SnippetEmptyTitle()
				}
				ui.Text(c, note).FontSize(theme.Body).TextColor(tok.TextMuted).Padding(theme.Space(2))
				return
			}
			gutterScroll(c, ui.Scroll(c.Key("qcmd:list")).MaxHeight(420).FillWidth()).Children(func() {
				ui.Column(c).Gap(2).FillWidth().Children(func() {
					for i, h := range hits {
						if a.quickCommandRow(c, m, tok, h, i == p.sel) {
							a.runPickedSnippet(m, h.n.ID, h.n.SendEnter)
							p.open = false
						}
					}
				})
			})
			a.quickCommandFoot(c, m, tok)
		})
	}), tok)
	p.reveal = false
}

// quickCommandRow is one entry of the window: its body's first line and its
// group, with the icon of the mode Enter runs it in. It reports a click.
func (a *App) quickCommandRow(c *ui.Context, m locales.Messages, tok theme.Tokens, h snippetHit, chosen bool) bool {
	n := h.n
	row := ui.ButtonBase(c.Key("qcmd:"+n.ID)).Label(m.SnippetPreview(n.Name)).FillWidth().Padding(6, 10).Gap(10).
		Radius(theme.RadiusControl).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	ic := tok.TextMuted
	switch {
	case chosen:
		row.Background(tok.Press)
		ic = tok.Text
		if a.quickCmd.reveal {
			row.ScrollIntoView()
		}
	case row.Hovered():
		row.Background(tok.Hover)
	}
	preview := strings.SplitN(h.body, "\n", 2)[0]
	if h.err != nil {
		preview = snippetErrorText(m, h.err)
	}
	mode, modeLabel := icons.Paste, m.SnippetInsert()
	if n.SendEnter {
		mode, modeLabel = icons.Send, m.SnippetSendEnter()
	}
	row.Children(func() {
		ui.Icon(c, mode.SVG).Size(16, 16).Shrink(0).TextColor(ic)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, n.Name).FontSize(theme.Body).FontWeight(500).SingleLine()
			sub := snippetGroupTitle(m, n.Group)
			if preview != "" {
				sub = preview
			}
			ui.Text(c, sub).Font(a.termFamily()).FontSize(theme.Caption).TextColor(tok.TextMuted).SingleLine()
		})
		ui.Text(c, modeLabel).FontSize(theme.Caption).TextColor(tok.TextMuted).Shrink(0)
	})
	return row.Clicked()
}

// quickCommandFoot is the window's foot: the terminal the entry goes to, if
// any, so that a stripped focus is not a silent failure.
func (a *App) quickCommandFoot(c *ui.Context, m locales.Messages, tok theme.Tokens) {
	col, text := tok.TextMuted, m.SnippetNoTarget()
	if cn := a.snippetTarget(); cn != nil {
		col, text = tok.Success, m.SnippetTarget(a.pv(cn.host.Label()), strconv.Itoa(cn.termNo))
	}
	ui.Row(c.Key("qcmd:foot")).FillWidth().Gap(theme.Space(2)).AlignItems(ui.Center).Children(func() {
		dot(c, col)
		ui.Text(c, text).FontSize(theme.Caption).TextColor(tok.TextMuted).Shrink(1).SingleLine()
	})
}
