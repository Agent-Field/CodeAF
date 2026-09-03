package session

// A WORKTREE TASK HAS ONE WORKER-FACING PATH NAMESPACE: ITS PRIVATE COPY.
//
// This is issue #566, reproduced with nothing scripted but the model. The task
// engine can hold two contradictory truths at once: the worker is stood up in
// `<session>/trees/<id>` and handed a contract whose WHAT TO PRODUCE names the
// person's live checkout. A worker that follows the address it was given reads
// the directory that is still moving under it, and its first write at that same
// address is refused by [taskGroundGuard] as "is outside your copy".
//
// The isolation is right and the guard is right. What is wrong is the handoff:
// a contract path at or below the node's ground must be bound to the equivalent
// path inside the copy that was actually cut, BEFORE the worker is asked
// anything.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// theWidget is the one file the whole fixture is about, spelled once so that
// the source path, the copy's path and the address the worker is handed are all
// derived from the same name rather than typed three times.
const theWidget = "internal/widget.go"

// contractTarget answers the ONE address a worker was actually handed for the
// file it must produce: the path under WHAT TO PRODUCE in its opening message.
//
// It reads that section and nothing else on purpose. The person's own words are
// carried verbatim and the brief restates them, so a search over the whole
// message would find whichever spelling came first rather than the one the
// worker is contractually pointed at.
func contractTarget(messages []ai.Message, name string) string {
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		text := messageText(message)
		start := strings.Index(text, briefMakeHeading)
		if start < 0 {
			continue
		}
		body := text[start+len(briefMakeHeading):]
		if end := strings.Index(body, briefDoneHeading); end >= 0 {
			body = body[:end]
		}
		found := regexp.MustCompile(`[^\s"'` + "`" + `]*` + regexp.QuoteMeta(name)).FindString(body)
		if found != "" {
			return found
		}
	}
	return ""
}

// followContract is a worker doing exactly what a worker does: it takes the
// address out of its own contract and calls the named tool on it. Nothing here
// knows about the source checkout or about the copy — which is the point, since
// a worker cannot know either.
func followContract(id, tool string, arguments func(address string) any) step {
	return func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		address := contractTarget(messages, theWidget)
		if address == "" {
			// A contract that names no target at all is a different bug, and the
			// assertions below say so far more clearly than a scripted call on an
			// empty path would.
			return textResponse("the contract named no file to produce"), nil
		}
		wire, err := json.Marshal(arguments(address))
		if err != nil {
			return nil, err
		}
		return toolResponse(id, tool, string(wire)), nil
	}
}

