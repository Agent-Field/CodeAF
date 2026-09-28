package head

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// forgeCraft records that this brain has learned a way of working by that name.
// The repository still owns what the craft IS; the journal is where a process
// without the repository open finds out that the name exists at all, which is
// what the verb triad resolves a target against.
func forgeCraft(t *testing.T, graph *store.Store, name string) {
	t.Helper()
	if _, err := graph.RecordCraftForged(store.CraftForged{Name: name, Commit: "abc123"}); err != nil {
		t.Fatal(err)
	}
}

// The two things a person SAYS about a way of working that are not simply
// asking for work — that last version was worse, stop doing it that way — reach
// the same commands the notebook page journals. One executor, two doors. Doing
// it that way again is not one of them any more: naming the way IS the ask, and
// it travels in a task's own words to the recognizer that knows the names.
func TestTheVerbTriadJournalsTheSameCraftCommandsThePageDoes(t *testing.T) {
	for _, spoken := range []struct {
		tool    string
		args    map[string]any
		kind    store.CommandKind
		words   string
		receipt string
	}{
		{tool: beltToolChange,
			args:    map[string]any{"target": "release-notes", "words": "the new link check misses half of them"},
			kind:    store.CommandCraftRevert,
			words:   "the new link check misses half of them",
			receipt: "Putting release-notes back to the version before this one."},
		{tool: beltToolStop,
			args:    map[string]any{"targets": []string{"release-notes"}, "words": "we ship notes by hand now"},
			kind:    store.CommandCraftRetire,
			words:   "we ship notes by hand now",
			receipt: "Not working the release-notes way any more."},
	} {
		t.Run(string(spoken.kind), func(t *testing.T) {
			graph := openHeadStore(t)
			forgeCraft(t, graph, "release-notes")
			user := postUser(t, graph, "craft-"+string(spoken.kind), "the release notes thing")
			run := &beltRun{head: New(nil, graph), user: user}

			answer, failed := run.execute(spoken.tool, beltArguments(t, spoken.args))
			if failed {
				t.Fatalf("%s failed: %s", spoken.kind, answer)
			}
			commands, err := graph.PendingCommands(0)
			if err != nil || len(commands) != 1 {
				t.Fatalf("commands = %+v err=%v", commands, err)
			}
			if commands[0].Kind != spoken.kind || commands[0].Target != "release-notes" ||
				commands[0].Instruction != spoken.words {
				t.Fatalf("journaled %+v", commands[0])
			}
			if len(run.did) != 1 || run.did[0] != spoken.receipt {
				t.Fatalf("receipt = %q", run.did)
			}
			// The receipt a person reads never says craft, workflow or version
			// numbers — it says what is happening in their own words.
			if strings.Contains(strings.ToLower(run.did[0]), "craft") {
				t.Fatalf("the receipt speaks the machinery's language: %q", run.did[0])
			}
		})
	}
}

