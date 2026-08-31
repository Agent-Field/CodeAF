package furrow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// The four verbs furrow puts on the belt, and the one line that decides whether
// any of them are there.
//
// THEY ARE CONDITIONAL ON SOMETHING NO CONFIGURATION CAN FIX: whether this
// folder has been attached with `furrow watch`. The binary itself is no longer
// in question — aforge carries furrow inside it (internal/furrowbin) — so the
// half that used to vary by machine does not, and the half that remains is the
// one a person decides per project. A belt is a promise, though: every tool on
// it is something the model has been told it can do, and a `workspace_restore`
// that answers "this repository is not watched" costs the model a call, reads
// as temporary, and stays in its plan for the rest of the turn. A model that
// was never told about these simply says the files cannot be put back, which is
// true and free.
//
// The names all begin `workspace_` for a second reason, and it is not tidiness.
// aforge already has a rewind, and it is an edit of the CONVERSATION that
// deliberately touches nothing on disk (internal/session/rewind.go). These move
// the folder. A model holding both must never confuse them, so the two families
// do not share a word: one is rewind, the other is workspace_restore.
const (
	// ToolSnapshots is the read-only look at the timeline.
	ToolSnapshots = "workspace_snapshots"
	// ToolRestore puts the folder back, and is the only destructive one.
	ToolRestore = "workspace_restore"
	// ToolFork runs something risky in a copy of the whole workspace.
	ToolFork = "workspace_fork"
	// ToolMerge lands a copy's changes, gated on a check.
	ToolMerge = "workspace_merge"
)

// Tools is the whole seam a caller wires in one line: the four verbs when
// furrow is here and this folder is attached to it, and NOTHING AT ALL
// otherwise. A caller writes
//
//	tools = append(tools, furrow.Tools(ctx, workspace)...)
//
// and has nothing else to remember. The nil case is not an error and is not
// worth logging: on a machine without furrow it is what every session sees, and
// it is the design working.
func Tools(ctx context.Context, root string) []bare.Tool {
	workspace := Open(ctx, root)
	if workspace == nil {
		return nil
	}
	return workspace.Tools()
}

// Tools is the same four verbs for a caller that already holds the workspace —
// a host that opened it once and hands it to every session it serves.
func (w *Workspace) Tools() []bare.Tool {
	return []bare.Tool{
		w.snapshotsTool(),
		w.restoreTool(),
		w.forkTool(),
		w.mergeTool(),
	}
}

// ── what the model is told ───────────────────────────────────────────────────

// The descriptions say what furrow is, once, because the model has never heard
// of it: its training data does not contain this program any more than it
// contains aforge. They also say plainly which of these change the folder and
// which do not, since that distinction is the only one that can hurt somebody.

var snapshotsDescription = fmt.Sprintf("List the workspace restore points: moments the whole folder — files, dependencies, .env, the dev database, git's own mutable state — was sealed and can be put back to exactly. This is a separate history from git and it holds everything git does not: uncommitted edits, ignored files, local state. Newest first, with the id workspace_restore takes, when it was sealed, and what it was called. Reads only; changes nothing. Default %d restore points, at most %d.", snapshotsDefault, snapshotsMax)

var snapshotsSchemaJSON = fmt.Sprintf(`{"type":"object","properties":{"limit":{"type":"number","description":"How many restore points to list, newest first (default: %d, maximum: %d)"}},"additionalProperties":false}`, snapshotsDefault, snapshotsMax)

const restoreDescription = "Put the workspace back to a restore point from workspace_snapshots — the whole folder, or only the paths you name. This is how a deleted .env, a corrupted dev database or a wiped working tree comes back; git protects none of those. IT CHANGES FILES ON DISK. Called without confirm it only PREVIEWS: it lists every path the restore would touch and does nothing. Call it again with confirm true to actually restore, and only after the person has said yes — never on your own judgement. Restoring seals the current state first, so the restore is itself undoable."

const restoreSchemaJSON = `{"type":"object","properties":{"snapshot":{"type":"string","description":"The restore point id, exactly as workspace_snapshots gave it"},"paths":{"type":"array","items":{"type":"string"},"description":"Restore only these paths, relative to the workspace (for example '.env'). Omit to restore the whole folder."},"confirm":{"type":"boolean","description":"false or omitted previews the restore and changes nothing; true applies it. Only ever true when the person has agreed to this restore."}},"required":["snapshot"],"additionalProperties":false}`

