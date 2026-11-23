package trigger

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// The doors AgentField's reasoners call directly: a message reaches the job
// as a comment the manager reads, and the status is the job wire's facts,
// by (source, id) or by the job's number.
func TestTheMessageAndStatusDoorsAnswerWithoutHTTP(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := inbox.Add(ctx, st, inbox.Entry{Source: "inbox:ops", ExternalID: "A1", Title: "Fix the login timeout", Trust: factory.TierStranger}, t0)
	if err != nil {
		t.Fatal(err)
	}
	acc, err := Message(ctx, st, Said{Source: "inbox:ops", ID: "A1", Text: "mobile only"}, t0)
	if err != nil || acc.Job != res.ID || !acc.Accepted || acc.Outcome != factory.CommentContext {
		t.Fatalf("message = %+v, %v", acc, err)
	}
	got, err := Status(st, "inbox:ops", "A1")
	if err != nil || got.Job != res.ID || got.State == "" {
		t.Fatalf("status = %+v, %v", got, err)
	}
	byJob, err := StatusOfJob(st, res.ID)
	if err != nil || byJob != got {
		t.Fatalf("status by job = %+v, %v; by ref %+v", byJob, err, got)
	}
	if _, err := Status(st, "inbox:ops", "nope"); !errors.Is(err, inbox.ErrNoJob) {
		t.Fatalf("unknown = %v", err)
	}
	floor, _ := st.Add(ctx, factory.Item{Title: "typed", Repo: "acme/web"})
	if _, err := StatusOfJob(st, floor); !errors.Is(err, inbox.ErrNoJob) {
		t.Fatalf("a job the inbox did not file = %v", err)
	}
	inbox.WaitSent()
}
