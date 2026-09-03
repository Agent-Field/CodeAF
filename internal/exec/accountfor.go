package exec

// The leaf's own account of the change it made, taken for EVERY belt rather
// than for one of them.
//
// This is photograph.go's history repeating, one field over. [Account] was
// written to end a measured, expensive silence — a worker that could only hand
// back a sentence and a bill, and three readers downstream forced to act on the
// void when that sentence said nothing — and then the only thing in the tree
// that ever WROTE one lived beside a single belt. That belt was removed in
// 05b99537, and with it went the only assignment to Outcome.Account anywhere.
// Nothing noticed for a hundred commits, because every reader of an account is
// written to treat nil as "no claim": the plan's state view recorded an empty
// clause, the delivery gate weighed evidence with no change set and no diff in
// it, the composer's Landed() && Verified() answered no for every run in the
// world, and the bank was handed nothing to report. Each of them was quietly,
// correctly reporting that nobody had looked, forever.
//
// So the accounting is taken at THE ONE SEAM EVERY BELT LANDS THROUGH, which is
// PhotographAfter, and it is taken from sources every belt already has: the
// workspace's own before-and-after read of the tree, the repository underneath
// it, and the photograph's own second reading. It needs no subharness, no
// process boundary and no narration on a wire — a leaf that wrote one file with
// a shell command now accounts for it exactly as a pipeline behind a socket did.
//
// Everything here is a MEASUREMENT and never a gate, on photograph.go's own
// rule. It never fails a leaf, it never changes what a loop does, and every way
// it can go wrong — no git, no repository, a command that will not start, a
// scratch directory that cannot be written — leaves the outcome exactly as it
// would have been, holding a smaller claim rather than a false one.
//
// Two of those words are load-bearing and were only aspirations when this file
// was first written, which is worth saying plainly because a law stated in a
// comment and not met in the code is worse than no law at all.
//
// IT CANNOT BLOCK PAST THE LEAF. It runs from a defer on the leaf's own landing,
// so a git that hangs holds the whole run open. Every command is started on the
// leaf's context and every diff is run with `--no-ext-diff`: a repository can
// configure an external diff driver, per-repository or per-path, and a helper
// this program never named is not a helper cancelling a context reliably reaps.
//
// AND IT CANNOT PANIC. A nil context reaches os/exec as a crash, from a
// measurement whose whole contract is that it cannot change the outcome, so it
// is checked rather than assumed — every caller here is a defer, and a defer is
// exactly where a caller's mistake becomes this file's crash.

