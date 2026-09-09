package tui3

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE DECISION, ANSWERED FROM THE CARD.
//
// Everything here is about one landing: work that has ended with a question on
// it. It used to be a card with a question printed on it and no way to answer,
// and every assertion below is about the two halves of fixing that — the answers
// are on the card, and pressing one spends the person's answer through the
// engine's own door.

// The words the ENGINE spells and this surface only draws
// (internal/session's task_status.go). They are written out HERE, in the test,
// so a change to either side has to be made deliberately in both: the surface
// itself must never hold a second copy of a state word
// (docs/design/task-states/DESIGN.md).
const (
	taskYourCallWord    = "your call"
	taskDoneStateWord   = "done"
	taskStoppedState    = "stopped"
	taskIncompleteState = "incomplete"
	askCheckReason      = "nobody could check it"
	askHeldReason       = "the check did not pass it"
	askConflictReason   = "conflicts with your branch"
)

// doneStatus is one landing card's reading built the way the surface builds it
// (taskdone.go's [doneNodeFacts]): a node this window watched, read once through
// [session.ProjectTask].
func doneStatus(facts session.TaskFacts) session.TaskStatus {
	facts.Liveness = session.TaskLivenessHeld
	return session.ProjectTask(facts)
}

// settleFake is a tasker that can also be answered (tasksettle.go's
// [settleAgent]). IT HAS NO MERGE ROUND, on purpose: the conflict card's `[a]`
// is a capability of its own, and this fake is what a surface without it looks
// like.
type settleFake struct {
	*taskFake
	resolved []settleCall
	handed   []uint64
	back     []uint64
	refuse   error
}

type settleCall struct {
	id     uint64
	answer session.TaskResolution
}

func (f *settleFake) ResolveUnverified(id uint64, resolution session.TaskResolution, why string) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.resolved = append(f.resolved, settleCall{id: id, answer: resolution})
	return nil
}

func (f *settleFake) HandUnverifiedToModel(id uint64) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.handed = append(f.handed, id)
	return nil
}

func (f *settleFake) TakeBackDecision(id uint64) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.back = append(f.back, id)
	return nil
}

// mergeFake is [settleFake] with the merge round on it (tasksettle.go's
// [conflictAgent]), which is the door the engine lane landed and the only thing
// that makes a conflict card's `[a]` real.
type mergeFake struct {
	*settleFake
	merged []uint64
}

func (f *mergeFake) ResolveConflict(id uint64) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.merged = append(f.merged, id)
	return nil
}

// settleApp is [taskApp] with a resolver under it and a profile of its own, so
// that any write a test provokes lands in a temporary directory and never in the
// person running the suite.
func settleApp(t *testing.T) (*app, *settleFake) {
	t.Helper()
	agent := newSettleFake()
	return newSettleApp(t, agent), agent
}

// mergeApp is [settleApp] with the merge round under it.
func mergeApp(t *testing.T) (*app, *mergeFake) {
	t.Helper()
	agent := &mergeFake{settleFake: newSettleFake()}
	return newSettleApp(t, agent), agent
}

func newSettleFake() *settleFake {
	return &settleFake{taskFake: &taskFake{
		fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4-flash"},
		updates:   make(chan session.Event, 8),
	}}
}

func newSettleApp(t *testing.T, agent Agent) *app {
	t.Helper()
	a := newTestApp(agent)
	a.width, a.height = 200, 24
	a.profileDir = t.TempDir()
	now := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	return a
}

// landUnverified puts one landed card that nobody could check in front of the
// person, and answers with it.
func landUnverified(t *testing.T, a *app) *taskDone {
	t.Helper()
	return landAsk(t, a, session.TaskNotice{
		Elapsed: 400 * time.Second, Merge: mergeWordAborted, Branch: "task/parser",
	})
}

// landAsk lands one node as the person's call under whatever facts a test names.
func landAsk(t *testing.T, a *app, notice session.TaskNotice) *taskDone {
	t.Helper()
	drive(t, a, streamEventMsg{gen: a.gen,
		ev: update(7, "Port the parser", session.TaskUnverified, notice)})
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil || card.status.Tier != session.TaskTierYourCall {
		t.Fatalf("no card that is the person's call landed: %+v", card)
	}
	return card
}

// ── the four shapes, drawn ──────────────────────────────────────────────────

