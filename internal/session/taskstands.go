package session

// WHERE A TASK STANDS.
//
// A conversation opened in a home directory spent an hour fixing two security
// findings in a repository three levels down, and every piece of machinery
// underneath believed the work was about the home directory. The harness minted
// an empty repository beside the session, cut both workers a worktree from THAT,
// and the brief — written by a model that could see the real project — told them
// to go and work in the person's own checkout instead. So the guard that keeps a
// task off a real branch was guarding a copy nobody was in; two commits landed on
// the person's own branch; and the check that decides whether work is finished
// was run in the empty tree, found nothing, and marked both tasks incomplete
// while their pull requests sat open and correct (issue #76).
//
// Nothing there was a bug in isolation. The parts disagreed about ONE FACT
// nobody had ever written down: which repository or folder the work is about.
// This file is that fact, and it is called the GROUND.
//
// ── THE LADDER ──
//
// It is resolved from evidence the conversation already holds, in this order,
// and the first rung that answers wins:
//
//  1. SAID — `propose_task{ground}`, or the place a person named in their own
//     request. Somebody's own word is never overruled by anything below it.
//  2. TOUCHED — the git roots of every path this conversation's tool calls read,
//     edited, grepped or wrote, and every `cd` a shell command made, weighted by
//     recency. One root that dominates is the ground. TWO WITH REAL WEIGHT ARE A
//     QUESTION, never a coin toss: the caller is handed the two names and asks.
//  3. STANDING IN — the conversation's own workspace when it is a repository.
//     This is what every task got before this file existed, and a session opened
//     inside the project it is about still gets exactly it.
//  4. NOTHING — the conversation's own folder, with no repository anywhere. The
//     work happens there because there is nowhere else it could be about.
//
// ── AND THE MODE FALLS OUT OF THE DELIVERABLE ──
//
// Nobody is asked "worktree or copy?", because that is a question about
// machinery and the answer is already in what the work must leave behind
// ([TaskMode] spells the five out). A repository the work writes in gets a
// branch off its HEAD; a repository it only reads stays read-only and the work
// gets a folder; a plain folder it writes in is mirrored and landed back by
// name; and "here" is the person saying they want it in their own tree.
//
// ── THE NAME ──
//
// taskground.go is about the ground MOVING while a run is out — somebody else
// landing work in the files this node is writing. This file is about the ground
// a task STANDS ON. Neither name is spare, so the two live apart and this
// paragraph is the signpost between them.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// taskStand is what the ladder came to: the ground, how this task stands on it,
// and — when the evidence would not settle — what has to be asked instead.
//
// EXACTLY ONE OF dir, ask AND refusal IS EVER SET. A stand with a question is
// not a stand with a guess in it as well, because the guess would be what
// actually ran the moment a caller forgot to look.
type taskStand struct {
	// dir is the absolute path the work stands in: a repository root, or a
	// folder.
	dir string
	// mode is how the task stands on it.
	mode TaskMode
	// rung names which step of the ladder answered, in the words above. It is
	// written to the job log and read by nobody else — it is how a person
	// reading a log finds out why the work went where it went.
	rung string
	// ask is the one question the evidence could not answer, and "" whenever it
	// could. The caller puts it to the person through the road it already has
	// for a proposal nobody can start yet; nothing about this file starts work
	// on an ambiguity.
	ask string
	// refusal is an honest sentence about work that cannot be placed at all.
	refusal string
}

// The rungs, spelled once so a log line and a test cannot disagree about them.
const (
	taskGroundSaid       = "said"
	taskGroundTouched    = "touched"
	taskGroundStandingIn = "standing in"
	taskGroundNothing    = "nothing"
	// taskGroundHere and taskGroundNamed are the two the `where` argument
	// answers before the ladder is climbed at all: a person who said "in place"
	// or named a directory has said where the work happens, which settles where
	// it stands as well.
	taskGroundHere  = "here"
	taskGroundNamed = "named"
)

// taskGroundRunnerUp is how much of the leader's weight the second root has to
// carry before the evidence counts as two answers rather than one.
//
// It is a HALF, and the direction it errs in is deliberate. A conversation that
// spent nine calls in one repository and one in another has plainly been working
// in the first; a conversation that split its attention down the middle has not
// told anybody anything, and asking costs one keypress while guessing costs an
// hour of work done in the wrong project.
const taskGroundRunnerUp = 2

