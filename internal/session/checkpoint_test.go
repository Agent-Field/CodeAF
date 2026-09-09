package session

// THE FOURTH MOMENT: a turn priced while it is running, and READ BY SOMEBODY WHO
// IS NOT RUNNING IT.
//
// The three that existed before all decide with no evidence in front of them —
// the prompt's law, the route judge's read of a request, and a proposal the model
// remembers to make. This one reads the only thing that is a fact mid-turn: what
// the answer has cost so far, in finished tool rounds, against what handing it
// over costs — and then, at each mark, has a sidecar on the tier that thinks
// sketch what is actually left.
//
// So these tests pin the things that could quietly stop being true: that the
// meter fires at the marks and nowhere else, that the ask is the wording the
// benchmark chose and the shapes are read the way the benchmark scored them, that
// the running model is never sent a word of any of it, that a sketch with parts
// in it hands the turn over with the parts named and the road armed by a
// judgement, that a sketch saying "one job" costs the turn nothing at all, that
// every failure is a carry-on, that the ceiling still fires regardless, and that
// the turns which must never be checkpointed never are.

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// ── the meter ───────────────────────────────────────────────────────────────

// ── what the sidecar is asked ───────────────────────────────────────────────

// ── the lines a person reads ────────────────────────────────────────────────

// inTheHouseRegister is the shape every dim one-liner the harness writes over
// somebody's turn is held to: an observation, a middle dot, a promise — one line,
// lowercase, no full stop, no machinery vocabulary (internal/tui3's taskWideNote,
// and CLAUDE.md's vocabulary law).
func inTheHouseRegister(t *testing.T, line string) {
	t.Helper()
	if plain := plainWords(line); plain != line {
		t.Errorf("the line carries machinery vocabulary; plainly it would read %q", plain)
	}
	if strings.Contains(line, "\n") {
		t.Errorf("the line is more than one line: %q", line)
	}
	if line != strings.ToLower(line) {
		t.Errorf("the line is not lowercase: %q", line)
	}
	if strings.HasSuffix(line, ".") {
		t.Errorf("the line ends in a full stop, which makes a remark into an announcement: %q", line)
	}
	if !strings.Contains(line, " · ") {
		t.Errorf("the line has no middle dot, so it is not the observation-then-promise the surface "+
			"already speaks in: %q", line)
	}
}

// ── the fixture ─────────────────────────────────────────────────────────────

// checkpointMarkModel is what the mark's sidecar rides, and setting it is both
// the fixture and an assertion: the reader is asked of the CREW ALONE, with no
// fall-through to the conversation's own model ([Agent.readMark]), so a session
// without this configured gets no reading at all.
const checkpointMarkModel = "test/mark-reader"

// The two answers a sidecar gives in these tests, written as a real reader
// writes them: a shape line and a sentence naming the letters.
const (
	checkpointSplitSketch = "A | B | C\n" +
		"A is the validation workflow, B is the arithmetic module, C is the currency module."
	checkpointChainSketch = "A > B > C\n" +
		"A is reading the rest of the file, B is the one fix it needs, C is running the suite."
	// AND THE THIRD ANSWER, which the digest's ask now teaches the reader to give:
	// nothing is left. It is the second mind the ceiling's drop is corroborated
	// against ([checkpointSketch.saysDone]).
	checkpointDoneSketch = "(done)\n" +
		"Everything that was asked for has been written and checked."
)

// checkpointSlack is how many of a script's steps a grinding turn spends on
// things that are not tool rounds: one read per mark, the dowry DRAFT at the end,
// and the mastermind that writes the brief out of it. A script cut to the round
// count alone runs out under the sidecar, and the scripted completer's
// past-the-end answer would then be read as a sketch and as a brief.
const checkpointSlack = 6

