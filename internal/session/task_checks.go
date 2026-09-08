package session

// WHAT THE CHECKER MAY RUN, AND WHO GETS TO SAY.
//
// ── THE LAW ──
//
// THE AUDITOR VERIFIES THE WORK BY THE CHECKS THE WORK ITSELF NAMES, NOT BY A
// LIST THE HARNESS KNOWS. There are exactly three sources for the commands one
// audit may run, and none of them is a language, a toolchain, or a build system
// this file has heard of:
//
//	(a) THE CHECK THE WORK DECLARES — every command the node's own document
//	    names, read out of the brief and the frozen acceptance the node was
//	    finished against, and kept only if the checker could really run it where
//	    it is standing ([declaredChecks], [runnableHere]). A check of either
//	    spelling is read from the work's own account and never from the person's
//	    pasted words.
//	(b) THE CHECK THE WORK RAN — every command the last worker itself ran as one
//	    command, read off its own tool receipts ([ranChecks]).
//	(c) THE ALWAYS-SAFE READING COMMANDS — the ones that print and cannot change
//	    the thing under judgement ([auditReadCommands]). They are not verification
//	    and they are here anyway, for the reason stated on the list itself.
//
// Everything else is refused, and the refusal NAMES what this audit may run, so
// a model that reached for the wrong door reads the right one in the same
// breath (task_audit.go's [refuseOutsideDoor]).
//
// A CHECK IS A FILE OR IT IS A COMMAND, and the two are not matched the same
// way. A declared check that names a file the tree really holds is admitted
// UNDER THE SPELLINGS THAT REALLY START THAT FILE — the file on its own, the file
// with a directory in front of it, the file behind THE INTERPRETER THE FILE
// ITSELF NAMES — because which of those a model reaches for is a coin toss the
// work never had an opinion about. Which word may stand in front is read OFF THE
// FILE (its shebang line, its executable bit) and never off a list of launchers
// this package keeps, which is why `rm <the check>` is not a spelling of the
// check. A check that names no file keeps the field-by-field prefix match it
// always had.
//
// ── THE MEASURED FAILURE THAT PUT IT HERE ──
//
// The allowlist used to be a constant: `go test`, `go build`, `go vet`, and
// git's read-only reporting. On a live SWE-Marathon run — a Rust deliverable, in
// a container with no git on the PATH — every one of those was either wrong or
// absent. The auditor reached for the project's own check, the one the task
// itself names, and was told:
//
//	refused: bash run_tests.sh is not verification, and an auditor only runs
//	verification. You may run: go test, go build, go vet, git diff, git log,
//	git status, git show, pwd, wc, head, cat
//
// It could not run the build either. So it read source files until its five
// minutes ran out — 21 calls, 300 seconds, six cents — and the node landed on the
// window running out, which is "finished, but needs your look", which is a run
// stalled waiting for a person. (The sentence that says so is [checkerRanOut]
// now, and it says only what a clock knows.) That happened on
// EVERY ONE of the six audits of that run's main task. A gate that can only
// verify one language is not a gate, it is a coincidence.
//
// ── WHY THE SAFETY ARGUMENT DOES NOT MOVE ──
//
// The auditor still has no hand that writes, and its bash still runs ONE command
// with no shell composition. What changed is where the list of commands comes
// from, not what a command may be. That matters most for (b): a worker's line
// like `cd x && cargo build 2>&1 | tail -5` is NOT a door, because it cannot be
// re-run without composing it, and this gate does not compose. A check is
// re-run VERBATIM AS THE WORK RAN IT or it is not re-run at all — which is also
// why a command a blanket-allow policy would still stop and ask about
// (internal/approval's critical table) never becomes a door however the work
// spelled it.
//
// ── THE SECOND MEASURED FAILURE: ONE FILE, FIVE SPELLINGS, FIVE REFUSALS ──
//
// The door above was live on the next run of the same benchmark, and it opened
// onto nothing anyway. It offered:
//
//	You may run: /workspace/rust-java-lsp/target/release/rust-java-lsp,
//	run_tests.*, /workspace/golden.jsonl, bash /app/timer.sh, git diff, ...
//
// and the auditor, reading that, tried in order: the check with the directory it
// was standing in stated first — refused, composition; the check with its
// absolute path — refused, not on the list; the bare name — no such program on
// the PATH; the name with a program word in front — refused; the name with a dot
// and a slash in front — refused. FIVE SPELLINGS OF ONE FILE, and the file was
// sitting in the tree the whole time. That is [auditDoor.admitsFile]: the door
// asks WHICH FILE a command names, not how the model typed it — and then asks the
// file itself which word is entitled to start it ([fileFacts]).
//
// The same run's receipts were every one of them `cd /workspace/rust-java-lsp &&
// cargo build --release 2>&1 | tail -3`, so source (b) admitted NOTHING while the
// command the work checked itself with sat in the middle of every line. Two of
// those three pieces are not commands: the `cd` is the worker STATING THE CWD the
// auditor already stands in, and the tail is reading the output of the command in
// front of it. What the line RUNS is its first stage. That is [receiptCheck],
// and the derived command is put through the same two questions the raw one was —
// is it one simple command, and would a blanket allow still stop and ask about
// it — because what changed is which words are read off the receipt, not what the
// auditor may run.
//
// ── THE THIRD MEASURED FAILURE: A DOOR ONTO A WORD THAT IS NOT A PROGRAM ──
//
// Source (a) used to admit EVERY backticked span that had the shape of a
// command, on the reasoning written at [declaredChecks] that a dead entry costs
// a line of a refusal and a missing check costs a verdict. A real acceptance
// then backticked the things prose backticks — a remote, a branch, a repository,
// a rule identifier — and the door read:
//
//	You may run: origin, main, Agent-Field/agentfield, js/polynomial-redos, …
//
// The checker did exactly what it was told it could do. It ran them, one after
// another, collected the shell's 127s and 126s, and wrote "Ran the named checks:
// all refused or exit 126/127" into a finding A PERSON THEN READ as the state of
// the work. A dead entry does not cost a line of a refusal; it costs the finding
// the whole audit exists to produce, because a model handed a door believes the
// door.
//
// SO A DECLARED SPAN IS A DOOR ONLY IF IT COULD RUN WHERE THE CHECKER STANDS —
// its first word is a program the shell would find, or the span names a file
// really sitting in the ground the auditor was put in ([runnableHere]). Both
// halves are questions asked of the machine the check would run on rather than
// of a list this package keeps, which is the same law the rest of this file is
// made of. What the work RAN (source (b)) is not asked: a receipt is the work
// having already run the thing, which is a better answer than any lookup.

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
)

