package inbox

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

type fakeReplier struct {
	mu   sync.Mutex
	got  []string
	err  error
	seen []Event
}

func (r *fakeReplier) Reply(_ context.Context, id string, ev Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, id)
	r.seen = append(r.seen, ev)
	return r.err
}

func TestNotifyAnswersTheJobsOwnSource(t *testing.T) {
	st := openStore(t)
	e := entry()
	e.Source, e.ExternalID = "inbox:ops", "ticket:7"
	res, err := Add(context.Background(), st, e, at)
	if err != nil {
		t.Fatal(err)
	}
	replies.Wait() // the filing's own moment, told before anyone listens
	ops, other := &fakeReplier{}, &fakeReplier{}
	RegisterReplier("inbox:ops", ops)
	RegisterReplier("inbox", other)
	defer RegisterReplier("inbox:ops", nil)
	defer RegisterReplier("inbox", nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // a request that ended does not cut the answer short
	Notify(ctx, st, res.ID, Opened("https://github.com/acme/web/pull/3"))
	replies.Wait()
	if len(ops.got) != 1 || ops.got[0] != "ticket:7" || ops.seen[0].JobID != res.ID || ops.seen[0].Kind != EventPROpened || len(other.got) != 0 {
		t.Fatalf("ops %v %+v, other %v", ops.got, ops.seen, other.got)
	}
}

func TestNotifyNeverFailsAndSkipsJobsWithNoSource(t *testing.T) {
	st := openStore(t)
	floor, err := st.Add(context.Background(), factory.Item{Title: "typed", Repo: "acme/web"})
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeReplier{err: errors.New("token=sk-secret refused")}
	RegisterReplier("chat", r)
	RegisterReplier("inbox:ops", r)
	defer RegisterReplier("chat", nil)
	defer RegisterReplier("inbox:ops", nil)
	// Deliveries run on their own goroutines, so the log is guarded.
	var logMu sync.Mutex
	var logged []string
	logf = func(format string, args ...any) { logMu.Lock(); logged = append(logged, format); logMu.Unlock() }
	defer func() { logf = defaultLogf }()

	Notify(context.Background(), st, floor, Merged("u"))
	Notify(context.Background(), st, 999, Merged("u"))
	replies.Wait()
	if len(r.got) != 0 {
		t.Fatalf("a job with no source was answered: %v", r.got)
	}
	res, _ := Add(context.Background(), st, entry(), at)
	Notify(context.Background(), st, res.ID, Merged("u"))
	replies.Wait()
	// The filing and the merge: two answers, both failed, both logged.
	if len(r.got) != 2 || len(logged) != 2 || !strings.Contains(logged[0], "failed") {
		t.Fatalf("replies %v, logged %v", r.got, logged)
	}
}

func TestMomentsAreTheChangesOfAnInboxJob(t *testing.T) {
	job := factory.Item{SourceRef: "inbox:ops:A1", Origin: "inbox:ops", State: factory.StateRunning}
	mark := MarkOf(job)
	if got := mark.Moments(job); len(got) != 0 {
		t.Fatalf("no change = %+v", got)
	}
	opened := job
	opened.PullRequest = &factory.PullRequest{URL: "https://x/pull/1", State: factory.PROpen}
	if got := mark.Moments(opened); len(got) != 1 || got[0].Kind != EventPROpened || got[0].URL != "https://x/pull/1" {
		t.Fatalf("opened = %+v", got)
	}
	// A pull request changed in place still shows as merged.
	mark = MarkOf(opened)
	opened.PullRequest.State = factory.PRMerged
	if got := mark.Moments(opened); len(got) != 1 || got[0].Kind != EventMerged {
		t.Fatalf("merged = %+v", got)
	}
	stuck := job
	stuck.State, stuck.Question = factory.StateNeedsYou, "Approval needed: plan"
	if got := MarkOf(job).Moments(stuck); len(got) != 1 || got[0].Kind != EventQuestion || !strings.Contains(got[0].Text, "Approval needed: plan") {
		t.Fatalf("question = %+v", got)
	}
	stuck.Question = ""
	if got := MarkOf(job).Moments(stuck); len(got) != 1 || got[0].Kind != EventStuck {
		t.Fatalf("stuck = %+v", got)
	}
	// The first step beginning is started; a second report is not.
	fresh := factory.Item{SourceRef: "inbox:ops:A1", Origin: "inbox:ops", State: factory.StateQueued}
	ran := fresh
	ran.State, ran.Stream = factory.StateRunning, &factory.Stream{Started: at}
	if got := MarkOf(fresh).Moments(ran); len(got) != 1 || got[0].Kind != EventStarted {
		t.Fatalf("started = %+v", got)
	}
	if got := MarkOf(ran).Moments(ran); len(got) != 0 {
		t.Fatalf("a second report = %+v", got)
	}
	// Finishing with no pull request is done; a merge is merged alone.
	answered := ran
	answered.State, answered.DoneHow = factory.StateShipped, "answered"
	if got := MarkOf(ran).Moments(answered); len(got) != 1 || got[0].Kind != EventDone || !strings.Contains(got[0].Text, "answered") {
		t.Fatalf("done = %+v", got)
	}
	// A job the inbox did not file has none.
	forge := opened
	forge.SourceRef = ""
	if got := MarkOf(factory.Item{}).Moments(forge); len(got) != 0 {
		t.Fatalf("forge job moments = %+v", got)
	}
}

type fakeSink struct {
	mu  sync.Mutex
	got []Moment
}

func (s *fakeSink) Send(_ context.Context, m Moment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, m)
	return nil
}

// ONE BUS: a registered sink hears every moment of every job the inbox filed,
// addressed by source and key, beside the source's own replier; a job the
// inbox did not file reaches no sink.
func TestEverySinkHearsEachMomentBesideTheReplier(t *testing.T) {
	st := openStore(t)
	af, ops := &fakeSink{}, &fakeReplier{}
	RegisterSink("agentfield", af)
	RegisterReplier("inbox:ops", ops)
	defer RegisterSink("agentfield", nil)
	defer RegisterReplier("inbox:ops", nil)
	res, err := Add(context.Background(), st, entry(), at)
	if err != nil {
		t.Fatal(err)
	}
	floor, _ := st.Add(context.Background(), factory.Item{Title: "typed", Repo: "acme/web"})
	Notify(context.Background(), st, floor, Merged("u"))
	Notify(context.Background(), st, res.ID, Opened("https://x/pull/1"))
	WaitSent()
	if len(af.got) != 2 || af.got[0].Source != "inbox:ops" || af.got[0].ExternalID != "A1" || af.got[0].JobID != res.ID {
		t.Fatalf("sink heard %+v", af.got)
	}
	kinds := map[EventKind]bool{af.got[0].Kind: true, af.got[1].Kind: true}
	if !kinds[EventFiled] || !kinds[EventPROpened] || len(ops.seen) != 2 {
		t.Fatalf("sink kinds %v, replier %+v", kinds, ops.seen)
	}
}