// EVERY LANDING IS THREE ROWS AT MOST AND EVERY ROW HAS ONE JOB. The head says
// the tier, the word and the facts in one fixed order; the second row says the
// reason or the report and never both; the third is the answers, and only while
// there are any (docs/design/task-states/DESIGN.md).
func TestTheLandingCardDrawsTheFourShapes(t *testing.T) {
	for _, shape := range []struct {
		name  string
		card  *taskDone
		head  string
		glyph string
		under string
	}{{
		name: "done", glyph: glyphDone,
		card: &taskDone{
			title: "Port the parser", outcome: "the guard is in", span: 400 * time.Second,
			changed: []string{"a.go", "b.go"},
			status: doneStatus(session.TaskFacts{
				State: session.TaskDone, Merge: mergeWordMerged, Branch: "task/parser"}),
		},
		head:  " · " + taskDoneStateWord + " · 6m40s · 2 files · merged",
		under: `"the guard is in"`,
	}, {
		name: "stopped", glyph: glyphStopped,
		card: &taskDone{
			title: "Port the parser", span: 122 * time.Second,
			status: doneStatus(session.TaskFacts{
				State: session.TaskFailed, Ending: session.TaskEndingStopped, Stopped: true,
				Merge: mergeWordKept, Branch: "task/parser"}),
		},
		head: " · " + taskStoppedState + " · 2m02s · branch kept · task/parser",
	}, {
		name: "incomplete", glyph: glyphBad,
		card: &taskDone{
			title: "Port the parser", span: 242 * time.Second,
			changed: []string{"a.go"},
			status: doneStatus(session.TaskFacts{
				State: session.TaskFailed, Ending: session.TaskEndingSteps,
				Merge: mergeWordKept, Branch: "task/parser"}),
		},
		head:  " · " + taskIncompleteState + " · 4m02s · 1 file · branch kept · task/parser",
		under: "ran out of steps",
	}, {
		name: "your call", glyph: glyphAsk,
		card: &taskDone{
			title: "Port the parser", span: 400 * time.Second,
			changed: []string{"a.go", "b.go"},
			status: doneStatus(session.TaskFacts{
				State: session.TaskUnverified, Merge: mergeWordKept, Branch: "task/parser"}),
		},
		head: " · " + taskYourCallWord + " · 6m40s · 2 files · branch kept · task/parser",
	}} {
		t.Run(shape.name, func(t *testing.T) {
			a, _ := settleApp(t)
			head := plain(a.doneHead(shape.card, 200, false))
			if !strings.HasPrefix(head, shape.glyph+" ") {
				t.Fatalf("the head reads\n\t%q\nwant it to open with the tier glyph %q", head, shape.glyph)
			}
			if !strings.Contains(head, shape.head) {
				t.Fatalf("the head reads\n\t%q\nwant it to carry\n\t%q", head, shape.head)
			}
			if shape.under == "" {
				return
			}
			if under := plain(a.doneUnder(shape.card, 200)); !strings.Contains(under, shape.under) {
				t.Fatalf("the second row reads\n\t%q\nwant it to carry\n\t%q", under, shape.under)
			}
		})
	}
}

// THE WORDS THAT WERE DELETED ARE DELETED. Each of these was one surface's
// private name for a state that now has exactly one, and the card is where three
// of them were spelled.
func TestTheLandingCardHasStoppedSayingTheDeletedWords(t *testing.T) {
	a, _ := settleApp(t)
	card := landUnverified(t, a)
	card.open = true
	text := taskText(a)
	for _, gone := range []string{
		"awaiting review", "needs your look", "delivery needs attention",
		"what it produced was not taken as done", "stopped — branch kept",
		"finished, but nobody has checked it",
	} {
		if strings.Contains(text, gone) {
			t.Fatalf("the card still says %q:\n%s", gone, text)
		}
	}
	// AND `failed` IS NOT A LANDING'S WORD EITHER. The head says `incomplete`
	// plus the reason; the state keeps its name inside the engine.
	a, _ = settleApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Mix audio", session.TaskFailed,
		session.TaskNotice{Elapsed: time.Second, Ending: session.TaskEndingSteps})})
	if text := taskText(a); strings.Contains(text, " · failed") {
		t.Fatalf("a landing still reads `failed`:\n%s", text)
	}
}

// NO SENTENCE ON THE CARD ENDS IN `…` HIDING THE ONE INSTRUCTION. A reason
// carries a list and the key clause beside it is what a person acts on, so the
// list is what gets cut.
func TestANarrowReasonRowCutsTheListAndKeepsTheKey(t *testing.T) {
	card := &taskDone{
		report: "something", status: doneStatus(session.TaskFacts{
			State: session.TaskFailed, Ending: session.TaskEndingSteps}),
	}
	row := doneReasonRow("the check found gaps: a.go, b.go, c.go, d.go", doneReasonKey(card), 45)
	if !strings.HasSuffix(row, doneOutputKey) {
		t.Fatalf("the narrow reason row dropped its key:\n\t%q", row)
	}
	if !strings.HasPrefix(row, "the check found gaps") {
		t.Fatalf("the narrow reason row cut its own head off:\n\t%q", row)
	}
}

// ── the ask kinds, and which door each answer spends ────────────────────────

