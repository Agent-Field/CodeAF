package lease_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestStopPublishWorkerReceipt(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Assignee: "sam", State: factory.StateLanded, Proof: []factory.Claim{{Text: "works", OK: true}}})
	history, _ := record.Open(f.store, record.Options{})
	if _, err := history.Append(context.Background(), record.Event{Kind: record.Approved, Job: it.ID, Handle: "sam"}); err != nil {
		t.Fatal(err)
	}
	doors := lease.NewDoors(f.desk)
	if _, err := doors.SignOff(it.ID, false); err != nil {
		t.Fatal(err)
	}
	held, err := f.desk.Pull(worker("publisher", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.desk.Drain(held.ID, held.Worker); err != nil {
		t.Fatal(err)
	}
	if err := doors.Stop(it.ID); err != nil {
		t.Fatal(err)
	}
	report := factory.RunReport{PR: &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen}, Publishing: &factory.Publishing{State: "opened", Attempt: 1}}
	if err := f.desk.Apply(held.ID, held.Worker, true, false, func(it *factory.Item) error { return run.ApplyReport(it, report, f.now) }); err != nil {
		t.Fatalf("in-flight PR receipt lost after floor Stop: %v", err)
	}
	saved, err := f.store.Get(it.ID)
	if err != nil || saved.State != factory.StateShipped || saved.PullRequest == nil {
		t.Fatalf("opened PR hidden: %+v %v", saved, err)
	}
	var words string
	for _, l := range saved.Stream.Log {
		words += l.Text
	}
	if !strings.Contains(words, "not closed") {
		t.Fatal("person was not told PR remains open")
	}
}