// taskGroundPathsRead is how far back through a conversation the touched rung
// looks. It is generous — a ground is resolved once per proposal, and the walk
// is over a slice this process already holds — and it is bounded at all so that
// a very long session does not pay for a thousand path resolutions.
const taskGroundPathsRead = 400

// resolveTaskGround climbs the ladder for one proposal.
//
// IT IS THE ONLY PLACE THE GROUND IS DECIDED, and every door that admits a task
// calls it: the model's propose_task, a person's own `/task`, the route judge's
// card. A second reading of the same evidence somewhere else would be a second
// answer to the one question this file exists to have one answer to.
func (a *Agent) resolveTaskGround(spec taskSpec) taskStand {
	workspace := canonicalPath(strings.TrimSpace(a.config.Workspace))
	// THE PERSON'S OWN PLACEMENT COMES FIRST, and it is not a rung of the ladder
	// — it is the whole ladder skipped. `where` says where the work happens, and
	// work that happens in a named directory is work about that directory.
	if where := strings.TrimSpace(spec.where); where != "" {
		if strings.EqualFold(where, "in place") {
			return taskStand{dir: workspace, mode: TaskModeInPlace, rung: taskGroundHere}
		}
		dir, err := resolveTaskWhere(where, workspace)
		if err != nil {
			return taskStand{refusal: "this task names a folder it cannot work in: " + where}
		}
		return taskStand{dir: dir, mode: TaskModeInPlace, rung: taskGroundNamed}
	}
	stand := a.groundLadder(spec, workspace)
	if stand.ask != "" || stand.refusal != "" {
		return stand
	}
	// THE BRIEF IS READ LAST, and it may still move the ground. A model that has
	// been told to name the repository will name it in prose long before anybody
	// thinks to fill in an argument, and the first task this whole design was
	// written about said `Repo: ~/…/agentfield` in its brief while the harness
	// stood somewhere else entirely.
	if reground, refusal := groundLint(stand, spec); refusal != "" {
		return taskStand{refusal: refusal}
	} else if reground != "" {
		stand.dir, stand.rung = reground, taskGroundSaid
	}
	stand.mode = groundMode(stand, spec, workspace)
	return stand
}

// taskGroundOrStandingIn is [Agent.resolveTaskGround] for a door with NOBODY TO
// ASK: a person's own `/task`, the route judge's card. Both hand a node straight
// to the graph and have no road back to anybody, so a question or a refusal
// there would be a task that silently never started.
//
// It takes the rung below instead, which is the workspace the conversation is
// standing in — today's answer, unchanged, and never an invention. The question
// is asked where there is somebody to answer it: propose_task, whose result goes
// back to a model that is mid-conversation with the person.
func (a *Agent) taskGroundOrStandingIn(spec taskSpec) taskStand {
	stand := a.resolveTaskGround(spec)
	if stand.ask == "" && stand.refusal == "" {
		return stand
	}
	workspace := canonicalPath(strings.TrimSpace(a.config.Workspace))
	if root, ok := repositoryRoot(workspace); ok {
		return taskStand{dir: root, mode: TaskModeWorktree, rung: taskGroundStandingIn}
	}
	return taskStand{dir: workspace, mode: TaskModeFolder, rung: taskGroundNothing}
}

// groundLadder is rungs one to four, with the person's own placement already
// answered for.
func (a *Agent) groundLadder(spec taskSpec, workspace string) taskStand {
	if said := strings.TrimSpace(spec.ground); said != "" {
		dir, err := resolveTaskWhere(said, workspace)
		if err != nil {
			return taskStand{refusal: "this task names a folder it cannot work in: " + said}
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			// A ground is a place that IS there. Unlike `where`, which is somebody
			// saying where work should go and may name a folder to be made, this
			// argument names the project the work is about — and a project nobody
			// can find is a mistake worth saying out loud rather than creating.
			return taskStand{refusal: "this task names a folder that is not there: " + dir}
		}
		return taskStand{dir: groundRoot(dir), rung: taskGroundSaid}
	}
	// A PART STANDS WHERE ITS PARENT STANDS, and the rungs below are not climbed
	// for it. A sub-task's branch is cut from its parent's worktree and merges
	// back into it (task_run.go's [TaskNode.owner]), so a part re-grounded on
	// something its own reading of the evidence liked better is a part whose work
	// can never come home.
	if spec.parent == 0 {
		if stand, ok := a.groundFromTouched(workspace); ok {
			return stand
		}
	}
	if root, ok := repositoryRoot(workspace); ok {
		return taskStand{dir: root, rung: taskGroundStandingIn}
	}
	return taskStand{dir: workspace, rung: taskGroundNothing}
}

