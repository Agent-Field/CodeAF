package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE LAW: A HARNESS WAITS FOR ITS COMMANDS AT THE SAME TIME, NOT ONE AFTER
// ANOTHER. Every command still gets the whole of the budget [budgetFor] prices
// it at, measured from the moment it started, and a command that answers inside
// that budget is still delivered — nothing here is a shorter wait. What changes
// is that the waits OVERLAP: three parked waiters cost one budget between them
// instead of three, because the harness starts all three and then waits once.
//
// WHY THIS IS THE FIX AND NOT A SHORTER BUDGET. The note above [cmdBudget] is
// right and is not reopened: the waiters do answer, they answer out of a
// buffered channel in microseconds, and a budget tuned to that claim turns a
// delivered event into a drop the day the box is busy. That note names the seam
// this file is — "the seam which will remove the guessing" — and this is what it
// turned out to be. The guessing is removed by not making the harness's wall
// clock proportional to the number of commands that park, rather than by
// guessing better about how long one of them takes.
//
// WHAT IS ALLOWED TO OVERLAP IS EXACTLY [blockingCommands], MINUS TWO. Running
// two commands beside each other is only safe when neither touches anything the
// other can see, and this package's waiters are written to one shape: receive
// from a channel, wrap what came off it in a message, return. They read no field
// of [app] and call nothing that does — [waitEvent]'s fold loop is the longest
// of them and it touches only locals, [foldsInto] and [isLump]. So they may run
// together, and everything else on the belt still runs one at a time exactly
// where it did, because a command that reads a fake or writes the surface would
// be a data race the moment it had company.
//
// The two exceptions are [app.watchDriving] and [app.watchFollowing], whose
// commands CALL A FUNCTION THE TEST SUPPLIED (`<-changed()`, `<-follow()`)
// rather than receiving from a channel they were handed. What that function
// does is the test's business, so it is not this file's to run concurrently.
// They keep the old serial path and pay the old price.
var overlapExceptions = map[string]bool{
	"watchDriving":   true,
	"watchFollowing": true,
}

// AND THE TICK OVERLAPS TOO, WHICH IS WHERE MOST OF THE TIME IS.
//
// The measurement of 2026-09-08, over the whole package at 564s: 447s of it was
// spent on commands the harness dropped, and bubbletea's tick was 197s of that
// — one line, more than every waiter put together. Another 84s went on ticks
// that did answer, and the answer is usually [frameMsg], which [drive] throws
// away. A tick is a timer that was started when the command was BUILT
// (bubbletea's Tick calls time.NewTimer before it returns the closure), so its
// wait is running whether the harness is standing over it or not; standing over
// it is pure loss.
//
// A tick may overlap because this package's tick callbacks do nothing: every one
// of them is `func(time.Time) tea.Msg { return someMsg{…} }` over values captured
// when the command was built. [TestEveryTickCallbackIsAMessageAndNothingElse]
// reads the package's own source and holds that, because it is the whole reason
// the fn may run on another goroutine — including, for a dropped tick, minutes
// later and inside somebody else's test.
//
// THE ONE EXCEPTION IS taskmention.go's, which calls the reader the surface was
// handed (`read()`), and a test's own function is not this harness's to run
// beside itself. It exists only when the surface has a far task index at all, so
// [harnessDriver.ticksAreMessages] refuses to overlap any tick while the app has
// one. The AST law names that site and this guard in the same breath: a second
// impure tick fails it, and whoever adds one has to come here.
func (d *harnessDriver) ticksAreMessages() bool {
	return d.app != nil && d.app.farTasks == nil
}

// overlappable reports whether a command may be left running while the harness
// gets on with the next message.
//
// It recognises the command by the runtime symbol behind the closure, the same
// key [budgetFor] prices by. A waiter's closure is named for the function that
// built it — `…tui3.(*app).flyPilot.waitPilot.func1` — so the test is whether
// one of [blockingCommands]'s names appears as a whole segment in front of the
// closure's own. [TestTheHarnessOverlapsEveryWaiterItNames] builds real
// commands from the surface and holds this to them, so the convention cannot
// drift silently.
func (d *harnessDriver) overlappable(cmd tea.Cmd) bool {
	symbol := cmdSymbol(cmd)
	if symbol == "" {
		return false
	}
	if symbol == teaTickSymbol {
		return d.ticksAreMessages()
	}
	for _, name := range blockingCommands {
		if overlapExceptions[name] {
			continue
		}
		if strings.Contains(symbol, "."+name+".func") {
			return true
		}
	}
	return false
}

