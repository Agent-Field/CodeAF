package session

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/provider"
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
	// again is a redo of a crew that never started: the next-best models at
	// the same cost, because nothing ran to be too weak.
	again *crewroute.Decision
}

type crewWishKey struct{}

// withCrewWish hands a start door the crew wish; an empty wish is no value.
func withCrewWish(ctx context.Context, wish crewWish) context.Context {
	if wish.effort == "" && wish.stronger == nil && wish.again == nil {
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
	call    string
	title   string
	brief   string
	repo    string
	costUSD float64
	settled string

	// mu guards the three fields under it, which the run's completer moves
	// from the engine's goroutines while the row and the log read them.
	mu       sync.Mutex
	decision crewroute.Decision
	// started are the send ids that have answered a call on this task: a seat
	// whose model has answered once has started, and a later refusal is the
	// work's to handle, not a reason to change the crew.
	started map[string]bool
	// swaps are the send ids the engine asks for and where their seat moved.
	swaps map[string]string
	// original is each seat's send id as routed — what the engine asks for —
	// and ladders each unpinned seat's rungs as routed.
	original map[crewroute.Seat]string
	ladders  map[crewroute.Seat][]crewroute.Pick
	// bad are routes that failed here; broke are accounts that ran out of
	// credit here; gone are providers whose key was refused here.
	bad, broke, gone map[string]bool
}

// current is the crew as it stands, fallbacks taken.
func (c *taskCrew) current() crewroute.Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.decision
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
		Effort: wish.effort, Stronger: wish.stronger, Again: wish.again, Repo: repo,
		ChatModel: a.Model(),
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
		original: map[crewroute.Seat]string{}, ladders: decision.Ladder,
	}
	for _, pick := range decision.Crew {
		crew.original[pick.Seat] = pick.Send
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
	config.LogCrewOutcome(a.config.ProfileDir, crew.call, crew.current(), crew.repo, crew.title, outcome, costUSD)
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
	prior := crew.current()
	// A CREW THAT NEVER STARTED IS ASKED AGAIN, NOT ESCALATED: no seat answered
	// a call, so nothing ran to be too weak, and the redo takes the next-best
	// models at the same cost ([crewroute.Request.Again]).
	wish := crewWish{stronger: &prior}
	if !crew.anyStarted() {
		wish = crewWish{again: &prior}
	}
	id, title, _, err := a.startTaskRun(withCrewWish(ctx, wish), crew.brief, true, "")
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
	config.LogCrewOutcome(a.config.ProfileDir, crew.call, crew.current(), crew.repo, crew.title, router.CrewRedone, cost)
	return id, title, nil
}

// StartTaskEffort is [Agent.StartTask] with the one-task effort word said:
// `/task --best` and `/task --cheap`. The word moves this task's crew and
// nothing after it.
func (a *Agent) StartTaskEffort(ctx context.Context, brief string, solo bool, effort string) (uint64, string, string, error) {
	word, _ := crewroute.ParseEffort(effort)
	return a.StartTask(withCrewWish(ctx, crewWish{effort: word}), brief, solo)
}

// ── A SEAT WHOSE MODEL FAILS TO START: THE DEGRADATION LADDER ──────────────
//
// A routed model can be listed, priced and allowed and still refuse the first
// request a task sends it: an account out of credit, a key refused, a route
// that will not take this model, a limit reached, a model withdrawn. The task
// used to end there, in its first second, on a crew nobody chose. Now the
// failure is read into one kind (internal/provider's RouteFailure), written to
// the router's log as the ROUTE's outcome ([config.LogCrewRoute]) — which is
// what route health is learned from — and the seat walks its ladder, inside
// the task:
//
//  1. the same model on its next healthy route;
//  2. the next qualified models for the seat at a similar cost;
//  3. the seat as the last crew that completed a task here ran it;
//  4. the model the person is talking to, when it can sit the seat.
//
// The first two are the router's ([crewroute.Decision.Ladder]); the last two
// only this install knows ([config.CrewRescue]). A rung on a route this task
// has already seen fail — or on an account it has seen run out of credit, or a
// provider whose key was refused — is stepped over. With nothing left the call
// ends on ONE action read off the kind ("add credit on …", "reconnect …",
// "the limit resets at …"), never on the refusal's own text.
//
// A transient failure — a timeout, one 5xx — is asked again once first.
// ONLY A FIRST CALL COUNTS: a route that has answered once on this task has
// started, and a later error is the work's own to handle. A cancelled context
// is somebody stopping the work. A PINNED SEAT IS NEVER MOVED — the person
// chose it — and its failure ends on the same one action.

