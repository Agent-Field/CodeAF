package session

// THE DELEGATE DOOR: how a conversation hands a task to an outside program
// (docs/design/delegate/DESIGN.md, docs/DELEGATE-PROTOCOL.md). A delegate is
// one more worker kind behind the run engine, and this file is the half a
// conversation needs of it — which delegates this launch has, the door
// `/<name> <brief>` and `propose_task`'s `via` both open, and the landing of a
// run whose worker was a program rather than a bash worker.
//
// IT RIDES THE RUN ROAD WHATEVER THE BELT SAYS. `/task` takes the run road only
// under CODEAF_TASK_BELT=bash, because that road's WORKER is the bash belt. A
// delegate's worker is the program, so the road is asked for outright here: the
// store, the copy, the supervisor and the landing are the run's, and nothing in
// them reads the belt switch. What a delegated run does not have is the review
// round, because a check seat is a bash-belt worker and the belt may be off; the
// program's own verification is what the terminal record reports.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// DelegateRow is one delegate as a surface lists it: the command word, the
// sentence under it, and what it leaves behind.
type DelegateRow struct {
	Name        string
	Description string
	// Lands is delegate.LandsTree or delegate.LandsText.
	Lands string
	// Bin is the program as it resolved on this machine.
	Bin string
}

// DelegateReport is everything `/delegate` says: the delegates that can run,
// the ones whose program is not here (one dim line each), and the files the
// loader would not admit (one line each, with the reason).
type DelegateReport struct {
	Rows    []DelegateRow
	Absent  []string
	Refused []string
}

// Delegates is the report for this conversation. A build with no registry
// answers the zero report, which a surface draws as one sentence.
func (a *Agent) Delegates() DelegateReport { return a.config.delegateReport() }

func (c Config) delegateReport() DelegateReport {
	var report DelegateReport
	if c.Delegates == nil {
		return report
	}
	for _, m := range c.Delegates.All() {
		lands := m.Lands
		if lands == "" {
			lands = delegate.LandsTree
		}
		report.Rows = append(report.Rows, DelegateRow{Name: m.Name, Description: m.Description, Lands: lands, Bin: m.BinPath})
	}
	for _, absent := range c.Delegates.Absent() {
		report.Absent = append(report.Absent, absent.String())
	}
	for _, refusal := range c.Delegates.Refusals() {
		report.Refused = append(report.Refused, refusal.String())
	}
	return report
}

// delegateNames is the runnable names, sorted, for the prompt and the refusal.
func (c Config) delegateNames() []string {
	if c.Delegates == nil {
		return nil
	}
	return c.Delegates.Names()
}

// mayDelegate says whether this belt may hand work to a delegate: it is the
// conversation's own hand-off predicate with one more condition, that this
// launch has at least one delegate that can run. A task node never delegates,
// for the reason it never proposes: there is nowhere for the work to go from
// there.
func (c Config) mayDelegate() bool {
	return c.mayProposeTask() && !c.InTask && len(c.delegateNames()) > 0
}

// delegateFact is the hand-off page's one paragraph about delegates. It is
// rendered only where [Config.mayDelegate] holds, and its `fill` writes the
// installed names in, so the model is told the words it can put in `via` and
// never a name this machine does not have.
var delegateFact = beltFact{
	tools: []string{"propose_task"},
	holds: Config.mayDelegate,
	present: "AND WORK BIG ENOUGH TO WANT ITS OWN AGENT FOR AN HOUR — one large change, specified\n" +
		"well enough that nobody will be asked anything — can go to a DELEGATE: an outside\n" +
		"program on this machine that does the whole task on its own, in a copy of the folder,\n" +
		"under the same dollar and time limits, landed when it ends. Name it in `propose_task`'s\n" +
		"`via`. The delegates here are: %s. A delegate cannot ask the person anything, so its\n" +
		"brief has to settle everything; a change you would do in a few steps is never worth one.",
	fill: func(config Config, text string) string {
		return fmt.Sprintf(text, strings.Join(config.delegateNames(), ", "))
	},
}

