package placegraph

import (
	"reflect"
	"testing"
	"time"
)

var rulesNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func ruleLine(id, text string, kind LineSourceKind, created time.Time) Line {
	return Line{ID: id, PlaceID: "pl_1", Text: text, Source: LineSource{Kind: kind}, CreatedAt: created}
}

func TestSupersedeNewerWinsAndMarksOlder(t *testing.T) {
	older := ruleLine("a", "Ship on Fridays", LineYouWrote, rulesNow.AddDate(0, 0, -3))
	newer := ruleLine("b", "Never ship on Fridays", LineSaidInChat, rulesNow)
	got, ask := Supersede(older, newer, rulesNow)
	if ask != nil || got.ReplacedBy != "b" || !got.ReplacedAt.Equal(rulesNow) {
		t.Fatalf("got %+v ask %+v", got, ask)
	}
}

func TestSupersedeSameDayFromPersonAsksOnce(t *testing.T) {
	older := ruleLine("a", "x", LineYouWrote, rulesNow.Add(-2*time.Hour))
	newer := ruleLine("b", "y", LineSaidInChat, rulesNow)
	got, ask := Supersede(older, newer, rulesNow)
	if ask == nil || ask.A.ID != "a" || ask.B.ID != "b" || got.ReplacedBy != "" {
		t.Fatalf("got %+v ask %+v", got, ask)
	}
}

func TestSupersedeSameDayLearnedLineStillReplaces(t *testing.T) {
	older := ruleLine("a", "x", LineLearned, rulesNow.Add(-time.Hour))
	got, ask := Supersede(older, ruleLine("b", "y", LineYouWrote, rulesNow), rulesNow)
	if ask != nil || got.ReplacedBy != "b" {
		t.Fatalf("got %+v ask %+v", got, ask)
	}
}

func TestPurgeStrikesAfterSevenDays(t *testing.T) {
	old := ruleLine("a", "x", LineYouWrote, rulesNow)
	old.ReplacedBy, old.ReplacedAt = "b", rulesNow.Add(-StrikeAfter)
	fresh := ruleLine("c", "y", LineYouWrote, rulesNow)
	fresh.ReplacedBy, fresh.ReplacedAt = "b", rulesNow.Add(-StrikeAfter+time.Second)
	live := ruleLine("b", "z", LineYouWrote, rulesNow)
	got := Purge([]Line{old, fresh, live}, rulesNow)
	if len(got) != 2 || got[0].ID != "c" || got[1].ID != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestPurgeKeepsLineAStillStruckLineReplacedBy(t *testing.T) {
	a := ruleLine("a", "x", LineYouWrote, rulesNow)
	a.ReplacedBy, a.ReplacedAt = "b", rulesNow.Add(-time.Hour)
	b := ruleLine("b", "y", LineYouWrote, rulesNow)
	b.ReplacedBy, b.ReplacedAt = "c", rulesNow.Add(-10*24*time.Hour)
	c := ruleLine("c", "z", LineYouWrote, rulesNow)
	if got := Purge([]Line{a, b, c}, rulesNow); len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestStillTrueDueAfterSixtyUnusedDays(t *testing.T) {
	unused := ruleLine("a", "x", LineYouWrote, rulesNow.AddDate(0, 0, -90))
	used := ruleLine("b", "y", LineYouWrote, rulesNow.AddDate(0, 0, -90))
	used.LastUsedAt = rulesNow.AddDate(0, 0, -10)
	asked := ruleLine("c", "z", LineYouWrote, rulesNow.AddDate(0, 0, -90))
	asked.AskedStillTrueAt = rulesNow.AddDate(0, 0, -5)
	replaced := ruleLine("d", "w", LineYouWrote, rulesNow.AddDate(0, 0, -90))
	replaced.ReplacedBy, replaced.ReplacedAt = "a", rulesNow
	edge := ruleLine("e", "v", LineYouWrote, rulesNow.Add(-StillTrueAfter+time.Second))
	got := StillTrueDue([]Line{unused, used, asked, replaced, edge}, rulesNow)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("got %+v", got)
	}
}

func TestMergeDuplicatesKeepsOlderSource(t *testing.T) {
	newer := ruleLine("n", "Use  tabs.", LineSaidInChat, rulesNow)
	newer.LastUsedAt = rulesNow
	older := ruleLine("o", "use tabs", LineYouWrote, rulesNow.AddDate(0, 0, -5))
	other := ruleLine("x", "use spaces", LineYouWrote, rulesNow)
	otherPlace := ruleLine("p", "use tabs", LineYouWrote, rulesNow)
	otherPlace.PlaceID = "pl_2"
	merged, removed := MergeDuplicates([]Line{newer, older, other, otherPlace})
	if !reflect.DeepEqual(removed, []string{"n"}) || len(merged) != 3 {
		t.Fatalf("merged %+v removed %v", merged, removed)
	}
	if merged[0].ID != "o" || merged[0].Source.Kind != LineYouWrote || !merged[0].LastUsedAt.Equal(rulesNow) {
		t.Fatalf("winner %+v", merged[0])
	}
}

func TestMergeDuplicatesLeavesReplacedLinesAlone(t *testing.T) {
	a := ruleLine("a", "same", LineYouWrote, rulesNow)
	b := ruleLine("b", "same", LineYouWrote, rulesNow)
	b.ReplacedBy, b.ReplacedAt = "a", rulesNow
	if merged, removed := MergeDuplicates([]Line{a, b}); len(merged) != 2 || len(removed) != 0 {
		t.Fatalf("merged %+v removed %v", merged, removed)
	}
}
