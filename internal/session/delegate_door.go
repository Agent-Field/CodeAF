package session

// THE DELEGATE DOOR: how a conversation hands a task to a program codeaf
// carries — senior-dev first (internal/delegate, docs/design/delegate/). A
// program is one more worker kind behind the run engine, and this file is the
// half a conversation needs of it: which programs this build carries, the door
// `/<name> <brief>` and `propose_task`'s `via` both open, and the landing of a
// run whose worker was a program rather than a bash worker.
//
// "DELEGATE" IS A WORKING TITLE. Every sentence here a person or the model can
// read names the program itself, so a later rename of the idea changes code
// and never a promise already made on a screen.
//
// IT RIDES THE RUN ROAD WHATEVER THE BELT SAYS. `/task` takes the run road only
// under CODEAF_TASK_BELT=bash, because that road's WORKER is the bash belt. A
// program's worker is the program, so the road is asked for outright here: the
// store, the copy, the supervisor and the landing are the run's, and nothing in
// them reads the belt switch. What a delegated run does not have is the review
// round, because a check seat is a bash-belt worker and the belt may be off; the
// program's own verification is what its terminal record reports.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// DelegateRow is one program as a surface lists it: the command word, the
// sentence under it, and what it leaves behind.
type DelegateRow struct {
	Name        string
	Description string
	// Lands is delegate.LandsTree or delegate.LandsText.
	Lands string
}

// DelegateReport is the programs this conversation can hand work to, as the
// surface draws its command rows from them.
type DelegateReport struct {
	Rows []DelegateRow
}

// Delegates is the report for this conversation. A build that carries none
// answers the zero report, and the surface draws no rows.
func (a *Agent) Delegates() DelegateReport { return a.config.delegateReport() }

func (c Config) delegateReport() DelegateReport {
	var report DelegateReport
	for _, program := range c.Delegates {
		lands := program.Lands
		if lands == "" {
			lands = delegate.LandsTree
		}
		report.Rows = append(report.Rows, DelegateRow{Name: program.Name, Description: program.Summary, Lands: lands})
	}
	return report
}

// delegateNames is the programs' names, sorted, for the prompt and the refusal.
func (c Config) delegateNames() []string {
	names := make([]string, 0, len(c.Delegates))
	for _, program := range c.Delegates {
		names = append(names, program.Name)
	}
	sort.Strings(names)
	return names
}

// mayDelegate says whether this belt may hand work to a program: it is the
// conversation's own hand-off predicate with one more condition, that this
// build carries at least one. A task node never delegates, for the reason it
// never proposes: there is nowhere for the work to go from there.
func (c Config) mayDelegate() bool {
	return c.mayProposeTask() && !c.InTask && len(c.Delegates) > 0
}

// delegateFact is the hand-off page's one paragraph about these programs. It is
// rendered only where [Config.mayDelegate] holds, and its `fill` writes the
// programs in — each one's name and its own guide — so the model is told the
// words it can put in `via` and never a name this build does not carry.
//
// THE PARAGRAPH SAYS WHAT CODEAF DOES, AND EACH PROGRAM SAYS WHAT IT IS. What
// a program is for, what its brief must hold and what it needs of its folder
// are the program's own [delegate.Delegate.Guide], printed under its name, so
// nothing here names senior-dev. What is true of every program that edits
// files — the copy it works in, what lands, and so which folder it must be
// handed — is codeaf's mechanics, and is said here once ([delegateFolderRule]).
// That nobody can be asked anything is `propose_task`'s own `brief`
// description, and that small work is never handed off is this section's
// own; neither is said a second time here.
var delegateFact = beltFact{
	tools: []string{"propose_task"},
	holds: Config.mayDelegate,
	present: "AND ONE LARGE TASK CAN GO TO A PROGRAM BUILT INTO CODEAF, named in `propose_task`'s\n" +
		"`via`, which does the whole of it alone.%s The programs here:\n%s",
	fill: func(config Config, text string) string {
		rule := ""
		if config.carriesTreeProgram() {
			rule = delegateFolderRule
		}
		return fmt.Sprintf(text, rule, config.delegateGuides())
	},
}

