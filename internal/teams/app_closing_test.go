package teams

import (
	"errors"
	"testing"
	"time"
)

func TestAppClosingMutationDecidesBeforeClosingAndRejectsStaleWithoutDecision(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "Release", Manager: "manager", Members: []Member{{Key: "manager", Handle: "manager"}}}}))
	p, err := Raise(dir, Packet{Team: Person, Origin: "aaaaaaaaaaaa", Kind: PacketClosing, RaisedBy: FromManager, Question: "Finish?", Report: &ClosingReport{Done: "Shipped"}, Options: []Option{{ID: OptionClose, Label: "Finish crew", Consequence: "Stop members"}}})
	must(t, err)
	stale := errors.New("stale crew")
	err = UpdateClosing(dir, func(f *File) (string, string, error) { return p.ID, OptionClose, stale })
	if !errors.Is(err, stale) {
		t.Fatal(err)
	}
	waiting, err := PacketByID(dir, p.ID)
	must(t, err)
	if !waiting.Waiting() {
		t.Fatal("rejection accepted report")
	}
	must(t, UpdateClosing(dir, func(f *File) (string, string, error) { return p.ID, OptionClose, f.Close(p.Origin, time.Now(), p.ID) }))
	decided, err := PacketByID(dir, p.ID)
	must(t, err)
	f, err := Snapshot(dir)
	must(t, err)
	crew, _ := f.Team(p.Origin)
	if decided.Decision != OptionClose || decided.DecidedBy != Person || !crew.Closed() || crew.Report != p.ID {
		t.Fatal(decided, crew)
	}
}
