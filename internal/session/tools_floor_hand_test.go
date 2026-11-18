package session

// The Factory manager reads every job's stored wait, and on the person's word
// presses an offered action or passes their words to a job's main agent
// (tools_floor.go, tools_floor_hand.go).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// waitLab is a floor of jobs that each wait on somebody different, with the
// wait and the offers stored on each as the store writes them: one waiting on
// the person's approval to publish (landed, so its state alone never said
// so), one ready to merge, one whose step's question its main agent holds,
// one the person held, one the watchdog found stalled, one on GitHub's
// checks with public acts proposed, one out of rounds, and one not started.
func waitLab(t *testing.T) *fakeFloorDoor {
	t.Helper()
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	stream := func(cur int, states ...factory.PhaseState) *factory.Stream {
		names := []string{"plan", "write", "proof"}
		s := &factory.Stream{Cur: cur}
		for i, st := range states {
			s.Phases = append(s.Phases, factory.Phase{Name: names[i], Kind: factory.StageChat, State: st})
		}
		return s
	}
	stages := []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}, {Name: "proof", Kind: factory.StageCheck, On: true}}
	items := []factory.Item{
		// #1 landed and waits for the approval to publish.
		{ID: 1, Num: 1, Repo: "acme/api", Kind: factory.KindIssue, Title: "ledger counts twice", State: factory.StateLanded, Publish: "pr", Assignee: "sam", Cap: 5, HeadCommit: "abc",
			Proof: []factory.Claim{{Text: "tests pass", OK: true}}, Stages: stages, Stream: stream(2, factory.PhaseDone, factory.PhaseDone, factory.PhaseDone)},
		// #2 is published, and its pull request is ready to merge.
		{ID: 2, Num: 2, Repo: "acme/api", Kind: factory.KindIssue, Title: "typo on the login page", State: factory.StateShipped, Publish: "pr", Assignee: "sam", Cap: 5, HeadCommit: "def",
			PullRequest: &factory.PullRequest{Number: 161, State: factory.PROpen, HeadSHA: "def", Mergeable: factory.MergeableYes, ChecksRead: true, ReviewsRead: true}},
		// #3 runs, and its main agent holds a step's question.
		{ID: 3, Num: 3, Repo: "acme/api", Kind: factory.KindIssue, Title: "meter rounds wrong", State: factory.StateRunning, Cap: 5, Stages: stages, Stream: stream(1, factory.PhaseDone, factory.PhaseRunning, factory.PhasePending),
			Asking: &factory.Asked{Stage: "write", Question: "round half up or half even?", With: factory.AskedManager, At: now.Add(-2 * time.Minute)}},
		// #4 was held by the person.
		{ID: 4, Num: 4, Repo: "acme/api", Kind: factory.KindIssue, Title: "new auth flow", State: factory.StateRunning, Cap: 5, Stages: stages, Stream: stream(1, factory.PhaseDone, factory.PhaseRunning, factory.PhasePending)},
		// #5 is queued, and the watchdog found no computer took it.
		{ID: 5, Num: 5, Repo: "acme/api", Kind: factory.KindIssue, Title: "cache warmup", State: factory.StateQueued, Cap: 5, Stages: stages, Stream: stream(0, factory.PhasePending, factory.PhasePending, factory.PhasePending)},
		// #6 follows its pull request, and two public acts wait for a yes.
		{ID: 6, Num: 6, Repo: "acme/api", Kind: factory.KindIssue, Title: "bump the parser", State: factory.StateShipped, Publish: "pr", Assignee: "sam", Cap: 5, HeadCommit: "aaa", Autonomy: factory.AutonomyManual,
			PullRequest: &factory.PullRequest{Number: 162, State: factory.PROpen, Draft: true, HeadSHA: "aaa", Mergeable: factory.MergeableYes, ReviewsRead: true}},
		// #7 used its rounds and asks what to do.
		{ID: 7, Num: 7, Repo: "acme/api", Kind: factory.KindIssue, Title: "flaky export", State: factory.StateNeedsYou, Cap: 5, Stages: stages, Stream: stream(1, factory.PhaseDone, factory.PhaseRunning, factory.PhasePending),
			QKind: factory.QKindOutOfAttempts, Question: "write is not proven after 3 rounds · try again, send it back, or stop the job?"},
		// #8 has not started.
		{ID: 8, Num: 8, Repo: "acme/api", Kind: factory.KindIssue, Title: "dark mode", State: factory.StateNew, Created: now.Add(-48 * time.Hour)},
	}
	items[3].Stream.Paused = true
	items[4].Stall = &factory.Stall{Kind: factory.StallRunner, Words: "No computer picked this job up in time.", Wait: "runner:plan", At: now.Add(-20 * time.Minute)}
	if _, err := factory.AddActs(&items[5], []factory.PublicAct{{Kind: factory.ActKindComment, Text: "Rebased on main."}, {Kind: factory.ActKindMarkReady}}, factory.ActByMainAgent, "the review is addressed", now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("the lab's public acts were refused: %v", err)
	}
	// Each wait began at its own time, so the longest wait is known: #7 an
	// hour ago, #1 half an hour, #2 five minutes.
	since := map[int]time.Duration{1: 30 * time.Minute, 2: 5 * time.Minute, 7: time.Hour}
	for i := range items {
		at := now
		if d, ok := since[items[i].ID]; ok {
			at = now.Add(-d)
		}
		factory.SettleWait(factory.Item{}, &items[i], at)
		if items[i].Created.IsZero() {
			items[i].Created = now.Add(-3 * time.Hour)
		}
	}
	return &fakeFloorDoor{snap: factory.Snapshot{Now: now, Items: items}}
}

