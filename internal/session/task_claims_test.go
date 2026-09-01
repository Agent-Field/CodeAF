package session

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE CLAIM THE GROUND CONTRADICTS ────────────────────────────────────────

// THE COUNTERFACTUAL, END TO END. This is the diff the dogfood run of
// 2026-08-31 produced and nothing caught: its own note said the `$0.00`
// exception had been taken out of the status line, while the function that
// returns `$0.00` was still there and still being called. The work met its
// acceptance, the manual gates were satisfied — they check that a name is
// mentioned, never that the sentence around it is true — and a reviewer reading
// the note would have signed it off.
//
// It now fails its check, and the finding NAMES THE CLAIM, because a person told
// "incomplete" about a change whose code is fine needs to know it is the
// sentence that is wrong.
func TestALandingClaimingAStringIsGoneWhileItRemainsFailsItsCheckNamingTheClaim(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "cccc1111cccc1111", 1, "take the $0.00 exception off the status line")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	// The code as the run actually left it: the exception is still there.
	writeFile(t, filepath.Join(tree.dir, "status.go"), "package tui3\n\nfunc dollars(spend float64) string {\n\treturn \"$0.00\"\n}\n")
	// And the note the landing wrote about itself, which says otherwise.
	writeFile(t, filepath.Join(tree.dir, "changes", "144-status-line.md"),
		"---\nkind: changed\ntitle: the status line drops its money exception\npr: 144\ninvalidates:\n"+
			"  - \"The status line kept `$0.00` so its segments would not jump sideways. That exception is gone: `$0.00` is rendered nowhere.\"\n"+
			"---\n\nThe emptiness law now holds everywhere.\n")

	// The checker is scripted to accept the work, so that a pass here would be a
	// pass on the strength of the model rather than on the strength of the tree.
	completer := &routedCompleter{audit: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("VERIFIED — the change reads correctly"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = repo })
	node := loneTestNode(t, "take the $0.00 exception off the status line")
	node.graph.mu.Lock()
	node.spec.acceptance = "the status line no longer draws a zero"
	node.graph.mu.Unlock()

	verdict := agent.auditNode(context.Background(), node, tree,
		[]string{"status.go", "changes/144-status-line.md"}, "removed the exception", io.Discard)
	if !verdict.answered || verdict.verified {
		t.Fatalf("the check came back %s, want the finding", verdict.report())
	}
	said := strings.Join(verdict.evidence, "\n")
	if !strings.Contains(said, "`$0.00` is rendered nowhere") {
		t.Fatalf("the finding does not name the claim:\n%s", said)
	}
	if !strings.Contains(said, "status.go") {
		t.Fatalf("the finding does not say where the string still is:\n%s", said)
	}
	// AND NOBODY WAS PAID FOR IT. The search is the whole of the evidence; a
	// check that had to buy a model call to be told what a grep already knows is
	// a check charging the person for a look.
	if calls := completer.auditCalls(); calls != 0 {
		t.Fatalf("a checker was asked %d times for something the ground already answered", calls)
	}
}

// AND THE NOTE ITSELF IS NOT THE GROUND DISAGREEING WITH THE NOTE. A note saying
// `$0.00` is gone contains `$0.00` in the act of saying so; a hunt that read its
// own source back would refute every true claim anybody ever wrote.
func TestALandingNoteIsNotEvidenceAgainstItsOwnClaim(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "changes", "144-status-line.md"),
		"---\nkind: changed\ninvalidates:\n  - \"That exception is gone: `$0.00` is rendered nowhere.\"\n---\n")
	writeFile(t, filepath.Join(dir, "status.go"), "package tui3\n\nfunc dollars() string { return \"\" }\n")

	checklist := checklistFor(dir, []string{"changes/144-status-line.md", "status.go"}, "")
	if broken := checklist.broken(); len(broken) > 0 {
		t.Fatalf("the note refuted itself: %s", broken[0].line())
	}
	if len(checklist.findings) != 1 || checklist.findings[0].standing != claimHolds {
		t.Fatalf("the claim was not settled as holding: %+v", checklist.findings)
	}
}

