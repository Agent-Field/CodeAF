package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the harness ─────────────────────────────────────────────────────────────

// writeCall is one call to the belt's write hand, as the model would send it.
func revertWriteCall(id, path string) ai.ToolCall {
	return ai.ToolCall{ID: id, Type: "function", Function: ai.ToolCallFunction{
		Name: "write", Arguments: `{"path":"` + path + `","content":"new\n"}`}}
}

// touchThrough runs one call through the two hooks that straddle its execution,
// with the write itself in between — which is exactly what a turn does, and the
// only order in which the ledger can tell a created file from a modified one.
func touchThrough(t *testing.T, ep *episode, workspace string, call ai.ToolCall, content string) {
	t.Helper()
	ctx := context.Background()
	ep.preAction(ctx, nil, call)

	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		t.Fatalf("call arguments: %v", err)
	}
	path := filepath.Join(workspace, filepath.FromSlash(args.Path))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ep.postFeedback(ctx, nil, []ai.ToolCall{call}, []toolResult{{text: "wrote it"}}, false)
}

// gitRepo makes a workspace that is a repository, with one committed file.
func revertRepo(t *testing.T, workspace string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		if out, err := git(workspace, args...); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
}

func revertCommit(t *testing.T, workspace, message string) {
	t.Helper()
	if out, err := git(workspace, "add", "-A"); err != nil {
		t.Fatalf("git add: %v (%s)", err, out)
	}
	if out, err := git(workspace, "commit", "-q", "--no-verify", "-m", message); err != nil {
		t.Fatalf("git commit: %v (%s)", err, out)
	}
}

func revertRead(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(text)
}

// ── the ledger ──────────────────────────────────────────────────────────────