// crewSeatCompleter is the run's completer with the ladder in front. It asks
// through the one model door ([Agent.completeWithModel]) — the account-aware
// road [modelRoutingCompleter] takes — naming the seat's current send.
type crewSeatCompleter struct {
	agent *Agent
	run   *beltRun
}

func (c crewSeatCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	crew := c.run.crew
	var request ai.Request
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	key := strings.TrimSpace(request.Model)
	if key == "" {
		key = c.agent.Model()
	}
	retried := false
	for {
		current := crew.sendFor(key)
		response, err := c.agent.completeWithModel(ctx, purposeInherited, messages, current, options...)
		if err == nil {
			c.agent.crewAnswered(c.run, current)
			return response, nil
		}
		if ctx.Err() != nil || crew.hasStarted(current) {
			return response, err
		}
		kind, until := provider.RouteFailureOf(err)
		if kind == "" {
			return response, err
		}
		if kind == provider.RouteTransient && !retried {
			retried = true
			continue
		}
		retried = false
		if action, moved := c.agent.moveCrewSeat(c.run, key, current, kind, until); !moved {
			if action == "" {
				return response, err
			}
			return response, crewStopped{action: action, cause: err}
		}
	}
}

// crewStopped is a seat with nowhere left to go: its text is the one action,
// and the refusal underneath stays reachable for whoever reads it.
type crewStopped struct {
	action string
	cause  error
}

func (e crewStopped) Error() string { return e.action }
func (e crewStopped) Unwrap() error { return e.cause }

// sendFor is the send id a request for key goes to now: key, or where the
// seat has moved.
func (c *taskCrew) sendFor(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if to := c.swaps[key]; to != "" {
		return to
	}
	return key
}

// hasStarted is whether a send id has answered a call on this task.
func (c *taskCrew) hasStarted(send string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.started[send]
}

// anyStarted is whether any seat answered a call on this task.
func (c *taskCrew) anyStarted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.started) > 0
}

// crewAnswered notes that a send id answered its first call on this task, and
// writes the route's success to the router's log once.
func (a *Agent) crewAnswered(run *beltRun, send string) {
	crew := run.crew
	crew.mu.Lock()
	if crew.started[send] {
		crew.mu.Unlock()
		return
	}
	if crew.started == nil {
		crew.started = map[string]bool{}
	}
	crew.started[send] = true
	decision := crew.decision
	crew.mu.Unlock()
	for _, seat := range crewroute.Seats {
		if pick := decision.Seat(seat); pick.Send == send {
			config.LogCrewRoute(a.config.ProfileDir, crew.call, decision, crew.repo, crew.title, seat, pick, "", time.Time{})
		}
	}
}

// moveCrewSeat moves every unpinned seat whose calls go to key, and which is
// on current, one rung down its ladder. It answers false and the one action
// when there is nowhere to go.
func (a *Agent) moveCrewSeat(run *beltRun, key, current string, kind provider.RouteFailure, until time.Time) (string, bool) {
	crew := run.crew
	crew.mu.Lock()
	provided := a.crewFailedLocked(crew, current, kind, until)
	decision, next := crew.decision, crewroute.Pick{}
	for _, seat := range crewroute.Seats {
		pick := decision.Seat(seat)
		if pick.Send != current || pick.Pinned || crew.original[seat] != key {
			continue
		}
		rung, ok := crew.nextRung(append(append([]crewroute.Pick(nil), crew.ladders[seat]...),
			config.CrewRescue(a.config.ProfileDir, decision.Class, seat, a.Model())...))
		if !ok {
			continue
		}
		decision = decision.WithRung(seat, rung, crewWhy(kind, provided))
		if rung.Kind == crewroute.Free && !strings.Contains(decision.Note, crewFreeNotice) {
			decision.Note = strings.TrimSpace(decision.Note + " " + crewFreeNotice)
		}
		if next.Send == "" {
			next = rung
		}
	}
	if next.Send == "" {
		action := crewActionFor(kind, provided, until)
		stopped := crewStopsOn(kind)
		if stopped {
			// THE CAUSE IS KNOWN, SO THE LINE SAYS THE ONE THING TO DO — and
			// not "/redo stronger", which no stronger crew on the same
			// account could make work.
			decision.Stopped = action
			crew.decision = decision
		}
		crew.mu.Unlock()
		if stopped {
			a.republishCrew(run, decision, current+" could not start · "+action)
		}
		return action, false
	}
	if crew.swaps == nil {
		crew.swaps = map[string]string{}
	}
	crew.swaps[key], crew.decision = next.Send, decision
	crew.mu.Unlock()
	a.republishCrew(run, decision, fmt.Sprintf("%s could not start (%s) · moved to %s", current, crewWhy(kind, provided), next.Send))
	return "", true
}

