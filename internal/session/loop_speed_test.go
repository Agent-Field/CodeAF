package session

// THE LAW IN loop.go's HEADER, AS A TEST THAT FAILS THE BUILD.
//
// "The only wait a person experiences is the main model generating." Every
// reading this harness makes on its own behalf — the recall that picks which
// remembered lines a message needs, the judge that asks whether it was work, the
// reader that sketches what is left of a long answer, the namer, the narrator —
// runs BESIDE the work and may only interrupt it.
//
// THIS FILE IS THE GATE THAT CATCHES THE NEXT SERIAL CALL WHOEVER ADDS IT. Every
// reading is made three seconds slow and the two figures the law is about are
// asserted in milliseconds. A turn that awaits any one of them cannot pass.
//
// AND THE QUALITY HALF IS ASSERTED BESIDE IT, because taking a wait off a path is
// easy and keeping the answer is the hard part: a reading that lands before the
// first word is applied to THIS request, one that lands after it is applied to
// the next step, and neither is dropped.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// paceLawBudget is the whole of loop.go's law in one figure: how long a person
// may wait to be sent ANYWHERE, and how long a tool result may sit before the
// next request leaves.
//
// IT IS NOT A LATENCY TARGET AND IT IS NOT A NUMBER SOMEBODY LIKED. It separates
// two populations that are three orders of magnitude apart — assembling messages
// in memory, which is what a turn really does between those two moments, and one
// model call, which is what used to happen there — so any figure inside that gap
// is the same test. This one is derived from the figure the build already holds
// about people: a twentieth of [lanes.SpokenWithin], which is one second, the
// limit of a person's uninterrupted flow of thought (Miller 1968 through Nielsen
// 1993) and the point past which this build owes them a word about a wait. A
// request that has not left by then has not left because something was awaited.
const paceLawBudget = lanes.SpokenWithin / 20

// paceSlowReading is how long EVERY reading beside the work takes here. Three
// seconds sits inside the measured spread of the 2026-09-11 census's pre-turn
// gate — mean 4.3s, max 10.7s — so a turn that waits for one of them misses the
// budget above by a factor of sixty.
const paceSlowReading = 3 * time.Second

// paceJudgeReading is what the POST-TURN JUDGE costs in this fixture. It is an
// order of magnitude over [paceSlowReading] so that a turn which waited for it
// cannot be mistaken for a turn that waited for the reader beside it — the one
// wait at the end of a turn that is honest.
const paceJudgeReading = 30 * time.Second

// ── the fixture ─────────────────────────────────────────────────────────────

// paceCompleter answers the turn instantly and EVERY reading beside it slowly.
//
// It routes by the shape of the request rather than by a queue, for
// [scriptedCompleter.aside]'s reason said the other way round: this fixture is
// ABOUT the readings, so each of them has to be recognised, counted and delayed
// rather than quietly answered off the end of a positional script.
type paceCompleter struct {
	mu sync.Mutex
	// steps is the turn's own script, taken positionally by main calls alone.
	steps []step
	taken int
	// slow is what every reading costs.
	slow time.Duration
	// judgeSlow is what the POST-TURN JUDGE costs, when it differs. It is its own
	// figure because the judge is the one reading at the end of a turn whose
	// answer can be spent after the turn has sealed — the re-open reader beside
	// it decides whether there IS a turn to seal, and has nothing to be applied
	// to afterwards — so the only way to see the seal being held by the right one
	// of the two is to make them cost different amounts.
	judgeSlow time.Duration
	// route is what the memory router answers, and sketch what a mark's reader
	// draws.
	route  string
	sketch string
	// readings counts what was asked beside the work, by kind.
	readings map[string]int
	// mainAt is when each of the turn's own requests arrived.
	mainAt []time.Time
	// requests is every main request, joined, so a test can ask whether the
	// answer carried the routed block.
	requests []string
}

