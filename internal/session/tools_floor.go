package session

// THE FOREMAN'S ONE DOOR: `factory_floor`, THE FLOOR READ WHOLE.
//
// The foreman is the floor's own conversation (`m` on the factory floor,
// cmd/codeaf's factory_foreman.go, opened from the app by factory.foreman_open):
// the product-level judgment about what to take first, never the runner. It
// reads the floor through this one tool, proposes by SELECTING items, and
// acts on one job when the person asks: it starts or prioritises it
// ([FloorActor]), passes the person's words to its main agent and presses an
// action the job offers the person ([FloorHand], tools_floor_hand.go). ONLY
// THE FOREMAN CARRIES IT: cmd/codeaf sets [Config.Floor] on
// the conversation whose session file the floor names as its foreman and on
// no other, so an ordinary chat proposes through `factory_add` and an item's
// own conversation through `factory_item`.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - IT READS AND MARKS BY ITSELF, AND ACTS ONLY FOR THE PERSON. A start and
//     a priority go through the job page's own doors ([FloorActor]), only
//     when the person asked. A selection (the field is `mark`) is a proposal
//     the person sees on the floor, and takes or leaves. Nothing here stops
//     or drops a job.
//
//   - A PRESS AND WORDS FOR A JOB'S MAIN AGENT NEED THE PERSON'S OWN WORDS IN
//     THE TURN ([Agent.turnPersonWords]), read from the turn and never from
//     the model's arguments. The door checks it again, and presses only what
//     the job's stored offers list for its person (cmd/codeaf's
//     factory_foreman_press.go).
//
//   - EVERY ROW SAYS THE JOB'S STORED WAIT ([factory.FloorWaitWords]): who it
//     waits on and what it accepts from the person now. The floor's own
//     reading of a state never says who a job waits on.
//
//   - ONLY A NEW ITEM TAKES A MARK. The door refuses anything else, and the
//     tool says which items it left alone and why, so the model never believes
//     it selected a stream.
//
//   - ITEMS ARE NAMED BY THEIR REF. The table, the marks and the answers all
//     speak `#4`, which is what the person reads on the floor; the store's own
//     ids never reach the model.
//
//   - A CAPABILITY WITH NOTHING BEHIND IT IS ABSENT. With no [Config.Floor]
//     the tool is off the belt (every conversation but the foreman, --once,
//     a task node, a stage, --host, --at).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// FloorDoor is the foreman's door onto the factory floor: the floor as the
// page reads it, and the floor's own marks. Both may touch disk; the tool calls
// them from its own goroutine, never from a surface's loop.
type FloorDoor interface {
	Snapshot(ctx context.Context) (factory.Snapshot, error)
	Mark(ctx context.Context, ids []int, on bool) error
}

// FloorActor is a [FloorDoor] that may also start a job and set its priority
// for the person, through the doors the job page's Start and priority take.
// The foreman uses it only when the person asks; a door that is not one leaves
// the tool reading and selecting, as before. Each answers the line the model
// reads back, or a refusal in words.
//
// said is the person's own words in the running turn, "" when the turn holds
// none: a start that would carry a job past an approve step is the person's,
// and the door refuses it without them.
type FloorActor interface {
	StartItem(ctx context.Context, id int, said string) (string, error)
	SetPriority(ctx context.Context, id int, value string) (string, error)
}

// mayFloor says whether `factory_floor` belongs on this belt: there is a door
// behind it.
func (c Config) mayFloor() bool { return !c.readingTask() && c.Floor != nil }

// floorTools is `factory_floor`, or nothing when there is no floor behind it.
func (a *Agent) floorTools() []bare.Tool {
	if !a.config.mayFloor() {
		return nil
	}
	return []bare.Tool{a.factoryFloorTool()}
}

// The asks the tool answers, spelled once for the schema and the switch.
const (
	floorAskWaiting  = "waiting"
	floorAskNeedsYou = "needs_you"
	floorAskRisky    = "risky"
	floorAskCheapest = "cheapest"
	floorAskOldest   = "oldest"
	floorAskAll      = "all"
	floorAskItem     = "item"
	floorAskRunning  = "running"
	floorAskQueue    = "queue"
	floorAskShipped  = "shipped"
	floorAskRepos    = "repos"
)

// floorAsks is every ask in the order the schema and the refusal list them.
var floorAsks = []string{floorAskRunning, floorAskQueue, floorAskWaiting, floorAskNeedsYou, floorAskShipped, floorAskRepos, floorAskRisky, floorAskCheapest, floorAskOldest, floorAskAll, floorAskItem}

// floorShippedDaysMost is how far back `shipped` reads when asked for more
// than today: a quarter, which is as far as "recent work" reaches.
const floorShippedDaysMost = 90

// floorReadWait and floorActWait are how long one call may wait on the
// factory before the tool answers that it did not. A healthy floor reads in
// milliseconds; the bound is for the read that never comes back, which once
// held a manager's turn open until the app was quit. A start, a priority or a
// press is a request to the factory and is given longer. Words passed to a
// job's main agent are a whole turn of that agent's conversation, bounded by
// [FloorTellWait]: the door stops the turn there, and the tool waits
// floorTellGrace more for the door to say so.
const (
	floorReadWait  = 20 * time.Second
	floorActWait   = 60 * time.Second
	FloorTellWait  = 3 * time.Minute
	floorTellGrace = 10 * time.Second
)

// The floor's limits on one answer: how many rows a ranked ask lists, how many
// rows `all` lists before it says how many more there are, and how much of an
// item's body `item` quotes.
const (
	floorTopRows  = 10
	floorAllRows  = 50
	floorBodyCut  = 2000
	floorTitleCut = 80
	// floorAsksCut is how much of what a job asks its person a row quotes.
	floorAsksCut = 240
)

// The sentences the model reads back. The manual quotes the handoff
// (internal/manual/chat/factory.md, `## the foreman — m`).
const (
	// FloorMarkedTail follows the refs a call selected: `selected #1 #4 #6 ·
	// the person starts them from the factory, or asks you to start one`.
	FloorMarkedTail = " · the person starts them from the factory, or asks you to start one"
	floorNothing    = "the factory has no items yet"
	floorNoneAsked  = "no items in the factory answer that ask"
)