func rowOf(t *testing.T, text, ref string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, ref+" ") {
			return line
		}
	}
	t.Fatalf("no row for %s in:\n%s", ref, text)
	return ""
}

// EVERY ROW SAYS WHO THE JOB WAITS ON AND WHAT IT ACCEPTS, from the wait and
// the offers the store wrote. A job handed to its main agent reads as that,
// a stalled one as stalled, and public acts that wait for a yes are said.
func TestFloorRowsSayTheStoredWaitAndWhatTheJobAccepts(t *testing.T) {
	door := waitLab(t)
	text, bad := floorCall(t, door, `{"ask":"all"}`)
	if bad {
		t.Fatal(text)
	}
	for ref, wants := range map[string][]string{
		"#1": {"waits on the person to approve publishing the checked work, for 30m", "accepts from the person: approve"},
		"#2": {"waits on the person to merge the pull request, for 5m", "accepts from the person: merge"},
		"#3": {"handed to its main agent, which is deciding what to do"},
		"#4": {"waits on the person to resume it, put away by them", "accepts from the person: resume"},
		"#5": {"waits on a computer to run it", "stalled: No computer picked this job up in time. (reported to its main agent)"},
		"#6": {"waits on the person to approve what is to be posted or changed on the pull request", "waiting for approval to post 1 comment and mark the pull request ready", "acts_approve", "acts_decline"},
		"#7": {"waits on the person to answer the question, for 1h", "accepts from the person: answer"},
	} {
		row := rowOf(t, text, ref)
		for _, want := range wants {
			if !strings.Contains(row, want) {
				t.Errorf("%s does not say %q:\n%s", ref, want, row)
			}
		}
	}
	// A job that has not started waits on nothing, and its row says nothing
	// of a wait; a step is never named by its key.
	if row := rowOf(t, text, "#8"); strings.Contains(row, "waits") || strings.Contains(row, "accepts") {
		t.Errorf("a job that has not started says a wait: %s", row)
	}
	if strings.Contains(text, "you, the manager") {
		t.Errorf("a row speaks to the job's own manager:\n%s", text)
	}
	// THE COUNTS AGREE WITH THE WAITS: four wait on the person now (the two
	// published ones among them, whose state alone says following), one is
	// put away, one is with its main agent, and each job is counted once.
	head := strings.SplitN(text, "\n", 2)[0]
	if !strings.Contains(head, "in the factory: 1 queued to run, 1 with a main agent, 4 waiting on the person, 1 put away by the person, 1 new") {
		t.Errorf("the counts do not follow the stored waits: %s", head)
	}
}