// inlineSpin is how long the harness gives a command it has just started before
// it moves on to the next message.
//
// IT IS AN ORDERING AID AND NEVER A DROP DECISION. A command that does not
// answer inside it is not dropped and is not billed for it: it keeps every
// microsecond of its own budget and is collected the moment it answers. All the
// spin buys is that a waiter answering out of an already-full channel — which is
// what the measurement says almost every answer is — lands in the queue at the
// same place it landed before this file existed, so a suite whose assertions
// were written against the old order still reads the old order. Missing it costs
// a message its position in the queue, which real bubbletea does not promise
// either, and costs nothing else.
//
// The wait is a spin on the scheduler rather than a timer because that is what
// the question is: has the goroutine that was just started had a turn yet. On an
// idle box it has after one or two, and a command that will never answer costs
// the whole spin — tens of microseconds against a budget of 150 milliseconds.
const inlineSpin = 200 * time.Microsecond

// harnessDriver owns the commands one [drive] call has in flight.
//
// It is not safe for concurrent use and does not need to be: the harness runs on
// one goroutine and the commands it starts never touch it. The only crossing is
// each command's own [inflight], written by its goroutine before it sends and
// read by the harness after it receives — which the channel orders.
type harnessDriver struct {
	// app is the surface being driven, or nil for a bare [runCmd]. It is read
	// for one question only — [harnessDriver.ticksAreMessages] — and it is kept
	// current by [drive] on every step, because the door that arms the one
	// impure tick can be opened in the middle of a call (app.go's
	// [app.attachConversation]).
	app  *app
	live []*inflight
	// doorbell is how a command that answered wakes a harness that is waiting on
	// a deadline. It is one slot and the send never blocks: a token already in it
	// is a wake-up already owed, and [harnessDriver.settle] re-checks everything
	// it is holding every time it wakes, so a coalesced wake loses nothing.
	doorbell chan struct{}
}

// inflight is one command the harness has started and not yet finished with.
type inflight struct {
	cmd      tea.Cmd
	started  time.Time
	deadline time.Time
	// answer carries the command's message. It is buffered so that a command
	// which answers after the harness has given up on it still ends rather than
	// parking on a send nobody will take.
	answer chan tea.Msg
	// at is when the command answered, written before the send on `answer` and
	// therefore safe to read after the receive. It is compared against the
	// deadline so that a command which came back late is dropped exactly as the
	// old harness dropped it, rather than delivering a message the suite has
	// never seen.
	at time.Time
}

func newHarnessDriver() *harnessDriver {
	return &harnessDriver{doorbell: make(chan struct{}, 1)}
}

// busy reports whether anything is still running.
func (d *harnessDriver) busy() bool { return len(d.live) > 0 }

// run executes one command and returns whatever it produced right away.
//
// A command that may overlap is started and left running, so this returns
// nothing for it and [harnessDriver.collect] or [harnessDriver.settle] delivers
// its message later. Everything else runs here and now, blocking for its whole
// budget, which is what the harness always did.
func (d *harnessDriver) run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	if d.overlappable(cmd) {
		p := d.start(cmd)
		// The spin is the ordering aid described above [inlineSpin]: if the
		// answer is already there, it is delivered from here, in the place it
		// would have been delivered from before.
		if msg, ok := d.spin(p); ok {
			d.forget(p)
			return d.expand(msg)
		}
		return nil
	}
	return d.expand(runInline(cmd))
}

