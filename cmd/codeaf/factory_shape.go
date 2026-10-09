package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE MANAGER SHAPES THE RUN ─────────────────────────────────────────────
//
// The manager is the author of the run (the owner's decision of 2026-10-08).
// Two doors make it so, and this file is both:
//
//   - `factory_run` on the manager's own belt ([runDoor]): what the manager
//     sets while the person talks to it is made at once through the floor's
//     edit door ([factory.Seam.Edit]), before a run or on the stages a run has
//     not started;
//   - the runner's shaping turn ([shapeTurns], wired as Options.Shape): at
//     launch of an item the manager never shaped, the runner gives the
//     manager ONE turn of its own conversation with an ask
//     (`Shape the run for this item now. …`), at the cheap thinking unless the
//     item reads as large (or, when a window here holds the conversation,
//     through that window at the person's own thinking), and collects the edit its `factory_run` call made,
//     for the runner to apply itself.
//
// THE SHAPING TURN COLLECTS, IT DOES NOT WRITE. Its `factory_run` door checks
// the edit against the item in hand ([factory.Edit]) and answers the line, so
// the manager is told what it set, but the item is changed only by the runner,
// under its own write, with the phases rebuilt in the same step.

// The runner's asks, said once so the manual and the tests quote the same
// words. THERE IS NO ASK ON A STEER: what the person types in the manager's
// chat is already a turn of the manager, which reshapes with `factory_run`
// when that is what they meant; a second turn on the same words was the
// doubled answer of the owner's run of 2026-10-09.
const (
	shapeAsk     = "Shape the run for this item now."
	shapeAskSaid = " What the person said: %s"
)

// shapeAskFor is the launch's ask, with what the person typed before the run.
func shapeAskFor(said []string) string {
	var words []string
	for _, s := range said {
		if s = strings.Join(strings.Fields(s), " "); s != "" {
			words = append(words, s)
		}
	}
	if len(words) == 0 {
		return shapeAsk
	}
	return shapeAsk + fmt.Sprintf(shapeAskSaid, strings.Join(words, " · "))
}

// managerTurn is what one shaping turn needs of an open conversation; the
// session's *Agent answers it, and a test hands in a fake.
type managerTurn interface {
	SubmitRunnerNote(ctx context.Context, text string) (<-chan session.Event, error)
	Interrupt()
	Close() error
}

// idleWaiter is a managerTurn that can be waited on until its conversation is
// between turns: the live road's, where the person's own turn may be running.
type idleWaiter interface {
	WaitIdle(ctx context.Context) error
}

// shapeTurns gives an item's manager one turn to shape its run.
type shapeTurns struct {
	dirs func(repo string) string
	// live answers the item's manager conversation when a window of THIS
	// process holds it ([session.LiveAgentFor]), as a turn through it with
	// door lent to that turn alone.
	live func(transcript string, door session.RunDoor) (managerTurn, bool)
	// open opens the item's manager conversation for one turn, with door as
	// its `factory_run`, at the cheap thinking when cheap.
	open func(ctx context.Context, it factory.Item, door session.RunDoor, cheap bool) (managerTurn, error)
}

// factoryShapeTurns is this process's shaping turns, opened from parent, the
// launch's own config.
func factoryShapeTurns(st *store.Store, workspace, profileDir string, parent session.Config) *shapeTurns {
	return &shapeTurns{
		dirs: factoryRepoDirs(st, workspace),
		live: liveManagerTurnFor,
		open: func(_ context.Context, it factory.Item, door session.RunDoor, cheap bool) (managerTurn, error) {
			return openManagerTurn(parent, workspace, profileDir, it, door, cheap)
		},
	}
}