// delegateFolderRule is codeaf's one sentence about the folder a program that
// edits files is handed, and it is printed only when the build carries one.
//
// IT EXISTS BECAUSE A MODEL SENT SENIOR-DEV TO THE WRONG REPOSITORY. Asked to
// solve a benchmark task whose code lived in a repository not on the machine,
// the conversation handed senior-dev the one repository it knew — the
// benchmark's, which holds the task's reference solution beside its statement —
// and wrote a brief telling it to make a checkout of the real one. senior-dev
// cloned it into the person's own projects folder and edited it there, outside
// the copy codeaf lands from, and the task ended saying it had changed nothing.
// The copy is cut from the folder the proposal names, so the folder is the one
// thing the model has to get right, and fetching a repository that is not here
// is its job, done before the proposal.
//
// AND IT PROMISES NO MERGE, because there is none. It said "only that copy
// lands", and a model told its work lands tells the person the work is in their
// folder; a program's work is left on the task's own branch
// ([delegateKeepsBranch]), and bringing it in is a separate step.
const delegateFolderRule = "\nIt works in a copy of the task's folder, and only that copy's work is kept, on a branch\n" +
	"nothing merges, so hand it the repository the work belongs in: clone one this machine\n" +
	"lacks into a new folder, on a branch at the commit the work names, and pass it as\n" +
	"`ground`. Never brief it to work elsewhere."

// carriesTreeProgram says whether any program this conversation can hand work
// to edits files, which is when [delegateFolderRule] is true of it.
func (c Config) carriesTreeProgram() bool {
	for _, program := range c.Delegates {
		if program.LandsTree() {
			return true
		}
	}
	return false
}

// delegateGuides is the programs as the hand-off paragraph lists them: one item
// each, sorted by name, the name as `via` takes it and then the program's own
// guide.
func (c Config) delegateGuides() string {
	programs := append([]delegate.Delegate(nil), c.Delegates...)
	sort.Slice(programs, func(i, j int) bool { return programs[i].Name < programs[j].Name })
	items := make([]string, 0, len(programs))
	for _, program := range programs {
		items = append(items, "- `"+program.Name+"`: "+strings.TrimSpace(program.Guide))
	}
	return strings.Join(items, "\n")
}

// delegateKeepsBranch says how a tree program's work comes home: ON ITS
// BRANCH, and never merged into the person's checkout by codeaf.
//
// A PROGRAM'S HOUR OF WORK IS NOT MERGED BEHIND ANYBODY'S BACK. A merge at the
// end of an hour meets whatever happened to the checkout in that hour — the
// person's own edits, another task's landing, a conversation that kept working
// — and a clash then turned a finished run into one that read as failed, with
// its work parked on a branch anyway. So the branch is the landing: one
// squashed commit, reachable from the folder the task was proposed on, and the
// conversation or the person merges it when they choose. Nothing can conflict
// at the landing, because the landing writes nothing of anybody's.
func delegateKeepsBranch(via *delegate.Delegate, plain bool) bool {
	return via != nil && via.LandsTree() && !plain
}

// branchOnlySentence is what a person is told about work that landed as its
// branch: where it is, and that it is theirs to bring in.
func branchOnlySentence(branch, root string) string {
	return "its work is on the branch " + branch + " in " + root + "; nothing was merged into your checkout"
}

// delegateReceipt is the sentence an approved hand-off to a program adds to
// its receipt: who has the work, where, and where it will be when it ends.
// ground is the folder the run was started on; whether it has a history to
// copy is read off it the way the run read it ([delegateOnPlainFolder]), and
// not off the live run, which a program that dies in its first second has
// already left by the time the receipt is written.
//
// IT NEVER SAYS THE WORK LANDS. It said "lands when it ends", and a program's
// work is left on the task's own branch and merged by nobody; a model that read
// "lands" told the person their folder held work it did not.
//
// IT NAMES THE FOLDER. A receipt that said "a copy" and "the folder itself"
// without saying which let a model that had named ~/Desktop/pong read that its
// program was there while it had been handed the person's home folder.
func delegateReceipt(ground string, via delegate.Delegate) string {
	if !via.LandsTree() {
		return "It is " + via.Name + "'s: it works alone, and its answer arrives when it ends."
	}
	if !hasGitHistory(ground) {
		return "It is " + via.Name + "'s: it works alone in " + ground + " itself, which has no git history, so its changes are there as it makes them."
	}
	return "It is " + via.Name + "'s: it works alone in a copy of " + ground + ", and when it ends its work is left on the task's own branch; nothing is merged into the checkout."
}

