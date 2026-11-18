package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// stopFloor is a service over a store in a temporary folder with the real
// lease doors, on a clock the test moves, and a scripted main agent.
type stopFloor struct {
	t   *testing.T
	st  *store.Store
	rec *record.Record
	h   *Handler

	mu    sync.Mutex
	now   time.Time
	calls []factory.Wait
	// turn is the scripted main agent; nil says one plain line and decides
	// nothing.
	turn func(ctx context.Context, it factory.Item, wait factory.Wait) (string, error)
	done chan struct{}
}

// newStopFloor stands the service up. agent false is a factory with no main
// agent to ask.
func newStopFloor(t *testing.T, agent bool) *stopFloor {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &stopFloor{t: t, st: st, now: time.Now(), done: make(chan struct{}, 16)}
	if f.rec, err = record.Open(st, record.Options{Now: f.clock}); err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(st, lease.Options{Now: f.clock, Record: f.rec})
	if err != nil {
		t.Fatal(err)
	}
	doors := lease.NewDoors(desk)
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := st.List()
			return factory.Snapshot{Items: items}, err
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "sam"}, nil },
		Launch: doors.Launch, Stop: doors.Stop, Hold: doors.Hold, Answer: doors.Answer, SignOff: doors.SignOff,
	}
	cfg := Config{Version: "test", Mode: api.FactorySolo, Store: st, Record: f.rec, Now: f.clock, LeaseOptions: lease.Options{Now: f.clock, Record: f.rec}}
	if agent {
		cfg.Stopped = func(ctx context.Context, it factory.Item, wait factory.Wait) (string, error) {
			f.mu.Lock()
			f.calls = append(f.calls, wait)
			turn := f.turn
			f.mu.Unlock()
			defer func() { f.done <- struct{}{} }()
			if turn != nil {
				return turn(ctx, it, wait)
			}
			return "Both failures are in code this job did not touch.", nil
		}
	}
	f.h = New(seam, cfg)
	t.Cleanup(f.h.Close)
	return f
}

func (f *stopFloor) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *stopFloor) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *stopFloor) asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *stopFloor) pass() {
	f.t.Helper()
	held, err := f.h.leases()
	if err != nil {
		f.t.Fatal(err)
	}
	f.h.settleJobs(false, held)
}

func (f *stopFloor) job(id int) factory.Item {
	f.t.Helper()
	it, err := f.st.Get(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return it
}

// turned waits for the main agent's turn in flight to end and its write to
// land.
func (f *stopFloor) turned(id int) factory.Item {
	f.t.Helper()
	select {
	case <-f.done:
	case <-time.After(10 * time.Second):
		f.t.Fatal("the main agent was never asked")
	}
	for deadline := time.Now().Add(10 * time.Second); f.h.stalls.busy(id); time.Sleep(2 * time.Millisecond) {
		if time.Now().After(deadline) {
			f.t.Fatal("the turn never ended")
		}
	}
	return f.job(id)
}

// press sends the main agent's decision through its door.
func (f *stopFloor) press(id int, body string) *httptest.ResponseRecorder {
	f.t.Helper()
	return stopRequest(f.h, "POST", fmt.Sprintf("/v1/jobs/%d/%s", id, ManagerDecideVerb), body)
}

// stopRequest is one request to the service from this computer.
func stopRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// refusal is the sentence a refused press carries.
func refusal(w *httptest.ResponseRecorder) string {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Error
}

// stopJob is a job on its second step, at work, in mode.
func stopJob(mode string) factory.Item {
	return factory.Item{Title: "stops", Repo: "acme/api", Kind: factory.KindIssue, Num: 7, State: factory.StateRunning, Cap: 10, Assignee: "sam", Autonomy: mode,
		Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true, Until: factory.UntilDone}, {Name: "test", Kind: factory.StageChat, On: true, Until: factory.UntilGreen}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
			{Name: "test", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 2},
		}}}
}

func stopsOutOfRounds(it *factory.Item) {
	it.State, it.QKind = factory.StateNeedsYou, factory.QKindOutOfAttempts
	it.Question = "test is not green after 2 rounds · try again, send it back, or stop the job?"
	it.Asking = &factory.Asked{Stage: "test", Question: it.Question, With: factory.AskedYou}
	it.Stream.Phases[1].State = factory.PhaseWaiting
	it.Stream.Phases[1].Result = &factory.StageResult{Findings: 2, Output: "two tests fail"}
}

