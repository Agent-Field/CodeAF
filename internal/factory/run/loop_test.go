package run

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// rig is a runner over a temp store with fake executors and one bench.
type rig struct {
	t      *testing.T
	st     *store.Store
	r      *Runner
	events chan Event
}

func newRig(t *testing.T, exec map[factory.StageKind]Executor, tweak func(*Options)) *rig {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan Event, 1000)
	opts := Options{Store: st, Exec: exec, Benches: 1, Events: events}
	if tweak != nil {
		tweak(&opts)
	}
	g := &rig{t: t, st: st, r: New(opts), events: events}
	t.Cleanup(func() {
		// Stop whatever is still in flight so no goroutine outlives the
		// test's temp directory.
		items, _ := st.List()
		for _, it := range items {
			_ = g.r.Stop(it.ID)
		}
		lp := g.r.loop()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
			lp.mu.Lock()
			n := len(lp.ctls)
			lp.mu.Unlock()
			if n == 0 {
				break
			}
		}
	})
	return g
}

func (g *rig) add(title string, stages ...factory.Stage) int {
	g.t.Helper()
	for i := range stages {
		stages[i].On = true
	}
	id, err := g.st.Add(context.Background(), factory.Item{Title: title, Repo: "acme/api", Stages: stages})
	if err != nil {
		g.t.Fatal(err)
	}
	return id
}

// wait polls the store until the item satisfies ok, and fails after a while.
func (g *rig) wait(id int, what string, ok func(factory.Item) bool) factory.Item {
	g.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		it, err := g.st.Get(id)
		if err == nil && ok(it) {
			return it
		}
		if time.Now().After(deadline) {
			g.t.Fatalf("item %d never became %s; it is %s, asking %q", id, what, it.State, it.Question)
		}
		time.Sleep(3 * time.Millisecond)
	}
}

func (g *rig) waitState(id int, s factory.State) factory.Item {
	g.t.Helper()
	return g.wait(id, string(s), func(it factory.Item) bool { return it.State == s })
}

// asked waits for the item to be asking exactly q.
func (g *rig) asked(id int, q string) factory.Item {
	g.t.Helper()
	return g.wait(id, "asking "+q, func(it factory.Item) bool {
		return it.State == factory.StateNeedsYou && it.Question == q
	})
}

func logHas(it factory.Item, text string) bool {
	if it.Stream == nil {
		return false
	}
	for _, l := range it.Stream.Log {
		if l.Text == text {
			return true
		}
	}
	return false
}

func chat(name string) factory.Stage {
	return factory.Stage{Name: name, Kind: factory.StageChat, Ask: "do " + name, Until: "done"}
}

// approveStep is an approve step; last in a run, it is the landing.
func approveStep() factory.Stage {
	return factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate}
}

func done(out string) factory.StageResult { return factory.StageResult{Done: true, Output: out} }