// groundFromTouched weighs the repositories this conversation has actually been
// working in. The second answer reports false, and the caller climbs on.
func (a *Agent) groundFromTouched(workspace string) (taskStand, bool) {
	weights := groundWeights(touchedPaths(a.snapshot()), workspace)
	if len(weights) == 0 {
		return taskStand{}, false
	}
	leader, second := weights[0], (groundWeight{})
	if len(weights) > 1 {
		second = weights[1]
	}
	if second.root != "" && second.weight*taskGroundRunnerUp >= leader.weight {
		// THE ONE QUESTION THIS FILE ASKS, and it is asked in the two names
		// themselves: a person reading it recognizes their own projects, and
		// anything the harness said about "weight" or "evidence" would be
		// machinery explaining itself instead of asking.
		return taskStand{ask: "this conversation has been working in two places — " +
			leader.root + " and " + second.root +
			" — so say which one this task is about"}, true
	}
	return taskStand{dir: leader.root, rung: taskGroundTouched}, true
}

// groundWeight is one repository and how much of this conversation happened in
// it, heaviest first.
type groundWeight struct {
	root   string
	weight int
}

// groundWeights turns paths into repositories, WEIGHTED BY RECENCY: the newest
// path in the conversation counts for as much as the whole first half of it.
//
// The weight is the path's own position, which is the cheapest honest reading of
// "lately" there is — no clock, no decay constant to tune, and a conversation
// that changed project halfway through swings within a handful of calls.
func groundWeights(paths []string, workspace string) []groundWeight {
	roots := make(map[string]int, 4)
	// A directory is asked of git ONCE. A conversation reads forty files out of
	// one folder, and forty `rev-parse` processes to learn one fact is the kind
	// of cost that turns a proposal into a pause.
	known := make(map[string]string, 8)
	for index, path := range paths {
		dir := groundDirOf(path, workspace)
		if dir == "" {
			continue
		}
		root, asked := known[dir]
		if !asked {
			if found, ok := repositoryRoot(dir); ok {
				root = found
			}
			known[dir] = root
		}
		if root == "" {
			// A PATH IN NO REPOSITORY SAYS NOTHING HERE. Reading a note in a home
			// directory is not evidence about which project a task is for, and
			// counting it would make the home directory win every conversation that
			// opened one file in it.
			continue
		}
		roots[root] += index + 1
	}
	out := make([]groundWeight, 0, len(roots))
	for root, weight := range roots {
		out = append(out, groundWeight{root: root, weight: weight})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].weight != out[j].weight {
			return out[i].weight > out[j].weight
		}
		return out[i].root < out[j].root
	})
	return out
}

// groundDirOf is the directory a touched path belongs to, absolute: the path
// itself when it is one, and its parent otherwise. A relative path is read
// against the workspace, which is the directory the tool call ran in.
func groundDirOf(path, workspace string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), string(filepath.Separator)))
	}
	if !filepath.IsAbs(path) {
		if workspace == "" {
			return ""
		}
		path = filepath.Join(workspace, path)
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return filepath.Clean(path)
	}
	return filepath.Dir(filepath.Clean(path))
}

// touchedPaths is every path this conversation's tool calls named, oldest
// first.
//
// IT DERIVES FROM THE TRANSCRIPT AND KEEPS NO BOOK OF ITS OWN. The calls are
// already there, whole, with their arguments — the same record [lastToolReceipts]
// reads for the auditor — and a second ledger written alongside them would be a
// second account of one fact, drifting from the moment the first tool grew an
// argument nobody updated it about.
//
// THE ARGUMENT IS ALWAYS SPELLED `path`, on every hand that takes one: read,
// write, edit, grep, find and ls (internal/exec/bare's schemas). So this asks
// for that one key rather than carrying a list of tools, and a hand added later
// that spells its file argument the same way is counted without anybody coming
// back here. A shell command is the exception and is read for the one thing it
// says about place: where it changed directory to.
func touchedPaths(messages []ai.Message) []string {
	var out []string
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			var fields struct {
				Path    string `json:"path"`
				Command string `json:"command"`
			}
			if json.Unmarshal([]byte(call.Function.Arguments), &fields) != nil {
				continue
			}
			if path := strings.TrimSpace(fields.Path); path != "" {
				out = append(out, path)
			}
			out = append(out, changedDirectories(fields.Command)...)
		}
	}
	if len(out) > taskGroundPathsRead {
		out = out[len(out)-taskGroundPathsRead:]
	}
	return out
}

