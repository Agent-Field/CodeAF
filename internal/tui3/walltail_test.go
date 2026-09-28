package tui3

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestWallTailFrom(t *testing.T) {
	many := make([]session.DisplayEntry, 0, wallTailCap+10)
	for i := 0; i < wallTailCap+10; i++ {
		many = append(many, session.DisplayEntry{Role: "assistant", Text: "line " + strconv.Itoa(i)})
	}
	cases := []struct {
		name    string
		entries []session.DisplayEntry
		want    []wallLine
	}{
		{
			name: "a tool call folds to one line on its hint",
			entries: []session.DisplayEntry{
				{Role: "tool", Tool: "bash", Hint: "go test ./...", Text: "ok\nok\nFAIL"},
			},
			want: []wallLine{{wallTool, "▸ bash go test ./..."}},
		},
		{
			name: "a tool call with no hint takes its first line",
			entries: []session.DisplayEntry{
				{Role: "tool", Tool: "read", Text: "\n  main.go\nmore"},
			},
			want: []wallLine{{wallTool, "▸ read main.go"}},
		},
		{
			name: "blank runs and fences are dropped, markers stripped",
			entries: []session.DisplayEntry{
				{Role: "user", Text: "fix it\n\nplease"},
				{Role: "assistant", Text: "## Plan\n\n\n**first** the `test`\n```go\nx := 1\n```\n- then this"},
				{Role: "aside", Text: "\ntask done\ndetails"},
			},
			want: []wallLine{
				{wallUser, "› fix it"},
				{wallUser, "  please"},
				{wallProse, "Plan"},
				{wallProse, "first the test"},
				{wallProse, "x := 1"},
				{wallProse, "• then this"},
				{wallNote, "task done"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := wallTailFrom(tc.entries)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d lines %v, want %v", len(got), got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("line %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
	t.Run("the cap keeps the last lines", func(t *testing.T) {
		got, n := wallTailFrom(many)
		if len(got) != wallTailCap {
			t.Fatalf("got %d lines, want %d", len(got), wallTailCap)
		}
		if got[0].text != "line 10" || got[len(got)-1].text != "line "+strconv.Itoa(wallTailCap+9) {
			t.Errorf("kept %q .. %q", got[0].text, got[len(got)-1].text)
		}
		want := 0
		for _, e := range many {
			want += len(e.Text)
		}
		if n != want {
			t.Errorf("textLen = %d, want %d", n, want)
		}
	})
}

func TestWallTakeFresh(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	base := []session.DisplayEntry{{Role: "user", Text: "go"}, {Role: "assistant", Text: "one"}}
	var tail wallTail
	tail.take(base, t0, true)
	if tail.fresh != 0 || !tail.freshAt.IsZero() {
		t.Fatalf("first reading counted as fresh: %d", tail.fresh)
	}
	if got := tail.sparkline(t0); !allZero(got) {
		t.Fatalf("first reading sampled: %v", got)
	}

	// The last entry grows: one fresh line.
	grown := []session.DisplayEntry{base[0], {Role: "assistant", Text: "one\ntwo"}}
	tail.take(grown, t0.Add(time.Second), true)
	if tail.fresh != 1 {
		t.Errorf("growing last entry fresh = %d, want 1", tail.fresh)
	}

	// Two new entries, one of two lines: three fresh.
	more := append(append([]session.DisplayEntry(nil), grown...),
		session.DisplayEntry{Role: "tool", Tool: "bash", Hint: "ls"},
		session.DisplayEntry{Role: "assistant", Text: "a\n\nb"})
	tail.take(more, t0.Add(2*time.Second), true)
	if tail.fresh != 3 {
		t.Errorf("new entries fresh = %d, want 3", tail.fresh)
	}

	// Nothing changed: the fresh count and its time stand.
	at := tail.freshAt
	tail.take(more, t0.Add(3*time.Second), true)
	if tail.freshAt != at {
		t.Errorf("an unchanged reading moved freshAt")
	}

	// A shrinking transcript is a new one.
	tail.take(base, t0.Add(4*time.Second), true)
	if tail.fresh != 0 {
		t.Errorf("a rewind counted fresh = %d", tail.fresh)
	}
}

func TestWallTakeSparkRing(t *testing.T) {
	t0 := time.Unix(2_000_000, 0)
	var tail wallTail
	text := ""
	read := func(at time.Time, add int) {
		text += strings.Repeat("x", add)
		tail.take([]session.DisplayEntry{{Role: "assistant", Text: text}}, at, true)
	}
	read(t0, 1) // first reading, not activity
	read(t0.Add(1*time.Second), 1)
	read(t0.Add(2*time.Second), 81)
	read(t0.Add(2*time.Second+500*time.Millisecond), 1000)
	// Skip seconds 3..5, then one more.
	read(t0.Add(6*time.Second), 40)

	got := tail.sparkline(t0.Add(7 * time.Second))
	if len(got) != wallSparkLen {
		t.Fatalf("len = %d", len(got))
	}
	// Index of second s, with now at second 7: len-1-(7-s).
	at := func(s int) uint8 { return got[wallSparkLen-1-(7-s)] }
	if at(1) != 1 {
		t.Errorf("second 1 = %d, want 1", at(1))
	}
	if at(2) != 7 {
		t.Errorf("second 2 = %d, want 7 (capped)", at(2))
	}
	for s := 3; s <= 5; s++ {
		if at(s) != 0 {
			t.Errorf("skipped second %d = %d, want 0", s, at(s))
		}
	}
	if at(6) != 1 || at(7) != 0 {
		t.Errorf("second 6 = %d, second 7 = %d, want 1 and 0", at(6), at(7))
	}

	// A full ring past the last sample, every old slot reads 0 even where the
	// ring wraps onto it.
	later := t0.Add(time.Duration(6+wallSparkLen) * time.Second)
	read(later, 1)
	got = tail.sparkline(later)
	for i, v := range got[:wallSparkLen-1] {
		if v != 0 {
			t.Errorf("slot %d = %d after a full wrap, want 0", i, v)
		}
	}
	if got[wallSparkLen-1] != 1 {
		t.Errorf("newest = %d, want 1", got[wallSparkLen-1])
	}
}

func TestWallAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "0s"},
		{0, "0s"},
		{12 * time.Second, "12s"},
		{59*time.Second + 900*time.Millisecond, "59s"},
		{2 * time.Minute, "2m"},
		{3*time.Hour + 59*time.Minute, "3h"},
		{4 * 24 * time.Hour, "4d"},
	}
	for _, tc := range cases {
		if got := wallAge(tc.d); got != tc.want {
			t.Errorf("wallAge(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func allZero(v []uint8) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}
