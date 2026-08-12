package head

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The verb triad, and the doors that used to stand where it stands.
//
// What is pinned here is not that three tools exist — it is that each of the
// ten they replaced still reaches the same command with the same guard. A merge
// that quietly dropped one of them would leave a person's sentence with nowhere
// to land, and every case below is one of those sentences.

// liveJob puts one piece of the person's own work on the board, running.
func liveJob(t *testing.T, graph *store.Store, id, title, intent string) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: id, Title: title, Brief: intent, Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "room", Intent: intent}); err != nil {
		t.Fatal(err)
	}
	if _, won, err := graph.Claim(id, "worker"); err != nil || !won {
		t.Fatalf("claim %s: won=%v err=%v", id, won, err)
	}
}

// The bug, replayed from the user's own journal: a task is commissioned, and
// forty seconds later the person changes one of its constraints. That is the
// task being corrected, and it used to become a second identical task — two
// plans, two workforces, two bills, and two identical commitment lines.
//
// The door that caught it was a prefix list (amend.go) reading the person's
// sentence for openings a revision announces itself with. The list is gone: the
// judgment is the model's, the tool is change, and what the words MEAN is not
// decided here at all — CommandRedirect carries them verbatim to the party that
// can see the plan.
func TestAFollowUpAboutLiveWorkGoesToThatWorkVerbatim(t *testing.T) {
	graph := openHeadStore(t)
	liveJob(t, graph, "stocks", "Twenty best stocks",
		"Produce a ranked list of the 20 best stocks with the reasoning behind each")
	user := postUser(t, graph, "room", "no you can use internet search")
	run := &beltRun{head: New(nil, graph), user: user}

	answer, failed := run.execute(beltToolChange, beltArguments(t, map[string]any{
		"target": "stocks", "words": user.Body,
	}))
	if failed {
		t.Fatalf("the change was refused outright: %s", answer)
	}
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("one change journaled %d commands: %+v", len(commands), commands)
	}
	if commands[0].Kind != store.CommandRedirect || commands[0].Target != "stocks" {
		t.Fatalf("the change did not go to the running work: %+v", commands[0])
	}
	// Their words, verbatim, are what the judge reads — never the model's
	// paraphrase, and never a flavor the head picked for it.
	if commands[0].Instruction != user.Body {
		t.Fatalf("the revision carries %q rather than what they said", commands[0].Instruction)
	}
	if !strings.Contains(answer, "hear those words") {
		t.Fatalf("the loop was not told what actually happens next: %q", answer)
	}
	if len(run.did) != 1 || !strings.Contains(run.did[0], "Twenty best stocks") {
		t.Fatalf("the receipt does not name the work it changed: %q", run.did)
	}
}

// The settled half. "That's wrong, do it again" is not a redirect — there is no
// remaining plan to edit — and it is not a fresh job either, because the whole
// point is that the previous attempt goes back in with the criticism. It is one
// argument on task now, and everything correction.go wrote is still what gets
// written.
func TestATaskThatAmendsSettledWorkTakesTheCorrectionPath(t *testing.T) {
	graph := openHeadStore(t)
	const delivered = "Dear Mr Okafor, I am writing to formally notify you of persistent mould in the bathroom."
	deliverJob(t, graph, "polite", "landlord-letter", "Landlord mould letter",
		"write my landlord about the mould", delivered)

	const ask = "make it warmer and less legal"
	user := postUser(t, graph, "polite", ask)
	run := &beltRun{head: New(nil, graph), user: user}

	answer, failed := run.execute(beltToolTask, beltArguments(t, map[string]any{
		"amends": "landlord-letter", "instruction": ask,
	}))
	if failed {
		t.Fatalf("amending delivered work was refused: %s", answer)
	}
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("one amendment journaled %d commands: %+v", len(commands), commands)
	}
	command := commands[0]
	if command.Kind != store.CommandSplice || command.Target != "landlord-letter" {
		t.Fatalf("the amendment did not go back to the letter: %s at %q", command.Kind, command.Target)
	}
	if !strings.HasPrefix(command.Instruction, ask) {
		t.Fatalf("the user's words are not verbatim at the front:\n%s", command.Instruction)
	}
	for _, want := range []string{
		CorrectionPrefix + " landlord-letter", delivered, correctionDisputeLine,
	} {
		if !strings.Contains(command.Instruction, want) {
			t.Fatalf("the correction block lost %q:\n%s", want, command.Instruction)
		}
	}
	// And the receipt says which deliverable it is about, because that is the
	// only thing the command actually promises.
	if len(run.did) != 1 || !strings.Contains(run.did[0], "Landlord mould letter") {
		t.Fatalf("the receipt does not name the deliverable it redoes: %q", run.did)
	}
}