func (p *paceCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if kind, answer := p.readingFor(messages); kind != "" {
		p.mu.Lock()
		if p.readings == nil {
			p.readings = map[string]int{}
		}
		p.readings[kind]++
		slow := p.slow
		if kind == "judge" && p.judgeSlow > 0 {
			slow = p.judgeSlow
		}
		p.mu.Unlock()
		select {
		case <-time.After(slow):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return textResponse(answer), nil
	}

	var whole strings.Builder
	for _, message := range messages {
		whole.WriteString(messageText(message))
		whole.WriteString("\n")
	}
	p.mu.Lock()
	p.mainAt = append(p.mainAt, time.Now())
	p.requests = append(p.requests, whole.String())
	index := p.taken
	p.taken++
	var next step
	if index < len(p.steps) {
		next = p.steps[index]
	}
	p.mu.Unlock()
	if next == nil {
		return textResponse("(unscripted)"), nil
	}
	return next(ctx, messages)
}

// readingFor names the reading this request is, or "" for one of the turn's own.
//
// EVERY SHAPE IS NAMED, because a reading this fixture failed to recognise would
// be answered instantly off the script and the law would pass without having been
// tested.
func (p *paceCompleter) readingFor(messages []ai.Message) (string, string) {
	system := ""
	if len(messages) > 0 && messages[0].Role == "system" {
		system = messageContentText(messages[0])
	}
	switch {
	case isCaptionCall(messages):
		return "caption", ""
	case isTitleCall(messages):
		return "title", ""
	case isNameCall(messages):
		return "name", ""
	case strings.Contains(system, "memory router"):
		return "recall", p.route
	case strings.Contains(system, "worth remembering after this session ends"):
		return "keep", `{"mem":0}`
	case strings.Contains(system, "one candidate memory and the lines already stored"):
		return "settle", `{"action":"skip"}`
	case askedForSketch(messages):
		return "mark", p.sketch
	case askedForRemains(messages):
		return "remains", checkpointNothingLeft
	case askedForHandoff(messages):
		return "handoff", "Finish what is left\nthe rest of the parts named above"
	case askedToWriteHandoff(messages):
		return "brief", "Finish what is left, working through the parts named in the sketch above."
	case strings.Contains(system, "You judge ONE message a person has just typed"):
		return "judge", `{"work": false}`
	case strings.Contains(system, "You judge ONE exchange"):
		return "judge", `{"work": false}`
	}
	return "", ""
}

func (p *paceCompleter) count(kind string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.readings[kind]
}

func (p *paceCompleter) mainCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.mainAt)
}

func (p *paceCompleter) request(index int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.requests) {
		return ""
	}
	return p.requests[index]
}

// paceRounds is a script of tool rounds ending in one plain answer. The tool is
// `ls`, which is what the checkpoint fixtures grind on.
func paceRounds(count int, answer string) []step {
	steps := make([]step, 0, count+1)
	for index := 0; index < count; index++ {
		round := index
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			// A DIFFERENT PATH EVERY ROUND, for [grindingSteps]'s reason: the loop
			// detector reads a turn repeating one call as a turn going in circles
			// and hands it over, which would end this fixture long before its mark.
			arguments, _ := json.Marshal(struct {
				Path string `json:"path"`
			}{Path: fmt.Sprintf("./%d", round)})
			// AND IT FILLS THE WINDOW AS IT GOES, for [checkpointFillAt]'s reason:
			// a turn is taken away from a person when its context can no longer
			// hold another step, never for having finished a number of rounds.
			return filling(toolResponseWithText(fmt.Sprintf("call-%d", round), "ls", string(arguments),
				"Working through the next path."), round), nil
		})
	}
	return append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(answer), nil
	})
}

// paceAgent is a session with everything a reading needs to be asked at all: a
// brain with something in it, a crew with a mastermind on it, somebody watching,
// a policy that lets the belt run, and a journal to read the decomposition back
// out of.
func paceAgent(t *testing.T, completer *paceCompleter) (*Agent, *store.Store, string) {
	t.Helper()
	brain := openTestBrain(t)
	journal := t.TempDir() + "/session.jsonl"
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Memory = brain
		config.AskConsent = true
		config.SessionFile = journal
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): checkpointMarkModel,
			roles.TierKey(roles.TierReflex):     "test/reflex",
			roles.TierKey(roles.TierLow):        "test/cheap",
		})
	})
	return agent, brain, journal
}