func TestLaunchRunsStagesInOrderAndLands(t *testing.T) {
	var mu sync.Mutex
	var ran []string
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			mu.Lock()
			ran = append(ran, job.Stage.Name)
			mu.Unlock()
			if job.Round != 1 {
				t.Errorf("%s ran round %d", job.Stage.Name, job.Round)
			}
			return done(job.Stage.Name + " went fine\nmore"), nil
		}),
	}, nil)
	skipped := chat("security")
	skipped.When = "large"
	id := g.add("fix it", chat("plan"), chat("write"), skipped, chat("review"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	mu.Lock()
	got := strings.Join(ran, ",")
	mu.Unlock()
	if got != "plan,write,review" {
		t.Fatalf("ran %s", got)
	}
	s := it.Stream
	if len(s.Phases) != 3 {
		t.Fatalf("a stage that does not fit became a phase: %+v", s.Phases)
	}
	for _, ph := range s.Phases {
		if ph.State != factory.PhaseDone || ph.Round != 1 {
			t.Fatalf("phase %s is %s round %d", ph.Name, ph.State, ph.Round)
		}
	}
	if s.Bench != 1 || s.Started.IsZero() || s.Ended.IsZero() {
		t.Fatalf("stream = bench %d, started %v, ended %v", s.Bench, s.Started, s.Ended)
	}
	if !logHas(it, "write: write went fine") || !logHas(it, "landed · proof sheet ready · your approval") {
		t.Fatalf("log = %+v", s.Log)
	}
	if err := g.r.Launch(id); err == nil || !strings.Contains(err.Error(), "has landed") {
		t.Fatalf("a landed item launched again: %v", err)
	}
}

func TestLaunchRefusesWhatIsAlreadyRunningAndAStrangersWrite(t *testing.T) {
	release := make(chan struct{})
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return done(""), nil
		}),
	}, nil)
	id := g.add("one", chat("plan"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err == nil || err.Error() != "#1 is already running" {
		t.Fatalf("second launch = %v", err)
	}
	close(release)
	g.waitState(id, factory.StateLanded)

	sid, err := g.st.Add(context.Background(), factory.Item{Title: "theirs", Tier: factory.TierStranger, Stages: []factory.Stage{{Name: "write", On: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(sid); err == nil || err.Error() != "a stranger's work does not run write on this floor" {
		t.Fatalf("a stranger's write launched: %v", err)
	}
}

func TestTheRailRefusesLaunch(t *testing.T) {
	g := newRig(t, nil, func(o *Options) {
		o.Rail = func() float64 { return 60 }
		o.SpentToday = func() float64 { return 60.5 }
	})
	id := g.add("one", chat("plan"))
	err := g.r.Launch(id)
	if err == nil || err.Error() != "the day rail is $60 and today's spend has reached it" {
		t.Fatalf("launch past the rail = %v", err)
	}
	if it, _ := g.st.Get(id); it.State != factory.StateNew || it.Stream != nil {
		t.Fatalf("a refused launch moved the item: %+v", it)
	}
}

// AN APPROVE STEP HOLDS THE RUN for the person: yes goes on, words go on
// with them in the next step's brief, no sends the run back to the step
// before with the words, and an approve with nothing before it stops on no.
func TestAnApproveStepHoldsYesGoesOnNoSendsBack(t *testing.T) {
	var mu sync.Mutex
	var briefs []string
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			mu.Lock()
			briefs = append(briefs, fmt.Sprintf("%s/%d:%s", job.Stage.Name, job.Round, strings.Join(job.Notes, ";")))
			mu.Unlock()
			return done(""), nil
		}),
	}, func(o *Options) { o.Benches = 0 })
	approve := factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate}

	yes := g.add("yes", chat("plan"), approve, chat("write"))
	if err := g.r.Launch(yes); err != nil {
		t.Fatal(err)
	}
	it := g.asked(yes, "plan is ready · continue, or send it back?")
	if it.QKind != factory.QKindApprove || it.Stream.Phases[1].State != factory.PhaseWaiting || it.State != factory.StateNeedsYou {
		t.Fatalf("approve = %q, phase %s, state %s", it.QKind, it.Stream.Phases[1].State, it.State)
	}
	// NO SENDS IT BACK: plan runs a second round with the words, then the
	// approve step holds again.
	if err := g.r.Answer(yes, false, "use the other file"); err != nil {
		t.Fatal(err)
	}
	it = g.wait(yes, "plan sent back and held again", func(it factory.Item) bool {
		return it.State == factory.StateNeedsYou && it.Stream != nil && it.Stream.Phases[0].Round == 2
	})
	if !logHas(it, "sent back to plan: use the other file") {
		t.Fatalf("log = %+v", it.Stream.Log)
	}
	// WORDS WITH A YES GO ON, and the next step reads them.
	if err := g.r.Answer(yes, true, "only the parser"); err != nil {
		t.Fatal(err)
	}
	g.waitState(yes, factory.StateLanded)
	mu.Lock()
	got := strings.Join(briefs, " | ")
	mu.Unlock()
	if !strings.Contains(got, "plan/2:use the other file") || !strings.Contains(got, "write/1:use the other file;only the parser") {
		t.Fatalf("briefs = %s", got)
	}
	if err := g.r.Answer(yes, true, ""); err == nil || err.Error() != "#1 is not waiting on you" {
		t.Fatalf("an answer to nothing = %v", err)
	}

	first := g.add("first", approve, chat("write"))
	if err := g.r.Launch(first); err != nil {
		t.Fatal(err)
	}
	g.asked(first, "ready to start · continue?")
	if err := g.r.Answer(first, false, ""); err != nil {
		t.Fatal(err)
	}
	it = g.waitState(first, factory.StateNew)
	if it.Stream == nil || !logHas(it, "stopped · branch kept") || it.Question != "" {
		t.Fatalf("a no with nothing before did not stop: %+v", it)
	}

	// AN APPROVE STEP WITH NOTHING AFTER IT IS THE LANDING: no question
	// before it, and the sheet waits for the person's approval.
	last := g.add("last", chat("plan"), approve)
	if err := g.r.Launch(last); err != nil {
		t.Fatal(err)
	}
	it = g.waitState(last, factory.StateLanded)
	if logHas(it, "plan is ready · continue, or send it back?") {
		t.Fatalf("the last approve asked before landing: %+v", it.Stream.Log)
	}
}