// expand turns one command's message into the messages the harness queues: a
// batch is opened and each member run in its turn, and everything else is
// itself.
func (d *harnessDriver) expand(msg tea.Msg) []tea.Msg {
	switch produced := msg.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		noteBatch(len(produced))
		var out []tea.Msg
		for _, one := range produced {
			out = append(out, d.run(one)...)
		}
		return out
	default:
		return []tea.Msg{produced}
	}
}

// start puts a command on its own goroutine with its own deadline.
func (d *harnessDriver) start(cmd tea.Cmd) *inflight {
	now := time.Now()
	p := &inflight{
		cmd:      cmd,
		started:  now,
		deadline: now.Add(budgetFor(cmd)),
		answer:   make(chan tea.Msg, 1),
	}
	doorbell := d.doorbell
	go func() {
		msg := cmd()
		p.at = time.Now()
		p.answer <- msg
		select {
		case doorbell <- struct{}{}:
		default:
		}
	}()
	d.live = append(d.live, p)
	return p
}

// spin gives a just-started command the scheduler's attention for [inlineSpin]
// and reports whether it answered in that time.
func (d *harnessDriver) spin(p *inflight) (tea.Msg, bool) {
	until := time.Now().Add(inlineSpin)
	for {
		select {
		case msg := <-p.answer:
			noteCommand(p.cmd, p.at.Sub(p.started), true)
			return msg, true
		default:
		}
		if time.Now().After(until) {
			return nil, false
		}
		runtime.Gosched()
	}
}

// forget drops one command from the live set, because it has been dealt with.
func (d *harnessDriver) forget(gone *inflight) {
	kept := d.live[:0]
	for _, p := range d.live {
		if p != gone {
			kept = append(kept, p)
		}
	}
	d.live = kept
}

// collect takes every command that has answered and reaps every one whose
// budget has run out. IT NEVER BLOCKS.
//
// The live set is walked in the order the commands were started, so the messages
// come back in that order and a run of this harness is repeatable.
func (d *harnessDriver) collect() []tea.Msg {
	if len(d.live) == 0 {
		return nil
	}
	now := time.Now()
	var ready []tea.Msg
	kept := make([]*inflight, 0, len(d.live))
	for _, p := range d.live {
		select {
		case msg := <-p.answer:
			if p.at.After(p.deadline) {
				// It came back after its budget. The old harness had already
				// stopped listening by then and returned nothing, so this
				// returns nothing too: a message the suite has never been given
				// is not one to start giving it here.
				noteCommand(p.cmd, p.at.Sub(p.started), false)
				continue
			}
			noteCommand(p.cmd, p.at.Sub(p.started), true)
			ready = append(ready, msg)
		default:
			if now.After(p.deadline) {
				// Dropped, exactly as before, and its goroutine stays parked on
				// the channel nobody will write to — see [TestMain]'s count.
				noteCommand(p.cmd, budgetFor(p.cmd), false)
				continue
			}
			kept = append(kept, p)
		}
	}
	d.live = kept
	// The expansion happens after the live set has been rebuilt, because opening
	// a batch starts more commands and may not write into the slice being read.
	var out []tea.Msg
	for _, msg := range ready {
		out = append(out, d.expand(msg)...)
	}
	return out
}

// settle blocks until the harness has something to show for its live commands:
// at least one answer, or at least one budget run out. It is what makes the
// waiting overlap — every live command is waited for in the same breath, and the
// wait is over as soon as the FIRST of them is decided.
//
// It always makes progress. Either a command answers, in which case
// [harnessDriver.collect] takes it out of the live set, or the earliest deadline
// passes, in which case that command is reaped. The doorbell may wake it with
// nothing to do — a token left by a command collected already — and then it
// simply waits again, which can happen at most once per answer.
func (d *harnessDriver) settle() []tea.Msg {
	for len(d.live) > 0 {
		earliest := d.live[0].deadline
		for _, p := range d.live[1:] {
			if p.deadline.Before(earliest) {
				earliest = p.deadline
			}
		}
		if wait := time.Until(earliest); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-d.doorbell:
			case <-timer.C:
			}
			timer.Stop()
		}
		before := len(d.live)
		if out := d.collect(); len(out) > 0 {
			return out
		}
		if len(d.live) == before {
			// A doorbell token from a command that was already collected. Wait
			// again; the token is gone now and the deadline is still coming.
			continue
		}
		return nil
	}
	return nil
}