// stopped is a job the store wrote at work, then wrote again with stop
// applied: the write that makes the stop, as a runner's report does.
func (f *stopFloor) stopped(mode string, stop func(*factory.Item)) factory.Item {
	f.t.Helper()
	made, err := f.st.Create(stopJob(mode))
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.st.Update(made.ID, func(it *factory.Item) error { stop(it); return nil }); err != nil {
		f.t.Fatal(err)
	}
	return f.job(made.ID)
}

func logTexts(it factory.Item) []string {
	var out []string
	if it.Stream != nil {
		for _, l := range it.Stream.Log {
			out = append(out, l.Text)
		}
	}
	return out
}

// THE MAIN AGENT GETS ONE TURN ON A STOP, AT ONCE, and a turn that changes
// nothing hands the stop to the person with what it said as the reason.
func TestAStopIsGivenToTheMainAgentOnceAndThenToThePerson(t *testing.T) {
	f := newStopFloor(t, true)
	it := f.stopped(factory.AutonomyManual, stopsOutOfRounds)
	if it.Wait.Who != factory.WaitMainAgent || it.Wait.What != factory.ActAnswer {
		t.Fatalf("the stop reads %+v", it.Wait)
	}
	f.pass()
	got := f.turned(it.ID)
	if f.asked() != 1 || f.calls[0].Who != factory.WaitYou || f.calls[0].Action != factory.ActAnswer || f.calls[0].Words != it.Question {
		t.Fatalf("the turn was given %+v, %d times", f.calls, f.asked())
	}
	if got.Wait.Who != factory.WaitYou || got.Wait.Action != factory.ActAnswer || got.Handoff == nil || !got.Handoff.Over || got.Handoff.Turns != 1 {
		t.Fatalf("after a turn that changed nothing: wait %+v, handoff %+v", got.Wait, got.Handoff)
	}
	want := "The main agent could not settle this. Both failures are in code this job did not touch."
	if log := logTexts(got); len(log) == 0 || log[len(log)-1] != want {
		t.Fatalf("the log says %q, want its last line %q", log, want)
	}
	if err := factory.WaitFault(got); err != nil {
		t.Fatal(err)
	}
	// It never gets a second turn on the same stop.
	f.advance(time.Hour)
	f.pass()
	f.pass()
	if f.asked() != 1 {
		t.Fatalf("the main agent was asked %d times about one stop", f.asked())
	}
}

// A TURN THAT FAILS, A HOST THAT ASKS NOBODY AND A FACTORY WITH NO MAIN AGENT
// all hand the stop to the person within the pass, the last two without a
// word on the job's log.
func TestAStopNobodyCanBeAskedAboutGoesToThePerson(t *testing.T) {
	for name, c := range map[string]struct {
		agent bool
		err   error
		line  string
		why   string
	}{
		"the turn fails":             {true, errors.New("the conversation could not be opened"), "The main agent could not settle this. It could not be asked.", factory.HandoffFailed},
		"the runner is off":          {true, ErrNotAsked, "", factory.HandoffNotAsked},
		"the factory has no manager": {false, nil, "", factory.HandoffNotAsked},
	} {
		t.Run(name, func(t *testing.T) {
			f := newStopFloor(t, c.agent)
			f.turn = func(context.Context, factory.Item, factory.Wait) (string, error) { return "", c.err }
			it := f.stopped(factory.AutonomyAuto, stopsOutOfRounds)
			before := len(logTexts(it))
			f.pass()
			got := f.job(it.ID)
			if c.agent {
				got = f.turned(it.ID)
			}
			if got.Wait.Who != factory.WaitYou || got.Handoff == nil || !got.Handoff.Over || got.Handoff.Why != c.why {
				t.Fatalf("wait %+v, handoff %+v", got.Wait, got.Handoff)
			}
			log := logTexts(got)[before:]
			if c.line == "" && len(log) != 0 {
				t.Fatalf("a stop nobody was asked about wrote %q", log)
			}
			if c.line != "" && (len(log) != 1 || log[0] != c.line) {
				t.Fatalf("the log gained %q, want %q", log, c.line)
			}
		})
	}
}

