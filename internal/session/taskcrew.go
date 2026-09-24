package session

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/router"
)

// A TASK'S CREW IS PICKED FOR THAT TASK, AT THE MOMENT IT STARTS.
//
// The /crew panel says what is allowed and what is pinned, and that persists;
// the words in the ask say how hard to try THIS task, and nothing else sticks.
// So every run this conversation starts asks the router (internal/crewroute,
// through [Config.RouteCrew]) for its worker, planner and checker, with the
// task's own title and brief to classify and the one-task effort word the
// person or the conversation said — `/task --best`, `/task --cheap`, or the
// `effort` field of a hand-off. The decision rides the run's spec into the
// engine's crew factory, rides its row to the surface (the card's crew line),
// and is written to the router's log twice: once when it is made, once when
// the task settles, which is what the router learns from.
//
// THE ROUTER IS OPTIONAL HERE AND NOTHING IS INVENTED WITHOUT IT. A session no
// door gave a router — a test, the harness — runs its tasks on its role
// ladder's seats the way it always has; the crew is a surface's decision to
// wire, never a model this package names.
//
// A HAND-OFF THAT JOINS A RUN ALREADY UNDERWAY RIDES THAT RUN'S CREW. A run is
// one crew factory, seated once; the joined work is a child of its root.

// crewWish is what a hand-off asked of its crew: the one-task effort word, and
// for a redo the crew it is asking to beat. It travels on the context the start
// door is handed, because that door has half a dozen callers and every one of
// them but two asks nothing of the crew.
type crewWish struct {
	effort   crewroute.Effort
	stronger *crewroute.Decision
}

type crewWishKey struct{}

// withCrewWish hands a start door the crew wish; an empty wish is no value.
func withCrewWish(ctx context.Context, wish crewWish) context.Context {
	if wish.effort == "" && wish.stronger == nil {
		return ctx
	}
	return context.WithValue(ctx, crewWishKey{}, wish)
}

func crewWishOf(ctx context.Context) crewWish {
	wish, _ := ctx.Value(crewWishKey{}).(crewWish)
	return wish
}

// taskCrew is one task's crew as this conversation remembers it: enough to
// settle its log row and to redo it stronger.
type taskCrew struct {
	call     string
	decision crewroute.Decision
	title    string
	brief    string
	repo     string
	costUSD  float64
	settled  string
}

// crewBook is the conversation's crews by row. It is a small map guarded by
// its own lock, so neither the belt's locks nor the agent's are ever held
// across a router call.
type crewBook struct {
	mu   sync.Mutex
	rows map[uint64]*taskCrew
}

func (b *crewBook) put(row uint64, crew *taskCrew) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rows == nil {
		b.rows = map[uint64]*taskCrew{}
	}
	b.rows[row] = crew
}

func (b *crewBook) get(row uint64) *taskCrew {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rows[row]
}

// latest is the newest row with a crew — the task `/redo stronger` means when
// it names none.
func (b *crewBook) latest() (uint64, *taskCrew) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var best uint64
	for row := range b.rows {
		if row > best {
			best = row
		}
	}
	return best, b.rows[best]
}

// routeTaskCrew asks the router for one task's crew. It answers nil and no
// error when this session has no router — the ladder's seats then stand.
//
// AT THE DAILY CAP THE TASK DOES NOT START, and the refusal says the cap, what
// was spent and the two ways on: raise it, or say `--cheap`. A conversation
// has nobody to answer a yes/no at the moment a hand-off is committed, and a
// cap that spends anyway is not a cap.
func (a *Agent) routeTaskCrew(ctx context.Context, row uint64, title, brief string) (*taskCrew, error) {
	route := a.config.RouteCrew
	if route == nil {
		return nil, nil
	}
	wish := crewWishOf(ctx)
	repo := canonicalPath(a.config.Workspace)
	decision, err := route(config.CrewAsk{
		Task:   crewroute.Task{Text: strings.TrimSpace(title + "\n\n" + brief)},
		Effort: wish.effort, Stronger: wish.stronger, Repo: repo,
	})
	if errors.Is(err, config.ErrCrewAtCap) {
		spent, capUSD := config.CrewHistory(a.config.ProfileDir).SpentUSD, config.CrewCapAt(a.config.ProfileDir)
		return nil, fmt.Errorf("today's crew spend (%s) has reached the daily cap of %s · raise it with /crew cap, or ask for this task with --cheap",
			crewroute.Money(spent), crewroute.Money(capUSD))
	}
	if err != nil {
		return nil, err
	}
	crew := &taskCrew{
		call: router.CrewCallID(a.journalID() + ":" + strconv.FormatUint(row, 10)), decision: decision,
		title: title, brief: brief, repo: repo,
	}
	a.crews.put(row, crew)
	config.LogCrewDecision(a.config.ProfileDir, crew.call, decision, repo, title)
	return crew, nil
}