const (
	// auditCheckCount bounds how many checks one source may contribute. The
	// door is read by a model — it is interpolated into the bash description
	// and into every refusal — and a list of forty commands is a paragraph
	// nobody parses. Eight from the work's own words and eight from what it ran
	// is more of both than any real check needs, and it keeps the refusal
	// something a model reads rather than skims.
	auditCheckCount = 8

	// auditReadingDeadline bounds an audit that HAS NOTHING TO RUN.
	//
	// [auditDeadline] is five minutes because a real check is slow: it is a full
	// test run on a real repository plus the reading around it. An audit with no
	// runnable check has no slow half at all — its whole cost is reading files
	// and writing one answer — so five minutes there buys nothing except the
	// exact failure this file was written for: an auditor with no door open to
	// it, spending the person's money until the clock says nobody answered.
	//
	// IT IS DERIVED FROM WHETHER THERE IS A CHECK, NEVER FROM HOW BIG THE WORK
	// IS. A rule that read the size of the tree would be a rule that is wrong on
	// the next tree; this one asks the only question that bounds the cost.
	auditReadingDeadline = auditDeadline / 5
)

// auditReadCommands is the third source: commands that PRINT and cannot change
// the thing under judgement.
//
// They are not verification and they are on the belt anyway, because refusing
// them cost a verdict once: an auditor that cannot ask where it is standing
// spends its steps finding out the hard way, and an audit that died in the wild
// burned two of them on a refused `pwd`. The safety argument the belt rests on
// is about what can CHANGE the thing under judgement, not about which program
// prints it.
//
// NOTHING HERE NAMES A LANGUAGE OR A TOOLCHAIN, and nothing here ever may. The
// moment this list learns what a Go repository or a Rust one looks like, it is
// the constant that was measured failing at the top of this file.
var auditReadCommands = []string{
	"git diff",
	"git log",
	"git status",
	"git show",
	"pwd",
	"wc",
	"head",
	"cat",
}

// auditAllowed is the policy [ranChecks] asks about a command the work ran, and
// it asks the one question that is not this package's to answer: would a gate
// that had been told to allow everything STILL stop and put this command to a
// person?
//
// That is internal/approval's critical table (bash.go), which is the build's one
// floor under a blanket allow and is deliberately short — the handful of shapes
// that destroy a disk or drop the machine. Asking it here rather than writing a
// second table means the auditor's door and the person's gate can never disagree
// about what is critical, which is the drift a second copy guarantees.
var auditAllowed = approval.Policy{Default: approval.ActionAllow}

// auditDoor is what ONE audit may run: the checks it found, and the whole
// allowlist those checks sit at the front of.
//
// THE FIELDS ARE NOT THE SAME QUESTION. `allowed` is what the gate PREFIX-matches
// against. `files` is what it matches BY IDENTITY — the checks that turned out to
// name a file the tree really holds, which are admitted under any spelling of
// that file and are matched by nothing else ([auditDoor.admitsFile]). `checks` is
// whether this audit can VERIFY anything at all — an audit holding nothing but
// the reading commands can look at the work and cannot test it, and that changes
// both what it is told and how long it is given ([auditDoor.window],
// [auditDoor.line]).
type auditDoor struct {
	// checks are the commands the work named or ran, declared first.
	checks []string
	// allowed is checks followed by [auditReadCommands] — the order matters,
	// because it is the order a refusal lists them in and the check is the thing
	// the auditor came for.
	allowed []string
	// ground is the directory the auditor will stand in, which is the one every
	// relative spelling of a file is resolved against. Empty means there is no
	// tree to resolve against and so no file check can exist.
	ground string
	// files are the checks that name a file under `ground`.
	files []fileCheck
}

// fileCheck is a check that turned out to NAME A FILE THE TREE HOLDS, together
// with everything that decides which spellings of it open.
//
// `written` is the spelling the WORK used — the entry this check occupies in
// [auditDoor.allowed], the one the prefix walk must therefore skip, and a
// spelling admitted exactly as it stands, because the work is the one citizen
// entitled to say how its own check is run. `path` is the file itself, resolved
// and cleaned, which is the identity every spelling the AUDITOR reaches for is
// compared against. `interpreter` and `runnable` are what the FILE says about
// being started, read off it by [fileFacts].
type fileCheck struct {
	written     string
	path        string
	interpreter string
	runnable    bool
}

// auditPlace is the two directories a door is read against, and they are two
// because THE AUDIT DOES NOT HAPPEN WHERE THE WORK HAPPENED. `ground` is where
// the auditor will stand — a clean restore beside the node's own checkout
// (task_audit.go's [auditGroundFor]) — and it is what every spelling of a file is
// resolved against, since a file check has to name something that is really there
// under the auditor's own feet. `ran` is where the WORK stood, and it is what a
// receipt's statement of its own working directory is read against, since that is
// the directory the worker was talking about. A door that asked one of those
// questions of the other's directory would answer both of them wrong.
type auditPlace struct {
	ground string
	ran    string
}

// plainDoor is a door made of a bare list of commands and nothing else: the
// fork's read-only shell, the reading-only belt, and every test that asks the
// gate about a list it wrote by hand. There is no ground under it, so there are
// no file checks in it and the gate is exactly the prefix walk it always was.
func plainDoor(allowed []string) auditDoor {
	return auditDoor{allowed: allowed}
}

