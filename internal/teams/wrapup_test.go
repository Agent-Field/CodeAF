package teams

import (
	"os"
	"strings"
	"testing"
)

// THE PACKET FILE ROTATES AND LOSES NOTHING WAITING. With a tiny rotation
// size, a packet raised, one escalated and one decided, then enough raises to
// rotate twice: every packet still waiting reads back as it stood (the
// escalated one still escalated, at the team it went to), the rotated file
// exists, the current file is small again, and a packet decided just before
// the last rotation is still readable by id (its raiser is handed the answer
// from it).
func TestThePacketFileRotatesAndKeepsEveryWaitingPacket(t *testing.T) {
	dir := packetTeams(t)
	old := decisionsRotateBytes
	decisionsRotateBytes = 4096
	t.Cleanup(func() { decisionsRotateBytes = old })

	waiting, err := Raise(dir, conflict())
	must(t, err)
	up, err := Raise(dir, conflict())
	must(t, err)
	if _, err := Escalate(dir, up.ID, "lead", "aaaaaaaaaaaa", "not mine to judge"); err != nil {
		t.Fatal(err)
	}
	var decided Packet
	for i := 0; i < 12; i++ {
		p, err := Raise(dir, conflict())
		must(t, err)
		decided, err = Decide(dir, p.ID, "lead", "1", "matches the rest")
		must(t, err)
	}
	path := DecisionsPath(dir, "bbbbbbbbbbbb")
	if _, err := os.Stat(decisionsRotated(path)); err != nil {
		t.Fatalf("the file never rotated: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() > decisionsRotateBytes {
		t.Fatalf("the current file is %v after rotating (%v)", info, err)
	}
	open, _, err := OpenPackets(dir, ScopeAll)
	must(t, err)
	got := map[string]Packet{}
	for _, p := range open {
		got[p.ID] = p
	}
	if len(open) != 2 || got[waiting.ID].State != PacketOpen {
		t.Fatalf("waiting packets after rotation: %+v", open)
	}
	if e := got[up.ID]; e.State != PacketEscalated || e.Team != "aaaaaaaaaaaa" || len(e.Trail) != 1 {
		t.Fatalf("the escalated packet lost its state across rotation: %+v", e)
	}
	back, err := PacketByID(dir, decided.ID)
	if err != nil || back.State != PacketDecided || back.Decision != "1" {
		t.Fatalf("the last decided packet: %+v %v", back, err)
	}
	// And a fresh process (an empty memory) folds the two files the same way.
	forgetPackets(dir)
	again, _, err := OpenPackets(dir, ScopeAll)
	if err != nil || len(again) != 2 {
		t.Fatalf("a fresh fold across rotation: %+v %v", again, err)
	}
	// A waiting packet can still be decided after its raise rotated away.
	if _, err := Decide(dir, waiting.ID, "lead", "2", "the handler is newer"); err != nil {
		t.Fatalf("a carried packet could not be decided: %v", err)
	}
}

// A QUESTION'S ANSWER READS AS ONE ON THE RAIL.
func TestAQuestionsAnswerIsLoggedAsAnswered(t *testing.T) {
	dir := packetTeams(t)
	p, err := Raise(dir, Packet{Team: "bbbbbbbbbbbb", Kind: PacketQuestion, RaisedBy: "web", Question: "tabs or spaces?"})
	must(t, err)
	if _, err := Decide(dir, p.ID, FromManager, "tabs", "the repo uses tabs"); err != nil {
		t.Fatal(err)
	}
	log, err := ReadTraffic(dir, "bbbbbbbbbbbb", "", 0)
	must(t, err)
	last := log[len(log)-1]
	if last.Kind != KindPacket || last.State != PacketDecided || last.Text != "answered @web: tabs" {
		t.Fatalf("the answer's line: %+v", last)
	}
}

// THE WRAP-UP MARKER IS THE STATE, NOT THE WORDS, and accepting a closing
// report closes the team once, whichever side calls it first.
func TestWrapUpMarkerAndAcceptingAClosingReport(t *testing.T) {
	e := WrapUpRequest("")
	if !IsWrapUp(e) || e.Text != WrapUpText || e.To != ToManager {
		t.Fatalf("the request: %+v", e)
	}
	if IsWrapUp(Entry{Kind: KindDirective, From: FromYou, To: ToManager, Text: WrapUpText}) {
		t.Fatal("words without the marker read as a wrap-up")
	}
	dir := packetTeams(t)
	p, err := Raise(dir, Packet{Team: Person, Origin: "bbbbbbbbbbbb", Kind: PacketClosing, RaisedBy: FromManager,
		Question: "close dock?", Report: &ClosingReport{Done: "the form", Files: []string{"web/form.go"}},
		Options: []Option{{ID: OptionClose, Label: "Close", Consequence: "dock closes"},
			{ID: OptionKeepGoing, Label: "Keep going", Consequence: "dock stays open"}}})
	must(t, err)
	if closed, err := AcceptClosing(dir, p); closed || err != nil {
		t.Fatalf("a waiting report closed the team: %v %v", closed, err)
	}
	p, err = Decide(dir, p.ID, Person, OptionClose, "")
	must(t, err)
	closed, err := AcceptClosing(dir, p)
	if !closed || err != nil {
		t.Fatalf("accepting did not close: %v %v", closed, err)
	}
	if again, err := AcceptClosing(dir, p); again || err != nil {
		t.Fatalf("a second accept closed again: %v %v", again, err)
	}
	f, err := Load(dir)
	must(t, err)
	dock, _ := f.Team("bbbbbbbbbbbb")
	if !dock.Closed() || dock.Report != p.ID {
		t.Fatalf("dock after accepting: %+v", dock)
	}
	log, _ := ReadTraffic(dir, "bbbbbbbbbbbb", "", 0)
	if last := log[len(log)-1]; last.Kind != KindClose || !strings.Contains(last.Text, "closed dock") {
		t.Fatalf("no close line: %+v", last)
	}
	if _, err := AcceptClosing(dir, Packet{Kind: PacketCap}); err != ErrNotClosing {
		t.Fatalf("a cap packet: %v", err)
	}
}
