package teams

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// A LINE'S PROVENANCE IS KEPT, READ BACK AND NEVER INVENTED.

func TestEntryProvenanceRoundTripsThroughTheLogAndItsRotation(t *testing.T) {
	dir := t.TempDir()
	defer func(n int64) { trafficRotateBytes = n }(trafficRotateBytes)
	prov := Provenance{Turn: "msg_a", Run: "RUN1", RunBy: "/conv/manager"}
	stamped := note("a", "b", "stamped")
	stamped.Provenance = prov
	if err := AppendTraffic(dir, "t1", stamped); err != nil {
		t.Fatal(err)
	}
	// Enough further lines to rotate the stamped one into the second file.
	trafficRotateBytes = 600
	for i := 0; i < 12; i++ {
		if err := AppendTraffic(dir, "t1", note("a", "b", fmt.Sprintf("filler %02d %s", i, strings.Repeat("x", 40)))); err != nil {
			t.Fatal(err)
		}
	}
	path := TrafficPath(dir, "t1")
	if _, err := os.Stat(trafficRotated(path)); err != nil {
		t.Fatalf("nothing rotated: %v", err)
	}
	all, err := ReadTraffic(dir, "t1", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range all {
		if e.Text != "stamped" && e.Provenance != (Provenance{}) {
			t.Errorf("a line nobody stamped reads back as stamped: %+v", e)
		}
	}
	// A rotated-away line is simply gone, and the walk of a reader ends there.
	for _, e := range all {
		if e.Text == "stamped" && e.Provenance != prov {
			t.Errorf("provenance read back as %+v", e.Provenance)
		}
	}
}

func TestOldLogLinesReadAsUnknownAndNewFieldsAreOmittedWhenEmpty(t *testing.T) {
	old := `{"id":"000000000001","at":"2026-09-24T12:00:00Z","kind":"note","from":"a","to":"b","text":"before turns were recorded"}`
	var e Entry
	if err := json.Unmarshal([]byte(old), &e); err != nil {
		t.Fatal(err)
	}
	if e.Provenance != (Provenance{}) || e.Provenance.Known() {
		t.Fatalf("an old line read as %+v", e.Provenance)
	}
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"turn"`, `"run"`, `"run_by"`} {
		if strings.Contains(string(raw), key) {
			t.Errorf("an unstamped line writes %s: %s", key, raw)
		}
	}
	// And a stamped one writes the three flat, under the names the wire reads.
	e.Provenance = Provenance{Turn: "m", Run: "r", RunBy: "k"}
	raw, _ = json.Marshal(e)
	for _, want := range []string{`"turn":"m"`, `"run":"r"`, `"run_by":"k"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
}

// ONLY THE PERSON'S OWN LINE MAY KEEP A MESSAGE ID WITHOUT A RUN.
func TestAppendKeepsOnlyAMessageThatSomethingRecordedWithARun(t *testing.T) {
	dir := t.TempDir()
	for _, e := range []Entry{
		{Kind: KindNote, From: "a", To: "b", Text: "guess", Provenance: Provenance{Turn: "m", RunBy: "k"}},
		{Kind: KindYou, From: FromYou, To: "b", Text: "person", Provenance: Provenance{Turn: "m"}},
		{Kind: KindNote, From: "a", To: "b", Text: "run", Provenance: Provenance{Run: "r", RunBy: "k"}},
	} {
		if err := AppendTraffic(dir, "t1", e); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := ReadTraffic(dir, "t1", "", 0)
	want := []Provenance{{}, {Turn: "m"}, {Run: "r", RunBy: "k"}}
	for i := range want {
		if got[i].Provenance != want[i] {
			t.Errorf("%q kept %+v, want %+v", got[i].Text, got[i].Provenance, want[i])
		}
	}
}

// THE PACKET SIBLINGS STAMP EVERY LINE THEY WRITE, the ruling directives of a
// conflict included, and the legacy doors stamp none.
func TestPacketLinesCarryTheTurnThatRaisedAndDecidedThem(t *testing.T) {
	dir := packetTeams(t)
	raised := Provenance{Turn: "msg_raise", Run: "RUN-R", RunBy: "w1"}
	p, err := RaiseFrom(dir, conflict(), raised)
	if err != nil {
		t.Fatal(err)
	}
	decided := Provenance{Turn: "msg_decide", Run: "RUN-D", RunBy: "dm"}
	if _, err := DecideFrom(dir, p.ID, FromManager, "1", "because", decided); err != nil {
		t.Fatal(err)
	}
	log, _ := ReadTraffic(dir, "bbbbbbbbbbbb", "", 0)
	var sawRaise, sawDecide, sawRuling bool
	for _, e := range log {
		switch {
		case e.Kind == KindPacket && e.State == PacketOpen:
			sawRaise = true
			if e.Provenance != raised {
				t.Errorf("the raise line: %+v", e.Provenance)
			}
		case e.Kind == KindPacket:
			sawDecide = true
			if e.Provenance != decided {
				t.Errorf("the decision line: %+v", e.Provenance)
			}
		case IsRuling(e):
			sawRuling = true
			if e.Provenance != decided {
				t.Errorf("the ruling: %+v", e.Provenance)
			}
		}
	}
	if !sawRaise || !sawDecide || !sawRuling {
		t.Fatalf("raise %v decide %v ruling %v in %+v", sawRaise, sawDecide, sawRuling, log)
	}

	// The unchanged doors write unstamped lines.
	dir = packetTeams(t)
	q, err := Raise(dir, conflict())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decide(dir, q.ID, Person, "2", ""); err != nil {
		t.Fatal(err)
	}
	log, _ = ReadTraffic(dir, "bbbbbbbbbbbb", "", 0)
	for _, e := range log {
		if e.Provenance != (Provenance{}) {
			t.Errorf("a legacy door stamped %+v", e)
		}
	}
}

func TestEscalateFromStampsItsLine(t *testing.T) {
	dir := packetTeams(t)
	p, err := Raise(dir, conflict())
	must(t, err)
	prov := Provenance{Run: "RUN-E", RunBy: "dm"}
	if _, err := EscalateFrom(dir, p.ID, "lead", "aaaaaaaaaaaa", "it touches billing too", prov); err != nil {
		t.Fatal(err)
	}
	stamped := 0
	log, _ := ReadTraffic(dir, "bbbbbbbbbbbb", "", 0)
	for _, e := range log {
		if strings.Contains(e.Text, "sent it up") {
			stamped++
			if e.Provenance != prov {
				t.Errorf("the escalation line: %+v", e.Provenance)
			}
		}
	}
	if stamped == 0 {
		t.Fatalf("no escalation line in %+v", log)
	}
}
