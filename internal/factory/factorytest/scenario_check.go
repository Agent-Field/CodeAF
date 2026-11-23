package factorytest

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// THE PROPERTIES, held after every event of every scenario:
//
//  1. The stored wait is valid ([factory.WaitFault]).
//  2. A job that waits on its person offers them something, every action it
//     offers them is accepted by that action's own door, and the action the
//     wait names moves the job off that wait.
//  3. A job that waits on GitHub is one the poller reads, with a time by which
//     it must have been read.
//  4. A job that waits on a runner or on its manager has a deadline.
//  5. The wait's clock moves only when the wait does.
//
// A press is made on a copy of the factory's folder, so the scenario itself
// goes on from where it stood.

// Check holds the properties on the job as it stands, and fails the test with
// the scenario's story when one does not hold.
func (s *Scenario) Check() {
	s.T.Helper()
	if faults := s.Faults(); len(faults) > 0 {
		s.T.Fatalf("the job is idle without an owner and an action that will work:\n  %s\n%s", strings.Join(faults, "\n  "), s.Story())
	}
}

// Faults is every property that does not hold on the job as it stands.
func (s *Scenario) Faults() []string {
	s.T.Helper()
	it := s.Item()
	var faults []string
	s.mu.Lock()
	faults = append(faults, s.clockFaults...)
	s.mu.Unlock()
	if err := factory.WaitFault(it); err != nil {
		return append(faults, err.Error())
	}
	w := it.Wait
	switch w.Who {
	case factory.WaitYou:
		faults = append(faults, s.personFaults(it)...)
	case factory.WaitGitHub:
		if why := unread(it); why != "" {
			faults = append(faults, "waits on GitHub, and the poller would not read it: "+why)
		}
		if w.Deadline.IsZero() {
			faults = append(faults, "waits on GitHub with no time by which it must be read")
		}
	case factory.WaitRunner, factory.WaitMainAgent:
		if w.Deadline.IsZero() {
			faults = append(faults, "waits on "+w.Who+" with no deadline")
		}
		// THE PERSON MAY STILL ACT WHILE THE MAIN AGENT HOLDS A STOP: every
		// action the job offers them is still taken by its door.
		if _, handed := factory.HandedOver(it); handed {
			faults = append(faults, s.personFaults(it)...)
		}
	}
	return faults
}

// unread says why the follow-up poller would pass the job by, or "". The
// poller reads every issue job that carries a numbered pull request on a
// repository that is on (github.followOpened); a job that waits on GitHub
// must also still have that pull request open.
func unread(it factory.Item) string {
	pr := it.PullRequest
	switch {
	case !factory.IsIssueJob(it):
		return "it is not an issue job"
	case it.DoneHow == "closed_locally":
		return "it is closed locally"
	case pr == nil || pr.Number <= 0:
		return "it has no pull request"
	case pr.State != factory.PROpen:
		return "its pull request is " + pr.State
	}
	if f := it.FollowUp; f != nil && f.State != "watching" && f.State != "healthy" {
		return "Follow PR is " + f.State + ", which no read moves"
	}
	return ""
}

// observe keeps the fifth property on every write the store lands: a write
// that leaves the wait the same leaves its clock where it was.
func (s *Scenario) observe(before, after factory.Item) {
	if after.ID != s.Job || before.Wait == nil || after.Wait == nil {
		return
	}
	s.stopHanded(before, after)
	if waitKey(before) == waitKey(after) && !before.Wait.Since.Equal(after.Wait.Since) {
		s.mu.Lock()
		s.clockFaults = append(s.clockFaults, fmt.Sprintf("a write left the wait at %s and moved its clock from %s to %s", waitKey(after), before.Wait.Since.Format(time.RFC3339Nano), after.Wait.Since.Format(time.RFC3339Nano)))
		s.mu.Unlock()
	}
}

