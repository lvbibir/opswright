package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/locales"
)

func TestQuickCommands(t *testing.T) {
	srv, shell := startScript(t, "\x1b[?2004hready")
	a, tt := newTestApp(t)
	m := locales.In("en")
	cn := connected(t, a, tt, srv)
	waitFor(t, tt, "bracketed paste", func() bool { return cn.s.Term.Modes().BracketedPaste })
	if _, err := a.saveSnippet(assets.Snippet{Name: "restart", SendEnter: true}, "echo restart"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.saveSnippet(assets.Snippet{Name: "deploy"}, "echo deploy"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// Ctrl+M opens it and closes it again.
	tt.Key(ui.Ctrl, ui.KeyM)
	tt.Frame()
	tt.Frame()
	if !a.quickCmd.open {
		t.Fatal("Ctrl+M did not open quick commands")
	}
	tt.Key(ui.Ctrl, ui.KeyM)
	tt.Frame()
	if a.quickCmd.open {
		t.Fatal("Ctrl+M does not close it")
	}

	// The search narrows the list, and Enter runs the entry's own mode:
	// an insert-only entry is written without a newline.
	tt.Key(ui.Ctrl, ui.KeyM)
	tt.Frame()
	shell.take()
	tt.Type("deploy")
	tt.Frame()
	if a.quickCmd.query != "deploy" || len(a.snippetHits(m, a.store.Snippets(), "deploy")) != 1 {
		t.Fatalf("query %q", a.quickCmd.query)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	inserted, wantInsert := "", "\x1b[200~echo deploy\x1b[201~"
	waitFor(t, tt, "insert", func() bool { inserted += shell.take(); return len(inserted) >= len(wantInsert) })
	if inserted != wantInsert || a.quickCmd.open {
		t.Fatalf("inserted %q, open %v", inserted, a.quickCmd.open)
	}
	waitFor(t, tt, "insert done", func() bool { return !cn.tv.snippetSending })

	// An entry that sends appends Enter, as its mode asks.
	tt.Key(ui.Ctrl, ui.KeyM)
	tt.Frame()
	tt.Type("restart")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	sent, wantSend := "", "\x1b[200~echo restart\x1b[201~\r"
	waitFor(t, tt, "send", func() bool { sent += shell.take(); return len(sent) >= len(wantSend) })
	if sent != wantSend {
		t.Fatalf("sent %q", sent)
	}
	waitFor(t, tt, "send done", func() bool { return !cn.tv.snippetSending })

	// Up and Down choose among the matches, and Esc closes without running.
	tt.Key(ui.Ctrl, ui.KeyM)
	tt.Frame()
	tt.Type("echo")
	tt.Frame()
	if n := len(a.snippetHits(m, a.store.Snippets(), "echo")); n != 2 {
		t.Fatalf("hits %d", n)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.quickCmd.sel != 1 {
		t.Fatalf("Down chose %d", a.quickCmd.sel)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if a.quickCmd.sel != 0 {
		t.Fatalf("Up chose %d", a.quickCmd.sel)
	}
	shell.take()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.quickCmd.open {
		t.Fatal("Esc does not close it")
	}
	if got := shell.take(); got != "" {
		t.Fatalf("Esc ran something: %q", got)
	}
}

func TestQuickCommandsNoTerminal(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Key(ui.Ctrl, ui.KeyM)
	tt.Frame()
	if !a.quickCmd.open || len(a.store.Snippets()) != 0 {
		t.Fatalf("open %v, snippets %d", a.quickCmd.open, len(a.store.Snippets()))
	}
	if _, err := a.saveSnippet(assets.Snippet{Name: "deploy", SendEnter: true}, "echo deploy"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.snippetTarget() != nil {
		t.Fatal("a target without a terminal")
	}
	// Enter with no focused terminal runs nothing and closes the window.
	tt.Type("deploy")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.quickCmd.open {
		t.Fatal("Enter does not close it")
	}
}
