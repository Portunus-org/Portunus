package core

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text that people type into the panel is data, and is treated as data
// everywhere it goes: it reaches the database through placeholders, the
// sing-box configuration and the client profiles through a JSON encoder,
// links through a URL encoder and pages through React, none of which lets
// it be read as anything else. This file is about what is left: characters
// that are never part of a name and only cause trouble where the name is
// shown, logged or pasted.

// oddText reports whether s holds something no name or note should: bytes
// that are not text, control characters (a NUL, an escape, a line break in
// what is one line) and the invisible marks that reverse the direction of
// the text around them, used to make one name look like another. multiline
// lets line breaks and tabs through, for notes.
func oddText(s string, multiline bool) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			if !multiline {
				return true
			}
		case unicode.IsControl(r):
			return true
		case r == 0x061C, r == 0x200E, r == 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
			return true
		}
	}
	return false
}

// plainText removes from s what oddText objects to: for a name that comes
// from outside and is not typed (the label of an imported link), where
// refusing the whole thing over a stray character would be no help.
func plainText(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if oddText(string(r), false) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, "")))
}
