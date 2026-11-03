package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// briefDoor is a job agent whose errand answers a scripted brief and keeps
// what it was asked.
type briefDoor struct {
	*fakeConversation
	mu     sync.Mutex
	answer session.ErrandAnswer
	fail   error
	asks   []session.ErrandAsk
}

func (d *briefDoor) RunErrand(_ context.Context, ask session.ErrandAsk) (session.ErrandAnswer, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asks = append(d.asks, ask)
	return d.answer, d.fail
}

const briefDraft = `{"verdict":"Fixed. Retries no longer double-send; 3 tests pass.","verdict_evidence":[{"step":"test","try":1,"kind":"tests"}],` +
	`"blocks":[{"kind":"bullets","heading":"What happened","bullets":[{"text":"**Cause** a stale lease resent","evidence":[{"step":"test","try":1,"kind":"tests"}]},` +
	`{"text":"5 tests pass","evidence":[{"step":"test","try":1,"kind":"tests"}]}]}]}`

// THE WRITER, THROUGH THE APP'S DOOR: factory.job_brief_write asks the job's
// agent one errand under the brief role with the factory's prompt and input,
// hands its answer to the factory, which keeps what its check passes, says
// what it dropped; the brief then reads it. Billing belongs to the session.
func TestAppFactoryJobBriefWriteKeepsTheCheckedAnswer(t *testing.T) {
	s := newLocalSetup(t)
	door := &briefDoor{fakeConversation: &fakeConversation{}, answer: session.ErrandAnswer{Text: briefDraft, Model: "deepseek/deepseek-v4.1-flash", Usage: session.ErrandUsage{CostUSD: 0.002}}}
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: door, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	if !strings.Contains(s.hello, appwire.FeatureFactoryJobBriefWriter) {
		t.Fatalf("hello does not announce %s", appwire.FeatureFactoryJobBriefWriter)
	}
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 7, Title: "Fix retries", Kind: factory.KindIssue, State: factory.StateRunning,
		Stream: &factory.Stream{Spent: 1, Phases: []factory.Phase{
			{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 3}}},
		}}})
	if err != nil {
		t.Fatal(err)
	}
	var got appwire.FactoryJobBriefWritten
	s.must("factory.job_brief_write", map[string]any{"factory": st.Factory, "job": it.ID}, &got)
	if !got.Kept || got.Model != "deepseek/deepseek-v4.1-flash" || got.CostUSD != 0.002 || len(got.Drops) != 1 ||
		got.Current.Status != factory.BriefStatusReady || got.Current.Brief.Rev != 1 || len(got.Current.Brief.Blocks[0].Bullets) != 1 {
		t.Fatalf("written = %+v", got)
	}
	door.mu.Lock()
	ask := door.asks[0]
	door.mu.Unlock()
	if ask.Role != string(roles.RoleBrief) || ask.Purpose != briefPurpose || !ask.JSON || !strings.Contains(ask.Messages[0].Text, `"title":"Fix retries"`) {
		t.Fatalf("asked %+v", ask)
	}
	var read appwire.FactoryJobBrief
	s.must("factory.job_brief", map[string]any{"factory": st.Factory, "job": it.ID}, &read)
	if read.Brief.Rev != 1 || read.Brief.Verdict != "Fixed. Retries no longer double-send; 3 tests pass." {
		t.Fatalf("read = %+v", read)
	}
	if cur, _ := floor.Get(it.ID); cur.Stream.Spent != 1 {
		t.Fatalf("job spend = %v", cur.Stream.Spent)
	}

	// A model that answers nothing leaves the version and says why.
	door.mu.Lock()
	door.answer = session.ErrandAnswer{Reason: session.ErrandDeadline}
	door.mu.Unlock()
	s.must("factory.job_brief_write", map[string]any{"factory": st.Factory, "job": it.ID}, &got)
	if got.Kept || got.Current.Status != factory.BriefStatusFailed || got.Current.Failure != session.ErrandDeadline || got.Current.Brief.Rev != 1 {
		t.Fatalf("failed write = %+v", got)
	}
	s.mustFail("factory.job_brief_write", map[string]any{"factory": "f-elsewhere", "job": it.ID}, appwire.CodeNotFound)

	// A refused ask is model_failed on the brief, and its words are logged.
	var log bytes.Buffer
	was := errandLog
	errandLog = &log
	defer func() { errandLog = was }()
	door.mu.Lock()
	door.answer, door.fail = session.ErrandAnswer{}, fmt.Errorf("%w: system and messages are over 49152 bytes", session.ErrErrandAsk)
	door.mu.Unlock()
	s.must("factory.job_brief_write", map[string]any{"factory": st.Factory, "job": it.ID}, &got)
	if got.Current.Failure != session.ErrandModelFailed || !strings.Contains(log.String(), "over 49152 bytes") {
		t.Fatalf("refused write = %+v, log %q", got, log.String())
	}
}