// THE DEADLINE ENDS A HANDOFF WHATEVER ITS TURN IS DOING, and a turn lost to
// a restart ends one too. What the late turn says afterwards writes nothing
// over the person's stop.
func TestAHandoffPastItsDeadlineOrWithItsTurnLostGoesToThePerson(t *testing.T) {
	f := newStopFloor(t, true)
	release := make(chan struct{})
	f.turn = func(ctx context.Context, _ factory.Item, _ factory.Wait) (string, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "I need longer.", nil
	}
	it := f.stopped(factory.AutonomyAuto, stopsOutOfRounds)
	f.pass()
	for deadline := time.Now().Add(10 * time.Second); f.job(it.ID).Handoff.Turns == 0; time.Sleep(2 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the turn never began")
		}
	}
	// While the turn runs and the deadline has not come, the pass leaves it.
	f.pass()
	if got := f.job(it.ID); got.Wait.Who != factory.WaitMainAgent {
		t.Fatalf("the hold ended while its turn was in flight: %+v", got.Wait)
	}
	f.advance(factory.WaitMainAgentFor + time.Second)
	f.pass()
	got := f.job(it.ID)
	if got.Wait.Who != factory.WaitYou || !got.Handoff.Over || got.Handoff.Why != factory.HandoffLate {
		t.Fatalf("past the deadline: wait %+v, handoff %+v", got.Wait, got.Handoff)
	}
	lines := len(logTexts(got))
	version, _ := f.st.Revision(it.ID)
	close(release)
	late := f.turned(it.ID)
	if after, _ := f.st.Revision(it.ID); after != version || len(logTexts(late)) != lines {
		t.Fatalf("the late turn wrote the job again: revision %d to %d, log %q", version, after, logTexts(late))
	}

	// A turn counted and nobody running it: the service restarted mid-turn.
	lost := f.stopped(factory.AutonomyAuto, stopsOutOfRounds)
	if err := f.st.Annotate(lost.ID, func(cur *factory.Item) error {
		next := cur.Handoff.Turned(f.clock())
		cur.Handoff = &next
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	asked := f.asked()
	f.pass()
	if got := f.job(lost.ID); got.Wait.Who != factory.WaitYou || !got.Handoff.Over || f.asked() != asked {
		t.Fatalf("a lost turn: wait %+v, handoff %+v, asked %d more times", got.Wait, got.Handoff, f.asked()-asked)
	}

	// A stop that came while the service was down is past its deadline when
	// the service next looks. Its main agent still gets its one turn, and a
	// minute for it: a pass in the middle of that minute leaves the turn be.
	began, finish := make(chan struct{}), make(chan struct{})
	f.mu.Lock()
	f.turn = func(context.Context, factory.Item, factory.Wait) (string, error) {
		close(began)
		<-finish
		return "Nothing I may do settles it.", nil
	}
	f.mu.Unlock()
	down := f.stopped(factory.AutonomyAuto, stopsOutOfRounds)
	f.advance(factory.WaitMainAgentFor + time.Hour)
	asked = f.asked()
	f.pass()
	select {
	case <-began:
	case <-time.After(10 * time.Second):
		t.Fatal("a stop found late was given no turn")
	}
	f.advance(factory.HandoffTurnFor / 2)
	f.pass()
	if got := f.job(down.ID); got.Wait.Who != factory.WaitMainAgent || got.Handoff.Over {
		t.Fatalf("a turn given late was cut short: wait %+v, handoff %+v", got.Wait, got.Handoff)
	}
	close(finish)
	if got := f.turned(down.ID); f.asked() != asked+1 || got.Wait.Who != factory.WaitYou || got.Handoff.Why != "Nothing I may do settles it." {
		t.Fatalf("a stop found late: asked %d more times, wait %+v, handoff %+v", f.asked()-asked, got.Wait, got.Handoff)
	}
}

// THE MAIN AGENT'S DECISION IS THE PERSON'S ANSWER, PRESSED FOR THEM: another
// round with instructions puts an unheld step back in the queue with the
// words in its notes, is counted against the step, kept as an answer the main
// agent gave, written on the log and recorded as the agent's.
func TestManagerDecideGivesAnotherRoundWithInstructions(t *testing.T) {
	f := newStopFloor(t, true)
	var pressed *httptest.ResponseRecorder
	f.turn = func(_ context.Context, it factory.Item, _ factory.Wait) (string, error) {
		pressed = f.press(it.ID, `{"decision":"another_round","step":"test","words":"Fix the import cycle first."}`)
		return "Gave it another round.", nil
	}
	it := f.stopped(factory.AutonomyManual, stopsOutOfRounds)
	f.pass()
	got := f.turned(it.ID)
	if pressed == nil || pressed.Code != http.StatusOK {
		t.Fatalf("the decision was refused: %v", pressed)
	}
	if got.State != factory.StateQueued || got.Question != "" || got.Wait.Who != factory.WaitRunner {
		t.Fatalf("after another round: state %s, wait %+v, question %q", got.State, got.Wait, got.Question)
	}
	if ph := got.Stream.Phases[1]; ph.Round != 3 || ph.State != factory.PhasePending {
		t.Fatalf("the step: %+v", ph)
	}
	if len(got.Notes) != 1 || got.Notes[0] != "Fix the import cycle first." {
		t.Fatalf("the round's instructions: %q", got.Notes)
	}
	if got.Handoff == nil || got.Handoff.Wait != "" || got.Handoff.Rounds["test"] != 1 {
		t.Fatalf("the count: %+v", got.Handoff)
	}
	log := logTexts(got)
	if last := log[len(log)-1]; last != "Main agent: another round · Fix the import cycle first." {
		t.Fatalf("the log's last line: %q", last)
	}
	for _, line := range log {
		if strings.HasPrefix(line, factory.HandoffUnsettled) {
			t.Fatalf("a settled stop was handed to the person: %q", log)
		}
	}
	if n := len(got.AnsweredQuestions); n != 1 || got.AnsweredQuestions[0].With != factory.AskedManager || got.AnsweredQuestions[0].Answer.By != factory.AnsweredByManager {
		t.Fatalf("the answer kept: %+v", got.AnsweredQuestions)
	}
	events, err := f.rec.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Job == it.ID && e.Kind == record.Answered && e.Actor.Kind == "agent" && e.Detail == "another round · Fix the import cycle first." {
			found = true
		}
	}
	if !found {
		t.Fatalf("the decision is not on the record as the agent's: %+v", events)
	}
	// The person presses their card a moment later: the job has moved on.
	late := stopRequest(f.h, "POST", fmt.Sprintf("/v1/jobs/%d/answer", it.ID), `{"choice":"try_once_more"}`)
	if late.Code != http.StatusConflict || !strings.Contains(refusal(late), "answered this") {
		t.Fatalf("the person's late press: %d %s", late.Code, late.Body)
	}
}