// answerTheNamerOffTheQueue installs the aside every handover fixture needs
// ([scriptedCompleter.aside]).
//
// THE NAMER IS NOT ONE OF THE TURN'S ROUNDS, so it must not spend one of the
// turn's steps. The ceiling's handover asks for a name the moment it decides to
// move the work, on a goroutine of its own and ahead of the two model calls that
// write the brief (checkpoint.go, taskname.go's [nameAhead]) — which is #333's
// design and is correct. What was wrong was here: this file scripted ONE
// positional queue, so on a machine with a spare processor the namer's call took
// whichever step the turn was about to take, and three tests failed about one run
// in five (#392).
//
// IT IS ANSWERED WITH AN EMPTY NAME, which is a namer that could not name — the
// one answer that changes nothing anywhere. [cleanTaskName] hands "" back,
// [Agent.launchRouteTask] adopts a landed name only when it is non-empty, and
// [TaskGraph.nameNode]'s fallback ask carries the same system line and so is
// answered off the queue too. So the row and the told-after line keep the
// person's own words, which is what every assertion on these pages was written
// against.
//
// A completer that is not scripted has no queue to protect and is left alone.
func answerTheNamerOffTheQueue(completer Completer) {
	scripted, ok := completer.(*scriptedCompleter)
	if !ok {
		return
	}
	scripted.aside = func(messages []ai.Message) (*ai.Response, bool) {
		if !isNameCall(messages) {
			return nil, false
		}
		return textResponse(""), true
	}
}

// checkpointAgent is a watched conversation with a mastermind the mark can be
// read by. Everything else is [newTestAgent]'s.
func checkpointAgent(t *testing.T, completer Completer, mutate ...func(*Config)) *Agent {
	t.Helper()
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): checkpointMarkModel,
		})
		for _, extra := range mutate {
			extra(config)
		}
	})
	return agent
}

// grindingSteps answers every request one of three ways: the mark's sidecar ask
// with the sketch it is given, the ceiling's dowry ask with the brief, and
// everything else with one more tool call — a different path each time, so the
// loop detector has nothing to say about it.
//
// It is a script rather than a stub so that what the sidecar is shown is the real
// transcript: the reader has to see a turn's worth of tool calls to be reading
// anything at all.
func grindingSteps(count int, sketch, brief string) []step {
	steps := make([]step, count)
	for index := range steps {
		round := index
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse(sketch), nil
			}
			if askedForHandoff(messages) {
				return textResponse(brief), nil
			}
			// THE WRITER IS SILENT IN THIS FIXTURE, and that is deliberate: what
			// these tests are about is the road, and a mastermind answering with a
			// tool call is the ordinary failure that drops the ladder back onto the
			// draft the running model wrote. The tests that are about the writer
			// script it themselves ([handoffSteps]).
			if askedToWriteHandoff(messages) {
				return toolResponse("no-writer", "ls", `{"path":"."}`), nil
			}
			// AND A TURN'S END ASKS WHETHER THE ASK IS FINISHED. A grinding turn is
			// moved by a mark or by the ceiling long before it reaches this, and
			// the tests that end in words say so here rather than letting the
			// question fall past the script.
			if askedForRemains(messages) {
				return textResponse("NOTHING LEFT TO DO"), nil
			}
			arguments, _ := json.Marshal(struct {
				Path string `json:"path"`
			}{Path: fmt.Sprintf("./%d", round)})
			// Visible progress keeps this fixture about checkpoint pricing rather
			// than the independent silent-turn ladder, which now ends a turn at its
			// third warning before the ordinary forty-round ceiling.
			return toolResponseWithText(fmt.Sprintf("call-%d", round), "ls", string(arguments),
				"Working through the next path."), nil
		}
	}
	return steps
}

// askedForSketch reports whether this request ends on a mark's ask.
func askedForSketch(messages []ai.Message) bool {
	if len(messages) == 0 {
		return false
	}
	return strings.Contains(messageText(messages[len(messages)-1]), "[checkpoint]")
}

// askedForHandoff reports whether this request ends on the dowry ask — the DRAFT,
// put to the model that spent the turn.
func askedForHandoff(messages []ai.Message) bool {
	return endsOn(messages, "[handing over]")
}

// askedToWriteHandoff reports whether this request ends on the mastermind's ask:
// the draft, the digest and the person's words in, one worker's instruction out.
func askedToWriteHandoff(messages []ai.Message) bool {
	return endsOn(messages, "[write the handoff]")
}