// A rule id and a sentence that could mean three different edits. Nothing is
// guessed: the candidates come back in the person's terms, and the journal is
// untouched — the same shape every ambiguity in this package has.
func TestAChangeToARuleWithAmbiguousWordsHandsBackCandidates(t *testing.T) {
	graph := openHeadStore(t)
	charter := activateReminder(t, graph, "plants",
		"remind me every sunday to water the plants", "every sunday", "water the plants")

	user := postUser(t, graph, "rules", "sort that one out for me")
	run := &beltRun{head: New(nil, graph), user: user}

	answer, failed := run.execute(beltToolChange, beltArguments(t, map[string]any{
		"target": charter.ID, "words": user.Body,
	}))
	if failed {
		t.Fatalf("an unreadable rule change errored instead of offering candidates: %s", answer)
	}
	for _, want := range []string{"when it runs", "what it says", "going back to asking"} {
		if !strings.Contains(answer, want) {
			t.Fatalf("the candidates do not offer %q:\n%s", want, answer)
		}
	}
	if run.acted {
		t.Fatal("offering candidates counted as acting")
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("an ambiguous rule change journaled %+v", commands)
	}
}

// The consent gate is reached by the same road control used, so a withdrawal
// wide enough to cross it stops at the question with nothing journaled.
func TestStoppingAWideSetRaisesTheConsentGate(t *testing.T) {
	graph := openHeadStore(t)
	ids := make([]string, 0, SurgeryCascadeGateNodes+2)
	for index := 0; index <= SurgeryCascadeGateNodes+1; index++ {
		id := "wide-" + string(rune('a'+index))
		spliceSurgeryJob(t, graph, id, "Wide job "+id, "do the wide thing")
		ids = append(ids, id)
	}
	user := postUser(t, graph, "wide", "cancel all of those")
	run := &beltRun{head: New(nil, graph), user: user}

	answer, failed := run.execute(beltToolStop, beltArguments(t, map[string]any{"targets": ids}))
	if failed {
		t.Fatalf("a wide withdrawal errored instead of asking: %s", answer)
	}
	if !strings.HasPrefix(answer, "needs_confirmation:") ||
		!strings.Contains(answer, "NOTHING has changed") {
		t.Fatalf("the gate did not claim the turn: %q", answer)
	}
	if run.confirm == nil || run.confirm.kind != store.CommandCancel {
		t.Fatalf("the confirmation carries %+v", run.confirm)
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("a gated withdrawal journaled %+v", commands)
	}
	// And nothing else may act while the person is being asked.
	if answer, failed := run.execute(beltToolStop,
		beltArguments(t, map[string]any{"targets": ids[:1]})); !failed ||
		!strings.Contains(answer, "already been asked to confirm") {
		t.Fatalf("a second withdrawal acted over an open consent question: %q failed=%t", answer, failed)
	}
}

// Withdrawal takes one reading from the words and only one: held, or ended.
func TestStopReadsHoldingFromTheWordsAndNothingElse(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "migration", "Schema migration", "migrate the schema")
	user := postUser(t, graph, "hold", "hold that for now")
	run := &beltRun{head: New(nil, graph), user: user}

	if answer, failed := run.execute(beltToolStop, beltArguments(t, map[string]any{
		"targets": []string{"migration"}, "words": user.Body})); failed {
		t.Fatalf("holding was refused: %s", answer)
	}
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 || commands[0].Kind != store.CommandPause {
		t.Fatalf("holding did not pause: %+v", commands)
	}
}