const forkDescription = "Run a command inside a copy-on-write fork of the entire workspace — every file, dependency, .env and the dev database, ready in about a second — so a risky upgrade, a destructive migration or a wide refactor cannot touch the real folder. The real workspace is not modified whatever the command does. Returns the fork's name, what the command printed and its exit code; nothing lands until you call workspace_merge with that name."

const forkSchemaJSON = `{"type":"object","properties":{"command":{"type":"string","description":"The shell command to run inside the fork, as you would type it"},"name":{"type":"string","description":"A stable name for this fork, so workspace_merge can land it later. Omit to let furrow name it."}},"required":["command"],"additionalProperties":false}`

const mergeDescription = "Land a fork's changes back into the real workspace, made with workspace_fork. Give a check — the project's tests, a build — and nothing lands unless it passes: furrow assembles the merged result somewhere else, runs the check there, and abandons it if the check says no. Conflicting paths stop the merge and nothing lands. Set preview true to see the changes and conflicts without merging or running the check."

const mergeSchemaJSON = `{"type":"object","properties":{"fork":{"type":"string","description":"The fork's name, as workspace_fork gave it"},"check":{"type":"string","description":"A command that must pass before anything lands, for example 'go test ./...'. Strongly recommended."},"preview":{"type":"boolean","description":"true plans the merge and reports its changes and conflicts without merging anything or running the check"}},"required":["fork"],"additionalProperties":false}`

// Descriptions and Schemas are the tool metadata as data, for a caller that
// registers these somewhere other than a bare.Tool slice — a remote belt
// summary, a manual gate, a test that wants the exact strings. They are the
// same values the tools carry, read from one place so the two cannot drift.
func Descriptions() map[string]string {
	return map[string]string{
		ToolSnapshots: snapshotsDescription,
		ToolRestore:   restoreDescription,
		ToolFork:      forkDescription,
		ToolMerge:     mergeDescription,
	}
}

func Schemas() map[string]string {
	return map[string]string{
		ToolSnapshots: snapshotsSchemaJSON,
		ToolRestore:   restoreSchemaJSON,
		ToolFork:      forkSchemaJSON,
		ToolMerge:     mergeSchemaJSON,
	}
}

// Names is every tool this package can put on a belt, for the manual gate and
// for a caller that wants to name them without building them.
func Names() []string {
	return []string{ToolSnapshots, ToolRestore, ToolFork, ToolMerge}
}

// ── the hands ────────────────────────────────────────────────────────────────

func (w *Workspace) snapshotsTool() bare.Tool {
	return bare.Tool{
		Name:        ToolSnapshots,
		Description: snapshotsDescription,
		Schema:      json.RawMessage(snapshotsSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Limit *int `json:"limit"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			limit := snapshotsDefault
			if parsed.Limit != nil {
				limit = *parsed.Limit
			}
			snapshots, err := w.Snapshots(ctx, limit)
			if err != nil {
				return err.Error(), true, nil
			}
			if len(snapshots) == 0 {
				return "This workspace has no restore points yet.", false, nil
			}
			var out strings.Builder
			for _, snapshot := range snapshots {
				fmt.Fprintf(&out, "%s  %s", snapshot.ID, snapshot.SealedAt.Format(time.RFC3339))
				// The emptiness law: a label nobody wrote, a grade furrow did
				// not declare and a pin nobody set render as nothing rather
				// than as an empty column.
				if snapshot.Label != "" {
					fmt.Fprintf(&out, "  %s", snapshot.Label)
				}
				if snapshot.Grade != "" {
					fmt.Fprintf(&out, "  [%s]", snapshot.Grade)
				}
				if snapshot.Pinned {
					out.WriteString("  pinned")
				}
				out.WriteByte('\n')
			}
			return strings.TrimRight(out.String(), "\n"), false, nil
		},
	}
}

func (w *Workspace) restoreTool() bare.Tool {
	return bare.Tool{
		Name:        ToolRestore,
		Description: restoreDescription,
		Schema:      json.RawMessage(restoreSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Snapshot string   `json:"snapshot"`
				Paths    []string `json:"paths"`
				Confirm  bool     `json:"confirm"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			// The preview runs first WHATEVER WAS ASKED FOR, so that a restore
			// and the account of what it would do are never two different
			// answers, and so that an empty plan stops before furrow is asked
			// to change anything.
			preview, err := w.PreviewRestore(ctx, parsed.Snapshot, parsed.Paths)
			if err != nil {
				return err.Error(), true, nil
			}
			if len(preview.Changes) == 0 {
				return "Nothing to restore: the workspace already matches that restore point.", false, nil
			}
			if !parsed.Confirm {
				return "Nothing applied — this is a preview. Call workspace_restore again with confirm true to apply it.\n\n" + changeList(preview), false, nil
			}
			applied, err := w.ApplyRestore(ctx, parsed.Snapshot, parsed.Paths)
			if err != nil {
				return err.Error(), true, nil
			}
			out := fmt.Sprintf("Restored %d path(s) from %s.", len(applied.Changes), applied.Snapshot)
			if applied.Undo != "" {
				out += fmt.Sprintf("\nThe state before this restore was sealed as %s, so this restore can itself be undone.", applied.Undo)
			}
			return out + "\n\n" + changeList(applied), false, nil
		},
	}
}

