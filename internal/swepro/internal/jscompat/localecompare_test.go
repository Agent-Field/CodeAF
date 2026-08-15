package jscompat

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Replays testdata/localecompare.json, produced by
// tools/fixtures/gen-localecompare.ts against Bun 1.2.23's
// String.prototype.localeCompare. There is no codeaf function in the loop:
// the runtime IS the oracle, so the fixture records raw comparator output.
//
// Four gates:
//
//	TestPairwise  — every ordered pair of the adversarial corpus.
//	TestOrders    — the exhaustive ASCII/identifier/randomized sweeps, checked
//	                via (sorted, eq) rather than pairwise because the corpora
//	                are 9k-50k strings. For two TRANSITIVE comparators,
//	                agreeing on the sorted sequence and on which adjacent
//	                elements are collation-equal is equivalent to agreeing on
//	                every pair, and both comparators are sort-key based.
//	TestSorts     — toSorted() output, including tie stability.
//	TestKnownDivergences — pairs where golang.org/x/text's frozen Unicode
//	                6.2.0 / CLDR 23 tables disagree with Bun's modern ICU.
//	                Asserted to STILL disagree: if one starts matching, the
//	                tables moved and the coverage note in localecompare.go is
//	                stale.

type lcFixture struct {
	Meta struct {
		Runtime         string          `json:"runtime"`
		ResolvedLocale  string          `json:"resolvedLocale"`
		CollatorOptions json.RawMessage `json:"collatorOptions"`
		Lang            *string         `json:"lang"`
	} `json:"meta"`
	Pairwise struct {
		Corpus []string `json:"corpus"`
		Signs  string   `json:"signs"`
	} `json:"pairwise"`
	Orders []struct {
		Name   string   `json:"name"`
		Sorted []string `json:"sorted"`
		Eq     string   `json:"eq"`
	} `json:"orders"`
	Sorts []struct {
		Name string   `json:"name"`
		In   []string `json:"in"`
		Out  []string `json:"out"`
	} `json:"sorts"`
	Divergent []struct {
		A    string `json:"a"`
		B    string `json:"b"`
		JS   int    `json:"js"`
		Note string `json:"note"`
	} `json:"divergent"`
}

var (
	lcOnce sync.Once
	lcFx   lcFixture
)

func loadLC(t *testing.T) *lcFixture {
	t.Helper()
	lcOnce.Do(func() {
		b, err := os.ReadFile("testdata/localecompare.json")
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		if err := json.Unmarshal(b, &lcFx); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
	})
	return &lcFx
}

// sign renders a comparison the way the fixture encodes it.
func sign(r int) byte {
	switch {
	case r < 0:
		return '<'
	case r > 0:
		return '>'
	default:
		return '='
	}
}

func TestLocaleCompareFixtureMeta(t *testing.T) {
	fx := loadLC(t)
	// The Go side is language.Und; that is only correct because JSC resolves
	// to en-US and CLDR gives `en` no collation tailorings.
	if fx.Meta.ResolvedLocale != "en-US" {
		t.Fatalf("fixture recorded locale %q, localecompare.go assumes en-US", fx.Meta.ResolvedLocale)
	}
	var opts map[string]any
	if err := json.Unmarshal(fx.Meta.CollatorOptions, &opts); err != nil {
		t.Fatalf("decode collator options: %v", err)
	}
	for k, want := range map[string]any{
		"usage":             "sort",
		"sensitivity":       "variant",
		"ignorePunctuation": false,
		"collation":         "default",
		"numeric":           false,
		"caseFirst":         "false",
	} {
		if got := opts[k]; got != want {
			t.Errorf("collator option %s = %v, want %v", k, got, want)
		}
	}
}

func TestLocaleCompareFixturePairwise(t *testing.T) {
	fx := loadLC(t)
	c := fx.Pairwise.Corpus
	n := len(c)
	if want := n * (n - 1) / 2; want != len(fx.Pairwise.Signs) {
		t.Fatalf("fixture has %d pairs for a corpus of %d (want %d)", len(fx.Pairwise.Signs), n, want)
	}
	k, bad := 0, 0
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			got := sign(LocaleCompare(c[i], c[j]))
			if want := fx.Pairwise.Signs[k]; got != want {
				bad++
				if bad <= 20 {
					t.Errorf("LocaleCompare(%q, %q) = %c, want %c", c[i], c[j], got, want)
				}
			}
			// The comparator must be antisymmetric, which JS's is and which
			// sort.SliceStable relies on.
			if rev := LocaleCompare(c[j], c[i]); rev != -LocaleCompare(c[i], c[j]) {
				t.Fatalf("LocaleCompare not antisymmetric on (%q, %q)", c[i], c[j])
			}
			k++
		}
	}
	if bad > 20 {
		t.Errorf("... and %d more pairwise mismatches", bad-20)
	}
	t.Logf("corpus=%d pairs=%d", n, k)
}

