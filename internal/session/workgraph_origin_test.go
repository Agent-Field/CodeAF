package session

import (
	"strings"
	"testing"
	"unsafe"
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
	if len(record.Earlier) == 0 || record.JournalLine(true, 0) != 2 {
		t.Fatalf("earlier origin: %+v", record.Earlier)
	}
	for index, item := range record.Entries {
		if item.Role == "user" && record.JournalLine(false, index) == 0 {
			t.Fatalf("user lost its physical line: %+v", item)
		}
	}
}

// Graph provenance must not scale with tool rounds or survive in a resumed
// agent. Only a display record retains the person's physical message address.
func TestMapOriginMetadataStaysOnTheDisplayRecord(t *testing.T) {
	const question = `{"type":"message","role":"user","content":"run sixty rounds"}` + "\n"
	const round = `{"type":"message","role":"assistant","content":"working","toolCalls":[{"id":"c","function":{"name":"read","arguments":"{}"}}]}` + "\n" +
		`{"type":"message","role":"tool","toolCallId":"c","content":"result"}` + "\n"
	raw := question + strings.Repeat(round, 60)
	resumed, err := readJournal(strings.NewReader(raw), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.userLines != nil {
		t.Fatal("a resumed agent retained graph-only physical origins")
	}
	record := ReadTranscriptBytes([]byte(raw))
	if len(record.userLines) != 1 || record.JournalLine(false, 0) != 1 {
		t.Fatalf("expected only the person's original line: %+v", record.userLines)
	}
	if len(record.Entries) < 100 || record.JournalLine(false, len(record.Entries)-1) != 0 {
		t.Fatal("tool rounds acquired physical origin metadata")
	}
	copied := cloneRecord(record)
	if copied.JournalLine(false, 0) != 1 {
		t.Fatal("a cache lease lost the origin")
	}
	without := record
	without.userLines = nil
	want := int64(cap(record.userLines)) * int64(unsafe.Sizeof(recordUserLine{}))
	if got := recordCost(record) - recordCost(without); got != want {
		t.Fatalf("record origin allocation costs %d bytes, charged %d", want, got)
	}
}