// A CLAIM QUOTING WHAT USED TO BE TRUE IS NOT A CLAIM THAT IT IS GONE. The form
// every good invalidation is written in is two halves — what it WAS, and what it
// is NOW — and the old name is quoted in the first half. A hunt reading the whole
// sentence would find the old name beside "no longer", go looking, and refute a
// landing for telling the truth.
func TestTheHalfOfAClaimSayingWhatUsedToBeTrueIsNotHunted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "note.md"),
		"---\ninvalidates:\n  - \"The trunk was `chat-v3-task`. It no longer exists; work goes to dev.\"\n---\n")
	// The old name is all over the tree, exactly as it would really be.
	writeFile(t, filepath.Join(dir, "guide.md"), "the branch chat-v3-task was the trunk until 2026-08-31\n")

	checklist := checklistFor(dir, []string{"note.md"}, "")
	if broken := checklist.broken(); len(broken) > 0 {
		t.Fatalf("a true claim was refuted: %s", broken[0].line())
	}
	if len(checklist.open()) != 1 {
		t.Fatalf("the claim should have gone to the checker unsettled: %+v", checklist.findings)
	}
}

// AND A LITERAL INSIDE A LONGER ONE IS A DIFFERENT STRING. `$0` is not `$0.00`,
// and a landing that took `$0` out of a receipt has not been contradicted by the
// money figure two files away.
func TestAStringInsideALongerOneIsNotTheStringThatWasClaimedGone(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "note.md"),
		"---\ninvalidates:\n  - \"A money row at zero is rendered nowhere: `$0` appears in no receipt.\"\n---\n")
	writeFile(t, filepath.Join(dir, "status.go"), "package ui\n\nconst idle = \"$0.00\"\n")

	if broken := checklistFor(dir, []string{"note.md"}, "").broken(); len(broken) > 0 {
		t.Fatalf("$0.00 was read as $0: %s", broken[0].line())
	}
}

// ── THE CLAIM ABOUT WHERE WORK LANDED ───────────────────────────────────────

// A LANDING THAT SAYS IT UPDATED SOMEWHERE AND WROTE NOTHING THERE is the
// cheapest false claim there is to catch, and it is the exact shape the manual
// law gets broken in.
func TestALandingClaimingItUpdatedAPlaceItNeverWroteInFailsItsCheck(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docs", "guide", "keys.md"), "# keys\n")
	writeFile(t, filepath.Join(dir, "note.md"),
		"---\ninvalidates:\n  - \"The pages under `docs/guide` said the key was unbound; they now say what is true instead.\"\n---\n")

	broken := checklistFor(dir, []string{"note.md"}, "").broken()
	if len(broken) != 1 {
		t.Fatalf("the claim was not caught: %+v", checklistFor(dir, []string{"note.md"}, "").findings)
	}
	if !strings.Contains(broken[0].line(), "nothing under docs/guide was written") {
		t.Fatalf("the finding does not say what is missing: %s", broken[0].line())
	}
	// And a landing that did write there is not accused of anything.
	writeFile(t, filepath.Join(dir, "docs", "guide", "keys.md"), "# keys\n\nctrl+k switches conversation.\n")
	if broken := checklistFor(dir, []string{"note.md", "docs/guide/keys.md"}, "").broken(); len(broken) > 0 {
		t.Fatalf("a landing that did the work was refuted: %s", broken[0].line())
	}
}

// ── THE CLAIM NOBODY COULD SETTLE ───────────────────────────────────────────

