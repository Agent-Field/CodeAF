package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the carry ladder is an event, not a default ─────────────────────────────
//
// THE MEASURED FAILURE. SWE-Marathon s4, 00:01:54Z. The round-40 ceiling ran the
// ladder that decides what the task it opens is started on. The running model's
// draft came back as seven tokens; the mastermind that writes the real brief was
// asked and never answered, and its call was cut ninety seconds later by
// [checkpointHandoffWindow] — to the millisecond. Both upper rungs answered "",
// the task opened on the person's raw request, and the worker spent twelve
// minutes and seventy calls re-deriving what the chat already held.
//
// The s2 twin of that run walked the same ladder in 1.5 s and its task opened on
// a 3.5 KB account of what had been learned. THE TWO JOURNALS WERE IDENTICAL at
// that moment: one ceiling line reading `moved`. Nothing said which rung had
// answered, nothing said the writer had been asked at all, and nothing carried
// one word of why it had not answered — the errand's own failure row was skipped
// because the deadline that killed it was checked before the row was written
// (auxiliary.go's [Agent.callRole]).

// A WRITER THAT FAULTS LEAVES THE PROVIDER'S OWN WORDS IN THE FILE, AND THE
// LADDER SAYS WHICH RUNG THE WORKER ACTUALLY OPENED ON.
func TestAFaultedHandoffWriterIsJournaledWithItsWordsAndTheRungIsNamed(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const draft = "Finish the four pieces, and the auth test is the one still failing."
	const upstream = "the writer is out of capacity for this model right now"

	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: refusedWriterSteps(
		checkpointMarkAt(checkpointMarks)+checkpointSlack, draft, upstream)}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := ran.await(t)

	// THE MOVE STILL HAPPENED. A blind worker beats a stalled chat, and here the
	// runner's own draft was still there to carry.
	if !strings.Contains(node.spec.brief, draft) {
		t.Fatalf("the ladder did not fall back to the draft:\n%s", node.spec.brief)
	}

	// THE REFUSAL IS IN THE FILE, IN THE UPSTREAM'S OWN WORDS, under the role that
	// made the call. This is the row the measured run never wrote.
	said := false
	for _, row := range journaledErrors(t, path) {
		if row.Role == string(roles.RoleHandoff) && strings.Contains(row.Message, upstream) {
			said = true
		}
	}
	if !said {
		t.Errorf("no journaled failure names the writer and what it said; rows: %+v",
			journaledErrors(t, path))
	}

	// AND THE LADDER SAYS WHAT EACH RUNG DID AND WHICH ONE ANSWERED.
	ladder := journaledCarries(t, path)
	if len(ladder) != 3 {
		t.Fatalf("the ladder wrote %d lines, want one per rung: %+v", len(ladder), ladder)
	}
	if ladder[0].Rung != carryRungHandoff || ladder[0].Outcome != carryFailed {
		t.Errorf("the writer's rung reads %+v, want a failed handoff", ladder[0])
	}
	if !strings.Contains(ladder[0].Reason, upstream) {
		t.Errorf("the failed rung does not carry the provider's words: %q", ladder[0].Reason)
	}
	if used := carriedRung(ladder); used != carryRungDraft {
		t.Errorf("the ladder says %q supplied the brief, want %q", used, carryRungDraft)
	}
	if got := lastCeiling(t, path).Carry; got != carryRungDraft {
		t.Errorf("the ceiling line names %q as the rung that supplied the brief, want %q",
			got, carryRungDraft)
	}
}