// A BURST OF NEWS IS ONE WRITE, of the burst's strongest trigger.
func TestBriefDebounceTakesABurstAsOne(t *testing.T) {
	fired := make(chan string, 4)
	d := newBriefDebounce(5*time.Millisecond, func(job int, trigger string) { fired <- trigger })
	defer d.stop()
	d.note(3, factory.BriefTriggerStep)
	d.note(3, factory.BriefTriggerAsk)
	d.note(3, factory.BriefTriggerStep)
	if got := <-fired; got != factory.BriefTriggerAsk {
		t.Fatalf("fired %q", got)
	}
	// The burst was taken whole: the next news is a burst of its own, and
	// what fires is that news, not anything left of the first.
	d.note(3, factory.BriefTriggerEdit)
	if got := <-fired; got != factory.BriefTriggerEdit {
		t.Fatalf("the next burst fired %q", got)
	}
}

func TestStrongerTrigger(t *testing.T) {
	for _, c := range []struct{ had, next, want string }{
		{"", factory.BriefTriggerStep, factory.BriefTriggerStep},
		{factory.BriefTriggerStep, factory.BriefTriggerAsk, factory.BriefTriggerAsk},
		{factory.BriefTriggerAsk, factory.BriefTriggerStep, factory.BriefTriggerAsk},
		{factory.BriefTriggerStep, factory.BriefTriggerEdit, factory.BriefTriggerStep},
	} {
		if got := strongerTrigger(c.had, c.next); got != c.want {
			t.Errorf("%s then %s = %s", c.had, c.next, got)
		}
	}
}

// THE RUNNER'S NEWS IS THE WRITER'S TRIGGER: a step's line for a job on this
// machine's factory is noted for that job, done as a step result and waiting
// as an ask; a start, and a conversation that is no job's, are not.
func TestBriefNewsTriggersTheJobsWrite(t *testing.T) {
	s := newLocalSetup(t)
	s.start()
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 7, Title: "Fix", Kind: factory.KindIssue, State: factory.StateRunning, Talk: "/talk/job7.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	type fire struct {
		job     int
		trigger string
	}
	fired := make(chan fire, 4)
	s.backend.briefs.stop()
	s.backend.briefs = newBriefDebounce(time.Millisecond, func(job int, trigger string) { fired <- fire{job, trigger} })
	news := func(path string, ev factory.ProgressEvent) session.ProgressNews {
		return session.ProgressNews{Path: path, Step: &factory.StepProgress{Step: "test", Event: ev}}
	}
	s.backend.briefNews(news("/talk/other.jsonl", factory.ProgressDone))
	s.backend.briefNews(news(it.Talk, factory.ProgressStarted))
	s.backend.briefNews(news(it.Talk, factory.ProgressWaiting))
	if got := <-fired; got != (fire{it.ID, factory.BriefTriggerAsk}) {
		t.Fatalf("fired %+v", got)
	}
	s.backend.briefNews(news(it.Talk, factory.ProgressDone))
	if got := <-fired; got != (fire{it.ID, factory.BriefTriggerStep}) {
		t.Fatalf("fired %+v", got)
	}
}

// A job that ended with no work on it has nothing to brief: a closed issue a
// poll or a start finishes must not buy a model call. Work on it still does.
func TestABriefEndNeedsWorkOnTheJob(t *testing.T) {
	fired := make(chan string, 2)
	b := &appBackend{briefs: newBriefDebounce(time.Millisecond, func(_ int, trigger string) { fired <- trigger })}
	backlog := factory.Item{ID: 1, Kind: factory.KindIssue, State: factory.StateNew}
	closed := backlog
	closed.State, closed.DoneHow = factory.StateShipped, "closed"
	b.briefJobChanged(backlog, closed)
	select {
	case trigger := <-fired:
		t.Fatalf("a no-work closed job asked for a brief: %s", trigger)
	case <-time.After(50 * time.Millisecond):
	}
	worked, ended := backlog, closed
	worked.Stream, ended.Stream = &factory.Stream{Spent: 0.1}, &factory.Stream{Spent: 0.1}
	b.briefJobChanged(worked, ended)
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("a job with work wrote no end brief")
	}
}