// THE CHIPS ARE ALWAYS THE SAME COLUMNS IN THE SAME ORDER, and what changes
// between one question and another is the WORDS on the first two — which are the
// engine's and not this surface's.
func TestEachAskKindDrawsItsOwnVerbsOnTheSameThreeColumns(t *testing.T) {
	for _, kind := range []struct {
		name    string
		notice  session.TaskNotice
		reason  string
		yes, no string
	}{{
		name:   "check",
		notice: session.TaskNotice{Elapsed: time.Second, Merge: mergeWordAborted, Branch: "task/parser"},
		reason: askCheckReason, yes: "accept", no: "not right",
	}, {
		name: "held",
		notice: session.TaskNotice{
			Elapsed: time.Second, Merge: mergeWordAborted, Branch: "task/parser",
			ResultHeld: true, Result: "the chapters",
		},
		reason: askHeldReason, yes: "accept anyway", no: "not right",
	}} {
		t.Run(kind.name, func(t *testing.T) {
			a, _ := settleApp(t)
			card := landAsk(t, a, kind.notice)
			if got := card.status.Ask.Reason; !strings.HasPrefix(got, kind.reason) {
				t.Fatalf("the reason reads %q, want it to open with %q", got, kind.reason)
			}
			text := taskText(a)
			// `[s] tell it` is asserted where there is somewhere to say it — a
			// surface with task pages under it (roomsettle_test.go). This fixture
			// has none, which is the absence law's own case further down.
			for _, want := range []string{
				kind.reason,
				settleYesKey + " " + kind.yes,
				settleNoKey + " " + kind.no,
				settleHandKey + settleHandWord,
			} {
				if !strings.Contains(text, want) {
					t.Fatalf("the card is missing %q:\n%s", want, text)
				}
			}
			// THE VOCABULARY LAW HOLDS ON THE ROW THAT SPENDS THE ENGINE'S VERBS.
			for _, never := range []string{"reaudit", "refute", "verdict", "auditor", "unverified"} {
				if strings.Contains(text, never) {
					t.Fatalf("the answers row says %q, which is machinery:\n%s", never, text)
				}
			}
		})
	}
}

// A CONFLICT ASKS A DIFFERENT QUESTION AND SPENDS A DIFFERENT DOOR. `resolve it`
// is one more merge round and never an accept: nothing is taken as done on the
// way past.
func TestAConflictCardResolvesThroughTheMergeRound(t *testing.T) {
	a, agent := mergeApp(t)
	card := landAsk(t, a, session.TaskNotice{
		Elapsed: time.Second, Merge: mergeWordConflicted, Branch: "task/parser",
		Conflicts: []string{"parser.go", "parser_test.go"},
	})
	text := taskText(a)
	for _, want := range []string{
		askConflictReason + ": parser.go, parser_test.go",
		settleYesKey + " resolve it",
		settleNoKey + " drop it",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the conflict card is missing %q:\n%s", want, text)
		}
	}
	a.settleCard(card, settleYes)
	if len(agent.merged) != 1 || agent.merged[0] != 7 {
		t.Fatalf("`resolve it` did not spend the merge round: %v", agent.merged)
	}
	if len(agent.resolved) != 0 {
		t.Fatalf("`resolve it` settled the node as well: %+v", agent.resolved)
	}
	if card.decided != settleResolveLine {
		t.Fatalf("the receipt reads %q, want %q", card.decided, settleResolveLine)
	}
}

// AND WITHOUT THAT DOOR THE CHIP IS ABSENT, NOT BROKEN. An engine that cannot
// merge for you still lets somebody drop the branch, so `[n]` stands on its own
// rather than the whole row going away.
func TestWithNoMergeRoundTheConflictCardHasNoResolveChip(t *testing.T) {
	a, agent := settleApp(t)
	card := landAsk(t, a, session.TaskNotice{
		Elapsed: time.Second, Merge: mergeWordConflicted, Branch: "task/parser",
		Conflicts: []string{"parser.go"},
	})
	text := taskText(a)
	if strings.Contains(text, settleYesKey+" resolve it") {
		t.Fatalf("a surface with no merge round offered to resolve:\n%s", text)
	}
	if !strings.Contains(text, settleNoKey+" drop it") {
		t.Fatalf("the answer that does work went away with the one that does not:\n%s", text)
	}
	// AND THE KEY IS AS ABSENT AS THE CHIP. A letter that is not drawn may not act.
	a.sel = len(a.entries) - 1
	drive(t, a, key("a"))
	if len(agent.resolved) != 0 || card.decided != "" {
		t.Fatalf("`a` acted on a card that does not draw it: %+v", agent.resolved)
	}
}