// AN ITEM WITH NO APPROVE STEP SHIPS ITSELF ON GREEN PROOF; one with an
// approve step waits for the approval.
func TestNoApproveStepShipsOnGreenProof(t *testing.T) {
	exec := map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			res := done("")
			if job.Stage.Name == "proof" {
				res.Claims = []factory.Claim{{Text: "it works", OK: true, Evidence: "a test", Medium: "test"}}
			}
			return res, nil
		}),
	}
	g := newRig(t, exec, nil)
	self := g.add("self", chat("write"), factory.Stage{Name: "proof", Kind: factory.StageChat, Ask: "show it", Until: "proven"})
	if err := g.r.Launch(self); err != nil {
		t.Fatal(err)
	}
	g.waitState(self, factory.StateShipped)
	held := g.add("held", chat("write"), factory.Stage{Name: "proof", Kind: factory.StageChat, Ask: "show it", Until: "proven"}, factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate})
	if err := g.r.Launch(held); err != nil {
		t.Fatal(err)
	}
	g.waitState(held, factory.StateLanded)
}

func TestUntilCleanWithMaxTwoLoopsTwiceThenAsks(t *testing.T) {
	var calls atomic.Int32
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			if job.Stage.Name != "review" {
				return done(""), nil
			}
			if int(calls.Add(1)) != job.Round {
				t.Errorf("round %d on call %d", job.Round, calls.Load())
			}
			return factory.StageResult{Done: true, Findings: 2}, nil
		}),
	}, nil)
	review := chat("review")
	review.Until, review.Max = "clean", 2
	id := g.add("tidy", review, chat("proof"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	q := "review is not clean after 2 rounds: 2 findings · one more round, or go on as is?"
	it := g.asked(id, q)
	if calls.Load() != 2 || it.QKind != "scope" {
		t.Fatalf("calls %d, kind %q", calls.Load(), it.QKind)
	}
	if ph := it.Stream.Phases[0]; ph.State != factory.PhaseFailed || ph.Note != "review is not clean after 2 rounds" || ph.Round != 2 {
		t.Fatalf("phase = %+v", ph)
	}
	if err := g.r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	g.asked(id, "review is not clean after 3 rounds: 2 findings · one more round, or go on as is?")
	if calls.Load() != 3 {
		t.Fatalf("one more round ran %d calls", calls.Load())
	}
	if err := g.r.Answer(id, false, ""); err != nil {
		t.Fatal(err)
	}
	it = g.waitState(id, factory.StateLanded)
	if it.Stream.Phases[0].State != factory.PhaseDone || !logHas(it, "going on as is · review is not clean after 3 rounds") {
		t.Fatalf("going on = %+v", it.Stream)
	}
}

func TestAFailedExecutorAsksToSkipOrStop(t *testing.T) {
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			if job.Stage.Name == "write" {
				return factory.StageResult{}, errors.New("the worktree is gone\nstack")
			}
			return done(""), nil
		}),
	}, nil)
	id := g.add("one", chat("write"), chat("review"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.asked(id, "write did not finish: the worktree is gone · skip it, or stop?")
	if err := g.r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if it.Stream.Phases[0].State != factory.PhaseFailed || it.Stream.Phases[1].State != factory.PhaseDone {
		t.Fatalf("phases = %+v", it.Stream.Phases)
	}

	// A kind nothing runs fails its stage with a sentence, never a panic.
	check := factory.Stage{Name: "test", Kind: factory.StageCheck, Ask: "go test ./...", Until: "green"}
	id = g.add("two", check, chat("review"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it = g.asked(id, "test cannot run here · skip it, or stop?")
	if it.Stream.Phases[0].Note != "codeaf cannot run a check stage here" {
		t.Fatalf("note = %q", it.Stream.Phases[0].Note)
	}
	if err := g.r.Answer(id, false, ""); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateNew)
}

func TestThePlanEditIsAppliedUnderAdaptAndAskedUnderAsk(t *testing.T) {
	var mu sync.Mutex
	var ran []string
	exec := map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			mu.Lock()
			ran = append(ran, job.Stage.Name)
			mu.Unlock()
			res := done("")
			if job.Stage.Name == "plan" && job.Round == 1 {
				res.Edit = &factory.PlanEdit{Add: []factory.Stage{{Ask: "after write, neaten: make it neater"}}, Skip: []string{"review"}, Why: "small change"}
			}
			return res, nil
		}),
	}
	g := newRig(t, exec, nil)
	id := g.add("adapt", chat("plan"), chat("write"), chat("review"), chat("proof"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	mu.Lock()
	got := strings.Join(ran, ",")
	ran = nil
	mu.Unlock()
	if got != "plan,write,neaten,proof" {
		t.Fatalf("ran %s", got)
	}
	if strings.Join(it.Adapted, "|") != "plan added neaten|plan skipped review|why: small change" {
		t.Fatalf("adapted = %q", it.Adapted)
	}

	ask := newRig(t, exec, func(o *Options) {
		o.Recipe = func(string) factory.Recipe {
			r := factory.DefaultRecipe()
			r.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindIssue: factory.AdaptAsk}
			return r
		}
	})
	id = ask.add("ask", chat("plan"), chat("write"), chat("review"), chat("proof"))
	if err := ask.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	// UNDER ASK, PLAN'S CHANGE PUTS AN APPROVE STEP AFTER PLAN, and the run
	// holds there before anything else runs.
	it = ask.asked(id, "plan is ready · continue, or send it back?")
	if it.QKind != factory.QKindApprove || it.Stages[1].Name != factory.ApproveName {
		t.Fatalf("kind %q stages %+v", it.QKind, it.Stages)
	}
	mu.Lock()
	if strings.Join(ran, ",") != "plan" {
		t.Fatalf("something ran before the person said go: %v", ran)
	}
	mu.Unlock()
	if err := ask.r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	ask.waitState(id, factory.StateLanded)
}