var factoryFloorDescription = "Read the whole factory, act on one job when the person asks, and propose what to take on by selecting items. " +
	"You are the factory's foreman, its Factory manager: you read, rank, propose and sum up, and you act on a job only when the person asks for it in this conversation. Nothing here stops or drops a job, and nothing here ships, merges or posts on your own judgment. " +
	"ask: running (jobs running or queued to run, with their step), queue (jobs not started yet, most urgent then oldest first), waiting (every job that waits on the person, the ones they put away included), needs_you (the jobs that wait on the person now, the longest wait first, each with what it asks and what it accepts), shipped (jobs shipped today, with their pull requests and changes, to sum up; set days to read further back), repos (every repository with its count of jobs in each state, for how many and where), risky, cheapest (ten, by estimate), oldest (ten), all, or item (one item in full: its read, facts, stages, what it waits on, what it accepts, the public acts that wait for approval, its question and body; set item). " +
	"Every row of a job that has started says what it waits on (the person, its main agent, GitHub, a computer, or nobody while work is under way), whether the factory found it stalled, and what it accepts from the person now. Say who a job waits on from that, never from its state. " +
	"Every answer but item and repos says the factory's counts by state on its first line, so a how-many question is answered by one read. " +
	"start takes one job by the number in its ref and starts it as the job page's Start does. priority is p0 (urgent), p1 (high), p2 (medium) or p3 (low), set on item. " +
	"press (with item) presses, as the person, one action the job's row says it accepts from them: " + floorPressList + ". With answer, words are the person's answer, and choice is one of the choices when the item lists them (a question with listed choices takes only a choice; send_back also takes words saying why). With reject_plan, acts_decline, task, resume_pipeline, resume_auto and approve, words are what the person said to do or why. With raise_budget, budget is the new budget in USD the person named. " +
	"A press is refused unless the person's own message in this turn asked for it; never press on your own judgment, and merge only when they say to merge. An action the job does not accept now is refused with what it does accept: tell the person that. Before acts_approve, show the person exactly what would be posted or changed (ask item). " + floorPressElsewhere + " " +
	"tell (with item) passes the person's words to that job's main agent, gives it one turn of its own conversation, and answers what it said, for you to relay. Use it only when the person asks in this turn; the turn is charged to the job's budget and is stopped after " + strconv.Itoa(int(FloorTellWait/time.Minute)) + " minutes. That turn is the factory's, not the person speaking in the job's conversation: outside Auto the job's main agent cannot push, approve, mark the pull request ready or approve public acts on it, and it never merges on it. Never promise that it will; press those here when the person asks and the job accepts them. " +
	"item, mark and unmark take items by the number in their ref (#4 is 4). Only a new item can be selected; selections stay until the person runs or unselects them. The fields are named mark and unmark; the person calls them select and unselect. " +
	"A message may start with blocks the app writes: <afi-now> is the app's present state as of when it was sent (\"Waiting on you\" there is the person) and <app-places> says where things are in the app; read here for what they do not say. " +
	"Name items by their ref in everything you say."

func factoryFloorSchemaJSON() string {
	return `{"type":"object","properties":{` +
		`"ask":{"type":"string","enum":["` + strings.Join(floorAsks, `","`) + `"],"description":"What to read from the factory."},` +
		`"days":{"type":"integer","description":"With ask shipped: how many days to read, today included (1 is today and the default; at most ` + strconv.Itoa(floorShippedDaysMost) + `)."},` +
		`"item":{"type":"integer","description":"With ask item, priority, press or tell: the number in the item's ref (#4 is 4)."},` +
		`"start":{"type":"integer","description":"The number in the ref of one job to start now, only when the person asked."},` +
		`"priority":{"type":"string","enum":["p0","p1","p2","p3"],"description":"With item: the priority to set on that job, only when the person asked."},` +
		`"press":{"type":"string","enum":["` + strings.Join(floorPresses, `","`) + `"],"description":"With item: one action that job accepts from the person now, pressed as them. Only when the person asked for it in this turn."},` +
		`"words":{"type":"string","description":"With press: the person's words that go with it: their answer, their instructions for a task, what to change in a plan, why they decline."},` +
		`"choice":{"type":"string","description":"With press answer: the one of the question's listed choices the person chose."},` +
		`"budget":{"type":"number","description":"With press raise_budget: the new budget in USD the person named."},` +
		`"tell":{"type":"string","description":"With item: the person's words for that job's main agent, passed on as theirs. Only when the person asked in this turn."},` +
		`"mark":{"type":"array","items":{"type":"integer"},"description":"Refs' numbers of new items to select for the person's next run."},` +
		`"unmark":{"type":"array","items":{"type":"integer"},"description":"Refs' numbers whose selection to take off."}` +
		`},"additionalProperties":false}`
}

// The gloss a person reads beside the call is what was asked.
func init() { glossField["factory_floor"] = "ask" }

// factoryFloorTool reads the floor, and marks or unmarks items on it.
func (a *Agent) factoryFloorTool() bare.Tool {
	return bare.Tool{
		Name:        "factory_floor",
		Description: factoryFloorDescription,
		Schema:      json.RawMessage(factoryFloorSchemaJSON()),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			// A DOOR THAT FAULTED ANSWERS AS ANY FAULTED TOOL DOES. The door's
			// calls run on goroutines of their own ([floorWithin]), out of reach
			// of the tool loop's guard, so the fault is recovered there and
			// carried back here, and the call answers the loop's own sentence.
			var fault floorFault
			text, isError, err := a.factoryFloorCall(withFloorFault(ctx, &fault), args)
			if fault.err != nil {
				return floorPanickedWords, true, nil
			}
			return text, isError, err
		},
	}
}

// floorPanickedWords is the result a factory_floor call whose door panicked
// answers: the sentence the tool loop seeds for every call ([newBatchSlots]).
const floorPanickedWords = "tool panicked: factory_floor did not return a result"

