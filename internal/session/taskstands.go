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
//  1. SAID — `propose_task{ground}`, the place a person named in their own
//     request, or a place THIS CONVERSATION IS ALREADY ABOUT (places.go).
//     Somebody's own word is never overruled by anything below it, and a folder
//     the conversation named or resolved once is not asked about twice.
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
	"regexp"
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
	// frozen is THE FAMILY'S OWN WORLD, when this stand belongs to a part of one:
	// the commit the parent froze the family tree at when it handed the work out
	// (task_divide_wip.go). The ground ladder cuts from it and does not seal the
	// parent's tree again, so every sibling starts from one world however far
	// apart their working copies were prepared. Empty for every stand that is not
	// a part's, and for a part whose family never froze anything.
	frozen string
	// kept marks a ground that answered at SAID because the CONVERSATION
	// remembered it rather than because anybody said it out loud — a place this
	// conversation resolved once and wrote down (places.go's [PlaceKept]).
	//
	// The one thing that turns on it is [groundLint]: a ground somebody said is
	// never second-guessed, and a cached answer is not somebody saying it. So a
	// brief that names a repository the remembered place is not still re-grounds
	// the work onto it, exactly as it does for every rung below.
	kept bool
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
	//
	// AND IT IS HONORED ONLY WHEN THEY ASKED FOR IT. A non-empty `where` used to
	// skip the tree for any caller, and both doors that fill the field are
	// models: propose_task's optional argument, and the `/task` shaper told to
	// invent `in place` for "non-code work". Either one stood the worker in the
	// conversation checkout — the person's own working copy, when they opened
	// aforge in a repository. The schema already said "never guess"; this is
	// the gate that makes that a fact rather than a prompt.
	spec.where = placementThePersonAskedFor(spec.request, spec.where, workspace)
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
	// A MODE THE PERSON SAID IS NOT RECOMPUTED. Every rung leaves this blank and
	// the mode falls out of the deliverable, which is the law above — except for
	// a referred place carrying the person's own word about how work happens
	// there ([PlaceRef.Mode]), and their word is the one thing this file obeys
	// rather than reads.
	if stand.mode == "" {
		stand.mode = groundMode(stand, spec, workspace)
	}
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
		// AND THE ANSWER IS KEPT, here rather than in the ladder, for the reason
		// [Agent.keepGround] states: a ground is written down where it is
		// CONSUMED, so nothing is remembered out of a reading that a question or
		// a refusal then threw away.
		a.keepGround(stand)
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
		// ONE READING OF THE EVIDENCE, weighed once and handed to both rungs that
		// want it. The walk stats every path this conversation named and asks git
		// about every directory it finds; doing it twice for one answer would
		// double that cost in front of a person waiting on a proposal.
		weights := groundWeights(touchedPaths(a.snapshot()), workspace)
		// AND THE PLACES THIS CONVERSATION IS ABOUT ANSWER AT SAID, above the
		// evidence they were mostly resolved out of. That is the ruling this feed
		// was built under (docs/design/places/DESIGN.md): a conversation-level
		// place is EVIDENCE for the one ladder and never a second resolver, so it
		// arrives here as a said-level answer rather than as a mechanism of its
		// own — and the question that was asked once is not asked again.
		if stand, ok := a.groundFromPlaces(spec, workspace, weights); ok {
			return stand
		}
		if stand, ok := groundFromTouched(weights); ok {
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
func groundFromTouched(weights []groundWeight) (taskStand, bool) {
	if len(weights) == 0 {
		return taskStand{}, false
	}
	leader, second := weights[0], (groundWeight{})
	if len(weights) > 1 {
		second = weights[1]
	}
	if second.root != "" && second.weight*taskGroundRunnerUp >= leader.weight {
		return taskStand{ask: groundAsk("this conversation has been working in two places",
			leader.root, second.root)}, true
	}
	return taskStand{dir: leader.root, rung: taskGroundTouched}, true
}

// groundFromPlaces reads the folders THIS CONVERSATION IS ABOUT (places.go) as
// a said-level answer, and it is the whole of what a referred place does to the
// ladder: no rung of its own, no second resolver, one more source of evidence
// for the one decision [Agent.resolveTaskGround] makes.
//
// THE ORDER INSIDE IT IS FOUR TIERS, and every one of them is the same idea from
// a different distance:
//
//  1. A place the CONTRACT NAMES that the person also named. Work whose brief
//     spells out a path inside a folder the person referred to is work plainly
//     about that folder, and there is nothing left to weigh.
//  2. A place the contract names that the conversation merely kept.
//  3. A place the person NAMED. It never expires: said is never overruled, which
//     is the law this file already keeps about the rung above.
//  4. A place the conversation KEPT and the evidence has not left behind
//     ([placeOutweighed]).
//
// TWO ANSWERS IN ONE TIER ARE STILL THE QUESTION. A conversation about two
// projects has not said which one this work is for any more than a conversation
// that read two of them has — and it is a better question than the touched one,
// because both names are folders the person already knows they are working in.
func (a *Agent) groundFromPlaces(spec taskSpec, workspace string, weights []groundWeight) (taskStand, bool) {
	places := a.referredPlaces()
	if len(places) == 0 {
		return taskStand{}, false
	}
	var namedSaid, namedKept, said, kept []PlaceRef
	for _, place := range places {
		// A PLACE THAT IS NOT THERE IS NOT AN ANSWER, for the reason a `ground`
		// naming a missing folder is refused: work stands somewhere real. The
		// record stays on the meta — a folder on a disk that is unplugged today
		// is a folder again tomorrow — and it is simply not weighed here.
		if info, err := os.Stat(place.Path); err != nil || !info.IsDir() {
			continue
		}
		// AND THE STANDING PLACE IS NOT A REFERRED ONE. The rungs below answer
		// for the folder the conversation is standing in, and letting it in here
		// would move work that was already going somewhere right onto a rung it
		// did not climb.
		if place.Path == workspace {
			continue
		}
		named, isSaid := placeNamedIn(place.Path, spec), place.Arrival == PlaceSaid
		switch {
		case named && isSaid:
			namedSaid = append(namedSaid, place)
		case named:
			namedKept = append(namedKept, place)
		case isSaid:
			said = append(said, place)
		case placeOutweighed(place.Path, weights):
			// A KEPT PLACE THE CONVERSATION HAS MOVED ON FROM. It stays on the
			// meta as history and is out of the running here, so that fresh
			// evidence about where the work actually is beats last hour's answer.
		default:
			kept = append(kept, place)
		}
	}
	for _, tier := range [][]PlaceRef{namedSaid, namedKept, said, kept} {
		switch len(tier) {
		case 0:
		case 1:
			return placeStand(tier[0]), true
		default:
			return taskStand{ask: groundAsk("this conversation is about two places",
				tier[0].Path, tier[1].Path)}, true
		}
	}
	return taskStand{}, false
}

// placeStand is one referred place as an answer: the SAID rung, and the person's
// own word about how work happens there when they said one.
func placeStand(place PlaceRef) taskStand {
	return taskStand{
		dir:  place.Path,
		mode: TaskMode(place.Mode),
		rung: taskGroundSaid,
		kept: place.Arrival == PlaceKept,
	}
}

// placeNamedIn reports whether this contract SPELLS OUT a path inside one
// referred place.
//
// IT READS ONLY ABSOLUTE PATHS, which is the difference between it and
// [groundNamesWorkUnder] and the reason it is a function of its own. That one
// asks whether work would WRITE under a ground and counts a bare `README.md`
// that exists there; this one asks which of several folders the work is ABOUT,
// and a relative name that exists under all of them would make every place look
// equally named and turn an easy answer into a question.
//
// AND IT COMPARES CANONICAL SPELLINGS. A referred place was canonicalized on the
// way in (places.go) while a path in a brief is spelled however the model wrote
// it, and on macOS those two spellings of one directory differ by a `/private`
// nobody typed (place.go's [canonicalPath] states that law).
func placeNamedIn(place string, spec taskSpec) bool {
	for _, token := range pathTokens(spec.brief + "\n" + spec.deliverable + "\n" + spec.acceptance) {
		if !strings.HasPrefix(token, "~") && !filepath.IsAbs(token) {
			continue
		}
		dir := canonicalPath(groundDirOf(token, ""))
		if dir == "" {
			continue
		}
		if _, inside := insideWorkspace(place, dir); inside {
			return true
		}
	}
	return false
}

// placeOutweighed reports whether the evidence has left a kept place behind.
//
// A KEPT PLACE IS A CACHED ANSWER AND DECAYS BY RECENCY, which is the whole of
// how the set stays alive: a conversation that resolved one project an hour ago
// and has spent the last twenty calls in another is about the other one now, and
// a cache that outranked what the person is visibly doing would be the chore
// this design was written to remove, wearing the opposite face.
//
// It decays against the SAME half-law the two-roots question is asked under
// ([taskGroundRunnerUp]), so there is one statement of what "dominates" means
// here. And it is never outweighed by AMBIGUOUS evidence: two roots with real
// weight is exactly the question somebody already answered, and a kept place is
// their answer to it.
func placeOutweighed(path string, weights []groundWeight) bool {
	if len(weights) == 0 {
		return false
	}
	leader := weights[0]
	if leader.root == path {
		return false
	}
	if len(weights) > 1 && weights[1].weight*taskGroundRunnerUp >= leader.weight {
		return false
	}
	own := 0
	for _, weight := range weights {
		if weight.root == path {
			own = weight.weight
			break
		}
	}
	return own*taskGroundRunnerUp < leader.weight
}

// groundAsk is THE ONE QUESTION THIS FILE ASKS, spelled once so the two rungs
// that can reach it cannot drift into two different questions.
//
// It is asked in the two names themselves: a person reading it recognizes their
// own projects, and anything the harness said about "weight" or "evidence" would
// be machinery explaining itself instead of asking. The opening clause is the
// caller's because the two are true of different things — one conversation has
// been WORKING in two places, another IS ABOUT two of them.
func groundAsk(opening, first, second string) string {
	return opening + " — " + first + " and " + second + " — so say which one this task is about"
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
	// AND A GROUND THE CONVERSATION MERELY REMEMBERED IS NOT SOMEBODY SAYING IT.
	// A referred place answers at SAID ([Agent.groundFromPlaces]) so that nobody
	// is asked twice, but a cached answer has no authority over a brief that
	// names a repository — only a person's own word does ([taskStand.kept]).
	if (stand.rung == taskGroundSaid && !stand.kept) ||
		stand.rung == taskGroundNamed || stand.rung == taskGroundHere {
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

// placementThePersonAskedFor is the gate on `where`: the value a model wrote,
// kept only when the PERSON's own request named that placement.
//
// THE FIELD IS A MODEL-CONTROLLED ISOLATION BYPASS. Any non-empty `where`
// short-circuits the ground ladder into [TaskModeInPlace] and stands the
// worker in that directory — the conversation workspace, when the value is
// `in place`, `.`, `here`, or the workspace path itself. Both doors that fill
// it are models, and both have been measured inventing it: the shaper was
// told to use `in place` for "non-code work", and a parent proposing children
// copies the repository path it can see. Neither is the person opting out.
//
// What they actually said is `request` — already the person's words at both
// doors, set before the ground is resolved. A brief the model groomed is not
// read here, because the model that invented `where` will invent the same
// words in the brief.
//
// An in-place token the person did not say is dropped, and so is a path that
// does not appear in their request. What remains is empty, and the ladder
// cuts a worktree when the conversation is standing in a repository.
func placementThePersonAskedFor(request, where, workspace string) string {
	where = strings.TrimSpace(where)
	if where == "" {
		return ""
	}
	workspace = canonicalPath(workspace)
	if isInPlaceWhereToken(where) {
		if personSaidInPlace(request) {
			return "in place"
		}
		return ""
	}
	resolved, err := resolveTaskWhere(where, workspace)
	if err != nil {
		// A path that cannot be resolved is still a claim. Keep it only when
		// they named it, so the door that already refuses a bad folder still
		// refuses one they asked for, and a guess that does not even resolve
		// falls through to the ladder instead of failing the task.
		if requestNamesWhere(request, where, "", workspace) {
			return where
		}
		return ""
	}
	resolved = canonicalPath(resolved)
	if resolved != "" && resolved == workspace {
		// The workspace path is how a model restates "here" without saying
		// `in place`. Honor it only when they said those words or named the
		// path themselves — naming the folder they are standing in is them
		// asking to work in it, and inventing the path is not.
		if personSaidInPlace(request) || requestNamesWhere(request, where, resolved, workspace) {
			return where
		}
		return ""
	}
	if requestNamesWhere(request, where, resolved, workspace) {
		return where
	}
	return ""
}

// dropGuessedWhere clears a `where` the person did not ask for, so the value
// stored on the spec — the card, the notice, the journal — matches the stand
// [Agent.resolveTaskGround] will come to. The resolver also filters its local
// copy; this is the write that reaches the node.
func (a *Agent) dropGuessedWhere(spec *taskSpec) {
	if spec == nil {
		return
	}
	workspace := canonicalPath(strings.TrimSpace(a.config.Workspace))
	spec.where = placementThePersonAskedFor(spec.request, spec.where, workspace)
}

// isInPlaceWhereToken is every spelling that means "the conversation
// workspace itself", not a folder they named. `here` and `directly` are
// [placeModeWord]'s own words; `.` and `./` are the same claim written as a
// path. None of them is a folder to resolve — `here` as a relative path is
// `workspace/here`, which is a different directory and the wrong one.
func isInPlaceWhereToken(where string) bool {
	switch strings.ToLower(strings.TrimSpace(where)) {
	case "in place", "here", "directly", ".", "./":
		return true
	}
	return false
}

// personSaidInPlace reports the one phrase that is the person's opt-out of
// isolation. The word "here" in English is not that phrase — "look here",
// "the crash is here" — and treating it as one used to skip the tree for
// ordinary work.
var inPlacePhrase = regexp.MustCompile(`(?i)\bin\s+place\b`)

func personSaidInPlace(request string) bool {
	return inPlacePhrase.MatchString(request)
}

// requestNamesWhere reports whether the person's request named this directory
// as a path, not as a prose word that happens to match a fragment of one.
func requestNamesWhere(request, where, resolved, workspace string) bool {
	if looksLikeWherePath(where) && pathAppearsInRequest(request, where) {
		return true
	}
	if resolved == "" {
		return false
	}
	for _, token := range pathTokens(request) {
		got, err := resolveTaskWhere(token, workspace)
		if err != nil {
			continue
		}
		if canonicalPath(got) == resolved {
			return true
		}
	}
	return false
}

// looksLikeWherePath is a path somebody could have typed as `where`: absolute,
// home-relative, or with a separator. A bare word is not one — "docs" in
// "update the docs" is not a folder they named.
func looksLikeWherePath(where string) bool {
	where = strings.TrimSpace(where)
	if where == "" || where == "." || where == "./" {
		return false
	}
	return strings.HasPrefix(where, "/") || strings.HasPrefix(where, "~") ||
		strings.HasPrefix(where, "./") || strings.HasPrefix(where, "../") ||
		strings.ContainsRune(where, '/')
}

// pathAppearsInRequest is a bounded search: `/tmp/x` does not name `/tmp/xyz`.
func pathAppearsInRequest(request, path string) bool {
	if path == "" || path == "." || path == "./" {
		return false
	}
	for start := 0; start < len(request); {
		i := strings.Index(request[start:], path)
		if i < 0 {
			return false
		}
		i += start
		if pathBounded(request, i, i+len(path)) {
			return true
		}
		start = i + 1
	}
	return false
}

func pathBounded(s string, start, end int) bool {
	if start > 0 && isPathByte(s[start-1]) {
		return false
	}
	if end < len(s) && isPathByte(s[end]) {
		return false
	}
	return true
}

func isPathByte(b byte) bool {
	return b == '/' || b == '.' || b == '-' || b == '_' || b == '~' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