// chatManual is the manual this conversation answers from: the packed corpus,
// with every installed delegate's own page layered over it under
// `delegate-<name>` (internal/manual's overlay). It is what makes "what does
// /senior-dev do" answerable from senior-dev's page and nowhere else, and it is built
// once per agent because the registry is read once per launch.
func (a *Agent) chatManual() *manual.Corpus {
	a.manualOnce.Do(func() {
		a.manualCorpus = manual.Chat().WithPages(a.config.Delegates.Pages())
	})
	return a.manualCorpus
}

// DelegateUnknownError is the refusal for a `via` naming no delegate this
// machine can run. It names the ones it can, sorted, so the next attempt has
// the words in front of it.
type DelegateUnknownError struct {
	Named string
	Have  []string
}

func (e DelegateUnknownError) Error() string {
	if len(e.Have) == 0 {
		return "no delegate is called " + e.Named + ": this machine has no delegates (a manifest under ~/.codeaf/delegates adds one)"
	}
	have := append([]string(nil), e.Have...)
	sort.Strings(have)
	return "no delegate is called " + e.Named + "; the delegates here are " + strings.Join(have, ", ")
}

// delegateFor resolves a `via` word to its manifest, or the refusal.
func (a *Agent) delegateFor(name string) (delegate.Manifest, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return delegate.Manifest{}, errors.New("a delegate needs a name")
	}
	if a.config.Delegates != nil {
		if m, ok := a.config.Delegates.Find(name); ok {
			return m, nil
		}
	}
	return delegate.Manifest{}, DelegateUnknownError{Named: name, Have: a.config.delegateNames()}
}

// StartDelegate hands one person-authored brief to the named delegate. It is
// `/<name> <brief>`'s door and it answers what StartTask answers: the id the
// row wears, the title, a note about where the work stands (always empty here)
// and the error. Nothing is waited for: the run starts and the turn goes on.
//
// The refusals a person can meet, in their own words: a name this machine has
// no delegate for, an empty brief, and a build whose run road is not linked.
func (a *Agent) StartDelegate(ctx context.Context, name, brief string) (uint64, string, string, error) {
	brief = strings.TrimSpace(brief)
	if brief == "" {
		return 0, "", "", errors.New("a delegate needs a brief")
	}
	m, err := a.delegateFor(name)
	if err != nil {
		return 0, "", "", err
	}
	if a.config.InTask {
		return 0, "", "", errors.New("a task cannot hand its work to a delegate; only the conversation can")
	}
	g := a.graph()
	if chatRunEngine == nil || g == nil || g.planPath() == "" {
		return 0, "", "", errors.New("delegates need the run road, and this build has none")
	}
	id := g.reserve()
	title := taskPersonTitle(brief)
	if err := a.startKnownTaskRunVia(ctx, id, title, brief, nil, delegateStand(a.config.Workspace, m), "", &m); err != nil {
		return 0, "", "", err
	}
	return id, title, "", nil
}

// delegateStand is where a delegate works. A program that lands a tree gets a
// working copy of the folder, as every task does; one that lands text reads the
// person's folder in place and changes nothing, which is what its manifest
// promised.
func delegateStand(workspace string, m delegate.Manifest) taskStand {
	if m.LandsTree() {
		return taskStand{dir: workspace, mode: TaskModeWorktree}
	}
	return taskStand{dir: workspace, mode: TaskModeInPlace}
}

// landDelegateRun is a delegated run's landing, in place of the engine's own.
//
// A TREE DELEGATE'S COMMITS ARE SQUASHED. senior-dev commits every edit as it goes
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
// A TEXT DELEGATE LANDS NOTHING: it worked in place and promised to change
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
					g.planNote("the delegate's commits could not be squashed: " + firstLine(out))
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