// floorFault is where [floorWithin] leaves a door's recovered panic for the
// call that is waiting on it. It is written and read on the tool's own
// goroutine: floorWithin receives the fault on its channel and files it here
// before returning.
type floorFault struct{ err error }

type floorFaultKey struct{}

func withFloorFault(ctx context.Context, fault *floorFault) context.Context {
	return context.WithValue(ctx, floorFaultKey{}, fault)
}

// factoryFloorCall is the factory_floor tool's body.
func (a *Agent) factoryFloorCall(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed struct {
		Ask      string  `json:"ask"`
		Item     int     `json:"item"`
		Mark     []int   `json:"mark"`
		Unmark   []int   `json:"unmark"`
		Start    int     `json:"start"`
		Priority string  `json:"priority"`
		Days     int     `json:"days"`
		Press    string  `json:"press"`
		Words    string  `json:"words"`
		Choice   string  `json:"choice"`
		Budget   float64 `json:"budget"`
		Tell     string  `json:"tell"`
	}
	if len(args) > 0 {
		if err := decodeToolArguments(args, &parsed); err != nil {
			return rejectToolArguments(ctx, "Invalid arguments: "+err.Error()), true, nil
		}
	}
	ask := strings.ToLower(strings.TrimSpace(parsed.Ask))
	priority := strings.ToLower(strings.TrimSpace(parsed.Priority))
	press := strings.ToLower(strings.TrimSpace(parsed.Press))
	tell := strings.TrimSpace(parsed.Tell)
	// ONE ACT A CALL: a start, a priority, a press or words for a main agent,
	// and nothing else done beside it.
	acting := 0
	for _, on := range []bool{parsed.Start > 0, priority != "", press != "", tell != ""} {
		if on {
			acting++
		}
	}
	acts := acting > 0
	if acting > 1 {
		return rejectToolArguments(ctx, "Invalid arguments: start, priority, press and tell are one to a call."), true, nil
	}
	if ask == "" && len(parsed.Mark)+len(parsed.Unmark) == 0 && !acts {
		ask = floorAskAll
	}
	if ask != "" && !slices.Contains(floorAsks, ask) {
		return rejectToolArguments(ctx, "Invalid arguments: ask is one of "+strings.Join(floorAsks[:len(floorAsks)-1], ", ")+" or "+floorAsks[len(floorAsks)-1]+"."), true, nil
	}
	if parsed.Days < 0 || parsed.Days > floorShippedDaysMost || (parsed.Days > 0 && ask != floorAskShipped) {
		return rejectToolArguments(ctx, "Invalid arguments: days goes with ask shipped, from 1 to "+strconv.Itoa(floorShippedDaysMost)+"."), true, nil
	}
	if priority != "" && parsed.Item <= 0 {
		return rejectToolArguments(ctx, "Invalid arguments: priority needs item, the number in the job's ref."), true, nil
	}
	if (press != "" || tell != "") && parsed.Item <= 0 {
		return rejectToolArguments(ctx, "Invalid arguments: press and tell need item, the number in the job's ref."), true, nil
	}
	if press != "" && !slices.Contains(floorPresses, press) {
		return rejectToolArguments(ctx, "Invalid arguments: press is one of "+strings.Join(floorPresses, ", ")+"."), true, nil
	}
	if press == "" && (strings.TrimSpace(parsed.Words) != "" || strings.TrimSpace(parsed.Choice) != "" || parsed.Budget != 0) {
		return rejectToolArguments(ctx, "Invalid arguments: words, choice and budget go with press."), true, nil
	}
	switch {
	case press != "":
		return a.floorPress(ctx, parsed.Item, FloorPress{Action: press, Words: strings.TrimSpace(parsed.Words), Choice: strings.TrimSpace(parsed.Choice), Budget: parsed.Budget})
	case tell != "":
		return a.floorTell(ctx, parsed.Item, tell)
	case acts:
		return a.floorAct(ctx, parsed.Start, parsed.Item, priority)
	}
	if ask == floorAskItem && parsed.Item <= 0 {
		return rejectToolArguments(ctx, "Invalid arguments: ask item needs item, the number in the item's ref."), true, nil
	}
	return a.floorRun(ctx, ask, parsed.Item, parsed.Days, parsed.Mark, parsed.Unmark)
}

// floorAct starts one job, or sets one job's priority, through the door's
// actor, by the ref the person read. Nothing else is done on the same call.
func (a *Agent) floorAct(ctx context.Context, start, item int, priority string) (string, bool, error) {
	actor, ok := a.config.Floor.(FloorActor)
	if !ok {
		return "Nothing changed: this window cannot start or prioritise a job; the person can on the job's page.", true, nil
	}
	snap, err := floorSnapshot(ctx, a.config.Floor)
	if err != nil {
		return "the factory could not be read: " + oneLine(err.Error()), true, nil
	}
	n := start
	if n <= 0 {
		n = item
	}
	said := a.turnPersonWords()
	found := floorFind(snap, n)
	switch {
	case len(found) == 0:
		return fmt.Sprintf("Nothing changed: there is no #%d in the factory", n), true, nil
	case len(found) > 1:
		return fmt.Sprintf("Nothing changed: #%d is on %d repositories (%s)", n, len(found), floorRepos(found)), true, nil
	}
	line, err := floorWithin(ctx, floorActWait, func(ctx context.Context) (string, error) {
		if start > 0 {
			// The person's own words of the turn ride with a start: going past
			// an approve step is theirs, and the door refuses it without them.
			return actor.StartItem(ctx, found[0].ID, said)
		}
		value, _ := factory.PriorityFromShort(priority)
		return actor.SetPriority(ctx, found[0].ID, value)
	})
	if errors.Is(err, errFloorLate) {
		// NOT "Nothing changed": the request was sent and its answer never
		// came, so whether it landed is exactly what is not known.
		return fmt.Sprintf("%s for %s. It may still take effect: read %s again before asking twice.", floorLateWords(floorActWait), found[0].Ref(), found[0].Ref()), true, nil
	}
	if err != nil {
		return oneLine(err.Error()), true, nil
	}
	return line, false, nil
}

