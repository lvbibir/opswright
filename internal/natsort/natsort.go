// Package natsort orders strings the way people read them: runs of
// digits are compared as numbers, so "host2" comes before "host10" and
// "1.1.1.2" before "1.1.1.10", where a plain byte-wise compare would put
// "1.1.1.10" first.
package natsort

import (
	"cmp"
	"strings"
)

// Compare returns -1, 0 or 1 as a sorts before, with or after b, with
// runs of digits compared as numbers. Runs of equal value are then
// ordered by their leading zeros, so "host1" comes before "host01".
// Other bytes compare as themselves.
func Compare(a, b string) int {
	for a != "" && b != "" {
		if isDigit(a[0]) && isDigit(b[0]) {
			da, ra := leadingDigits(a)
			db, rb := leadingDigits(b)
			na, nb := strings.TrimLeft(da, "0"), strings.TrimLeft(db, "0")
			if c := cmp.Or(cmp.Compare(len(na), len(nb)), strings.Compare(na, nb), cmp.Compare(len(da), len(db))); c != 0 {
				return c
			}
			a, b = ra, rb
			continue
		}
		if a[0] != b[0] {
			return cmp.Compare(a[0], b[0])
		}
		a, b = a[1:], b[1:]
	}
	return cmp.Compare(len(a), len(b))
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func leadingDigits(s string) (digits, rest string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}