// pacedTurn is the one decomposition row this turn wrote.
func pacedTurn(t *testing.T, journal string) journalPace {
	t.Helper()
	rows := journaledEntries(t, journal, "pace")
	if len(rows) != 1 {
		t.Fatalf("%d pace rows, want exactly one per turn", len(rows))
	}
	if rows[0].Pace == nil {
		t.Fatalf("the pace row carries nothing: %+v", rows[0])
	}
	return *rows[0].Pace
}

// ── THE LAW ─────────────────────────────────────────────────────────────────

// EVERY READING IS SLOW AND THE PERSON WAITS FOR NONE OF THEM.
//
// A five-step turn with a memory in the store, a crew with a mastermind, and a
// mark crossed part way through — with the recall, the namer, the narrator, the
// work-or-words judge and the mark's own reader each taking three seconds.
func TestTheOnlyWaitAPersonExperiencesIsTheModelGenerating(t *testing.T) {
	// THE ONE RUNG THAT STILL ASKS ANYBODY TO READ THE WORK IS THE NET, so the
	// script runs until the context it is filling can hold no more
	// (inherit.go's [Agent.turnHasRunAway]). The rungs below it tell the turn and
	// call nobody, which is a saving rather than a gap in this law: what this test
	// pins is that the reading which DOES happen is never in front of the person.
	rounds := checkpointMarkAt(checkpointMarks) + 2
	completer := &paceCompleter{
		steps: paceRounds(rounds, "all done"),
		slow:  paceSlowReading,
		// AND THE POST-TURN JUDGE IS THE SLOWEST THING IN THE FIXTURE, which is
		// what makes the seal assertion below say something. In the product it
		// usually is: the re-open reader beside it is one call, and the judge is a
		// cheap screen and then, on a yes, a mastermind confirm, in series.
		judgeSlow: paceJudgeReading,
		route:     `{"inject":[],"cmd":null}`,
		sketch:    checkpointChainSketch,
	}
	agent, brain, journal := paceAgent(t, completer)
	remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")

	collect(t, mustSubmit(t, agent,
		"work through every file under internal and tell me what you find in each one"))

	pace := pacedTurn(t, journal)
	if pace.SendMS > paceLawBudget.Milliseconds() {
		t.Fatalf("the person waited %dms to be sent anywhere, want at most %dms — "+
			"a reading beside the work was awaited in front of it",
			pace.SendMS, paceLawBudget.Milliseconds())
	}
	if pace.StepGapMS > paceLawBudget.Milliseconds() {
		t.Fatalf("a tool result waited %dms for the next request, want at most %dms — "+
			"a reading beside the work was awaited between two steps",
			pace.StepGapMS, paceLawBudget.Milliseconds())
	}
	// AND THE SCRIPT RAN. The net cuts the step it lands beside, so the turn ends
	// a round or two short of its script and the floor is the rung rather than the
	// whole of it.
	if floor := checkpointMarkAt(checkpointMarks); pace.Steps < floor {
		t.Fatalf("the turn took %d steps, want at least %d — the script did not run", pace.Steps, floor)
	}
	// AND THE END OF THE TURN IS THE THIRD PLACE A READING CAN STAND IN FRONT OF
	// A PERSON, which is where the post-turn judge stood: the answer is written,
	// it is on their screen, and the turn is held open — the seal, the usage row,
	// the next Submit and the follow-up drain all parked — while a cheap screen
	// and a mastermind confirm decide whether it should have been work.
	//
	// It is the same budget as the two above because it is the same claim: what
	// separates sealing a turn in memory from awaiting one model call is three
	// orders of magnitude, so any figure inside that gap is the same test.
	// The seal is held by the RE-OPEN READER and by nothing else. That one is
	// [Agent.checkpointReopen], and it is the guardian's named exception read at
	// the other end of the turn: it decides whether there is a turn to seal at
	// all, so there is nothing left to apply its answer to afterwards and it has
	// to precede. Every other reading at the end of a turn CAN be spent after the
	// seal, and the post-turn judge is the one that had to be taught to be — so
	// the fixture makes it far the slowest thing here and the seal must not have
	// noticed.
	if seal := time.Duration(pace.SealMS) * time.Millisecond; seal >= paceJudgeReading {
		t.Fatalf("the turn stayed open %s after the model's last word, want well under the %s "+
			"the post-turn judge takes — a reading whose answer could have been spent after the "+
			"seal was awaited in front of it",
			seal, paceJudgeReading)
	}

	// AND THE READINGS REALLY WERE ASKED. A law that passed because nothing ran
	// beside the work would be the law deleted rather than kept, which is exactly
	// what the decomposition row's names exist to make visible.
	if completer.count("recall") == 0 {
		t.Fatal("the recall was never asked — the law was not tested")
	}
	if completer.count("mark") == 0 {
		t.Fatal("the mark's reader was never asked — the law was not tested")
	}
	if !namesAside(pace.Aside, "recall") || !namesAside(pace.Aside, "mark") {
		t.Fatalf("the decomposition names %v, want the recall and the mark beside the work", pace.Aside)
	}
}