// A CLAIM THE MACHINERY CANNOT SETTLE IS NEVER SILENTLY PASSED. It goes to the
// checker as a written question, and a check that comes back holding without
// having answered it says so on the card — which is the whole difference between
// "this was checked" and "nothing looked at this".
func TestAClaimNothingCouldSettleIsPutToTheCheckerAndThenSaidOutLoud(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "dddd2222dddd2222", 1, "stop the reply waiting on a watch")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "watch.go"), "package session\n\nfunc waits() bool { return false }\n")
	writeFile(t, filepath.Join(tree.dir, "changes", "150-watch.md"),
		"---\nkind: changed\ninvalidates:\n  - \"A reply that ended while a watch was live carried on regardless. It ends the reply now.\"\n---\n")

	completer := &routedCompleter{audit: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("VERIFIED — go test ./... passes"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = repo })
	node := loneTestNode(t, "stop the reply waiting on a watch")
	node.graph.mu.Lock()
	node.spec.acceptance = "a live watch ends the reply"
	node.graph.mu.Unlock()

	verdict := agent.auditNode(context.Background(), node, tree,
		[]string{"watch.go", "changes/150-watch.md"}, "the reply now ends", io.Discard)
	if !verdict.verified {
		t.Fatalf("the check came back %s, want it holding", verdict.report())
	}
	// THE CHECKER WAS HANDED THE CLAIM as something to settle.
	asked := completer.auditAsked()
	if len(asked) == 0 {
		t.Fatal("no checker was asked anything")
	}
	packet := messageText(asked[len(asked)-1])
	if !strings.Contains(packet, "WHAT THIS LANDING CLAIMS") {
		t.Fatalf("the checker was not given the checklist:\n%s", packet)
	}
	if !strings.Contains(packet, "It ends the reply now.") {
		t.Fatalf("the checker was not given the claim itself:\n%s", packet)
	}
	// AND THE CARD SAYS THE CLAIM WENT UNCHECKED, because a pass that quietly
	// swallowed an unexamined obligation is the failure this whole lane is for.
	if !strings.Contains(strings.Join(verdict.evidence, "\n"), "nothing checked this claim") {
		t.Fatalf("the unchecked claim was passed over:\n%s", verdict.report())
	}
}

// AND A CHECKER THAT DID ANSWER THE CLAIM IS NOT SECOND-GUESSED. The line is
// news about an unexamined obligation, and printing it over evidence that
// already speaks to the claim would be the harness talking over the checker.
func TestAClaimTheCheckerAnsweredIsNotReportedAsUnchecked(t *testing.T) {
	made := claim{text: "The refusal `8 open is as many as aforge holds` is gone.", source: "note.md", declared: true}
	held := auditVerdict{verified: true, answered: true, word: auditVerified,
		evidence: []string{"grep found no `8 open is as many as aforge holds` anywhere in the surface"}}
	if got := withOpenClaims(held, []claimFinding{{claim: made}}); len(got.evidence) != 1 {
		t.Fatalf("the checker's own answer was talked over: %v", got.evidence)
	}
	silent := auditVerdict{verified: true, answered: true, word: auditVerified,
		evidence: []string{"go test ./... passes"}}
	if got := withOpenClaims(silent, []claimFinding{{claim: made}}); len(got.evidence) != 2 {
		t.Fatalf("an unexamined obligation was passed over: %v", got.evidence)
	}
}

// A SENTENCE OF THE WORK'S OWN PROSE IS NOT NAMED AS UNCHECKED. It is hunted and
// it rides the checker's checklist like everything else, but a card that said
// "nothing checked this claim" about every line of every report is a card people
// stop reading.
func TestOnlyAWrittenObligationIsNamedAsUnchecked(t *testing.T) {
	held := auditVerdict{verified: true, answered: true, word: auditVerified, evidence: []string{"the check passes"}}
	prose := claimFinding{claim: claim{text: "I rewrote the parser and it is much tidier now.", source: "the work's own account"}}
	if got := withOpenClaims(held, []claimFinding{prose}); len(got.evidence) != 1 {
		t.Fatalf("a line of prose was reported as an unchecked obligation: %v", got.evidence)
	}
}

