// Package keymap is a keyboard shortcut as Opswright's settings keep it:
// its modifiers and key, written "super+shift+p".
package keymap

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// None is what a shortcut with no keys is written as: an action whose
// shortcut is cleared runs on no key until another is recorded.
const None = "none"

// Combo is a shortcut: modifiers held and a key pressed.
type Combo struct {
	Mods ui.Modifiers
	Key  ui.Key
}

// Unbound reports a shortcut with no keys at all: the zero Combo, which
// None writes and reads back, and no action runs on.
func (c Combo) Unbound() bool { return c == Combo{} }

// mods are the modifiers in the order they are written.
var mods = []struct {
	mod  ui.Modifiers
	name string
}{{ui.Ctrl, "ctrl"}, {ui.Alt, "alt"}, {ui.Shift, "shift"}, {ui.Super, "super"}}

// names are the keys a shortcut may use: letters, digits, punctuation,
// Tab and the function keys. The others edit text or move in it.
var names = func() map[ui.Key]string {
	n := map[ui.Key]string{
		ui.KeyTab:   "tab",
		ui.KeyMinus: "minus", ui.KeyEqual: "equal", ui.KeyComma: "comma", ui.KeyPeriod: "period",
		ui.KeySlash: "slash", ui.KeySemicolon: "semicolon", ui.KeyQuote: "quote",
		ui.KeyBracketLeft: "bracketleft", ui.KeyBracketRight: "bracketright",
		ui.KeyBackslash: "backslash", ui.KeyBackquote: "backquote",
	}
	for k := ui.KeyA; k <= ui.KeyZ; k++ {
		n[k] = string(rune('a' + k - ui.KeyA))
	}
	for k := ui.Key0; k <= ui.Key9; k++ {
		n[k] = string(rune('0' + k - ui.Key0))
	}
	for k := ui.KeyF1; k <= ui.KeyF12; k++ {
		n[k] = "f" + strconv.Itoa(int(k-ui.KeyF1)+1)
	}
	return n
}()

// Bindable reports a key a shortcut may use.
func Bindable(k ui.Key) bool {
	_, ok := names[k]
	return ok
}

// String writes c as the settings keep it, "ctrl+shift+p"; a shortcut
// with no keys writes None.
func (c Combo) String() string {
	if c.Unbound() {
		return None
	}
	var parts []string
	for _, m := range mods {
		if c.Mods&m.mod != 0 {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, names[c.Key]), "+")
}

// Parse reads what String writes.
func Parse(s string) (Combo, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == None {
		return Combo{}, true
	}
	parts := strings.Split(s, "+")
	var c Combo
	for _, p := range parts[:len(parts)-1] {
		found := false
		for _, m := range mods {
			if p == m.name && c.Mods&m.mod == 0 {
				c.Mods, found = c.Mods|m.mod, true
			}
		}
		if !found {
			return Combo{}, false
		}
	}
	last := parts[len(parts)-1]
	for k, n := range names {
		if n == last {
			c.Key = k
			return c, true
		}
	}
	return Combo{}, false
}
