package assets

import (
	"strings"
	"unicode/utf8"
)

// A host's terminals run su - as their shell starts (Host.TerminalSu),
// and type its password when they see the prompt the shell prints
// (Host.TerminalSuPrompt). The password is a host-owned secret
// (keychain.HostElevate), kept in the encrypted store like a snippet's
// body.

// DefaultSuPrompt is the prompt Opswright waits for when a host sets
// none. Linux-PAM prints it (pam_get_authtok.c), and it is localizable,
// so a host may set its own.
const DefaultSuPrompt = "Password: "

// suPromptMaxRunes bounds a host's own prompt: it is matched against the
// terminal's output, so a long one is a mistake.
const suPromptMaxRunes = 64

// suPasswordMaxBytes bounds a host's su password: it is typed at a
// prompt, so a long one is a mistake.
const suPasswordMaxBytes = 1 << 10

// SuPrompt is the prompt a host's terminals wait for before typing its su
// password: Host.TerminalSuPrompt, or DefaultSuPrompt when it is empty.
func (h Host) SuPrompt() string {
	if h.TerminalSuPrompt == "" {
		return DefaultSuPrompt
	}
	return h.TerminalSuPrompt
}

// ValidateSuPassword accepts a password to be typed at a shell's prompt:
// at most 1 KiB of UTF-8, and no CR or LF, either of which would end the
// line early, nor any other C0 control but TAB. Errors never contain the
// text.
func ValidateSuPassword(text string) error {
	switch {
	case !utf8.ValidString(text):
		return &FieldError{"elevate", "invalid"}
	case len(text) > suPasswordMaxBytes:
		return &FieldError{"elevate", "tooLong"}
	case strings.ContainsFunc(text, func(r rune) bool { return r == 0x7f || r < 0x20 && r != '\t' }):
		return &FieldError{"elevate", "invalid"}
	}
	return nil
}

// ValidateSuPrompt accepts a host's own prompt: at most 64 runes of UTF-8
// and no control characters. An empty prompt is the default, always
// valid. Errors never contain the text.
func ValidateSuPrompt(text string) error {
	switch {
	case text == "":
		return nil
	case !utf8.ValidString(text) || strings.ContainsFunc(text, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		return &FieldError{"elevatePrompt", "invalid"}
	case utf8.RuneCountInString(text) > suPromptMaxRunes:
		return &FieldError{"elevatePrompt", "tooLong"}
	}
	return nil
}