// askedForRemains reports whether this request ends on the question a turn's END
// puts to the mark's reader: is what the person asked for finished?
func askedForRemains(messages []ai.Message) bool {
	return endsOn(messages, "[still asked]")
}

func endsOn(messages []ai.Message, marker string) bool {
	if len(messages) == 0 {
		return false
	}
	return strings.Contains(messageText(messages[len(messages)-1]), marker)
}

// finalAnswer is [finalText] for a session that has a mastermind: the turn's last
// words, and the one answer the end of a turn now asks a reader for.
func finalAnswer(text string) step {
	return func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		if askedForRemains(messages) {
			return textResponse("NOTHING LEFT TO DO"), nil
		}
		return textResponse(text), nil
	}
}

// marksRead is how many times the sidecar was actually asked.
func marksRead(completer *scriptedCompleter) int {
	read := 0
	for index := range completer.requests() {
		if askedForSketch(completer.request(index)) {
			read++
		}
	}
	return read
}

// handoffAsks counts the requests that ended on the handoff ask.
func handoffAsks(completer *scriptedCompleter) int {
	asked := 0
	for index := range completer.requests() {
		if askedForHandoff(completer.request(index)) {
			asked++
		}
	}
	return asked
}

func noticeTexts(events []Event) []string {
	var said []string
	for _, event := range events {
		if event.Kind == EventNotice {
			said = append(said, event.Text)
		}
	}
	return said
}

func saidSomething(said []string, want string) bool {
	for _, line := range said {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// ── the running model never sees any of it ──────────────────────────────────

// ── a mark that says the work has parts ─────────────────────────────────────

// ── the word the mark's reading wears ───────────────────────────────────────

// holdingCompleter answers nothing at all: it waits for its context and reports
// what ended it, which is what a provider that is thinking too slowly does.
type holdingCompleter struct{}

func (holdingCompleter) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// ── the ceiling ─────────────────────────────────────────────────────────────

// ── what comes back when the dowry is asked for ─────────────────────────────

// dsmlSentinel is the answer a deepseek model actually gave the handoff ask on a
// live turn: the request carried no tools, and the model wrote its own chat
// template's tool-call token as text anyway. The person read
// "task 1 started: <｜DSML｜tool_calls>".
//
// NOTHING IN THE CODE MATCHES THIS STRING. [briefIsProse] reads the SHAPE of an
// answer — words with spaces between them — so this is a fixture here and a
// pattern nowhere, and the guard holds for the next provider's sentinel too.
const dsmlSentinel = "<｜DSML｜tool_calls>"

// ── the turns that are never checkpointed ───────────────────────────────────

// ── what the reader is actually shown ───────────────────────────────────────

// ── what the journal now holds ──────────────────────────────────────────────

// ── the ceiling's drop is believed once ─────────────────────────────────────

// ── the reader sees what the model saw ──────────────────────────────────────

// ── the handoff is drafted by the runner and written by the mastermind ──────

// ── the acceptance stays the person's ───────────────────────────────────────

// ── every turn is metered ───────────────────────────────────────────────────

// ── a turn ends; the ask does not ───────────────────────────────────────────

// ── the fixtures these use ──────────────────────────────────────────────────

// handoffSteps is [grindingSteps] with the mastermind that WRITES the brief
// scripted too: the sketch it draws at a mark, the draft the running model
// writes, and the document the writer makes out of it.
func handoffSteps(count int, sketch, draft, written string) []step {
	steps := grindingSteps(count, sketch, draft)
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedToWriteHandoff(messages) {
				return textResponse(written), nil
			}
			return inner(ctx, messages)
		}
	}
	return steps
}