// errFloorLate is a call on the floor that did not answer inside its wait.
var errFloorLate = errors.New("the factory did not answer in time")

func floorLateWords(wait time.Duration) string {
	return fmt.Sprintf("the factory did not answer within %d seconds", int(wait/time.Second))
}

// floorWithin runs one call on the floor's door and WAITS FOR IT NO LONGER
// THAN wait, and no longer than the turn. The door reads disk and may take a
// lock, and it is the engine's own code rather than a subprocess a context
// can kill: a door that never came back held the manager's turn open, with
// the person's stop changing nothing, until the app was quit. So the call
// runs on a goroutine of its own and this returns at the bound or the moment
// the turn is cut, whichever is first; a call that outlives it finishes with
// nobody waiting, which is the most a caller can do about code that will not
// look at its context.
func floorWithin[T any](ctx context.Context, wait time.Duration, call func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	type answer struct {
		value T
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		// THE DOOR'S CODE RUNS HERE, off the tool loop's guarded goroutine, so
		// a panic in it is recovered here or it takes the process down. The
		// fault goes back on done as an error, and the call that is waiting
		// returns at once.
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- answer{err: &floorPanic{guard.Note("session tool factory_floor", recovered)}}
			}
		}()
		value, err := call(ctx)
		done <- answer{value, err}
	}()
	select {
	case got := <-done:
		var panicked *floorPanic
		if errors.As(got.err, &panicked) {
			if fault, ok := ctx.Value(floorFaultKey{}).(*floorFault); ok && fault.err == nil {
				fault.err = panicked.fault
			}
		}
		return got.value, got.err
	case <-ctx.Done():
		var none T
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return none, errFloorLate
		}
		return none, ctx.Err()
	}
}

// floorPanic is a door call that panicked, as [floorWithin] returns it.
type floorPanic struct{ fault error }

func (p *floorPanic) Error() string { return p.fault.Error() }
func (p *floorPanic) Unwrap() error { return p.fault }

// floorSnapshot is the floor read under [floorReadWait], in words the model
// can act on when it did not come.
func floorSnapshot(ctx context.Context, door FloorDoor) (factory.Snapshot, error) {
	snap, err := floorWithin(ctx, floorReadWait, door.Snapshot)
	if errors.Is(err, errFloorLate) {
		return snap, errors.New(floorLateWords(floorReadWait) + "; say so rather than guess, and try once more if the person asks")
	}
	return snap, err
}

// floorRun does one call: the marks first, then the ask over the floor as it
// is after them, so a call that marks and reads sees its own marks.
func (a *Agent) floorRun(ctx context.Context, ask string, item, days int, mark, unmark []int) (string, bool, error) {
	door := a.config.Floor
	snap, err := floorSnapshot(ctx, door)
	if err != nil {
		return "the factory could not be read: " + oneLine(err.Error()), true, nil
	}
	var out []string
	if len(mark)+len(unmark) > 0 {
		said, failed := floorMarks(ctx, door, snap, mark, unmark)
		out = append(out, said...)
		if failed {
			return strings.Join(out, "\n"), true, nil
		}
		if ask != "" {
			if snap, err = floorSnapshot(ctx, door); err != nil {
				return strings.Join(out, "\n"), false, nil
			}
		}
	}
	if ask != "" {
		out = append(out, floorAnswer(snap, ask, item, days))
	}
	return strings.Join(out, "\n\n"), false, nil
}

// floorMarks marks and unmarks through the door and says what happened in one
// line each: what was selected and the handoff, what was unselected, and every
// ref left alone with its reason. failed is a door that refused.
func floorMarks(ctx context.Context, door FloorDoor, snap factory.Snapshot, mark, unmark []int) (said []string, failed bool) {
	var notes []string
	resolve := func(nums []int, needNew bool) (ids []int, refs []string) {
		for _, n := range nums {
			found := floorFind(snap, n)
			switch {
			case len(found) == 0:
				notes = append(notes, fmt.Sprintf("there is no #%d in the factory", n))
			case len(found) > 1:
				notes = append(notes, fmt.Sprintf("#%d is on %d repositories (%s); left alone", n, len(found), floorRepos(found)))
			case needNew && found[0].State != factory.StateNew:
				notes = append(notes, fmt.Sprintf("%s is %s, and only a new item can be selected", found[0].Ref(), floorStateWords(found[0].State)))
			default:
				ids = append(ids, found[0].ID)
				refs = append(refs, found[0].Ref())
			}
		}
		return ids, refs
	}
	if ids, refs := resolve(unmark, false); len(ids) > 0 {
		if err := floorMark(ctx, door, ids, false); err != nil {
			return append(said, floorMarkRefused("unselected", err)), true
		}
		said = append(said, "unselected "+strings.Join(refs, " "))
	}
	if ids, refs := resolve(mark, true); len(ids) > 0 {
		if err := floorMark(ctx, door, ids, true); err != nil {
			return append(said, floorMarkRefused("selected", err)), true
		}
		said = append(said, "selected "+strings.Join(refs, " ")+FloorMarkedTail)
	}
	return append(said, notes...), false
}

// floorMark is the door's Mark under [floorActWait].
func floorMark(ctx context.Context, door FloorDoor, ids []int, on bool) error {
	_, err := floorWithin(ctx, floorActWait, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, door.Mark(ctx, ids, on)
	})
	return err
}

// floorMarkRefused is the line for a selection the door did not make. One
// that got no answer in time is NOT said to have changed nothing: the write
// was asked for, and whether it landed is what is not known.
func floorMarkRefused(word string, err error) string {
	if errors.Is(err, errFloorLate) {
		return floorLateWords(floorActWait) + ": whether anything was " + word + " is not known; read the factory again"
	}
	return "nothing " + word + ": " + oneLine(err.Error())
}

// floorFind is every item on the floor whose ref is #n, dismissed ones left
// out.
func floorFind(snap factory.Snapshot, n int) []factory.Item {
	want := "#" + strconv.Itoa(n)
	var out []factory.Item
	for _, it := range snap.Items {
		if it.State != factory.StateDismissed && it.KeyRef() == want {
			out = append(out, it)
		}
	}
	return out
}

