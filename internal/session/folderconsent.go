package session

// THE CHAT EDITS THE REAL FOLDER, AND IT ASKS ONCE BEFORE IT FIRST DOES.
//
// Until 2026-09-12 a folder the person ATTACHED got a working copy of its own:
// a `git worktree` off its HEAD, or a recursive copy, with `read`, `write` and
// `edit` quietly aimed into it and `/land` to bring the work home. This file is
// what replaced the whole of that, and the replacement is not a smaller copy —
// it is no copy at all.
//
// ── WHY THE COPY HAD TO GO ───────────────────────────────────────────────────
//
// The copy was aimed by a `path` ARGUMENT, so it only ever covered the three
// hands that take one. `bash` did not: a shell command names its paths inside a
// string nobody parses, and it ran in the workspace and touched whatever it
// named. `grep`, `find` and `ls` did not either. On a measured turn of 62 tool
// calls, 27 were bash — `git pull`, `git checkout -b`, `go build`, all in the
// person's REAL repository — while six edits landed in the copy. The sentence
// the model was handed on its first write ("reading and writing them reaches
// your own version") was false for four hands out of seven, and the model
// reasoned from it for the rest of the turn. Worse, the copy was cut at the
// local HEAD and never refreshed, so the landing refused as soon as the model's
// own `git pull` moved the branch — and two stray branches were left behind in
// somebody's project.
//
// A worktree isolates CONCURRENT UNATTENDED WRITERS. That is the task case, and
// tasks keep theirs ([taskTree.comeHome], [taskTree.landMirror]). A chat is one
// person watching one model, and git is already the undo.
//
// ── SO WHAT IS LEFT IS ONE QUESTION, THROUGH THE GATE THAT ALREADY ASKS ──────
//
// There is no new gate here and no second policy. consent.go's approval gate
// already grades every call and already remembers an answer; all this file does
// is two things to a decision that gate has already taken:
//
//   - IT NAMES THE FOLDER IN THE QUESTION. A call that is not read-only and that
//     spells an attached folder's path is about to change something in a place
//     the person only pointed at, and that — not "tool \"edit\"" — is what the
//     card should say.
//   - IT MAKES THE REMEMBERED ANSWER FOLDER-SHAPED. A standing yes to a question
//     about a folder is a yes ABOUT THAT FOLDER, not about `edit` everywhere
//     forever, and the next call under it — `bash` included — is covered by it.
//
// THE GATE'S OWN FLOORS ARE UNTOUCHED. A rule that denies still denies, the
// critical-command table and the calls that act in the person's name still ask
// whatever is remembered ([approval.AlwaysAsks]), and a session whose blanket
// mode is `allow` — `tools.approvalMode: allow`, or `--yolo` — is never asked
// anything here, because nothing in this file raises a decision. It only
// rephrases one that was already going to be a question, and widens the answer.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// folderAimed is the referred folder ONE CALL is about, or nothing.
//
// THE WHOLE CALL IS READ AND NOT ONLY ITS `path`. That is the lesson the copy
// taught at cost: a reading that knows about one argument name covers three
// hands and silently misses the one that does most of the work. So there are two
// arms and the call has to fail both to be judged not about a folder — the path
// where a hand has one, resolved the way the tools resolve it, and then the
// whole arguments text searched for the folder as it is spelled.
//
// WHAT IT CANNOT SEE IS WRITTEN DOWN RATHER THAN GUESSED AT. A command that
// names no path under an attached folder is not about one: a relative path in a
// shell command resolves against [Config.Workspace] (bare runs it there), and
// the workspace is never inside an attached folder — [Agent.referredFolderOf]
// sends every path under it direct. So the one shape this misses is a command
// that reaches an attached folder through a name this process cannot resolve: a
// symlink, or a variable the shell expands for itself. The manual says so in the
// person's own words rather than leaving it to be discovered.
//
// THE DEEPEST FOLDER WINS, exactly as the aiming it replaces decided: somebody
// who attached a project and then one library inside it is asked about the
// library.
func (a *Agent) folderAimed(call ai.ToolCall) (string, bool) {
	args := strings.TrimSpace(call.Function.Arguments)
	if args == "" || args == "null" {
		return "", false
	}
	workspace := a.workspaceStoodIn()
	// FIRST THE PATH THE HAND WAS GIVEN, where it has one, read through the
	// belt's own decoder so a model that spelled its arguments loosely is read
	// the same way the tool will read them (toolargs.go). This is the arm that
	// covers `~/code/proj/x.go` and a relative name — spellings a search of the
	// text could never match against an absolute folder.
	var named struct {
		Path string `json:"path"`
	}
	if err := decodeToolArguments(json.RawMessage(args), &named); err == nil && strings.TrimSpace(named.Path) != "" {
		if folder, ok := a.referredFolderOf(canonicalPath(resolveAgainst(named.Path, workspace)), workspace); ok {
			return folder, true
		}
	}
	// AND THEN THE WHOLE CALL AS TEXT. A folder is spelled the same way wherever
	// it appears — inside a `command`, inside a list of files, inside an argument
	// no hand on today's belt has yet — so this arm is the one that holds for
	// every hand, and it is the arm the copy never had. A JSON escape cannot hide
	// a path: a directory name holds no character JSON escapes.
	best := ""
	for _, place := range a.referredPlaces() {
		folder := strings.TrimSpace(place.Path)
		if place.Arrival != PlaceSaid || folder == "" || len(folder) <= len(best) {
			continue
		}
		if workspace != "" && under(folder, workspace) {
			continue
		}
		if strings.Contains(args, folder) {
			best = folder
		}
	}
	return best, best != ""
}