// stoppingSteps is a turn that does `rounds` tool calls and then STOPS IN WORDS,
// however many times it is re-opened — which is what makes it a fixture for the
// end of a turn rather than for the ceiling. remains answers the question a
// turn's end puts to the reader, and is a function so a test can say something
// different the second time it is asked.
// scriptedWorkingNote is the sentence a scripted long turn writes beside its
// calls. It exists because a turn of twenty tool calls with NOTHING visible
// between them is now held rather than run (processrule.go), and a test about
// the checkpoint ladder must not accidentally be a test of that rule. A model
// that says one line per step is the ordinary shape these tests mean to script.
const scriptedWorkingNote = "looking at the next piece, then I will say what I found"

func stoppingSteps(rounds int, stopped string, remains func() string) []step {
	var done atomic.Int64
	steps := make([]step, rounds+40)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse(checkpointChainSketch), nil
			}
			if askedForHandoff(messages) {
				return textResponse("a draft of what is left"), nil
			}
			if askedToWriteHandoff(messages) {
				return textResponse("a brief somebody could work from, written by the mastermind"), nil
			}
			if askedForRemains(messages) {
				return textResponse(remains()), nil
			}
			if call := done.Add(1); call <= int64(rounds) {
				return toolResponseWithText(fmt.Sprintf("call-%d", call), "ls",
					fmt.Sprintf(`{"path":"./%d"}`, call), scriptedWorkingNote), nil
			}
			return textResponse(stopped), nil
		}
	}
	return steps
}

// writtenTurn is [workedTurn] for a turn that ENDED ON A WRITE: the calls it made
// are writes, so the newest thing in the transcript is a change nothing looked at.
func writtenTurn(a *Agent, asked string, calls int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.personAsk = asked
	a.messages = append(a.messages, textMessage("user", asked))
	for index := 1; index <= calls; index++ {
		id := fmt.Sprintf("written-%d", index)
		a.messages = append(a.messages, ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
			ID: id,
			Function: ai.ToolCallFunction{
				Name:      "write",
				Arguments: fmt.Sprintf(`{"path":"./%d.rs","content":"fn main() {}"}`, index),
			},
		}}})
		a.messages = append(a.messages, ai.Message{
			Role: "tool", ToolCallID: id,
			Content: []ai.ContentPart{{Type: "text", Text: "Successfully wrote 13 bytes"}},
		})
	}
	a.messages = append(a.messages, textMessage("assistant", "Now let me build and run the full test suite"))
}

// workedTurn puts a turn's worth of work into an agent by hand: the person's ask
// and n finished tool calls. It is what the digest is assembled out of, so a test
// about the reader needs one before there is anything to read.
func workedTurn(a *Agent, asked string, calls int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.personAsk = asked
	for index := 0; index < calls; index++ {
		a.messages = append(a.messages, toolCallMessage(fmt.Sprintf("call-%d", index), "read",
			fmt.Sprintf(`{"path":"./%d.txt"}`, index)))
	}
}

// toolCallMessage is one assistant message that called one tool and said nothing,
// which is what nearly every message of a grinding turn is.
func toolCallMessage(id, name, arguments string) ai.Message {
	return ai.Message{
		Role:    "assistant",
		Content: []ai.ContentPart{{Type: "text", Text: ""}},
		ToolCalls: []ai.ToolCall{{ID: id, Type: "function",
			Function: ai.ToolCallFunction{Name: name, Arguments: arguments}}},
	}
}

// journaledMarks, journaledCeilings and journaledCalls read one kind of line back
// out of a session file. They parse the file rather than a struct the agent kept,
// because the file is what the bench reads.
func journaledMarks(t *testing.T, path string) []journalMark {
	t.Helper()
	var marks []journalMark
	for _, entry := range journaledEntries(t, path, "mark") {
		if entry.Mark != nil {
			marks = append(marks, *entry.Mark)
		}
	}
	return marks
}

func journaledCeilings(t *testing.T, path string) []journalCeiling {
	t.Helper()
	var ceilings []journalCeiling
	for _, entry := range journaledEntries(t, path, "ceiling") {
		if entry.Ceiling != nil {
			ceilings = append(ceilings, *entry.Ceiling)
		}
	}
	return ceilings
}

