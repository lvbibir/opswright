package app

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/lvbibir/opswright/internal/layout"
	"github.com/lvbibir/opswright/internal/settings"
	"github.com/lvbibir/opswright/locales"
)

// TestRailRows: the rail shows every item by default; hiding items drops
// them, a group left empty takes its separator with it, at the start, in
// the middle or at the end, and an ID naming no item changes nothing.
func TestRailRows(t *testing.T) {
	seen := map[string]bool{}
	for _, it := range railItems {
		if it.id == "" || it.id == railSep || seen[it.id] {
			t.Fatalf("item ID %q", it.id)
		}
		seen[it.id] = true
	}
	all := []string{"host", "ai", "monitor", "usage", railSep, "files", "docker", "forward", railSep, "logs", "snippets", "history", "broadcast"}
	for _, tc := range []struct {
		name   string
		hidden []string
		top    []string
	}{
		{"none", nil, all},
		{"unknown", []string{"later", ""}, all},
		{"some", []string{"usage"},
			[]string{"host", "ai", "monitor", railSep, "files", "docker", "forward", railSep, "logs", "snippets", "history", "broadcast"}},
		{"first group", []string{"host", "ai", "monitor", "usage"},
			[]string{"files", "docker", "forward", railSep, "logs", "snippets", "history", "broadcast"}},
		{"middle group", []string{"files", "docker", "forward"},
			[]string{"host", "ai", "monitor", "usage", railSep, "logs", "snippets", "history", "broadcast"}},
		{"last group", []string{"logs", "snippets", "history", "broadcast"},
			[]string{"host", "ai", "monitor", "usage", railSep, "files", "docker", "forward"}},
		{"one left", []string{"host", "ai", "monitor", "files", "docker", "forward", "logs", "snippets", "history", "broadcast"},
			[]string{"usage"}},
	} {
		top := railRows(tc.hidden)
		if !slices.Equal(top, tc.top) {
			t.Errorf("%s: top %q", tc.name, top)
		}
	}
}

// TestRailHide: hiding an item from its menu saves the settings and
// leaves the pane it opened open; the tool still opens and closes the
// other ways, and showing it again takes it off the list.
func TestRailHide(t *testing.T) {
	a, _ := newTestApp(t)
	m := locales.In("en")
	h := savedHost(t, a, "saved", "127.0.0.1")
	term, err := a.makeConn(h.ID, layout.Terminal, 0)
	if err != nil {
		t.Fatal(err)
	}
	a.ws.Open(term.item())
	a.ToggleTool(layout.Docker)
	entries := a.railEntries(m, "docker")
	if got := menuLabels(entries); !slices.Equal(got, []string{m.RailHide(), "-", m.RailCustomize()}) {
		t.Fatalf("menu %q", got)
	}
	entries[0].Do()
	if a.Settings().RailShown("docker") || !a.toolHere(layout.Docker) {
		t.Fatalf("hidden %q, docker open %v", a.Settings().RailHidden, a.toolHere(layout.Docker))
	}
	saved, err := settings.Load(filepath.Join(a.p.DataDir, "settings.json"))
	if err != nil || !slices.Equal(saved.RailHidden, []string{"docker"}) {
		t.Fatalf("saved %q %v", saved.RailHidden, err)
	}
	a.ToggleTool(layout.Docker)
	if a.toolHere(layout.Docker) {
		t.Fatal("a hidden tool no longer closes")
	}
	a.ToggleTool(layout.Docker)
	if !a.toolHere(layout.Docker) {
		t.Fatal("a hidden tool no longer opens")
	}
	a.setRailShown("docker", true)
	if a.Settings().RailHidden != nil {
		t.Fatalf("shown again %q", a.Settings().RailHidden)
	}
	// The rail's own menu only opens the settings, at the items.
	entries = a.railEntries(m, "")
	if got := menuLabels(entries); !slices.Equal(got, []string{m.RailCustomize()}) {
		t.Fatalf("rail menu %q", got)
	}
	entries[0].Do()
	if a.prefNow() != prefGeneral || a.prefSearch.row != m.RailSection() {
		t.Fatalf("opened %v at %q", a.prefNow(), a.prefSearch.row)
	}
}