// referredFolderOf is the attached folder one PATH is inside, or nothing, and it
// keeps the two laws this reading has always kept:
//
//   - THE STANDING WORKSPACE ALWAYS WINS. A path inside the folder the
//     conversation was launched in is edited directly and always was; a card
//     about it would be asking somebody to approve their own working directory.
//   - THE DEEPEST ATTACHED FOLDER WINS AFTER THAT, so somebody who attached a
//     project and then one library inside it is asked about the library.
//
// A ground the ladder worked out for itself is not an attached folder (places.go
// calls that a [PlaceKept] row), and a card about one would ask the person to
// approve a path they never typed.
func (a *Agent) referredFolderOf(path, workspace string) (string, bool) {
	if workspace != "" && under(path, workspace) {
		return "", false
	}
	best := ""
	for _, place := range a.referredPlaces() {
		if place.Arrival != PlaceSaid || !under(path, place.Path) {
			continue
		}
		if len(place.Path) > len(best) {
			best = place.Path
		}
	}
	return best, best != ""
}

// workspaceStoodIn is the directory this conversation is standing in, canonical,
// read under the lock that moves it.
//
// IT TAKES THE LOCK BECAUSE THE ANSWER MOVES. `anchor_workspace` rewrites both
// [Config.Workspace] and [Config.Place] under a.mu in one breath
// (tools_anchor_workspace.go), and this runs on the tool path where that call
// can land beside it.
func (a *Agent) workspaceStoodIn() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return canonicalPath(strings.TrimSpace(a.config.Workspace))
}

// under reports whether a path is the directory itself or something inside it.
// Both sides are canonical by the time they reach here, so this is arithmetic
// on strings and never touches the disk.
func under(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// resolveAgainst is the tools' own reading of a path — absolute as it stands, a
// relative name against the conversation's directory — spelled here rather than
// borrowed from bare so that this file does not depend on the belt to answer a
// question about a string.
func resolveAgainst(raw, workspace string) string {
	path := strings.TrimSpace(raw)
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), string(filepath.Separator)))
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(strings.TrimSpace(workspace), path)
	}
	return filepath.Clean(path)
}

// folderSays is the transform, applied to the gate's answer beside
// [Agent.capabilitySays] and for the same reason: the policy is a pure function
// of a call, and what this particular call is ABOUT is not in it.
//
// IT MOVES NO DECISION AT ALL. It changes the WORDS of a question the policy
// was already going to ask — which is why a session on `tools.approvalMode:
// allow` or `--yolo` is asked nothing here, and why a rule that denies still
// denies in its own words. Widening is not its job either: the answer to this
// question is banked against the FOLDER ([Agent.askAnswer]) and read back by
// [Agent.rememberedAnswer], which is the gate's own memo and already carries
// the floor guard that memo has to have.
//
// A CALL THE FLOOR ALWAYS ASKS ABOUT KEEPS ITS OWN REASON. `rm -rf` inside an
// attached folder is a critical command first and a change to that folder
// second, and a card that said only "a change in <folder>" would have taken the
// one sentence the person needed off the screen.
func (a *Agent) folderSays(call ai.ToolCall, decision approval.Decision) approval.Decision {
	if decision.Action != approval.ActionPrompt {
		return decision
	}
	args := json.RawMessage(call.Function.Arguments)
	if approval.ReadOnly(call.Function.Name, args) || approval.AlwaysAsks(call.Function.Name, args) {
		return decision
	}
	folder, aimed := a.folderAimed(call)
	if !aimed {
		return decision
	}
	// THE CARD SAYS THE FOLDER AND NOT THE MACHINERY. What a person needs to
	// know at this moment is that the call is about to change files in a folder
	// they only pointed at — the real ones, not a copy — and which folder it is.
	return approval.Decision{
		Action: approval.ActionPrompt,
		Rule:   "a change in " + folder + " — the folder itself, not a copy",
	}
}

// rememberedFolder is what this conversation has been told about one attached
// folder, and whether it has been told anything at all.
func (a *Agent) rememberedFolder(folder string) (bool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	allow, known := a.folderConsent[folder]
	return allow, known
}

// rememberFolder banks a standing answer about one attached folder.
//
// IT IS WRITTEN INSTEAD OF THE TOOL MEMO AND NEVER BESIDE IT ([Agent.askAnswer]
// holds that branch), for [ConsentToolSession]'s own reason read one level up:
// the memo is keyed by what the QUESTION WAS ABOUT, and a question that named a
// folder was not a question about `edit`. Two records of one answer would drift
// the moment the person was asked about the same tool somewhere else.
func (a *Agent) rememberFolder(folder string, allow bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.folderConsent == nil {
		a.folderConsent = make(map[string]bool, 1)
	}
	a.folderConsent[folder] = allow
}