// namesAside reports whether the decomposition row names one reading.
func namesAside(list []string, want string) bool {
	for _, word := range list {
		if word == want {
			return true
		}
	}
	return false
}

// ── THE QUALITY HALF ────────────────────────────────────────────────────────

// A RECALL THAT LANDS BEFORE THE FIRST WORD IS SPENT ON THIS ANSWER.
//
// The request is cut and asked again with the block in, once, and the person
// sees nothing — there was nothing on their screen yet. This is the move that
// makes taking the wait off the path cost no memories at all.
func TestARecallThatLandsBeforeTheFirstWordIsAskedAgainWithIt(t *testing.T) {
	completer := &paceCompleter{
		// The turn's first request waits for its own context, which is what a
		// model that has not written a word yet looks like from here. The recall
		// lands underneath it and cuts it; the second step is the re-ask.
		steps: []step{
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("reformatted"), nil
			},
		},
		// Instant: this reading is meant to win the race.
		slow: 0,
	}
	agent, brain, journal := paceAgent(t, completer)
	tabs := remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")
	completer.route = `{"inject":["` + tabs.ID + `"],"cmd":null}`

	collect(t, mustSubmit(t, agent, "reformat this file the way I like it"))

	pace := pacedTurn(t, journal)
	if !namesAside(pace.Aside, "recall:reasked") {
		t.Fatalf("the decomposition names %v, want the one cut-and-re-ask this turn bought", pace.Aside)
	}
	if completer.mainCalls() < 2 {
		t.Fatalf("%d requests went out, want the cut one and the re-ask", completer.mainCalls())
	}
	// AND THE RE-ASK IS THE ONE THAT CARRIED THE MEMORY, which is the whole point:
	// the block was routed, the answer was given with it, and nothing was dropped.
	if !strings.Contains(completer.request(1), "prefers tabs over spaces in Go") {
		t.Fatalf("the re-asked request did not carry the routed memory:\n%s", completer.request(1))
	}
}

// A RECALL THAT LANDS AFTER THE FIRST WORD RIDES THE NEXT STEP, and says so.
//
// The alternative — dropping it — is what makes a reading that came back too
// late indistinguishable from a store with nothing in it, and leaves the ranking
// with nothing to learn from.
func TestARecallThatLandsAfterTheFirstWordRidesTheNextStep(t *testing.T) {
	completer := &paceCompleter{
		steps: []step{
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				// The first word arrives at once, so the request may not be cut.
				provider.Emit(ctx, provider.StreamDelta, "looking")
				arguments, _ := json.Marshal(struct {
					Path string `json:"path"`
				}{Path: "./0"})
				select {
				case <-time.After(400 * time.Millisecond):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return toolResponseWithText("call-0", "ls", string(arguments), "looking"), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("reformatted"), nil
			},
		},
		// Long enough to land behind the delta above, short enough to land inside
		// the step it then rides.
		slow: 100 * time.Millisecond,
	}
	agent, brain, journal := paceAgent(t, completer)
	tabs := remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")
	completer.route = `{"inject":["` + tabs.ID + `"],"cmd":null}`

	collect(t, mustSubmit(t, agent, "reformat this file the way I like it"))

	pace := pacedTurn(t, journal)
	if !namesAside(pace.Aside, "recall:late") {
		t.Fatalf("the decomposition names %v, want the block written down as late", pace.Aside)
	}
	if namesAside(pace.Aside, "recall:reasked") {
		t.Fatal("the request was cut after the person had already read a word")
	}
	if !strings.Contains(completer.request(1), "prefers tabs over spaces in Go") {
		t.Fatalf("the late block did not ride the next step:\n%s", completer.request(1))
	}
}

