package app

import (
	"math"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/settings"
	"github.com/lvbibir/opswright/locales"
)

// The interface zooms the whole window's content (ui.Context.SetZoom):
// laid out smaller and drawn larger at the screen's resolution, so text,
// icons and the terminal's cells stay sharp. The zoom is the one chosen
// in the settings, or follows the system: the text size Windows lets the
// user set, which the app's own type scale would otherwise ignore.

// zoomState is what the zoom follows besides the settings.
type zoomState struct {
	// now is the main window's zoom in effect, which the shortcuts step
	// from.
	now float32
}

// zoomFor is the zoom of a window: the one chosen (percent, 0 for the
// system's), else the system's text size.
func zoomFor(chosen int, textScale float32) float32 {
	if chosen > 0 {
		return float32(chosen) / 100
	}
	z := textScale
	if z <= 0 {
		z = 1
	}
	// In steps of 5%.
	z = float32(math.Round(float64(z)*20) / 20)
	return max(0.5, min(z, 3))
}

// applyZoom zooms the content of the window c builds.
func (a *App) applyZoom(c *ui.Context, main bool) {
	text := c.Preferences().TextScale
	z := zoomFor(a.settings.UIZoom, text)
	c.SetZoom(z)
	if main {
		a.zoom.now = z
	}
}

// stepZoom sets the zoom to the next of settings.UIZooms larger (dir > 0)
// or smaller than the one in effect, or back to following the system (dir
// 0), and says so.
func (a *App) stepZoom(dir int) {
	s := a.settings
	switch cur := int(math.Round(float64(a.zoom.now) * 100)); {
	case dir == 0:
		s.UIZoom = 0
	case dir > 0:
		i := slices.IndexFunc(settings.UIZooms, func(z int) bool { return z > cur })
		if i < 0 {
			return
		}
		s.UIZoom = settings.UIZooms[i]
	default:
		i := slices.IndexFunc(settings.UIZooms, func(z int) bool { return z >= cur })
		if i == 0 {
			return
		}
		if i < 0 {
			i = len(settings.UIZooms)
		}
		s.UIZoom = settings.UIZooms[i-1]
	}
	if s.UIZoom == a.settings.UIZoom {
		return
	}
	if err := a.ApplySettings(s); err != nil {
		a.toastError(err.Error())
		return
	}
	a.toast(func(m locales.Messages) ui.Toast {
		return ui.Toast{Title: m.ZoomNow(zoomLabel(m, s.UIZoom))}
	})
}

// stepTermFont makes the terminals' font n points larger (smaller when n
// is negative), within the settings' bounds, and says so in one toast
// that each step changes.
func (a *App) stepTermFont(n int) {
	s := a.settings
	s.TerminalFontSize = min(max(s.TerminalFontSize+n, settings.MinFontSize), settings.MaxFontSize)
	if s.TerminalFontSize == a.settings.TerminalFontSize {
		return
	}
	if err := a.ApplySettings(s); err != nil {
		a.toastError(err.Error())
		return
	}
	a.toast(func(m locales.Messages) ui.Toast {
		return ui.Toast{ID: "termfont", Title: m.FontSizeNow(strconv.Itoa(s.TerminalFontSize))}
	})
}

// zoomLabel names a zoom of the settings: "125%", or following the system.
func zoomLabel(m locales.Messages, z int) string {
	if z == 0 {
		return m.ZoomSystem()
	}
	return strconv.Itoa(z) + "%"
}