// ── THE DIVIDER IS A LANDING TOO ────────────────────────────────────────────

// A DIVIDING NODE'S OWN TREE IS WHAT ITS CHECK STANDS ON, PARTS INCLUDED — and
// that check happens before anything merges upward. Before this, the parent was
// handed the files ITS OWN worker wrote, so everything its parts brought home was
// laid over the ground as though it had always been there: the parts were each
// checked, correctly, and the tree they were assembled into went home unexamined.
func TestADividersCheckStandsOnTheWholeTreeItsPartsCameHomeInto(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "eeee3333eeee3333", 1, "rework the chrome")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	// The parent wrote the note. The PART wrote the file that contradicts it, and
	// merged it into this same tree.
	writeFile(t, filepath.Join(tree.dir, "changes", "126-chrome.md"),
		"---\nkind: changed\ninvalidates:\n  - \"The header drew `0 tok` beside every idle model. That is gone: `0 tok` is rendered nowhere.\"\n---\n")
	writeFile(t, filepath.Join(tree.dir, "header.go"), "package tui3\n\nconst idle = \"0 tok\"\n")

	parent := loneTestNode(t, "rework the chrome")
	parent.graph.mu.Lock()
	parent.spec.acceptance = "the chrome is reworked"
	part := &TaskNode{graph: parent.graph, id: 2, parent: parent.id, state: TaskDone,
		spec: taskSpec{title: "the header"}, changed: []string{"header.go"}}
	parent.graph.nodes[part.id] = part
	parent.graph.order = append(parent.graph.order, part.id)
	parent.graph.mu.Unlock()

	// The part's file is part of what this landing carries, and it is named as the
	// part's rather than as the parent's own.
	files := landingFilesFor(parent, []string{"changes/126-chrome.md"})
	if !containsPath(files.all(), "header.go") {
		t.Fatalf("the part's work is not in what the check stands on: %+v", files)
	}
	if !files.divided() || !containsPath(files.parts, "header.go") {
		t.Fatalf("the part's work was filed as the parent's own: %+v", files)
	}

	completer := &routedCompleter{audit: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("VERIFIED — the assembly reads correctly"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = repo })
	verdict := agent.auditNode(context.Background(), parent, tree,
		[]string{"changes/126-chrome.md"}, "the parts are in", io.Discard)
	if !verdict.answered || verdict.verified {
		t.Fatalf("the divider's tree came back %s, want the finding", verdict.report())
	}
	if said := strings.Join(verdict.evidence, "\n"); !strings.Contains(said, "header.go") {
		t.Fatalf("the finding does not reach into the part's work:\n%s", said)
	}
}

// AND A PART THAT DID NOT COME HOME IS NOT COUNTED. Its branch was kept and never
// merged, so its files are not in the parent's tree, and a check standing on them
// would be standing on work that is not there.
func TestAPartThatNeverLandedIsNotCountedInTheDividersCheck(t *testing.T) {
	parent := loneTestNode(t, "rework the chrome")
	parent.graph.mu.Lock()
	for id, state := range map[uint64]TaskState{2: TaskDone, 3: TaskFailed, 4: TaskUnverified} {
		part := &TaskNode{graph: parent.graph, id: id, parent: parent.id, state: state,
			changed: []string{"part-" + string(rune('0'+id)) + ".go"}}
		parent.graph.nodes[id] = part
		parent.graph.order = append(parent.graph.order, id)
	}
	parent.graph.mu.Unlock()

	files := landingFilesFor(parent, []string{"own.go"})
	if len(files.parts) != 1 || files.parts[0] != "part-2.go" {
		t.Fatalf("work that never merged was counted: %+v", files.parts)
	}
}