// AND A HANDOFF THAT WORKED SAYS SO TOO, which is the whole point of writing the
// rung down: two ceilings that both read `moved` are not the same event, and only
// the rung tells them apart.
func TestASuccessfulHandoffJournalsTheRungThatSuppliedTheBrief(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const draft = "Finish the four pieces, and the auth test is the one still failing."
	const written = "Finish the currency module. The auth test is the one still failing and the yaml " +
		"route has been ruled out. It is done when the whole suite is green."

	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: handoffSteps(checkpointMarkAt(checkpointMarks)+checkpointSlack,
		checkpointChainSketch, draft, written)}
	agent := checkpointAgent(t, completer, func(config *Config) { config.SessionFile = path })
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := ran.await(t)
	if !strings.Contains(node.spec.brief, written) {
		t.Fatalf("the worker's brief is not the written handoff:\n%s", node.spec.brief)
	}

	ladder := journaledCarries(t, path)
	if len(ladder) != 3 {
		t.Fatalf("the ladder wrote %d lines, want one per rung: %+v", len(ladder), ladder)
	}
	if ladder[0].Rung != carryRungHandoff || ladder[0].Outcome != carryWritten {
		t.Errorf("the writer's rung reads %+v, want a written handoff", ladder[0])
	}
	// AND HOW BIG THE DOWRY WAS, which is the number that says a 3.5 KB account
	// apart from a bare sentence without anybody re-reading the transcript.
	if ladder[0].Chars < len(written) {
		t.Errorf("the written rung says %d characters, want at least %d", ladder[0].Chars, len(written))
	}
	if used := carriedRung(ladder); used != carryRungHandoff {
		t.Errorf("the ladder says %q supplied the brief, want %q", used, carryRungHandoff)
	}
	if got := lastCeiling(t, path).Carry; got != carryRungHandoff {
		t.Errorf("the ceiling line names %q, want %q", got, carryRungHandoff)
	}
	// AND EXACTLY ONE RUNG IS EVER MARKED USED. A worker opens on one document.
	used := 0
	for _, rung := range ladder {
		if rung.Used {
			used++
		}
	}
	if used != 1 {
		t.Errorf("%d rungs claim to have supplied the brief, want exactly one", used)
	}
}

// AND THE PERSON IS TOLD THE DIFFERENCE.
//
// A move that could carry no state beyond the ask is still allowed to proceed,
// but it is not the same event as a move that carried what the turn found out —
// and on the measured run the person read the identical line for both.
func TestTheLineSaysWhenNothingButTheAskWentWithTheWork(t *testing.T) {
	const asked = "work through the four things I listed and report back"

	// NEITHER UPPER RUNG SURVIVES: the runner answers a sentinel and the writer
	// faults, which is exactly the shape of the measured turn.
	blind := &scriptedCompleter{steps: refusedWriterSteps(
		checkpointMarkAt(checkpointMarks)+checkpointSlack, dsmlSentinel, "the writer is unreachable")}
	agent := checkpointAgent(t, blind)
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })
	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	bare := ceilingNotice(t, collect(t, events))
	node := ran.await(t)

	// THE WORK MOVED ANYWAY, on the person's own sentence.
	if !strings.Contains(node.spec.brief, asked) {
		t.Fatalf("the ask itself did not reach the worker:\n%s", node.spec.brief)
	}
	if !strings.Contains(bare, carryAskOnlyNote) {
		t.Fatalf("the line does not say the work went with nothing but the ask: %q", bare)
	}
	if !strings.Contains(bare, carrySaidUnreachable) {
		t.Errorf("the line does not say why the brief could not be written: %q", bare)
	}
	// AND IT IS STILL THE HOUSE'S OWN LINE: one line, lowercase, no full stop, no
	// machinery vocabulary, and the provider's error body nowhere near it.
	inTheHouseRegister(t, bare)

	// AND A MOVE THAT CARRIED THE FINDINGS SAYS NOTHING OF THE KIND.
	const written = "Finish the currency module. The auth test is the one still failing and the yaml " +
		"route has been ruled out. It is done when the whole suite is green."
	carrying := &scriptedCompleter{steps: handoffSteps(checkpointMarkAt(checkpointMarks)+checkpointSlack,
		checkpointChainSketch, "Finish the four pieces.", written)}
	second := checkpointAgent(t, carrying)
	stubbedGraph(second, func(*TaskNode) {})
	events, err = second.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	full := ceilingNotice(t, collect(t, events))
	if strings.Contains(full, carryAskOnlyNote) {
		t.Errorf("a move that carried the whole dowry apologised for carrying nothing: %q", full)
	}
	if full == bare {
		t.Error("a worker that opened on the findings and one that opened blind read the same line")
	}
}