func TestStopMidRound(t *testing.T) {
	cut := make(chan struct{})
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			<-ctx.Done()
			close(cut)
			return factory.StageResult{}, ctx.Err()
		}),
	}, nil)
	id := g.add("one", chat("write"), chat("review"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.wait(id, "in its round", func(it factory.Item) bool {
		return it.Stream != nil && len(it.Stream.Phases) > 0 && it.Stream.Phases[0].State == factory.PhaseRunning
	})
	if err := g.r.Stop(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cut:
	case <-time.After(5 * time.Second):
		t.Fatal("the round's ctx was never cancelled")
	}
	it := g.waitState(id, factory.StateNew)
	if it.Stream == nil || it.Stream.Phases[0].State != factory.PhaseFailed || !logHas(it, "stopped · branch kept") {
		t.Fatalf("stopped stream = %+v", it.Stream)
	}
	if err := g.r.Stop(id); err == nil || err.Error() != "#1 is not running" {
		t.Fatalf("a second stop = %v", err)
	}
	// The stream is kept for the log, and the item launches again from new.
	time.Sleep(20 * time.Millisecond)
	if again, _ := g.st.Get(id); again.State != factory.StateNew {
		t.Fatalf("a stopped round wrote after the stop: %s", again.State)
	}
}

func TestPauseHoldsTheRoundAndResumeStartsItOver(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	rounds := make(chan int, 4)
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			calls.Add(1)
			rounds <- job.Round
			select {
			case <-ctx.Done():
				return factory.StageResult{}, ctx.Err()
			case <-release:
				return done(""), nil
			}
		}),
	}, nil)
	id := g.add("one", chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	if r := <-rounds; r != 1 {
		t.Fatalf("first round %d", r)
	}
	if err := g.r.Pause(id); err != nil {
		t.Fatal(err)
	}
	it := g.wait(id, "paused", func(it factory.Item) bool { return it.Stream != nil && it.Stream.Paused })
	if !logHas(it, "paused") {
		t.Fatalf("log = %+v", it.Stream.Log)
	}
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("a paused item ran %d rounds", calls.Load())
	}
	if err := g.r.Pause(id); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-rounds:
		if r != 1 {
			t.Fatalf("a resumed round started at %d", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resume never ran the round again")
	}
	close(release)
	it = g.waitState(id, factory.StateLanded)
	if it.Stream.Paused || !logHas(it, "resumed") {
		t.Fatalf("resumed stream = %+v", it.Stream)
	}
}