// runInline is the harness's old body, kept for every command that may not
// overlap: run it, and give up on it when its budget is gone.
func runInline(cmd tea.Cmd) tea.Msg {
	started := time.Now()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		noteCommand(cmd, time.Since(started), true)
		return msg
	case <-time.After(budgetFor(cmd)):
		noteCommand(cmd, time.Since(started), false)
		return nil
	}
}

// TestTheHarnessOverlapsEveryWaiterItNames builds a real command from each of
// the waiters this package declares and holds [overlappable] to it.
//
// THE MATCHER IS A STRING TEST AGAINST A RUNTIME SYMBOL, which is exactly the
// kind of thing that goes quietly wrong: a waiter renamed, a closure the
// compiler decided to name differently, an entry added to [blockingCommands]
// whose command turns out never to match. Any of those would leave a waiter
// paying the old serial price with nothing failing, so the harness would get
// slower one waiter at a time and nobody would be told. Here it is told.
//
// The commands are built and never called — a called waiter would park — so the
// channels they are handed are only ever closed.
func TestTheHarnessOverlapsEveryWaiterItNames(t *testing.T) {
	events := make(chan session.Event)
	stirs := make(chan behindStirMsg)
	wakes := make(chan (<-chan session.Event))
	shaping := make(chan shapingRead)
	var a app

	// One command per name in [blockingCommands] that is not an exception.
	// The map is written out rather than derived so that a waiter added to the
	// table without a case here fails this test rather than slipping through.
	built := map[string]tea.Cmd{
		"pumpShaping":      a.pumpShaping(1, shaping),
		"waitDesign":       waitDesign(events, 1),
		"waitEvent":        waitEvent(events, 1),
		"waitGuestNotices": waitGuestNotices(events, 1),
		"waitPilot":        waitPilot(events, 1, 1),
		"waitRoom":         waitRoom(events, 1),
		"waitRun":          waitRun(events, 1),
		"waitSteerLane":    waitSteerLane(events, 1),
		"waitStir":         waitStir(stirs),
		"waitTask":         waitTask(events, 1),
		"waitTitle":        waitTitle(events, 1),
		"waitWake":         waitWake(wakes, 1),
	}
	d := newHarnessDriver()
	for name, cmd := range built {
		if !d.overlappable(cmd) {
			t.Errorf("%s builds a command the harness will not overlap — its symbol is %q, and [overlappable] looks for %q in it, so every call of it pays a whole %s on its own", name, cmdSymbol(cmd), "."+name+".func", cmdBudget)
		}
	}
	// Every name in the table is either built here or an exception. A waiter in
	// neither is one this test is silently not covering.
	for _, name := range blockingCommands {
		if overlapExceptions[name] {
			continue
		}
		if _, ok := built[name]; ok {
			continue
		}
		if strings.HasPrefix(name, "watch") {
			// A watch* builds and returns one of the wait* above, so its
			// command's symbol is that waiter's and it is covered by it.
			continue
		}
		t.Errorf("blockingCommands names %s and this test builds no command from it — add one, or the harness may be failing to overlap it", name)
	}

	// And the two exceptions really are refused, because a test's own function
	// runs inside those and this harness does not run a test's code beside
	// itself.
	a.link.DrivingChanged = func() <-chan struct{} { return make(chan struct{}) }
	a.link.Follow = func() <-chan Following { return make(chan Following) }
	for name, cmd := range map[string]tea.Cmd{
		"watchDriving":   a.watchDriving(),
		"watchFollowing": a.watchFollowing(),
	} {
		if cmd == nil {
			t.Fatalf("%s built no command, so this test is asserting nothing", name)
		}
		if d.overlappable(cmd) {
			t.Errorf("%s is overlapped, and it calls a function the test supplied — two of them could run at once on a fake written for one", name)
		}
	}

	// And the tick, whose whole question is the surface it is being driven on.
	tick := tea.Tick(time.Hour, func(time.Time) tea.Msg { return nil })
	if d.overlappable(tick) {
		t.Error("a bare driver overlaps the tick, and a bare driver cannot see whether the surface has the far task index that arms the one tick callback that calls a test's own function")
	}
	d.app = &a
	if a.farTasks != nil {
		t.Fatal("this test's app was given a far task index, so it cannot ask the question below")
	}
	if !d.overlappable(tick) {
		t.Errorf("a surface with no far task index does not overlap the tick, and the tick is the largest single line in this package's wall clock — 197s of the 447s the harness spent on dropped commands on 2026-09-08")
	}
	a.farTasks = func() ([]session.TaskIndexEntry, bool) { return nil, true }
	if d.overlappable(tick) {
		t.Error("the tick is overlapped on a surface that has a far task index, and taskmention.go's [app.tasksLoaded] arms a tick whose callback calls that index — which is a function the test wrote, running beside the test")
	}
}