// programPlace is where a program works, in a person's words: the folder
// itself, or a copy of it when it has a history to copy from and the program
// edits code.
func programPlace(program delegate.Delegate, ground string) string {
	if !program.LandsTree() || !hasGitHistory(ground) {
		return ground
	}
	return "a copy of " + ground
}

// hasGitHistory says ground is in a repository with at least one commit — the
// same reading [delegateOnPlainFolder] makes of the copy it was given.
func hasGitHistory(ground string) bool {
	root, ok := repositoryRoot(ground)
	return ok && hasCommit(root)
}

// delegateStartedReceipt is an approved hand-off's receipt: a task's first line
// and its wake sentence, with the program's own account of where it works
// ([Agent.delegateReceipt]) in place of a task's "in a copy of its own", which
// a program on a plain folder is not.
// on is the models the person asked it to work with, "" for the crew's.
func delegateStartedReceipt(id uint64, title, on, where, elsewhere string) string {
	if on != "" {
		on = " on " + on
	}
	return withElsewhere(fmt.Sprintf("task %d started%s: %s\n%s %s", id, on, title, where, taskHandoffWakeSentence), elsewhere)
}

// programHomeRefusal is the one folder a tree program is never handed: the
// person's home folder, or one that holds it. It is not a project, and a
// program on a folder with no git history snapshots the whole of it to know
// what it changed — every file under the home folder, and a refusal from the
// first one macOS keeps to itself. instead is what the one refused can do.
func programHomeRefusal(program delegate.Delegate, dir, instead string) string {
	if !program.LandsTree() || !holdsHomeFolder(dir) {
		return ""
	}
	what := "holds your home folder"
	if home, err := os.UserHomeDir(); err == nil && canonicalPath(home) == canonicalPath(dir) {
		what = "is your home folder"
	}
	return program.Name + " works in one project's folder, and " + dir + " " + what + "; " + instead
}

// holdsHomeFolder says dir is the person's home folder or a folder above it.
func holdsHomeFolder(dir string) bool {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" || strings.TrimSpace(dir) == "" {
		return false
	}
	rel, err := filepath.Rel(canonicalPath(dir), canonicalPath(home))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// DelegateUnknownError is the refusal for a `via` or a command naming no
// program this build carries. It names the ones it does, sorted, so the next
// attempt has the words in front of it.
type DelegateUnknownError struct {
	Named string
	Have  []string
}

func (e DelegateUnknownError) Error() string {
	if len(e.Have) == 0 {
		return "this codeaf carries no program called " + e.Named
	}
	have := append([]string(nil), e.Have...)
	sort.Strings(have)
	return "this codeaf carries no program called " + e.Named + "; it carries " + strings.Join(have, ", ")
}

// delegateFor resolves a name to the program, or the refusal.
func (a *Agent) delegateFor(name string) (delegate.Delegate, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return delegate.Delegate{}, errors.New("name the program to hand the work to")
	}
	for _, program := range a.config.Delegates {
		if program.Name == name {
			return program, nil
		}
	}
	return delegate.Delegate{}, DelegateUnknownError{Named: name, Have: a.config.delegateNames()}
}

// StartDelegate hands one person-authored brief to the named program. It is
// `/<name> <brief>`'s door and it answers what StartTask answers: the id the
// row wears, the title, a note about where the work stands (always empty here)
// and the error. Nothing is waited for: the run starts and the turn goes on.
//
// The refusals a person can meet, in their own words: a name this build
// carries no program for, an empty brief, and a build whose run road is not
// linked.
func (a *Agent) StartDelegate(ctx context.Context, name, brief string) (uint64, string, string, error) {
	brief = strings.TrimSpace(brief)
	if brief == "" {
		return 0, "", "", errors.New("/" + strings.TrimSpace(name) + " needs a brief: the whole task, in words")
	}
	program, err := a.delegateFor(name)
	if err != nil {
		return 0, "", "", err
	}
	if a.config.InTask {
		return 0, "", "", errors.New("a task cannot hand its work to " + program.Name + "; only the conversation can")
	}
	if refusal := programHomeRefusal(program, canonicalPath(a.config.Workspace), "open codeaf in that folder, or ask for the work in the chat and say which folder it is in"); refusal != "" {
		return 0, "", "", errors.New(refusal)
	}
	g := a.graph()
	if chatRunEngine == nil || g == nil || g.planPath() == "" {
		return 0, "", "", errors.New(program.Name + " needs the run road, and this build has none")
	}
	id := g.reserve()
	title := taskPersonTitle(brief)
	if err := a.startKnownTaskRunVia(ctx, id, title, brief, nil, delegateStand(a.config.Workspace, program), "", &program); err != nil {
		return 0, "", "", err
	}
	return id, title, "", nil
}