func TestSteerReachesTheRoundAndTheNextNotes(t *testing.T) {
	heard := make(chan string, 1)
	notes := make(chan []string, 1)
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			if job.Round == 1 {
				select {
				case w := <-job.Steer:
					heard <- w
				case <-ctx.Done():
					return factory.StageResult{}, ctx.Err()
				}
				return factory.StageResult{Done: true, Findings: 1, Notes: []string{"two callers"}}, nil
			}
			notes <- job.Notes
			return done(""), nil
		}),
	}, nil)
	st := chat("review")
	st.Until, st.Max = "clean", 2
	id := g.add("one", st)
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.wait(id, "running", func(it factory.Item) bool { return it.State == factory.StateRunning })
	if err := g.r.Steer(id, "use the queue"); err != nil {
		t.Fatal(err)
	}
	if w := <-heard; w != "use the queue" {
		t.Fatalf("the round heard %q", w)
	}
	got := <-notes
	if strings.Join(got, "|") != "use the queue|two callers" {
		t.Fatalf("the next round's notes = %q", got)
	}
	it := g.waitState(id, factory.StateLanded)
	if !logHas(it, "steer: use the queue") {
		t.Fatalf("log = %+v", it.Stream.Log)
	}
}

func TestSignOffShipsACleanSheetAndRefusesAFailedClaim(t *testing.T) {
	ok := map[int]bool{}
	var mu sync.Mutex
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageCheck: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			mu.Lock()
			good := ok[job.Item.ID]
			mu.Unlock()
			return factory.StageResult{Done: true, Claims: []factory.Claim{
				{Text: "the tests pass", OK: good, Evidence: "go test", Medium: "test"},
				{Text: "no new dependencies", OK: true, Evidence: "go.mod unchanged", Medium: "policy"},
			}}, nil
		}),
	}, func(o *Options) { o.Benches = 0 })
	check := factory.Stage{Name: "test", Kind: factory.StageCheck, Ask: "go test ./..."}

	bad := g.add("bad", check, approveStep())
	if err := g.r.Launch(bad); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(bad, factory.StateLanded)
	if len(it.Proof) != 1 || len(it.Policy) != 1 {
		t.Fatalf("proof %+v policy %+v", it.Proof, it.Policy)
	}
	if _, err := g.r.SignOff(bad, false); err == nil || err.Error() != "#1 has a claim not shown" {
		t.Fatalf("a failed claim signed off: %v", err)
	}
	if due, err := g.r.SignOff(bad, true); err != nil || due {
		t.Fatalf("an edited sign-off = %v, %v", due, err)
	}
	g.waitState(bad, factory.StateShipped)

	// Three clean sign-offs on one repo offer a habit.
	var dues []bool
	for i := 0; i < 3; i++ {
		id := g.add("good", check, approveStep())
		mu.Lock()
		ok[id] = true
		mu.Unlock()
		if err := g.r.Launch(id); err != nil {
			t.Fatal(err)
		}
		g.waitState(id, factory.StateLanded)
		due, err := g.r.SignOff(id, false)
		if err != nil {
			t.Fatal(err)
		}
		dues = append(dues, due)
	}
	if dues[0] || dues[1] || !dues[2] {
		t.Fatalf("habit due = %v", dues)
	}
	if _, err := g.r.SignOff(bad, false); err == nil || err.Error() != "#1 has not landed" {
		t.Fatalf("a shipped item signed off again: %v", err)
	}
}

func TestSendBackAppendsProveAndLandsAgain(t *testing.T) {
	asks := make(chan string, 8)
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			asks <- job.Stage.Name + ": " + job.Stage.Ask
			return done(""), nil
		}),
	}, nil)
	id := g.add("one", chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateLanded)
	<-asks
	if err := g.r.SendBack(id, "prove restart survival"); err != nil {
		t.Fatal(err)
	}
	if a := <-asks; a != "prove: prove restart survival" {
		t.Fatalf("the sent-back round was %q", a)
	}
	it := g.wait(id, "landed again", func(it factory.Item) bool {
		return it.State == factory.StateLanded && len(it.Stream.Phases) == 2 && it.Stream.Phases[1].State == factory.PhaseDone
	})
	if it.Stream.Phases[1].Name != "prove" || !logHas(it, "changes requested: prove restart survival") {
		t.Fatalf("stream = %+v", it.Stream)
	}
	if err := g.r.SendBack(id, "and again"); err != nil {
		t.Fatal(err)
	}
	if a := <-asks; a != "prove2: and again" {
		t.Fatalf("the second send-back was %q", a)
	}
	g.wait(id, "landed a third time", func(it factory.Item) bool {
		return it.State == factory.StateLanded && len(it.Stream.Phases) == 3
	})
}