// ── THE LIVE ROAD: THE WINDOW HOLDS THE MANAGER ─────────────────────────────
//
// The most natural flow is the person opening the item, telling the manager
// what they want in its window, going back to the floor and pressing `r`. The
// window's conversation holds the journal's lock, so a second open is
// refused. The shaping turn then runs AS A TURN OF THAT CONVERSATION: the
// window shows the manager think, its `factory_run` on that one turn is the
// collecting door (lent to the turn, [session.Agent.SubmitRunnerNoteThrough]),
// and the runner applies the edit itself, as on the road that opens. The
// thinking is the person's setting there: this road does not make it cheap.
// Closing the turn closes nothing; the window keeps its conversation.

// liveManagerTurn is one shaping turn through a conversation open here.
type liveManagerTurn struct {
	agent *session.Agent
	door  session.RunDoor
}

func (l liveManagerTurn) SubmitRunnerNote(ctx context.Context, text string) (<-chan session.Event, error) {
	return l.agent.SubmitRunnerNoteThrough(ctx, text, l.door)
}
func (l liveManagerTurn) Interrupt()                         { l.agent.Interrupt() }
func (l liveManagerTurn) WaitIdle(ctx context.Context) error { return l.agent.WaitIdle(ctx) }

// Close lets the turn go and leaves the window's conversation open.
func (l liveManagerTurn) Close() error { return nil }

// liveManagerTurnFor is the live road when the transcript is open here.
func liveManagerTurnFor(transcript string, door session.RunDoor) (managerTurn, bool) {
	agent, ok := session.LiveAgentFor(transcript)
	if !ok {
		return nil, false
	}
	return liveManagerTurn{agent: agent, door: door}, true
}

// Shape is the runner's Options.Shape: the launch's one turn.
func (s *shapeTurns) Shape(ctx context.Context, it factory.Item, said []string) (factory.RunEdit, string, error) {
	return s.run(ctx, it, shapeAskFor(said), false)
}

// shapeCheap says whether the shaping turn thinks cheap: always, unless the
// item's read says it is large.
func shapeCheap(it factory.Item) bool {
	return !strings.EqualFold(strings.TrimSpace(it.Triage.Size), "L")
}

// run is one turn: the ask in, the turn drained, and the edit collected.
func (s *shapeTurns) run(ctx context.Context, it factory.Item, ask string, inRun bool) (factory.RunEdit, string, error) {
	if s == nil || s.open == nil {
		return factory.RunEdit{}, "", errors.New("no manager to ask")
	}
	door := &collectRunDoor{it: it, recipe: factoryItemRecipe(s.dirs, it), inRun: inRun}
	reply, err := s.through(ctx, it, ask, door)
	if err != nil {
		return factory.RunEdit{}, "", err
	}
	return door.collected(), reply, nil
}

// through is one manager turn on ask with door as its `factory_run` (and, on
// an inbox turn, its `factory_answer`): through the window's conversation
// when this process holds it, else opened for the turn. It answers what the
// manager said.
func (s *shapeTurns) through(ctx context.Context, it factory.Item, ask string, door session.RunDoor) (string, error) {
	if s == nil || s.open == nil {
		return "", errors.New("no manager to ask")
	}
	var turn managerTurn
	if s.live != nil {
		turn, _ = s.live(it.Talk, door)
	}
	if turn == nil {
		opened, err := s.open(ctx, it, door, shapeCheap(it))
		if errors.Is(err, session.ErrSessionLocked) {
			// ANOTHER PROCESS HOLDS IT (a window on the engine host): this
			// runner cannot give it a turn, and says so by name. No retry.
			return "", fmt.Errorf("%w: %v", factoryrun.ErrManagerAway, err)
		}
		if err != nil {
			return "", err
		}
		turn = opened
	}
	defer turn.Close()
	events, err := s.submit(ctx, turn, ask)
	if err != nil {
		return "", err
	}
	var reply strings.Builder
	var failure error
	for {
		select {
		case ev, open := <-events:
			if !open {
				if failure != nil {
					return "", failure
				}
				return strings.TrimSpace(reply.String()), nil
			}
			switch ev.Kind {
			case session.EventTextDelta:
				reply.WriteString(ev.Text)
			case session.EventError:
				failure = ev.Err
				if failure == nil {
					failure = errors.New("the turn failed without a reason")
				}
			}
		case <-ctx.Done():
			turn.Interrupt()
			return "", ctx.Err()
		}
	}
}

