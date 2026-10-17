package factory

import (
	"encoding/json"
	"testing"
	"time"
)

var sizeReadAt = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

func TestShownSizeProvenance(t *testing.T) {
	for _, tc := range []struct {
		name       string
		it         Item
		size, from string
	}{
		{"a label beats a completed read", Item{Labels: []string{"size:l"}, Triage: Triage{Size: "S", SizeFrom: SizeFromRead, TriagedAt: sizeReadAt}}, "L", ShownLabel},
		{"a label beats a measured diff", Item{Kind: KindPR, Labels: []string{"size/s"}, Triage: Triage{Size: "L", SizeFrom: SizeFromDiff}}, "S", ShownLabel},
		{"a completed read with no label", Item{Triage: Triage{Size: "M", SizeFrom: SizeFromRead, TriagedAt: sizeReadAt}}, "M", ShownRead},
		{"a read that never completed", Item{Triage: Triage{Size: "M", SizeFrom: SizeFromRead}}, "", ""},
		{"a legacy size with no provenance, triaged", Item{Triage: Triage{Size: "S", TriagedAt: sizeReadAt}}, "", ""},
		{"a legacy size on an old pull request", Item{Kind: KindPR, Origin: OriginForge, Triage: Triage{Size: "M"}}, "", ""},
		{"a chat guess", Item{Origin: OriginChat, Triage: Triage{Size: "S", SizeFrom: SizeFromChat, TriagedAt: sizeReadAt}}, "", ""},
		{"a measured diff", Item{Kind: KindPR, Triage: Triage{Size: "M", SizeFrom: SizeFromDiff}}, "M", ShownDiff},
		{"a read outside the vocabulary", Item{Triage: Triage{Size: "XL", SizeFrom: SizeFromRead, TriagedAt: sizeReadAt}}, "", ""},
		{"a made-up word", Item{Triage: Triage{Size: "huge", SizeFrom: SizeFromRead, TriagedAt: sizeReadAt}}, "", ""},
		{"nothing at all", Item{}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if size, from := ShownSize(tc.it); size != tc.size || from != tc.from {
				t.Fatalf("ShownSize = %q from %q, want %q from %q", size, from, tc.size, tc.from)
			}
		})
	}
}

// A document written before provenance existed decodes to "unknown", and the
// bytes it was written with are unchanged.
func TestLegacyTriageDocumentHasNoShownSize(t *testing.T) {
	var it Item
	if err := json.Unmarshal([]byte(`{"Kind":"issue","Triage":{"Size":"S","TriagedAt":"2026-10-01T00:00:00Z"}}`), &it); err != nil {
		t.Fatal(err)
	}
	if it.Triage.Size != "S" || it.Triage.SizeFrom != "" {
		t.Fatalf("decoded %+v", it.Triage)
	}
	if size, from := ShownSize(it); size != "" || from != "" {
		t.Fatalf("a legacy size was shown: %q from %q", size, from)
	}
	raw, err := json.Marshal(Triage{Type: "bug"})
	if err != nil || containsKey(raw, "SizeFrom") {
		t.Fatalf("an unset provenance reached the document: %s %v", raw, err)
	}
}

func containsKey(raw []byte, key string) bool {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	_, ok := m[key]
	return ok
}

// size: and risky read the ONE resolver, so a body-length-era value stays out.
func TestSizeFilterAndRiskyReadTheResolver(t *testing.T) {
	read := func(size string) Item {
		return Item{Triage: Triage{Size: size, SizeFrom: SizeFromRead, TriagedAt: sizeReadAt}}
	}
	legacy := Item{Triage: Triage{Size: "L"}}
	chat := Item{Triage: Triage{Size: "L", SizeFrom: SizeFromChat}}
	unread := Item{}
	labelled := Item{Labels: []string{"size:s"}, Triage: Triage{Size: "L", SizeFrom: SizeFromRead, TriagedAt: sizeReadAt}}
	for _, tc := range []struct {
		name  string
		it    Item
		query string
		want  bool
	}{
		{"read M matches size:m", read("M"), "size:m", true},
		{"legacy L does not match size:l", legacy, "size:l", false},
		{"chat L does not match size:l", chat, "size:l", false},
		{"an unread issue matches no size", unread, "size:s", false},
		{"the label overrides the read", labelled, "size:s", true},
		{"the read behind a label does not match", labelled, "size:l", false},
		{"read L is risky", read("L"), "risky", true},
		{"legacy L is not risky", legacy, "risky", false},
		{"chat L is not risky", chat, "risky", false},
		{"a label L is risky", Item{Labels: []string{"size:l"}}, "risky", true},
		{"a risk phrase is risky", Item{Triage: Triage{Risk: []string{"touches money"}}}, "risky", true},
	} {
		if got := Match(tc.query)(tc.it); got != tc.want {
			t.Errorf("%s: %q = %v, want %v", tc.name, tc.query, got, tc.want)
		}
	}
}

// A size that the read wrote goes with the read; one it did not write stays.
func TestClearReadTakesOnlyTheReadsSize(t *testing.T) {
	for _, tc := range []struct {
		name, from string
		keep       bool
	}{
		{"read", SizeFromRead, false},
		{"diff", SizeFromDiff, true},
		{"chat", SizeFromChat, true},
		{"legacy", "", true},
	} {
		tr := Triage{Size: "M", SizeFrom: tc.from, Read: "x", TriagedAt: sizeReadAt}
		ClearRead(&tr)
		if kept := tr.Size == "M" && tr.SizeFrom == tc.from; kept != tc.keep {
			t.Errorf("%s: after ClearRead %q from %q", tc.name, tr.Size, tr.SizeFrom)
		}
	}
}

// A start is a label's call: JobSize never sees a read's, a diff's or a
// chat model's size.
func TestJobSizeIgnoresEveryOtherSize(t *testing.T) {
	for _, from := range []string{SizeFromRead, SizeFromDiff, SizeFromChat, ""} {
		if got := JobSize(Item{Triage: Triage{Size: "S", SizeFrom: from, TriagedAt: sizeReadAt}}); got != "" {
			t.Errorf("from %q: JobSize = %q", from, got)
		}
	}
}