func TestLocaleCompareFixtureOrders(t *testing.T) {
	fx := loadLC(t)
	for _, o := range fx.Orders {
		o := o
		t.Run(o.Name, func(t *testing.T) {
			if len(o.Eq) != len(o.Sorted)-1 {
				t.Fatalf("eq has %d flags for %d strings", len(o.Eq), len(o.Sorted))
			}
			bad := 0
			for i := 0; i+1 < len(o.Sorted); i++ {
				r := LocaleCompare(o.Sorted[i], o.Sorted[i+1])
				wantEq := o.Eq[i] == '1'
				if r > 0 || (r == 0) != wantEq {
					bad++
					if bad <= 10 {
						t.Errorf("adjacent %d: LocaleCompare(%q, %q) = %d, JS says %s",
							i, o.Sorted[i], o.Sorted[i+1], r,
							map[bool]string{true: "equal", false: "strictly less"}[wantEq])
					}
				}
			}
			if bad > 10 {
				t.Errorf("... and %d more adjacency mismatches", bad-10)
			}
			// Sorting an already-sorted sequence must be a no-op — this is
			// the stability half of the check, and it is unambiguous even
			// where collation-equal runs exist.
			got := LocaleSortStrings(o.Sorted)
			for i := range got {
				if got[i] != o.Sorted[i] {
					t.Fatalf("LocaleSortStrings is not idempotent at %d: got %q want %q", i, got[i], o.Sorted[i])
				}
			}
			t.Logf("n=%d", len(o.Sorted))
		})
	}
}

func TestLocaleSortStringsFixture(t *testing.T) {
	fx := loadLC(t)
	for _, sc := range fx.Sorts {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			in := make([]string, len(sc.In))
			copy(in, sc.In)
			got := LocaleSortStrings(in)
			gotJSON, err := Stringify(got)
			if err != nil {
				t.Fatalf("stringify got: %v", err)
			}
			wantJSON, err := Stringify(sc.Out)
			if err != nil {
				t.Fatalf("stringify want: %v", err)
			}
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("LocaleSortStrings mismatch\n got: %s\nwant: %s", gotJSON, wantJSON)
			}
			// toSorted() does not mutate its receiver.
			for i := range in {
				if in[i] != sc.In[i] {
					t.Fatalf("LocaleSortStrings mutated its input at %d: %q != %q", i, in[i], sc.In[i])
				}
			}
		})
	}
}

func TestLocaleCompareKnownDivergences(t *testing.T) {
	fx := loadLC(t)
	if len(fx.Divergent) == 0 {
		t.Fatal("fixture recorded no divergences; expected the Unicode 6.2 / CLDR 23 boundary")
	}
	for _, d := range fx.Divergent {
		got := LocaleCompare(d.A, d.B)
		if got == d.JS {
			t.Errorf("LocaleCompare(%q, %q) now AGREES with JS (%d) — golang.org/x/text's "+
				"collation tables moved. Re-run the coverage survey and rewrite the "+
				"boundary note in localecompare.go, then drop this entry.\nnote: %s",
				d.A, d.B, got, d.Note)
		}
	}
	t.Logf("%d divergent pairs still divergent", len(fx.Divergent))
}

// ── properties the fixtures cannot express ────────────────────────────────

func TestLocaleCompareIsTotalPreorder(t *testing.T) {
	fx := loadLC(t)
	// Transitivity of the derived strict order over a slice of the corpus.
	// O(n^3), so a slice — the pairwise gate already covers agreement.
	c := fx.Pairwise.Corpus
	if len(c) > 120 {
		c = c[:120]
	}
	for i := range c {
		if r := LocaleCompare(c[i], c[i]); r != 0 {
			t.Fatalf("LocaleCompare(%q, %q) = %d, want 0 (reflexivity)", c[i], c[i], r)
		}
		for j := range c {
			for k := range c {
				ij, jk, ik := LocaleCompare(c[i], c[j]), LocaleCompare(c[j], c[k]), LocaleCompare(c[i], c[k])
				if ij < 0 && jk < 0 && ik >= 0 {
					t.Fatalf("not transitive: %q < %q < %q but cmp(a,c) = %d", c[i], c[j], c[k], ik)
				}
				if ij == 0 && jk == 0 && ik != 0 {
					t.Fatalf("equality not transitive: %q == %q == %q but cmp(a,c) = %d", c[i], c[j], c[k], ik)
				}
			}
		}
	}
}