// `waiting` AGREES WITH THE STORED WAIT, never with the job's state: a landed
// job waiting for its approval and a published one ready to merge wait on the
// person; a running job whose question its main agent holds does not.
// `needs_you` is the ones that wait on the person now, the longest wait
// first, each with what it asks.
func TestTheForemanReadsWhoWaitsOnThePersonFromTheStoredWait(t *testing.T) {
	door := waitLab(t)
	text, _ := floorCall(t, door, `{"ask":"waiting"}`)
	for _, ref := range []string{"#1 ", "#2 ", "#4 ", "#6 ", "#7 "} {
		if !strings.Contains(text, "\n"+ref) {
			t.Errorf("waiting leaves out %s:\n%s", ref, text)
		}
	}
	for _, ref := range []string{"#3 ", "#5 ", "#8 "} {
		if strings.Contains(text, "\n"+ref) {
			t.Errorf("waiting lists %s, which does not wait on the person:\n%s", ref, text)
		}
	}
	text, bad := floorCall(t, door, `{"ask":"needs_you"}`)
	if bad {
		t.Fatal(text)
	}
	lines := strings.Split(text, "\n")
	if len(lines) != 5 || !strings.HasPrefix(lines[0], "waiting on the person now, longest wait first · 4 items") {
		t.Fatalf("needs_you:\n%s", text)
	}
	for i, ref := range []string{"#7 ", "#1 ", "#2 ", "#6 "} {
		if !strings.HasPrefix(lines[i+1], ref) {
			t.Errorf("row %d is not %s, the longest wait first:\n%s", i+1, ref, text)
		}
	}
	if !strings.Contains(lines[1], "asks: write is not proven after 3 rounds") || !strings.Contains(lines[2], "asks: Checked and ready to publish.") || !strings.Contains(lines[3], "asks: Ready to merge.") {
		t.Errorf("needs_you does not say what each asks:\n%s", text)
	}
	// Public acts that wait are said once, by the row's own line.
	if strings.Contains(lines[4], "asks:") || !strings.Contains(lines[4], "waiting for approval to post 1 comment") {
		t.Errorf("the acts that wait are not said once: %s", lines[4])
	}
	// The one the person put away is theirs, and is not asked of them again.
	if strings.Contains(text, "#4 ") {
		t.Errorf("needs_you lists a job the person put away:\n%s", text)
	}
	// A floor where nobody is waited on says so beside the counts.
	door.snap.Items = door.snap.Items[7:]
	if text, _ := floorCall(t, door, `{"ask":"needs_you"}`); !strings.Contains(text, floorNoneAsked) || !strings.Contains(text, "in the factory: 1 new") {
		t.Errorf("an empty needs_you = %q", text)
	}
}

// ONE JOB READ IN FULL says its wait whole: what it asks, the choices its
// question lists, what it accepts from its main agent, and each public act
// that waits, in the words that would be posted.
func TestTheForemansItemReadSaysTheWaitInFull(t *testing.T) {
	door := waitLab(t)
	text, _ := floorCall(t, door, `{"ask":"item","item":7}`)
	for _, want := range []string{"waiting on the person for: write is not proven after 3 rounds", "its question's choices: try_once_more, send_back, cancel_job (cancel_job only on the job page)", "mode Manual"} {
		if !strings.Contains(text, want) {
			t.Errorf("#7 in full does not say %q:\n%s", want, text)
		}
	}
	text, _ = floorCall(t, door, `{"ask":"item","item":6}`)
	for _, want := range []string{"Waiting on the person: approve 2 public acts?", "Rebased on main."} {
		if !strings.Contains(text, want) {
			t.Errorf("#6 in full does not say %q:\n%s", want, text)
		}
	}
	text, _ = floorCall(t, door, `{"ask":"item","item":3}`)
	for _, want := range []string{"its main agent is deciding: round half up or half even?", "accepts from its main agent now: answer"} {
		if !strings.Contains(text, want) {
			t.Errorf("#3 in full does not say %q:\n%s", want, text)
		}
	}
	text, _ = floorCall(t, door, `{"ask":"item","item":8}`)
	if !strings.Contains(text, "It has not started. It accepts from the person now: start.") {
		t.Errorf("#8 in full does not say where it stands:\n%s", text)
	}
}

