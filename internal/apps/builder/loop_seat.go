package builder

import (
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/draft"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// buildsDir holds one folder per build under the apps root. It sits beside the
// app folders and not inside them, so scratch files never reach a version, and
// its leading dot keeps the store from listing it as an app.
const buildsDir = ".builds"

// loopRootID names the root task of a build's plan store.
const loopRootID = "root"

// errNoRoot is why the loop road cannot start: it has no folder to build in.
var errNoRoot = errors.New("the Builder has no folder to build in")

// seat is one build's loop: the draft journal the worker's verbs write, the
// plan store its task lives in, and the feed that turns the journal into files
// and events. It lives as long as the build.
type seat struct {
	dir    string
	drafts *draft.Store
	tasks  *plandb.Store
	feed   *feed

	verbMu sync.Mutex // one server verb at a time
	mu     sync.Mutex
	live   string // id of the task a worker is on now, "" between tasks
}

func (s *Service) buildDir(app string) string { return filepath.Join(s.root, buildsDir, app) }

// openSeat opens the build's folder, journal and plan store once.
func (s *Service) openSeat(b *build) (*seat, error) {
	s.chooseTint(b)
	defaults := s.defaults(b) // read before b.mu is held: it reads the plan under that lock
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seat != nil {
		return b.seat, nil
	}
	if s.root == "" {
		return nil, errNoRoot
	}
	dir := s.buildDir(b.app)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	drafts, err := draft.Open(dir)
	if err != nil {
		return nil, err
	}
	tasks, err := plandb.Open(filepath.Join(dir, "plan.db"), "apps-"+b.app, loopRootID, "Build "+b.app, "")
	if err != nil {
		return nil, err
	}
	if err := drafts.SaveDefaults(defaults); err != nil {
		return nil, err
	}
	b.seat = &seat{dir: dir, drafts: drafts, tasks: tasks}
	b.seat.feed = newFeed(s, b, drafts)
	return b.seat, nil
}

func (t *seat) setLive(id string) {
	t.mu.Lock()
	t.live = id
	t.mu.Unlock()
}

// liveTask is the task a worker is on now, or "" when none is.
func (t *seat) liveTask() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.live
}

// loopTask is one task of the build: what it is called, what the worker is
// told, and the bounds it works within.
type loopTask struct {
	prefix string // the task id is prefix, or prefix-N for the Nth try
	title  string
	brief  string
	steps  int
	wall   time.Duration
}

// The bounds of the three tasks (DESIGN §7): a plan is short, a build is long,
// and a replan is as short as a plan.
const (
	planSteps, planWall   = 30, 4 * time.Minute
	buildSteps, buildWall = 140, 15 * time.Minute
)

// admit gives the task a claimed place in the plan store. A task a restart cut
// off is claimed again, so its worker opens on the resume clause; a task that
// ended gets the next id, because a finished task cannot run twice.
func (t *seat) admit(task loopTask) (*plandb.Task, error) {
	id := task.prefix
	for n := 2; ; n++ {
		existing := t.tasks.Task(id)
		if existing == nil {
			if _, err := t.tasks.AddMany([]plandb.TaskSpec{{ID: id, Title: task.title, Description: task.brief}}); err != nil {
				return nil, err
			}
			break
		}
		if !taskEnded(existing.Status) {
			break
		}
		id = fmt.Sprintf("%s-%d", task.prefix, n)
	}
	return t.claim(id)
}

func taskEnded(s plandb.Status) bool {
	return s == plandb.StatusDone || s == plandb.StatusFailed || s == plandb.StatusCancelled
}

// claim takes the task under its own name, the way the run engine does, so the
// ownership check `plandb done` answers is taken against that name. A task the
// last run left claimed is released first.
func (t *seat) claim(id string) (*plandb.Task, error) {
	if cur := t.tasks.Task(id); cur != nil && (cur.Status == plandb.StatusRunning || cur.Status == plandb.StatusClaimed) {
		if _, err := t.tasks.Release(id, id); err != nil {
			return nil, err
		}
	}
	return t.tasks.Claim(id, id)
}

// settle closes the task after its worker returned: a task the worker did not
// end is failed, or released when the build was cancelled so Write can start
// it again.
func (t *seat) settle(id string, workerErr error, cancelled bool) {
	cur := t.tasks.Task(id)
	if cur == nil || taskEnded(cur.Status) {
		return
	}
	switch {
	case cancelled:
		_, _ = t.tasks.Release(id, id)
	case workerErr != nil:
		_, _ = t.tasks.Fail(id, id, workerErr.Error())
	default:
		_, _ = t.tasks.Done(id, id, "", nil, nil)
	}
}

// liveCompleter builds the worker's provider seat from the same client
// configuration the Builder's single calls use: the same provider stack, key
// ladder and billing receipts.
func liveCompleter(model string) (session.Completer, error) {
	client, err := provider.NewClient(clientConfig(model))
	if err != nil {
		return nil, err
	}
	return client, nil
}