// AND AN ERRAND CUT BY ITS CALLER'S OWN DEADLINE IS STILL WRITTEN DOWN.
//
// This is the silence itself. The errand ladder checked the caller's context
// before it wrote the failure row, so the ONE failure that leaves a caller with
// nothing to say and no idea why — the deadline — was the one failure that left
// no trace. It is why the s4 journal could not say whether the writer had even
// been asked.
func TestAnErrandCutByItsCallersDeadlineIsStillWrittenDown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent := checkpointAgent(t, &deadCompleter{}, func(config *Config) { config.SessionFile = path })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := agent.callRole(ctx, roles.RoleHandoff, "", []ai.Message{
		textMessage("user", "write the brief"),
	}); err == nil {
		t.Fatal("an errand made on a dead context reported success")
	}

	rows := journaledErrors(t, path)
	if len(rows) == 0 {
		t.Fatal("an errand cut by its caller's deadline wrote nothing at all")
	}
	if rows[0].Role != string(roles.RoleHandoff) {
		t.Errorf("the row names %q, want the errand that made the call", rows[0].Role)
	}
	if strings.TrimSpace(rows[0].Message) == "" {
		t.Error("the row says a call failed and not one word about why")
	}
}

// AND A WEDGED FIRST RUNG LEAVES THE FALL-THROUGH TIME TO ANSWER.
//
// The caller's deadline used to bound only the context while every rung was
// still given the tier's whole patience, so one silent endpoint on rung one ate
// the caller's entire budget and the ladder's floor — the session's own model,
// alive by construction — was never asked. That is how a division review died
// on 2026-08-28: three minutes of provider silence, and the one model
// answering every other request in the session never heard the question. The
// budget is now shared across the rungs that remain, so the errand survives
// exactly one wedged endpoint, which is the failure the ladder exists for.
func TestAWedgedFirstRungLeavesTheFallThroughTimeToAnswer(t *testing.T) {
	agent := checkpointAgent(t, &wedgedFirstRung{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, model, err := agent.callRole(ctx, roles.RoleHandoff, "the-session-model",
		[]ai.Message{textMessage("user", "write the brief")})

	if err != nil {
		t.Fatalf("the errand died on one wedged rung: %v", err)
	}
	if model != "the-session-model" {
		t.Fatalf("the answer came from %q, want the fall-through rung", model)
	}
	if response == nil || response.Text() != "answered" {
		t.Fatalf("the fall-through's answer did not come back: %+v", response)
	}
}

// wedgedFirstRung sits silent for the whole of its context on the first model
// it is ever asked, and answers instantly on any other — one wedged endpoint
// and one live one, which is the shape of the measured failure.
type wedgedFirstRung struct {
	mu    sync.Mutex
	first string
}

func (w *wedgedFirstRung) CompleteWithMessages(ctx context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	w.mu.Lock()
	if w.first == "" {
		w.first = request.Model
	}
	first := w.first
	w.mu.Unlock()
	if request.Model == first {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return textResponse("answered"), nil
}

// ── the fixtures ────────────────────────────────────────────────────────────

// refusedWriterSteps is a grinding turn whose mastermind REFUSES: the sketch and
// the draft come back as scripted, and the request to write the brief is answered
// with a provider refusal carrying the upstream's own sentence.
func refusedWriterSteps(count int, draft, upstream string) []step {
	steps := grindingSteps(count, checkpointChainSketch, draft)
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedToWriteHandoff(messages) {
				return nil, refusalOf(429, upstream, "sail-research", `{"error":"capacity"}`)
			}
			return inner(ctx, messages)
		}
	}
	return steps
}

// deadCompleter answers every request with the context's own error, which is what
// an adapter does with a request made on a context that is already over.
type deadCompleter struct{}

func (deadCompleter) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, context.Canceled
}

func journaledCarries(t *testing.T, path string) []journalCarry {
	t.Helper()
	var rows []journalCarry
	for _, entry := range journaledEntries(t, path, "carry") {
		if entry.Carry != nil {
			rows = append(rows, *entry.Carry)
		}
	}
	return rows
}

func carriedRung(ladder []journalCarry) string {
	for _, rung := range ladder {
		if rung.Used {
			return rung.Rung
		}
	}
	return ""
}

func lastCeiling(t *testing.T, path string) journalCeiling {
	t.Helper()
	entries := journaledEntries(t, path, "ceiling")
	if len(entries) == 0 || entries[len(entries)-1].Ceiling == nil {
		t.Fatal("no ceiling was journaled")
	}
	return *entries[len(entries)-1].Ceiling
}

// ceilingNotice is the dim line the person read when their turn was moved.
func ceilingNotice(t *testing.T, events []Event) string {
	t.Helper()
	for _, event := range events {
		if event.Kind == EventNotice && strings.HasPrefix(event.Text, checkpointCeilingNote) {
			return event.Text
		}
	}
	t.Fatalf("no ceiling line reached the person; events: %v", kinds(events))
	return ""
}