// THE NET'S READING RUNS BESIDE THE NEXT STEP AND INTERRUPTS IT.
//
// The reading takes three seconds. The next step goes out at once — the law
// above — and the drawing then cuts it where it stands rather than waiting for
// it to end. The reading is the runaway net's now and the only one there is: the
// rungs under it tell the turn and buy nothing (inherit.go).
func TestAMarksReadingRidesBesideTheStepAndCutsItOnIndependentParts(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks)
	// The mark falls once `rounds` tool rounds have FINISHED, so the step that
	// rides beside the reading is the one after them. It waits for its own
	// context, which makes the cut the only thing that can end it.
	steps := paceRounds(rounds, "never reached")
	steps[len(steps)-1] = func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	completer := &paceCompleter{
		steps:  append(steps, func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil }),
		slow:   500 * time.Millisecond,
		route:  `{"inject":[],"cmd":null}`,
		sketch: checkpointSplitSketch,
	}
	agent, _, journal := paceAgent(t, completer)
	started := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { started <- node })

	collect(t, mustSubmit(t, agent,
		"work through every file under internal and tell me what you find in each one"))

	pace := pacedTurn(t, journal)
	if pace.StepGapMS > paceLawBudget.Milliseconds() {
		t.Fatalf("a tool result waited %dms for the next request while the mark was being read, "+
			"want at most %dms", pace.StepGapMS, paceLawBudget.Milliseconds())
	}
	if completer.count("mark") == 0 {
		t.Fatal("the mark's reader was never asked")
	}
	started.await(t)
}

// ── AND THE LAW READ FROM THE OTHER END ─────────────────────────────────────

// A SLOW LISTENER NEVER HOLDS THE ENGINE'S OWN GOROUTINE.
//
// The readings above are things the turn asks FOR. This is the thing the turn
// TELLS: every phase it posts went straight down the caller's own stack into the
// surface's reader, and internal/tui3's reader asks Bubble Tea for a frame
// through an unbuffered channel — so the last act of every model call,
// `phase.done()`, paid a whole Update-and-View cycle before the engine goroutine
// got its call back. On every call, not only a cancelled one.
//
// The listener here takes as long as every other auxiliary in this file, and the
// two figures the law is about are the same two. It passes because the news is
// left on a desk (sidecar.go) rather than carried.
func TestASlowListenerNeverHoldsTheTurnThatIsTellingIt(t *testing.T) {
	// The listener is released the instant the test ends, so a desk still working
	// through a backlog never outlives the thing it was describing.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var told int64
	previous := OnPhaseNews(func(PhaseNews) {
		atomic.AddInt64(&told, 1)
		select {
		case <-release:
		case <-time.After(paceSlowReading):
		}
	})
	t.Cleanup(func() { OnPhaseNews(previous) })

	rounds := 3
	completer := &paceCompleter{
		steps: paceRounds(rounds, "all done"),
		route: `{"inject":[],"cmd":null}`,
	}
	agent, _, journal := paceAgent(t, completer)

	collect(t, mustSubmit(t, agent, "read three files and tell me what is in them"))

	pace := pacedTurn(t, journal)
	if pace.SendMS > paceLawBudget.Milliseconds() {
		t.Fatalf("the person waited %dms to be sent anywhere while a listener was slow, "+
			"want at most %dms — a phase was posted down the turn's own stack",
			pace.SendMS, paceLawBudget.Milliseconds())
	}
	if pace.StepGapMS > paceLawBudget.Milliseconds() {
		t.Fatalf("a tool result waited %dms for the next request while a listener was slow, "+
			"want at most %dms — a phase was posted down the turn's own stack",
			pace.StepGapMS, paceLawBudget.Milliseconds())
	}
	// AND THE LISTENER REALLY WAS TOLD. A turn that posted nothing would pass this
	// law by saying nothing, which is the other way to break the status line.
	if atomic.LoadInt64(&told) == 0 {
		t.Fatal("the listener was never told a phase — the law was not tested")
	}
}
