package session

// readroot.go is WHERE WORK NOBODY IS WATCHING MAY READ: inside the project it
// was set up in, and nowhere else.
//
// ── THE RUN THAT WENT LOOKING ──
//
// Validator S25a, 2026-09-10. A firing was told to judge upgrades "under any
// upgrade policy you have been given". The folder verbs that answer exactly
// that were refused it (they were not yet looks, internal/approval's
// readOnlyCalls), so it went looking with the hands it had: `ls ..`, then
// aforge's own home — the item's standing document, config.json, the SQLite
// collections database read as bytes, and another conversation's transcript,
// grepped for the policy. Its prompt grew from 20k to 105k tokens and it spent
// past its limit. Nothing it read was forbidden to a person, and all of it was
// the wrong road: the applicability law says a readable record is not thereby
// an applicable one, and raw state files are the one road around that law.
//
// ── THE LAW ──
//
// A TASK MAY READ THE WHOLE MACHINE (taskoutside.go), because a person handed
// it out from a conversation they are sitting in and a task about another
// repository still has to look at it. A FIRING IS NOT THAT: nobody handed it
// this run, nobody is reading along, and what it is for is the project it was
// set up in. So its reading hands resolve their path exactly as the tool will
// ([bare.ResolvePath]), through the filesystem as it is now, and a path that
// lands outside its root is refused with one line saying so — never a question
// for the person, because there is nothing for them to allow.
//
// What this cannot see, said plainly: a shell a person granted by pattern reads
// wherever its command names. That is a grant somebody wrote, and it is theirs.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// readingHands are the hands that read a file or a folder named by their
// `path` argument. Each defaults to the working directory when it names none.
var readingHands = map[string]bool{
	"read": true, "ls": true, "grep": true, "find": true,
	"read_document": true, "view_image": true,
}

// readRootGuard is the pre-action citizen for the law above, registered only on
// an agent that has a read root ([Config.readRoot]).
type readRootGuard struct{ root string }

func (readRootGuard) Name() string { return "read-root" }

func (g readRootGuard) PreAction(_ context.Context, _ *episode, _ *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	if !readingHands[call.Function.Name] {
		return call, toolResult{}, true
	}
	var args struct {
		Path string `json:"path"`
	}
	// Arguments that do not parse are the tool's own to complain about.
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return call, toolResult{}, true
	}
	named := strings.TrimSpace(args.Path)
	if named == "" {
		named = "."
	}
	if resolvesInside(g.root, bare.ResolvePath(named, g.root)) {
		return call, toolResult{}, true
	}
	return call, toolResult{text: outsideReadRoot(named, g.root), isError: true}, false
}

// outsideReadRoot is the refusal, in one place. It names both sides for
// taskoutside.go's reason: a model told only "outside" re-sends the call with a
// different corner changed.
func outsideReadRoot(named, root string) string {
	return named + " is outside this work's project (" + root + "): work that runs while nobody is watching reads only inside the project it was set up in."
}

// resolvesInside answers whether path, resolved through the filesystem as it is
// now, lies inside root. A path that does not exist yet is judged by the
// deepest part of it that does ([deepestExisting]), so a missing file inside
// the project is the tool's own "not found" and never this refusal.
func resolvesInside(root, path string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	return insideProject(realRoot, deepestExisting(filepath.Clean(path))) == nil
}

// deepestExisting is the longest leading part of path that is on disk now —
// path itself when it exists. It is what a containment check resolves, since a
// symlink can only be followed where there is something to follow.
func deepestExisting(path string) string {
	for {
		if _, err := os.Lstat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}
