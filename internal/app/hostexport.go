package app

import (
	"bytes"
	"encoding/csv"
	"path/filepath"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/icons"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/settings"
	"github.com/lvbibir/opswright/internal/theme"
	"github.com/lvbibir/opswright/locales"
)

// exportForm is the sheet exporting the hosts chosen in the sidebar as a
// CSV host list, the same one the import reads.
type hostExportForm struct {
	// hosts are the chosen hosts, as they were when the sheet opened.
	hosts []assets.Host
	// secrets is whether the passwords go in the file, in the clear. It is
	// off by default, and what turned it on is a warning the user had to
	// read.
	secrets bool
	// busy is set while the system's save dialog is open.
	busy bool
	err  string
}

// exportColumns are the CSV's columns, the keys the import reads. A
// password column goes in after the group ones when the file is to hold
// secrets.
var exportColumns = []string{"host", "port", "user", "title", "group", "gateway", "su", "suprompt", "supassword"}

// exportSecretColumns are inserted into exportColumns when the file is to
// hold secrets.
var exportSecretColumns = []string{"password"}

// openHostExport opens the sheet exporting the chosen hosts.
func (a *App) openHostExport() {
	hosts, _, _ := a.picked()
	if len(hosts) == 0 {
		return
	}
	a.exp, a.exporting = &hostExportForm{hosts: hosts}, true
}

// exportCSV is the chosen hosts as a CSV host list. Passwords go in only
// when the sheet's box is ticked; a host without one leaves its cell
// blank. A byte order mark starts it and its lines end CR LF, so that a
// spreadsheet reads it as UTF-8 whatever the machine's code page is.
func (a *App) exportCSV(f *hostExportForm) ([]byte, error) {
	cols := exportColumns
	if f.secrets {
		cols = append(append([]string(nil), cols[:5]...), append(exportSecretColumns, cols[5:]...)...)
	}
	var buf bytes.Buffer
	buf.WriteString("\ufeff")
	w := csv.NewWriter(&buf)
	w.UseCRLF = true
	if err := w.Write(cols); err != nil {
		return nil, err
	}
	for _, h := range f.hosts {
		row := map[string]string{
			"host":  h.Address,
			"port":  strconv.Itoa(h.Port),
			"user":  h.User,
			"title": h.Name,
			"group": h.Group,
		}
		if gw, ok := a.store.Gateway(h.GatewayID); ok {
			row["gateway"] = gw.Label()
		}
		if h.TerminalSu {
			row["su"] = "1"
			if h.TerminalSuPrompt != "" {
				row["suprompt"] = h.TerminalSuPrompt
			}
			if f.secrets {
				if v, err := a.kc.Get(keychain.HostElevate(h.ID)); err == nil {
					row["supassword"] = string(v)
					clear(v)
				}
			}
		}
		if f.secrets {
			if h.Auth == assets.AuthPassword {
				if v, err := a.kc.Get(keychain.Host(h.ID, keychain.Password)); err == nil {
					row["password"] = string(v)
					clear(v)
				}
			}
		}
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = row[c]
		}
		if err := w.Write(cells); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// exportHosts writes the CSV where the user chooses and ends the sheet.
// The dialog is off the UI thread, as the platform hook is.
func (a *App) exportHosts(m locales.Messages) {
	f := a.exp
	if f == nil || f.busy || a.p.SaveFile == nil {
		return
	}
	f.err = ""
	data, err := a.exportCSV(f)
	if err != nil {
		f.err = m.ExportFailed(err.Error())
		return
	}
	title := m.ExportCsv()
	def := filepath.Join(a.localHome(), "Opswright-hosts-"+time.Now().Format("2006-01-02")+".csv")
	f.busy = true
	go func() {
		path, err := a.p.SaveFile(title, def)
		if path == "" && err == nil {
			a.post(func() { f.busy = false })
			return
		}
		if err == nil {
			// The file holds passwords: it is the user's to keep, and
			// nothing but the user reads it.
			err = settings.WriteFileAtomic(path, data, 0o600)
		}
		a.post(func() {
			if a.exp != f {
				return
			}
			f.busy = false
			if err != nil {
				f.err = m.ExportFailed(err.Error())
				return
			}
			a.exporting = false
			a.toast(func(m locales.Messages) ui.Toast {
				return ui.Toast{Title: m.ExportDone(path), Type: "success"}
			})
		})
	}()
}

// exportView is the sheet exporting the chosen hosts, with what the file
// will hold and the warning its passwords are in the clear.
func (a *App) exportView(c *ui.Context, m locales.Messages) {
	f := a.exp
	if f == nil {
		return
	}
	tok := currentTokens(c)
	sheet(ui.Modal(c, &a.exporting, func() {
		ui.Column(c).Gap(12).Width(520).Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				tile(c, icons.Export, theme.Brand, 36)
				ui.Column(c).Gap(1).Shrink(1).Grow(1).Children(func() {
					sheetTitle(c, m.ExportCsv())
					ui.Text(c, m.ExportSummary(strconv.Itoa(len(f.hosts)))).FontSize(theme.Footnote).TextColor(tok.TextMuted)
				})
			})
			ui.Text(c, m.ExportHint()).FontSize(theme.Footnote).TextColor(tok.TextMuted)
			formCheckbox(c.Key("exp:secrets"), &f.secrets, m.ExportSecrets())
			if f.secrets {
				ui.Row(c.Key("exp:warn")).Padding(8).Gap(8).AlignItems(ui.Center).Radius(theme.RadiusControl).
					Background(tok.Warning.Alpha(0.12)).Border(1, tok.Warning.Alpha(0.22)).Children(func() {
					ui.Icon(c, icons.Warning.SVG).Size(14, 14).Shrink(0).TextColor(tok.Warning)
					ui.Text(c, m.ExportPlaintextWarning()).FontSize(theme.Footnote).TextColor(tok.Warning).Shrink(1)
				})
			}
			if f.err != "" {
				ui.Text(c, f.err).FontSize(theme.Footnote).TextColor(tok.Danger)
			}
			ui.Box(c).Height(1).FillWidth().Background(tok.Line)
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if pill(c, m.Cancel(), false, false, f.busy) {
					a.exporting = false
				}
				if pill(c, m.ExportEllipsis(), true, false, f.busy || a.p.SaveFile == nil) {
					a.exportHosts(m)
				}
			})
		})
	}), tok)
	if !a.exporting {
		a.exp = nil
	}
}
