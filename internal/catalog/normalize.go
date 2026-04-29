package catalog

import (
	"strings"
	"unicode"
)

// NormalizeTitle lowercases, strips non-alphanumeric characters (except spaces),
// and collapses multiple spaces into one.
func NormalizeTitle(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	lastWasSpace := true
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(unicode.ToLower(r))
			lastWasSpace = false
		} else if !lastWasSpace {
			b.WriteRune(' ')
			lastWasSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}