// PRESSING ACCEPT SPENDS THE ANSWER, and the card stops asking.
func TestAcceptingFromTheCardResolvesTheTask(t *testing.T) {
	a, agent := settleApp(t)
	card := landUnverified(t, a)

	a.settleCard(card, settleYes)

	if len(agent.resolved) != 1 || agent.resolved[0].id != 7 ||
		agent.resolved[0].answer != session.TaskAccept {
		t.Fatalf("the accept reached the engine as %+v", agent.resolved)
	}
	// THE LANDING ABOVE THE CHOICES IS NOT REWRITTEN. The head is a record of how
	// this work came home — the branch it kept included — and a surface that
	// re-lettered it "done" would be reporting a merge nobody told it about. The
	// engine's own second card says what became of the work.
	if card.status.Tier != session.TaskTierYourCall {
		t.Fatalf("the card's landing was rewritten: %+v", card.status)
	}
	if card.decided != settleTookLine {
		t.Fatalf("the card's receipt reads %q, want %q", card.decided, settleTookLine)
	}
	text := taskText(a)
	if !strings.Contains(text, settleTookLine) {
		t.Fatalf("the card does not say what was decided:\n%s", text)
	}
	// DECIDED MEANS THE CONTROLS ARE GONE, not greyed: the question and every
	// answer to it leave the card.
	for _, gone := range []string{askCheckReason, settleYesKey + " accept", settleHandWord} {
		if strings.Contains(text, gone) {
			t.Fatalf("an answered card still offers %q:\n%s", gone, text)
		}
	}
	// And a second press cannot double-spend it.
	a.settleCard(card, settleNo)
	if len(agent.resolved) != 1 {
		t.Fatalf("an answered card was answered again: %+v", agent.resolved)
	}
}

// "NOT RIGHT" IS THE REFUSAL.
func TestSayingItIsNotRightFailsTheTask(t *testing.T) {
	a, agent := settleApp(t)
	card := landUnverified(t, a)

	a.settleCard(card, settleNo)

	if len(agent.resolved) != 1 || agent.resolved[0].answer != session.TaskRefute {
		t.Fatalf("the refusal reached the engine as %+v", agent.resolved)
	}
	if card.status.Tier != session.TaskTierYourCall {
		t.Fatalf("the card's landing was rewritten: %+v", card.status)
	}
	if card.decided != settleNotRightLine {
		t.Fatalf("the receipt reads %q", card.decided)
	}
}

// `check again` LEFT THE ROW. The engine retries a check that never answered by
// itself, so a person offered `look again` was being asked to spend a round the
// machine had already spent.
func TestTheCardNoLongerOffersToCheckAgain(t *testing.T) {
	a, agent := settleApp(t)
	landUnverified(t, a)
	if text := taskText(a); strings.Contains(text, "look again") || strings.Contains(text, "check again") {
		t.Fatalf("the card still offers to check again:\n%s", text)
	}
	a.sel = len(a.entries) - 1
	drive(t, a, key("l"))
	if len(agent.resolved) != 0 {
		t.Fatalf("`l` still spends an answer: %+v", agent.resolved)
	}
}

// `[d]` HANDS OVER THIS CARD AND CHANGES NO SETTING. What used to stand here
// flipped `task.settle` to `auto` on the way past, which is a persistent
// preference disguised as an answer — and it is why the owner found a card with
// no choices and no explanation.
func TestLettingAforgeDecideThisOneWritesNoSetting(t *testing.T) {
	a, agent := settleApp(t)
	card := landUnverified(t, a)
	before := config.TaskSettleAt(a.profileDir)

	a.settleCard(card, settleHand)

	if got := config.TaskSettleAt(a.profileDir); got != before {
		t.Fatalf("task.settle moved to %q; the one-card hand-over writes no setting", got)
	}
	if len(agent.handed) != 1 || agent.handed[0] != 7 {
		t.Fatalf("this card was not handed over: %v", agent.handed)
	}
	if len(agent.resolved) != 0 {
		t.Fatalf("handing a decision over settled the node: %+v", agent.resolved)
	}
	if card.decided != settleHandedLine {
		t.Fatalf("the receipt reads %q, want %q", card.decided, settleHandedLine)
	}
}

// `[s] tell it` OPENS THE PAGE AND RESOLVES NOTHING. "Looks good" typed on a
// card must never silently become an accept.
func TestTellingItSaysSomethingAndSettlesNothing(t *testing.T) {
	a, agent := roomSettleApp(t, session.TaskUnverified)
	a.closeRoom()
	card := a.doneCardFor(7)

	a.settleCard(card, settleTell)

	if len(agent.resolved) != 0 || len(agent.handed) != 0 {
		t.Fatalf("`tell it` spent an answer: %+v / %v", agent.resolved, agent.handed)
	}
	if card.decided != "" {
		t.Fatalf("`tell it` marked the card answered: %q", card.decided)
	}
	if a.room == nil || a.room.id != 7 {
		t.Fatalf("`tell it` did not point the box at the task: %+v", a.room)
	}
	if a.composerOwner != taskRecipient(7) {
		t.Fatalf("the box is talking to %+v, want the task", a.composerOwner)
	}
}

