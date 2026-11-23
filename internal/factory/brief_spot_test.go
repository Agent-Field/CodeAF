package factory

import (
	"strings"
	"testing"
)

// A SPOT OPENS ON ITS OWN HUNKS: those of its file that name its symbol (all
// of its file's when none does), with the writer's notes as rows after the
// hunk each is on. Spots are numbered as the version is kept, and a note on
// another file's hunk, or with a number its hunk does not show, is dropped.
func TestBriefSpotOpensOnItsOwnHunks(t *testing.T) {
	it := writerJob()
	raw := `{"verdict":"Fixed.","verdict_evidence":[],"blocks":[{"kind":"where_to_look","heading":"Where to look","rest":"go.sum 1 line",` +
		`"spots":[{"file":"retry/send.go","symbol":"Send","lines":4,"why":"the retry returns early","evidence":[{"kind":"hunk","ref":"retry/send.go:10"}],` +
		`"notes":[{"hunk":"retry/send.go:10","text":"returns before the resend"},{"hunk":"go.sum:1","text":"not this file"},{"hunk":"retry/send.go:10","text":"adds 9 lines"}]},` +
		`{"file":"retry/send.go","symbol":"Nothing","lines":1,"why":"the ledger write","evidence":[{"kind":"hunk","ref":"retry/send.go:42"}]}]}]}`
	v, drops, fault := CheckBriefDraft(it, []byte(raw))
	if fault != "" || len(drops) != 2 {
		t.Fatalf("fault %q drops %+v", fault, drops)
	}
	it.KeepBrief(v)
	got, ok := BriefSpotOf(it, "1")
	if !ok || got.File != "retry/send.go" || got.Why != "the retry returns early" || len(got.Hunks) != 1 || got.Hunks[0].Start != 10 ||
		!strings.Contains(got.Hunks[0].Text, "return nil") || len(got.Annotations) != 1 || got.Annotations[0] != (SpotNote{AfterHunk: 0, Text: "returns before the resend"}) {
		t.Fatalf("spot 1 = %+v", got)
	}
	if got, ok := BriefSpotOf(it, "2"); !ok || len(got.Hunks) != 2 || len(got.Annotations) != 0 {
		t.Fatalf("spot 2 (no hunk names its symbol) = %+v", got)
	}
	if _, ok := BriefSpotOf(it, "3"); ok {
		t.Fatal("a spot the brief does not have")
	}
	it.Change = nil
	if got, ok := BriefSpotOf(it, "1"); !ok || got.Hunks == nil || len(got.Hunks) != 0 {
		t.Fatalf("a job without its change = %+v", got)
	}
}