// changedDirectories is every place a shell command went. `cd` is the whole of
// what a command line says about where work is happening that can be read
// without running it — the rest of a command's effects are whatever it did, and
// recovery.go states that law for the ledger.
func changedDirectories(command string) []string {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	var out []string
	for _, piece := range strings.FieldsFunc(command, func(r rune) bool {
		return r == ';' || r == '\n' || r == '&' || r == '|'
	}) {
		fields := strings.Fields(strings.TrimSpace(piece))
		if len(fields) < 2 || fields[0] != "cd" {
			continue
		}
		if word := strings.Trim(fields[1], `"'`); word != "" && word != "-" {
			out = append(out, word)
		}
	}
	return out
}

// groundRoot snaps a path to the repository it belongs to, because a branch is
// cut from a repository and not from a directory inside one. A path in no
// repository is its own ground, which is what a plain folder is.
func groundRoot(dir string) string {
	if root, ok := repositoryRoot(dir); ok {
		return root
	}
	return canonicalPath(dir)
}

// groundMode reads the mode off the deliverable, per [TaskMode].
//
// STANDING IN IS ALWAYS A BRANCH. A conversation opened inside its own project
// gets exactly what it got before this file existed, whatever its deliverable
// looks like — this design came to place work that was going somewhere WRONG,
// and it may not quietly take a worktree away from work that was already going
// somewhere right.
func groundMode(stand taskStand, spec taskSpec, workspace string) TaskMode {
	_, isRepo := repositoryRoot(stand.dir)
	writes := groundNamesWorkUnder(stand.dir, spec)
	switch {
	case isRepo && (stand.rung == taskGroundStandingIn || writes):
		return TaskModeWorktree
	case isRepo:
		return TaskModeReference
	case stand.rung == taskGroundNothing || stand.dir == workspace:
		return TaskModeFolder
	case writes:
		return TaskModeMirror
	}
	return TaskModeReference
}

// groundNamesWorkUnder reports whether this task's contract names a file under
// the ground at all — anywhere in the brief, the deliverable or the acceptance.
//
// WRITING IS THE DEFAULT AND READING IS THE NARROW CASE, and the bar is set here
// rather than in the mode's own switch. A contract that names a file is a
// contract that may well write one, whichever of the three sentences names it: a
// deliverable often says "the file, at the path named in the brief", and a mode
// read off the deliverable alone gave that task a folder to write in and a
// repository it could never merge into. What is left for reference is work whose
// whole contract names no file — a question about a project, an answer that
// comes back in a report — which is exactly the work that should not be cutting
// branches off somebody's repository.
func groundNamesWorkUnder(ground string, spec taskSpec) bool {
	ground = canonicalPath(ground)
	if ground == "" {
		return false
	}
	for _, token := range pathTokens(spec.brief + "\n" + spec.deliverable + "\n" + spec.acceptance) {
		if groundHolds(ground, token) {
			return true
		}
	}
	return false
}

// groundHolds reports whether one written path lands under the ground. An
// absolute path is compared as it stands; a relative one is a name inside the
// project and counts when the file or the directory that would hold it is really
// there, which keeps ordinary prose from reading as a path.
func groundHolds(ground, token string) bool {
	if strings.HasPrefix(token, "~") || filepath.IsAbs(token) {
		full := groundDirOf(token, "")
		if full == "" {
			return false
		}
		_, inside := insideWorkspace(ground, full)
		return inside
	}
	full := filepath.Join(ground, filepath.FromSlash(token))
	if _, err := os.Stat(full); err == nil {
		return true
	}
	info, err := os.Stat(filepath.Dir(full))
	return err == nil && info.IsDir()
}