// delegateStand is where a program works. One that lands a tree gets a working
// copy of the folder, as every task does; one that lands text reads the
// person's folder in place and changes nothing, which is what it promises.
func delegateStand(workspace string, program delegate.Delegate) taskStand {
	if program.LandsTree() {
		return taskStand{dir: workspace, mode: TaskModeWorktree}
	}
	return taskStand{dir: workspace, mode: TaskModeInPlace}
}

// landDelegateRun is a delegated run's landing, in place of the engine's own.
//
// A TREE PROGRAM'S COMMITS ARE SQUASHED. senior-dev commits every edit as it goes
// (`wip(edit): <path>`, dozens a run), so the copy's branch holds bookkeeping
// history that is the program's own and nobody else's; the engine's landing
// would also find nothing to commit, because everything is already committed,
// and answer "nothing to land" over a tree full of work. So the copy is taken
// back to the commit it stood on when the program started — recorded on the run
// at that moment, so the point is exact whatever the ground ladder put under it
// — with the tree and index kept, and committed once through the same road every
// task commits through. The subject is the task's title; the body is the
// terminal record's two sentences. Then the copy comes home the way every run's
// copy does. The one exception is a task's branch holding commits of the
// program's that the tree it left was not built on: the tree is committed on
// top of those, never over them ([headMove.squashOnto]).
//
// A TREE PROGRAM ON A PLAIN FOLDER LANDS NOTHING EITHER: there was no history
// to copy from, so it worked in the folder itself and its changes are already
// there ([delegateOnPlainFolder]).
//
// A TEXT PROGRAM LANDS NOTHING: it worked in place and promised to change
// nothing, and its answer is the run's result, which the outcome note carries.
func (a *Agent) landDelegateRun(run *beltRun, summary RunSummary) RunLanding {
	m := run.delegate
	if m == nil || !m.LandsTree() || run.tree.dir == "" {
		return RunLanding{Home: mergeInPlace}
	}
	if run.plain {
		// A PLAIN FOLDER HAS NO HISTORY TO COMMIT TO, and the program worked in
		// it where it stands: its changes are already the person's, and the
		// landing is only the note that says where they are — and where the
		// program's own records went, which are not the person's.
		note := "its work is in " + run.ground + ", which has no git history, so nothing was committed"
		if kept := a.keepPlainFolderNotes(run); kept != "" {
			note += "; " + kept
		}
		if _, err := run.store.AddNote(run.root, run.root, note); err != nil {
			if g := a.graph(); g != nil {
				g.planNote("the run's landing note failed: " + err.Error())
			}
		}
		return RunLanding{Home: mergeInPlace}
	}
	dir := run.workspace
	// THE SQUASH LANDS ON CODEAF'S BRANCH, WHEREVER THE PROGRAM LEFT HEAD.
	moved := a.homeDelegateCopy(run)
	// THE BRANCH AS THE PROGRAM LEFT IT is what says whether it held any work,
	// and the landing below moves it, so it is kept for the branch-only
	// landing's emptiness question ([dropEmptyTaskBranch]).
	run.taskTip = moved.tip
	message := "task: " + clip(firstLine(run.title), 72)
	if result := strings.TrimSpace(summary.Result); result != "" {
		message += "\n\n" + result
	}
	saved, _, _, err := commitTaskWorkAs(dir, message, nil, a.signsGitWork(), true)
	landing := RunLanding{}
	switch {
	case err != nil:
		landing.Refused = firstLine(err.Error())
	case len(saved) == 0:
		landing.Refused = runNothingToLand
	default:
		landing.Branch, landing.Changed = currentBranch(dir), saved
		landing.Unrelated = moved.warns()
	}
	note := landing.Refused
	if note == "" {
		note = fmt.Sprintf("landed on %s: %s", landing.Branch, fileCount(len(landing.Changed)))
	}
	if said := moved.sentence(m.Name, run.tree.branch, landing.Refused == ""); said != "" {
		note += " · " + said
	}
	if _, err := run.store.AddNote(run.root, run.root, note); err != nil {
		if g := a.graph(); g != nil {
			g.planNote("the run's landing note failed: " + err.Error())
		}
	}
	return a.bringBeltRunHome(run, landing)
}