// submit puts the ask in. A conversation the window holds that is in the
// middle of a turn of its own is waited on, for at most half of what is left
// of the shaping wait so the retry has the other half to answer, and asked
// ONCE more; busy again is [factoryrun.ErrManagerBusy].
func (s *shapeTurns) submit(ctx context.Context, turn managerTurn, ask string) (<-chan session.Event, error) {
	events, err := turn.SubmitRunnerNote(context.WithoutCancel(ctx), ask)
	if !errors.Is(err, session.ErrConversationBusy) {
		return events, err
	}
	waiter, ok := turn.(idleWaiter)
	if !ok {
		return nil, fmt.Errorf("%w: %v", factoryrun.ErrManagerBusy, err)
	}
	wait := ctx
	if deadline, has := ctx.Deadline(); has {
		var cancel context.CancelFunc
		wait, cancel = context.WithTimeout(ctx, time.Until(deadline)/2)
		defer cancel()
	}
	if werr := waiter.WaitIdle(wait); werr != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	events, err = turn.SubmitRunnerNote(context.WithoutCancel(ctx), ask)
	if errors.Is(err, session.ErrConversationBusy) {
		return nil, fmt.Errorf("%w: %v", factoryrun.ErrManagerBusy, err)
	}
	return events, err
}

// collectRunDoor is `factory_run` on a shaping turn: each call is checked
// against the item as the calls before it left it, answered with its line,
// and kept, and nothing is written.
type collectRunDoor struct {
	mu     sync.Mutex
	it     factory.Item
	recipe factory.Recipe
	inRun  bool
	got    factory.RunEdit
}

// EditRun checks and keeps one edit.
func (d *collectRunDoor) EditRun(_ context.Context, _ string, e factory.RunEdit) (factory.Item, []string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var next factory.Item
	var lines []string
	var err error
	if d.inRun {
		next, lines, err = factory.Edit(d.it, e, d.recipe, factory.EditInRun)
	} else {
		next, lines, err = factory.Edit(d.it, e, d.recipe, factory.EditBeforeRun)
	}
	if err != nil {
		return factory.Item{}, nil, err
	}
	d.it = next
	d.got = mergeRunEdit(d.got, e)
	return next, lines, nil
}

// Manages says the conversation on this turn manages an item: the shaping
// turn is given only to an item's manager ([session.RunManagerDoor]).
func (d *collectRunDoor) Manages(string) bool { return true }

// collected is every edit the turn made, as one.
func (d *collectRunDoor) collected() factory.RunEdit {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.got
}

// mergeRunEdit is two edits as one, THE LATER WINNING wherever both touch a
// stage: a later ask or thinking replaces the earlier one, and a later switch
// undoes an earlier one, so a stage skipped and then switched on again in the
// same turn is in neither list, and the edit applied to the item the turn
// started from leaves it exactly as the manager's last call left it. (The
// owner's run of 2026-10-09: skip then switch on was kept as both lists, which
// [factory.Edit] applies on before skip, so the stages ended off while the
// manager had been told they were on.)
func mergeRunEdit(a, b factory.RunEdit) factory.RunEdit {
	out := a
	out.Add = append(append([]factory.Added(nil), a.Add...), b.Add...)
	merge := func(x, y map[string]string) map[string]string {
		if len(x) == 0 && len(y) == 0 {
			return nil
		}
		m := map[string]string{}
		for k, v := range x {
			m[k] = v
		}
		for k, v := range y {
			m[k] = v
		}
		return m
	}
	out.Ask = merge(a.Ask, b.Ask)
	out.Thinking = merge(a.Thinking, b.Thinking)
	out.On = switchList(a.On, b.Skip, b.On)
	out.Skip = switchList(a.Skip, b.On, b.Skip)
	if strings.TrimSpace(b.Why) != "" {
		out.Why = b.Why
	}
	if strings.TrimSpace(b.By) != "" {
		out.By = b.By
	}
	return out
}

