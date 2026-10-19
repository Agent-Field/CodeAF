package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/aymanbagabas/go-udiff"
)

// QuickTaskFileLimit is enforced before the next distinct file can be changed.
const QuickTaskFileLimit = 30

// QuickFileCapture stays in the private task checkpoint. Bytes never cross the app wire.
// Existence is separate from a digest because an empty file is still a file.
type QuickFileCapture struct {
	Path         string      `json:"path"`
	Before       []byte      `json:"before,omitempty"`
	After        []byte      `json:"after,omitempty"`
	BeforeExists bool        `json:"before_exists"`
	AfterExists  bool        `json:"after_exists"`
	Mode         os.FileMode `json:"mode"`
}

func cloneQuickCaptures(in []QuickFileCapture) []QuickFileCapture {
	out := append([]QuickFileCapture(nil), in...)
	for i := range out {
		out[i].Before = append([]byte(nil), in[i].Before...)
		out[i].After = append([]byte(nil), in[i].After...)
	}
	return out
}

func quickHash(bytes []byte, exists bool) string {
	if !exists {
		return ""
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:])
}

func quickRead(path string) ([]byte, bool, os.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, 0600, nil
	}
	if err != nil {
		return nil, false, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, 0, errors.New("quick tasks only capture regular files")
	}
	raw, err := os.ReadFile(path)
	return raw, true, info.Mode().Perm(), err
}

// executeTaskTool surrounds the actual execution, so a batch cannot overwrite a
// first capture or mistake a later external edit for its own after bytes.
func (a *Agent) executeTaskTool(ctx context.Context, tool bare.Tool, call ai.ToolCall, args json.RawMessage) (string, bool, error) {
	if refusal := a.readingToolRefusal(tool.Name); refusal != "" {
		return refusal, true, nil
	}
	node := a.config.taskControl
	if node == nil {
		return tool.Execute(ctx, args)
	}
	if err := node.appBoundary(ctx); err != nil {
		return err.Error(), true, nil
	}
	if node.spec.quick == nil {
		return tool.Execute(ctx, args)
	}
	node.quickIO.Lock()
	defer node.quickIO.Unlock()
	node.graph.mu.Lock()
	promoted, undone := node.spec.quick.promoted, node.spec.quick.undone
	node.graph.mu.Unlock()
	if promoted || undone {
		return "this quick task has ended", true, nil
	}
	// A shell's effects cannot be bounded from its text. All shell execution asks
	// first, which includes wrappers, aliases and scripts that hide a test run —
	// EXCEPT A COMMAND THAT CAN CHANGE NOTHING: a search, a listing, `git
	// status`, read by the same rule that lifts a prompt off such a call
	// (internal/approval's ReadOnly, which never vouches for a compound line).
	// A rename across a codebase starts by finding every use; finding them is
	// not a reason to become a full task.
	if quickToolNeedsPromotion(tool.Name) && !approval.ReadOnly(tool.Name, args) {
		return a.proposeQuickPromotion(ctx, node, call, "This needs a full task before running a command or creating media")
	}
	path, _, writes := a.mutatingPath(call)
	if !writes {
		return tool.Execute(ctx, args)
	}
	return a.executeQuickFileTool(ctx, node, tool, call, args, path)
}