// headMove is what a tree program had done with its copy's HEAD by the time
// it ended, as [delegateHeadHome] found it: the branch it had moved to (empty
// with detached set for no branch at all), and whether its work stood on the
// commit the copy started from. The zero value is a HEAD that never left the
// task's branch.
//
// tip is the task's branch as the program left it, read before codeaf moved
// anything; kept says that branch held commits of the program's that the HEAD
// it left was not built on, so its work is committed on top of them rather
// than squashed over them ([headMove.squashOnto]).
type headMove struct {
	moved     bool
	from      string
	detached  bool
	unrelated bool
	tip       string
	kept      bool
}

// squashOnto is the commit a tree program's finished tree is committed on:
// the commit its copy started from, so the program's own bookkeeping commits
// fold into one, or the task's branch as the program left it when that branch
// holds commits the finished tree was not built on.
//
// THE PROGRAM'S COMMITS ARE NEVER SQUASHED OVER FROM ELSEWHERE. senior-dev
// commits every write on the task's branch; a model that then ran `git
// checkout --detach` to look at the baseline, and was ended there by a limit,
// had that branch reset back to the start under a tree that held none of its
// work, and the branch, then empty, deleted with the only reference to an
// hour of paid commits. Committed on top, every one of them stays on the
// task's branch, and the note says the finished tree may undo them.
func (move headMove) squashOnto(startSha string) string {
	if move.kept {
		return move.tip
	}
	return startSha
}

// warns says the landing's commit may also undo changes the branch held
// before it: work built on another commit than the copy's start, or on
// something other than the program's own commits on the task's branch.
func (move headMove) warns() bool {
	return move.unrelated || move.kept
}

// homeDelegateCopy puts a tree program's copy back on the task's own branch
// and takes that branch back to the commit its work is committed on
// ([headMove.squashOnto]), keeping the index and the files exactly as the
// program left them, so the one commit that follows holds the program's whole
// work. THE LANDING AND THE STOP BOTH TAKE IT: a stop that committed on
// whatever branch HEAD was on put codeaf's commit on the person's own branch
// while its report named the task's branch, which held nothing.
func (a *Agent) homeDelegateCopy(run *beltRun) headMove {
	dir := run.workspace
	move := delegateHeadHome(dir, run.tree.branch, run.startSha)
	onto := move.squashOnto(run.startSha)
	if onto == "" {
		return move
	}
	if head, err := git(dir, "rev-parse", "--verify", "-q", "HEAD"); err == nil && strings.TrimSpace(head) == onto {
		return move
	}
	if out, err := git(dir, "reset", "--soft", onto); err != nil {
		if g := a.graph(); g != nil {
			g.planNote(run.delegate.Name + "'s commits could not be squashed: " + firstLine(out))
		}
	}
	return move
}