// separate is the person's own override on admission control, and the ONLY one.
// Without it the funnel refuses a twin of work already waiting; with it the
// second job is admitted, because they said in so many words that it runs
// beside the first.
func TestSeparateIsTheOneOverrideOnTheDuplicateGuard(t *testing.T) {
	graph := openHeadStore(t)
	const ask = "produce a ranked list of the twenty best stocks with reasoning"
	first := postUser(t, graph, "room", ask)
	if answer, failed := (&beltRun{head: New(nil, graph), user: first}).execute(
		beltToolTask, beltArguments(t, map[string]any{"instruction": ask})); failed {
		t.Fatalf("the first ask was refused: %s", answer)
	}

	// A new turn, so the turn-level guard cannot be what catches it.
	second := postUser(t, graph, "room", "start the twenty best stocks list please")
	repeat := &beltRun{head: New(nil, graph), user: second}
	answer, failed := repeat.execute(beltToolTask, beltArguments(t, map[string]any{
		"instruction": "produce a ranked list of the twenty best stocks with the reasoning"}))
	if failed {
		t.Fatalf("the twin was an error rather than an answer: %s", answer)
	}
	if !strings.Contains(answer, "already queued from an earlier ask") {
		t.Fatalf("the twin answered %q", answer)
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 1 {
		t.Fatalf("a twin was admitted: %+v", commands)
	}

	deliberate := &beltRun{head: New(nil, graph), user: second}
	if answer, failed := deliberate.execute(beltToolTask, beltArguments(t, map[string]any{
		"instruction": "produce a ranked list of the twenty best stocks with the reasoning",
		"separate":    true})); failed {
		t.Fatalf("an explicitly separate job was refused: %s", answer)
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 2 {
		t.Fatalf("the override did not admit the second job: %+v", commands)
	}
}

// The model words the task tool was given reach the resolver that owns the
// catalog, on the marked line every other route already uses.
func TestTaskCarriesTheModelWordsItWasGiven(t *testing.T) {
	graph := openHeadStore(t)
	user := postUser(t, graph, "model", "research the market with the better model")
	run := &beltRun{head: New(nil, graph), user: user}

	if answer, failed := run.execute(beltToolTask, beltArguments(t, map[string]any{
		"instruction": "research the market", "model": "the better model"})); failed {
		t.Fatalf("a task naming a model was refused: %s", answer)
	}
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("commands = %+v", commands)
	}
	words, wanted := RecognizeModelWords(commands[0].Instruction)
	if !wanted || !words.Boost {
		t.Fatalf("the task lost the model words:\n%s", commands[0].Instruction)
	}
	// The person's own sentence is still the whole of the instruction's front.
	if !strings.HasPrefix(commands[0].Instruction, "research the market") {
		t.Fatalf("the marked line displaced the ask:\n%s", commands[0].Instruction)
	}
	// And a task that named no model comes back byte-identical.
	plain := postUser(t, graph, "model", "research the bond market")
	quiet := &beltRun{head: New(nil, graph), user: plain}
	if _, failed := quiet.execute(beltToolTask, beltArguments(t, map[string]any{
		"instruction": "research the bond market"})); failed {
		t.Fatal("a task naming no model was refused")
	}
	after := pendingCommandsOf(t, graph)
	if after[len(after)-1].Instruction != "research the bond market" {
		t.Fatalf("a task with no model words was rewritten: %q", after[len(after)-1].Instruction)
	}
}

// The prompt has to state the one thing the code cannot enforce: which door a
// follow-up takes, and that the head does not decide what a change means.
func TestThePromptStatesTheAmendmentPrinciple(t *testing.T) {
	for _, phrase := range []string{
		"A follow-up about work in flight is a change to that work before it is a second job",
		"a task that amends it",
		"you never pick a verb for them",
	} {
		if !strings.Contains(orchestratorPrompt, phrase) {
			t.Errorf("the prompt no longer says %q", phrase)
		}
	}
}