// tickCallbackCallers names the ONE place in this package whose tea.Tick
// callback does something other than name a message.
//
// A tick's callback runs on a goroutine of the harness's choosing and, for a
// tick the harness dropped, it runs after the drive call that armed it is over —
// possibly during another test. That is only safe while the callback touches
// nothing, so [TestEveryTickCallbackIsAMessageAndNothingElse] holds the whole
// package to it and this table is the exception list. AN ENTRY HERE IS NOT FREE:
// [harnessDriver.ticksAreMessages] has to be able to tell, at run time, that the
// surface in front of it cannot have armed the exception, and today it does that
// by refusing every tick while `farTasks` is set. A second entry needs its own
// answer to that question in the same change.
var tickCallbackCallers = map[string]string{
	"tasksLoaded": "calls the far task index the surface was handed; [harnessDriver.ticksAreMessages] refuses to overlap any tick while app.farTasks is set",
}

// TestEveryTickCallbackIsAMessageAndNothingElse reads the surface's own source
// and fails when a tea.Tick has grown a callback that does work.
func TestEveryTickCallbackIsAMessageAndNothingElse(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(f os.FileInfo) bool {
		return !strings.HasSuffix(f.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("reading the surface's source: %v", err)
	}
	seen := map[string]bool{}
	ticks := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			// The enclosing declaration is carried down so a failure can name a
			// function rather than a line number nobody can hold a table to.
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name == nil {
					continue
				}
				ast.Inspect(fn, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Tick" {
						return true
					}
					if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "tea" {
						return true
					}
					ticks++
					if len(call.Args) != 2 {
						return true
					}
					body, ok := call.Args[1].(*ast.FuncLit)
					if !ok {
						t.Errorf("%s: %s passes tea.Tick a callback that is not written out here, so nothing can say what it does on the goroutine the harness runs it on", filepath.Base(path), fn.Name.Name)
						return true
					}
					pure := true
					ast.Inspect(body.Body, func(inner ast.Node) bool {
						if _, ok := inner.(*ast.CallExpr); ok {
							pure = false
							return false
						}
						return true
					})
					if pure {
						return true
					}
					seen[fn.Name.Name] = true
					if _, known := tickCallbackCallers[fn.Name.Name]; !known {
						t.Errorf("%s: %s arms a tea.Tick whose callback CALLS SOMETHING. The harness runs a tick on its own goroutine while it goes on driving the surface, and runs a dropped one after the drive call is over — so a callback that touches anything is a race. Make it return a message and nothing else, or add %q to tickCallbackCallers in harnessdriver_test.go AND give [harnessDriver.ticksAreMessages] a way to recognise a surface that could have armed it", filepath.Base(path), fn.Name.Name, fn.Name.Name)
					}
					return true
				})
			}
		}
	}
	if ticks == 0 {
		t.Error("nothing in the package calls tea.Tick any more, so this law and [tickBudget] are dead — delete them")
	}
	for name := range tickCallbackCallers {
		if !seen[name] {
			t.Errorf("tickCallbackCallers names %s, whose tea.Tick callback no longer calls anything — remove the line, and with it whatever [harnessDriver.ticksAreMessages] refuses on its account", name)
		}
	}
}
