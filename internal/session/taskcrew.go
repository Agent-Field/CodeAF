package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	// worker is a model the hand-off named for this task, which seats the
	// worker as a one-task pin ([Agent.routeTaskCrew]).
	worker string
	// sends are one-task pins a person chose per role for this task — a rerun
	// from Pick models — laid over the worker named above (taskrouting.go).
	sends map[crewroute.Seat]string
	// taskCap is a task cap a person explicitly raised for this one task.
	taskCap float64
	// autoSeats are roles a person asked Auto for, this task only, over a
	// saved pin or one-model mode (Best, a stronger worker).
	autoSeats map[crewroute.Seat]bool
}

type crewWishKey struct{}

// withCrewWish hands a start door the crew wish; an empty wish is no value.
func withCrewWish(ctx context.Context, wish crewWish) context.Context {
	if wish.effort == "" && wish.stronger == nil && wish.again == nil && wish.worker == "" && len(wish.sends) == 0 && wish.taskCap == 0 && len(wish.autoSeats) == 0 {
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
	persistMu sync.Mutex
	persist   func() error
	inFlight  int

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
	rescue   map[crewroute.Seat][]crewroute.Pick
	// bad are routes that failed here; broke are accounts that ran out of
	// credit here; gone are providers whose key was refused here.
	bad, broke, gone map[string]bool
	// guard holds every seat call to the day's cap and the checker to its
	// own ceiling (spendguard.go); nil when nothing prices the crew.
	guard *SpendGuard
	// freeTried is how many free pools each seat has been moved onto in this
	// task: at most crewFreePools, so a task on an account out of credit
	// whose every free pool is at its limit stops on its action in seconds,
	// not after walking every pool the catalog lists.
	freeTried map[crewroute.Seat]int
	// failed is how each send that failed its first call on this task failed:
	// it is not asked again on this task, by any seat.
	failed map[string]crewFailure
	// helperUSD is what helper calls made for this task cost — its run's
	// closing summary — added to the seats' spend on the crew line.
	helperUSD float64
	// day and dayAtStart are the conversation's guarded day and what it had
	// spent when this task was routed: a helper the surface asked for before
	// the run was registered (its first summary) carries no task, and is on
	// this task's line through the day's own figure.
	day        *SpendDay
	dayAtStart float64

	// changes are this task's model changes, oldest first and bounded
	// ([taskCrewChangesKept]); changeCount counts every one ever written.
	changes     []TaskCrewChange
	changeCount int
	// taskPins are the roles a person pinned for this task alone; pending
	// are roles whose person-made change has not answered a call yet.
	taskPins map[crewroute.Seat]bool
	pending  map[crewroute.Seat]bool
	// restored is every time this crew came back from its checkpoint.
	restored     []time.Time
	restoreCount int
	// seatStarted are the roles that have had a call answered on this task,
	// marked by the role that made it. roleFacts is a crew whose per-role
	// facts (these, and the role tallies) were kept from its admission; a crew
	// restored from an older checkpoint has none, and its role use is unknown.
	seatStarted map[crewroute.Seat]bool
	roleFacts   bool
	// seatSends are roles a person moved on this task, by role rather than by
	// the send id the engine asks for, so two roles that share a model can
	// part ([Agent.TaskRoutingAct]).
	seatSends map[crewroute.Seat]string
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
// AT THE DAILY CAP THE TASK DOES NOT START, whatever word it carries — `--cheap`
// included, because a dollar cap is a cap — and the refusal says the cap, what
// was spent and the ways on that work: raise or turn off /crew cap, or wait for
// the day to turn over at midnight. A conversation has nobody to answer a
// yes/no at the moment a hand-off is committed, and a cap that spends anyway is
// not a cap.
func (a *Agent) routeTaskCrew(ctx context.Context, row uint64, title, brief string) (*taskCrew, error) {
	route := a.config.RouteCrew
	if route == nil {
		return nil, nil
	}
	wish := crewWishOf(ctx)
	repo := canonicalPath(a.config.Workspace)
	ask := config.CrewAsk{
		Task:   crewroute.Task{Text: strings.TrimSpace(title + "\n\n" + brief)},
		Effort: wish.effort, Stronger: wish.stronger, Again: wish.again, Repo: repo, AutoSeats: wish.autoSeats,
		ChatModel: a.Model(),
	}
	// A MODEL NAMED FOR THE TASK SEATS ITS WORKER, as a one-task pin: the one
	// the hand-off named, else the `task model` row. That is the ladder the
	// proposal card, the receipt and the manual state ([Agent.defaultTaskModel]
	// minus its last two rungs, which are the crew's own answer), and it used
	// to stop at the card while the router seated whatever it picked.
	if worker := a.namedTaskWorker(wish.worker); worker != "" {
		ask.Sends = map[crewroute.Seat]string{crewroute.Worker: worker}
	}
	for seat, send := range wish.sends {
		if send = strings.TrimSpace(send); send != "" {
			if ask.Sends == nil {
				ask.Sends = map[crewroute.Seat]string{}
			}
			ask.Sends[seat] = send
		}
	}
	decision, err := route(ask)
	if errors.Is(err, config.ErrCrewAtCap) {
		spent, capUSD := config.CrewHistory(a.config.ProfileDir).SpentUSD, config.CrewCapAt(a.config.ProfileDir)
		return nil, fmt.Errorf("today's crew spend (%s) has reached the daily cap of %s · raise it or turn it off with /crew cap, or wait until midnight",
			crewroute.Money(spent), crewroute.Money(capUSD))
	}
	if err != nil {
		return nil, err
	}
	crew := &taskCrew{
		roleFacts: true,
		call:      router.CrewCallID(a.journalID() + ":" + strconv.FormatUint(row, 10)), decision: decision,
		title: title, brief: brief, repo: repo,
		original: map[crewroute.Seat]string{}, ladders: decision.Ladder,
	}
	for _, pick := range decision.Crew {
		crew.original[pick.Seat] = pick.Send
	}
	crew.rescue = map[crewroute.Seat][]crewroute.Pick{}
	// ONE MODEL FOR EVERY ROLE IS THE PERSON'S CHOICE, and no role moves off
	// it on its own: no rescue rung is kept for a decision admitted in it.
	if decision.Mode != crewroute.ModeOne {
		for _, seat := range crewroute.Seats {
			crew.rescue[seat] = config.CrewRescue(a.config.ProfileDir, decision.Class, seat, a.Model())
		}
	}
	for seat := range wish.sends {
		if crew.taskPins == nil {
			crew.taskPins = map[crewroute.Seat]bool{}
		}
		crew.taskPins[seat] = true
	}
	crew.guard = crewSpendGuard(a.config.ProfileDir, decision, true)
	if wish.taskCap > crew.guard.TaskCap && crew.guard.TaskCap > 0 {
		crew.guard.TaskCap = wish.taskCap
	}
	crew.guard.Day = a.crewDay()
	crew.day, crew.dayAtStart = a.crewDay(), a.crewDay().Total()
	a.crews.put(row, crew)
	config.LogCrewDecision(a.config.ProfileDir, crew.call, decision, repo, title)
	return crew, nil
}

// namedTaskWorker is the model somebody named for a task's worker: named, when
// the hand-off named one, else the `task model` row resolved the way a
// proposal resolves it, else nothing — and nothing is a worker the router
// picks.
func (a *Agent) namedTaskWorker(named string) string {
	if named = strings.TrimSpace(named); named != "" {
		return named
	}
	a.mu.Lock()
	configured := strings.TrimSpace(a.config.TaskModel)
	a.mu.Unlock()
	if configured == "" {
		return ""
	}
	if candidates := matchTaskModel(configured, a.taskModelList()); len(candidates) == 1 {
		return candidates[0]
	}
	return configured
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
	if err := a.CheckWork(); err != nil {
		return 0, "", err
	}
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
	ctx = withTaskSource(ctx, a.graph().sourceOfID(row))
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
	if err := crew.beginCall(); err != nil {
		return nil, err
	}
	defer crew.endCall()
	key, err := c.seatKey(options)
	if err != nil {
		return nil, err
	}
	if !crew.seats(key) {
		if !c.agent.config.freeHelperAllowed(key) {
			return nil, crewSeatEnd(freeRoutesOffAction, nil)
		}
		// NOT A SEAT OF THIS CREW: a tier the run asks for that the crew does
		// not name (a probe, a small errand). It is an auxiliary call, and it
		// goes through the route-health guard like every other one — never
		// to a route this task's crew was routed around.
		// It is a call of this task, so it counts against the task's limit.
		return c.agent.completeWithModel(withCrewTask(ctx, crew), purposeInherited, messages, key, options...)
	}
	return c.completeSeat(ctx, key, messages, options)
}

// completeSeat owns retries and fallback movement for an identified crew seat.
// Its caller keeps the durable in-flight checkpoint around the whole loop.
func (c crewSeatCompleter) completeSeat(ctx context.Context, key string, messages []ai.Message, options []ai.Option) (*ai.Response, error) {
	crew := c.run.crew
	retried := false
	seat := crewSeatOf(ctx)
	for {
		current := crew.sendForSeat(seat, key)
		// A REFUSAL ITS CREDENTIAL HAS SINCE ANSWERED is let go here, at the
		// call that would ask it again, and nowhere else: this is the explicit
		// ask, and nothing is sent because a refusal was lifted.
		c.agent.liftAnsweredRefusals(c.run, current)
		if c.freeRouteBlocked(ctx, current) {
			return nil, crewSeatEnd(freeRoutesOffAction, nil)
		}
		if failed, ok := crew.failedHere(current); ok {
			// A SEND THAT FAILED ON THIS TASK IS NOT ASKED AGAIN, by this seat
			// or any other: a pool that answered 429 with an hour's
			// Retry-After is rested for the task, and the seat walks on — or
			// ends on its action — without another request.
			if action, moved := c.agent.moveCrewSeat(c.run, key, current, failed.kind, failed.until); !moved {
				return nil, crewSeatEnd(action, failed.err)
			}
			continue
		}
		held, err := crew.guard.before(ctx, current, messages, options)
		if err != nil {
			// THE CALL THAT WOULD CROSS THE LINE IS NOT MADE, and the line
			// says which line it met — never "/redo stronger".
			return nil, c.agent.stopCrew(c.run, err)
		}
		// THE ROLE HAS STARTED the moment its call is admitted and about to
		// leave — not when the provider answers — and only that role.
		crew.seatStarting(seat)
		// A SEAT NEVER WAITS OUT A LIMIT: a 429 goes back at once, and the
		// seat moves to its next route or model ([provider.WithoutPatientRateLimits]).
		callCtx := provider.WithDiscardedUsage(ctx, func(_ string, _ string, response *ai.Response) {
			crew.guard.after(ctx, current, response, 0)
		})
		// THIS CALL'S OWN DISPATCH FACT, told from the header each attempt was
		// written with ([withDispatchObserver]): the LAST told is the attempt
		// that failed, whichever key an earlier attempt carried.
		sent := new(lastSent)
		callCtx = withDispatchObserver(callCtx, sent.tell)
		response, err := c.agent.completeWithModel(provider.WithoutPatientRateLimits(asCrewSeatCall(callCtx)), purposeInherited, messages, current, options...)
		failedAt := time.Now()
		crew.guard.after(ctx, current, response, held)
		if err == nil {
			c.agent.crewAnswered(c.run, current, seat)
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
		crew.noteFailed(current, crew.failureSent(current, crewFailure{kind: kind, until: until, err: err, at: failedAt}, sent.read()))
		if action, moved := c.agent.moveCrewSeat(c.run, key, current, kind, until); !moved {
			if action == "" {
				return response, err
			}
			return response, crewStopped{action: action, cause: err}
		}
	}
}

// lastSent is the dispatch fact of the newest request one seat call wrote. A
// streamed request is written on the stream's own goroutine, so it is read
// under its own lock.
type lastSent struct {
	mu   sync.Mutex
	fact sentConnection
}

func (l *lastSent) tell(fact sentConnection) {
	l.mu.Lock()
	l.fact = fact
	l.mu.Unlock()
}

func (l *lastSent) read() sentConnection {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fact
}

// seatKey is the send id a seat call names — the seat as routed — or the
// conversation's model when it names none.
func (c crewSeatCompleter) seatKey(options []ai.Option) (string, error) {
	var request ai.Request
	for _, option := range options {
		if err := option(&request); err != nil {
			return "", err
		}
	}
	if key := strings.TrimSpace(request.Model); key != "" {
		return key, nil
	}
	return c.agent.Model(), nil
}

// crewFailure is how a send failed its first call on this task.
type crewFailure struct {
	kind  provider.RouteFailure
	until time.Time
	err   error
	// at is when the failure came back, and provider the provider the send
	// was routed to. sent is the print of the credential the failed call was
	// SENT with, as the adapter printed it from the header of the last attempt
	// it wrote ([withDispatchObserver]); empty when nothing could say. A failure
	// restored from an older checkpoint has none of the three, and an auth or
	// payment refusal without them is never lifted by this task.
	at       time.Time
	provider string
	sent     config.CrewDispatch
}

// failureSent is failure with what is known about the call that failed: the
// provider send is routed to and, when the door's fact is about that same
// provider, the credential print it was sent with. A fact about another
// provider's account — a seat the pool moved onto another service — names
// nothing about this provider's credential, and is left unprinted.
func (c *taskCrew) failureSent(send string, failure crewFailure, fact sentConnection) crewFailure {
	c.mu.Lock()
	failure.provider = c.providerOfLocked(send)
	c.mu.Unlock()
	if failure.provider == "" {
		failure.provider = fact.provider
	}
	if fact.provider != "" && fact.provider == failure.provider {
		failure.sent = fact.sent
	}
	return failure
}

// providerOfLocked is the provider send is routed to on this crew: a seat's
// pick, a rung of its ladder or rescue, or the provider a change recorded it
// moving from. Lowercased, as the router names providers; "" when this crew
// never named one for it. The caller holds c.mu.
func (c *taskCrew) providerOfLocked(send string) string {
	named := func(pick crewroute.Pick) bool { return pick.Send == send && pick.Provider != "" }
	for _, pick := range c.decision.Crew {
		if named(pick) {
			return strings.ToLower(pick.Provider)
		}
	}
	for _, rungs := range []map[crewroute.Seat][]crewroute.Pick{c.ladders, c.rescue} {
		for _, seat := range crewroute.Seats {
			for _, rung := range rungs[seat] {
				if named(rung) {
					return strings.ToLower(rung.Provider)
				}
			}
		}
	}
	for i := len(c.changes) - 1; i >= 0; i-- {
		switch change := c.changes[i]; {
		case change.FromSend == send && change.FromProvider != "":
			return strings.ToLower(change.FromProvider)
		case change.ToSend == send && change.ToProvider != "":
			return strings.ToLower(change.ToProvider)
		}
	}
	return ""
}

// crewCredentialRefusal is a failure that belongs to the credential it was
// made with: a refused key, or an account out of credit.
func crewCredentialRefusal(kind provider.RouteFailure) bool {
	return kind == provider.RouteAuth || kind == provider.RoutePayment
}

// ── A REFUSAL ITS CREDENTIAL HAS ANSWERED ──────────────────────────────────
//
// A send refused for its key or its credit was held for the rest of the task:
// failed, bad, and its provider gone or broke — and a pinned role, or the one
// model, never moves, so reconnecting or adding credit could not reach a task
// already running or carried on. Route health had learned to lift such a
// refusal on a fact (internal/config's crewcred.go); this task's own record of
// it did not, and held the pin regardless.
//
// NOW THE SAME FACTS LIFT IT HERE, judged at the call that would ask the send
// again and never on a timer or in the background:
//
//   - route health no longer holds the refusal against the profile
//     ([config.CrewRefusalApplies] — a different known credential than the
//     one it was sent with, a credential change after an unprinted one, or
//     credit read after a payment refusal), AND
//   - the very connection the next call would be handed — the pool's own,
//     which can lag the profile ([Agent.nextSentConnection]) — carries the
//     credential in force, with a credential that is there; and for a
//     refused key, it is not the key that was refused.
//
// ONLY THE REFUSALS THOSE FACTS ANSWER ARE LET GO: a rate limit, a route gone,
// a cap, another provider's refusal, and a refusal on the same provider the
// facts do not answer (an older checkpoint's, with no time and no print) all
// stand, and while one stands its provider stays gone or broke. The pick is
// not changed, nothing is moved onto Auto or a rescue, the history, the call
// counts and the spend are kept exactly as they were, and no success is
// written: the next call either answers on the repaired credential, or fails
// and is held again on its own fact.

// liftAnsweredRefusals lets go of the auth and payment refusals on send's
// provider that its credential has since answered, when send's own is one of
// them. The facts are read with crew.mu released — they are files — and the
// failures are let go only if they are still the ones read.
func (a *Agent) liftAnsweredRefusals(run *beltRun, send string) {
	crew := run.crew
	on, asked := crew.credentialRefusalsOn(send)
	if len(asked) == 0 {
		return
	}
	answered, why := a.answeredRefusals(on, send, asked)
	if why == "" {
		return
	}
	crew.mu.Lock()
	before := crew.decision.Stopped
	crew.liftLocked(on, answered)
	decision := crew.decision
	crew.mu.Unlock()
	if decision.Stopped != before {
		a.republishCrew(run, decision, send+" may be asked again · "+why)
	}
}

// credentialRefusalsOn is, when send's own failure is an auth or payment
// refusal on a provider it names, that provider and every such refusal on it
// this task holds — send's included — copied out from under c.mu. A refusal
// that names no provider, an older checkpoint's, is answered by nothing here.
func (c *taskCrew) credentialRefusalsOn(send string) (string, map[string]crewFailure) {
	c.mu.Lock()
	defer c.mu.Unlock()
	failure, ok := c.failed[send]
	if !ok || c.started[send] || !crewCredentialRefusal(failure.kind) || failure.provider == "" {
		return "", nil
	}
	asked := map[string]crewFailure{}
	for other, f := range c.failed {
		if f.provider == failure.provider && !c.started[other] && crewCredentialRefusal(f.kind) {
			asked[other] = f
		}
	}
	return failure.provider, asked
}

// answeredRefusals judges each refusal in asked on its own facts, and answers
// the ones that no longer hold with the fact that answered send's — "" when
// send's own still holds.
func (a *Agent) answeredRefusals(on, send string, asked map[string]crewFailure) (map[string]crewFailure, string) {
	var inForce config.CrewDispatch
	if on != "codex" {
		inForce = config.CrewDispatchFor(a.config.ProfileDir, on)
	}
	answered := map[string]crewFailure{}
	why := ""
	for other, f := range asked {
		because := a.refusalAnswered(other, on, f, inForce)
		if because == "" {
			continue
		}
		answered[other] = f
		if other == send {
			why = because
		}
	}
	return answered, why
}

// refusalAnswered is the fact that answers one auth or payment refusal of send
// on provider on, for the call that would be made next — "" while it holds.
func (a *Agent) refusalAnswered(send, on string, failure crewFailure, inForce config.CrewDispatch) string {
	if config.CrewRefusalApplies(a.config.ProfileDir, router.CrewRouteOutcome{
		At: failure.at, Send: send, Provider: on, Kind: string(failure.kind), Cred: failure.sent.Cred,
	}) {
		return ""
	}
	next := a.nextSentConnection(send)
	if next.provider != on {
		return ""
	}
	if next.sent.Cred == "" {
		// AN UNPRINTED NEXT CONNECTION is a sign-in whose transport carries
		// the tokens of the moment ([sentConnectionOf]), which the profile's
		// own facts describe; on any other provider it is a connection with
		// no credential at all, and a disconnected key repairs nothing.
		if on != "codex" {
			return ""
		}
		return on + " was signed in again after it refused"
	}
	if next.sent != inForce {
		// The pool would send a credential the profile no longer holds: the
		// facts that lifted the refusal are about another credential.
		return ""
	}
	switch {
	case failure.sent.Cred == "":
		return "the credential on " + on + " changed after it refused"
	case next.sent != failure.sent:
		return "a different credential is connected on " + on
	case failure.kind == provider.RoutePayment:
		return "credit on " + on + " was read after it refused"
	}
	return ""
}

// liftLocked lets go of the answered refusals that are still as they were
// read, and keeps on gone or broke while any other refusal of that kind on it
// stands. A stop the refused credential set is said again from what is left,
// and dropped when nothing is; any other stop — a spend line met — is
// untouched. The caller holds c.mu.
func (c *taskCrew) liftLocked(on string, answered map[string]crewFailure) {
	credentialStop := c.stoppedOnCredentialLocked()
	if !c.dropAnsweredLocked(answered) {
		return
	}
	gone, broke := c.stillRefusedLocked(on)
	if !gone {
		forgetProvider(c.gone, on)
	}
	if !broke {
		forgetProvider(c.broke, on)
	}
	if credentialStop {
		c.decision.Stopped = ""
		if len(c.broke) > 0 || len(c.gone) > 0 {
			c.decision.Stopped = c.actionLocked("", "", time.Time{})
		}
	}
}

// stoppedOnCredentialLocked is whether the crew's stop is the one its refused
// keys and empty accounts say ([taskCrew.actionLocked]). The caller holds c.mu.
func (c *taskCrew) stoppedOnCredentialLocked() bool {
	if c.decision.Stopped == "" || (len(c.broke) == 0 && len(c.gone) == 0) {
		return false
	}
	return c.decision.Stopped == c.actionLocked("", "", time.Time{})
}

// dropAnsweredLocked forgets each answered refusal that is still the failure
// that was judged — a newer failure of the send is its own fact — and answers
// whether any went. The caller holds c.mu.
func (c *taskCrew) dropAnsweredLocked(answered map[string]crewFailure) bool {
	dropped := false
	for send, was := range answered {
		now, ok := c.failed[send]
		if !ok || now.kind != was.kind || !now.at.Equal(was.at) || now.sent != was.sent || now.provider != was.provider {
			continue
		}
		delete(c.failed, send)
		delete(c.bad, send)
		dropped = true
	}
	return dropped
}

// stillRefusedLocked is whether a key refusal, and an out-of-credit refusal,
// still stand on provider on — counting a refusal whose provider this crew
// cannot name, which may be on. The caller holds c.mu.
func (c *taskCrew) stillRefusedLocked(on string) (gone, broke bool) {
	for send, f := range c.failed {
		p := f.provider
		if p == "" {
			p = c.providerOfLocked(send)
		}
		if p != "" && p != on {
			continue
		}
		gone = gone || f.kind == provider.RouteAuth
		broke = broke || f.kind == provider.RoutePayment
	}
	return gone, broke
}

// forgetProvider drops on from a set of providers, as they were written.
func forgetProvider(set map[string]bool, on string) {
	for provided := range set {
		if strings.EqualFold(provided, on) {
			delete(set, provided)
		}
	}
}

// noteFailed remembers how send failed on this task.
func (c *taskCrew) noteFailed(send string, failure crewFailure) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failed == nil {
		c.failed = map[string]crewFailure{}
	}
	c.failed[send] = failure
}

// failedHere is how send failed on this task, if it did and never answered.
func (c *taskCrew) failedHere(send string) (crewFailure, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	failure, ok := c.failed[send]
	return failure, ok && !c.started[send]
}

// crewFinal is whether err is a crew's own last word — a seat with nowhere
// left to go, a call a spend line stopped, a route resting — which ends the
// turn it reaches rather than being retried as the failure under it.
func crewFinal(err error) bool {
	var stopped crewStopped
	var spend ErrSpendStopped
	var resting errRouteResting
	return errors.As(err, &stopped) || errors.As(err, &spend) || errors.As(err, &resting)
}

// crewSeatEnd is the error a seat with nowhere left to go ends on: its one
// action when the failures name one, the failure itself otherwise.
func crewSeatEnd(action string, cause error) error {
	if action == "" {
		return cause
	}
	return crewStopped{action: action, cause: cause}
}

// crewSeatCallKey marks a context as a crew seat's own call, which walks its
// own ladder and must reach exactly the route it names ([Agent.healthyModel]).
type crewSeatCallKey struct{}

func asCrewSeatCall(ctx context.Context) context.Context {
	return context.WithValue(ctx, crewSeatCallKey{}, true)
}

func isCrewSeatCall(ctx context.Context) bool {
	on, _ := ctx.Value(crewSeatCallKey{}).(bool)
	return on
}

// healthyModel is the model an auxiliary call asks: model, unless route
// health says model's route will not answer, in which case the router's
// healthy pick ([config.CrewHealthySend]). THE PERSON'S TURN, THEIR OWN
// MODEL AND A CREW SEAT'S CALL ARE NEVER REROUTED: the first two are the
// person's choice, and a seat walks its own ladder and says so on its line.
func (a *Agent) healthyModel(ctx context.Context, purpose callPurpose, model string) string {
	// AN EMPTY ProfileDir IS THE DEFAULT PROFILE, not no profile: every
	// ordinary launch leaves it empty (config.ProfileDir is the override
	// variable) and every read below resolves it to the home directory.
	if a.config.RouteCrew == nil || isCrewSeatCall(ctx) || strings.TrimSpace(model) == "" {
		return model
	}
	// ONLY THE PERSON'S OWN TURN ON THEIR OWN MODEL is theirs to send where
	// they chose. An errand that falls back to the conversation's model —
	// a run summary, a title — is still an errand, and is not sent to a route
	// that refused that model a minute ago.
	chat := a.Model()
	if purpose == purposeTurn && model == chat {
		return model
	}
	return config.CrewHealthySend(a.config.ProfileDir, model, chat)
}

// routeGate is the provider's last word on a route ([provider.Config.RouteGate]):
// a body naming a model whose route health says will not answer is not sent,
// WHICHEVER ROAD IT CAME BY — the door's own reroute ([Agent.healthyModel])
// runs first, and this is what holds when a road skipped it. Two calls pass:
// a crew seat's own, which walks its ladder and probes an account the task
// is routed to, and the person's turn, whose model is theirs. send is the id
// the client was built for and wire how it is spelled on the wire.
func (c Config) routeGate(send, wire string) func(context.Context, string, string) error {
	if c.RouteCrew == nil {
		return nil
	}
	dir := c.ProfileDir
	return func(ctx context.Context, model, tag string) error {
		if isCrewSeatCall(ctx) || tag == string(purposeTurn) {
			return nil
		}
		asked := strings.TrimSpace(model)
		if asked == "" || asked == wire {
			asked = send
		}
		if config.CrewRouteAnswers(dir, asked) {
			return nil
		}
		return errRouteResting{model: asked}
	}
}

// errRouteResting is a call the route gate did not send.
type errRouteResting struct{ model string }

func (e errRouteResting) Error() string {
	return e.model + " was not asked: its route refused it recently and is resting"
}

// crewSpendGuard is a crew's spend guard: its prices, the day as the usage
// ledger has it, the day's cap (the daily spending limit too when withDaily),
// the per-task limit — which withDaily does not touch — and the checker's
// ceiling.
func crewSpendGuard(profileDir string, d crewroute.Decision, withDaily bool) *SpendGuard {
	capUSD, action := config.CrewSpendCap(profileDir, false)
	taskCap, taskAction := config.CrewTaskSpendCap(profileDir)
	guard := &SpendGuard{
		Price: config.CrewCallPriceAt(profileDir), Cap: capUSD, CapAction: action,
		SeatCeilings: config.CrewSeatCeilings(d), CeilingAction: config.CrewCheckCeilingAction,
		TaskCap: taskCap, TaskAction: taskAction, Task: &SpendTask{},
	}
	// THE DAILY LIMIT IS READ AT EVERY CALL, not frozen here: a limit raised,
	// lowered or switched on after this crew was admitted is the one that
	// holds, so the day exists whether or not a limit does today.
	if withDaily {
		guard.DayLimit = config.CrewDailyLimit(profileDir)
	}
	if capUSD > 0 || withDaily {
		guard.Day = newCheckedLedgerSpendDay(spentTodayOnLedgerChecked, time.Now)
	}
	return guard
}

// crewDay is the day's spend this conversation holds its crews and their
// helpers to: the usage ledger's figure when first asked, and every guarded
// call after — seats and helpers alike — so a helper's call counts against
// the cap a seat's next call is priced under, and the other way round.
func (a *Agent) crewDay() *SpendDay {
	a.crewDayOnce.Do(func() { a.crewDayHeld = newCheckedLedgerSpendDay(spentTodayOnLedgerChecked, time.Now) })
	return a.crewDayHeld
}

// helperGuard is the guard an auxiliary call in a conversation with crews is
// held to: the crew's day cap, on the same day its seats are priced against,
// and — for a call made for a task — that task's limit, on the same tally its
// seats are priced against. Nil where there is no crew — a conversation under
// --one-model.
func (a *Agent) helperGuard(crew *taskCrew) *SpendGuard {
	if a.config.RouteCrew == nil && crew == nil {
		return nil
	}
	guard := &SpendGuard{Price: config.CrewCallPriceAt(a.config.ProfileDir), Day: a.crewDay()}
	if capUSD, action := config.CrewSpendCap(a.config.ProfileDir, false); capUSD > 0 {
		guard.Cap, guard.CapAction = capUSD, action
	}
	guard.DayLimit = config.CrewDailyLimit(a.config.ProfileDir)
	if crew != nil && crew.guard != nil {
		guard.TaskCap, guard.TaskAction, guard.Task = crew.guard.TaskCap, crew.guard.TaskAction, crew.guard.tally()
		guard.Cap, guard.CapAction, guard.DayLimit = crew.guard.Cap, crew.guard.CapAction, crew.guard.DayLimit
	}
	return guard
}

// crewTaskKey marks a helper call made for one task — its run's closing
// summary — so what it cost is on that task's crew line.
type crewTaskKey struct{}

func withCrewTask(ctx context.Context, crew *taskCrew) context.Context {
	if crew == nil {
		return ctx
	}
	return context.WithValue(ctx, crewTaskKey{}, crew)
}

func crewTaskOf(ctx context.Context) *taskCrew {
	crew, _ := ctx.Value(crewTaskKey{}).(*taskCrew)
	return crew
}

// addHelperSpend puts a helper call's cost on the task it was made for.
func (c *taskCrew) addHelperSpend(response *ai.Response) {
	if c == nil || response == nil || response.Usage == nil || response.Usage.Cost == nil || *response.Usage.Cost <= 0 {
		return
	}
	c.mu.Lock()
	c.helperUSD += *response.Usage.Cost
	c.mu.Unlock()
}

// liveCrewFor is the crew of the run this conversation is driving under
// root, when there is one: a summary a surface asks for while the run is live
// is that task's helper, and its cost is on that task's line.
func (a *Agent) liveCrewFor(root string) *taskCrew {
	a.beltMu.Lock()
	defer a.beltMu.Unlock()
	if a.beltRun == nil || a.beltRun.root != root {
		return nil
	}
	return a.beltRun.crew
}

// stoppedAction is the action this task stopped on, if it stopped on one or
// saw its account cut off.
func (c *taskCrew) stoppedAction() string {
	if c == nil {
		return ""
	}
	return c.stoppedIfCutOff().Stopped
}

// helperSpent is what helpers spent on this task.
func (c *taskCrew) helperSpent() float64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.helperUSD
}

// taskSpent is what this task cost for its crew line and its log row: the
// seats' spend and the helpers made for it, or — when more — every guarded
// call the conversation made since the task was routed, which a conversation
// running one task at a time spent on this task.
func (c *taskCrew) taskSpent(seats float64) float64 {
	if c == nil {
		return seats
	}
	spent := seats + c.helperSpent()
	if c.day != nil {
		if since := c.day.Total() - c.dayAtStart; since > spent {
			spent = since
		}
	}
	return spent
}

// CrewSpendGuard is [crewSpendGuard] for a headless run's seats.
func CrewSpendGuard(profileDir string, d crewroute.Decision, withDaily bool) *SpendGuard {
	return crewSpendGuard(profileDir, d, withDaily)
}

// TaskSpendGuard is the guard a headless run with no routed crew is held to:
// the per-task limit alone.
func TaskSpendGuard(profileDir string) *SpendGuard {
	taskCap, taskAction := config.CrewTaskSpendCap(profileDir)
	return &SpendGuard{Price: config.CrewCallPriceAt(profileDir), TaskCap: taskCap, TaskAction: taskAction, Task: &SpendTask{}}
}

// stopCrew records a guard's stop on the crew — the line leads with it — and
// answers the error the seat call ends on.
func (a *Agent) stopCrew(run *beltRun, err error) error {
	var stopped ErrSpendStopped
	if !errors.As(err, &stopped) {
		return err
	}
	crew := run.crew
	crew.mu.Lock()
	crew.decision.Stopped = stopped.Action
	decision := crew.decision
	crew.mu.Unlock()
	a.republishCrew(run, decision, stopped.Action)
	return crewStopped{action: stopped.Action, cause: err}
}

// crewStopped is a seat with nowhere left to go: its text is the one action,
// and the refusal underneath stays reachable for whoever reads it.
type crewStopped struct {
	action string
	cause  error
}

func (e crewStopped) Error() string { return e.action }
func (e crewStopped) Unwrap() error { return e.cause }

// seats is whether key is one of this crew's seats as routed — what the run
// engine asks for when it calls a seat. original is written once, when the
// crew is routed, and only read after.
func (c *taskCrew) seats(key string) bool {
	for _, send := range c.original {
		if send == key {
			return true
		}
	}
	return false
}

// sendFor is the send id a request for key goes to now: key, or where the
// seat has moved. A call that names no role keeps this meaning.
func (c *taskCrew) sendFor(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sendForLocked(key)
}

// sendForLocked is [taskCrew.sendFor] for a caller that already holds c.mu.
func (c *taskCrew) sendForLocked(key string) string {
	if to := c.swaps[key]; to != "" {
		return to
	}
	return key
}

// sendForSeat is where one role's call for key goes now.
//
// A SAVED PIN, A PIN FOR THIS TASK, AND ONE MODEL FOR EVERY ROLE hold the
// exact send the decision records for that role. An unpinned role that shares
// the original key and falls back writes one global swap, and that swap
// remembers only the first role's destination. It must not drag a role that
// is held, and it must not drag a role whose own recorded move is a different
// model. An unpinned role with no move of its own still follows the shared
// swap. A person who named a send for the role keeps that send.
//
// A call with no seat is [taskCrew.sendFor]. Every map this reads is read
// under c.mu. The walk is this one role — three seats at most — and nothing
// here reads the profile.
func (c *taskCrew) sendForSeat(seat crewroute.Seat, key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seat == "" {
		return c.sendForLocked(key)
	}
	return c.dispatchSeatLocked(seat, key)
}

// dispatchSeatLocked resolves one role's send. The caller holds c.mu.
func (c *taskCrew) dispatchSeatLocked(seat crewroute.Seat, key string) string {
	if to, ok := c.explicitSeatSendLocked(seat, key); ok {
		return to
	}
	if c.original[seat] == key && c.seatHoldsExactLocked(seat) {
		return c.recordedSendLocked(seat, key)
	}
	if moved := c.ownFallbackLocked(seat, key); moved != "" {
		return moved
	}
	return c.sendForLocked(key)
}

// explicitSeatSendLocked is the send a person named for this role, when this
// call is for the role's original key. The caller holds c.mu.
func (c *taskCrew) explicitSeatSendLocked(seat crewroute.Seat, key string) (string, bool) {
	to := c.seatSends[seat]
	if to == "" || c.original[seat] != key {
		return "", false
	}
	return to, true
}

// seatHoldsExactLocked is whether this role stays on the send the decision
// records: a saved pin, a pin for this task, or one model for every role.
// The caller holds c.mu.
func (c *taskCrew) seatHoldsExactLocked(seat crewroute.Seat) bool {
	if c.decision.Mode == crewroute.ModeOne {
		return true
	}
	if c.taskPins[seat] {
		return true
	}
	return c.decision.Seat(seat).Pinned
}

// recordedSendLocked is the send the decision records for seat, or key when
// the decision has none. The caller holds c.mu.
func (c *taskCrew) recordedSendLocked(seat crewroute.Seat, key string) string {
	if pick := c.decision.Seat(seat).Send; pick != "" {
		return pick
	}
	return key
}

// ownFallbackLocked is this unpinned role's own recorded move off key: the
// rung [Agent.moveCrewSeat] wrote onto the decision. Empty when the role is
// still on key, so the shared swap — another role's destination — is not
// what a role with its own move calls. The caller holds c.mu.
func (c *taskCrew) ownFallbackLocked(seat crewroute.Seat, key string) string {
	if c.original[seat] != key {
		return ""
	}
	pick := c.decision.Seat(seat).Send
	if pick == "" || pick == key {
		return ""
	}
	return pick
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

// seatStarting marks a role started at its admitted call and clears its
// pending change. An unmarked call (no seat) marks nothing.
func (c *taskCrew) seatStarting(seat crewroute.Seat) {
	if seat == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seatStarted == nil {
		c.seatStarted = map[crewroute.Seat]bool{}
	}
	c.seatStarted[seat] = true
	delete(c.pending, seat)
}

// crewAnswered notes that a role's call answered on send: the role has run —
// that role only, never every role on the same model — and the first answer
// on send writes the route's success to the router's log once. An empty seat
// is a call no role was marked for, which marks no role.
func (a *Agent) crewAnswered(run *beltRun, send string, seat crewroute.Seat) {
	crew := run.crew
	crew.mu.Lock()
	if seat != "" {
		if crew.seatStarted == nil {
			crew.seatStarted = map[crewroute.Seat]bool{}
		}
		crew.seatStarted[seat] = true
		delete(crew.pending, seat)
	}
	first := !crew.started[send]
	if first {
		if crew.started == nil {
			crew.started = map[string]bool{}
		}
		crew.started[send] = true
	}
	decision := crew.decision
	crew.mu.Unlock()
	if !first {
		return
	}
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
	shared := false
	for _, seat := range crewroute.Seats {
		pick := decision.Seat(seat)
		if pick.Send != current || crew.original[seat] != key {
			continue
		}
		// A PINNED ROLE — a saved pin, a pin for this task, one model for
		// every role — HOLDS ITS EXACT MODEL when its route fails. Only a
		// person moves it; the history says it failed, when and why.
		if seatHoldsItsModel(pick, decision.Mode) {
			noteHeldFailureLocked(crew, seat, pick, kind)
			continue
		}
		rung, ok := crew.takeRungLocked(seat, config.AutomaticCrewRungs(a.config.ProfileDir, rescueLadderLocked(a, run, crew, decision, seat)))
		if !ok {
			continue
		}
		decision, next, shared = fallBackSeatLocked(crew, decision, seat, pick, rung, kind, provided, next, shared)
	}
	if next.Send == "" {
		action, stopped, decision := nowhereLeftLocked(crew, decision, kind, provided, until)
		crew.mu.Unlock()
		if stopped {
			a.republishCrew(run, decision, current+" could not start · "+action)
		}
		return action, false
	}
	if crew.swaps == nil {
		crew.swaps = map[string]string{}
	}
	if shared {
		crew.swaps[key] = next.Send
	}
	crew.decision = decision
	crew.mu.Unlock()
	a.republishCrew(run, decision, fmt.Sprintf("%s could not start (%s) · moved to %s", current, crewWhy(kind, provided), next.Send))
	return "", true
}

// seatHoldsItsModel is a role a failure must not move: a pin, or one model
// for every role. Only a person changes it.
func seatHoldsItsModel(pick crewroute.Pick, mode crewroute.Mode) bool {
	return pick.Pinned || mode == crewroute.ModeOne
}

// noteHeldFailureLocked records that a held role's route failed, once per
// send and reason. The caller holds crew.mu.
func noteHeldFailureLocked(crew *taskCrew, seat crewroute.Seat, pick crewroute.Pick, kind provider.RouteFailure) {
	if crew.heldLocked(seat, pick.Send, string(kind)) {
		return
	}
	crew.noteChangeLocked(TaskCrewChange{At: time.Now(), Seat: seat, From: pick.Model, FromProvider: pick.Provider, FromSend: pick.Send,
		By: TaskCrewByEngine, Kind: TaskCrewChangeHeld, Why: string(kind)})
}

// rescueLadderLocked is the rungs a seat may fall to: its own ladder, then
// the rescue list. A crew restored from a checkpoint uses the rescue it kept,
// not today's settings. The caller holds crew.mu.
func rescueLadderLocked(a *Agent, run *beltRun, crew *taskCrew, decision crewroute.Decision, seat crewroute.Seat) []crewroute.Pick {
	rescue := config.CrewRescue(a.config.ProfileDir, decision.Class, seat, a.Model())
	if run.recoveredCrew != nil {
		rescue = crew.rescue[seat]
	}
	return append(append([]crewroute.Pick(nil), crew.ladders[seat]...), rescue...)
}

// fallBackSeatLocked moves one seat onto rung and remembers the first such
// rung as where the shared send goes. A seat that had its own send keeps
// that send pointed at the rung; a shared send is swapped once, outside.
// The caller holds crew.mu.
func fallBackSeatLocked(crew *taskCrew, decision crewroute.Decision, seat crewroute.Seat, pick, rung crewroute.Pick, kind provider.RouteFailure, provided string, next crewroute.Pick, shared bool) (crewroute.Decision, crewroute.Pick, bool) {
	decision = withFreeNotice(decision.WithRung(seat, rung, crewWhy(kind, provided)), rung)
	crew.noteChangeLocked(TaskCrewChange{Seat: seat, From: pick.Model, FromProvider: pick.Provider, FromSend: pick.Send, To: rung.Model, ToProvider: rung.Provider, ToSend: rung.Send,
		By: TaskCrewByEngine, Kind: TaskCrewChangeFallback, Why: string(kind)})
	if crew.seatSends[seat] != "" {
		crew.seatSends[seat] = rung.Send
	} else {
		shared = true
	}
	if next.Send == "" {
		next = rung
	}
	return decision, next, shared
}

// nowhereLeftLocked is the one action when no seat could move. A cause a
// person can fix — credit, a key, a limit — stops the task on that action,
// rather than offering a stronger crew on the same account. The caller holds
// crew.mu and unlocks after.
func nowhereLeftLocked(crew *taskCrew, decision crewroute.Decision, kind provider.RouteFailure, provided string, until time.Time) (string, bool, crewroute.Decision) {
	action := crew.actionLocked(kind, provided, until)
	stopped := crewStopsOn(kind) || len(crew.broke) > 0 || len(crew.gone) > 0
	if stopped {
		decision.Stopped = action
		crew.decision = decision
	}
	return action, stopped, decision
}

// crewFailedLocked writes a failed first call to the router's log for every
// seat on current, and marks what this task must not ask again: the route, an
// account out of credit, a provider whose key was refused. It answers the
// provider the route was on. The caller holds crew.mu.
func (a *Agent) crewFailedLocked(crew *taskCrew, current string, kind provider.RouteFailure, until time.Time) string {
	var provided string
	// A send another seat already found failed is written down once.
	again := crew.bad[current]
	for _, seat := range crewroute.Seats {
		if pick := crew.decision.Seat(seat); pick.Send == current {
			provided = pick.Provider
			if again {
				continue
			}
			// THE REFUSAL IS WRITTEN AGAINST THE CREDENTIAL THE CALL WAS SENT
			// WITH ([crewFailure.sent], noted before this), never the one in
			// force now — a key connected while the call was in flight is
			// not blamed for it.
			config.LogCrewRouteSent(a.config.ProfileDir, crew.call, crew.decision, crew.repo, crew.title, seat, pick, string(kind), until, crew.failed[current].sent)
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

// actionLocked is the one thing to do when a seat has nowhere left to go,
// read off EVERYTHING this task saw fail rather than the last failure alone:
// an account out of credit is the cause even when the free pool tried after
// it was at its limit, and a refused key is the cause of every call after it.
// The caller holds c.mu.
func (c *taskCrew) actionLocked(kind provider.RouteFailure, on string, until time.Time) string {
	switch {
	case len(c.broke) > 0:
		return crewActionFor(provider.RoutePayment, strings.Join(crewKeys(c.broke), ", "), until)
	case len(c.gone) > 0:
		return crewActionFor(provider.RouteAuth, strings.Join(crewKeys(c.gone), ", "), until)
	}
	return crewActionFor(kind, on, until)
}

// stoppedIfCutOff is the crew as a failed task ends it: when this task saw an
// account run out of credit or a key refused, a stronger crew is out of reach
// on it too, so the one action is what the line ends on — never "/redo
// stronger". A crew that already stopped on its action, or saw neither, is
// left as it is.
func (c *taskCrew) stoppedIfCutOff() crewroute.Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.decision.Stopped == "" && (len(c.broke) > 0 || len(c.gone) > 0) {
		c.decision.Stopped = c.actionLocked("", "", time.Time{})
	}
	return c.decision
}

// takeRungLocked is the seat's next rung this task has not seen fail, within
// its free pools, counted when it is a free pool. The caller holds c.mu.
func (c *taskCrew) takeRungLocked(seat crewroute.Seat, rungs []crewroute.Pick) (crewroute.Pick, bool) {
	rung, ok := c.nextRung(c.withinFreePools(seat, rungs))
	if ok && rung.Kind == crewroute.Free {
		if c.freeTried == nil {
			c.freeTried = map[crewroute.Seat]int{}
		}
		c.freeTried[seat]++
	}
	return rung, ok
}

// withFreeNotice is d with the free-pool notice when rung is a free pool.
func withFreeNotice(d crewroute.Decision, rung crewroute.Pick) crewroute.Decision {
	if rung.Kind == crewroute.Free && !strings.Contains(d.Note, "free routes in use") {
		d.Note = strings.TrimSpace(d.Note + " " + crewFreeNotice)
	}
	return d
}

// withinFreePools is rungs without the free pools once seat has been moved
// onto crewFreePools of them in this task. The caller holds c.mu.
func (c *taskCrew) withinFreePools(seat crewroute.Seat, rungs []crewroute.Pick) []crewroute.Pick {
	if c.freeTried[seat] < crewFreePools {
		return rungs
	}
	out := rungs[:0]
	for _, r := range rungs {
		if r.Kind != crewroute.Free {
			out = append(out, r)
		}
	}
	return out
}

// crewFreePools is how many free pools — models, not attempts — one seat is
// moved onto in one task.
const crewFreePools = 3

// crewKeys is a set's members, sorted.
func crewKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k, on := range set {
		if on && k != "" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// crewFreeNotice is what the crew line says when a seat fell to a free pool:
// the pool may keep what it is sent.
const crewFreeNotice = "free routes in use (may log prompts)"

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
