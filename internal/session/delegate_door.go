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
// store, the supervisor and the row are the run's, and nothing in them reads
// the belt switch. What a delegated run does not have is a copy — a program
// that edits files works in the folder itself, on a branch of its own in a
// repository (programfolder.go) — or the review round, because a check seat is
// a bash-belt worker and the belt may be off; the program's own verification is
// what its terminal record reports.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
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
// files — that it works in the folder itself, and so which folder it must be
// handed — is codeaf's mechanics, and is said here once ([delegateFolderRule]).
// That nobody can be asked anything is `propose_task`'s own `brief`
// description, and that small work is never handed off is this section's
// own; neither is said a second time here.
//
// AND CODEAF PREFERS A PROGRAM FOR THE WORK IT IS FOR. The paragraph said a
// large task "can" go to one, which is a permission, and a permission loses
// to the two roads the rest of the section teaches: an issue in a mature
// project is worked through inline, or handed to codeaf's own worker, which
// has none of a program's machinery for that work. The owner's call of
// 2026-09-24 is that codeaf reach for a program by itself on complicated,
// many-sided coding work, and almost always when the person asks for one. So
// the first sentence is a preference over both of those roads, the program's
// guide under it says which work that is, and WHATEVER ITS CRITICAL PATH is
// said because the section's own test for a hand-off is the critical path:
// one hard fix has a single one, and a model holding that test alone would
// keep the fix. The second sentence is the person's ask, which outranks the
// section's floor on small work; code holds both halves of it, where a prompt
// would be forgotten (delegate_asked.go).
//
// THE TWO SENTENCES STAND APART FROM THE FOLDER RULE, which is spliced in
// after them, so either can be reworded without touching the other.
var delegateFact = beltFact{
	tools: []string{"propose_task"},
	holds: Config.mayDelegate,
	present: "AND WORK A PROGRAM BUILT INTO CODEAF IS FOR GOES TO IT WHOLE, named in\n" +
		"`propose_task`'s `via`, rather than to you or a worker, whatever its critical path;\n" +
		"so does work the person asks one for, by name or as `/name`.%s The programs here:\n%s",
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
// cloned it into the person's own projects folder and edited it there, and the
// task ended saying it had changed nothing. The program works in the folder
// the proposal names ([PrepareProgramFolder]), so the folder is the one thing
// the model has to get right, and fetching a repository that is not here is
// its job, done before the proposal.
//
// AND IT PROMISES NO MERGE, because there is none: in a repository the work is
// left on the program's own branch, checked out in that folder, and bringing
// it into the person's branch is a separate step the landing's line names.
const delegateFolderRule = "\nIt works in the task's folder itself, on a branch of its own in a repository, so hand\n" +
	"it the repository the work belongs in: clone one this machine lacks into a new folder,\n" +
	"at the commit the work names, and pass it as `ground`. Never brief it to work elsewhere."

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

// delegateReceipt is the sentence an approved hand-off to a program adds to
// its receipt: who has the work, where, and where it will be when it ends.
// ground is the folder the run was started on and record the run's own record
// of it, written as the run started ([runCopyOf]): the program's branch, and
// the person's branch it was cut from. They are read off the record and not off
// the live run, which a program that dies in its first second has already
// left by the time the receipt is written.
//
// IT NEVER SAYS THE WORK LANDS. A program's work is left on its own branch and
// merged by nobody; a model that read "lands" told the person their branch
// held work it did not.
//
// IT NAMES THE FOLDER. A receipt that said "a copy" and "the folder itself"
// without saying which let a model that had named ~/Desktop/pong read that its
// program was there while it had been handed the person's home folder.
//
// AND IT SAYS THE FOLDER IS THE PROGRAM'S UNTIL IT ENDS ([programHoldGuard]).
// The receipt's next sentence invites the model to carry on with other work,
// and a model that did it by writing into the program's folder was refused one
// file at a time; told once here, it works elsewhere or waits.
func delegateReceipt(ground string, via delegate.Delegate, record *TaskCopyRecord) string {
	if !via.LandsTree() {
		return "It is " + via.Name + "'s: it works alone, and its answer arrives when it ends."
	}
	return delegateFolderReceipt(ground, via, record) + " Until it ends, codeaf's own tools write nothing in " + ground + "."
}

// delegateFolderReceipt is where a program that edits files works, as its
// receipt says it ([delegateReceipt]).
//
// A FOLDER WITH NO BRANCH IS NOT ALWAYS A FOLDER WITH NO HISTORY. One inside a
// repository whose root holds the home folder — a dotfiles repository — is
// worked in without git because codeaf will not cut a branch there, and a
// receipt that said it "has no git history" had the chat telling the person so,
// or advising `git init` inside their dotfiles; it names the repository, the
// way the run's ending does ([ProgramFolderEnd.Sentence]).
func delegateFolderReceipt(ground string, via delegate.Delegate, record *TaskCopyRecord) string {
	if record == nil || record.Branch == "" {
		if _, _, outer, _ := programFolderOf(ground); outer != "" {
			return "It is " + via.Name + "'s: it works alone in " + ground + " itself, inside the git repository at " + outer +
				", which holds your home folder, so codeaf cuts no branch there and commits nothing; its changes are there as it makes them."
		}
		return "It is " + via.Name + "'s: it works alone in " + ground + " itself, which has no git history, so its changes are there as it makes them."
	}
	folder := ProgramFolder{Home: record.Home, Start: record.HomeSha}
	stays := folder.homeWords() + " does not move"
	// THE PROMISE IS READ BEFORE IT IS MADE ([ProgramFolder.homeMoved]): a
	// branch of the person's that has already moved from where the run was
	// cut is said to have, rather than promised to stay. One that cannot be
	// read at all, or a record that never wrote down where it stood, is a
	// branch this receipt cannot hold up against anything, and keeps the
	// promise the run itself keeps.
	if tip := branchCommit(ground, record.Home); tip != "" && record.HomeSha != "" && tip != record.HomeSha {
		stays = "your branch " + record.Home + " has already moved, from " + shortSha(record.HomeSha) + " to " + shortSha(tip) +
			", and codeaf does not move it"
	}
	on := "on a new branch " + record.Branch
	if record.Continues {
		on = "carrying on on its branch " + record.Branch + ", where the last run left it"
	}
	return "It is " + via.Name + "'s: it works alone in " + ground + " itself, " + on + "; " +
		stays + ", and when it ends " + record.Branch + " stays checked out there with its work."
}

// runRowCopy is the record a run's row was published with as it started
// ([runCopyOf]): where it works, and a program's branch. Nil when there is
// none.
func (a *Agent) runRowCopy(id uint64) *TaskCopyRecord {
	g := a.graph()
	if g == nil {
		return nil
	}
	if kept, ok := runRowOf(g, id); ok {
		return kept.Copy
	}
	return nil
}

// programPlace is where a program works, in a person's words: the folder
// itself, on a branch of its own when it is a repository and the program
// edits code.
func programPlace(program delegate.Delegate, ground string) string {
	if !program.LandsTree() || !hasGitHistory(ground) {
		return ground
	}
	return ground + ", on a branch of its own"
}

// hasGitHistory says a program handed ground works there on a branch of its
// own: ground is in a repository with a commit, whose root is below the home
// folder. It is the one reading of "a branch or not" ([programFolderOf]), so
// the card, the receipt and the run cannot disagree about it.
func hasGitHistory(ground string) bool {
	_, repo, _, _ := programFolderOf(ground)
	return repo
}

// delegateStartedReceipt is an approved hand-off's receipt: a task's first line
// and its wake sentence, with the program's own account of where it works
// ([delegateReceipt]) in place of a task's "in a copy of its own", which a
// program never is.
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
	folder := canonicalPath(a.config.Workspace)
	if refusal := programHomeRefusal(program, folder, "open codeaf in that folder, or ask for the work in the chat and say which folder it is in"); refusal != "" {
		return 0, "", "", errors.New(refusal)
	}
	g := a.graph()
	if chatRunEngine == nil || g == nil || g.planPath() == "" {
		return 0, "", "", errors.New(program.Name + " needs the run road, and this build has none")
	}
	id := g.reserve()
	title := taskPersonTitle(brief)
	note := ""
	if program.Name == "senior-dev" {
		note = a.seniorDevCeilings(a.Usage().CostUSD).Summary()
	}
	if err := a.startKnownTaskRunVia(ctx, id, title, brief, nil, delegateStand(folder), "", &program); err != nil {
		return 0, "", "", err
	}
	return id, title, note, nil
}