// A DECISION IS REFUSED IN A SENTENCE, AND CHANGES NOTHING, when the job is
// on no stop the main agent may decide, when it does not fit the stop, when
// it names another step's stop, when it lacks its reason, and when the step
// has had its rounds.
func TestManagerDecideRefusesWhatIsNotTheMainAgents(t *testing.T) {
	f := newStopFloor(t, false)
	rounds := f.stopped(factory.AutonomyManual, stopsOutOfRounds)
	approval := f.stopped(factory.AutonomyManual, func(it *factory.Item) {
		it.State = factory.StateLanded
		it.Stream.Phases[1].State = factory.PhaseDone
		it.Proof = []factory.Claim{{Text: "works", OK: true}}
	})
	working, err := f.st.Create(stopJob(factory.AutonomyAuto))
	if err != nil {
		t.Fatal(err)
	}
	used := f.stopped(factory.AutonomyAuto, stopsOutOfRounds)
	if err := f.st.Annotate(used.ID, func(cur *factory.Item) error {
		cur.Handoff = &factory.Handoff{Wait: cur.Handoff.Wait, At: cur.Handoff.At, Over: true, Rounds: map[string]int{"test": factory.ManagerRoundsMost}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		id     int
		body   string
		status int
		says   string
	}{
		"an unknown decision":            {rounds.ID, `{"decision":"merge"}`, 422, "decision must be one of"},
		"a send back with no reason":     {rounds.ID, `{"decision":"send_back"}`, 422, "needs its reason"},
		"an accept with no reason":       {rounds.ID, `{"decision":"accept"}`, 422, "needs its reason"},
		"a decision for another step":    {rounds.ID, `{"decision":"another_round","step":"write"}`, 409, "stopped at test, not at write"},
		"a decision that does not fit":   {rounds.ID, `{"decision":"retry_publish"}`, 409, "does not fit this stop"},
		"a stop that is not Follow PR's": {rounds.ID, `{"decision":"wait_github","words":"nothing to do"}`, 409, "does not fit this stop"},
		"a Manual publish approval":      {approval.ID, `{"decision":"another_round"}`, 409, "not on a stop the main agent may decide"},
		"a job at work":                  {working.ID, `{"decision":"rerun","step":"test"}`, 409, "not on a stop the main agent may decide"},
		"a step that has had its rounds": {used.ID, `{"decision":"another_round"}`, 409, "already put this step back to work 2 times"},
	} {
		t.Run(name, func(t *testing.T) {
			before, _ := f.st.Revision(c.id)
			w := f.press(c.id, c.body)
			if w.Code != c.status || !strings.Contains(refusal(w), c.says) {
				t.Fatalf("%d %s, want %d saying %q", w.Code, w.Body, c.status, c.says)
			}
			if after, _ := f.st.Revision(c.id); after != before {
				t.Fatalf("a refused decision wrote the job: revision %d to %d", before, after)
			}
		})
	}
}

// HANDING A STOP TO THE PERSON ENDS THE HANDOFF WITH THE REASON on the job's
// log, and the turn that follows writes nothing over it.
func TestManagerDecideHandsAStopToThePersonWithItsReason(t *testing.T) {
	f := newStopFloor(t, true)
	f.turn = func(_ context.Context, it factory.Item, _ factory.Wait) (string, error) {
		if w := f.press(it.ID, `{"decision":"hand_to_person","words":"The two failing tests need a product decision."}`); w.Code != http.StatusOK {
			t.Errorf("hand_to_person refused: %d %s", w.Code, w.Body)
		}
		return "I handed it to you.", nil
	}
	it := f.stopped(factory.AutonomyAuto, stopsOutOfRounds)
	f.pass()
	got := f.turned(it.ID)
	if got.Wait.Who != factory.WaitYou || got.Wait.Action != factory.ActAnswer || !got.Handoff.Over || got.Handoff.Why != "The two failing tests need a product decision." {
		t.Fatalf("wait %+v, handoff %+v", got.Wait, got.Handoff)
	}
	var settled []string
	for _, line := range logTexts(got) {
		if strings.HasPrefix(line, factory.HandoffUnsettled) {
			settled = append(settled, line)
		}
	}
	if len(settled) != 1 || settled[0] != "The main agent could not settle this. The two failing tests need a product decision." {
		t.Fatalf("the hand-back lines: %q", settled)
	}
	// Handing over what the person already holds is refused.
	if w := f.press(it.ID, `{"decision":"hand_to_person","words":"again"}`); w.Code != http.StatusConflict {
		t.Fatalf("a second hand-over: %d %s", w.Code, w.Body)
	}
}

// WHILE THE MAIN AGENT HOLDS A STOP A READER IS SHOWN A JOB AT WORK, with the
// one line that says so, and neither the job nor the queue says it needs its
// person. The stored wait and the offers still say everything, and once the
// handoff ends the job is shown as it is stored.
func TestAReaderSeesAJobAtWorkWhileTheMainAgentHoldsItsStop(t *testing.T) {
	f := newStopFloor(t, true)
	it := f.stopped(factory.AutonomyManual, func(it *factory.Item) {
		it.State = factory.StateNew
		factory.StampFailure(it, factory.NewFailure("test", factory.CodeToolError, "the delegate crashed", "", factory.FailureStopped, 1, f.clock()))
		it.Stream.Phases[1].State = factory.PhaseFailed
		it.Stream.Ended = f.clock()
	})
	read := func() (api.Job, api.Queue) {
		t.Helper()
		var job api.Job
		w := stopRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "")
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &job) != nil {
			t.Fatalf("read the job: %d %s", w.Code, w.Body)
		}
		var queue api.Queue
		w = stopRequest(f.h, "GET", "/v1/queue", "")
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &queue) != nil {
			t.Fatalf("read the queue: %d %s", w.Code, w.Body)
		}
		return job, queue
	}
	hold := make(chan struct{})
	f.turn = func(ctx context.Context, _ factory.Item, _ factory.Wait) (string, error) {
		select {
		case <-hold:
		case <-ctx.Done():
		}
		return "It crashes the same way every time.", nil
	}
	job, queue := read()
	if job.State != api.StateRunning || job.NeedsYou != nil || job.Failure != nil ||
		job.StatePresentation.Kind != api.StateRunning || job.StatePresentation.Detail != factory.MainAgentDeciding {
		t.Fatalf("held by the main agent the job reads state %s, needs %+v, failure %+v, presentation %+v", job.State, job.NeedsYou, job.Failure, job.StatePresentation)
	}
	if job.Wait == nil || job.Wait.Who != factory.WaitMainAgent || job.Wait.What != factory.ActRetry {
		t.Fatalf("the wait on the wire: %+v", job.Wait)
	}
	if len(queue.NeedsYou) != 0 || len(queue.Jobs) != 1 || queue.Jobs[0].State != api.StateRunning || queue.Jobs[0].NeedsYou != nil {
		t.Fatalf("the queue says the job needs its person: groups %+v, job state %s", queue.NeedsYou, queue.Jobs[0].State)
	}
	// The stored job is untouched: it is the reader's copy that is dressed.
	if stored := f.job(it.ID); stored.Failure == nil || factory.JobState(stored) != api.StateNeedsYou {
		t.Fatalf("the stored job changed: state %s, failure %+v", factory.JobState(stored), stored.Failure)
	}
	close(hold)
	f.turned(it.ID)
	job, queue = read()
	if job.State != api.StateNeedsYou || job.NeedsYou == nil || job.Failure == nil || job.StatePresentation.Kind != "failed" {
		t.Fatalf("handed to the person the job reads state %s, needs %+v, presentation %+v", job.State, job.NeedsYou, job.StatePresentation)
	}
	if len(queue.NeedsYou) != 1 {
		t.Fatalf("the queue's groups: %+v", queue.NeedsYou)
	}
}