// AND WHERE THERE IS NOWHERE TO SAY IT, THE COLUMN IS ABSENT. A window whose
// agent has no task pages has nowhere to point the box, so the chip is not drawn
// and its letter does nothing — never a key that opens a refusal.
func TestWithNoTaskPagesThereIsNoTellItColumn(t *testing.T) {
	a, _ := settleApp(t)
	card := landUnverified(t, a)
	if text := taskText(a); strings.Contains(text, settleTellKey+settleTellWord) {
		t.Fatalf("a surface with no task pages offered to tell one something:\n%s", text)
	}
	a.sel = len(a.entries) - 1
	drive(t, a, key("s"))
	if a.room != nil {
		t.Fatalf("`s` opened a page this surface cannot draw: %+v", a.room)
	}
	if card.decided != "" {
		t.Fatalf("`s` marked the card answered: %q", card.decided)
	}
}

// ── who is holding the question ─────────────────────────────────────────────

// UNDER `auto` THE CARD SAYS WHO IS DECIDING, and offers the way back. A card
// with no choices and no explanation is the defect this row closes.
func TestUnderAutoTheCardNamesTheDeciderAndOffersTakeBack(t *testing.T) {
	a, agent := settleApp(t)
	card := landAsk(t, a, session.TaskNotice{
		Elapsed: time.Second, Merge: mergeWordAborted, Branch: "task/parser",
		Decider: session.TaskAskOwnerModel,
	})
	text := taskText(a)
	for _, want := range []string{askCheckReason, "aforge is deciding", settleBackKey + settleBackWord} {
		if !strings.Contains(text, want) {
			t.Fatalf("the card under auto is missing %q:\n%s", want, text)
		}
	}
	// AND NO CHIPS AND NO SECOND PROMPT: the question is somebody else's.
	for _, gone := range []string{settleYesKey + " accept", settleHandKey + settleHandWord} {
		if strings.Contains(text, gone) {
			t.Fatalf("the card offers %q while aforge is deciding:\n%s", gone, text)
		}
	}

	// `t` TAKES IT BACK AND RESOLVES NOTHING.
	a.sel = len(a.entries) - 1
	drive(t, a, key("t"))
	if len(agent.back) != 1 || agent.back[0] != 7 {
		t.Fatalf("`t` did not take the decision back: %v", agent.back)
	}
	if len(agent.resolved) != 0 || card.decided != "" {
		t.Fatalf("taking a decision back settled the node: %+v / %q", agent.resolved, card.decided)
	}
	if text := taskText(a); !strings.Contains(text, settleYesKey+" accept") {
		t.Fatalf("the chips did not come back:\n%s", text)
	}
}

// AND THE FLOOR HANDS IT BACK BY ITSELF. A task never stays unowned past the end
// of a turn: the engine publishes an ordinary update whose only news is who is
// deciding, and the card that is already on the page picks up its chips rather
// than a second landing appearing under it.
func TestTheFloorHandsTheQuestionBackAndTheCardDrawsItsChips(t *testing.T) {
	a, _ := settleApp(t)
	card := landAsk(t, a, session.TaskNotice{
		Elapsed: time.Second, Merge: mergeWordAborted, Branch: "task/parser",
		Decider: session.TaskAskOwnerModel,
	})
	entries := len(a.entries)

	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		session.TaskNotice{
			Elapsed: time.Second, Merge: mergeWordAborted, Branch: "task/parser",
			Decider: session.TaskAskOwnerPerson,
		})})

	if len(a.entries) != entries {
		t.Fatalf("the hand-back wrote %d more blocks into the conversation", len(a.entries)-entries)
	}
	if card.status.Ask.Owner != session.TaskAskOwnerPerson {
		t.Fatalf("the card is still owned by %q", card.status.Ask.Owner)
	}
	text := taskText(a)
	if !strings.Contains(text, settleYesKey+" accept") {
		t.Fatalf("the handed-back card draws no chips:\n%s", text)
	}
	if strings.Contains(text, "aforge is deciding") {
		t.Fatalf("the handed-back card still says aforge is deciding:\n%s", text)
	}
}

// ── refusals ────────────────────────────────────────────────────────────────