// auditDoorFor reads one node's door off the node itself: what its own document
// declares, what its last worker actually ran, and which of those turn out to
// name a file sitting in the ground this audit will stand on.
//
// A NIL NODE STILL GETS THE READING COMMANDS. Every caller here has a node, but
// a door with no allowlist at all would be a bash that refuses everything, and a
// belt whose hand refuses everything is a hand this build would not have put on
// (CLAUDE.md's absent-not-broken law).
//
// THE GROUND IS THE DIRECTORY THE AUDITOR WILL BE PUT IN and not some other one:
// a file check resolved against a directory the auditor is not standing in would
// admit spellings that name nothing where it is typing them. The receipts are
// read against the other directory in [auditPlace], for the reason stated there.
func auditDoorFor(node *TaskNode, place auditPlace) auditDoor {
	var checks []string
	if node != nil {
		var declared []string
		for _, source := range node.checkTexts() {
			declared = append(declared, declaredChecks(source.text, place.ground, source.from)...)
		}
		checks = appendChecks(checks, declared)
		checks = appendChecks(checks, ranChecks(node.lastReceipts(), place.ran))
		// AND SOURCE (c): THE CHECKS THIS NODE OWNS FOR THE FAMILY IT HANDED OUT
		// ([TaskNode.Family]). They were taken off its parts because a check that
		// proves the whole proves nothing about a part, and they were given to
		// this node because it is the only one that can honestly make them — so
		// its own door has to open on them, or the node would be told to run a
		// check its bash refuses.
		checks = appendChecks(checks, node.familyChecks())
	}
	allowed := make([]string, 0, len(checks)+len(auditReadCommands))
	allowed = append(allowed, checks...)
	allowed = append(allowed, auditReadCommands...)
	door := auditDoor{checks: checks, allowed: allowed, ground: place.ground}
	for _, check := range checks {
		door.files = append(door.files, fileChecksIn(place.ground, check)...)
	}
	return door
}

// admitsFile is the identity half of the gate: does this command NAME A FILE THIS
// DOOR HOLDS, and is it a spelling that really STARTS that file?
//
// THE FIRST QUESTION IS WHICH FILE, and it is asked of the shape rather than of
// the words. One word IS the file — `run_tests.sh`, `./run_tests.sh`,
// `/abs/path/run_tests.sh`, all of which start it. Two words are a program and
// the file it is handed. Three words are not a spelling of the check at all —
// they are the check plus arguments the work never declared, and this door speaks
// only for what the work declared.
//
// THE SECOND QUESTION IS ASKED OF THE FILE ITSELF, and it is why `rm <the check>`
// is refused while `<its interpreter> <the check>` is not. Three answers open a
// spelling, and all three are facts rather than a list this package keeps:
//
//   - THE WORK'S OWN SPELLING, exactly as the work wrote or ran it. The one
//     citizen entitled to say how a check is run is the work that declared it, and
//     a receipt is the work saying it a second time, in the shell.
//   - THE FILE ON ITS OWN, when the file is executable or carries a line saying
//     what starts it. Both are the file stating that running it is a thing that
//     happens; a file that states neither is data until the work says otherwise.
//   - THE INTERPRETER THE FILE NAMES, in front of it. A script's first line names
//     the program that runs it ([fileFacts]), so the file — not this package —
//     answers which word may stand there. Any path to that program does, since the
//     name at the end of it is the same program.
//
// THE CRITICAL FLOOR STILL STANDS UNDER ALL THREE: one simple command, and not one
// a gate told to allow everything would still stop and put to a person.
func (d auditDoor) admitsFile(fields []string) bool {
	if len(d.files) == 0 {
		return false
	}
	var program, word string
	switch len(fields) {
	case 1:
		word = fields[0]
	case 2:
		// An option is not a program, which is the same reading [commandLike]
		// gives a first word.
		if strings.HasPrefix(fields[0], "-") {
			return false
		}
		program, word = fields[0], fields[1]
	default:
		return false
	}
	path, ok := groundFile(d.ground, word)
	if !ok {
		return false
	}
	command := strings.Join(fields, " ")
	if !approval.Vouchable(command) || auditAllowed.CheckBash(command).Action != approval.ActionAllow {
		return false
	}
	for _, file := range d.files {
		if !sameFile(path, file.path) {
			continue
		}
		switch {
		case command == file.written:
			return true
		case program == "":
			if file.runnable || file.interpreter != "" {
				return true
			}
		case file.interpreter != "" && filepath.Base(program) == file.interpreter:
			return true
		}
	}
	return false
}

// identified says whether one entry of the allowlist is a file check, which is
// how the prefix walk knows to leave it alone: a file check is matched by which
// file it is and by nothing else, so prefix-matching it as well would admit
// `<the check> --whatever-else`, which the work never declared.
func (d auditDoor) identified(entry string) bool {
	for _, file := range d.files {
		if file.written == entry {
			return true
		}
	}
	return false
}

// spelling is how ONE entry of this door is written down for the model to read.
//
// A plain command is written as it stands. A FILE CHECK IS WRITTEN AS THE
// SPELLINGS THAT OPEN IT, NAMED, because the measured failure was a model reading
// a door that named a file and then guessing wrong about it five times running. A
// door that says `<the interpreter> <the file>` is a door walked through on the
// first try, and a door that cannot say it says THAT instead — a file with no
// line naming what starts it and no executable bit is run the way the work ran
// it, or not at all, and a model told so stops guessing at launchers.
func (d auditDoor) spelling(entry string) string {
	var said []string
	for _, file := range d.files {
		if file.written != entry {
			continue
		}
		var ways []string
		if file.runnable || file.interpreter != "" {
			ways = append(ways, "`"+file.path+"`")
		}
		if file.interpreter != "" {
			ways = append(ways, "`"+file.interpreter+" "+file.path+"`")
		}
		if !containsWord(ways, "`"+entry+"`") {
			ways = append(ways, "`"+entry+"`")
		}
		if file.runnable || file.interpreter != "" {
			said = append(said, "the check "+file.path+" — run it as "+strings.Join(ways, " or "))
			continue
		}
		said = append(said, "the check "+file.path+" declares no interpreter; run it the way the work ran it, "+
			strings.Join(ways, " or "))
	}
	if len(said) == 0 {
		return entry
	}
	return strings.Join(said, ", ")
}