func TestLocaleCompareReturnsMinusOneZeroOne(t *testing.T) {
	// JSC returns exactly -1/0/1 and the fixtures record those literals; a
	// caller doing `if LocaleCompare(a,b) == -1` must not break.
	for _, tc := range [][2]string{{"a", "b"}, {"b", "a"}, {"a", "a"}, {"Å", "Å"}, {"", "a"}, {"", ""}} {
		switch r := LocaleCompare(tc[0], tc[1]); r {
		case -1, 0, 1:
		default:
			t.Errorf("LocaleCompare(%q, %q) = %d, want -1, 0 or 1", tc[0], tc[1], r)
		}
	}
}

func TestLocaleCompareInvalidUTF8(t *testing.T) {
	// No JS oracle exists (JSON cannot carry a lone surrogate), so this only
	// pins "does not panic" and "still a total order". Unreachable from both
	// real call sites, which read keys out of parsed JSON.
	bad := []string{"a\xffb", "\xff", "\xed\xa0\x80", "\xff\xfe", "�", "ab", "a"}
	for _, x := range bad {
		for _, y := range bad {
			r := LocaleCompare(x, y)
			if r < -1 || r > 1 {
				t.Fatalf("LocaleCompare(%q, %q) = %d", x, y, r)
			}
			if rev := LocaleCompare(y, x); rev != -r {
				t.Fatalf("LocaleCompare not antisymmetric on (%q, %q): %d vs %d", x, y, r, rev)
			}
		}
	}
	// Invalid bytes are NOT folded into U+FFFD.
	if LocaleCompare("�", "\xff") == 0 {
		t.Error("invalid bytes now collate equal to U+FFFD; the fidelity note in localecompare.go is stale")
	}
}

func TestLocaleCompareConcurrent(t *testing.T) {
	// collate.Collator carries mutable iterators; the pool in localecompare.go
	// is what makes this safe. Run under -race to mean anything.
	fx := loadLC(t)
	c := fx.Pairwise.Corpus
	if len(c) > 400 {
		c = c[:400]
	}
	want := make([]int, 0, len(c))
	for i := 0; i+1 < len(c); i++ {
		want = append(want, LocaleCompare(c[i], c[i+1]))
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rep := 0; rep < 5; rep++ {
				for i := 0; i+1 < len(c); i++ {
					if got := LocaleCompare(c[i], c[i+1]); got != want[i] {
						t.Errorf("concurrent LocaleCompare(%q, %q) = %d, want %d", c[i], c[i+1], got, want[i])
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

func TestLocaleSortStringsMatchesSliceStable(t *testing.T) {
	// The two real call sites sort PAIRS, not bare strings, so they will use
	// sort.SliceStable + LocaleCompare directly. Pin that this produces the
	// same order LocaleSortStrings does.
	fx := loadLC(t)
	for _, sc := range fx.Sorts {
		manual := make([]string, len(sc.In))
		copy(manual, sc.In)
		sort.SliceStable(manual, func(i, j int) bool { return LocaleCompare(manual[i], manual[j]) < 0 })
		if got, want := strings.Join(manual, "\x00"), strings.Join(sc.Out, "\x00"); got != want {
			t.Errorf("%s: sort.SliceStable order differs from JS toSorted\n got %q\nwant %q", sc.Name, got, want)
		}
	}
}

func ExampleLocaleCompare() {
	// The three orderings byte comparison gets wrong.
	fmt.Println(LocaleCompare("a_b", "a-b")) // "_" sorts before "-"
	fmt.Println(LocaleCompare("_x", "0x"))   // punctuation before digits
	fmt.Println(LocaleCompare("a", "A"))     // lowercase before uppercase
	fmt.Println(strings.Compare("a_b", "a-b"), strings.Compare("_x", "0x"), strings.Compare("a", "A"))
	// Output:
	// -1
	// -1
	// -1
	// 1 1 1
}