func TestReverifyRunsTheChecksAgain(t *testing.T) {
	var calls atomic.Int32
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			return done(""), nil
		}),
		factory.StageCheck: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			n := calls.Add(1)
			return factory.StageResult{Done: true, Exit: 0, Claims: []factory.Claim{{Text: "the tests pass", OK: n > 1, Evidence: "go test", Medium: "test"}}}, nil
		}),
	}, nil)
	id := g.add("one", chat("write"), factory.Stage{Name: "test", Kind: factory.StageCheck, Until: "green"}, chat("proof"), approveStep())
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if len(it.Proof) != 1 || it.Proof[0].OK {
		t.Fatalf("first sheet = %+v", it.Proof)
	}
	if err := g.r.Reverify(id); err != nil {
		t.Fatal(err)
	}
	it = g.wait(id, "checked again", func(it factory.Item) bool {
		return it.State == factory.StateLanded && len(it.Proof) == 1 && it.Proof[0].OK
	})
	if calls.Load() != 2 || !logHas(it, "checking again") {
		t.Fatalf("calls %d log %+v", calls.Load(), it.Stream.Log)
	}
}

func TestTwoLaunchesWithOneBenchQueueTheSecond(t *testing.T) {
	release := make(chan struct{})
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			if job.Item.Title == "first" {
				select {
				case <-release:
				case <-ctx.Done():
					return factory.StageResult{}, ctx.Err()
				}
			}
			return done(""), nil
		}),
	}, nil)
	a := g.add("first", chat("write"))
	b := g.add("second", chat("write"))
	if err := g.r.Launch(a); err != nil {
		t.Fatal(err)
	}
	g.waitState(a, factory.StateRunning)
	if err := g.r.Launch(b); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if it, _ := g.st.Get(b); it.State != factory.StateQueued {
		t.Fatalf("the second item is %s with the bench taken", it.State)
	}
	close(release)
	g.waitState(a, factory.StateLanded)
	it := g.waitState(b, factory.StateLanded)
	if it.Stream.Bench != 1 {
		t.Fatalf("the second took bench %d", it.Stream.Bench)
	}
}

func TestEventsArriveInOrder(t *testing.T) {
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			return done(""), nil
		}),
	}, nil)
	id := g.add("one", chat("plan"), chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	var got []string
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-g.events:
			if ev.Item != id {
				t.Fatalf("event for %d", ev.Item)
			}
			got = append(got, string(ev.Kind)+":"+ev.Stage)
		case <-deadline:
			t.Fatalf("events so far %v", got)
		}
		if len(got) > 0 && strings.HasPrefix(got[len(got)-1], "landed") {
			break
		}
	}
	want := "queued:,started:plan,done:plan,started:write,done:write,landed:"
	if strings.Join(got, ",") != want {
		t.Fatalf("events = %v", got)
	}
}

func TestTheCapAsksAndYesRaisesIt(t *testing.T) {
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			res := done("")
			res.Spent = 1.5
			return res, nil
		}),
	}, nil)
	id, err := g.st.Add(context.Background(), factory.Item{Title: "pricey", Cap: 1, Stages: []factory.Stage{chat("write")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.st.Update(id, func(it *factory.Item) error { it.Stages[0].On = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.asked(id, "budget of $1 reached · $1 more, or stop?")
	if it.QKind != "cap" || it.Stream.Spent != 1.5 {
		t.Fatalf("kind %q spent %v", it.QKind, it.Stream.Spent)
	}
	if err := g.r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	it = g.waitState(id, factory.StateLanded)
	if it.Cap != 2 || !logHas(it, "budget raised to $2 · carrying on") {
		t.Fatalf("cap %v log %+v", it.Cap, it.Stream.Log)
	}
}

// TestARoundCountIsSaidTheWayAPersonSaysIt pins the stop note's count: `after 1
// round` and `after 2 rounds`, never `round(s)`, and a shortfall of one
// finding is `1 finding`.
func TestARoundCountIsSaidTheWayAPersonSaysIt(t *testing.T) {
	for n, want := range map[int]string{1: "1 round", 2: "2 rounds", 3: "3 rounds"} {
		if got := roundsWord(n); got != want {
			t.Errorf("roundsWord(%d) = %q, want %q", n, got, want)
		}
	}
	if got := shortfall(factory.StageResult{Done: true, Findings: 1}); got != "1 finding" {
		t.Errorf("one finding reads %q", got)
	}
}

// TestTheProofSheetIsTheProofStagesAndTheChecksOnce holds the sheet to its
// rules: only the proof stage and the checks put rows on it, a row is one
// claim however its case and punctuation came, a claim with no evidence is
// not shown, and a later claim with evidence replaces an earlier one without.
// The hand run of 2026-10-08 landed 27 rows with repeats.
func TestTheProofSheetIsTheProofStagesAndTheChecksOnce(t *testing.T) {
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			switch job.Stage.Name {
			case "write":
				return factory.StageResult{Done: true, Claims: []factory.Claim{{Text: "Refunds count once.", OK: true}}}, nil
			case "proof":
				return factory.StageResult{Done: true, Claims: []factory.Claim{
					{Text: "Refunds count once.", OK: true},
					{Text: "the export matches march", OK: true, Evidence: "export.csv diff", Medium: "transcript"},
				}}, nil
			}
			return done(""), nil
		}),
		factory.StageCheck: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			return factory.StageResult{Done: true, Claims: []factory.Claim{{Text: "refunds count ONCE", OK: true, Evidence: "TestRefund", Medium: "test"}}}, nil
		}),
	}, nil)
	id := g.add("ledger",
		chat("write"),
		factory.Stage{Name: "proof", Kind: factory.StageChat, Ask: "show each claim", Until: "done"},
		factory.Stage{Name: "test", Kind: factory.StageCheck, Ask: "go test ./..."},
		approveStep(),
	)
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if !logHas(it, "claimed: Refunds count once.") {
		t.Fatalf("write's claim did not reach the log: %+v", it.Stream.Log)
	}
	if len(it.Proof) != 2 {
		t.Fatalf("the sheet has %d rows, want 2: %+v", len(it.Proof), it.Proof)
	}
	if c := it.Proof[0]; !c.OK || c.Evidence != "TestRefund" || c.Text != "refunds count ONCE" {
		t.Fatalf("the check's evidence did not replace the bare claim: %+v", c)
	}
	if c := it.Proof[1]; !c.OK || c.Evidence != "export.csv diff" {
		t.Fatalf("row 2 = %+v", c)
	}
}