// settleTaskCrew writes a task's crew outcome once: accepted when the work
// came home, not kept otherwise. A redo overwrites it later with its own row,
// and the log reads the last row for a call as the truth.
func (a *Agent) settleTaskCrew(row uint64, outcome string, costUSD float64) {
	crew := a.crews.get(row)
	if crew == nil || a.config.RouteCrew == nil {
		return
	}
	a.crews.mu.Lock()
	if crew.settled != "" {
		a.crews.mu.Unlock()
		return
	}
	crew.settled, crew.costUSD = outcome, costUSD
	a.crews.mu.Unlock()
	config.LogCrewOutcome(a.config.ProfileDir, crew.call, crew.decision, crew.repo, crew.title, outcome, costUSD)
}

// ErrNoCrewToRedo is `/redo stronger` in a conversation that has started no
// routed task.
var ErrNoCrewToRedo = errors.New("no task here to redo · /redo stronger follows a task this conversation started")

// RedoStronger runs a task again with a stronger crew: every seat nobody
// pinned steps up one ([crewroute.Decide] with Stronger set), and the router's
// log is told the first crew under-served this class here, so the next task of
// the class in this repository starts a step higher until enough accepted
// work decays it back. row 0 means the newest task this conversation started.
//
// IT IS ONE TASK'S ASK AND NOTHING STICKS: the pins and the allowed rule are
// the panel's, untouched. A crew already at the top answers
// [crewroute.ErrStrongest] in words, and nothing starts.
func (a *Agent) RedoStronger(ctx context.Context, row uint64) (uint64, string, error) {
	var crew *taskCrew
	if row == 0 {
		row, crew = a.crews.latest()
	} else {
		crew = a.crews.get(row)
	}
	if crew == nil {
		return 0, "", ErrNoCrewToRedo
	}
	// A RUN STILL GOING IS JOINED, NOT REDONE: a hand-off meeting a live run
	// becomes a child of it and rides its crew, which is the one thing a redo
	// is asking not to happen.
	a.beltMu.Lock()
	live := a.beltRun != nil
	a.beltMu.Unlock()
	if live {
		return 0, "", errors.New("the work is still running · stop it first, then /redo stronger")
	}
	prior := crew.decision
	id, title, _, err := a.startTaskRun(withCrewWish(ctx, crewWish{stronger: &prior}), crew.brief, true, "")
	if err != nil {
		if strings.Contains(err.Error(), crewroute.ErrStrongest.Error()) {
			return 0, "", errors.New("this crew is already the strongest allowed · pin a stronger model with /crew pin, or widen /crew models")
		}
		return 0, "", err
	}
	// THE LESSON IS WRITTEN ONLY ONCE THE STRONGER CREW IS REALLY GOING: a
	// redo refused at the top of the ladder taught nothing about the class.
	a.crews.mu.Lock()
	crew.settled = router.CrewRedone
	cost := crew.costUSD
	a.crews.mu.Unlock()
	config.LogCrewOutcome(a.config.ProfileDir, crew.call, crew.decision, crew.repo, crew.title, router.CrewRedone, cost)
	return id, title, nil
}

// StartTaskEffort is [Agent.StartTask] with the one-task effort word said:
// `/task --best` and `/task --cheap`. The word moves this task's crew and
// nothing after it.
func (a *Agent) StartTaskEffort(ctx context.Context, brief string, solo bool, effort string) (uint64, string, string, error) {
	word, _ := crewroute.ParseEffort(effort)
	return a.StartTask(withCrewWish(ctx, crewWish{effort: word}), brief, solo)
}