// A revert with nothing behind it is refused where the model can still ask,
// rather than a tick later in a receipt: the repository will not move a version
// without a reason, and the conversation is still in hand right here.
func TestRevertingAWayOfWorkingWithoutAReasonIsRefusedAtTheBelt(t *testing.T) {
	graph := openHeadStore(t)
	forgeCraft(t, graph, "release-notes")
	user := postUser(t, graph, "craft-thin", "put it back")
	run := &beltRun{head: New(nil, graph), user: user}

	answer, failed := run.execute(beltToolChange, beltArguments(t, map[string]any{
		"target": "release-notes", "words": "worse",
	}))
	if !failed || !strings.Contains(answer, "got wrong") {
		t.Fatalf("a reasonless revert answered %q (failed=%v)", answer, failed)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 0 {
		t.Fatalf("a refused revert journaled %+v", commands)
	}
}

// A name nothing answers to is refused at the belt, and the refusal names all
// four places that were looked in. Making a craft the fall-through was the
// obvious shape and it was wrong: a mistyped job id would have gone off as a
// command against a workflow nobody has ever forged.
func TestAnUnknownNameIsRefusedNamingEverywhereItLooked(t *testing.T) {
	graph := openHeadStore(t)
	user := postUser(t, graph, "craft-bad", "do something to it")
	run := &beltRun{head: New(nil, graph), user: user}

	answer, failed := run.execute(beltToolChange, beltArguments(t, map[string]any{
		"target": "release-notes", "words": "no",
	}))
	if !failed || !strings.Contains(answer, "no learned way of working is called") {
		t.Fatalf("an unknown name answered %q (failed=%v)", answer, failed)
	}
	if commands, _ := graph.PendingCommands(0); len(commands) != 0 {
		t.Fatalf("a refused change journaled %+v", commands)
	}
}

// "Forget that" aimed at a forged tool has to take the tool off the shelf, not
// just quiet the belief that names it — a belief that went silent while its
// command stayed on the person's PATH is a half-done retirement, and the
// command kind is what does both halves in one place.
func TestForgettingAToolThatWasForgedRetiresTheToolRatherThanOnlyTheBelief(t *testing.T) {
	graph := openHeadStore(t)
	skill, err := graph.RecordSkillCandidate(store.RootID, "tool:imgshrink",
		"imgshrink squeezes screenshots", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ActivateSkill(skill.Seq, skill.Artifact, ""); err != nil {
		t.Fatal(err)
	}
	user := postUser(t, graph, "forget-tool", "stop using imgshrink, it mangles the colours")
	run := &beltRun{head: New(nil, graph), user: user}

	answer, failed := run.execute(beltToolForget, beltArguments(t, map[string]any{
		"belief": skill.Seq,
	}))
	if failed {
		t.Fatalf("forgetting a tool failed: %s", answer)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandSkillRetire {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if commands[0].Target != "" && commands[0].Instruction != user.Body {
		t.Fatalf("the retirement lost the person's own words: %+v", commands[0])
	}
	// The belief is left standing until the command applies — one retirement,
	// applied in one place, rather than a quarantine here and a bin sweep there.
	fact, found, err := graph.FactBySeq(skill.Seq)
	if err != nil || !found || fact.Status != store.FactActive {
		t.Fatalf("the belief was quarantined behind the command's back: %+v", fact)
	}
}

// An ordinary belief still goes quiet on the spot. Nothing about the tool path
// may slow down or complicate the common case.
func TestForgettingAnOrdinaryBeliefStillActsAtOnce(t *testing.T) {
	graph := openHeadStore(t)
	fact, err := graph.RecordFact(store.RootID, "user", store.FactPlain, "prefers tables over prose")
	if err != nil {
		t.Fatal(err)
	}
	user := postUser(t, graph, "forget-belief", "forget that")
	run := &beltRun{head: New(nil, graph), user: user}

	if answer, failed := run.execute(beltToolForget, beltArguments(t, map[string]any{
		"belief": fact.Seq,
	})); failed {
		t.Fatalf("forgetting failed: %s", answer)
	}
	after, found, err := graph.FactBySeq(fact.Seq)
	if err != nil || !found || after.Status != store.FactQuarantined {
		t.Fatalf("the belief is %+v", after)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 0 {
		t.Fatalf("forgetting an ordinary belief journaled a command: %+v", commands)
	}
}

// One submission is one job however many times the loop asks for it in one
// breath. Two identical spawns in a turn were two commands, two compiles, two
// plans and two "Here's my reading" lines under each other — the double line in
// the 2026-08-11 screenshots.
func TestTheSameWordsCommissionedTwiceInOneTurnAreOneJob(t *testing.T) {
	graph := openHeadStore(t)
	user := postUser(t, graph, "double", "audit last quarter's billing code")
	run := &beltRun{head: New(nil, graph), user: user}

	first, failed := run.execute(beltToolTask, beltArguments(t, map[string]any{
		"instruction": "audit last quarter's billing code",
	}))
	if failed {
		t.Fatalf("the first spawn failed: %s", first)
	}
	second, failed := run.execute(beltToolTask, beltArguments(t, map[string]any{
		"instruction": "audit last quarter's billing code",
	}))
	if failed {
		t.Fatalf("the repeat was an error rather than an answer: %s", second)
	}
	if !strings.Contains(second, "already in hand from this turn") {
		t.Fatalf("the repeat answered %q", second)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 1 {
		t.Fatalf("one submission journaled %d commands: %+v", len(commands), commands)
	}
	// And exactly one receipt, so the turn cannot say it twice either.
	if len(run.did) != 1 {
		t.Fatalf("receipts = %q", run.did)
	}
}

// Two genuinely different asks in one turn are still two jobs: the guard is
// about the same sentence, never about how many times the loop may commission.
func TestTwoDifferentAsksInOneTurnAreStillTwoJobs(t *testing.T) {
	graph := openHeadStore(t)
	user := postUser(t, graph, "two", "audit the billing code and write the release notes")
	run := &beltRun{head: New(nil, graph), user: user}

	for _, instruction := range []string{"audit the billing code", "write the release notes"} {
		if answer, failed := run.execute(beltToolTask, beltArguments(t, map[string]any{
			"instruction": instruction,
		})); failed {
			t.Fatalf("%q failed: %s", instruction, answer)
		}
	}
	commands, err := graph.PendingCommands(0)
	if err != nil || len(commands) != 2 {
		t.Fatalf("two asks journaled %d commands: %+v", len(commands), commands)
	}
}
