package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestBandClausesKeepsWholeFacts(t *testing.T) {
	ink := func(s string) string { return s }
	tests := []struct {
		name    string
		width   int
		indent  int
		clauses []string
		want    []string
	}{
		{name: "one row", width: 30, clauses: []string{"one", "two", "three"}, want: []string{"one · two · three"}},
		{name: "greedy", width: 11, clauses: []string{"one", "two", "three"}, want: []string{"one · two", "three"}},
		{name: "drops empty", width: 20, clauses: []string{"", " one ", "  ", "two"}, want: []string{"one · two"}},
		{name: "clips only one clause", width: 8, clauses: []string{"extraordinary", "last"}, want: []string{"extraor…", "last"}},
		{name: "continuation indent", width: 10, indent: 2, clauses: []string{"first", "second"}, want: []string{"first", "  second"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := bandClauses(test.width, test.indent, ink, test.clauses...)
			if strings.Join(got, "\n") != strings.Join(test.want, "\n") {
				t.Fatalf("got %q, want %q", got, test.want)
			}
			for _, row := range got {
				if ansi.StringWidth(row) > test.width {
					t.Fatalf("%q is wider than %d", row, test.width)
				}
			}
		})
	}
}

func assertNarrowRows(t *testing.T, name string, rows []string, width int, last string) {
	t.Helper()
	joined := plain(strings.Join(rows, "\n"))
	if !strings.Contains(joined, last) {
		t.Errorf("%s lost its last clause %q:\n%s", name, last, joined)
	}
	for _, row := range rows {
		if ansi.StringWidth(row) > width {
			t.Errorf("%s drew %d cells at width %d: %q", name, ansi.StringWidth(row), width, plain(row))
		}
	}
}

func TestFitLeftKeepsThePlaceTail(t *testing.T) {
	got := fitLeft("alpha · /a/very/long/project/reporting", 30)
	if !strings.HasSuffix(got, "/project/reporting") || ansi.StringWidth(got) > 30 {
		t.Fatalf("left-clipped place = %q", got)
	}
}
