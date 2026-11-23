package record_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A public act carried out on a job's pull request is a line of the record:
// what was done, where, and who said yes. In Auto that is the job's manager,
// and the person whose sign-in posted it is named beside it.
func TestThePublicActsOfAJobAreRecorded(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := open(t, st, time.Now)
	ctx := context.Background()
	replied, err := r.Append(ctx, record.Event{Kind: record.PRActed, Job: 7, Handle: "@Me", Verb: "replied",
		Actor: record.Actor{Kind: "agent", Name: "main agent"}, Detail: "Replied in inline:501. https://github.com/acme/api/pull/7#discussion_r502 · Auto mode"})
	if err != nil {
		t.Fatal(err)
	}
	if replied.Actor.Kind != "agent" || replied.Handle != "me" || replied.Object != "job:7" || replied.Verb != "replied" {
		t.Fatalf("recorded %+v", replied)
	}
	for _, verb := range []string{"edited pull request", "changed base", "commented", "resolved review thread", "asked for review", "reran checks"} {
		if _, err := r.Append(ctx, record.Event{Kind: record.PRActed, Job: 7, Handle: "me", Verb: verb}); err != nil {
			t.Errorf("%s: %v", verb, err)
		}
	}
	yes, err := r.Append(ctx, record.Event{Kind: record.ActsApproved, Job: 7, Handle: "me", Detail: "post 2 replies"})
	if err != nil || yes.Verb != "approved public acts" || yes.Actor.Kind != "person" {
		t.Fatalf("approved %+v, %v", yes, err)
	}
	no, err := r.Append(ctx, record.Event{Kind: record.ActsDeclined, Job: 7, Handle: "me"})
	if err != nil || no.Verb != "declined public acts" {
		t.Fatalf("declined %+v, %v", no, err)
	}
	// An act with no verb of its own is not a line the record can read.
	if _, err := r.Append(ctx, record.Event{Kind: record.PRActed, Job: 7, Handle: "me"}); err == nil {
		t.Fatal("an act with no verb was recorded")
	}
}
