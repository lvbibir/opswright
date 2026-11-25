package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTrayPreferencesDefaults(t *testing.T) {
	assertDefaults := func(s Settings) {
		t.Helper()
		if s.TrayClick != TrayClickPanel || s.TrayHeight != DefaultTrayHeight {
			t.Fatalf("tray defaults: click=%q height=%d", s.TrayClick, s.TrayHeight)
		}
	}
	assertDefaults(Default())
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, data := range []string{"", `{}`, `{"trayClick":"invalid","trayHeight":-1}`} {
		if data != "" {
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		s, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		assertDefaults(s)
	}
}

func TestTrayHeightNormalization(t *testing.T) {
	for _, tc := range []struct{ input, want int }{
		{-1, DefaultTrayHeight}, {0, DefaultTrayHeight}, {1, MinTrayHeight},
		{MinTrayHeight, MinTrayHeight}, {700, 700}, {MaxTrayHeight, MaxTrayHeight}, {MaxTrayHeight + 1, MaxTrayHeight},
	} {
		t.Run(fmt.Sprint(tc.input), func(t *testing.T) {
			s := Default()
			s.TrayHeight = tc.input
			s.Normalize()
			if s.TrayHeight != tc.want {
				t.Fatalf("height %d normalized to %d, want %d", tc.input, s.TrayHeight, tc.want)
			}
		})
	}
}

func TestTrayPreferencesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, click := range []string{TrayClickPanel, TrayClickWindow} {
		s := Default()
		s.TrayClick, s.TrayHeight = click, 700
		if err := Save(path, s); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if got.TrayClick != click || got.TrayHeight != 700 {
			t.Fatalf("tray preferences did not survive reload: click=%q height=%d", got.TrayClick, got.TrayHeight)
		}
	}
}