// A DECISION SOMEBODY ELSE ALREADY MADE IS A REFRESH, NOT AN ALERT.
func TestAnAlreadyAnsweredCardRefreshesQuietly(t *testing.T) {
	a, agent := settleApp(t)
	card := landUnverified(t, a)
	agent.refuse = fmt.Errorf("task 7 is done, and only a task that needs a look is waiting on somebody to decide: %w",
		session.ErrTaskDecided)
	entries := len(a.entries)

	a.settleCard(card, settleYes)

	if len(a.entries) != entries {
		t.Fatalf("the refusal wrote %d blocks into the conversation", len(a.entries)-entries)
	}
	if card.decided != settleGoneLine {
		t.Fatalf("the card reads %q, want %q", card.decided, settleGoneLine)
	}
	if text := taskText(a); strings.Contains(text, "waiting on somebody to decide") {
		t.Fatalf("the engine's refusal was put on screen:\n%s", text)
	}
}

// AND A REFUSAL THAT IS NOT A DECISION LEAVES THE QUESTION STANDING.
func TestAnAnswerThatCouldNotBeTakenKeepsTheChoices(t *testing.T) {
	a, agent := settleApp(t)
	card := landUnverified(t, a)
	agent.refuse = errors.New("task.audit is off, so there is no auditor to ask")

	a.settleCard(card, settleNo)

	if card.decided != "" {
		t.Fatalf("the card was marked answered by a refusal: %q", card.decided)
	}
	text := taskText(a)
	if !strings.Contains(text, settleTroubleLine) {
		t.Fatalf("the card does not say the answer could not be taken:\n%s", text)
	}
	if !strings.Contains(text, settleYesKey+" accept") {
		t.Fatalf("the remaining answers went away with the one that failed:\n%s", text)
	}
	// AND THE ENGINE'S OWN SENTENCE STAYS OFF THE SCREEN: it names the apparatus,
	// which is the one vocabulary this surface may never spend on a person.
	for _, never := range []string{"task.audit", "auditor"} {
		if strings.Contains(text, never) {
			t.Fatalf("the engine's refusal reached the person (%q):\n%s", never, text)
		}
	}
	// A second answer that works clears it.
	agent.refuse = nil
	a.settleCard(card, settleYes)
	if card.trouble != "" || card.decided != settleTookLine {
		t.Fatalf("the trouble line survived a decision: %+v", card)
	}
}

// AND AN ORDINARY LANDING OFFERS NOTHING. There is nothing to decide about work
// that came home.
func TestADoneCardHasNoAnswersRow(t *testing.T) {
	a, _ := settleApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskDone,
		session.TaskNotice{Elapsed: time.Second, Merge: mergeWordMerged})})
	if text := taskText(a); strings.Contains(text, settleYesKey+" accept") {
		t.Fatalf("a finished card asks to be decided about:\n%s", text)
	}
}

// AND SO DOES A SURFACE WITH NOTHING BEHIND THE CHOICES. The absence law: a
// capability that cannot work is absent rather than drawn and broken.
func TestWithNoResolverTheCardDoesNotOfferChoices(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		session.TaskNotice{Elapsed: time.Second})})
	if text := taskText(a); strings.Contains(text, settleYesKey+" accept") {
		t.Fatalf("a surface that cannot answer offered to:\n%s", text)
	}
}

// ── the keyboard and the pointer ────────────────────────────────────────────

// THE KEYS AND THE POINTER ANSWER THE SAME ROW, and the keys are letters — so
// every guard `x` has is on them.
func TestTheAnswersAnswerToKeysAndToTheColumn(t *testing.T) {
	a, agent := settleApp(t)
	landUnverified(t, a)

	// A letter with nothing selected is a letter, and goes in the box.
	drive(t, a, key("a"))
	if len(agent.resolved) != 0 {
		t.Fatalf("a letter typed with no card selected answered one: %+v", agent.resolved)
	}
	a.input.reset()

	// Selected, over an empty box: the same letter is the answer.
	a.sel = len(a.entries) - 1
	drive(t, a, key("a"))
	if len(agent.resolved) != 1 || agent.resolved[0].answer != session.TaskAccept {
		t.Fatalf("the key did not answer the selected card: %+v", agent.resolved)
	}

	// And the pointer answers the same columns the row was drawn with.
	b, other := settleApp(t)
	second := landUnverified(t, b)
	_ = taskText(b)
	if len(second.chips) != 3 {
		t.Fatalf("the row recorded %d pressable answers, want 3", len(second.chips))
	}
	if !b.settlePress(len(b.entries)-1, second.chips[1].span.from+1) {
		t.Fatal("a press on the answers row was not taken")
	}
	if len(other.resolved) != 1 || other.resolved[0].answer != session.TaskRefute {
		t.Fatalf("the second chip answered %+v", other.resolved)
	}
}