// The ledger's whole job is the distinction nothing else can make afterwards:
// this file was here before the turn, that one was not.
func TestTheLedgerTellsCreatedFromModified(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	old := filepath.Join(workspace, "old.md")
	if err := os.WriteFile(old, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// What the file held BEFORE the turn, digested here while it is still there
	// to digest — which is the whole reason the ledger takes its own copy at
	// pre-action ([changeLedger.PreAction]).
	original := fileDigest(old)
	episode := agent.newEpisode()

	touchThrough(t, episode, workspace, revertWriteCall("c1", "old.md"), "edited\n")
	touchThrough(t, episode, workspace, revertWriteCall("c2", "new.md"), "fresh\n")

	changes := episode.changes.list()
	if len(changes) != 2 {
		t.Fatalf("the ledger holds %d changes, want 2 (%+v)", len(changes), changes)
	}
	if changes[0].shown != "old.md" || changes[0].created {
		t.Errorf("old.md recorded as %+v, want a modification", changes[0])
	}
	if changes[1].shown != "new.md" || !changes[1].created {
		t.Errorf("new.md recorded as %+v, want a creation", changes[1])
	}
	// AND THE DIGEST IS THE ONE FROM BEFORE THE WRITE. Taken a moment later it
	// would be a digest of the session's own work, and every file the session
	// touched would read as unchanged forever after.
	if changes[0].before != original {
		t.Errorf("old.md's before-digest is %q, want the content from before the write (%q)", changes[0].before, original)
	}
	if changes[0].before == fileDigest(old) {
		t.Errorf("old.md's before-digest is the content the write left behind: %q", changes[0].before)
	}
	// A FILE THAT WAS NOT THERE DIGESTS AS NOTHING, which is what makes a created
	// file differ from whatever it holds now.
	if changes[1].before != "" {
		t.Errorf("new.md was digested before it existed: %q", changes[1].before)
	}
}

// A mutation that FAILED changed nothing, so the ledger holds nothing — a
// revert that restored a file no call ever wrote would be the recovery move
// causing the damage.
func TestTheLedgerIgnoresFailedAndReadOnlyCalls(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	episode := agent.newEpisode()
	ctx := context.Background()

	failed := revertWriteCall("c1", "never.md")
	episode.preAction(ctx, nil, failed)
	episode.postFeedback(ctx, nil, []ai.ToolCall{failed}, []toolResult{{text: "permission denied", isError: true}}, false)

	read := ai.ToolCall{ID: "c2", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"old.md"}`}}
	episode.preAction(ctx, nil, read)
	episode.postFeedback(ctx, nil, []ai.ToolCall{read}, []toolResult{{text: "the file"}}, false)

	if changes := episode.changes.list(); len(changes) != 0 {
		t.Fatalf("the ledger recorded %+v, want nothing", changes)
	}
	_ = workspace
}

// ── the revert ──────────────────────────────────────────────────────────────

// In a repository: a modified tracked file comes back, a created file goes
// away.
func TestRevertRestoresATrackedFileAndRemovesACreatedOne(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	revertRepo(t, workspace)
	if err := os.WriteFile(filepath.Join(workspace, "old.md"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	revertCommit(t, workspace, "base")

	episode := agent.newEpisode()
	touchThrough(t, episode, workspace, revertWriteCall("c1", "old.md"), "the model's edit\n")
	touchThrough(t, episode, workspace, revertWriteCall("c2", "sub/new.md"), "the model's file\n")

	outcome := agent.revert(episode.offerFor())

	if got := revertRead(t, filepath.Join(workspace, "old.md")); got != "original\n" {
		t.Fatalf("old.md = %q, want the committed content back", got)
	}
	if _, err := os.Stat(filepath.Join(workspace, "sub", "new.md")); !os.IsNotExist(err) {
		t.Fatalf("the created file survived the revert (%v)", err)
	}
	if len(outcome.restored) != 1 || outcome.restored[0] != "old.md" {
		t.Errorf("restored = %v, want [old.md]", outcome.restored)
	}
	if len(outcome.removed) != 1 || outcome.removed[0] != "sub/new.md" {
		t.Errorf("removed = %v, want [sub/new.md]", outcome.removed)
	}
	if len(outcome.manual) != 0 || len(outcome.failed) != 0 {
		t.Errorf("manual = %v, failed = %v, want neither", outcome.manual, outcome.failed)
	}
}

// Without a repository the honest answer is half a revert and a sentence about
// the other half: the created file goes, the modified one is NAMED and left
// exactly as it is.
func TestRevertWithoutGitRemovesCreatedFilesAndNamesTheRest(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	if err := os.WriteFile(filepath.Join(workspace, "old.md"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	episode := agent.newEpisode()
	touchThrough(t, episode, workspace, revertWriteCall("c1", "old.md"), "the model's edit\n")
	touchThrough(t, episode, workspace, revertWriteCall("c2", "new.md"), "the model's file\n")

	outcome := agent.revert(episode.offerFor())

	if got := revertRead(t, filepath.Join(workspace, "old.md")); got != "the model's edit\n" {
		t.Fatalf("old.md = %q, want it left exactly as the turn left it", got)
	}
	if _, err := os.Stat(filepath.Join(workspace, "new.md")); !os.IsNotExist(err) {
		t.Fatalf("the created file survived the revert (%v)", err)
	}
	if len(outcome.manual) != 1 || outcome.manual[0] != "old.md" {
		t.Fatalf("manual = %v, want [old.md]", outcome.manual)
	}
	if len(outcome.restored) != 0 {
		t.Fatalf("restored = %v with no repository to restore from", outcome.restored)
	}

	// And the model is told, in the words the person read.
	note := agent.revertNote(episode.offerFor())
	if !strings.Contains(note, "not under git, restore by hand: old.md") {
		t.Fatalf("the note hides what was not reverted: %q", note)
	}
}

// An untracked file that existed before the turn has no version to be restored
// TO, so it is named rather than deleted: it may be the person's own work.
func TestRevertLeavesAnUntrackedModifiedFileAlone(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	revertRepo(t, workspace)
	if err := os.WriteFile(filepath.Join(workspace, "kept.md"), []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	revertCommit(t, workspace, "base")
	if err := os.WriteFile(filepath.Join(workspace, "theirs.md"), []byte("the person's draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	episode := agent.newEpisode()
	touchThrough(t, episode, workspace, revertWriteCall("c1", "theirs.md"), "the model's edit\n")

	outcome := agent.revert(episode.offerFor())
	if _, err := os.Stat(filepath.Join(workspace, "theirs.md")); err != nil {
		t.Fatalf("an untracked file was deleted by a revert: %v", err)
	}
	if len(outcome.manual) != 1 || outcome.manual[0] != "theirs.md" {
		t.Fatalf("manual = %v, want [theirs.md]", outcome.manual)
	}
}

// THE WORKSPACE IS THE BOUNDARY. A file outside it is never deleted, whatever
// the ledger says about who created it.
func TestRevertNeverTouchesAnythingOutsideTheWorkspace(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(outside, []byte("not ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	episode := agent.newEpisode()
	episode.changes.note(outside, false, "") // as if this turn created it
	episode.changes.touched(outside, outside)

	outcome := agent.revert(episode.offerFor())
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("a file outside the workspace was removed: %v", err)
	}
	if len(outcome.manual) != 1 || len(outcome.removed) != 0 {
		t.Fatalf("outcome = %+v, want the file named and left alone", outcome)
	}
	_ = workspace
}

// ── the offer and the three answers ─────────────────────────────────────────

// With nothing changed there is no move to offer, and the question is the one
// it always was.
func TestTheOfferIsSilentWhenTheTurnChangedNothing(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	episode := agent.newEpisode()
	looping := nudge{tool: "grep", count: 3}

	offer := episode.offerFor()
	if offer.available() {
		t.Fatal("a turn that changed nothing offered a revert")
	}
	if offer.rule(looping) != loopRule(looping) {
		t.Fatalf("rule = %q, want the plain stuck wording", offer.rule(looping))
	}
}

// With files touched, the question says exactly what would be undone.
func TestTheOfferNamesHowManyFilesWouldBeReverted(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	episode := agent.newEpisode()
	touchThrough(t, episode, workspace, revertWriteCall("c1", "a.md"), "one\n")
	touchThrough(t, episode, workspace, revertWriteCall("c2", "b.md"), "two\n")

	rule := episode.offerFor().rule(nudge{tool: "write", count: 3})
	if !strings.HasPrefix(rule, "stuck: the same write call 3 times") {
		t.Fatalf("the offer lost the stuck wording: %q", rule)
	}
	if !strings.Contains(rule, "revert the 2 files this turn touched and retry from clean?") {
		t.Fatalf("the offer does not say what it would do: %q", rule)
	}
}

// The old third-rung recovery question must not survive the checkpoint hand-off
// law. With no consent surface the turn ends honestly and, most importantly,
// does not spend the dormant revert offer against work on disk.
func TestTheThirdRungEndsWithoutRevertingTheTurn(t *testing.T) {
	completer := &scriptedCompleter{steps: repeatedCalls("write", `{"path":"a.md","content":"loop\n"}`, 7)}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
	})

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if fired := nudgeEvents(collected); len(fired) != 3 {
		t.Fatalf("nudges: got %d, want 3 — at the third, fifth and seventh call", len(fired))
	}
	if _, err := os.Stat(filepath.Join(workspace, "a.md")); err != nil {
		t.Fatalf("the loop ceiling reverted the turn without consent: %v", err)
	}
	if notice, ok := firstOfKind(collected, EventNotice); !ok || notice.Text != loopLeftUndoneNote {
		t.Fatalf("left-undone notice = %q, present=%v", notice.Text, ok)
	}
	if notes := transcriptNotes(agent); len(notes) != loopNudgeCeiling {
		t.Fatalf("notes: got %d, want %d before the ceiling", len(notes), loopNudgeCeiling)
	}
}