func TestAClaimWithNoEvidenceIsNotShown(t *testing.T) {
	sheet := mergeClaim(nil, factory.Claim{Text: "it is fast", OK: true})
	if len(sheet) != 1 || sheet[0].OK || sheet[0].Evidence != "no evidence given" {
		t.Fatalf("a bare claim = %+v", sheet)
	}
	sheet = mergeClaim(sheet, factory.Claim{Text: "It is fast!", OK: true, Evidence: "bench 2x"})
	sheet = mergeClaim(sheet, factory.Claim{Text: "it is FAST", OK: true})
	if len(sheet) != 1 || !sheet[0].OK || sheet[0].Evidence != "bench 2x" {
		t.Fatalf("a later bare claim undid the evidence: %+v", sheet)
	}
}

// pauseExec is a chat executor that opens a chat of its own name each round,
// tells the loop, and waits to be cut or released. It keeps every job it saw.
type pauseExec struct {
	mu      sync.Mutex
	jobs    []Job
	started chan struct{}
	release chan struct{}
}

func newPauseExec() *pauseExec {
	return &pauseExec{started: make(chan struct{}, 8), release: make(chan struct{})}
}

func (p *pauseExec) Run(ctx context.Context, job Job) (factory.StageResult, error) {
	p.mu.Lock()
	p.jobs = append(p.jobs, job)
	n := len(p.jobs)
	p.mu.Unlock()
	chat := job.Resume
	if chat == "" {
		chat = "chat-" + strings.Repeat("x", n)
	}
	if job.Opened != nil {
		job.Opened(chat)
	}
	p.started <- struct{}{}
	select {
	case <-ctx.Done():
		return factory.StageResult{}, ctx.Err()
	case <-p.release:
		return factory.StageResult{Done: true, Chat: chat}, nil
	}
}

func (p *pauseExec) job(i int) Job {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.jobs[i]
}

func waitStarted(t *testing.T, p *pauseExec) {
	t.Helper()
	select {
	case <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the round never started")
	}
}

func TestPauseKeepsTheChatAndRunCarriesOnInIt(t *testing.T) {
	p := newPauseExec()
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: p}, nil)
	id := g.add("one", chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	if j := p.job(0); j.Resume != "" {
		t.Fatalf("a first round resumed %q", j.Resume)
	}
	if err := g.r.Pause(id); err != nil {
		t.Fatal(err)
	}
	it := g.wait(id, "paused with its chat", func(it factory.Item) bool {
		return it.Stream != nil && it.Stream.Paused && it.Stream.Phases[0].Chat == "chat-x"
	})
	if it.Stream.Phases[0].State != factory.PhaseRunning {
		t.Fatalf("the paused step is %s", it.Stream.Phases[0].State)
	}
	// Launch on the paused item is run: the same round, the same chat.
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	j := p.job(1)
	if j.Resume != "chat-x" || j.Round != 1 {
		t.Fatalf("resumed job = round %d resume %q", j.Round, j.Resume)
	}
	close(p.release)
	it = g.waitState(id, factory.StateLanded)
	if it.Stream.Phases[0].Chat != "chat-x" || it.Stream.Paused {
		t.Fatalf("landed stream = %+v", it.Stream)
	}
}