// offer is the whole door in the one line a refusal and the shell's own
// description both end on: every entry, in order, each written the way a model
// can retype it.
func (d auditDoor) offer() string {
	if len(d.files) == 0 {
		return strings.Join(d.allowed, ", ")
	}
	said := make([]string, 0, len(d.allowed))
	for _, entry := range d.allowed {
		said = append(said, d.spelling(entry))
	}
	return strings.Join(said, ", ")
}

// window is how long this audit gets. See [auditReadingDeadline] for why the
// answer turns on whether there is a check and on nothing else.
func (d auditDoor) window() time.Duration {
	if len(d.checks) == 0 {
		return auditReadingDeadline
	}
	return auditDeadline
}

// line is what the auditor is TOLD about its own door, and it is written for the
// two cases separately because they are different jobs.
//
// With a check in hand, the auditor is pointed at it: these are the work's own
// checks, run them, they are the whole reason a verdict is worth anything. With
// nothing runnable it is told SO, plainly, and told to judge from reading and
// answer — because the failure this file exists for is an auditor that kept
// reaching for a door that was never going to open, and a model that has not
// been told there is no door will keep reaching for one.
func (d auditDoor) line() string {
	if len(d.checks) == 0 {
		return "NOTHING THIS WORK DECLARES OR RAN IS A CHECK YOU CAN RE-RUN. Your bash will run only " +
			strings.Join(auditReadCommands, ", ") + ", none of which verifies anything. Do not go looking for a " +
			"command to run: read the files and the change, judge what you can see, and answer now. " +
			"An answer from reading alone is a real answer; running out of time is not.\n"
	}
	said := make([]string, 0, len(d.checks))
	for _, check := range d.checks {
		said = append(said, d.spelling(check))
	}
	return "THE CHECKS THIS WORK NAMES OR RAN, which are the only verification commands your bash will run:\n" +
		"  " + strings.Join(said, "\n  ") + "\n" +
		"Run them as they are written. Anything else is refused, and the refusal will say what you may run.\n"
}

// appendChecks folds one source's commands into the door, keeping the order
// they were found in, never listing one twice, and stopping at
// [auditCheckCount] for that source.
func appendChecks(checks, more []string) []string {
	seen := make(map[string]bool, len(checks))
	for _, command := range checks {
		seen[command] = true
	}
	added := 0
	for _, command := range more {
		if seen[command] || added >= auditCheckCount {
			continue
		}
		seen[command] = true
		checks = append(checks, command)
		added++
	}
	return checks
}

// checkSource states whose account named a candidate check. A check belongs to
// the work, never to a shell transcript in the person's pasted words.
//
// THE ZERO VALUE IS THE RESTRICTIVE ONE, AND THAT ORDER IS THE POINT. Both
// callers name the account explicitly today, but a third one written later
// that forgets to will be handed the person's reading rather than the work's —
// so a forgotten field costs a run one check it could have made, which is
// recoverable, instead of costing the gate itself, which is how
// `chmod 000 tox.ini` came out of a pasted reproduction and ran.
type checkSource int

const (
	checksFromAsk checkSource = iota
	checksFromWork
)

// checkText keeps one part of a node's document beside whose account supplied
// it, because that provenance decides whether the text names a check at all.
type checkText struct {
	text string
	from checkSource
}

// declaredChecks is source (a): every command the work's OWN DOCUMENT names.
//
// IT READS THE TWO CONVENTIONS PROSE HAS FOR NAMING A COMMAND and no others: a
// span in backticks, and a line that opens with a shell prompt. Both are how a
// person, a planner or a benchmark writes down "this is the thing to run", in
// every language there is, which is exactly why they are the ones read here — a
// rule that looked for a build system's name would be the constant this file
// replaced, wearing a regexp.
//
// WHOSE ACCOUNT THE TEXT CAME FROM DECIDES WHETHER IT NAMES A CHECK AT ALL.
// Either convention in the work's own brief or acceptance names a check; either
// one in the person's pasted words names none. A measured tox run read
// `chmod 000 tox.ini` out of a pasted reproduction and tried it against the
// deliverable tree, where success would have made the project's configuration
// unreadable without tripping the tree-moved guard. Nearly every bug report
// backticks its reproduction, so exempting backticks was the same hole wearing
// different punctuation.
//
// TWO FILTERS STAND BETWEEN A BACKTICK AND A DOOR, and they ask different
// questions. [commandLike] asks whether the span has the SHAPE of one command —
// a path, an option or a sentence fails it. [runnableHere] then asks whether it
// is a command AT ALL WHERE THE CHECKER WILL BE STANDING, which is the question
// the third measured failure at the top of this file was made of: prose
// backticks a branch and a repository as readily as it backticks a build, and
// every one of those has the shape of a command.
//
// THE GROUND IS THE DIRECTORY THE CHECKER WILL BE PUT IN, threaded down from the
// caller that already knows it. A span resolved against any other directory
// would be admitted or refused on the strength of a tree nobody is standing in.
func declaredChecks(text, ground string, from checkSource) []string {
	// ONLY THE WORK'S ACCOUNT NAMES A CHECK, IN EITHER SPELLING. Every value
	// not explicitly identified as the work stays restrictive, so a new source
	// added later cannot read the person's words merely because its caller
	// forgot to classify it.
	if from != checksFromWork {
		return nil
	}

	var out []string
	// The odd-numbered pieces of a split on the backtick are what was BETWEEN a
	// pair of them. A fenced block splits into empty pieces around its own
	// content, and the content itself carries newlines, so both fail
	// [commandLike] on their own without a special case for fences.
	spans := strings.Split(text, "`")
	for index := 1; index < len(spans); index += 2 {
		if command, ok := commandLike(spans[index]); ok && runnableHere(ground, command) {
			out = append(out, command)
		}
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "$ ") {
			continue
		}
		command, ok := commandLike(strings.TrimPrefix(line, "$ "))
		if ok && runnableHere(ground, command) {
			out = append(out, command)
		}
	}
	return out
}

