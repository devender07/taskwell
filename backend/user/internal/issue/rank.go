package issue

import (
	"fmt"
	"strings"
)

// Ranks order a project's issues by hand: base-62 fractions written in digits, ascending in byte (C collation) order,
// never ending in 0, so there's always room for another between two of them.
const rankDigits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// maxRank is the longest rank kept; a longer one rebalances its project first (see place).
const maxRank = 50

// rankBetween returns a rank strictly between a and b; a "" is before everything, b "" after everything.
// Panics unless a < b.
func rankBetween(a, b string) string {
	if b != "" && a >= b {
		panic(fmt.Sprintf("rankBetween(%q, %q)", a, b))
	}
	digit := func(s string, i int) int {
		if i >= len(s) {
			return 0
		}
		return strings.IndexByte(rankDigits, s[i])
	}
	var out []byte
	for i := 0; ; i++ {
		lo, hi := digit(a, i), len(rankDigits)
		if b != "" {
			hi = digit(b, i)
		}
		switch {
		case lo == hi: // a common prefix digit
			out = append(out, rankDigits[lo])
		case hi-lo > 1:
			return string(append(out, rankDigits[(lo+hi)/2]))
		default: // adjacent digits: keep a's and go past the rest of a
			rest := ""
			if i < len(a) {
				rest = a[i+1:]
			}
			return string(append(out, rankDigits[lo])) + rankBetween(rest, "")
		}
	}
}

// rankAt is the nth (from 1) of evenly spaced ranks: the migration's backfill and a rebalance write these.
func rankAt(n int) string { return fmt.Sprintf("%010dV", n) }