// ── one model means one model at every rung ─────────────────────────────────
//
// THE MEASURED FAILURE, the second one. A canary chat run under `--one-model`
// handed off, and the person read "the brief could not be written: the second
// model could not be reached". Nothing had been unreachable. The flag withheld
// the roles ladder, the brief's writer hands that ladder an EMPTY floor because
// it is crew-only, and no pin plus no tier plus no floor is a role with no model
// — so the mark was never read and the brief was never written, and the worker
// opened on the bare paste and spent its first nine calls re-deriving the turn.

// UNDER THE FLAG A CREW-ONLY ERRAND RIDES THE CONVERSATION'S OWN MODEL.
//
// The caller still passes the empty floor, because its law is unchanged for
// every run that did not pass the flag. The seam is what knows better.
func TestUnderOneModelACrewOnlyErrandRidesTheConversationsModel(t *testing.T) {
	asked := &modelAsked{}
	agent := checkpointAgent(t, asked, func(config *Config) {
		config.Model = "the-one/model"
		// NO LADDER AT ALL, which is exactly what the door leaves behind under
		// `--one-model` (cmd/aforge's applyV3Governance).
		config.RolesSource = nil
		config.OneModel = true
	})

	response, model, err := agent.callRole(context.Background(), roles.RoleHandoff, "",
		[]ai.Message{textMessage("user", "write the brief")})
	if err != nil {
		t.Fatalf("a crew-only errand under --one-model had no model to call: %v", err)
	}
	if model != "the-one/model" {
		t.Errorf("the errand was answered by %q, want the conversation's own model", model)
	}
	if got := asked.model(); got != "the-one/model" {
		t.Errorf("the request was made against %q, want the conversation's own model", got)
	}
	if response == nil || response.Text() != "the brief" {
		t.Fatalf("the writer's answer did not come back: %+v", response)
	}
}

// AND WITHOUT THE FLAG THE CREW-ONLY REFUSAL STANDS, because it is a quality
// judgement about a profile that has a crew and this change does not touch it: an
// install with no mastermind still gets no brief writer rather than the running
// model editing the document it just wrote badly.
func TestWithoutOneModelACrewOnlyErrandStillRefusesToFallToTheConversation(t *testing.T) {
	agent := checkpointAgent(t, &modelAsked{}, func(config *Config) {
		config.Model = "the-one/model"
		config.RolesSource = nil
	})

	_, _, err := agent.callRole(context.Background(), roles.RoleHandoff, "",
		[]ai.Message{textMessage("user", "write the brief")})
	if !errors.Is(err, roles.ErrNoModel) {
		t.Fatalf("a crew-only errand with no crew answered %v, want roles.ErrNoModel", err)
	}
}

// AND A ROLE WITH NO MODEL READS AS ONE, never as a wire that failed. Four facts,
// four sentences: no model set, no answer in time, and anything else.
func TestARoleWithNoModelIsToldApartFromAProviderThatCouldNotBeReached(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
		want string
	}{
		{"no model for the role", fmt.Errorf("%w for role %q", roles.ErrNoModel, roles.RoleHandoff), carrySaidNoSecond},
		{"no client at all", errNoCompleter, carrySaidNoSecond},
		{"no answer in time", context.DeadlineExceeded, carrySaidTooSlow},
		{"anything else", errors.New("dial tcp: connection refused"), carrySaidUnreachable},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			reason, said := carryFault(testCase.err)
			if said != testCase.want {
				t.Errorf("the person is told %q, want %q", said, testCase.want)
			}
			// AND THE FILE KEEPS THE EXACT SENTENCE whatever the person is told,
			// because the autopsy's question is which row was never written.
			if !strings.Contains(reason, testCase.err.Error()) {
				t.Errorf("the journal reads %q, want the error's own words %q", reason, testCase.err)
			}
		})
	}
}

// modelAsked answers every request with the brief and remembers which model it
// was asked for, which is the whole of what a rung's resolution can be observed
// by from outside.
type modelAsked struct {
	mu   sync.Mutex
	seen string
}

func (m *modelAsked) CompleteWithMessages(_ context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	m.mu.Lock()
	m.seen = request.Model
	m.mu.Unlock()
	return textResponse("the brief"), nil
}

func (m *modelAsked) model() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seen
}