// switchList is the earlier names with every one the later edit switched the
// other way taken out, then the later names, each name once.
func switchList(earlier, undone, later []string) []string {
	gone := map[string]bool{}
	for _, n := range undone {
		gone[strings.ToLower(strings.TrimSpace(n))] = true
	}
	seen := map[string]bool{}
	var out []string
	keep := func(n string) {
		k := strings.ToLower(strings.TrimSpace(n))
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, n)
	}
	for _, n := range earlier {
		if !gone[strings.ToLower(strings.TrimSpace(n))] {
			keep(n)
		}
	}
	for _, n := range later {
		keep(n)
	}
	return out
}

// openManagerTurn opens the item's manager conversation for one turn. It is a
// conversation nobody is at the keyboard of for this turn: no card door is on
// its belt (a card nobody answers holds the turn until the wait ends), the
// approvals are the person's own rows, headless, and `factory_run` is door.
// A conversation a window of THIS process holds never reaches here (the live
// road above); one another process holds is refused by its lock, and the run
// goes on with the recipe, saying so by name.
func openManagerTurn(parent session.Config, workspace, profileDir string, it factory.Item, door session.RunDoor, cheap bool) (managerTurn, error) {
	transcript := strings.TrimSpace(it.Talk)
	if transcript == "" {
		return nil, errors.New("the item has no manager conversation")
	}
	cfg, err := v3Reopen(parent, transcript, workspace)
	if err != nil {
		return nil, err
	}
	where := strings.TrimSpace(cfg.Workspace)
	if where == "" {
		where = workspace
	}
	cfg.Interactive = false
	cfg.AskConsent = false
	cfg.Factory, cfg.Recipe, cfg.FactoryItem, cfg.Floor, cfg.Stage = nil, nil, nil, nil, nil
	cfg.FactoryRun = door
	if cheap {
		cfg.Effort = effort.Low
	}
	gate := v3ApprovalGate{workspace: where, profileDir: profileDir, headless: true}
	cfg.ApprovalGate = gate
	cfg.ApprovalPosture = session.PostureAsk
	if cfg.ApprovalPolicy, cfg.Guardian, err = gate.Build(session.PostureAsk); err != nil {
		return nil, err
	}
	cfg, open := v3Shape(cfg, v3Lanes{})
	agent, _, _, err := openV3Agent(cfg, where, open)
	if err != nil {
		return nil, err
	}
	return agent, nil
}

// runDoor is `factory_run`'s door for a conversation the person talks to: the
// floor's edit door over st, for the one item whose manager the asking
// conversation is. A NIL STORE IS A NIL DOOR, for [factoryDoor]'s reason.
func runDoor(st *store.Store, workspace string) session.RunDoor {
	if st == nil {
		return nil
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(factoryRepoDirs(st, workspace)))
	if seam.Edit == nil {
		return nil
	}
	return seamRunDoor{st: st, workspace: workspace, edit: seam.Edit}
}

// seamRunDoor finds the item a conversation manages and edits it through the
// seam.
type seamRunDoor struct {
	st        *store.Store
	workspace string
	edit      func(ctx context.Context, id int, e factory.RunEdit) (factory.Item, []string, error)
}

// errNotAManager is `factory_run` asked by a conversation that manages no item.
var errNotAManager = errors.New("this conversation is not the manager of any item on the floor")

// EditRun edits the item whose conversation is the asking one.
func (d seamRunDoor) EditRun(ctx context.Context, conversation string, e factory.RunEdit) (factory.Item, []string, error) {
	it, err := d.managed(conversation)
	if err != nil {
		return factory.Item{}, nil, err
	}
	return d.edit(ctx, it.ID, e)
}