// delegateStand is where a program works: the folder itself, always. One that
// edits files is readied there by [PrepareProgramFolder], on a branch of its
// own in a repository; one that lands text reads the person's folder and
// changes nothing, which is what it promises.
func delegateStand(folder string) taskStand {
	return taskStand{dir: folder, mode: TaskModeInPlace}
}

// landDelegateRun is a delegated run's landing, in place of the engine's own:
// the program's folder finished per the contract ([ProgramFolder.Finish]) —
// what it left uncommitted committed on its branch with the run's ending as
// the commit's body, or a run that changed nothing undone — and the landing
// that says where the work is ([ProgramFolderEnd.landing]).
//
// A TEXT PROGRAM LANDS NOTHING: it worked in place and promised to change
// nothing, and its answer is the run's result, which the outcome note carries.
func (a *Agent) landDelegateRun(run *beltRun, summary RunSummary) RunLanding {
	if run.folder == nil {
		return RunLanding{Home: mergeInPlace}
	}
	if err := SetProgramAnswerAttribution(run.folder, a.signsGitWork().named); err != nil {
		if g := a.graph(); g != nil {
			g.planNote("the program's answered models could not be read: " + err.Error())
		}
	}
	outcome, result := runEndingWords(summary)
	if result == "" {
		result = outcome
	}
	return run.folder.Finish(result).landing()
}
