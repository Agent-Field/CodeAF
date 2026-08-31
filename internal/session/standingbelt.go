package session

// THE BELT SEAM FOR THE STANDING TREE.
//
// standingtree.go decides WHERE a path aimed at a referred folder really goes.
// This file is the one place that decision touches a tool: three hands — read,
// write and edit — pass their `path` argument through it on the way in, and the
// write hands say one sentence about it on the way out.
//
// ── WHY THE WRAPPER IS OUTERMOST ──
//
// It rewrites `path` to the ABSOLUTE path inside the working copy and hands the
// call on. Everything downstream then works unchanged and unknowing: bare's own
// tools resolve an absolute path to itself, the appendable write reads the file
// it is about to add to from the copy rather than from the folder
// (tools_write.go), and the PDF sense opens the copy's bytes. A wrapper on the
// inside would have left each of those reading one file and writing another.
//
// ── AND WHY THE MODEL IS TOLD ──
//
// A write that quietly lands somewhere else is a write the model will reason
// about wrongly for the rest of the turn. So the FIRST write into each folder's
// copy carries one sentence in its result: where the work is, that the folder
// itself has not moved, and that the paths it has been using still work. It is
// said once per copy per process rather than on every call — a model that has
// been told does not need telling again, and a resumed conversation gets told
// again because its model has not been told at all.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// standingWriters are the hands that may CUT a working copy, spelled once. Every
// other hand on the belt either only reads or does not take a path at all.
var standingWriters = map[string]bool{"write": true, "edit": true}

// placeAimed wraps one tool so that its path is read against the folders this
// conversation refers to.
//
// A tool whose arguments do not parse, or that names no path, is handed to the
// inner tool exactly as it arrived: this wrapper owns the aiming and never the
// wording of a fault, which belongs to the tool that has always reported it.
func (a *Agent) placeAimed(inner bare.Tool) bare.Tool {
	writing := standingWriters[inner.Name]
	return bare.Tool{
		Name:        inner.Name,
		Description: inner.Description,
		Schema:      inner.Schema,
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			// The path is read through the belt's one decoder (toolargs.go), so a
			// model that spelled its arguments loosely is aimed correctly rather
			// than handed past this wrapper by a stricter second reading. The map
			// beside it is what carries EVERY OTHER ARGUMENT THROUGH UNTOUCHED:
			// this wrapper knows about one field and must not become a place where
			// content, edits or a limit could be dropped.
			var named struct {
				Path string `json:"path"`
			}
			if err := decodeToolArguments(args, &named); err != nil || strings.TrimSpace(named.Path) == "" {
				return inner.Execute(ctx, args)
			}
			var arguments map[string]json.RawMessage
			if err := decodeToolArguments(args, &arguments); err != nil {
				return inner.Execute(ctx, args)
			}
			aimed, tree, ok := a.standingAim(named.Path, writing)
			if !ok {
				return inner.Execute(ctx, args)
			}
			replaced, err := json.Marshal(aimed)
			if err != nil {
				return inner.Execute(ctx, args)
			}
			arguments["path"] = replaced
			rewritten, err := json.Marshal(arguments)
			if err != nil {
				return inner.Execute(ctx, args)
			}
			text, isError, execErr := inner.Execute(ctx, rewritten)
			if execErr != nil || isError || !writing {
				// A WRITE THAT FAILED IS NOT A FILE THAT MOVED. Recording it
				// would put a file on the chip's count and in the landing that
				// nothing ever wrote.
				return text, isError, execErr
			}
			a.noteStandingWrite(tree, aimed)
			if a.tellStanding(tree.Folder) {
				return text + "\n\n" + standingSentence(tree.Folder), false, nil
			}
			return text, false, nil
		},
	}
}

// tellStanding answers whether this process has told the model about one
// folder's working copy yet, and marks it told.
//
// The record is IN MEMORY AND NOT ON THE META, deliberately. What it tracks is
// whether the model in front of it has been told, and a resumed conversation is
// a model that has been told nothing — so the sentence comes back once on the
// first write of every process, which is exactly when it is needed again.
func (a *Agent) tellStanding(folder string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.toldStanding == nil {
		a.toldStanding = map[string]bool{}
	}
	if a.toldStanding[folder] {
		return false
	}
	a.toldStanding[folder] = true
	return true
}

// standingSentence is what the model reads the first time it writes into a
// folder it referred to.
//
// IT SAYS THE THREE THINGS THAT CHANGE WHAT IT DOES NEXT: the folder itself has
// not moved, the paths it is already using keep working, and somebody has to
// run /land for the work to arrive. Everything else — which road the copy took,
// where the copy sits — is machinery it cannot act on, so it is not said.
func standingSentence(folder string) string {
	name := filepath.Base(folder)
	return "Your changes to " + name + " are being kept for this conversation and are not in " + folder +
		" itself yet. Go on using the same paths — reading and writing them reaches your own version." +
		" When the work is ready, tell the person to run /land, which shows them what changed and puts it into the folder."
}
