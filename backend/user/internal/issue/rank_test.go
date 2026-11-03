package issue

import (
	"slices"
	"strings"
	"testing"
)

func TestRankBetween(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"", ""}, {"", "V"}, {"V", ""}, {"", "1"}, {"0V", "1"}, {"z", ""}, {"zz", ""}, {"V", "W"}, {"V", "V1"},
		{rankAt(1), rankAt(2)}, {rankAt(9), rankAt(10)}, {"", rankAt(1)}, {rankAt(99), ""},
	} {
		r := rankBetween(c.a, c.b)
		if r <= c.a || c.b != "" && r >= c.b || strings.HasSuffix(r, "0") || strings.Trim(r, rankDigits) != "" {
			t.Errorf("rankBetween(%q, %q) = %q", c.a, c.b, r)
		}
	}
}

// TestRankSameSpot inserts 1,000 times right after the first issue, rebalancing like place does, and checks the
// ranks stay strictly ordered.
func TestRankSameSpot(t *testing.T) {
	ranks := []string{rankAt(1), rankAt(2)}
	rebalances := 0
	for range 1000 {
		r := rankBetween(ranks[0], ranks[1])
		if len(r) > maxRank {
			rebalances++
			for k := range ranks {
				ranks[k] = rankAt(k + 1)
			}
			r = rankBetween(ranks[0], ranks[1])
		}
		ranks = slices.Insert(ranks, 1, r)
		for k := 1; k < len(ranks); k++ {
			if ranks[k-1] >= ranks[k] {
				t.Fatalf("not strictly ordered after %d inserts: %q, %q", len(ranks)-2, ranks[k-1], ranks[k])
			}
		}
	}
	if rebalances == 0 {
		t.Error("never rebalanced")
	}
	// And at the end of the project.
	last := rankAt(3)
	for range 1000 {
		r := rankBetween(last, "")
		if r <= last {
			t.Fatalf("rankBetween(%q, \"\") = %q", last, r)
		}
		if last = r; len(last) > maxRank {
			last = rankAt(1001)
		}
	}
}

// rankAt sorts like its number, as the SQL backfill's lpad(n, 10, '0') || 'V' does.
func TestRankAt(t *testing.T) {
	if got := rankAt(42); got != "0000000042V" {
		t.Errorf("rankAt(42) = %q", got)
	}
	var rs []string
	for n := 1; n <= 1200; n++ {
		rs = append(rs, rankAt(n))
	}
	if !slices.IsSorted(rs) {
		t.Error("rankAt isn't ordered")
	}
}