// crewFailedLocked writes a failed first call to the router's log for every
// seat on current, and marks what this task must not ask again: the route, an
// account out of credit, a provider whose key was refused. It answers the
// provider the route was on. The caller holds crew.mu.
func (a *Agent) crewFailedLocked(crew *taskCrew, current string, kind provider.RouteFailure, until time.Time) string {
	var provided string
	for _, seat := range crewroute.Seats {
		if pick := crew.decision.Seat(seat); pick.Send == current {
			provided = pick.Provider
			config.LogCrewRoute(a.config.ProfileDir, crew.call, crew.decision, crew.repo, crew.title, seat, pick, string(kind), until)
		}
	}
	if crew.bad == nil {
		crew.bad, crew.broke, crew.gone = map[string]bool{}, map[string]bool{}, map[string]bool{}
	}
	crew.bad[current] = true
	switch kind {
	case provider.RoutePayment:
		crew.broke[provided] = true
	case provider.RouteAuth:
		crew.gone[provided] = true
	}
	return provided
}

// republishCrew puts a moved crew on the run's row, so the card's crew line
// says it, and leaves a note on the plan.
func (a *Agent) republishCrew(run *beltRun, decision crewroute.Decision, note string) {
	g := a.graph()
	if g == nil {
		return
	}
	for _, kept := range g.runRows(run.row) {
		if kept.ID != run.row {
			continue
		}
		kept.Crew = &decision
		kept.Model = decision.Seat(crewroute.Worker).Model
		a.publishRunRow(g, kept)
		break
	}
	g.planNote(note)
}

// nextRung is the first rung this task has not seen fail: not a route that
// failed here, not a paid route on an account that ran out of credit here, not
// a provider whose key was refused here. The caller holds c.mu.
func (c *taskCrew) nextRung(ladder []crewroute.Pick) (crewroute.Pick, bool) {
	for _, rung := range ladder {
		switch {
		case rung.Send == "" || c.bad[rung.Send] || c.gone[rung.Provider]:
		case rung.Kind == crewroute.Metered && c.broke[rung.Provider]:
		default:
			return rung, true
		}
	}
	return crewroute.Pick{}, false
}

// crewFreeNotice is what the crew line says when a seat fell to a free pool:
// the pool may keep what it is sent.
const crewFreeNotice = "free routes in use · free routes may log prompts"

// crewStopsOn is whether a seat with nowhere left to go stops the task on its
// one action: a failure whose cause a person can fix (credit, a key, a
// limit), where a stronger crew would meet the same wall.
func crewStopsOn(kind provider.RouteFailure) bool {
	switch kind {
	case provider.RoutePayment, provider.RouteAuth, provider.RouteQuota:
		return true
	}
	return false
}

// crewWhy is a failure as the crew line says why a seat moved.
func crewWhy(kind provider.RouteFailure, on string) string {
	if on == "" {
		on = "its provider"
	}
	switch kind {
	case provider.RoutePayment:
		return "credit unavailable on " + on
	case provider.RouteAuth:
		return "key refused on " + on
	case provider.RouteForbidden:
		return "not available on " + on
	case provider.RouteQuota:
		return "limit reached on " + on
	case provider.RouteUnavailable:
		return "not served on " + on
	}
	return on + " not answering"
}

// crewActionFor is the one thing a person can do when a seat has nowhere left
// to go, read off what the route said.
func crewActionFor(kind provider.RouteFailure, on string, until time.Time) string {
	if on == "" {
		on = "the provider"
	}
	switch kind {
	case provider.RoutePayment:
		return "add credit on " + on + " to continue"
	case provider.RouteAuth:
		return "reconnect " + on + " with /connect"
	case provider.RouteQuota:
		if !until.IsZero() {
			return "the limit on " + on + " resets at " + until.Local().Format("15:04") + " — try again then"
		}
		return "the limit on " + on + " is reached — try again later"
	}
	return "no model could start this seat · widen /crew models or pin one with /crew"
}