// readingToolRefusal also guards calls named off the belt. The names come from
// the same set that arms the hand-off, so its promise cannot drift from it.
func (a *Agent) readingToolRefusal(name string) string {
	if !a.config.readingTask() || earlyTools[name] {
		return ""
	}
	names := make([]string, 0, len(earlyTools))
	for name, allowed := range earlyTools {
		if allowed {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return "this reading can only look — use " + strings.Join(names, ", ") + "; nothing ran"
}

// readingTask is the admission mark, never a guess from the worker's title.
func (c Config) readingTask() bool {
	node := c.taskControl
	return node != nil && node.spec.quick != nil && node.spec.quick.reading
}

// A known-path mutation keeps both measurements inside one serialized action.
func (a *Agent) executeQuickFileTool(ctx context.Context, node *TaskNode, tool bare.Tool, call ai.ToolCall, args json.RawMessage, path string) (string, bool, error) {
	node.graph.mu.Lock()
	captures := cloneQuickCaptures(node.spec.quick.captures)
	node.graph.mu.Unlock()
	initialCount := len(captures)
	index := -1
	for i := range captures {
		if captures[i].Path == path {
			index = i
			break
		}
	}
	if index < 0 && len(captures) >= QuickTaskFileLimit {
		return a.proposeQuickPromotion(ctx, node, call, fmt.Sprintf("This is bigger than a quick task · %d files", len(captures)+1))
	}
	before, exists, mode, err := quickRead(path)
	if err != nil {
		return "Can't capture the file before writing: " + err.Error(), true, nil
	}
	if index < 0 {
		index = len(captures)
		captures = append(captures, QuickFileCapture{Path: path, Before: before, BeforeExists: exists, After: before, AfterExists: exists, Mode: mode})
	} else if quickHash(before, exists) != quickHash(captures[index].After, captures[index].AfterExists) {
		return "The file changed outside this quick task; nothing was written: " + path, true, nil
	}
	if err := node.saveQuickCaptures(captures); err != nil {
		return "Can't keep Undo before writing: " + err.Error(), true, nil
	}
	text, failed, runErr := tool.Execute(ctx, args)
	after, afterExists, _, readErr := quickRead(path)
	if readErr != nil {
		return "Can't read the file after writing: " + readErr.Error(), true, nil
	}
	captures[index].After, captures[index].AfterExists = after, afterExists
	if failed && index == initialCount && quickHash(before, exists) == quickHash(after, afterExists) {
		captures = captures[:initialCount]
	}
	if err := node.saveQuickCaptures(captures); err != nil {
		return "The file was written, but its Undo receipt could not be saved: " + err.Error(), true, nil
	}
	return text, failed, runErr
}

func (n *TaskNode) saveQuickCaptures(captures []QuickFileCapture) error {
	err := n.graph.writeAppControl(func() (func(), error) {
		old := n.spec.quick.captures
		n.spec.quick.captures = cloneQuickCaptures(captures)
		n.app.Version++
		return func() { n.spec.quick.captures = old; n.app.Version-- }, nil
	})
	if err == nil {
		n.graph.announce(n)
	}
	return err
}

// A promotion has no clock and the blocked action never runs in the quick worker.
// The new task waits for that worker to leave, then continues in the same folder.
func (a *Agent) proposeQuickPromotion(ctx context.Context, node *TaskNode, call ai.ToolCall, reason string) (string, bool, error) {
	owner := node.graph.quickProposalOwner(node)
	if owner == nil {
		return reason + " · ask for a full task", true, nil
	}
	graph := node.graph
	id := graph.reserve()
	graph.mu.Lock()
	line, items := node.spec.quick.line, append([]string(nil), node.spec.quick.items...)
	graph.mu.Unlock()
	spec := taskSpec{title: node.title(), named: true, summary: reason, brief: line + "\n\nItems: " + strings.Join(items, "; ") + "\n\nThe next blocked action was " + call.Function.Name + ": " + call.Function.Arguments,
		acceptance: taskPersonAcceptance, ground: a.config.Workspace, where: "in place", mode: TaskModeInPlace,
		admission: a.admissionContext(), model: node.runModel(), irreversible: true, dependsOn: []uint64{node.id}, parent: node.parent, owner: node.owner}
	// The quick-task route must know which proposal to answer before that
	// proposal is published; otherwise an immediate Promote can wait on itself.
	if err := graph.writeAppControl(func() (func(), error) {
		old, oldReason := node.spec.quick.promotion, node.spec.quick.grew
		node.spec.quick.promotion, node.spec.quick.grew = id, reason
		return func() { node.spec.quick.promotion, node.spec.quick.grew = old, oldReason }, nil
	}); err != nil {
		return "Can't keep the promotion: " + err.Error(), true, nil
	}
	wait, err := owner.openTask(ctx, id, spec, "")
	if err != nil {
		clearErr := node.clearQuickPromotion()
		return errors.Join(err, clearErr).Error(), true, nil
	}
	answer, err := wait.answer()
	if err != nil || !answer.Approved {
		if clearErr := node.clearQuickPromotion(); err != nil || clearErr != nil {
			return errors.Join(err, clearErr).Error(), true, nil
		}
		return reason + " · promotion declined; nothing ran", true, nil
	}
	if answer.Redirect != "" {
		spec.brief += "\n\n" + answer.Redirect
	}
	if answer.Model != "" {
		spec.model = answer.Model
	}
	if err := graph.writeAppControl(func() (func(), error) {
		node.spec.quick.promoted = true
		return func() { node.spec.quick.promoted = false }, nil
	}); err != nil {
		return err.Error(), true, nil
	}
	graph.admit(id, spec)
	graph.mu.Lock()
	cancel := node.cancel
	graph.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return "Promoted to a task; the blocked action will run there", true, nil
}

// A declined or withdrawn question no longer holds the worker. Clear its
// pending mark as well, so a running row cannot offer a dead promotion.
func (n *TaskNode) clearQuickPromotion() error {
	return n.graph.writeAppControl(func() (func(), error) {
		quick := n.spec.quick
		old, reason := quick.promotion, quick.grew
		quick.promotion, quick.grew = 0, ""
		return func() { quick.promotion, quick.grew = old, reason }, nil
	})
}

// Undo validates every after hash before changing any file. A mismatch changes
// nothing and names every path, including a newly created file removed elsewhere.
func (a *Agent) UndoQuickTask(id uint64) ([]string, []string, error) {
	node := a.taskNode(id)
	if node == nil || node.spec.quick == nil {
		return nil, nil, errors.New("no such quick task")
	}
	node.quickIO.Lock()
	defer node.quickIO.Unlock()
	node.graph.mu.Lock()
	quick := node.spec.quick
	state := node.state
	captures := cloneQuickCaptures(quick.captures)
	ended := quick.promoted || quick.undone
	node.graph.mu.Unlock()
	if !state.settled() || ended {
		return nil, nil, errors.New("only a finished quick task can be undone")
	}
	changed := []string{}
	for _, capture := range captures {
		raw, exists, _, err := quickRead(capture.Path)
		if err != nil || quickHash(raw, exists) != quickHash(capture.After, capture.AfterExists) {
			changed = append(changed, capture.Path)
		}
	}
	if len(changed) > 0 {
		sort.Strings(changed)
		return nil, changed, nil
	}
	undone := []string{}
	for _, capture := range captures {
		var err error
		if capture.BeforeExists {
			err = os.WriteFile(capture.Path, capture.Before, capture.Mode)
			if err == nil {
				err = os.Chmod(capture.Path, capture.Mode)
			}
		} else {
			err = os.Remove(capture.Path)
		}
		if err != nil {
			return undone, nil, fmt.Errorf("undo %s: %w", capture.Path, err)
		}
		undone = append(undone, capture.Path)
	}
	err := node.graph.writeAppControl(func() (func(), error) { quick.undone = true; return func() { quick.undone = false }, nil })
	return undone, nil, err
}

// Line edits count only changes, rather than charging every unchanged line twice.
func quickLineChanges(before, after []byte) (added, removed int) {
	old, new := string(before), string(after)
	for _, edit := range udiff.Lines(old, new) {
		added += lineTally(edit.New)
		removed += lineTally(old[edit.Start:edit.End])
	}
	return
}

// Workspace restores and merges have the same unbounded file effects as a shell.
func quickToolNeedsPromotion(name string) bool {
	switch name {
	case "bash", "edit_video", "speak", furrow.ToolRestore, furrow.ToolFork, furrow.ToolMerge:
		return true
	}
	return strings.HasPrefix(name, "generate_")
}