// delegateHeadHome puts a tree program's copy back on the task's own branch
// before its work is squashed, and answers what it found ([headMove]).
//
// A PROGRAM'S SHELL CAN MOVE HEAD, AND ONE DID. A brief said "work on a new
// branch", and senior-dev ran `git checkout -b` four times in one run. The
// landing squashed and committed on whatever branch HEAD was on, while the row,
// the note and the carry home all named codeaf's task branch, which held
// nothing: the person was told their work was on a branch that was empty. And
// where the program had checked out one of the PERSON'S OWN branches, the
// squash's `reset --soft` moved that branch back to the copy's first commit,
// taking the person's own commits off it.
//
// `git symbolic-ref` moves HEAD alone: the index and the files stay exactly as
// the program left them, so the squash and the commit that follow land its
// finished tree on the task's branch, and the branch the program moved to is
// never reset by codeaf. Any commit the program made there stays on that
// branch, which the note says.
//
// A PROGRAM WHOSE WORK DID NOT STAND ON THE COPY'S FIRST COMMIT is said out
// loud too. The squash commits the program's finished tree over that commit,
// so work the program built on some other commit (a branch cut from `main`,
// say) also undoes whatever the copy's first commit had and that one did not,
// and the diff is the only place that would show.
//
// THE TASK'S BRANCH IS READ BEFORE HEAD MOVES ONTO IT. Where it holds commits
// past the copy's start that the HEAD the program left was not built on, those
// are the program's own work, and the squash must not reset over them
// ([headMove.squashOnto]).
func delegateHeadHome(dir, branch, startSha string) headMove {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return headMove{}
	}
	tip, _ := git(dir, "rev-parse", "--verify", "-q", "refs/heads/"+branch)
	stay := headMove{tip: strings.TrimSpace(tip)}
	current := currentBranch(dir)
	if current == branch {
		return stay
	}
	head, _ := git(dir, "rev-parse", "--verify", "-q", "HEAD")
	head = strings.TrimSpace(head)
	if _, err := git(dir, "symbolic-ref", "HEAD", "refs/heads/"+branch); err != nil {
		return stay
	}
	move := headMove{moved: true, from: current, detached: current == "", tip: stay.tip}
	if startSha != "" && head != "" {
		_, err := git(dir, "merge-base", "--is-ancestor", startSha, head)
		move.unrelated = err != nil
	}
	if move.tip != "" && move.tip != startSha {
		_, err := git(dir, "merge-base", "--is-ancestor", move.tip, head)
		move.kept = head == "" || err != nil
	}
	return move
}

// sentence is what the landing note adds about a HEAD the program had moved:
// where it had left the copy, where its work was committed when anything was,
// and the warning about work built on another commit. Empty for a HEAD that
// never moved. A landing that committed nothing says only where HEAD had been,
// because "its work was committed" would be a claim about a commit that does
// not exist.
func (move headMove) sentence(name, branch string, landed bool) string {
	if !move.moved {
		return ""
	}
	said := name + " had moved its copy to the branch " + move.from
	if move.detached {
		said = name + " had left its copy on no branch"
	}
	if landed {
		said += "; its work was committed on " + branch
	}
	if !move.detached {
		said += ", and any commit it made on " + move.from + " is still on that branch"
	}
	switch {
	case landed && move.kept:
		said += " · the commits it had made on " + branch + " are kept there, under its finished work; that work was not built on them, " +
			"so it may also undo their changes; read its diff before you merge it"
	case landed && move.unrelated:
		said += " · its work was not built on the commit its copy started from, so the commit on " + branch +
			" may also undo changes that commit had; read its diff before you merge it"
	}
	return said
}

// keepPlainFolderNotes moves a program's notes folder ([delegate.Delegate.Notes])
// out of the plain folder it worked in and into the task's own record folder,
// beside its conversation with codeaf, and answers the sentence that says
// where they went ("" when nothing moved).
//
// THE PERSON'S FOLDER GETS BACK ONLY THE WORK. A senior-dev run left 46 files
// in `.senior-dev/` there — its session database and its whole conversation
// with its model among them — and the manual's own advice for isolation next
// time, `git init` then `git add -A`, would have committed every one. A notes
// folder that was already there when the run began is left alone, because it
// is not this run's alone. A move across disks falls back to a copy and then a
// removal, and a move that fails leaves the folder where it was, whole.
func (a *Agent) keepPlainFolderNotes(run *beltRun) string {
	m := run.delegate
	if m == nil || m.Notes == "" || run.notesWereThere || run.tree.dir == "" {
		return ""
	}
	from := filepath.Join(run.tree.dir, m.Notes)
	if info, err := os.Lstat(from); err != nil || !info.IsDir() {
		return ""
	}
	taskDir := plandb.TaskDir(filepath.Dir(run.store.Path()), run.root)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return ""
	}
	to := filepath.Join(taskDir, m.Name)
	for n := 1; ; n++ {
		if _, err := os.Lstat(to); os.IsNotExist(err) {
			break
		}
		to = filepath.Join(taskDir, fmt.Sprintf("%s.%d", m.Name, n))
	}
	if err := os.Rename(from, to); err != nil {
		if err := copyPath(from, to); err != nil {
			_ = os.RemoveAll(to)
			if g := a.graph(); g != nil {
				g.planNote(m.Name + "'s notes could not be moved out of " + run.ground + ": " + err.Error())
			}
			return ""
		}
		_ = os.RemoveAll(from)
	}
	return "its notes (" + m.Notes + "/) are kept in " + to
}