// managed is the item whose conversation is the asking one.
func (d seamRunDoor) managed(conversation string) (factory.Item, error) {
	want := sameFileKey(conversation)
	if want == "" || d.st == nil {
		return factory.Item{}, errNotAManager
	}
	items, err := d.st.List()
	if err != nil {
		return factory.Item{}, err
	}
	for _, it := range items {
		if strings.TrimSpace(it.Talk) != "" && sameFileKey(it.Talk) == want {
			return it, nil
		}
	}
	return factory.Item{}, errNotAManager
}

// sameFileKey is a path as one spelling, links resolved.
func sameFileKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}

// ── `shape steps`: THE MANAGER SHAPES THE STEPS WHEN THE PERSON ASKS ─────────
//
// The owner's journey (2026-10-09): OPENING AN ISSUE RUNS NOTHING. The steps
// an item shows are the recipe's until somebody asks the manager to shape
// them: ▶ run on an item never shaped (the runner's own shaping turn), words
// in the manager's chat (its `factory_run`), or `shape steps`, which is this
// door ([factory.Seam.Shape]). It gives the manager one turn, the launch's own
// ([shapeTurns.run], nothing said, through the window's conversation when this
// process holds it, so it streams where the person is looking), and applies
// its edit as the launch applies it ([factoryrun.ApplyShape]) under the
// store's write: to every step before a run, and to the steps not yet started
// during one. The conversation is made when the item has none
// ([managerMaker]); making it asks no model anything.
//
// THE RECIPE STANDING IS A SHAPING TOO: an edit that changes nothing on an
// item that never ran is recorded as [shapeKeptRecord], so ▶ run afterwards
// knows the manager read it and does not spend a second turn on it. A turn
// that did not answer, or an edit refused, records nothing. AN ITEM THE
// MANAGER ALREADY SHAPED IS ANSWERED WITHOUT A TURN ([sayShapedAlready]): from
// then on its steps change where the person talks to the manager.

// shapeKeptRecord is the line an item's record ([factory.Item.Adapted]) keeps
// when the manager read it and kept the recipe. It starts with the manager's
// name, which is how [factoryrun.ShapedByManager] knows it.
const shapeKeptRecord = factory.ByManager + " kept the recipe"

// shapeDoor is the floor's Shape door over one store.
type shapeDoor struct {
	st interface {
		Get(id int) (factory.Item, error)
		Update(id int, change func(*factory.Item) error) error
	}
	// turn is the shaping turn ([shapeTurns.run]); inRun bounds its edit to
	// the steps not yet started.
	turn func(ctx context.Context, it factory.Item, inRun bool) (factory.RunEdit, string, error)
	// make is the item's manager conversation, made when it has none.
	make func(ctx context.Context, it factory.Item) (string, error)
	// say writes a progress line into a conversation.
	say func(transcript, line string) error
	// recipe is the item's recipe.
	recipe func(it factory.Item) factory.Recipe
	wait   time.Duration
	now    func() time.Time

	mu     sync.Mutex
	inTurn map[int]bool
}

// factoryShapeDoor is this process's Shape door, over the shaping turns the
// runner shapes with.
func factoryShapeDoor(st *store.Store, workspace, profileDir string, turns *shapeTurns) func(ctx context.Context, id int) (string, error) {
	if st == nil || turns == nil {
		return nil
	}
	dirs := factoryRepoDirs(st, workspace)
	d := &shapeDoor{
		st: st,
		turn: func(ctx context.Context, it factory.Item, inRun bool) (factory.RunEdit, string, error) {
			return turns.run(ctx, it, shapeAsk, inRun)
		},
		make:   managerMaker(st, workspace, profileDir),
		say:    sessionTalk{}.Say,
		recipe: func(it factory.Item) factory.Recipe { return factoryItemRecipe(dirs, it) },
	}
	return d.Shape
}

// itemRan says whether an item's run has started: its edit is then bounded to
// the steps not yet started.
func itemRan(it factory.Item) bool {
	s := it.Stream
	return s != nil && (!s.Started.IsZero() || len(s.Phases) > 0)
}

