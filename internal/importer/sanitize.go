package importer

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxNameBytes = 200

// SanitizeName makes name safe to use as a single path segment on common
// filesystems.
func SanitizeName(name string) string {
	s := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		if unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	s = strings.ToValidUTF8(s, "_")
	s = strings.Trim(s, " .")
	if len(s) > maxNameBytes {
		cut := maxNameBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = strings.Trim(s[:cut], " .")
	}
	if s == "" {
		return "untitled"
	}
	return s
}