import (
	"context"
	"errors"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// AccountFor fills in the leaf's account of its own work, where there is
// anything to account for.
//
// It is called from exactly one place — the end of [PhotographAfter]'s body,
// through a defer, so that it runs on every one of that function's exits and no
// belt has to remember it. That is the whole structural point of the repair and
// account_writer_test.go is what keeps it true: a measurement wired into one
// belt is a measurement that leaves with that belt.
//
// base is where the repository's history stood when the tree was photographed,
// carried from [PhotographBefore] on the [Opening]. It cannot be re-derived
// here — by the time a leaf lands, HEAD is wherever the leaf left it — and it is
// empty for every root that is not a git work tree, which is the honest "no
// claim" and never "measured, and nothing moved".
//
// secondReading is the seam's own answer to whether THIS leaf's reading of the
// finished tree was taken, and it is the retake law read a second time rather
// than a fact of its own. It decides the checks and nothing else: the files, the
// span and the patch are what the tree says, and the tree says it whether or not
// a suite ran.
//
// A RUN WITH NOTHING TO ACCOUNT FOR LEAVES THE ACCOUNT NIL. A leaf that changed
// no file and took no reading has made no claim, and nil is how [Account] spells
// that everywhere it is rendered; an empty account attached anyway would turn
// "nobody looked" into "we looked and there was nothing", which is the one
// substitution the emptiness law exists to forbid.
func AccountFor(
	ctx context.Context, workspace *Workspace, task Task,
	base string, secondReading bool, outcome *Outcome,
) {
	if outcome == nil || workspace == nil {
		return
	}
	leaf := task.leafKey()
	files, checks := changedFiles(workspace, leaf), ranChecks(outcome, secondReading)
	if len(files) == 0 && len(checks) == 0 {
		return
	}
	account := outcome.Account
	if account == nil {
		account = &Account{}
		outcome.Account = account
	}
	if len(checks) > 0 {
		account.Checks = checks
	}
	// The leaf's last word for the whole job, in its own words — trimmed of the
	// blank lines around it and edited in no other way, which is what every
	// reader of the field does to it anyway. It is the same string the
	// deliverable opens with, which is why [Account.Report] leaves it out and
	// only the gate's copy carries it: the gate is routinely judging a
	// deliverable that has been composed since, and the sentence the worker
	// signed off with may no longer be anywhere in it.
	account.Final = strings.TrimSpace(outcome.Text)
	// Withheld stays empty, and that is an answer rather than an omission. It
	// described work sitting on a private branch inside a checkout only the
	// removed belt ever made; every belt that lands through this seam works in
	// the shared tree, so the change set is exactly where anyone can open it and
	// [Account.Landed] may say so.
	if len(files) == 0 {
		return
	}
	root := workspace.Root()
	if head := gitHead(ctx, root); head != "" {
		// Sizes, span and text all come off the same repository in the same
		// breath, because they are three views of one change set and deriving
		// them apart is how two answers to one question get written down.
		measureLines(ctx, root, base, head, files)
		account.Range = Range{Base: base, Head: head}
		account.Patch = recordPatch(ctx, workspace, leaf, base, head, files)
	}
	// SetFiles rather than Note, per its own comment: this is a derivation and
	// it is already the whole answer, so it replaces whatever narration had
	// said rather than merging into it and doubling every line the two agree on.
	account.SetFiles(files)
}

// changedFiles is the change set as the WORLD reports it: every path this leaf
// claimed through a write tool and every path the tree was seen to gain, lose or
// move under it, which is [Workspace.ArtifactFacts] exactly.
//
// It is the workspace's answer rather than a git one on purpose. Every belt
// takes this reading already — WatchTree at the top, RecordChanges at landing —
// so it is the one account of a change set that exists whether or not there is a
// repository underneath, and it is the same list [Workspace.Artifacts] hands
// downstream. Two lists of "what this leaf changed", derived two ways, is two
// answers to one question.
func changedFiles(workspace *Workspace, leaf string) []FileChange {
	facts := workspace.ArtifactFacts(leaf)
	files := make([]FileChange, 0, len(facts))
	for _, fact := range facts {
		files = append(files, FileChange{Path: fact.Path, Change: changeWord(fact.Change)})
	}
	return files
}

// changeWord is the tree diff's own answer in the account's vocabulary. The
// empty answer belongs to a path only a write tool ever mentioned — a file
// rewritten inside one coarse filesystem second at exactly its old length is
// invisible to the diff and perfectly visible to the tool that wrote it — and
// "changed" is what is known about it, which is what [Account.Note] would have
// defaulted it to anyway.
func changeWord(change ArtifactChange) string {
	switch change {
	case ArtifactCreated:
		return ChangeAdded
	case ArtifactDeleted:
		return ChangeDeleted
	}
	return ChangeChanged
}

// ranChecks is the photograph's second reading, said in the account's own
// vocabulary: ONE command, because one command is what was run.
//
// [Check] is documented as one command the verifier ran, and Known as a check
// that "came back red and was red in exactly the same places before the work
// began" — the places being the individual tests the runner named. So the row
// is the command and the subtraction is over the two failure lists, which is the
// question a person re-running the suite is actually asking. Writing one row per
// test identity instead would put a suite's whole roster — 922 checks, in ink's
// case — into [Account.Summary]'s one clause and every context it travels
// through, for no answer that this does not already give.
//
// A READING THAT NAMED NO CHECK IS NOT A GREEN SUITE. A command that started,
// exited cleanly and reported nothing this reader could name is an incomplete
// observation, and letting it through would make [Account.Verified] answer yes
// off a roster of nothing — which is the exact silence Verified was written to
// stop collapsing.
//
// AND NEITHER IS A ROSTER THIS LEAF INHERITED. Since #460 a leaf that changed
// nothing in a tree its job had not moved keeps the reading it is holding — the
// suite would be run again over the identical bytes for the identical roster, so
// it is not run — and the photograph is right to stand it as the reading of the
// finished tree. It is not right as an ACCOUNT: the roster is a fact about the
// tree and the account is this leaf's claim about its own work, and read as the
// second, a leaf that ran no test at all comes back Verified. That is the
// collapse [Account.Verified]'s own doc forbids four lines above the function,
// and it now has a person-facing receipt in front of it — revision's "checked by
// tests, coverage not measured" — so it is worse than a silent wrong answer.
//
// Both facts stand and they are different facts, which is the shape Range and
// Patch already have outside a git work tree: the honest answer is no claim, not
// a borrowed one. The roster still rides outcome.Verification for every reader
// that is asking about the TREE.
func ranChecks(outcome *Outcome, secondReading bool) []Check {
	reading := outcome.Verification
	after := reading.After
	if !secondReading || !reading.AfterTaken || len(after.Reported) == 0 {
		return nil
	}
	command := strings.TrimSpace(after.Strategy.Command)
	if command == "" {
		command = strings.TrimSpace(reading.Strategy.Command)
	}
	if command == "" {
		return nil
	}
	// Kind is left empty. The engine that filled it had several kinds of check
	// to tell apart — test, lint, build; the photograph takes exactly one kind
	// of reading, nothing renders the field, and a word invented here to fill it
	// would be a fact nobody measured.
	check := Check{Command: command, Passed: len(after.Failing) == 0}
	if !check.Passed {
		// KNOWN MAY ONLY BE CLAIMED OFF A BASELINE SOMEBODY ACTUALLY READ. A
		// reading nobody took names nothing failing and a CUT one stops where
		// the clock did, and against either of those the subset below is
		// trivially satisfiable — a suite red now, red in a stale roster of one,
		// comes back settled and [Account.Verified] answers TRUE. That is the
		// emptiness law inverted: nobody looked is not everything passed, and it
		// is the exact substitution that gets correct work thrown away in the
		// other direction. So the three clauses verify.Reading.comparable spends
		// on the before half are spent here too, and anywhere they do not hold
		// the honest answer is that this red is unaccounted for.
		baseline := reading.Taken && !reading.Partial && !reading.Before.Uncollected
		check.Known = baseline && failedBefore(reading.Before.Failing, after.Failing)
		// Bounded by describeChecks, which is the same eight-and-a-count rule
		// the outcome's baseline sentence uses, spelled once.
		check.Tail = describeChecks(after.Failing)
	}
	return []Check{check}
}

// failedBefore reports that every check red now was red before the work began —
// "red in exactly the same places", which is [Check.Known] verbatim.
//
// It reads the two failure lists and nothing else. Whether there was a baseline
// worth reading them against is its caller's question and is asked there, on the
// same three clauses verify.Reading.comparable spends: this answers only the
// arithmetic. A check the run WROTE and left red is unaccounted for by that
// arithmetic, which is right — a leaf whose own new checks are red has not
// finished.
func failedBefore(before, now []string) bool {
	if len(now) == 0 {
		return false
	}
	already := make(map[string]bool, len(before))
	for _, name := range before {
		already[strings.TrimSpace(name)] = true
	}
	for _, name := range now {
		if !already[strings.TrimSpace(name)] {
			return false
		}
	}
	return true
}

// measureLines fills in the sizes, which come from git or do not come at all.
//
// A COUNT NOBODY MEASURED IS NOT A NUMBER THIS MAY INVENT. Where the root is not
// a work tree, or the path is one git has never seen, or the reading could not
// be taken, the row keeps its zeroes and [Account.FileLines] renders "+0 -0" — a
// path that is named, with a size that was not measured, which is a smaller
// claim than a fabricated one.
func measureLines(ctx context.Context, root, base, head string, files []FileChange) {
	sizes := map[string][2]int{}
	// What this leaf's job committed, then what is still sitting uncommitted on
	// top of it. The second half is not belt-and-braces: a leaf that was
	// cancelled, deadlined or died has whatever it was in the middle of writing,
	// and an account that said "nothing" over a tree full of its own edits is
	// the original defect wearing a different hat.
	if base != "" && base != head {
		readNumstat(ctx, root, sizes, base, head)
	}
	readNumstat(ctx, root, sizes, "HEAD")
	for index := range files {
		if size, measured := sizes[files[index].Path]; measured {
			files[index].Added, files[index].Removed = size[0], size[1]
		}
	}
}

// readNumstat reads one `git diff --numstat` into the size table, keyed by the
// spelling the rest of this program uses.
//
// It carries no pathspec and filters on the way in instead, which is what keeps
// it out of [recordPatch]'s argv problem: a size table is cheap to over-read and
// the rows nobody asked for are simply never looked up.
//
// `--relative` is what makes the two spellings one. git speaks in paths relative
// to the repository's top and the workspace may be a directory inside it, so
// without it a monorepo leaf's rows would be keyed by paths no reader of this
// job recognises — and it also drops the siblings' files outright, which is the
// second half of the same fix: a leaf working in one directory of a repository
// must not claim what changed in another.
//
// Records are NUL-separated so a path is whatever git says it is rather than
// whatever survives a split on newlines, and nothing here trims one: a path may
// legally begin or end with a space, and a key this program tidied is a key that
// matches nothing the tree diff filed. A rename writes an empty third column and
// then the two names; the one that matters is the last of them.
func readNumstat(ctx context.Context, root string, sizes map[string][2]int, revisions ...string) {
	body, taken := runGit(ctx, root, append(
		[]string{"diff", "-z", "--numstat", "-M", "--relative", "--no-ext-diff"}, revisions...)...)
	if !taken {
		return
	}
	fields := strings.Split(body, "\x00")
	for index := 0; index < len(fields); index++ {
		columns := strings.Split(fields[index], "\t")
		if len(columns) < 3 {
			continue
		}
		path := columns[2]
		if path == "" {
			// A rename: the old and new names follow as their own fields, and
			// the new one is what the account names.
			if index+2 >= len(fields) {
				return
			}
			path, index = fields[index+2], index+2
		}
		added, _ := strconv.Atoi(columns[0])
		removed, _ := strconv.Atoi(columns[1])
		if path != "" {
			sizes[path] = [2]int{added, removed}
		}
	}
}

// accountPatchBytes is the largest change set this may write out in full, and
// accountPathspecBytes is the largest run of paths one `git diff` may be handed
// at once. See PERF.md, "What the change set's own text costs".
const (
	accountPatchBytes    = 1 << 20
	accountPathspecBytes = 96 << 10
)

// recordPatch writes the change set's own text where a reader can open it, and
// returns the handle.
//
// The text is the half of the record nothing downstream ever had. A gate handed
// a list of paths can settle whether a file exists; it cannot settle whether the
// deliverable's account of WHY those lines changed is true, and a method writer
// handed paths alone is left to infer the reason — which is exactly how a
// contract's illustrative example of a root cause was once shipped verbatim as a
// real one. A path to the diff costs one line in every context and carries the
// whole change to any reader willing to open it.
//
// It goes under the harness's own directory rather than into the workspace,
// because a patch file written beside the work would be part of the next diff
// and an auditor would — correctly — refuse to ship it. It is recorded as
// INTERNAL for the other half of that sentence: it is evidence about the
// deliverable and never a deliverable, so it must not turn up in the list of
// files the work produced.
//
// Three sources, and they are limited to the account's own paths. The committed
// range and the uncommitted edits are both read over the shared tree, where a
// sibling that landed in between is in the same history; a patch that swept that
// up would hand every reader another node's work as this node's. The paths are
// already the world's own answer to what THIS leaf touched, so they are the
// pathspec.
//
// A SOURCE THAT COULD NOT BE READ ABANDONS THE PATCH, and that is the difference
// between no claim and a false one. A patch missing its untracked half is not a
// smaller patch; it is a patch that reads as a complete account of a change set
// and is silent about the files the leaf created. Empty is what this returns
// there, and empty is what every reader already treats as nobody having looked.
func recordPatch(ctx context.Context, workspace *Workspace, leaf, base, head string, files []FileChange) string {
	root := workspace.Root()
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	var body strings.Builder
	take := func(text string, taken bool) bool {
		if taken {
			body.WriteString(text)
		}
		return taken
	}
	if base != "" && base != head {
		if !take(gitDiff(ctx, root, paths, base, head)) {
			return ""
		}
	}
	if !take(gitDiff(ctx, root, paths, "HEAD")) {
		return ""
	}
	// And the files git has never seen, which no diff between revisions can
	// reach. A newly created file is most of what many leaves do, and an account
	// whose patch was silent about it would be a patch of everything except the
	// work.
	fresh, listed := untracked(ctx, root, paths)
	if !listed {
		return ""
	}
	for _, path := range fresh {
		if !take(runGit(ctx, root, "diff", "--no-index", "--no-ext-diff", "--", os.DevNull, path)) {
			return ""
		}
	}
	text := clipPatch(body.String())
	if strings.TrimSpace(text) == "" {
		return ""
	}
	full, _, err := workspace.ScratchPath(patchName(leaf))
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		return ""
	}
	workspace.RecordInternal(leaf, full)
	return full
}