// sayShapedAlready is `shape steps` on an item the manager already shaped.
const sayShapedAlready = "the manager already shaped the steps · say what to change in its chat"

// errNothingToShape is `shape steps` on an item whose run is over.
var errNothingToShape = errors.New("its run is over · there are no steps still to come")

// Shape is the door: one turn for item id, or [factory.ShapeAlready] while a
// turn for it is already out.
func (d *shapeDoor) Shape(ctx context.Context, id int) (string, error) {
	// ONE TURN PER ITEM AT A TIME: two presses (or two windows) ask once; the
	// second hears it already out.
	if !d.begin(id) {
		return factory.ShapeAlready, nil
	}
	defer d.end(id)

	it, err := d.st.Get(id)
	if err != nil {
		return "", err
	}
	switch it.State {
	case factory.StateLanded, factory.StateShipped:
		return "", fmt.Errorf("%s: %w", it.Ref(), errNothingToShape)
	}
	// AN ITEM THE MANAGER ALREADY SHAPED IS NEVER SHAPED AGAIN UNASKED: the
	// person changes its steps by saying so in the manager's chat, where the
	// manager has its pen. A turn here would run on words nobody said (the
	// owner's run of 2026-10-09 had two such turns undo the steps the person
	// had just agreed with the manager).
	if factoryrun.ShapedByManager(it) {
		return sayShapedAlready, nil
	}
	chat := strings.TrimSpace(it.Talk)
	if chat == "" {
		made, err := d.make(ctx, it)
		if err != nil {
			return "", err
		}
		if made = strings.TrimSpace(made); made == "" {
			return "", errors.New("the item's conversation could not be made")
		}
		// THE FIRST CONVERSATION WRITTEN IS THE ITEM'S, as the Talk door keeps it.
		if err := d.st.Update(id, func(x *factory.Item) error {
			if strings.TrimSpace(x.Talk) == "" {
				x.Talk = made
			}
			return nil
		}); err != nil {
			return "", err
		}
		if it, err = d.st.Get(id); err != nil {
			return "", err
		}
		chat = strings.TrimSpace(it.Talk)
	}
	wait := d.wait
	if wait <= 0 {
		wait = time.Minute
	}
	turnCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	edit, _, err := d.turn(turnCtx, it, itemRan(it))
	if err != nil {
		line := factoryrun.ShapeFailLine(err)
		d.tell(chat, line)
		return line, errors.New(line)
	}
	now := time.Now()
	if d.now != nil {
		now = d.now()
	}
	line := ""
	var refused error
	if err := d.st.Update(id, func(x *factory.Item) error {
		// A RUN THAT STARTED DURING THE TURN bounds the edit to its tail.
		inRun := itemRan(*x)
		l, err := factoryrun.ApplyShape(x, edit, d.recipe(*x), inRun, now)
		if err != nil {
			refused = err
			return nil
		}
		if l == "" {
			if !inRun && !factoryrun.ShapedByManager(*x) {
				x.Adapted = append(append([]string(nil), x.Adapted...), shapeKeptRecord)
			}
			l = factoryrun.SayShapeStands
		}
		line = l
		return nil
	}); err != nil {
		return "", err
	}
	if refused != nil {
		line := factoryrun.ShapeRefusedLine(refused)
		d.tell(chat, line)
		return line, errors.New(line)
	}
	d.tell(chat, line)
	return line, nil
}

// tell writes line into the conversation, once; a conversation that cannot
// take it now misses it (the item's record keeps what was set).
func (d *shapeDoor) tell(chat, line string) {
	if d.say != nil && chat != "" && strings.TrimSpace(line) != "" {
		_ = d.say(chat, line)
	}
}

// begin marks item id's turn as in flight, and answers false when one is.
func (d *shapeDoor) begin(id int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.inTurn == nil {
		d.inTurn = map[int]bool{}
	}
	if d.inTurn[id] {
		return false
	}
	d.inTurn[id] = true
	return true
}

// end lets item id's turn go.
func (d *shapeDoor) end(id int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.inTurn, id)
}
