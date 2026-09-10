package session

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The checker must see both the statement and the work it describes. Its
// answer, rather than the harness's interpretation of a keyword, decides
// whether that statement contradicts the work.
func TestDeclaredClaimsReachTheCheckerWithTheirSource(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "cccc1111cccc1111", 1, "update the status line")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tree.dir, "status.go"), "package ui\nconst idle = \"$0.00\"\n")
	clause := "The status line's `$0.00` exception was removed."
	writeFile(t, filepath.Join(tree.dir, "note.md"), "---\ninvalidates:\n  - "+clause+"\n---\n")
	report := "The status line is ready for review."
	completer := &routedCompleter{audit: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			packet := strings.Join(messageTexts(messages), "\n")
			for _, want := range []string{clause, "(note.md)", "status.go", report} {
				if !strings.Contains(packet, want) {
					t.Errorf("checker is missing %q", want)
				}
			}
			if strings.Count(packet, report) != 1 {
				t.Error("the report was duplicated as a claim checklist")
			}
			return textResponse("REFUTED — status.go still renders $0.00, contrary to note.md"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.Workspace = repo })
	node := loneTestNode(t, "update the status line")
	verdict := agent.auditNode(context.Background(), node, tree, []string{"status.go", "note.md"}, report, io.Discard)
	if !verdict.answered || verdict.verified || completer.auditCalls() != 1 {
		t.Fatalf("the checker did not decide the contradiction: %s; calls=%d", verdict.report(), completer.auditCalls())
	}
}

// The same source words can describe a retained reference or a changed
// surface. A statement in a note is still prose, even when explicitly declared.
func TestDeclaredProseIsNotAutomaticallyAcceptedOrRefuted(t *testing.T) {
	for _, response := range []string{
		"VERIFIED — the reference remains and the requested display changed",
		"REFUTED — the requested display still contains the reference",
	} {
		t.Run(strings.Fields(response)[0], func(t *testing.T) {
			repo := newTestRepo(t)
			tree, err := prepareTaskTree(Place{}, repo, "cccc2222cccc2222", 1, "update the display")
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(tree.dir, "reference.txt"), "reference label\n")
			writeFile(t, filepath.Join(tree.dir, "note.md"), "---\ninvalidates:\n  - The `reference label` is removed from the display.\n---\n")
			completer := &routedCompleter{audit: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(response), nil },
			}}
			agent, _ := newTestAgent(t, completer, func(c *Config) { c.Workspace = repo })
			verdict := agent.auditNode(context.Background(), loneTestNode(t, "update the display"), tree,
				[]string{"reference.txt", "note.md"}, "The requested display was updated.", io.Discard)
			if !verdict.answered || verdict.verified != strings.HasPrefix(response, "VERIFIED") || completer.auditCalls() != 1 {
				t.Fatalf("prose preempted the checker: %s", verdict.report())
			}
			if strings.Contains(strings.Join(verdict.evidence, "\n"), "nothing checked this claim") {
				t.Fatal("literal overlap invented a second account of what the checker examined")
			}
		})
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
			return textResponse("REFUTED — header.go still renders the zero-token label"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = repo })
	verdict := agent.auditNode(context.Background(), parent, tree,
		[]string{"changes/126-chrome.md"}, "the parts are in", io.Discard)
	if !verdict.answered || verdict.verified {
		t.Fatalf("the divider's tree came back %s, want the finding", verdict.report())
	}
	asked := completer.auditAsked()
	if len(asked) == 0 || !strings.Contains(messageText(asked[len(asked)-1]),
		"And the parts it handed out wrote, into the same tree: header.go") {
		t.Fatal("the checker did not receive the assembled part's file")
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
	question := auditQuestion(node, taskTree{}, auditGround{dir: "/restore", restored: true}, auditDoor{}, checkGround{},
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
