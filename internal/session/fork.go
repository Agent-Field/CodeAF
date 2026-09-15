package session

// ── ONE READING OF A DECLARED WRITE SCOPE ───────────────────────────────────
//
// A DECLARED WRITE SCOPE is the list of paths an agent may change, and it is
// the only thing standing between two minds working in one working copy and a
// pair of writes nobody meant. It has two ends, and this file is the one
// reading both of them use:
//
//   - THE DOOR that accepts a scope somebody declared — a quick task's `files`
//     claim (task_quick.go), an adaptive run's node scope — normalizes it here
//     before it is ever stored, so what is kept is already the guard's form.
//   - THE GUARD that enforces it ([writeGuard], orchestrate.go, registered in
//     hooks.go) normalizes again here through [scopeAsGuarded], for the scopes
//     that reach it without passing a door.
//
// NORMALIZING TWICE IS FREE; NORMALIZING IN ONE PLACE ONLY WAS THE DEFECT — see
// [normalizeScopePath], which carries the account of the seam this cost.
//
// [forkScopesCollide] is the other half of the same safety: two scopes that
// share a path are the one shape the guard cannot save, so a door that hands
// out scopes refuses the overlap before either piece of work starts.

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/orchestrate"
)

// normalizeScopePath is THE ONE READING OF A DECLARED WRITE SCOPE, and both
// ends of the scope machinery go through it: the DOOR that accepts a model's
// declaration and the GUARD that enforces it (orchestrate.go's [writeGuard]
// through [scopeAsGuarded]).
//
// IT EXISTS BECAUSE THE TWO ENDS ONCE READ ONE STRING DIFFERENTLY, and a whole
// division was voided at the seam. A task worker handed three slices of one
// working copy out two minutes into its life and declared their scopes as
// ABSOLUTE paths — "/workspace/rust-java-lsp/src/parser.rs" — which the door
// only trimmed and checked for overlap, because the schema SAID
// "workspace-relative" and nothing made it so. The guard then turned each
// attempted write into a workspace-relative path ("src/parser.rs") before
// asking [orchestrate.Covers], and a relative path is never covered by an
// absolute one: every write in every slice was refused, the model reported that
// the tool was refusing files that were plainly in its scope, and it never
// divided again. Nothing had gone wrong except that two readings of one string
// disagreed.
//
// So the scope is converted to the form the guard reads — workspace-relative,
// slash-separated, cleaned — at the door, and the guard converts again with this
// same function for the scopes that never came through a door (an adaptive run's
// node scopes are drawn by a planner, orchestrate.go). NORMALIZING TWICE IS
// FREE; NORMALIZING IN ONE PLACE ONLY WAS THE DEFECT.
//
// The error is a FRAGMENT rather than a sentence: it names what is wrong with
// this one path, and the caller writes the sentence its own reader can act on.
func normalizeScopePath(workspace, raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", errors.New("it is empty")
	}
	local := filepath.FromSlash(clean)
	if filepath.IsAbs(local) {
		// AN ABSOLUTE PATH UNDER THE WORKSPACE IS ACCEPTED AND CONVERTED, never
		// refused. It names exactly the same file, it is the form a model reaches
		// for after a turn spent reading absolute paths, and a refusal would spend
		// a round teaching it a spelling. What is refused is an absolute path that
		// is NOT under the workspace, because that is a different claim entirely.
		root := strings.TrimSpace(workspace)
		if root == "" {
			return "", errors.New("it is an absolute path and there is no workspace to read it against")
		}
		relative, err := filepath.Rel(root, local)
		if err != nil {
			return "", errors.New("it is outside the workspace")
		}
		local = relative
	}
	slash := path.Clean(filepath.ToSlash(local))
	switch {
	case slash == ".." || strings.HasPrefix(slash, "../"):
		return "", errors.New("it is outside the workspace")
	case slash == "." || slash == "/":
		// THE WHOLE WORKSPACE IS NOT A SLICE OF IT. "." reaches every path and so
		// collides with every sibling, and [orchestrate.Covers] reads it as
		// covering NOTHING — so a scope of "." would be a declaration that
		// silently permitted no write at all, which is the exact shape of the
		// failure this function exists to end.
		return "", errors.New("it is the whole workspace rather than a slice of it")
	}
	return slash, nil
}

