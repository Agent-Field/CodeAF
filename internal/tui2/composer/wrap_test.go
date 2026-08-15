package composer

import "testing"

func TestLayoutRows_SplitsOnNewline(t *testing.T) {
	rows := layoutRows([]rune("ab\ncd"), 10)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0] != (span{0, 2}) || rows[1] != (span{3, 5}) {
		t.Fatalf("rows = %v, want [{0 2} {3 5}]", rows)
	}
}

func TestLayoutRows_HardWrapsAtWidth(t *testing.T) {
	rows := layoutRows([]rune("abcdef"), 2)
	want := []span{{0, 2}, {2, 4}, {4, 6}}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %v", len(rows), len(want), rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d = %v, want %v", i, rows[i], want[i])
		}
	}
}

func TestLayoutRows_EmptyValue(t *testing.T) {
	rows := layoutRows(nil, 10)
	if len(rows) != 1 || rows[0] != (span{0, 0}) {
		t.Fatalf("rows = %v, want a single empty row", rows)
	}
}

func TestLayoutRows_TrailingNewlineAddsEmptyRow(t *testing.T) {
	rows := layoutRows([]rune("ab\n"), 10)
	want := []span{{0, 2}, {3, 3}}
	if len(rows) != len(want) || rows[0] != want[0] || rows[1] != want[1] {
		t.Fatalf("rows = %v, want %v", rows, want)
	}
}

func TestLayoutRows_ZeroWidthNeverLoops(t *testing.T) {
	done := make(chan []span, 1)
	go func() { done <- layoutRows([]rune("some text\nmore text"), 0) }()
	rows := <-done
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want one per logical line (2)", len(rows))
	}
	for _, r := range rows {
		if r.Start != r.End {
			t.Fatalf("row %v is non-empty at width 0", r)
		}
	}
}

func TestLayoutRows_NegativeWidthNeverLoops(t *testing.T) {
	rows := layoutRows([]rune("text"), -5)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
}

func TestRowOf_FindsContainingRow(t *testing.T) {
	rows := []span{{0, 2}, {2, 4}, {4, 6}}
	cases := []struct {
		pos  int
		want int
	}{
		{0, 0}, {1, 0}, {2, 1}, {3, 1}, {4, 2}, {6, 2},
	}
	for _, c := range cases {
		if got := rowOf(rows, c.pos); got != c.want {
			t.Fatalf("rowOf(rows, %d) = %d, want %d", c.pos, got, c.want)
		}
	}
}

func TestRuneCells_WideAndNarrow(t *testing.T) {
	if runeCells('a') != 1 {
		t.Fatalf("runeCells('a') = %d, want 1", runeCells('a'))
	}
	if w := runeCells('世'); w != 2 {
		t.Fatalf("runeCells('世') = %d, want 2", w)
	}
}
