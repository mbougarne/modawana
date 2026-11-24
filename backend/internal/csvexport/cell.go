// Package csvexport handles cells from user-supplied text in spreadsheet exports.
package csvexport

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Literal prevents formula evaluation in spreadsheet applications.
func Literal(value string) string {
	candidate := strings.TrimLeftFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == '\ufeff'
	})
	first, _ := utf8.DecodeRuneInString(candidate)
	if strings.ContainsRune("=+-@＝＋－＠", first) {
		return "'" + value
	}
	return value
}