// normalizeWriteScope reads a WHOLE declared scope and answers either the paths
// in the guard's own form or the one sentence the model is given back.
//
// THE REFUSAL NAMES THE OFFENDING SCOPE AND THE EXPECTED FORM, because a model
// that is told only "no" re-sends the same declaration. It is a RESULT and not
// an error for the reason every refusal in this file is one: the caller redraws
// its scopes and carries on.
//
// A BLANK ENTRY IS DROPPED RATHER THAN REFUSED — a stray "" in a list of real
// paths is a formatting slip, not a claim — and a scope that is nothing BUT
// blanks comes back empty, which its caller answers in its own words.
func normalizeWriteScope(workspace string, scope []string) ([]string, string) {
	out := make([]string, 0, len(scope))
	for _, raw := range scope {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		clean, err := normalizeScopePath(workspace, raw)
		if err != nil {
			return nil, fmt.Sprintf("%q %s. Write scopes are paths INSIDE the working copy, relative to its "+
				"root — like src/parser.rs or docs — and the working copy is %s, so a path under it may also be "+
				"given in full. Redraw the scope and call again.",
				strings.TrimSpace(raw), err.Error(), workspaceShown(workspace))
		}
		out = append(out, clean)
	}
	return out, ""
}

// workspaceShown is the working copy as the refusal names it. An agent with no
// workspace configured is a real case in tests and in a door that never set one,
// and a sentence ending in "the working copy is ." would be worse than one that
// says there is not one.
func workspaceShown(workspace string) string {
	if workspace = strings.TrimSpace(workspace); workspace == "" {
		return "not set for this agent"
	}
	return workspace
}

// scopeAsGuarded is the scope as the ENFORCEMENT must read it: every declaration
// put through [normalizeScopePath], and the ones that cannot be normalized
// DROPPED rather than passed through.
//
// Dropping is the safe direction and it is the only one available here. A scope
// entry that names something outside the working copy cannot be satisfied by any
// path inside it, so keeping it would admit nothing; passing it through raw is
// what let an absolute declaration sit in a scope silently matching no write at
// all. The door refuses these with a sentence the model can act on — this is for
// the scopes that reach the guard without passing a door.
func scopeAsGuarded(workspace string, scope []string) []string {
	out := make([]string, 0, len(scope))
	for _, raw := range scope {
		if clean, err := normalizeScopePath(workspace, raw); err == nil {
			out = append(out, clean)
		}
	}
	return out
}

// forkScopesCollide reports the first path two scopes share, if any.
func forkScopesCollide(left, right []string) (string, bool) {
	for _, one := range left {
		if orchestrate.Covers(right, one) {
			return one, true
		}
	}
	for _, two := range right {
		if orchestrate.Covers(left, two) {
			return two, true
		}
	}
	return "", false
}

// ── AND ONE READING OF A TRANSCRIPT SOMEBODY ELSE IS ABOUT TO OPEN ON ────────

// forkSeed is the copy a promoted worker opens with: the caller's transcript up
// to this call, and the system message that stands in front of it.
//
// THE ASSISTANT MESSAGE CARRYING THIS VERY CALL LOSES ITS TOOL CALLS AND KEEPS
// ITS WORDS. It has to lose them: a request whose last assistant message asks
// for a tool nothing has answered is a shape providers refuse, and the answer
// does not exist yet — this function is running inside it. It keeps the words
// because those are the caller's own sentence about what it is about to do,
// which is the most recent thing a worker could be told and the one thing it
// would otherwise have to be told twice.
//
// THE SYSTEM MESSAGE IS HANDED OVER AS TEXT AS WELL AS RIDING AT seed[0], so the
// worker's own [Agent.refreshSystemLocked] rewrites message[0] to what is
// already there. Without it the worker's first request would carry a freshly
// rendered prompt in front of a transcript built against the caller's, and the
// shared prefix — the whole economic argument for inheriting anything — would
// break on its first byte.
//
// IT IS THE FUNCTION #811 DELETED, RESTORED RATHER THAN REWRITTEN. `fork` left
// the belt in that wave and took its only caller with it; the reading itself was
// never wrong, and writing a second one beside the scope reader above would have
// been two answers to one question (promote.go states the law it serves now).
func (a *Agent) forkSeed() ([]ai.Message, string) {
	seed := a.snapshot()
	if len(seed) == 0 {
		return nil, ""
	}
	if last := len(seed) - 1; len(seed[last].ToolCalls) > 0 {
		stripped := seed[last]
		stripped.ToolCalls = nil
		if strings.TrimSpace(messageContentText(stripped)) == "" {
			seed = seed[:last]
		} else {
			seed[last] = stripped
		}
	}
	if len(seed) == 0 {
		return nil, ""
	}
	return seed, messageContentText(seed[0])
}