// AND THE CHECKER IS TOLD WHICH FILES ARE THE PARTS'. "It wrote" would be a claim
// about this node that is not true, and a checker that believed it would judge
// one worker for five workers' output.
func TestThePacketSaysWhichFilesThePartsWrote(t *testing.T) {
	node := loneTestNode(t, "rework the chrome")
	question := auditQuestion(node, taskTree{}, auditGround{dir: "/restore", restored: true}, auditDoor{},
		landingFiles{own: []string{"assembly.go"}, parts: []string{"header.go", "rail.go"}}, "it is assembled", nil)
	if !strings.Contains(question, "Files it wrote: assembly.go") {
		t.Fatalf("the node's own files are not named:\n%s", question)
	}
	if !strings.Contains(question, "And the parts it handed out wrote, into the same tree: header.go, rail.go") {
		t.Fatalf("the parts' files are not named as the parts':\n%s", question)
	}
}

// ── READING A LANDING NOTE ──────────────────────────────────────────────────

// The three spellings a writer really uses. A reader that only took one of them
// is a reader that silently declares half a landing has no claims at all.
func TestALandingNoteIsReadInEverySpellingAWriterUses(t *testing.T) {
	note := "---\nkind: changed\ntitle: something moved\npr: 12\ninvalidates:\n" +
		"  - \"A quoted clause ending in a full stop.\"\n" +
		"  - A bare clause with no quotes at all.\n" +
		"  - >-\n    A folded clause that runs\n    over two lines.\n" +
		"---\n\nThe body, which is not a claim.\n"
	got := declaredInvalidations(note)
	want := []string{
		"A quoted clause ending in a full stop.",
		"A bare clause with no quotes at all.",
		"A folded clause that runs over two lines.",
	}
	if len(got) != len(want) {
		t.Fatalf("read %d clauses, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("clause %d is %q, want %q", i, got[i], want[i])
		}
	}
	// A document with no frontmatter declares nothing, and says so quietly.
	if clauses := declaredInvalidations("# just a page\n\ninvalidates: nothing\n"); len(clauses) != 0 {
		t.Fatalf("a plain page was read as a landing note: %q", clauses)
	}
	// And an empty list is an honest answer, not a parse failure.
	if clauses := declaredInvalidations("---\nkind: internal\ninvalidates: []\n---\n"); len(clauses) != 0 {
		t.Fatalf("an empty list produced %q", clauses)
	}
}

// EVERY SHAPE THIS BUILD SETTLES IS A REGISTERED HUNTER, never a switch inside
// the check. A shape of claim is a thing somebody adds — "the mail went out",
// "the table has forty rows" — and the registry is where they add it.
func TestEveryClaimHunterIsRegisteredWithAName(t *testing.T) {
	if len(claimHunters) == 0 {
		t.Fatal("nothing hunts a claim")
	}
	seen := map[string]bool{}
	for _, hunter := range claimHunters {
		if strings.TrimSpace(hunter.name) == "" || hunter.hunt == nil {
			t.Fatalf("a hunter is registered without a name or a hunt: %+v", hunter)
		}
		if seen[hunter.name] {
			t.Fatalf("two hunters answer to %q", hunter.name)
		}
		seen[hunter.name] = true
	}
}

// AND NOTHING A PERSON READS OFF A CLAIM CARRIES THE MACHINERY'S VOCABULARY.
func TestAClaimFindingIsWrittenInPlainWords(t *testing.T) {
	finding := claimFinding{
		claim:    claim{text: "`$0.00` is rendered nowhere.", source: "changes/144.md"},
		standing: claimBroken,
		evidence: "$0.00 is still in status.go:4",
	}
	line := finding.line()
	for _, banned := range []string{"auditor", "verdict", "refuted", "unverified"} {
		if strings.Contains(strings.ToLower(line), banned) {
			t.Fatalf("the finding says %q to a person: %s", banned, line)
		}
	}
	if !strings.HasPrefix(line, "it says \"") {
		t.Fatalf("the finding does not lead with the claim: %s", line)
	}
}