// checkCommand answers HOW A DECLARED CHECK IS INVOKED where the checker is
// going to stand, or "" when it cannot be invoked at all.
//
// A CHECK IS A COMMAND AND NEVER A PATH, which is the law this function is. The
// door a node's own auditor gets already knows the difference: it reads the file
// itself ([fileFacts]) and writes down which spellings START it, so a model is
// told `<the interpreter> <the file>` rather than left guessing
// ([auditDoor.spelling]). Whoever runs a check under a shell needs the same fact
// and used to be given only the span the prose held — so a session that harvested
// a bare `src/version.py` out of its own acceptance ran `bash -c src/version.py`,
// collected exit 126 from a file with no executable bit, and reported "does not
// pass" about it on every round of a whole evening (#468).
//
// THE TREE IS ASKED BEFORE THE PATH, and that order is a law rather than a
// preference: A FILE THE TREE HOLDS OUTRANKS A PROGRAM OF THE SAME NAME ON PATH.
// A repository that carries its own `check`, `build` or `test` beside the work
// means THAT file, and a lookup that answered first would silently run somebody
// else's program of the same name against somebody else's assumptions — the exact
// shape of wrongness this whole reading exists to remove.
//
// THREE ANSWERS, AND EVERY ONE OF THEM IS A FACT RATHER THAN A LIST:
//
//   - THE SPAN NAMES A FILE AND THE WORK NAMED THE PROGRAM TOO — two words, a
//     launcher and its file — so the program stands exactly as the work wrote it
//     and the file behind it is resolved and quoted like any other. The work is
//     the one citizen entitled to say WHICH program runs its check; it is not the
//     authority on how a shell splits a word, and `sh run'"'"'tests.sh` handed back
//     as written is an unterminated quote rather than a check.
//   - THE SPAN IS THE FILE ALONE: it is opened the way the file itself says it
//     opens, by its executable bit or by the interpreter its first line names, and
//     the RESOLVED path is what goes into the command, because a bare word with no
//     directory in it would send the shell looking down PATH for a file sitting in
//     the tree.
//   - THE TREE HOLDS NOTHING BY THAT NAME AND THE FIRST WORD IS A PROGRAM THE
//     SHELL WOULD FIND ([onThePath]): the span is already a command and stands
//     exactly as the work wrote it.
//
// AND A FILE THAT SAYS NEITHER IS NOT A CHECK. It is data the prose happened to
// backtick, there is no way to run it, and "does not pass" is a sentence about a
// check that RAN — so this answers "" and the caller drops it rather than
// carrying a permanent failure for the life of the session. It answers "" EVEN
// WHEN A PROGRAM OF THAT NAME IS ON THE PATH, because the tree said which file
// was meant and running a different one would be worse than running nothing.
func checkCommand(ground, check string) string {
	fields := strings.Fields(check)
	if len(fields) == 0 {
		return ""
	}
	if files := fileChecksIn(ground, check); len(files) > 0 {
		if len(fields) > 1 {
			// The second word is the one [fileChecksIn] resolved, so the file it
			// found is the file this command is about — and a wildcard the work
			// wrote takes its first match, for the reason stated just below.
			return fields[0] + " " + shellQuoted(files[0].path)
		}
		// A WILDCARD THE WORK WROTE RESOLVES TO WHATEVER IT MATCHES, and the
		// first match that can be started is the check. The alternative —
		// running every match — would turn one declared check into eight
		// processes nobody declared.
		for _, file := range files {
			switch {
			case file.runnable:
				return shellQuoted(file.path)
			case file.interpreter != "":
				return file.interpreter + " " + shellQuoted(file.path)
			}
		}
		return ""
	}
	if onThePath(fields[0]) {
		return check
	}
	return ""
}

