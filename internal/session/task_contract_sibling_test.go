package session

// A SECOND TASK IN ONE CONVERSATION IS NOT POINTED AT THE FIRST TASK'S COPY.
//
// This is issue #839, and it is #566 met from the third side. The conversation
// itself learns the address of a task's private copy — the first worker's own
// report names the directory it stood in, and the landing note names it again —
// so the model that proposes the NEXT piece of work writes that address into the
// contract instead of the person's checkout. The path is a real directory under
// this conversation's own trees/, it is simply somebody else's, and it is either
// gone by then or belongs to a copy this worker may not touch.
//
// Before the fix the binding left it exactly as written: it is not under the
// node's ground and not under the conversation's work/ folder, so neither rule
// reached it. The worker followed the address it was given and [taskGroundGuard]
// answered "is outside your copy" about the one directory the harness itself had
// invented. Under the full tagged package that shape fired once in five runs of
// TestAWorktreeTaskFollowsItsContractInsideItsOwnCopy, where the first attempt
// came home kept and the conversation asked again.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TestASecondTaskIsNotPointedAtTheFirstTasksCopy cuts two trees for one
// conversation in the order the package's own runs cut them — the first task
// settles, and only then does the conversation propose the second — and asks
// the one question the defect is about: does the document the second worker
// opens on name the tree it is actually standing in.
func TestASecondTaskIsNotPointedAtTheFirstTasksCopy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	source := newTestRepo(t)
	sourceWidget := filepath.Join(source, filepath.FromSlash(theWidget))
	writeFile(t, sourceWidget, "old\n")
	mustGit(t, source, "add", "-A")
	mustGit(t, source, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "the widget")

	place := Place{Dir: t.TempDir(), Workspace: source, Owned: true}
	// The two copies, named the way the session names them, worked out up front
	// for [TestAWorktreeTaskBindsContractPathsToItsPrivateCopy]'s reason: a
	// settled task's worktree is removed, so the first one is not there to read
	// by the time the second worker is asked anything.
	firstCopy := filepath.Join(place.Trees(), "1")
	secondCopy := filepath.Join(place.Trees(), "2")
	firstWidget := filepath.Join(firstCopy, filepath.FromSlash(theWidget))
	secondWidget := filepath.Join(secondCopy, filepath.FromSlash(theWidget))

	// The first proposal is the ordinary one: a parent standing in the person's
	// checkout writing that checkout's absolute path.
	first, err := json.Marshal(taskArguments{
		Title:       "Say new",
		Summary:     "two lines the person reads",
		Brief:       "change " + sourceWidget + " so it says new\n" + taskBriefMark,
		Deliverable: sourceWidget,
		Acceptance:  sourceWidget + " says new",
		Ground:      source,
		MaxSteps:    20,
	})
	if err != nil {
		t.Fatal(err)
	}
	// AND THE SECOND NAMES THE FIRST TASK'S COPY, which is the whole defect. It
	// is not a spelling a test invented: it is the address the first worker
	// reported working at, read back out of the conversation by the model that
	// writes the next contract.
	second, err := json.Marshal(taskArguments{
		Title:       "Finish saying new",
		Summary:     "two lines the person reads",
		Brief:       "the last attempt left " + firstWidget + " unfinished; finish it\n" + taskBriefMark,
		Deliverable: firstWidget,
		Acceptance:  firstWidget + " says new",
		Ground:      source,
		MaxSteps:    20,
	})
	if err != nil {
		t.Fatal(err)
	}

	var (
		handedMu  sync.Mutex
		handed    string
		inTheCopy string
	)
	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-task-1", "propose_task", string(first)), nil
			},
			finalText("handed off"),
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-task-2", "propose_task", string(second)), nil
			},
			finalText("handed off again"),
		},
		child: []step{
			// The first worker does nothing at all. What matters about it is
			// that its tree was cut and its id was spent.
			finalText("the first attempt got nowhere"),
			// The second worker is the one under test: it takes the address out
			// of its own contract, records it, and writes there.
			func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
				handedMu.Lock()
				handed = contractTarget(messages, theWidget)
				handedMu.Unlock()
				return followContract("call-edit", "edit", func(address string) any {
					return map[string]any{
						"path":  address,
						"edits": []map[string]string{{"oldText": "old", "newText": "new"}},
					}
				})(ctx, messages)
			},
			// The worker's last word, and the moment the copy is read: the
			// worktree is still on disk here and gone once the node lands.
			func(context.Context, []ai.Message) (*ai.Response, error) {
				data, err := os.ReadFile(secondWidget)
				handedMu.Lock()
				inTheCopy = string(data)
				handedMu.Unlock()
				if err != nil {
					t.Errorf("the second copy's %s could not be read while the worker stood in it: %v", theWidget, err)
				}
				return textResponse("the widget says new"), nil
			},
		},
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = source
		config.Place = place
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})

	collect(t, mustSubmit(t, agent, "make the widget say new"))
	firstNode := agent.graph().node(1)
	if firstNode == nil {
		t.Fatal("no first node was admitted")
	}
	waitDoneNode(t, firstNode)

	collect(t, mustSubmit(t, agent, "that did not work, try again"))
	secondNode := agent.graph().node(2)
	if secondNode == nil {
		t.Fatal("no second node was admitted")
	}
	waitDoneNode(t, secondNode)

	if where := secondNode.notice().Where; where != secondCopy {
		t.Fatalf("the second task worked at %q, want its own copy %q — the fixture did not cut two trees for one conversation", where, secondCopy)
	}

	// (1) THE ADDRESS THE SECOND WORKER WAS HANDED IS ITS OWN COPY. An earlier
	// tree of the same conversation is a copy of the same folder, so the
	// equivalent address inside this copy is the same path under it.
	handedMu.Lock()
	got, wrote := handed, inTheCopy
	handedMu.Unlock()
	if got != secondWidget {
		t.Fatalf("the second worker's WHAT TO PRODUCE names %q, want %q — the contract was bound to an earlier tree of this conversation rather than to the copy this task was given (#839)",
			got, secondWidget)
	}

	// (2) AND FOLLOWING IT WAS NOT REFUSED. This is the sentence #566 is
	// measured by, and it is the one the full tagged package saw.
	if refusal := childToolResultContaining(completer, outsideYourCopy); refusal != "" {
		t.Fatalf("the second worker following its own contract was refused: %q", firstLines(refusal, 2))
	}
	if !strings.Contains(wrote, "new") {
		t.Fatalf("the second copy's %s held %q while the worker stood in it, want it to say new — the edit did not land in the task's own directory", theWidget, wrote)
	}
}
