package app

import (
	"image"
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/settings"
)

func renderBackgroundFixture(a *App) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			if x < 100 {
				img.SetRGBA(x, y, color.RGBA{R: 240, G: 30, B: 20, A: 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{R: 20, G: 30, B: 240, A: 255})
			}
		}
	}
	a.settings.Background = settings.DefaultBackground()
	a.settings.Background.Image = "background-render.png"
	a.background.active, a.background.requested = a.settings.Background.Image, a.settings.Background.Image
	a.background.bmp = ui.NewBitmap(img)
}

// TestBackgroundClearsSurface: a background image leaves the content
// surface and the canvas clear so the image shows, a solid fill otherwise.
func TestBackgroundClearsSurface(t *testing.T) {
	a, _ := newTestApp(t)
	col := ui.Hex("#123456")
	if got := a.contentSurfaceFill(col); got != col {
		t.Errorf("surface without a background = %v", got)
	}
	renderBackgroundFixture(a)
	if got := a.contentSurfaceFill(col); got != ui.Transparent {
		t.Errorf("surface over a background = %v", got)
	}
	if got := a.contentFill(col, true); got != ui.Transparent {
		t.Errorf("canvas over a background = %v", got)
	}
}

func TestBackgroundEditorWindow(t *testing.T) {
	a, tt, _, _ := localPane(t, "background.txt")
	renderBackgroundFixture(a)
	a.settings.Background.ChromeOpacity = 0
	a.settings.Background.ContentOpacity = 0
	wins := fakeWindows(a)
	waitFor(t, tt, "local file", func() bool { return tt.HasText("background.txt") })
	mustClick(t, tt, "background.txt")
	mustClick(t, tt, "background.txt")
	_, et, ec := openedWindow(t, a, tt, wins, 1)
	et.Frame()
	ed := shown(ec)
	if ed.st.Theme == nil || ed.st.Theme.Background.A != 0 {
		t.Fatal("editor default background stayed opaque")
	}
	a.settings.Background.Image = ""
	et.Frame()
	if ed.st.Theme != nil {
		t.Fatal("removing image did not restore default editor theme")
	}
}
