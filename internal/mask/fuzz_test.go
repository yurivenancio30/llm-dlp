package mask

import (
	"strings"
	"testing"
)

// Fuzz: normalization keeps the map coherent and the round trip through the readers returns the
// text byte for byte, with any transport around it.
// go test ./internal/mask -run '^$' -fuzz FuzzNormalize -fuzztime 60s
// go test ./internal/mask -run '^$' -fuzz FuzzRoundTrip -fuzztime 60s

func fuzzSeeds(f *testing.F) {
	for _, c := range matrixContents[:3] {
		for _, d := range matrixLayouts[:12] {
			for _, tr := range transports {
				f.Add(tr.f(d.f(c.cols, c.rows)))
			}
		}
	}
	f.Add("\x1b[1m│ a ┆ b │\r\n\\n\\\"x\\u003c\n")
	f.Add("  1→a.py:2:x\n+@@ -1 +1 @@\n> > ```\n")
}

func FuzzNormalize(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, s string) {
		n, ok := normalize(s)
		if !ok {
			return
		}
		if len(n.mp) != len(n.t)+1 {
			t.Fatalf("map with %d positions for %d bytes", len(n.mp), len(n.t))
		}
		for i := 0; i < len(n.t); i++ {
			if p := n.mp[i]; p < 0 || p >= len(s) || i > 0 && p < n.mp[i-1] {
				t.Fatalf("position %d -> %d out of order", i, p)
			}
		}
		for a := 0; a+1 < len(n.t) && a < 200; a += 7 {
			b := min(len(n.t), a+5)
			if oa, ob, ok := n.original(s, a, b); ok && s[oa:ob] != n.t[a:b] {
				t.Fatalf("snippet [%d,%d) does not match the original", a, b)
			}
		}
	})
}

func FuzzRoundTrip(f *testing.F) {
	fuzzSeeds(f)
	m := newTest(&testing.T{})
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 || !strings.ContainsAny(s, "\n,=:|") {
			return
		}
		out, ents := m.Mask(s)
		if back := NewTable(ents).Unmask(out, false); back != s {
			t.Fatalf("the way back does not reproduce the original:\n%q\n%q", s, back)
		}
	})
}

// the manual scan of the SQL reader does the same as the regexes it replaced
func FuzzSQLScan(f *testing.F) {
	for _, s := range []string{"SELECT a.b, [x y].\"z\" FROM `db.t` GROUP  BY c ORDER\nBY d", "café.ação_1 $x #y 1abc", "[]\"\"``[a", "group by groupby ORDERBY"} {
		f.Add(s)
	}
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 2000 {
			return
		}
		eq := func(name string, a [][]int, b [][2]int) {
			if len(a) != len(b) {
				t.Fatalf("%s: %d against %d in %q", name, len(a), len(b), s)
			}
			for i := range a {
				if a[i][0] != b[i][0] || a[i][1] != b[i][1] {
					t.Fatalf("%s: %v against %v in %q", name, a[i], b[i], s)
				}
			}
		}
		eq("partes", reSQLPart.FindAllStringIndex(s, -1), sqlPartsIn(s))
		eq("qualificados", reQualTok.FindAllStringIndex(s, -1), sqlQualsIn(s))
		if a, b := len(reSQLClauses.FindAllStringIndex(s, 4)), countClauses(s, 4); a != b {
			t.Fatalf("clauses: %d against %d in %q", a, b, s)
		}
	})
}