// How a door answered an offered action.
const (
	pressTaken   = "taken"   // the door accepted it
	pressGuarded = "guarded" // a protective refusal: sign-in, the owner, the budget
	pressRefused = "refused" // refused for the job's state: the bug this looks for
	pressSettled = "settled" // the service moved the job before the press reached its door
)

// personFaults presses every action the job offers its person, each on its
// own copy of the factory. Every one must be taken by its door, and the one
// the wait names (the first of [factory.WaitMoves]) must move the job.
func (s *Scenario) personFaults(it factory.Item) []string {
	s.T.Helper()
	var mine []string
	for _, o := range it.Offers {
		if o.By == factory.OfferByYou {
			mine = append(mine, o.Action)
		}
	}
	if len(mine) == 0 {
		return []string{"waits on its person and offers them nothing"}
	}
	type answer struct {
		how, words string
		after      factory.Item
	}
	answers := make([]answer, len(mine))
	var wg sync.WaitGroup
	for i, action := range mine {
		wg.Add(1)
		go func() {
			defer wg.Done()
			how, words, after := s.pressOnCopy(it, action)
			answers[i] = answer{how, words, after}
		}()
	}
	wg.Wait()
	var faults []string
	named := ""
	if doors := factory.WaitMoves(it); len(doors) > 0 {
		named = doors[0]
	}
	for i, action := range mine {
		got := answers[i]
		budget := strings.Contains(strings.ToLower(got.words), "budget")
		switch got.how {
		case pressRefused:
			faults = append(faults, fmt.Sprintf("offers %s, and its door refuses: %s", action, got.words))
		case pressGuarded:
			// A guard the test cannot satisfy (a GitHub sign-in) is taken as
			// a door that would open. The budget is different: the person can
			// raise it, so the job must offer that too.
			if budget && !slices.Contains(mine, factory.DoRaiseBudget) {
				faults = append(faults, fmt.Sprintf("offers %s, which the budget refuses (%s), and does not offer a raise", action, got.words))
			}
		case pressTaken:
			// Results a runner could not save are sent again by that runner
			// when Resume pipeline tells it to; a copy has no runner, so the
			// door taking the press is all it can show.
			if it.QKind == "results_not_saved" && action == factory.DoResumePipeline {
				continue
			}
			if action == named && waitKey(got.after) == waitKey(it) && waitWords(got.after) == waitWords(it) {
				faults = append(faults, fmt.Sprintf("waits on its person to %s, and %s is taken and leaves it waiting exactly as before", it.Wait.Action, action))
			}
		}
	}
	return faults
}

func waitWords(it factory.Item) string {
	if it.Wait == nil {
		return ""
	}
	return it.Wait.Words
}

// factoryCopy is a second service over a copy of the factory's folder, with
// no runner: its doors answer exactly as the scenario's would.
type factoryCopy struct {
	store   *store.Store
	handler *serve.Handler
	clock   *Clock
}