func TestPausedBeforeTheRoundOpenedCarriesNothing(t *testing.T) {
	p := newPauseExec()
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: p}, nil)
	id := g.add("one", chat("plan"), chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	p.release <- struct{}{}
	// plan reports; write starts. Pause it twice over: cut, hold, run.
	waitStarted(t, p)
	if err := g.r.Pause(id); err != nil {
		t.Fatal(err)
	}
	g.wait(id, "paused", func(it factory.Item) bool { return it.Stream != nil && it.Stream.Paused })
	if err := g.r.Pause(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	if j := p.job(2); j.Resume != "chat-xx" {
		t.Fatalf("write carried on %q, want the chat it opened", j.Resume)
	}
	close(p.release)
	g.waitState(id, factory.StateLanded)
}

func TestAPausedItemStaysPausedAcrossARestartAndRunsInTheSameChat(t *testing.T) {
	p := newPauseExec()
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: p}, nil)
	id := g.add("one", chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	if err := g.r.Pause(id); err != nil {
		t.Fatal(err)
	}
	g.wait(id, "paused", func(it factory.Item) bool { return it.Stream != nil && it.Stream.Paused })
	// The process dies: its control goes, the document stays.
	old := g.r.loop().ctl(id)
	old.cancel()
	time.Sleep(20 * time.Millisecond)
	it, _ := g.st.Get(id)
	if it.State != factory.StateRunning || !it.Stream.Paused || it.Stream.Phases[0].Chat != "chat-x" {
		t.Fatalf("after the restart the item is %s, stream %+v", it.State, it.Stream)
	}

	p2 := newPauseExec()
	r2 := New(Options{Store: g.st, Exec: map[factory.StageKind]Executor{factory.StageChat: p2}, Benches: 1, Events: make(chan Event, 100)})
	if err := r2.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p2)
	if j := p2.job(0); j.Resume != "chat-x" || j.Round != 1 {
		t.Fatalf("after the restart: round %d resume %q", j.Round, j.Resume)
	}
	close(p2.release)
	it = g.waitState(id, factory.StateLanded)
	if it.Stream.Paused {
		t.Fatal("still paused after it landed")
	}
}

// WHAT CAME BEFORE OUTLIVES A RESTART: a step resumed in a new process is
// still handed the results and notes of the steps that finished before it.
func TestAResumedStepAfterARestartStillReadsTheStepsBefore(t *testing.T) {
	p := newPauseExec()
	plan := ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		if job.Stage.Name == "plan" {
			return factory.StageResult{Done: true, Output: "three files", Notes: []string{"keep the old flag"}}, nil
		}
		return p.Run(ctx, job)
	})
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: plan}, nil)
	id := g.add("one", chat("plan"), chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	if err := g.r.Pause(id); err != nil {
		t.Fatal(err)
	}
	g.wait(id, "paused", func(it factory.Item) bool { return it.Stream != nil && it.Stream.Paused })
	g.r.loop().ctl(id).cancel()
	time.Sleep(20 * time.Millisecond)
	if it, _ := g.st.Get(id); it.Stream.Phases[0].Result == nil || it.Stream.Phases[0].Result.Output != "three files" {
		t.Fatalf("the plan's result is not kept on its step: %+v", it.Stream.Phases[0])
	}

	p2 := newPauseExec()
	r2 := New(Options{Store: g.st, Exec: map[factory.StageKind]Executor{factory.StageChat: p2}, Benches: 1, Events: make(chan Event, 100)})
	if err := r2.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p2)
	j := p2.job(0)
	if len(j.Prior) != 1 || j.Prior[0].Output != "three files" {
		t.Fatalf("the resumed write's prior is %+v", j.Prior)
	}
	if !slices.Contains(j.Notes, "keep the old flag") {
		t.Fatalf("the resumed write's notes are %q", j.Notes)
	}
	close(p2.release)
	g.waitState(id, factory.StateLanded)
}

func TestRunOnAnItemNobodyPausedIsRefused(t *testing.T) {
	p := newPauseExec()
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: p}, nil)
	id := g.add("one", chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	if err := g.r.Launch(id); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("launch on a running item = %v", err)
	}
	close(p.release)
	g.waitState(id, factory.StateLanded)
}
