// Package textrule holds the text rules the standard library doesn't spell,
// so every step reads the same text the same way: the regexp classes for
// whitespace, digits and word characters, and slicing by code point.
//
// Whitespace is unicode.IsSpace's, which strings.TrimSpace and strings.Fields
// use too, and a letter, number or upper case letter is the unicode
// package's. Go's regexp spells `\s`, `\d` and `\w` as ASCII classes, but a
// source's no-break spaces, its non-ASCII digits and letters such as ē must
// match, so patterns spell them with Space, Digit and Word instead.
package textrule

import (
	"unicode"
	"unicode/utf8"
)

// Space is a regexp character class body matching the characters
// unicode.IsSpace is true of. Use it inside brackets: `[` + Space + `]`.
const Space = `\t\n\v\f\r\x{85}\p{Z}`

// Digit is a regexp class matching any decimal digit.
const Digit = `\p{Nd}`

// Word is a regexp character class body matching a word character: a
// letter, a number or the underscore. Use it inside brackets:
// `[` + Word + `]`.
const Word = `\p{L}\p{N}_`

// IsAlnum is true of a letter or a number.
func IsAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }

// IsWord is true of the characters Word matches.
func IsWord(r rune) bool { return r == '_' || IsAlnum(r) }

// Head is the first n code points of s, or all of s if it has fewer.
func Head(s string, n int) string {
	i := 0
	for k := 0; k < n; k++ {
		if i >= len(s) {
			return s
		}
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
	}
	return s[:i]
}

// Tail is the last n code points of s, or all of s if it has fewer.
func Tail(s string, n int) string {
	i := len(s)
	for k := 0; k < n && i > 0; k++ {
		_, w := utf8.DecodeLastRuneInString(s[:i])
		i -= w
	}
	return s[i:]
}