// A LETTER WITH A SENTENCE IN THE BOX IS A LETTER. This is the rule every key on
// this surface that is also a letter is held to, and it matters more here than
// anywhere: the thing on the other side of it is somebody's work being called
// finished.
func TestALetterTypedIntoASentenceNeverAnswersACard(t *testing.T) {
	a, agent := settleApp(t)
	landUnverified(t, a)
	a.sel = len(a.entries) - 1
	drive(t, a, key("h"), key("a"))
	if len(agent.resolved) != 0 {
		t.Fatalf("typing answered a card: %+v", agent.resolved)
	}
	if got := a.input.String(); got != "ha" {
		t.Fatalf("the box holds %q, want the letters that were typed", got)
	}
}

// ── the hierarchy ───────────────────────────────────────────────────────────

// child is one node the engine says was handed out by another.
func child(id, parent uint64, state session.TaskState) session.Event {
	return update(id, "Port the parser", state, session.TaskNotice{Parent: parent, Elapsed: time.Second})
}

// A PARENT'S RUNNING IS A FOLD, NOT A MUTE (session's pending.go, issue #268).
//
// While the parent works, its own agent is the one being asked about the child —
// so the child's demand FOLDS: the family head is the loud row. What it may
// never do is disappear. Before this the child was filed under `done` while its
// parent ran, so the footer counted a node nobody had decided as finished, and
// the card that carries the answers row was never written at all — a nested
// gate could expire without anybody ever being able to see it.
func TestAChildThatNeedsALookWaitsForItsParentAndThenForYou(t *testing.T) {
	a, _ := settleApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Rebuild the index", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: child(2, 1, session.TaskUnverified)},
	)
	kid := a.tasks[2]
	// NEVER DONE. A node nobody has decided about is not finished, whoever is
	// being asked about it.
	if group := a.railGroupOf(kid); group == railDone {
		t.Fatalf("a child nobody has decided about is filed under %q", railGroupWords[group])
	}
	// THE FOLD IS THE LOUDNESS AND NOT THE PRESENCE.
	if rank := a.railGlyphRank(kid); rank == 0 {
		t.Fatal("a folded family wore a demand its own head is already holding")
	}
	// AND THE CARD EXISTS ALREADY, which is the whole defect: the answers row
	// lives on the card and nowhere else, so a decision with no card is a gate
	// nobody can answer.
	if card := a.doneCardFor(2); card == nil || card.status.Tier != session.TaskTierYourCall {
		t.Fatalf("a nested landing that is the person's call wrote no card: %+v", card)
	}

	// The parent lands. Nobody is reading that child's news any more, so it is
	// the person's.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Rebuild the index", session.TaskDone,
		session.TaskNotice{Elapsed: time.Second, Merge: mergeWordMerged})})
	if group := a.railGroupOf(kid); group != railAttention {
		t.Fatalf("an orphaned child is filed under %q, want %q",
			railGroupWords[group], railGroupWords[railAttention])
	}
	if rank := a.railGlyphRank(kid); rank != 0 {
		t.Fatalf("the orphaned child ranks %d, want the loudest", rank)
	}

	// AND THEN FOR YOU, IN AS MANY WORDS. The conversation draws the answers row
	// for the nested node exactly as it does for a root.
	text := taskText(a)
	for _, want := range []string{askCheckReason, settleYesKey + " accept"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the nested card is missing %q:\n%s", want, text)
		}
	}
	// THE ROSTER'S ROW ASKS IT TOO, with the cursor on the child.
	drive(t, a, altT())
	for range 4 {
		if a.railWhere.id == 2 {
			break
		}
		drive(t, a, key("down"))
	}
	if a.railWhere.id != 2 {
		t.Fatalf("the roster cursor stands on %+v, want the child that needs a look", a.railWhere)
	}
	if card := a.railSettleCard(); card == nil || card.id != 2 {
		t.Fatalf("the roster row for a nested decision offers nothing: %+v", card)
	}
	drive(t, a, key("esc"))
}

// AND PRESSING `a` ON A NESTED DECISION RESOLVES THAT NODE. The engine's door
// takes an id and knows nothing about depth; what was missing was any way to
// reach it.
func TestANestedDecisionCanBeAcceptedFromTheCard(t *testing.T) {
	a, agent := settleApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Rebuild the index", session.TaskDone,
			session.TaskNotice{Elapsed: time.Second, Merge: mergeWordMerged})},
		streamEventMsg{gen: a.gen, ev: child(2, 1, session.TaskUnverified)},
	)
	if a.doneCardFor(2) == nil {
		t.Fatal("no card for the nested decision")
	}
	a.sel = a.doneEntryFor(2)
	_ = taskText(a)
	drive(t, a, key("a"))

	if len(agent.resolved) != 1 || agent.resolved[0].id != 2 ||
		agent.resolved[0].answer != session.TaskAccept {
		t.Fatalf("the accept reached the engine as %+v", agent.resolved)
	}
}