// floorStateWords is a state as a sentence says it: `#3 is running`, `#5 is
// waiting on the person`.
func floorStateWords(s factory.State) string {
	if s == factory.StateNeedsYou {
		return "waiting on the person"
	}
	return string(s)
}

func floorRepos(items []factory.Item) string {
	var names []string
	for _, it := range items {
		names = append(names, it.Repo)
	}
	return strings.Join(names, ", ")
}

// FloorAnswer is one ask's text over a floor: a compact table, one item a
// line, or one item in full for `item`.
func FloorAnswer(snap factory.Snapshot, ask string, item int) string {
	return floorAnswer(snap, ask, item, 0)
}

// floorAnswer is [FloorAnswer] with how many days `shipped` reads; zero is
// today.
func floorAnswer(snap factory.Snapshot, ask string, item, days int) string {
	now := snap.Now
	if now.IsZero() {
		now = time.Now()
	}
	open := floorOpen(snap)
	if ask == floorAskShipped {
		return floorWithCounts(floorShipped(snap, now, days), open)
	}
	if ask == floorAskRepos {
		return floorReposAnswer(snap)
	}
	if len(open) == 0 {
		return floorNothing
	}
	if ask == floorAskItem {
		found := floorFind(snap, item)
		switch len(found) {
		case 0:
			return fmt.Sprintf("there is no #%d in the factory", item)
		case 1:
			return FloorItemText(snap, found[0], now)
		}
		var parts []string
		for _, it := range found {
			parts = append(parts, FloorItemText(snap, it, now))
		}
		return strings.Join(parts, "\n\n")
	}
	rows, head, asks := open, "", false
	switch ask {
	case floorAskRunning:
		rows = floorKeep(open, func(it factory.Item) bool {
			return it.State == factory.StateRunning || it.State == factory.StateQueued || it.State == factory.StateLanded || floorFollowing(it)
		})
		head = "running, queued to run, or following an open pull request"
	case floorAskQueue:
		rows = floorKeep(open, func(it factory.Item) bool { return it.State == factory.StateNew })
		sort.SliceStable(rows, func(i, j int) bool {
			if ri, rj := factory.PriorityRank(rows[i]), factory.PriorityRank(rows[j]); ri != rj {
				return ri < rj
			}
			return rows[i].Created.Before(rows[j].Created)
		})
		head = "not started, most urgent then oldest first"
	case floorAskWaiting:
		// WHO A JOB WAITS ON IS ITS STORED WAIT, never the floor's reading of
		// its state: a landed job waiting for its approval and a published one
		// ready to merge wait on the person too, and a job whose stop is with
		// its main agent does not.
		rows = floorKeep(open, func(it factory.Item) bool {
			_, yours := factory.WaitsOnPerson(it)
			return yours
		})
		head = "waiting on the person"
	case floorAskNeedsYou:
		rows = floorKeep(open, floorNeedsPerson)
		sort.SliceStable(rows, func(i, j int) bool { return floorWaitSince(rows[i]).Before(floorWaitSince(rows[j])) })
		head, asks = "waiting on the person now, longest wait first", true
	case floorAskRisky:
		rows = floorKeep(open, factory.Match("risky"))
		head = "risky"
	case floorAskCheapest:
		// No model writes a price, so cheapest is least complex first: the
		// rubric's sum, which finished jobs are measured against.
		rows = floorKeep(open, func(it factory.Item) bool { return it.Triage.Rubric != nil })
		sort.SliceStable(rows, func(i, j int) bool {
			return rows[i].Triage.Rubric.Complexity() < rows[j].Triage.Rubric.Complexity()
		})
		rows = floorTop(rows, floorTopRows)
		head = "least complex first"
	case floorAskOldest:
		rows = floorKeep(open, func(it factory.Item) bool { return !it.Created.IsZero() })
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Created.Before(rows[j].Created) })
		rows = floorTop(rows, floorTopRows)
		head = "oldest first"
	default:
		head = "every open item"
	}
	if len(rows) == 0 {
		return floorWithCounts(floorNoneAsked+" ("+head+")", open)
	}
	var b strings.Builder
	b.WriteString(floorSummary(open, len(rows), head))
	more := 0
	if len(rows) > floorAllRows {
		more = len(rows) - floorAllRows
		rows = rows[:floorAllRows]
	}
	for _, it := range rows {
		b.WriteByte('\n')
		b.WriteString(FloorRow(it, now))
		if asks {
			// What the job asks the person, in the wait's own words.
			// Public acts that wait are already said whole by the row.
			if w, _ := factory.WaitsOnPerson(it); strings.TrimSpace(w.Words) != "" && w.Action != factory.ActApproveActs {
				b.WriteString(" · asks: " + floorCut(oneLine(w.Words), floorAsksCut))
			}
		}
	}
	if more > 0 {
		fmt.Fprintf(&b, "\nand %d more", more)
	}
	return b.String()
}

// floorNeedsPerson says the job waits on the person now: its stored wait is
// theirs, and they have not put it away.
func floorNeedsPerson(it factory.Item) bool {
	w, yours := factory.WaitsOnPerson(it)
	return yours && !w.PutAway
}

// floorWaitSince is when the job's wait began, and when the job was made for
// a wait that carries no clock.
func floorWaitSince(it factory.Item) time.Time {
	if w := it.Wait; w != nil && !w.Since.IsZero() {
		return w.Since
	}
	return it.Created
}

// floorOpen is every item still the factory's work: not dismissed, and not
// shipped unless it is still following its pull request.
func floorOpen(snap factory.Snapshot) []factory.Item {
	var open []factory.Item
	for _, it := range snap.Items {
		if it.State != factory.StateDismissed && (it.State != factory.StateShipped || floorFollowing(it)) {
			open = append(open, it)
		}
	}
	return open
}

