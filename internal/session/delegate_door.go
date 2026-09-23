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
// names in, so the model is told the words it can put in `via` and never a name
// this build does not carry.
var delegateFact = beltFact{
	tools: []string{"propose_task"},
	holds: Config.mayDelegate,
	present: "AND WORK BIG ENOUGH TO WANT ITS OWN AGENT FOR AN HOUR — one large change, specified\n" +
		"well enough that nobody will be asked anything — can go to a PROGRAM BUILT INTO CODEAF\n" +
		"that does the whole task on its own, in a copy of the folder, under the same dollar and\n" +
		"time limits, landed when it ends. Name it in `propose_task`'s `via`. The programs here\n" +
		"are: %s. It cannot ask the person anything, so its brief has to settle everything;\n" +
		"a change you would do in a few steps is never worth one.",
	fill: func(config Config, text string) string {
		return fmt.Sprintf(text, strings.Join(config.delegateNames(), ", "))
	},
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
// copy does.
//
// A TEXT PROGRAM LANDS NOTHING: it worked in place and promised to change
// nothing, and its answer is the run's result, which the outcome note carries.
func (a *Agent) landDelegateRun(run *beltRun, summary RunSummary) RunLanding {
	m := run.delegate
	if m == nil || !m.LandsTree() || run.tree.dir == "" {
		return RunLanding{Home: mergeInPlace}
	}
	dir := run.workspace
	if run.startSha != "" {
		head, err := git(dir, "rev-parse", "HEAD")
		if err == nil && strings.TrimSpace(head) != run.startSha {
			if out, err := git(dir, "reset", "--soft", run.startSha); err != nil {
				if g := a.graph(); g != nil {
					g.planNote(m.Name + "'s commits could not be squashed: " + firstLine(out))
				}
			}
		}
	}
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
	}
	note := landing.Refused
	if note == "" {
		note = fmt.Sprintf("landed on %s: %d files", landing.Branch, len(landing.Changed))
	}
	if _, err := run.store.AddNote(run.root, run.root, note); err != nil {
		if g := a.graph(); g != nil {
			g.planNote("the run's landing note failed: " + err.Error())
		}
	}
	return a.bringBeltRunHome(run, landing)
}