// handFloorDoor is a floor that presses and passes words on, recording each.
type handFloorDoor struct {
	*fakeFloorDoor
	pressed []string
	turnAts []time.Time
	told    []string
	refuse  error
	stuck   chan struct{}
}

func (d *handFloorDoor) Press(ctx context.Context, id int, p FloorPress, said string) (string, error) {
	raw, _ := json.Marshal(p)
	d.pressed = append(d.pressed, itoaFloor(id)+" "+string(raw)+" said="+said)
	d.turnAts = append(d.turnAts, p.TurnAt)
	if d.refuse != nil {
		return "", d.refuse
	}
	return "pressed " + p.Action, nil
}

func (d *handFloorDoor) Tell(ctx context.Context, id int, words, said string) (string, error) {
	d.told = append(d.told, itoaFloor(id)+" "+words+" said="+said)
	if d.stuck != nil {
		select {
		case <-d.stuck:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if d.refuse != nil {
		return "", d.refuse
	}
	return "its main agent answered: on it", nil
}

func (d *handFloorDoor) StartItem(_ context.Context, id int, said string) (string, error) {
	d.pressed = append(d.pressed, "start "+itoaFloor(id)+" said="+said)
	return "started", nil
}

func (d *handFloorDoor) SetPriority(context.Context, int, string) (string, error) { return "set", nil }

func handCall(t *testing.T, door FloorDoor, said, args string) (string, bool) {
	t.Helper()
	agent := &Agent{config: Config{Workspace: t.TempDir(), Floor: door}, personWords: said}
	text, isErr, err := agent.factoryFloorTool().Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return text, isErr
}

// THE FOREMAN'S PRESS TELLS THE DOOR WHEN ITS TURN BEGAN, so the door can tell
// public acts the person had a chance to read from acts a job's main agent
// proposed since their words.
func TestTheForemansPressCarriesTheStartOfTheTurnToTheDoor(t *testing.T) {
	door := &handFloorDoor{fakeFloorDoor: waitLab(t)}
	began := time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC)
	agent := &Agent{config: Config{Workspace: t.TempDir(), Floor: door}, personWords: "approve #1", teamTurnAt: began}
	for _, args := range []string{`{"item":1,"press":"approve"}`, `{"item":6,"press":"acts_approve"}`} {
		if text, isErr, err := agent.factoryFloorTool().Execute(context.Background(), json.RawMessage(args)); err != nil || isErr {
			t.Fatalf("%s = %q bad=%v err=%v", args, text, isErr, err)
		}
	}
	if len(door.turnAts) != 2 || !door.turnAts[0].Equal(began) || !door.turnAts[1].Equal(began) {
		t.Errorf("the door was told the turn began at %v, want %v", door.turnAts, began)
	}
}

// A PRESS AND WORDS FOR A MAIN AGENT NEED THE PERSON'S OWN WORDS IN THE TURN.
// On a turn that carries none (a team's note, a wake) the door is never
// reached, whatever the model writes in its arguments; with them the door is
// handed the job by its id, the press as asked, and the person's words.
func TestTheForemanPressesAndTellsOnlyOnThePersonsWord(t *testing.T) {
	door := &handFloorDoor{fakeFloorDoor: waitLab(t)}
	for _, args := range []string{
		`{"item":1,"press":"approve","words":"the person said yes"}`,
		`{"item":2,"press":"merge"}`,
	} {
		if text, bad := handCall(t, door, "", args); !bad || text != FloorPressNoWords {
			t.Errorf("%s with no person words = %q", args, text)
		}
	}
	if text, bad := handCall(t, door, "", `{"item":3,"tell":"the person says half even"}`); !bad || text != FloorTellNoWords {
		t.Errorf("tell with no person words = %q", text)
	}
	if len(door.pressed)+len(door.told) != 0 {
		t.Fatalf("a turn the person did not speak in reached the door: %v %v", door.pressed, door.told)
	}
	// With the person's words, each reaches the door as asked.
	if text, bad := handCall(t, door, "merge #2 please", `{"item":2,"press":"merge"}`); bad || text != "pressed merge" {
		t.Errorf("a merge the person asked for = %q", text)
	}
	if text, bad := handCall(t, door, "send #7 back, the export is wrong", `{"item":7,"press":"answer","choice":"send_back","words":"the export is wrong"}`); bad || text != "pressed answer" {
		t.Errorf("an answer the person gave = %q", text)
	}
	if text, bad := handCall(t, door, "give #1 twenty", `{"item":1,"press":"raise_budget","budget":20}`); bad || text != "pressed raise_budget" {
		t.Errorf("a budget the person named = %q", text)
	}
	if text, bad := handCall(t, door, "tell #3 to round half even", `{"item":3,"tell":" Round half even. "}`); bad || text != "its main agent answered: on it" {
		t.Errorf("words the person sent = %q", text)
	}
	want := []string{
		`2 {"Action":"merge","Words":"","Choice":"","Budget":0} said=merge #2 please`,
		`7 {"Action":"answer","Words":"the export is wrong","Choice":"send_back","Budget":0} said=send #7 back, the export is wrong`,
		`1 {"Action":"raise_budget","Words":"","Choice":"","Budget":20} said=give #1 twenty`,
	}
	if strings.Join(door.pressed, "\n") != strings.Join(want, "\n") {
		t.Errorf("pressed:\n%s\nwant:\n%s", strings.Join(door.pressed, "\n"), strings.Join(want, "\n"))
	}
	if len(door.told) != 1 || door.told[0] != "3 Round half even. said=tell #3 to round half even" {
		t.Errorf("told %v", door.told)
	}
	// A START CARRIES THE PERSON'S WORDS TOO, so the door can hold a start
	// that would pass an approve step to them; with none it carries none.
	door.pressed = nil
	handCall(t, door, "start #8", `{"start":8}`)
	handCall(t, door, "", `{"start":8}`)
	if strings.Join(door.pressed, "|") != "start 8 said=start #8|start 8 said=" {
		t.Errorf("starts carried %v", door.pressed)
	}
	// THE DOOR'S REFUSAL IS READ BACK AS IT IS.
	door.refuse = errors.New("Nothing changed: #2 does not accept approve now. It waits on the person to merge the pull request.")
	if text, bad := handCall(t, door, "approve #2", `{"item":2,"press":"approve"}`); !bad || !strings.Contains(text, "does not accept approve now. It waits on the person to merge the pull request.") {
		t.Errorf("a refused press = %q", text)
	}
}

// What the tool refuses before it reaches any door: more than one act in a
// call, a press or words with no job, a job that is not on the floor, an
// action that is no offer, words that go with nothing, and a floor with no
// hand.
func TestTheForemansPressAndTellRefuseWhatTheyCannotDo(t *testing.T) {
	door := &handFloorDoor{fakeFloorDoor: waitLab(t)}
	for args, want := range map[string]string{
		`{"item":2,"press":"merge","tell":"and say why"}`: "one to a call",
		`{"start":8,"item":2,"press":"merge"}`:            "one to a call",
		`{"press":"merge"}`:                               "need item",
		`{"tell":"rebase it"}`:                            "need item",
		`{"item":2,"press":"force_merge"}`:                "Invalid arguments",
		`{"item":2,"words":"go on"}`:                      "go with press",
		`{"ask":"all","budget":20}`:                       "go with press",
		`{"item":99,"press":"merge"}`:                     "there is no #99 in the factory",
		`{"item":99,"tell":"rebase it"}`:                  "there is no #99 in the factory",
	} {
		if text, bad := handCall(t, door, "do it", args); !bad || !strings.Contains(text, want) {
			t.Errorf("%s = %q, want it to say %q", args, text, want)
		}
	}
	if text, bad := handCall(t, door, "do it", `{"item":3,"tell":"`+strings.Repeat("x", FloorTellMost+1)+`"}`); !bad || !strings.Contains(text, "at most 4000 characters") {
		t.Errorf("words too long = %q", text)
	}
	if len(door.pressed)+len(door.told) != 0 {
		t.Fatalf("a refused call reached the door: %v %v", door.pressed, door.told)
	}
	// A floor that only reads says so and changes nothing.
	for _, args := range []string{`{"item":2,"press":"merge"}`, `{"item":3,"tell":"rebase it"}`} {
		if text, bad := handCall(t, waitLab(t), "do it", args); !bad || text != floorNoHand {
			t.Errorf("%s on a floor with no hand = %q", args, text)
		}
	}
}

// The tool says what it can do and what it cannot: every offer is a press by
// its own word, the two the job page keeps are named, the rule of the
// person's own words and the rule about a relayed turn are in the
// description (a Factory manager made before this keeps its old brief), and
// the manual has them.
func TestFactoryFloorDescribesItsPressesAndTheRelayedTurn(t *testing.T) {
	tool := (&Agent{config: Config{Workspace: t.TempDir(), Floor: waitLab(t)}}).factoryFloorTool()
	for _, want := range []string{
		"needs_you", "what it waits on", "accepts from the person now",
		"press (with item)", "approve, answer, reject_plan, accept_step, acts_approve, acts_decline, merge, pr_ready, retry_publish, resume, resume_pipeline, resume_auto, rerun_step, start, task, raise_budget",
		"A press is refused unless the person's own message in this turn asked for it", "merge only when they say to merge",
		"The person does spend_answer and publish_anyway on the job page",
		"tell (with item)", "stopped after 3 minutes", "outside Auto the job's main agent cannot push, approve, mark the pull request ready or approve public acts on it, and it never merges on it",
		"Nothing here stops or drops a job",
	} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("the description lacks %q", want)
		}
	}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"press", "words", "choice", "budget", "tell", "item", "ask"} {
		if _, ok := schema.Properties[field]; !ok {
			t.Errorf("the schema has no %s", field)
		}
	}
	// EVERY OFFER A JOB CAN STORE FOR ITS PERSON IS A WORD THE SCHEMA TAKES,
	// so the word a row lists is never an invalid argument.
	offers := map[string]bool{}
	for _, it := range waitLab(t).snap.Items {
		for _, action := range factory.PersonOffers(it) {
			offers[action] = true
		}
	}
	for _, action := range []string{factory.DoApprove, factory.DoAnswer, factory.DoSpendAnswer, factory.DoRejectPlan, factory.DoAcceptStep, factory.DoMerge, factory.DoMarkReady,
		factory.DoRetryPublish, factory.DoPublishAnyway, factory.DoRerunStep, factory.DoResume, factory.DoResumePipeline, factory.DoResumeAuto, factory.DoRaiseBudget,
		factory.DoStart, factory.DoActsApprove, factory.DoActsDecline, factory.DoTask} {
		offers[action] = true
	}
	for action := range offers {
		found := false
		for _, e := range schema.Properties["press"].Enum {
			found = found || e == action
		}
		if !found {
			t.Errorf("a job can offer %s, and press does not take it", action)
		}
	}
	if !manual.Chat().Mentions("needs_you") {
		t.Error("no chat manual page mentions the needs_you ask")
	}
}

// Words passed to a main agent wait for its whole turn, and no longer than
// the bound: a door that never comes back is let go of, and the line says
// the turn was stopped, never that nothing changed.
func TestTheForemansTellLetsGoOfATurnThatNeverEnds(t *testing.T) {
	door := &handFloorDoor{fakeFloorDoor: waitLab(t), stuck: make(chan struct{})}
	defer close(door.stuck)
	agent := &Agent{config: Config{Workspace: t.TempDir(), Floor: door}, personWords: "tell #3"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan string, 1)
	go func() {
		text, _, _ := agent.factoryFloorTool().Execute(ctx, json.RawMessage(`{"item":3,"tell":"round half even"}`))
		done <- text
	}()
	// The person's stop returns it at once.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a stop did not return the call")
	}
	if late := FloorTellLate("#3"); !strings.Contains(late, "did not finish within 3 minutes, so its turn was stopped") || strings.Contains(late, "Nothing changed") {
		t.Errorf("the late line = %q", late)
	}
}