// floorWithCounts puts the factory's counts on the first line of an answer
// that is not a table of open items (what shipped, or an ask nothing
// answers), so those say how the factory stands too: "nothing is waiting" is
// worth more beside what is running. Nothing is added when nothing is open.
func floorWithCounts(text string, open []factory.Item) string {
	counts := floorCounts(open)
	if counts == "" {
		return text
	}
	head, rest, more := strings.Cut(text, "\n")
	head += " · in the factory: " + counts
	if more {
		return head + "\n" + rest
	}
	return head
}

// floorSummary is the table's head: what was asked, how many rows answer it,
// and THE FLOOR'S COUNTS BY STATE, each left out when it is nothing. The
// counts are of the whole floor and not of the rows, so "how many are
// running" is answered by whichever table was read: a manager that had to
// count rows of a table cut at fifty answered with the cut.
func floorSummary(open []factory.Item, rows int, head string) string {
	parts := []string{head, strconv.Itoa(rows) + " " + floorPlural(rows, "item", "items")}
	if counts := floorCounts(open); counts != "" {
		parts = append(parts, "in the factory: "+counts)
	}
	if n := len(floorKeep(open, func(it factory.Item) bool { return it.Marked && it.State == factory.StateNew })); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" selected")
	}
	return strings.Join(parts, " · ")
}

// The states a count names, in the order a person asks about them, each in
// the words the rows use.
//
// A JOB IS COUNTED ONCE, AND WHO IT WAITS ON COMES FIRST ([floorBucket]): a
// job that waits on the person is counted there whatever its stored state, so
// the count agrees with the rows `waiting` and `needs_you` list.
const (
	floorCountRunning   = "running"
	floorCountQueued    = "queued to run"
	floorCountMainAgent = "with a main agent"
	floorCountWaiting   = "waiting on the person"
	floorCountPutAway   = "put away by the person"
	floorCountFollowing = "following a pull request"
	floorCountLanded    = "landed"
	floorCountNew       = "new"
)

var floorCountOrder = []string{floorCountRunning, floorCountQueued, floorCountMainAgent, floorCountWaiting, floorCountPutAway, floorCountFollowing, floorCountLanded, floorCountNew}

// floorBucket is the one count an open job is in, "" for none.
func floorBucket(it factory.Item) string {
	if w, yours := factory.WaitsOnPerson(it); yours {
		if w.PutAway {
			return floorCountPutAway
		}
		return floorCountWaiting
	}
	switch {
	case factory.WaitsOnMainAgent(it):
		return floorCountMainAgent
	case it.State == factory.StateRunning:
		return floorCountRunning
	case it.State == factory.StateQueued, factory.WaitsOnRunner(it):
		return floorCountQueued
	case it.State == factory.StateShipped && floorFollowing(it):
		return floorCountFollowing
	case it.State == factory.StateLanded:
		return floorCountLanded
	case it.State == factory.StateNew:
		return floorCountNew
	}
	return ""
}

// floorCounts is items counted by state in one line, `2 running, 1 waiting on
// the person, 40 new`, with nothing said for a state nothing is in.
func floorCounts(items []factory.Item) string {
	counts := map[string]int{}
	for _, it := range items {
		counts[floorBucket(it)]++
	}
	var parts []string
	for _, word := range floorCountOrder {
		if n := counts[word]; n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+word)
		}
	}
	return strings.Join(parts, ", ")
}