// clipPatch bounds the change set's own text at accountPatchBytes, and SAYS SO
// WHERE IT CUT.
//
// The bound exists because nothing else here has one: a refactor across a
// thousand files is a real outcome and its diff is a real number of megabytes,
// written to disk on the leaf's landing path and opened afterwards by a judge
// that is bounded whatever this file does. What is not acceptable is a clipped
// patch that reads whole — a reader who cannot see the cut concludes the change
// set ends where the bytes end, which is a false account of the work rather than
// a smaller one. So the cut is at a line boundary and it is announced, in the
// same grammar as every other bounded list in the account: what was left out,
// and where the whole of it is.
func clipPatch(body string) string {
	if len(body) <= accountPatchBytes {
		return body
	}
	kept := body[:accountPatchBytes]
	if cut := strings.LastIndexByte(kept, '\n'); cut > 0 {
		kept = kept[:cut+1]
	}
	return kept + "\n… this patch was clipped here at " +
		strconv.Itoa(accountPatchBytes>>10) + " KiB. The change set is larger than this file; " +
		"every path it covers is named in the account beside it.\n"
}

// gitDiff is one diff of the account's own paths, in full text, and whether it
// could be taken at all.
//
// THE PATHS ARE HANDED OVER IN RUNS RATHER THAN ALL AT ONCE. Every path goes
// into one argv, and an argv has a size the kernel enforces: a change set large
// enough to pass it makes git fail to start, which used to come back as an empty
// diff and leave the account claiming files and a range with no text to show for
// them. `git diff` does not read a pathspec from a file the way `git add` and
// its neighbours do (checked against git 2.43), so the run is what bounds it,
// and the runs concatenate to exactly the patch one invocation would have
// written: the paths are disjoint and each run's output is whole.
func gitDiff(ctx context.Context, root string, paths []string, revisions ...string) (string, bool) {
	var body strings.Builder
	for _, run := range pathRuns(paths) {
		args := append([]string{"diff", "-M", "--relative", "--no-ext-diff"}, revisions...)
		text, taken := runGit(ctx, root, append(append(args, "--"), run...)...)
		if !taken {
			return "", false
		}
		body.WriteString(text)
	}
	return body.String(), true
}