// AND A ROOT IS ALWAYS THE PERSON'S. Nothing is above it that could be asked.
func TestARootThatNeedsALookIsAlwaysAttention(t *testing.T) {
	a, _ := settleApp(t)
	landUnverified(t, a)
	if group := a.railGroupOf(a.tasks[7]); group != railAttention {
		t.Fatalf("a root that needs a look is filed under %q", railGroupWords[group])
	}
}

// ── the wire between the card and the engine (#706) ─────────────────────────

// reachFake is [settleFake] with the one extra question an agent that lives on a
// wire answers (tasksettle.go's [settleReach]).
type reachFake struct {
	*settleFake
	reaches bool
}

func (f *reachFake) SettleSupported() bool { return f.reaches }

// A REPLAYED LANDING NOBODY RECORDED AN OWNER FOR IS THE PERSON'S, and it draws
// its chips.
//
// THIS IS THE SHAPE THAT REACHES A WINDOW ON ATTACH. A graph the process left
// behind is replayed as ordinary notices, and a notice written by a build that
// never heard of [session.TaskAskOwner] carries the zero value — which the
// engine reads as the person, because work whose owner nobody wrote down is work
// waiting on whoever is looking at it (task_status.go's [taskDeciderOf]).
func TestAReplayedLandingWithNoRecordedOwnerAsksThePerson(t *testing.T) {
	a, _ := settleApp(t)
	card := landAsk(t, a, session.TaskNotice{
		Elapsed: 42 * time.Second, Merge: mergeWordAborted, Branch: "task/parser",
		Report: "the parser is ported and its tests run",
	})
	if card.status.Ask.Owner != session.TaskAskOwnerPerson {
		t.Fatalf("a replayed landing with no recorded owner reads as %q", card.status.Ask.Owner)
	}
	text := taskText(a)
	for _, want := range []string{askCheckReason, settleYesKey + " accept", settleNoKey + " not right"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the replayed landing draws no %q:\n%s", want, text)
		}
	}
}

// AND A WINDOW ON AN ENGINE HOST DRAWS THE SAME ROW, which is #706.
//
// The surface asserts its doors on the agent it holds, and an agent that lives
// on a wire has every method whatever the machine at the far end is — so the
// assertion is not the question. The welcome is ([settleReach]), and until it
// was asked a hosted landing card drew its reason with nothing to press.
func TestALandingOnAnEngineThatCanDecideDrawsItsAnswers(t *testing.T) {
	agent := &reachFake{settleFake: newSettleFake(), reaches: true}
	a := newSettleApp(t, agent)
	landUnverified(t, a)
	text := taskText(a)
	if !strings.Contains(text, settleYesKey+" accept") || !strings.Contains(text, settleNoKey+" not right") {
		t.Fatalf("a landing on a reachable engine draws no answers:\n%s", text)
	}
	a.sel = len(a.entries) - 1
	drive(t, a, key("a"))
	if len(agent.resolved) != 1 || agent.resolved[0].answer != session.TaskAccept {
		t.Fatalf("the answer was not spent through the engine: %+v", agent.resolved)
	}
}

// AND AN ENGINE THAT CANNOT IS ABSENT RATHER THAN BROKEN. The reason still
// stands — it says what is being asked, and a window that cannot spend an answer
// must still say that — and there is nothing on the row to press.
func TestALandingOnAnEngineThatCannotDecideDrawsItsReasonAndNoChips(t *testing.T) {
	agent := &reachFake{settleFake: newSettleFake(), reaches: false}
	a := newSettleApp(t, agent)
	landUnverified(t, a)
	text := taskText(a)
	if !strings.Contains(text, askCheckReason) {
		t.Fatalf("a landing nobody can answer stopped saying what it is asking:\n%s", text)
	}
	for _, gone := range []string{settleYesKey + " accept", settleNoKey + " not right", settleHandKey} {
		if strings.Contains(text, gone) {
			t.Fatalf("an engine that cannot decide a landing drew %q:\n%s", gone, text)
		}
	}
}

// AND THE MERGE ROUND GOES WITH THEM. A conflict card on an unreachable engine
// offers neither its yes nor its no, for the same reason.
func TestAConflictOnAnEngineThatCannotDecideOffersNothing(t *testing.T) {
	agent := &reachFake{settleFake: newSettleFake(), reaches: false}
	a := newSettleApp(t, agent)
	landAsk(t, a, session.TaskNotice{
		Elapsed: 42 * time.Second, Merge: mergeWordConflicted, Branch: "task/parser",
		Conflicts: []string{"parser.go"},
	})
	text := taskText(a)
	if !strings.Contains(text, askConflictReason) {
		t.Fatalf("a conflict nobody can answer stopped naming the clash:\n%s", text)
	}
	if strings.Contains(text, settleYesKey+" resolve it") || strings.Contains(text, settleNoKey+" drop it") {
		t.Fatalf("an engine that cannot merge offered to:\n%s", text)
	}
}