// floorReposAnswer is every repository the floor has jobs on, one a line with
// its jobs counted by state, the busiest first. It answers "how many
// repositories are active" and "where is the work", which no table of jobs cut
// at [floorAllRows] could. Dismissed jobs are counted nowhere; a repository
// whose every job is done says so rather than being left out, so the count of
// lines is the count of repositories the factory has worked on.
func floorReposAnswer(snap factory.Snapshot) string {
	type line struct {
		name          string
		open, done    int
		items, active []factory.Item
	}
	by := map[string]*line{}
	var order []*line
	for _, it := range snap.Items {
		if it.State == factory.StateDismissed {
			continue
		}
		l := by[it.Repo]
		if l == nil {
			l = &line{name: it.Repo}
			by[it.Repo] = l
			order = append(order, l)
		}
		if it.State == factory.StateShipped && !floorFollowing(it) {
			l.done++
			continue
		}
		l.open++
		l.items = append(l.items, it)
		if it.State != factory.StateNew {
			l.active = append(l.active, it)
		}
	}
	if len(order) == 0 {
		return floorNothing
	}
	sort.SliceStable(order, func(i, j int) bool {
		if len(order[i].active) != len(order[j].active) {
			return len(order[i].active) > len(order[j].active)
		}
		if order[i].open != order[j].open {
			return order[i].open > order[j].open
		}
		return order[i].name < order[j].name
	})
	withOpen, withActive := 0, 0
	for _, l := range order {
		if l.open > 0 {
			withOpen++
		}
		if len(l.active) > 0 {
			withActive++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "repositories · %d with jobs · %d with open jobs · %d with a job running, queued, waiting or following a pull request", len(order), withOpen, withActive)
	for _, l := range order {
		name := l.name
		if strings.TrimSpace(name) == "" {
			name = "(no repository)"
		}
		b.WriteString("\n" + name)
		if l.open > 0 {
			fmt.Fprintf(&b, " · %d open: %s", l.open, floorCounts(l.items))
		}
		if l.done > 0 {
			fmt.Fprintf(&b, " · %d done", l.done)
		}
	}
	return b.String()
}

// FloorRow is one item in one line: its ref and title, then every fact the
// floor knows about it, each labelled, and NOTHING FOR WHAT IS NOT KNOWN (the
// emptiness law) — an item with no estimate says no estimate.
func FloorRow(it factory.Item, now time.Time) string {
	parts := []string{it.Ref() + " " + floorCut(oneLine(it.Title), floorTitleCut)}
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	add(string(it.Kind))
	add(it.Repo)
	// THE SOURCE IS SAID: a label is "size S", a completed read is "read says
	// S" and a pull request's measured lines "size S (diff)". An item with no
	// label, diff or read says nothing (ShownSize).
	if s, from := factory.ShownSize(it); s != "" {
		switch from {
		case factory.ShownRead:
			add("read says " + s)
		case factory.ShownDiff:
			add("size " + s + " (diff)")
		default:
			add("size " + s)
		}
	}
	if r := it.Triage.Rubric; r != nil {
		add("complexity " + strconv.Itoa(r.Complexity()) + "/10, " + r.Band() + " (" + r.From + ")")
	}
	if p := factory.PriorityFor(it); p != nil {
		add("priority " + factory.PriorityShort(p.Value) + " (" + p.Value + ")")
	} else {
		add(floorPriority(it.Triage))
	}
	// A JOB FOLLOWING ITS PULL REQUEST IS ONE STATE, IN A PERSON'S WORDS. Its
	// stored state is shipped, and the row once said `shipped` beside a step
	// count that was not finished and named no step, so the foreman read back
	// "One job is active … State: shipped, step 3 of 4 done" to the person.
	// It is following its pull request, on Follow PR, whether it is resting
	// (shipped) or running a repair (running, on followpr); a shipped job
	// that is not following one is done.
	onFollow := it.Stream != nil && it.Stream.Cur >= 0 && it.Stream.Cur < len(it.Stream.Phases) && it.Stream.Phases[it.Stream.Cur].Name == factory.FollowPRStep
	following := floorFollowing(it) && (it.State == factory.StateShipped || it.State == factory.StateRunning && onFollow)
	state := string(it.State)
	switch {
	case following:
		state = floorFollowingWords
	case it.State == factory.StateShipped:
		state = "done"
	}
	if it.Marked && it.State == factory.StateNew {
		state += ", selected"
	}
	add(state)
	// THE ONE STEP COUNT ([factory.StepCount]) and the step it is on, for a
	// job that has begun: the count the job's own page shows.
	if it.Stream != nil {
		if done, total := factory.StepCount(it); total > 0 {
			step := fmt.Sprintf("steps %d of %d done", done, total)
			switch c := it.Stream.Cur; {
			case following:
				step += ", on " + floorFollowStep
			case c >= 0 && c < len(it.Stream.Phases) && it.State != factory.StateShipped:
				step += ", on " + it.Stream.Phases[c].Name
			}
			add(step)
		}
	}
	if pr := it.PullRequest; pr != nil && pr.Number > 0 {
		add(fmt.Sprintf("PR #%d %s", pr.Number, pr.State))
	}
	if age := floorAge(now, it.Created); age != "" {
		add(age + " old")
	}
	if len(it.Triage.Risk) > 0 {
		add("risk: " + strings.Join(it.Triage.Risk, ", "))
	}
	// WHAT THE JOB WAITS ON AND WHAT IT ACCEPTS NOW, from the wait and offers
	// the store wrote ([factory.FloorWaitWords]). A job that has not started
	// or is finished waits on nothing and says nothing here.
	if waits := factory.FloorWaitWords(it); waits != "" {
		if w := it.Wait; w != nil && w.Who != factory.WaitWorking {
			if age := floorAge(now, w.Since); age != "" {
				waits += ", for " + age
			}
		}
		add(waits)
		if offers := factory.PersonOffers(it); len(offers) > 0 {
			add("accepts from the person: " + strings.Join(offers, ", "))
		}
	}
	// PUBLIC ACTS THAT WAIT FOR THE PERSON'S YES, all of them in one line.
	if acts := factory.ProposedActs(it); len(acts) > 0 {
		add("waiting for approval to " + factory.ActsSummary(acts))
	}
	return strings.Join(parts, " · ")
}

// FloorItemText is one item in full: its row, then its read, its facts, its
// stages, its question and its body cut to [floorBodyCut] characters. Every
// line is left out when there is nothing to say.
func FloorItemText(snap factory.Snapshot, it factory.Item, now time.Time) string {
	var b strings.Builder
	line := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	line(FloorRow(it, now))
	if r := oneLine(it.Triage.Read); r != "" {
		line("read: " + r)
	}
	var facts []string
	fact := func(label, value string) {
		if value = strings.TrimSpace(value); value != "" {
			facts = append(facts, label+value)
		}
	}
	fact("type ", it.Triage.Type)
	fact("area ", it.Triage.Area)
	if it.Triage.Readiness > 0 {
		fact("readiness ", strconv.Itoa(it.Triage.Readiness))
	}
	fact("author ", it.Author)
	fact("tier ", string(it.Tier))
	if it.Stream != nil || it.Autonomy != "" {
		// The mode says who decides what on the job: in Auto its main agent
		// pushes and replies itself, and merging is the person's in both.
		if factory.Automatic(it) {
			fact("mode ", "Auto")
		} else {
			fact("mode ", "Manual")
		}
	}
	if it.Cap > 0 {
		fact("budget ", floorDollars(it.Cap))
	}
	fact("may repeat ", it.Triage.Dup)
	if len(it.Labels) > 0 {
		fact("labels ", strings.Join(it.Labels, ", "))
	}
	fact("checks ", it.Checks)
	line(strings.Join(facts, " · "))
	for _, q := range it.Triage.Questions {
		if q = oneLine(q); q != "" {
			line("open question from the read: " + q)
		}
	}
	stages := it.Stages
	if len(stages) == 0 {
		if repo, ok := snap.RepoNamed(it.Repo); ok {
			stages = repo.Recipe.For(it.Kind)
		}
	}
	if lines := factory.StageLines(stages); len(lines) > 0 {
		line("stages:")
		for _, l := range lines {
			line(l)
		}
	}
	// THE WAIT IN FULL: who it waits on and what it accepts, what it asks and
	// the choices its question lists, and the public acts that wait, each said
	// whole so the person can be shown exactly what an approval would send.
	if factory.FloorWaitWords(it) == "" {
		// A job that has not started, or is finished: the row says nothing of
		// a wait, so the read says where it stands and what it accepts.
		line(factory.FloorWaitLine(it))
	}
	switch w, yours := factory.WaitsOnPerson(it); {
	case yours:
		if q := floorAsked(w, it); q != "" {
			line("waiting on the person for: " + q)
		}
		if _, choices, _ := factory.QuestionForm(it); len(choices) > 0 {
			said := "its question's choices: " + strings.Join(choices, ", ")
			if slices.Contains(choices, factory.ChoiceCancel) {
				// Stopping a job is never pressed from here (tools_floor_hand.go).
				said += " (" + factory.ChoiceCancel + " only on the job page)"
			}
			line(said)
		}
	case factory.WaitsOnMainAgent(it):
		if q := floorAsked(w, it); q != "" {
			line("its main agent is deciding: " + q)
		}
	}
	if offers := factory.MainAgentOffers(it); len(offers) > 0 {
		line("accepts from its main agent now: " + strings.Join(offers, ", "))
	}
	for _, acts := range factory.ManagerActsWords(it) {
		line(acts)
	}
	if body := strings.TrimSpace(it.Body); body != "" {
		line("body:")
		line(floorCut(body, floorBodyCut))
	}
	return strings.TrimSpace(b.String())
}

func floorKeep(items []factory.Item, keep func(factory.Item) bool) []factory.Item {
	var out []factory.Item
	for _, it := range items {
		if keep(it) {
			out = append(out, it)
		}
	}
	return out
}

func floorTop(items []factory.Item, n int) []factory.Item {
	if len(items) > n {
		return items[:n]
	}
	return items
}

// floorPriority is `p1 money is wrong`, or "" when the read gave none.
func floorPriority(t factory.Triage) string {
	if t.Priority <= 0 {
		return ""
	}
	p := "p" + strconv.Itoa(t.Priority)
	if r := oneLine(t.Reason); r != "" {
		p += " " + r
	}
	return p
}

// floorDollars is an amount the way the floor writes an estimate: cents, and
// a tenth of a cent under one.
func floorDollars(usd float64) string {
	if usd < 0.01 {
		return "$" + strconv.FormatFloat(usd, 'f', 4, 64)
	}
	return "$" + strconv.FormatFloat(usd, 'f', 2, 64)
}

// floorAge is how long ago at was, in the floor's own short words (`40m`,
// `5h`, `3d`), or "" when it is not known.
func floorAge(now, at time.Time) string {
	if at.IsZero() || now.Before(at) {
		return ""
	}
	d := now.Sub(at)
	switch {
	case d < time.Hour:
		return strconv.Itoa(max(int(d/time.Minute), 1)) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

func floorPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// floorCut cuts s to n characters, on a rune boundary, with an ellipsis.
func floorCut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

// floorShipped is every job that shipped since the start of the floor's day,
// or of the day `days` back with today counted as the first, each with its
// pull request and its change, for the foreman to sum up; one line says none
// when none did. A job counts on the day it was done, or the day its pull
// request merged when no done time was kept. The head counts the pull requests
// merged among them, which is what "how many did we merge" asks.
func floorShipped(snap factory.Snapshot, now time.Time, days int) string {
	if days < 1 {
		days = 1
	}
	y, m, d := now.Date()
	since := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	window := "today"
	if days > 1 {
		window = fmt.Sprintf("in the last %d days", days)
	}
	type shipped struct {
		it factory.Item
		at time.Time
	}
	var rows []shipped
	merged := 0
	for _, it := range snap.Items {
		at := it.DoneAt
		if at.IsZero() && it.PullRequest != nil {
			at = it.PullRequest.MergedAt
		}
		if it.State == factory.StateShipped && !at.Before(since) {
			rows = append(rows, shipped{it, at})
			if it.PullRequest != nil && it.PullRequest.State == factory.PRMerged {
				merged++
			}
		}
	}
	if len(rows) == 0 {
		return "nothing shipped " + window
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].at.After(rows[j].at) })
	var b strings.Builder
	fmt.Fprintf(&b, "shipped %s · %d %s", window, len(rows), floorPlural(len(rows), "job", "jobs"))
	if merged > 0 {
		fmt.Fprintf(&b, " · %d %s merged", merged, floorPlural(merged, "pull request", "pull requests"))
	}
	more := 0
	if len(rows) > floorAllRows {
		more = len(rows) - floorAllRows
		rows = rows[:floorAllRows]
	}
	for _, row := range rows {
		it := row.it
		b.WriteString("\n")
		b.WriteString(FloorRow(it, now))
		if days > 1 && !row.at.IsZero() {
			b.WriteString(" · shipped " + row.at.In(now.Location()).Format("2006-01-02"))
		}
		if pr := it.PullRequest; pr != nil && strings.TrimSpace(pr.URL) != "" {
			b.WriteString(" · " + pr.URL)
		}
		if it.Change != nil && it.Change.Stat != nil && it.Change.Stat.Files > 0 {
			st := it.Change.Stat
			fmt.Fprintf(&b, " · %d %s, +%d −%d", st.Files, floorPlural(st.Files, "file", "files"), st.Added, st.Removed)
		}
		if how := strings.TrimSpace(it.DoneHow); how != "" {
			b.WriteString(" · " + how)
		}
	}
	if more > 0 {
		fmt.Fprintf(&b, "\nand %d more, older", more)
	}
	return b.String()
}

// floorFollowing says a published job is still at work on its pull request:
// the factory follows it (checks, reviews, readiness) and it still waits on
// Mark ready or Merge. The live audit's foreman called such a job's factory
// idle twice: first because a published job read as finished, then because
// the factory had not yet read the pull request's state, so only a pull
// request known merged or closed ends the following.
// floorFollowingWords and floorFollowStep are how a job following its pull
// request reads on the floor: the job page's own name for that step.
const (
	floorFollowingWords = "following its pull request (Follow PR)"
	floorFollowStep     = "Follow PR"
)

func floorFollowing(it factory.Item) bool {
	pr := it.PullRequest
	return pr != nil && pr.Number > 0 && pr.State != factory.PRMerged && pr.State != factory.PRClosed && it.DoneHow == ""
}

// floorAsked is what the job asks whoever it waits on, on one line: the
// wait's own words, and the job's whole question when the wait keeps only its
// first line.
func floorAsked(w factory.Wait, it factory.Item) string {
	asked, question := oneLine(w.Words), oneLine(it.Question)
	if asked == "" || (question != "" && strings.HasPrefix(question, asked)) {
		return question
	}
	return asked
}