// groundLint holds the brief up against the ground, and it is the second half of
// the law this file states: A TASK NEVER WRITES OUTSIDE ITS GROUND.
//
// An absolute path in the contract that sits outside the ground is one of two
// things, and they are answered differently:
//
//   - IT IS IN A REPOSITORY. Then the brief knows something the ladder did not,
//     and the work re-grounds onto it. This is the shape the whole design came
//     from: a brief that said `Repo: ~/…/agentfield · work in this repo
//     directly` while the harness had cut a worktree somewhere else.
//   - IT IS IN NO REPOSITORY, AND THE DELIVERABLE NAMES IT. Then the task is
//     asking to leave its work somewhere it does not stand, and it is refused in
//     one sentence rather than started and guarded to death.
//
// A path only the BRIEF names, in no repository, is left alone: briefs quote
// interpreters, log files and system directories constantly, and refusing work
// over `/usr/bin/python3` would be a lint that people learn to write around.
// What stops a write there is the guard, which is a different lane and a
// different law.
//
// A GROUND SOMEBODY SAID OUT LOUD IS NOT SECOND-GUESSED AT ALL — not moved, and
// not refused either. A person who named a directory with `where`, or a model
// that filled in `ground` because it knew, has answered this question already,
// and a lint that turned their answer back over a path in a paragraph would be
// the harness overruling the one source it is meant to obey.
//
// AND NEITHER IS A PART. A sub-task's branch is cut from its parent's worktree
// and merges back into it, so a part re-grounded onto a repository named in its
// brief is a part whose work can never come home — the same law [groundLadder]
// keeps the touched rung away from parts for.
func groundLint(stand taskStand, spec taskSpec) (string, string) {
	if stand.dir == "" || spec.parent != 0 {
		return "", ""
	}
	if stand.rung == taskGroundSaid || stand.rung == taskGroundNamed || stand.rung == taskGroundHere {
		return "", ""
	}
	var outside []string
	for _, token := range pathTokens(spec.brief + "\n" + spec.deliverable + "\n" + spec.acceptance) {
		if !strings.HasPrefix(token, "~") && !filepath.IsAbs(token) {
			continue
		}
		if groundHolds(stand.dir, token) {
			continue
		}
		outside = append(outside, token)
	}
	roots := map[string]bool{}
	for _, token := range outside {
		dir := groundDirOf(token, "")
		if dir == "" {
			continue
		}
		if root, ok := repositoryRoot(dir); ok {
			roots[root] = true
		}
	}
	switch len(roots) {
	case 0:
	case 1:
		for root := range roots {
			return root, ""
		}
	default:
		return "", "this task names folders it does not stand in: " + strings.Join(sortedKeys(roots), ", ")
	}
	for _, token := range pathTokens(spec.deliverable + "\n" + spec.acceptance) {
		if !strings.HasPrefix(token, "~") && !filepath.IsAbs(token) {
			continue
		}
		if !groundHolds(stand.dir, token) {
			return "", "this task names a folder it does not stand in: " + token
		}
	}
	return "", ""
}

// sortedKeys is a stable spelling of a set, so a sentence a person reads and a
// sentence a test reads are the same sentence.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// pathTokens picks the things in a piece of prose that could be paths: a word
// with a separator in it, or one that ends in an extension. Everything around it
// — quotes, backticks, brackets, the full stop that ended the sentence — is
// trimmed off.
//
// IT IS A READING AND NOT A PARSER, and every caller treats it as one: a token
// only ever matters here when it also turns out to exist under a directory, or
// to be absolute. A prose word that happens to look like a path costs nothing.
func pathTokens(text string) []string {
	var out []string
	seen := make(map[string]bool, 8)
	for _, raw := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ',' || r == ';' ||
			r == '`' || r == '"' || r == '\'' || r == '(' || r == ')' || r == '[' || r == ']' ||
			r == '<' || r == '>' || r == '{' || r == '}'
	}) {
		token := strings.Trim(raw, ".:")
		if token == "" || !looksLikePath(token) {
			continue
		}
		if seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

// looksLikePath is the one judgement pathTokens makes: a separator, or a name
// with a suffix on it.
func looksLikePath(token string) bool {
	if strings.ContainsRune(token, '/') {
		return true
	}
	dot := strings.LastIndex(token, ".")
	return dot > 0 && dot < len(token)-1 && !strings.ContainsAny(token, " =")
}