// runLoopTask runs one task's worker to its end. The journal is the truth about
// what the worker made, so the caller reads the draft afterwards and this only
// reports why it could not run (a cancelled build, the budget cap).
func (s *Service) runLoopTask(ctx context.Context, b *build, t *seat, task loopTask) error {
	claimed, err := t.admit(task)
	if err != nil {
		return err
	}
	completer, err := s.completerFor(b.model)
	if err != nil {
		t.settle(claimed.ID, err, false)
		return err
	}
	parent := ctx
	ctx, stopWorker := context.WithTimeout(ctx, task.wall)
	defer stopWorker()
	spend := &spendBank{b: b, stop: stopWorker}
	ctx = run.WithStepsPerTask(ctx, task.steps)
	ctx = run.WithSpendBank(ctx, spend.add)
	stopServing, err := draft.Serve(ctx, t.dir, s.verbHandler(b, t))
	if err != nil {
		t.settle(claimed.ID, err, false)
		return err
	}
	defer stopServing()
	stopTail := t.feed.watch(ctx)
	t.setLive(claimed.ID)
	rep, runErr := run.NewBashWorker(t.tasks, t.dir, b.model, "", completer).Run(ctx, *claimed)
	t.setLive("")
	stopTail()
	spend.settle(rep.USD)
	t.settle(claimed.ID, runErr, parent.Err() != nil)
	return b.withinBudget()
}

// spendBank charges the worker's calls to the build as they are paid for and
// stops the worker at the budget cap (rule 10). The worker reports its running
// total, so only the rise is charged.
type spendBank struct {
	b      *build
	stop   context.CancelFunc
	mu     sync.Mutex
	banked float64
}

func (sb *spendBank) add(total float64) {
	sb.mu.Lock()
	rise := total - sb.banked
	sb.banked = total
	sb.mu.Unlock()
	sb.b.charge(rise)
	if sb.b.withinBudget() != nil {
		sb.stop()
	}
}

// settle reconciles with the worker's final account, which can be higher than
// the last total it banked.
func (sb *spendBank) settle(final float64) {
	sb.mu.Lock()
	rise := final - sb.banked
	sb.banked += max(rise, 0)
	sb.mu.Unlock()
	if rise > 0 {
		sb.b.charge(rise)
	}
}

// brief composes what a task's worker is told. The teaching page is part of it
// so the first turn already knows the verbs.
func (s *Service) brief(lines ...string) string {
	return strings.Join(append(lines, "", prompt.AppBuilderPage()), "\n")
}

func (s *Service) planBrief(b *build) string {
	return s.brief(
		"TASK: PLAN. Plan the app below by typing codeaf app verbs, and stop at \"codeaf app plan ready\".",
		"The owner asked for: "+b.currentPlan().Ask,
		"The owner chose: "+budgetWords(b.budget)+"; the app agent runs on "+b.agentModel+connectorWords(b.connectors)+".",
		"The app lives in the "+personalSpace+" space. The catalog the screens may use: "+catalogText(),
		tintLine(b.tintChoice(), b.tintUsedBy()),
		"When the plan is ready, end the task with plandb done.")
}

func (s *Service) buildBrief(b *build) string {
	plan, _ := planJSON(b.currentPlan())
	return s.brief(
		"TASK: BUILD. Build the whole app from the plan below. The plan already holds every change the owner made.",
		"The draft may already hold pieces from an earlier try: read it with \"codeaf app show\" before you add to it.",
		"The owner may change a plan line while you work; a note on this task then tells you which. Follow it.",
		"The plan:", string(plan),
		"The app lives in the "+personalSpace+" space. Budget: "+budgetWords(b.budget)+"; the app agent runs on "+b.agentModel+connectorWords(b.connectors)+".",
		"The catalog the screens may use: "+catalogText())
}

func (s *Service) replanBrief(request string) string {
	return s.brief(
		"TASK: REPLAN. The owner asks for a change to the plan: "+request,
		"Read the plan with \"codeaf app show plan\", change the plan lines to fit, and end with \"codeaf app plan ready\".")
}

func connectorWords(connectors []string) string {
	if len(connectors) == 0 {
		return ""
	}
	return "; connectors: " + strings.Join(connectors, ", ")
}

// planTaskID is the first try of the plan task; later tries add a number.
const planTaskID = "plan"

func planTask(brief string) loopTask {
	return loopTask{prefix: planTaskID, title: "Plan the app", brief: brief, steps: planSteps, wall: planWall}
}

func buildTask(brief string) loopTask {
	return loopTask{prefix: "build", title: "Build the app", brief: brief, steps: buildSteps, wall: buildWall}
}

func replanTask(brief string) loopTask {
	return loopTask{prefix: "replan", title: "Change the plan", brief: brief, steps: planSteps, wall: planWall}
}