// changeList renders a restore's paths, capped by bytes like every other
// captured stream so that putting a large tree back does not spend a turn's
// context listing it.
func changeList(restore Restore) string {
	var out strings.Builder
	for _, change := range restore.Changes {
		fmt.Fprintf(&out, "%-8s %s\n", change.Action, change.Path)
	}
	return capped(out.String())
}

func (w *Workspace) forkTool() bare.Tool {
	return bare.Tool{
		Name:        ToolFork,
		Description: forkDescription,
		Schema:      json.RawMessage(forkSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Command string `json:"command"`
				Name    string `json:"name"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			fork, err := w.RunInFork(ctx, parsed.Name, parsed.Command)
			if err != nil {
				return err.Error(), true, nil
			}
			var out strings.Builder
			// A command that failed inside a fork is REPORTED AND NOT AN ERROR.
			// The fork was made, the command ran, and it said no — which is
			// frequently the whole reason somebody asked for a fork.
			if fork.ExitCode == 0 {
				fmt.Fprintf(&out, "Ran in fork %s; the command succeeded.\n", fork.Name)
			} else {
				fmt.Fprintf(&out, "Ran in fork %s; the command exited %d.\n", fork.Name, fork.ExitCode)
			}
			fmt.Fprintf(&out, "The real workspace was not modified. Land it with workspace_merge on %q.\n", fork.Name)
			if fork.Output != "" {
				out.WriteString("\n" + fork.Output)
			}
			return strings.TrimRight(out.String(), "\n"), false, nil
		},
	}
}

func (w *Workspace) mergeTool() bare.Tool {
	return bare.Tool{
		Name:        ToolMerge,
		Description: mergeDescription,
		Schema:      json.RawMessage(mergeSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Fork    string `json:"fork"`
				Check   string `json:"check"`
				Preview bool   `json:"preview"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			merge, err := w.MergeFork(ctx, parsed.Fork, parsed.Check, parsed.Preview)
			if err != nil {
				return err.Error(), true, nil
			}
			return mergeReport(merge, parsed.Preview), false, nil
		},
	}
}

// mergeReport is the one place a merge is put into words, so that the three
// ways a merge can decline — a check said no, paths conflicted, it was only a
// preview — read as three different sentences and never as one vague one.
func mergeReport(merge Merge, preview bool) string {
	var out strings.Builder
	switch {
	case merge.CheckFailed:
		out.WriteString("The check failed, so nothing was merged.\n")
	case len(merge.Conflicts) > 0:
		fmt.Fprintf(&out, "Nothing was merged: %d path(s) changed on both sides.\n", len(merge.Conflicts))
		for _, conflict := range merge.Conflicts {
			fmt.Fprintf(&out, "%-14s %s\n", conflict.Kind, conflict.Path)
		}
	case preview:
		fmt.Fprintf(&out, "Nothing merged — this is a preview. %d path(s) would change, with no conflicts.\n", merge.Changes)
	case merge.Landed:
		fmt.Fprintf(&out, "Merged %s: %d path(s) changed.\n", merge.Fork, merge.Changes)
		if merge.Result != "" {
			fmt.Fprintf(&out, "Sealed as %s.\n", merge.Result)
		}
	default:
		out.WriteString("Nothing was merged.\n")
	}
	if merge.CheckOutput != "" {
		out.WriteString("\n" + merge.CheckOutput)
	}
	return strings.TrimRight(out.String(), "\n")
}