// CLOSING THE SERVICE ENDS A TURN IN FLIGHT and leaves the handoff with its
// turn counted, so the next service hands the stop to the person and gives no
// second turn.
func TestClosingTheServiceEndsTheMainAgentsTurn(t *testing.T) {
	f := newStopFloor(t, true)
	began := make(chan struct{})
	f.turn = func(ctx context.Context, _ factory.Item, _ factory.Wait) (string, error) {
		close(began)
		<-ctx.Done()
		return "", ctx.Err()
	}
	it := f.stopped(factory.AutonomyAuto, stopsOutOfRounds)
	f.pass()
	select {
	case <-began:
	case <-time.After(10 * time.Second):
		t.Fatal("the turn never began")
	}
	f.h.Close()
	got := f.job(it.ID)
	if got.Handoff == nil || got.Handoff.Over || got.Handoff.Turns != 1 || got.Wait.Who != factory.WaitMainAgent {
		t.Fatalf("after the close: wait %+v, handoff %+v", got.Wait, got.Handoff)
	}
}

// A PRESS THAT READ THE JOB AS IT WAS SHOWN IS TAKEN. While the main agent
// holds a stop a reader is sent a job at work, and the app sends the state it
// read with every press: a Take over pressed then is not refused as stale,
// and it takes the stop from the main agent.
func TestAPressThatReadTheShownStateIsNotStale(t *testing.T) {
	f := newStopFloor(t, true)
	f.turn = func(ctx context.Context, _ factory.Item, _ factory.Wait) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	it := f.stopped(factory.AutonomyAuto, stopsOutOfRounds)
	f.pass()
	var job api.Job
	w := stopRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "")
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &job) != nil || job.State != api.StateRunning {
		t.Fatalf("read the job: %d state %s", w.Code, job.State)
	}
	body := fmt.Sprintf(`{"mode":"take_over","expect":{"version":%d,"state":%q}}`, job.Version, job.State)
	if w := stopRequest(f.h, "POST", fmt.Sprintf("/v1/jobs/%d/control", it.ID), body); w.Code != http.StatusOK {
		t.Fatalf("take over, pressed on the job as shown: %d %s", w.Code, w.Body)
	}
	got := f.job(it.ID)
	if _, held := factory.HandedOver(got); !got.Manual || held || got.Wait.Who != factory.WaitYou {
		t.Fatalf("after the take-over: manual %v, wait %+v", got.Manual, got.Wait)
	}
	// A press that names a state the job was never in is still stale.
	stale := fmt.Sprintf(`{"mode":"resume_pipeline","expect":{"version":%d,"state":"queued"}}`, job.Version)
	if w := stopRequest(f.h, "POST", fmt.Sprintf("/v1/jobs/%d/control", it.ID), stale); w.Code != http.StatusConflict {
		t.Fatalf("a press on a state the job was never in: %d %s", w.Code, w.Body)
	}
}
