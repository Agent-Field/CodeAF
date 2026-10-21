package session

import (
	"testing"
)

func TestMapOriginAnchorsSurviveEditedBriefs(t *testing.T) {
	record := taskRecord{OriginJournal: "/old/home/agent/session.jsonl", OriginLine: 7, Brief: "admitted", Request: "request"}
	// The real transcript spelling is supplied by the place, so the fixture stays
	// aligned with the store rather than declaring another filename convention.
	record.OriginJournal = Place{Dir: "/old/home/agent"}.Transcript()
	record.App.Brief = "edited"
	fact := nodeFact("owner", record)
	if fact.Node.OriginAnchor == nil || fact.Node.Instructions.Anchor == nil || fact.Node.Instructions.Anchor.Agent != "agent" || fact.Node.Instructions.Anchor.Line != 7 {
		t.Fatalf("edited brief lost its origin: %+v", fact.Node)
	}
	hosted := wgAnchor("file://other-machine"+record.OriginJournal, 7)
	if hosted == nil || hosted.Machine != "other-machine" {
		t.Fatalf("hosted: %+v", hosted)
	}
	if wgAnchor(record.OriginJournal, 0) != nil {
		t.Fatal("invented a missing line")
	}
}

func TestMapOriginReplayKeepsPhysicalUserLines(t *testing.T) {
	record := ReadTranscriptBytes([]byte(recordJournal(2, true)))
	if len(record.Earlier) == 0 || record.Earlier[0].JournalLine != 2 {
		t.Fatalf("earlier origin: %+v", record.Earlier)
	}
	for _, item := range record.Entries {
		if item.Role == "user" && item.JournalLine == 0 {
			t.Fatalf("user lost its physical line: %+v", item)
		}
	}
}