// shellQuoted wraps one path so that a shell reads it as ONE WORD, whatever is
// in it.
//
// EVERY PATH THIS FILE HANDS TO A SHELL GOES THROUGH IT, unconditionally. A rule
// that quoted only the paths that "needed" it would be a second reading of what a
// shell does with a character, drifting from the first the day somebody meets a
// bracket — and the measured shape is ordinary: a checkout under a directory with
// a space in its name split into two words, and the check ran against neither of
// them.
//
// SINGLE QUOTES, WITH THE ONE ESCAPE THEY HAVE. Inside single quotes a shell
// expands nothing at all, so the only character that has to be handled is the
// quote itself — closed, escaped, and reopened.
func shellQuoted(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// runnableHere is the question the third measured failure at the top of this
// file put to source (a): COULD THE CHECKER ACTUALLY RUN THIS WHERE IT IS BEING
// PUT? A word that is neither a program nor a file is not a check, however
// command-shaped the prose around it was.
//
// THERE ARE TWO WAYS TO BE RUNNABLE AND THIS ASKS BOTH, because a check is a
// file or it is a command and that is the same division the door itself is built
// on:
//
//   - THE FIRST WORD IS A PROGRAM THE SHELL WOULD FIND ([onThePath]). That is the
//     operating system answering, not a list — the checker's bash searches the
//     same PATH this process holds, so a lookup here is the lookup the shell is
//     about to do.
//   - OR THE SPAN NAMES A FILE THE GROUND REALLY HOLDS, which is [fileChecksIn],
//     the same reading that decides which spellings of a file check open. Asking
//     it here rather than writing a second resolver is what keeps one answer to
//     "does this check name a file": a span admitted by this question is the same
//     span [auditDoorFor] is about to build a [fileCheck] out of.
//
// A DEAD ENTRY IS NOT REFUSED FOR BEING DANGEROUS, and nothing here is a safety
// argument. The floor under what may be typed is unchanged; what changed is that
// the door no longer offers things that cannot be typed at all.
func runnableHere(ground, command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	if onThePath(fields[0]) {
		return true
	}
	return len(fileChecksIn(ground, command)) > 0
}

// onThePath asks whether one word names a program the checker's shell would
// find, and it asks it ONLY OF A BARE WORD.
//
// A word with a directory in it is a path, and a path is a question about the
// ground the checker stands in rather than about the PATH — the lookup would
// resolve it against THIS process's working directory, which is not where the
// check is going to run and so is not an answer to anything. Those words are
// left for [fileChecksIn], which resolves against the right directory.
func onThePath(word string) bool {
	if word == "" || filepath.Base(word) != word {
		return false
	}
	_, err := exec.LookPath(word)
	return err == nil
}

// normalizedCheckCommand is this package's ONE reading of "are these two
// spellings the same check", and it exists because nothing else here answers
// that question: [appendChecks] dedupes on the exact bytes, [commandLike] asks
// about SHAPE, and [runnableHere] asks whether a span could run at all.
//
// NORMALIZATION DROPS WHAT DOES NOT CHANGE WHAT IS MEASURED, AND KEEPS
// EVERYTHING THAT NARROWS WHAT RUNS. That is the whole rule, and it is stated
// once, here, because a second reading of it somewhere else would be a second
// rule the day the two disagreed (design-law §ONE SOURCE OF TRUTH).
//
// DROPPED: the whitespace a model happened to type, which is typing and not
// measurement; a trailing path separator on a word that is already a path,
// because a shell reads `./pkg/` and `./pkg` as one directory; and a word that
// only defeats a result cache (`-count=1`), because it changes whether an answer
// is REUSED and never which answer is asked for.
//
// KEPT: every other word — a filter, a package path, a flag — because each of
// them narrows what actually runs, and folding two narrowed checks together
// would quietly drop one somebody asked for. `./internal/tui3` and
// `./internal/tui3/...` are DELIBERATELY TWO COMMANDS: one measures a package
// and the other measures a subtree, and the day they read as one is the day a
// division could hoist away a check nobody else was going to make.
//
// AND TWO LIMITS, STATED RATHER THAN FIXED, because both of them cost a MISS and
// the repairs for them would cost a REFUSAL — which is the wrong way round for a
// reading that stands in front of a road:
//
//   - WHITESPACE INSIDE A QUOTED ARGUMENT IS COLLAPSED WITH ALL OTHER
//     WHITESPACE, so `printf 'a b'` and `printf 'a   b'` read as one command
//     here. That is inherited from this package's own reading of a command
//     ([refuseOutsideDoor] normalises a command's whitespace the same way), and
//     it is kept the same ON PURPOSE: one reading of what a command is, not two.
//   - `-count=1` AND `-count 1` ARE THE SAME CHECK AND DO NOT FOLD, because
//     folding them means parsing a flag's VALUE — knowing which flags take one
//     and which do not — and a guess at a flag's shape that came out wrong would
//     fold two different checks into one and refuse a division over it. The miss
//     costs a repeated run; the guess would cost a road.
func normalizedCheckCommand(command string) string {
	words := strings.Fields(command)
	kept := make([]string, 0, len(words))
	for _, word := range words {
		if word == cacheDefeatingWord {
			continue
		}
		if pathLikeWord(word) {
			word = strings.TrimSuffix(word, "/")
		}
		kept = append(kept, word)
	}
	return strings.Join(kept, " ")
}

// pathLikeWord says whether a trailing `/` on this word is a SEPARATOR — a word
// that names a directory either way — rather than a character somebody meant.
//
// THE SEPARATOR HAS TO HAVE SEPARATED SOMETHING, which is the whole rule. A word
// still holding a `/` once its last character is off is a path and `./pkg/` is
// `./pkg`; a word whose ONLY slash is the last one has not separated anything,
// and `-run TestHTTP/` is a FILTER whose subtests are a different check from
// `-run TestHTTP`'s. Cutting it there would fold two filters into one and refuse
// a division over it, which is the exact failure this normalisation promises not
// to cause. A word that opens with `-` is a flag and is never a path, and a bare
// `/` is a directory in its own right and not a spelling of anything.
func pathLikeWord(word string) bool {
	if strings.HasPrefix(word, "-") {
		return false
	}
	return strings.Contains(strings.TrimSuffix(word, "/"), "/")
}

// cacheDefeatingWord is the one word this package knows changes nothing about
// WHICH work a check does. It is a constant rather than a list because there is
// exactly one of it: a list would be the beginning of a grammar over flags, and
// every flag that is not this one narrows what runs and is kept.
const cacheDefeatingWord = "-count=1"

// ranChecks is source (b): what the last worker ITSELF ran, read off the
// receipts the node already carries for the auditor's packet (task_audit.go's
// [lastToolReceipts]).
//
// ONLY WHAT IT RAN AS ONE COMMAND, and [receiptCheck] is the reading that decides
// which words of a receipt those are. Everything after that reading is what it
// always was: [approval.Vouchable] is the build's existing answer to "is this one
// simple command" — the same question a standing approval has to ask before it
// may speak for a line somebody typed — and asking it here means the two can
// never drift apart.
//
// AND NOT WHAT A BLANKET ALLOW WOULD STILL ASK ABOUT ([auditAllowed]). The work
// ran with hands this auditor does not have; the fact that it ran something is
// not a reason to hand the judge a way to destroy the tree it is judging.
func ranChecks(receipts []toolReceipt, ran string) []string {
	var out []string
	for _, receipt := range receipts {
		if command, ok := receiptCheck(receipt.command, ran); ok {
			out = append(out, command)
		}
	}
	return out
}

// receiptCheck reads ONE COMMAND out of one line a worker ran, and it is the
// answer to the second measured failure at the top of this file: every receipt of
// that run was `cd <the tree> && <the build> 2>&1 | tail -3`, so the old reading —
// which asked whether the WHOLE LINE was one simple command and gave up when it
// was not — handed the auditor nothing at all, while the command the work checked
// itself with sat in the middle of every one of them.
//
// TWO OF THE THREE PIECES OF THAT LINE ARE NOT THE COMMAND:
//
//   - A LEADING `<word> <directory> &&` IS THE WORKER STATING WHERE IT IS. The
//     auditor is handed a working directory of its own, so the statement is
//     redundant rather than composed, and it is dropped. It is recognised by SHAPE
//     and not by which verb spells it: one word, then one word that resolves to the
//     tree the work ran in or to somewhere inside it. A directory anywhere else is
//     not a statement about this tree, and then the line stays composed and
//     contributes nothing.
//   - WHAT A PIPELINE RUNS IS ITS FIRST STAGE. The stages after it only read the
//     output of the one in front; so do the redirections of stdout and stderr that
//     trail the end of it. Neither is part of the command being checked, and
//     neither is re-run.
//
// WHAT IS DERIVED IS STILL RUN AS ONE COMMAND WITH NO COMPOSITION. This changes
// which words are read off a receipt; it changes nothing about what the auditor
// may type, which is the same single uncomposed command it always was.
func receiptCheck(line, ran string) (string, bool) {
	line, ok := dropStandingIn(strings.TrimSpace(line), ran)
	if !ok {
		return "", false
	}
	line, ok = firstStage(line)
	if !ok {
		return "", false
	}
	command, ok := commandLike(line)
	if !ok || !approval.Vouchable(command) {
		return "", false
	}
	if auditAllowed.CheckBash(command).Action != approval.ActionAllow {
		return "", false
	}
	return command, true
}

// dropStandingIn takes off a leading statement of the working directory the
// auditor is already standing in. See [receiptCheck] for why that is a redundancy
// rather than a composition, and why the shape rather than the verb is what is
// read.
func dropStandingIn(line, ran string) (string, bool) {
	head, rest, joined := strings.Cut(line, "&&")
	if !joined {
		return line, true
	}
	fields := strings.Fields(head)
	if len(fields) != 2 {
		return "", false
	}
	if _, ok := groundDir(ran, fields[1]); !ok {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// preparedAuditCommand is the command the gate will actually run: the first
// stage of what the model typed, with trailing pipes and redirections taken
// off. THE MODEL OFTEN ADDS THOSE ITSELF — `python3 -m pytest … 2>&1`,
// `go test ./... > /tmp/out` — and the gate used to refuse the whole line
// because '>' is composition, then the auditor retried the same shape on
// the dear tier (F40). What the line RUNS is its first stage; that is
// already the law for receipts ([firstStage]), and it is the law here so
// the auditor never emits a command its own contract then rejects.
//
// A LINE THAT IS NOT A PIPELINE IS LEFT ALONE. `go test ./... && rm -rf .`
// is still two commands, still refused, and a shorter reading of it would
// be a wider door.
func preparedAuditCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return command
	}
	if stage, ok := firstStage(command); ok {
		return strings.TrimSpace(stage)
	}
	return command
}

// firstStage keeps the command a line RUNS and drops what only reads its output:
// the stages after the first pipe, and the redirections of stdout and stderr that
// trail the end of it.
//
// A LINE WITH NOTHING IN FRONT OF ITS FIRST REDIRECTION IS NOT A COMMAND, and it
// does not become a door. Neither does `a || b`, which is not a pipeline at all
// but a second command waiting on the first one failing.
//
// THE REDIRECTIONS ARE TAKEN OFF THE END AND NOWHERE ELSE. A redirection in the
// middle of a line leaves the line composed, [commandLike] refuses it, and the
// receipt contributes nothing — which is the right way to be wrong: cutting a
// command short at the first arrow it happens to contain would hand the door a
// SHORTER command than the work ran, and a shorter command is a wider one.
func firstStage(line string) (string, bool) {
	if head, rest, piped := strings.Cut(line, "|"); piped {
		if strings.HasPrefix(rest, "|") {
			return "", false
		}
		line = head
	}
	fields := strings.Fields(line)
	for len(fields) > 0 {
		last := len(fields) - 1
		switch {
		case redirection(fields[last]):
			// `2>&1`, `>log`, and the bare arrow of a redirection whose file was
			// written apart from it.
			fields = fields[:last]
		case last > 0 && redirection(fields[last-1]):
			// The file that arrow was pointing at, and the arrow with it.
			fields = fields[:last-1]
		default:
			return strings.Join(fields, " "), true
		}
	}
	return "", false
}

// redirection reads one word for the SHAPE of a redirection — an optional file
// descriptor or an ampersand, and then an arrow — rather than for any particular
// spelling of one. A word that merely contains an arrow somewhere inside it is an
// argument, not a redirection, and is left where the work put it.
func redirection(field string) bool {
	arrow := strings.TrimLeft(field, "0123456789&")
	return strings.HasPrefix(arrow, ">") || strings.HasPrefix(arrow, "<")
}

// fileChecksIn decides whether one check NAMES A FILE, reading the same two
// shapes [auditDoor.admitsFile] admits: the file alone, or one program word and
// then the file.
//
// A WILDCARD THE WORK WROTE IS RESOLVED RATHER THAN REFUSED. The run this was
// written for declared its check as `run_tests.*`, which names exactly one file
// on disk and no program at all; a rule that only understood literal paths would
// have left that door shut for the same reason it was shut before.
func fileChecksIn(ground, check string) []fileCheck {
	fields := strings.Fields(check)
	var word string
	switch len(fields) {
	case 1:
		word = fields[0]
	case 2:
		if strings.HasPrefix(fields[0], "-") {
			return nil
		}
		word = fields[1]
	default:
		return nil
	}
	var out []fileCheck
	for _, path := range groundFiles(ground, word) {
		interpreter, runnable := fileFacts(path)
		out = append(out, fileCheck{
			written:     check,
			path:        path,
			interpreter: interpreter,
			runnable:    runnable,
		})
	}
	return out
}

// fileFacts asks the FILE the question this door is not allowed to answer out of
// a list: which word starts it, and is starting it a thing this file says happens
// at all.
//
// THE FIRST LINE OF A SCRIPT NAMES ITS OWN INTERPRETER. That is a convention of
// the operating system rather than of any one language, which is exactly why it
// is the one read here: the file is the authority on what runs it, so `rm` is not
// a spelling of a check and the program the file names is, without this package
// ever learning a launcher's name.
//
// THE LINE IS READ BY SHAPE, AND THE PROGRAM IS THE FIRST WORD ON IT. That is
// what the operating system itself does with the line: everything after the first
// word is an ARGUMENT handed to that program, not another program. Reading the
// last word instead made `#!/usr/bin/python3 isolated` answer `isolated` — a mode
// flag promoted to a launcher, and a door that would then admit `isolated <the
// check>` and refuse the interpreter that really starts it.
//
// THE ONE EXCEPTION IS THE LAUNCHER WHOSE JOB IS TO FIND ANOTHER PROGRAM, and it
// is recognised by its own name rather than by a list of launchers: a first word
// whose base name is `env` is a program that runs the next one it is given, so the
// interpreter is the next word that is not an option — which is what makes both
// `#!/usr/bin/env python3` and `#!/usr/bin/env -S python3 -u` answer `python3`.
//
// What comes back is a base name, because a program is the same program down
// every path that reaches it.
//
// THE EXECUTABLE BIT IS THE SECOND FACT, and it answers a different question: a
// file with it set states that running it is a thing that happens, which is what
// makes `./the-check` a spelling at all. A file with neither fact is data until
// the work itself says otherwise.
func fileFacts(path string) (interpreter string, runnable bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	runnable = info.Mode().Perm()&0o111 != 0

	file, err := os.Open(path)
	if err != nil {
		return "", runnable
	}
	defer func() { _ = file.Close() }()
	// Two hundred bytes is more first line than any shebang has ever needed, and
	// it keeps this off the end of a file that turned out to be a gigabyte.
	head := make([]byte, 200)
	read, err := io.ReadFull(file, head)
	if read == 0 || (err != nil && err != io.EOF && err != io.ErrUnexpectedEOF) {
		return "", runnable
	}
	line := string(head[:read])
	if cut := strings.IndexAny(line, "\r\n"); cut >= 0 {
		line = line[:cut]
	}
	if !strings.HasPrefix(line, "#!") {
		return "", runnable
	}
	fields := strings.Fields(line[2:])
	if len(fields) == 0 {
		return "", runnable
	}
	named := fields[0]
	if filepath.Base(named) == "env" {
		named = ""
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "-") {
				continue
			}
			named = field
			break
		}
	}
	if named == "" {
		return "", runnable
	}
	return filepath.Base(named), runnable
}