func journaledCalls(t *testing.T, path string) []journalCall {
	t.Helper()
	var calls []journalCall
	for _, entry := range journaledEntries(t, path, "call") {
		if entry.Call != nil {
			calls = append(calls, *entry.Call)
		}
	}
	return calls
}

func journaledEntries(t *testing.T, path, kind string) []sessionEntry {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}
	var entries []sessionEntry
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry sessionEntry
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Type != kind {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// scriptedCall is one tool call a turn makes, as the script names it.
type scriptedCall struct{ tool, arguments string }

// readingCalls is n calls that only LOOK, each at a different path so the loop
// detector has nothing to say about them.
func readingCalls(n int) []scriptedCall {
	calls := make([]scriptedCall, 0, n)
	for index := 1; index <= n; index++ {
		calls = append(calls, scriptedCall{"ls", fmt.Sprintf(`{"path":"./%d"}`, index)})
	}
	return calls
}

// fileWriteCall and commandCall are the two verbs the exposure arm tells apart:
// one CHANGES the working tree and one looks at what changed.
func fileWriteCall(path, content string) scriptedCall {
	arguments, _ := json.Marshal(struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}{Path: path, Content: content})
	return scriptedCall{"write", string(arguments)}
}

func commandCall(command string) scriptedCall {
	arguments, _ := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: command})
	return scriptedCall{"bash", string(arguments)}
}

// stoppingStepsCalling is [stoppingSteps] with the calls NAMED BY THE CALLER.
//
// The read-only fixture says `ls` for every round, which is exactly the wrong
// shape for the exposure arm: what a turn TOUCHED is the whole of what that arm
// reads, so a test about it has to be able to spell `write` and `bash` in the
// order the measured turn spelled them.
func stoppingStepsCalling(calls []scriptedCall, stopped string, remains func() string) []step {
	var done atomic.Int64
	steps := make([]step, len(calls)+40)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse(checkpointChainSketch), nil
			}
			if askedForHandoff(messages) {
				return textResponse("a draft of what is left"), nil
			}
			if askedToWriteHandoff(messages) {
				return textResponse("a brief somebody could work from, written by the mastermind"), nil
			}
			if askedForRemains(messages) {
				return textResponse(remains()), nil
			}
			if call := done.Add(1); call <= int64(len(calls)) {
				made := calls[call-1]
				return toolResponse(fmt.Sprintf("call-%d", call), made.tool, made.arguments), nil
			}
			return textResponse(stopped), nil
		}
	}
	return steps
}

// ── coordination is not a division, and watching is not working ─────────────
//
// The two halves of the same live failure (#276), 2026-09-01. A conversation had
// four pieces out and spent a turn watching them: reading their logs, waiting for
// what they sent back. The turn crossed a mark on the strength of that watching
// alone; the honest sketch of a turn like that is "wait for the second | wait for
// the third | wait for the fourth", which is three parts by the separator; and the
// harness converted it into a task, twice, whose whole brief was to review reports
// and accept work that a worker in its own copy cannot see. Two junk tasks, about
// fifteen minutes of node time, and a rail the person had to distrust.

// ── WAITING IS NOT WORKING, ON THE CONVERSATION'S SIDE ──────────────────────

// AND THE TWO WINDOWS IT NAMES ARE REAL TOOLS. A name that drifted out of the
// belt would turn this carve-out off in silence, which is the failure mode the
// whole file is built to avoid.
func TestTheWatchToolsAreOnTheBelt(t *testing.T) {
	agent := checkpointAgent(t, &scriptedCompleter{steps: []step{finalText("nothing")}})
	onBelt := make(map[string]bool)
	for _, name := range beltNames(agent) {
		onBelt[name] = true
	}
	for _, name := range []string{"tasks", "jobs"} {
		if !onBelt[name] {
			t.Errorf("the checkpoint discounts rounds spent in %q, which is not a tool on the belt: %v",
				name, beltNames(agent))
		}
	}
}

// ── #468: A READER THAT CANNOT WORK IS ABSENT, NOT FAILING ──────────────────

// ── #468: THE NOTE SAYS WHAT WAS OBSERVED ───────────────────────────────────
