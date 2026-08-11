package palette

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/registry"
)

func TestScoreSubsequence(t *testing.T) {
	cases := []struct {
		haystack, needle string
		want             bool
	}{
		{"cancel", "", true},
		{"cancel", "cancel", true},
		{"cancel", "can", true},
		{"cancel", "cnl", true},  // subsequence, not substring
		{"cancel", "cln", false}, // out of order
		{"cancel", "cancels", false},
		{"cancel", "z", false},
		{"", "a", false},
		{"", "", true},
	}
	for _, c := range cases {
		if _, ok := score(c.haystack, c.needle); ok != c.want {
			t.Errorf("score(%q, %q) matched = %v, want %v", c.haystack, c.needle, ok, c.want)
		}
	}
}

func TestScorePrefersEarlyAndPrefix(t *testing.T) {
	prefix, ok := score("cancel", "can")
	if !ok {
		t.Fatal("cancel should match can")
	}
	scattered, ok := score("clean and nudge", "can")
	if !ok {
		t.Fatal("clean and nudge should match can")
	}
	if prefix >= scattered {
		t.Errorf("prefix match scored %d, scattered scored %d: lower must be better and prefix must win", prefix, scattered)
	}
	if prefix >= 0 {
		t.Errorf("a leading match must take the flat bonus and go negative: got %d", prefix)
	}
	if noBonus, _ := score("uncancel", "can"); noBonus <= prefix+prefixBonus-1 {
		t.Errorf("the bonus is not flat: prefix %d, non-prefix %d", prefix, noBonus)
	}
}

// TestScoreAgreesWithRegistry pins this package's restated scorer against the
// registry's own over the registry's own catalog. The two must never disagree
// about which row best answers a query, or the ctrl+k palette and the slash
// filter would rank the same catalog differently and 5.22's "one registry, six
// surfaces" would be a claim rather than a fact.
func TestScoreAgreesWithRegistry(t *testing.T) {
	for _, query := range []string{"", "c", "can", "model", "task", "open", "zzz"} {
		want := registry.FuzzyMatch(registry.ScopeAny, query)
		needle := lower(query)
		got := make([]registry.Match, 0, len(want))
		for _, e := range registry.All() {
			verbScore, verbOK := score(lower(e.Verb), needle)
			descScore, descOK := score(lower(e.Description), needle)
			switch {
			case verbOK && descOK:
				got = append(got, registry.Match{Entry: e, Score: min(verbScore, descScore)})
			case verbOK:
				got = append(got, registry.Match{Entry: e, Score: verbScore})
			case descOK:
				got = append(got, registry.Match{Entry: e, Score: descScore})
			}
		}
		if len(got) != len(want) {
			t.Fatalf("query %q: matched %d entries, registry matched %d", query, len(got), len(want))
		}
		byID := make(map[string]int, len(got))
		for _, m := range got {
			byID[m.Entry.ID] = m.Score
		}
		for _, m := range want {
			if s, ok := byID[m.Entry.ID]; !ok || s != m.Score {
				t.Errorf("query %q: entry %s scored %d here, %d in the registry", query, m.Entry.ID, s, m.Score)
			}
		}
	}
}

func TestAppendPositions(t *testing.T) {
	cases := []struct {
		haystack, needle string
		want             []int32
	}{
		{"cancel", "can", []int32{0, 1, 2}},
		{"cancel", "cnl", []int32{0, 2, 5}},
		{"cancel", "", nil},
		{"cancel", "zz", nil},
		{"open settings", "set", []int32{5, 6, 7}},
		// A partial match reports what it found, which is all the highlighter
		// can honestly draw; the row will not be in the filtered set anyway.
		{"cancel", "cax", []int32{0, 1}},
	}
	for _, c := range cases {
		got := appendPositions(nil, c.haystack, c.needle)
		if len(got) != len(c.want) {
			t.Errorf("positions(%q, %q) = %v, want %v", c.haystack, c.needle, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("positions(%q, %q) = %v, want %v", c.haystack, c.needle, got, c.want)
				break
			}
		}
	}
}

// TestAppendPositionsNeverSplitsARune is the guard that keeps a highlight from
// becoming mojibake: every reported offset must start a rune.
func TestAppendPositionsNeverSplitsARune(t *testing.T) {
	haystacks := []string{"café résumé", "日本語 task", "naïve cancel", "\xff\xfe broken"}
	needles := []string{"c", "a", "é", "本", "\xff", "task"}
	for _, h := range haystacks {
		for _, n := range needles {
			for _, p := range appendPositions(nil, h, lower(n)) {
				if int(p) >= len(h) {
					t.Fatalf("position %d out of range for %q", p, h)
				}
				b := h[p]
				if b >= 0x80 && b < 0xC0 {
					t.Errorf("position %d in %q lands on a continuation byte", p, h)
				}
			}
		}
	}
}

func TestLowerReturnsInputUnallocatedWhenAlreadyLower(t *testing.T) {
	in := "cancel this task"
	if out := lower(in); out != in {
		t.Fatalf("lower(%q) = %q", in, out)
	}
	if got, want := lower("CanCEL"), "cancel"; got != want {
		t.Errorf("lower = %q, want %q", got, want)
	}
	// Length is preserved, which is what lets a match position computed on the
	// lowercase form index the original string.
	for _, s := range []string{"ABC", "café RÉSUMÉ", "日本語"} {
		if len(lower(s)) != len(s) {
			t.Errorf("lower(%q) changed length", s)
		}
	}
}

func TestLowerDoesNotAllocateForLowercaseInput(t *testing.T) {
	s := strings.Repeat("cancel ", 32)
	if n := testing.AllocsPerRun(100, func() { _ = lower(s) }); n != 0 {
		t.Errorf("lower allocated %v times for an already-lowercase string", n)
	}
}

// TestFilterIsAllocationLean is the production bar for the path that runs on
// every keystroke: re-ranking a whole catalog into a slice that has already
// grown must not touch the heap.
func TestFilterIsAllocationLean(t *testing.T) {
	rows := buildRows(nil, demoCatalog())
	hits := filter(nil, rows, "can")
	if len(hits) == 0 {
		t.Fatal("expected some matches to size the buffer with")
	}
	if n := testing.AllocsPerRun(200, func() { hits = filter(hits, rows, "can") }); n != 0 {
		t.Errorf("filter allocated %v times per keystroke", n)
	}
}

func TestFilterKeepsSectionOrderAndRanksWithinIt(t *testing.T) {
	rows := buildRows(nil, demoCatalog())
	hits := filter(nil, rows, "")
	if len(hits) != len(rows) {
		t.Fatalf("an empty query filtered %d of %d rows away", len(rows)-len(hits), len(rows))
	}
	// Empty query, stable sort: the order is exactly the built order.
	for i := range hits {
		if int(hits[i].idx) != i {
			t.Fatalf("empty query reordered row %d to position %d", hits[i].idx, i)
		}
	}
	hits = filter(hits, rows, "can")
	prevSec, prevScore := section(0), int32(-1<<30)
	for _, h := range hits {
		sec := rows[h.idx].sec
		if sec < prevSec {
			t.Fatalf("section %d came after section %d", sec, prevSec)
		}
		if sec != prevSec {
			prevSec, prevScore = sec, h.score
			continue
		}
		if h.score < prevScore {
			t.Fatalf("score %d came after %d inside section %d", h.score, prevScore, sec)
		}
		prevScore = h.score
	}
}