func (s *Scenario) copyFactory() *factoryCopy {
	s.T.Helper()
	dir, err := os.MkdirTemp(s.scratch, "copy")
	if err != nil {
		s.T.Fatal(err)
	}
	// A file a writer is replacing can vanish mid-copy; the copy is taken
	// again until one pass reads every file.
	for attempt := 0; ; attempt++ {
		if err = copyDir(s.Store.Root(), dir); err == nil {
			break
		}
		if attempt == 20 {
			s.T.Fatalf("copy the factory: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	st, err := store.Open(dir)
	if err != nil {
		s.T.Fatal(err)
	}
	c := &factoryCopy{store: st, clock: &Clock{offset: s.clock.Now().Sub(time.Now())}}
	desk, err := lease.Open(st, lease.Options{Now: c.clock.Now})
	if err != nil {
		s.T.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRunner(lease.NewDoors(desk)))
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{Login: scenarioPerson}, nil
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		s.T.Fatal(err)
	}
	c.handler = serve.New(seam, serve.Config{Address: "http://test", Store: st, Record: history, Now: c.clock.Now, LeaseOptions: lease.Options{Now: c.clock.Now},
		Stopped: stillDeciding})
	// A stop the main agent holds is held on the copy too, by an agent that
	// is still deciding, so a press there is the person's press beside it.
	if cur, err := st.Get(s.Job); err == nil {
		if _, handed := factory.HandedOver(cur); handed && cur.Handoff.Turns > 0 {
			_ = st.Annotate(s.Job, func(it *factory.Item) error {
				if it.Handoff != nil {
					h := *it.Handoff
					h.Turns, h.TurnAt = 0, time.Time{}
					it.Handoff = &h
				}
				return nil
			})
		}
	}
	return c
}

func (c *factoryCopy) close() {
	c.handler.Close()
	_ = os.RemoveAll(c.store.Root())
}

func copyDir(from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
}

// pressOnCopy presses one offered action through its door on a copy of the
// factory, waits out the undo window a press opens, and answers how the door
// took it, the door's words, and the job afterwards.
func (s *Scenario) pressOnCopy(it factory.Item, action string) (how, words string, after factory.Item) {
	s.T.Helper()
	c := s.copyFactory()
	defer c.close()
	read := func() factory.Item {
		cur, err := c.store.Get(it.ID)
		if err != nil {
			s.T.Fatalf("read the copy: %v", err)
		}
		return cur
	}
	// A person's page reads the job before they press, and every request
	// settles the floor first. A job that request alone moves was waiting on
	// no door at all, only on a request (Auto's own approval): the press is
	// then of a job that has already gone on.
	press(c.handler, http.MethodGet, "/v1/about", nil)
	if moved := read(); waitKey(moved) != waitKey(it) {
		return pressSettled, "a request alone moved the job to " + waitKey(moved), moved
	}
	got, known := pressDoor(c.handler, c.store, it, action)
	if !known {
		return pressRefused, "the scenario knows no door for " + action, it
	}
	if !got.OK() {
		return refusalKind(got), got.String(), read()
	}
	// An approval and a merge are taken at once and carried out when their
	// undo window closes; the door that carries them out can still refuse.
	c.clock.Advance(factory.ApprovalUndo + factory.MergeUndo + time.Second)
	press(c.handler, http.MethodGet, "/v1/about", nil)
	// Some doors leave the job to the next read of GitHub (a raised budget
	// wakes the poller and lets its fresh read queue the work), so the copy
	// is read once, as the woken poller would read it.
	_, _ = factorygithub.PollOnce(context.Background(), s.source, c.store, "", c.clock.Now)
	press(c.handler, http.MethodGet, "/v1/about", nil)
	after = read()
	if a := after.Approval; action == factory.DoApprove && a != nil && a.State == "waiting" && a.Error != "" {
		return refusalKind(Pressed{Status: http.StatusConflict, Error: a.Error}), "the approval came back: " + a.Error, after
	}
	if m := after.Merging; action == factory.DoMerge && m != nil && m.State == factory.MergeFailed {
		return pressRefused, "the merge came back: " + m.Why, after
	}
	return pressTaken, got.String(), after
}

// pressDoor presses the door of one offered action for the job as it stands,
// with the arguments a person's press carries. known is false for an action
// the scenario has no door for.
func pressDoor(handler http.Handler, st *store.Store, it factory.Item, action string) (got Pressed, known bool) {
	post := func(verb string, body any) Pressed {
		return press(handler, http.MethodPost, fmt.Sprintf("/v1/jobs/%d/%s", it.ID, verb), body)
	}
	version, _ := st.Revision(it.ID)
	expect := &api.Expect{Version: &version, State: api.StateNeedsYou}
	yes := true
	switch action {
	case factory.DoApprove:
		return post("approve", api.Approve{Commit: it.HeadCommit}), true
	case factory.DoAnswer:
		return post("answer", answerFor(it)), true
	case factory.DoSpendAnswer:
		return post("spend_answer", api.SpendAnswer{CarryOn: &yes, Expect: &api.Expect{Version: &version}}), true
	case factory.DoRejectPlan:
		return post("reject_plan", api.RejectPlan{Words: "Name the files the plan touches.", Expect: expect}), true
	case factory.DoAcceptStep:
		return post("answer", map[string]any{"choice": factory.ChoiceAccept}), true
	case factory.DoMerge:
		return post("merge", map[string]any{}), true
	case factory.DoMarkReady:
		return post("pr-ready", map[string]any{}), true
	case factory.DoRetryPublish:
		return post("retry_publish", map[string]any{}), true
	case factory.DoPublishAnyway:
		return post("publish_anyway", map[string]any{"expect": api.Expect{Version: &version}}), true
	case factory.DoRerunStep:
		return post("steps/"+rerunStep(it)+"/rerun", map[string]any{}), true
	case factory.DoResume:
		return post("resume", nil), true
	case factory.DoResumePipeline:
		return post("control", map[string]any{"mode": "resume_pipeline"}), true
	case factory.DoRequestChanges:
		return post("request_changes", map[string]any{"words": "Show the claim the proof left out."}), true
	case factory.DoResumeAuto:
		return post(serve.ResumeAutoVerb, map[string]any{}), true
	case factory.DoRaiseBudget:
		return post("raise_budget", map[string]any{"budget": raisedBudget(it)}), true
	case factory.DoStart:
		return post("start_now", nil), true
	case factory.DoActsApprove:
		return post(api.ActsApproveVerb, map[string]any{}), true
	case factory.DoActsDecline:
		return post(api.ActsDeclineVerb, map[string]any{"words": "Not this."}), true
	case factory.DoTask:
		return post("task", api.Task{Words: "Add a note on the ledger's rounding to the README.", By: api.TaskByYou}), true
	}
	return Pressed{}, false
}

// guardedAnswers are the service's own answers for a caller who is not signed
// in or does not own the job.
var guardedAnswers = []int{http.StatusUnauthorized, http.StatusForbidden}

// refusalKind tells a protective refusal from one made for the job's state.
// Sign-in and ownership are 401 and 403; the budget says its own name.
func refusalKind(p Pressed) string {
	words := strings.ToLower(p.Error + " " + p.Body)
	switch {
	case slices.Contains(guardedAnswers, p.Status):
		return pressGuarded
	case strings.Contains(words, "budget reached") || strings.Contains(words, "raise budget") || strings.Contains(words, "choose a budget"):
		return pressGuarded
	case strings.Contains(words, "sign in"):
		return pressGuarded
	}
	return pressRefused
}

// answerFor is the answer a person who says yes gives the job's question.
func answerFor(it factory.Item) map[string]any {
	body := map[string]any{"commit": it.HeadCommit}
	switch {
	case it.QKind == factory.QKindOutOfAttempts:
		body["choice"] = factory.ChoiceTryOnceMore
	case it.QKind == "cap":
		body["choice"] = factory.ChoiceContinue
	case it.QKind == factory.QKindApprove || it.QKind == factory.QKindAskFirst || it.QKind == factory.QKindSpendStopped:
		body["yes"] = true
	default:
		body["yes"] = true
		body["words"] = "Yes. Carry on with what you proposed."
	}
	return body
}

// rerunStep is the step a rerun names: the one that failed, else the one the
// job is on.
func rerunStep(it factory.Item) string {
	if f := it.Failure; f != nil && f.Step != "" {
		return f.Step
	}
	if s := it.Stream; s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) {
		return s.Phases[s.Cur].Name
	}
	return "none"
}

// raisedBudget is the next budget above both the job's budget and what it has
// spent, of those the door accepts.
func raisedBudget(it factory.Item) float64 {
	spent := factory.JobLifetimeSpent(it)
	for _, step := range factory.JobBudgetSteps() {
		if step > it.Cap && step > spent {
			return step
		}
	}
	return factory.NextJobCap(it)
}