// groundFiles resolves one word of a check to the files it names under the ground
// this audit stands on — one file for a literal path, however many a wildcard the
// work wrote actually matches, and none at all for a word that names nothing.
func groundFiles(ground, word string) []string {
	if !strings.ContainsAny(word, "*?[") {
		if path, ok := groundFile(ground, word); ok {
			return []string{path}
		}
		return nil
	}
	pattern, ok := underGround(ground, word)
	if !ok {
		return nil
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	var out []string
	for _, match := range matches {
		if path, ok := groundFile(ground, match); ok {
			out = append(out, path)
			if len(out) >= auditCheckCount {
				break
			}
		}
	}
	return out
}

// groundFile resolves one word to a REGULAR FILE UNDER THE GROUND, which is the
// only thing this door will ever call a file check: a word naming something
// outside the tree is a word about somebody else's machine, and a word naming a
// directory or a device is not a check anybody runs.
func groundFile(ground, word string) (string, bool) {
	path, ok := underGround(ground, word)
	if !ok {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return path, true
}

// groundDir is [groundFile]'s question asked about a directory: the ground itself
// counts, because "I am standing here" is the commonest thing a receipt says.
func groundDir(ground, word string) (string, bool) {
	path, ok := underGround(ground, word)
	if !ok {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return path, true
}

// underGround resolves a word the way the auditor's own shell would — relative to
// the directory it stands in — and then refuses anything that landed outside that
// directory. WITH NO GROUND THERE IS NO RESOLUTION AND NO FILE CHECK: a door built
// without a tree behind it is the prefix walk it always was.
func underGround(ground, word string) (string, bool) {
	ground = strings.TrimSpace(ground)
	if ground == "" || word == "" {
		return "", false
	}
	root, err := filepath.Abs(ground)
	if err != nil {
		return "", false
	}
	path := word
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	inside, err := filepath.Rel(root, path)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", false
	}
	return path, true
}

// sameFile asks whether two resolved paths are ONE FILE. The cleaned paths agree
// in the ordinary case; os.SameFile is asked when they do not, because a tree
// reached through a symlinked parent — a temporary directory on a Mac, a restore
// beside the node's own checkout — spells the same file two ways and a door that
// refused the second spelling would be the failure this was written for again.
func sameFile(one, other string) bool {
	if one == other {
		return true
	}
	first, err := os.Stat(one)
	if err != nil {
		return false
	}
	second, err := os.Stat(other)
	if err != nil {
		return false
	}
	return os.SameFile(first, second)
}

// commandLike decides whether a fragment of text is a command this door could
// ever open for, and normalizes the ones that are.
//
// IT IS A SHAPE TEST AND NOT A VOCABULARY TEST. It knows nothing about which
// programs exist; it asks whether what it is holding could be typed at a shell
// as one command:
//
//   - no shell composition, which is the gate's own standing law
//     ([shellComposition]) asked one step earlier;
//   - a first word that is a program rather than an option, because a brief
//     backticking `--stdio` is naming a flag and not a check;
//   - a first word with something in it besides wildcards, because a door
//     spelled `*` is not a door, it is an open wall;
//   - short enough to be read back inside a refusal, since a command nobody can
//     read in the door's own list is a command nobody will type.
//
// Whitespace is normalized for the reason the gate normalizes it: "go  test" and
// "go test" are one command, and the door is about which program runs rather
// than about how it was typed.
func commandLike(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, shellComposition) {
		return "", false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", false
	}
	program := fields[0]
	if strings.HasPrefix(program, "-") || strings.Trim(program, "*?[]") == "" {
		return "", false
	}
	normalized := strings.Join(fields, " ")
	if len(normalized) > auditCommandLimit {
		return "", false
	}
	return normalized, true
}