// pathRuns cuts a pathspec into runs no larger than accountPathspecBytes. A
// single path longer than the bound still goes out alone — refusing it would
// drop a real file from the patch to respect a bound the kernel enforces per
// argument anyway, and git either accepts it or the run reports that it could
// not be taken.
func pathRuns(paths []string) [][]string {
	var runs [][]string
	var run []string
	size := 0
	for _, path := range paths {
		if len(run) > 0 && size+len(path) > accountPathspecBytes {
			runs, run, size = append(runs, run), nil, 0
		}
		run, size = append(run, path), size+len(path)+1
	}
	if len(run) > 0 {
		runs = append(runs, run)
	}
	return runs
}

// untracked is which of the account's paths git has never been told about, in
// the account's own order, and whether the question could be answered at all.
//
// `ls-files --others` is asked rather than `status --porcelain` because it
// answers this one question and answers it in the shape a reader wants: one
// NUL-terminated path per record, relative to the directory it was run in, with
// no status letters to parse and no directories collapsed into a single row.
// `--exclude-standard` is what keeps a .gitignore's own droppings — a build
// directory, a virtualenv, a test runner's cache — out of a patch about the work.
func untracked(ctx context.Context, root string, paths []string) ([]string, bool) {
	body, listed := runGit(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if !listed {
		return nil, false
	}
	unknown := make(map[string]bool, 16)
	for _, path := range strings.Split(body, "\x00") {
		if path != "" {
			unknown[path] = true
		}
	}
	var mine []string
	for _, path := range paths {
		if unknown[path] {
			mine = append(mine, path)
		}
	}
	return mine, true
}

// gitHead is the commit the repository is standing on. Empty is the whole answer
// for a workspace that is not a work tree, a git that is not installed, and a
// repository with no commit yet — three different worlds that owe this account
// the same silence.
func gitHead(ctx context.Context, root string) string {
	head, _ := runGit(ctx, root, "rev-parse", "HEAD")
	return strings.TrimSpace(head)
}

// runGit runs one read-only git command in the workspace and reports both what
// it printed and whether that can be taken as an answer.
//
// A NIL CONTEXT IS REFUSED RATHER THAN PASSED ON. os/exec panics on one, and a
// panic out of a deferred measurement is the one failure this file's whole
// contract forbids: it would take down the leaf whose outcome the measurement
// is not allowed to change.
//
// EXIT 1 IS AN ANSWER AND EXIT 2 IS NOT. That is git's own convention rather
// than laxity — a diff exits 0 having found nothing and 1 having found
// something, which is what every `--no-index` diff of a newly created file
// exits with, while anything from 2 up is git refusing the command. Reading a
// refusal as an empty diff is what let an argv too large for the kernel come
// back as "this leaf changed nothing", so the two are told apart here and every
// caller above decides what a refusal costs it.
func runGit(ctx context.Context, root string, args ...string) (string, bool) {
	if ctx == nil {
		return "", false
	}
	command := osexec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	// A read that takes the index lock can lose a race with whatever else is
	// working in this tree, and an account is never worth blocking the work.
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := command.Output()
	if err == nil {
		return string(out), true
	}
	var exited *osexec.ExitError
	if errors.As(err, &exited) && exited.ExitCode() == 1 {
		return string(out), true
	}
	return "", false
}