// THE REPLICATION FOR #566. The parent proposes work on the source checkout and
// writes the absolute source path into brief, deliverable and acceptance — the
// shape prompts/system.md asks for. The node is given a private worktree. The
// source file then changes, so the two directories are observably different, and
// the worker follows its contract with a read and an edit.
func TestAWorktreeTaskBindsContractPathsToItsPrivateCopy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	source := newTestRepo(t)
	sourceWidget := filepath.Join(source, filepath.FromSlash(theWidget))
	writeFile(t, sourceWidget, "old\n")
	mustGit(t, source, "add", "-A")
	mustGit(t, source, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "the widget")

	place := Place{Dir: t.TempDir(), Workspace: source}
	// The copy the engine will cut, named the way the session names it. It is
	// worked out up front because A LANDED TASK'S WORKTREE IS REMOVED: by the
	// time the run is over there is nothing at this path to read, so what the
	// copy held has to be taken while the worker is still standing in it.
	copyDir := filepath.Join(place.Trees(), "1")
	copyWidget := filepath.Join(copyDir, filepath.FromSlash(theWidget))

	// The proposal names the SOURCE path three times, absolutely, because that
	// is what a parent standing in the source checkout writes today.
	proposal, err := json.Marshal(taskArguments{
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

	// THE SOURCE MOVES ONCE THE COPY HAS BEEN CUT, at the first moment the
	// worker exists — which is after [prepareTaskTreeOn] has carved the
	// worktree and before the worker has read anything. From here on the two
	// directories disagree, and which one the worker read is legible in what it
	// got back.
	var (
		once      sync.Once
		copyMu    sync.Mutex
		copyBytes string
		copyErr   error
	)
	divergeThenRead := func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		once.Do(func() { writeFile(t, sourceWidget, "new-in-source\n") })
		return followContract("call-read", "read", func(address string) any {
			return map[string]string{"path": address}
		})(ctx, messages)
	}

	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-task", "propose_task", string(proposal)), nil
			},
			finalText("handed off"),
		},
		child: []step{
			divergeThenRead,
			followContract("call-edit", "edit", func(address string) any {
				return map[string]any{
					"path":  address,
					"edits": []map[string]string{{"oldText": "old", "newText": "new"}},
				}
			}),
			// The worker's last word, and the moment the copy is read: the
			// worktree is still on disk here and gone once the node lands.
			func(context.Context, []ai.Message) (*ai.Response, error) {
				data, err := os.ReadFile(copyWidget)
				copyMu.Lock()
				copyBytes, copyErr = string(data), err
				copyMu.Unlock()
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

	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	notice := node.notice()

	opening := completer.childAsked()
	if opening == nil {
		t.Fatal("the worker was never asked anything, so no contract was handed over")
	}

	// (1) THE CONTRACT IS BOUND TO THE COPY. This is the bug: the worker is
	// pointed at the person's checkout while it stands in trees/1.
	handed := contractTarget(opening, theWidget)
	if handed != copyWidget {
		t.Fatalf("the worker's WHAT TO PRODUCE names %q, want %q — the contract was not bound to the copy the task was given (its working directory is %q)",
			handed, copyWidget, notice.Where)
	}

	// (2) THE ADDRESS IT WAS GIVEN HOLDS THE FROZEN BYTES. A worker sent to the
	// source reads whatever the person's checkout says at that instant.
	read := childToolResult(completer, "call-read")
	if !strings.Contains(read, "old") || strings.Contains(read, "new-in-source") {
		t.Fatalf("the worker's read at the address it was given returned %q, want the copy's frozen \"old\"", firstLines(read, 3))
	}

	// (3) AND ITS WRITE AT THAT SAME ADDRESS IS ALLOWED. The guard is right to
	// refuse a write into the person's checkout; a contract that provokes the
	// refusal is what is wrong.
	if refusal := childToolResultContaining(completer, outsideYourCopy); refusal != "" {
		t.Fatalf("the worker following its own contract was refused: %q", firstLines(refusal, 2))
	}
	copyMu.Lock()
	inTheCopy, copyReadErr := copyBytes, copyErr
	copyMu.Unlock()
	if copyReadErr != nil {
		t.Fatalf("the copy's %s could not be read while the worker stood in it: %v", theWidget, copyReadErr)
	}
	if inTheCopy != "new\n" {
		t.Fatalf("the copy's widget is %q, want %q — the edit did not land in the task's own directory", inTheCopy, "new\n")
	}

	// (4) NOTHING WROTE INTO THE PERSON'S CHECKOUT DURING THE RUN.
	if got := readFile(t, sourceWidget); got != "new-in-source\n" {
		t.Fatalf("the source checkout's widget is %q, want %q — the run reached into the person's own folder", got, "new-in-source\n")
	}

	// (5) AND THE RECORD STILL SAYS WHAT THE COPY IS OF. Binding the contract
	// moves the worker's addresses, never the node's provenance or its landing
	// target.
	if notice.Ground != canonicalPath(source) {
		t.Fatalf("Ground = %q, want the source checkout %q", notice.Ground, canonicalPath(source))
	}
	if notice.Where != copyDir {
		t.Fatalf("Where = %q, want the private copy %q", notice.Where, copyDir)
	}
	if notice.Mode != TaskModeWorktree {
		t.Fatalf("Mode = %q, want %q", notice.Mode, TaskModeWorktree)
	}
}

// childToolResult is what one scripted call actually got back, read out of the
// NEXT request the worker was sent — which is the only place a tool result is
// visible from outside the node.
func childToolResult(completer *routedCompleter, id string) string {
	completer.mu.Lock()
	defer completer.mu.Unlock()
	for _, request := range completer.childRequests {
		for _, message := range request {
			if message.Role == "tool" && message.ToolCallID == id {
				return messageText(message)
			}
		}
	}
	return ""
}

// childToolResultContaining is the same reading, asked the other way round: did
// any answer the worker got carry this sentence.
func childToolResultContaining(completer *routedCompleter, want string) string {
	completer.mu.Lock()
	defer completer.mu.Unlock()
	for _, request := range completer.childRequests {
		for _, message := range request {
			if message.Role != "tool" {
				continue
			}
			if text := messageText(message); strings.Contains(text, want) {
				return text
			}
		}
	}
	return ""
}
