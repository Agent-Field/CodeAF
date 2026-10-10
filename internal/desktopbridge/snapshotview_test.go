package desktopbridge

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/session"
)

func snapOf(texts ...string) Snapshot {
	s := Snapshot{ID: "s1", Title: "t", Seq: 7}
	for _, t := range texts {
		s.Entries = append(s.Entries, session.DisplayEntry{Role: "user", Text: t})
	}
	return s
}

func TestTailReturnsOnlyEntriesAfterSince(t *testing.T) {
	tail := tailOf(snapOf("a", "b", "c", "d"), 2)
	if tail.Reset || tail.From != 2 || tail.Header.EntryCount != 4 {
		t.Fatalf("tail = %+v", tail)
	}
	if len(tail.Entries) != 2 || tail.Entries[0].Text != "c" || tail.Entries[1].Text != "d" {
		t.Fatalf("entries = %+v", tail.Entries)
	}
	if got := tailOf(snapOf("a"), 1); len(got.Entries) != 0 || got.Reset {
		t.Fatalf("caught-up tail = %+v", got)
	}
}

func TestARewrittenTranscriptAnswersFullWithReset(t *testing.T) {
	for _, since := range []int{3, -1} {
		tail := tailOf(snapOf("summary", "x"), since)
		if !tail.Reset || tail.From != 0 || len(tail.Entries) != 2 {
			t.Fatalf("since %d: tail = %+v", since, tail)
		}
	}
}

func TestOutputsOverTheCapAreOmittedAndFlagged(t *testing.T) {
	big := strings.Repeat("x", outputCap+1)
	in := []session.DisplayEntry{
		{Role: "tool", CallID: "c1", Output: big},
		{Role: "tool", CallID: "c2", Output: strings.Repeat("y", outputCap)},
	}
	out := elideOutputs(in, outputCap)
	if !out[0].OutputOmitted || out[0].Output != "" || out[0].OutputBytes != outputCap+1 {
		t.Fatalf("over-cap = %+v", out[0])
	}
	if out[1].OutputOmitted || len(out[1].Output) != outputCap {
		t.Fatalf("at-cap must stay inline")
	}
	if in[0].Output != big {
		t.Fatal("input was mutated")
	}
	raw, _ := json.Marshal(fullView(Snapshot{Entries: in}))
	if !strings.Contains(string(raw), `"OutputOmitted":true`) || !strings.Contains(string(raw), `"entryCount":2`) {
		t.Fatalf("wire = %.200s", raw)
	}
}

func TestElisionPreservesUTF8Boundaries(t *testing.T) {
	// Multi-byte text straddling the cap must be dropped whole or kept whole.
	in := []session.DisplayEntry{{Output: strings.Repeat("é", outputCap)}, {Output: "é"}}
	for _, e := range elideOutputs(in, outputCap) {
		if !utf8.ValidString(e.Output) {
			t.Fatalf("invalid UTF-8 in %q", e.Output)
		}
	}
	if o := elideOutputs(in, outputCap); o[0].OutputBytes != 2*outputCap || o[1].Output != "é" {
		t.Fatalf("got %+v", o)
	}
}
