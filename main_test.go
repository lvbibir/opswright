package main

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/lvbibir/sshyard/internal/theme"
)

func TestThemeSource(t *testing.T) {
	for m, want := range map[theme.Mode]mygo.ThemeSource{theme.Auto: mygo.ThemeSystem, theme.Light: mygo.ThemeLight, theme.Dark: mygo.ThemeDark} {
		if got := themeSource(m); got != want {
			t.Errorf("themeSource(%q) = %q, want %q", m, got, want)
		}
	}
}
