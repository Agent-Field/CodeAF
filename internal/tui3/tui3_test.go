package tui3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// TestMain gives the whole package a state root of its own.
//
// AN EMPTY PROFILE DIRECTORY IS THE NORMAL CASE, NOT THE ABSENT CASE, so a bare
// [newTestApp] — which names no profile, exactly like an ordinary launch — reads
// and writes aforge's state root, and on a developer's machine that is their own
// ~/.aforge. Before #315 the surface's empty-profile guards hid that: the crew
// was not read and the notices' ledger was not written, so the suite touched
// nothing. Now that both resolve the way every other persisted setting always
// has, a suite left alone with the machine's own root would answer to the
// developer's crew and retire the developer's hints — which is a suite that
// passes here and fails there, and a test run with a side effect on the person
// running it.
//
// AFORGE_HOME is the one seam that moves every path (internal/home), and HOME
// itself is deliberately left alone: this package draws `~` in front of paths
// and those readings are about the real one.
func TestMain(m *testing.M) {
	code := runTests(m)
	// EVERY DROPPED COMMAND LEAVES A GOROUTINE PARKED on a channel nobody will
	// write to, so the count at the end is the running total of what the harness
	// gave up on — the measure that took #399 from a guess to a number. It is
	// printed rather than asserted: it moves with which tests ran.
	fmt.Fprintf(os.Stderr, "tui3: %d goroutines still parked at the end of the run\n", runtime.NumGoroutine())
	os.Exit(code)
}

// runTests is TestMain's body as a function with a return value, so the
// temporary root is still removed on the way out — os.Exit runs no deferred
// call.
func runTests(m *testing.M) int {
	root, err := os.MkdirTemp("", "aforge-tui3-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui3 tests: no temporary state root: %v\n", err)
		return 1
	}
	defer os.RemoveAll(root)
	if err := os.Setenv(home.EnvVar, root); err != nil {
		fmt.Fprintf(os.Stderr, "tui3 tests: could not move %s: %v\n", home.EnvVar, err)
		return 1
	}
	// And the narrow override that would move the profile back out from under
	// it. Empty reads as unset everywhere in internal/config.
	if err := os.Setenv(config.ProfileDirEnv, ""); err != nil {
		fmt.Fprintf(os.Stderr, "tui3 tests: could not clear %s: %v\n", config.ProfileDirEnv, err)
		return 1
	}
	return m.Run()
}

// fakeAgent is the scripted session every test here runs against: it answers
// from a queue of event batches and records what it was asked to do. It is the
// reason [Agent] is an interface — the surface is driven without a provider, a
// key, or a file.
type fakeAgent struct {
	turns   [][]session.Event
	turn    int
	live    chan session.Event
	model   string
	window  int
	usage   session.Usage
	sent    []string
	stops   int
	closes  int
	packs   int
	failing error
	// past is what a resumed session already holds — what [app.replay] draws.
	past            []session.DisplayEntry
	transcriptReads int
	// earlier is what sits above its latest compaction — the region the
	// scrollback reaches through the seam — and earlierFloor is how much of
	// `past` that region replaces.
	earlier      []session.DisplayEntry
	earlierFloor int
	// weight is what the agent says the conversation weighs in tokens, which is
	// what the context meter reads (the surface no longer measures it itself).
	weight int
	// levels is the reasoning strength held per model id, the session's own map
	// as far as the surface can see it (internal/session's agent.go).
	levels map[string]string
	// marked is every sentence that went through the MARKED door — the chord
	// that means "keep this true" (standmark.go). It is kept beside `sent`
	// rather than folded into it because which door a message took is the whole
	// question those tests ask.
	marked []string
	// steered is every sentence sent INTO a running turn (steer.go), and it is
	// kept apart from `sent` for `marked`'s reason exactly: which door a message
	// took is the whole question those tests ask, and a steer that showed up in
	// `sent` would look like a second turn. steerErr is what the session answers
	// instead, and steerCh the stream ONE steer gets back — the fall-through's
	// own case, where the channel outlives the turn it was made on.
	steered  []string
	steerErr error
	steerCh  chan session.Event
}

func (f *fakeAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	f.sent = append(f.sent, text)
	if f.failing != nil {
		return nil, f.failing
	}
	if f.live != nil {
		// Steering: the in-flight turn takes the message and the caller gets a
		// closed channel, exactly as session.Agent documents.
		done := make(chan session.Event)
		close(done)
		return done, nil
	}
	out := make(chan session.Event, 64)
	if f.turn < len(f.turns) {
		for _, event := range f.turns[f.turn] {
			out <- event
		}
		f.turn++
	}
	f.live = out
	return out, nil
}

// finish closes the live turn's channel, which is what ends a real stream.
func (f *fakeAgent) finish() {
	if f.live != nil {
		close(f.live)
		f.live = nil
	}
}

func (f *fakeAgent) Interrupt()                       { f.stops++ }
func (f *fakeAgent) Compact(context.Context) error    { f.packs++; return nil }
func (f *fakeAgent) Close() error                     { f.closes++; return nil }
func (f *fakeAgent) Model() string                    { return f.model }
func (f *fakeAgent) SetModel(model string)            { f.model = model }
func (f *fakeAgent) SetContextWindow(tokens int)      { f.window = tokens }
func (f *fakeAgent) ReasoningFor(model string) string { return f.levels[model] }
func (f *fakeAgent) SetReasoningFor(model, level string) {
	if f.levels == nil {
		f.levels = map[string]string{}
	}
	if level == "" {
		delete(f.levels, model)
		return
	}
	f.levels[model] = level
}
func (f *fakeAgent) Usage() session.Usage { return f.usage }
func (f *fakeAgent) Transcript() []session.DisplayEntry {
	f.transcriptReads++
	return f.past
}

// EarlierHistory is what the journal holds ABOVE the session's latest
// compaction, and how much of `past` is the pass's rewritten copy of it. Both
// are zero for the scripted sessions that were never compacted, which is most
// of them.
func (f *fakeAgent) EarlierHistory() session.EarlierHistory {
	return session.EarlierHistory{Entries: f.earlier, Floor: f.earlierFloor}
}
func (f *fakeAgent) ContextTokens() int                   { return f.weight }
func text(kind session.EventKind, s string) session.Event { return session.Event{Kind: kind, Text: s} }

// drive runs messages through the app the way the program loop would: update,
// run whatever command came back, feed its message in, repeat until the
// surface goes quiet.
//
// Two of the surface's commands never return on their own — a wait on a stream
// that has more turn to come, and the paint clock, which reschedules itself
// for as long as a turn is running. A command is therefore given a short budget
// and dropped when it exceeds it, and the paint clock's own message is dropped
// outright: it fires every 33ms forever while working, so a harness that
// followed it would never reach the end of the queue. The tests that care about
// the clock deliver [frameMsg] themselves.
// settleLevels spends the one frame of lateness reasoninglevel.go describes: it
// puts the named models back in the queue and runs the background ask the frame
// clock would have sent, so a test can assert on a level the SURFACE was never
// the one to set.
//
// It is here beside [drive] and for [drive]'s own reason — the paint clock's
// message is dropped there, so anything sent on that clock has to be spent by
// hand — and it loops because one ask carries at most [levelBatchMax] ids.
func settleLevels(a *app, ids ...string) {
	for _, id := range ids {
		delete(a.levels, id)
		a.wantLevel(id)
	}
	for range 8 {
		cmd := a.levelKick()
		if cmd == nil {
			return
		}
		if msg, ok := cmd().(levelsMsg); ok {
			a.levelsBack(msg)
		}
	}
}

func drive(t *testing.T, a *app, msgs ...tea.Msg) {
	t.Helper()
	queue := append([]tea.Msg(nil), msgs...)
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 500 {
			t.Fatal("the surface did not settle")
		}
		msg := queue[0]
		queue = queue[1:]
		model, cmd := a.Update(msg)
		a = model.(*app)
		for _, produced := range runCmd(cmd) {
			if _, clock := produced.(frameMsg); clock {
				a.painting = false // let the next mutation ask for a tick again
				continue
			}
			queue = append(queue, produced)
		}
	}
}

// runCmd executes one command within a budget and flattens a batch.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		switch produced := msg.(type) {
		case nil:
			return nil
		case tea.BatchMsg:
			var out []tea.Msg
			for _, one := range produced {
				out = append(out, runCmd(one)...)
			}
			return out
		default:
			return []tea.Msg{produced}
		}
	case <-time.After(budgetFor(cmd)):
		return nil
	}
}

// THE LAW: A HARNESS PAYS NOTHING FOR A TICK THAT CANNOT REACH IT, AND IT KNOWS
// BY NAME WHICH OF ITS COMMANDS WILL NEVER ANSWER. The budget above was one
// number for every command, and three quarters of this package's wall clock was
// spent running it out: 2415 commands were dropped at 150ms each over a full
// run, 362s of the 478s the package took, and each drop left one goroutine
// blocked on a channel nobody would ever write to (806 of the 886 alive at the
// end). CI's per-binary ceiling is eight minutes and the package reached it
// (#399).
//
// WHAT WAS BELIEVED, AND WHAT IS TRUE. The reading that opened #399 was that the
// fakes never feed the surface's wait channels, so a waiter could be answered
// with nil the moment it was recognised, spawning nothing. That is false, and
// measurably so: over one subset of this package [waitEvent] returned a real
// event on 28 of 118 calls and bubbletea's tick fired on 451 of 593. Dropping
// the recognised names outright fails better than thirty tests. These commands
// DO answer; the fakes hand back a buffered channel and the events in it are
// what the streaming tests assert on.
//
// SO THE WAITERS KEEP THE FULL [cmdBudget], and this file deliberately holds no
// knob to shorten it. When a waiter answers it answers from a channel that
// already holds the value, in microseconds — but "in microseconds" is a claim
// about the scheduler, not about the code, and this suite runs beside others on
// a loaded machine. A budget tuned to that claim turns a delivered event into a
// drop the day the box is busy, which is a load-shaped red on dev for nobody's
// change: the exact failure #399 exists to remove. The waiters are named here so
// that the seam which will remove the guessing — fakes that own and close their
// own channels, so a waiter ENDS rather than parks — has one place to work from,
// and so that a new waiter cannot be added without meeting this note.
//
// WHAT IS CHEAPENED IS THE TICK, where the question needs no scheduler at all. A
// tick is a real timer, so the surface's own constants decide it: the shortest
// are the paint clock and [resizeGrace] at 80ms and taskmention's at 100ms, and
// every tick at 150ms or longer — the polls, [homeEvery], [farRoomEvery],
// [hostPingEvery] and their kind — is dropped today and would be dropped whatever
// the harness did. [tickBudget] sits above the longest tick that still delivers
// and below the shortest that never does, so it takes back the wait on the polls
// and changes nothing that arrives.
const (
	// cmdBudget is what every command gets that is not a tick, the waiters
	// included: unchanged, so nothing real is dropped faster than it was before.
	cmdBudget = 150 * time.Millisecond
	// tickBudget is what bubbletea's tick gets. It clears the 100ms tick — the
	// longest one this package has that fires — with room for a loaded machine,
	// and stops short of the 150ms debounce, which the old budget already raced.
	tickBudget = 120 * time.Millisecond
)

// teaTickSymbol is bubbletea's tick closure, as the runtime spells it. It is
// pinned rather than pattern-matched, and [TestTheHarnessKnowsEveryCommandThatCannotAnswer]
// builds a real tick and fails if an upgrade moves the symbol.
const teaTickSymbol = "charm.land/bubbletea/v2.Tick.func1"

// blockingCommands is THE ONE TABLE. It names every command in this package that
// parks on a channel a test's fakes usually never write to and never close.
//
// IT DOES NOT PRICE ANYTHING — see the note above [cmdBudget] for why shortening
// a waiter's budget is a bet on the scheduler. It is the enumeration the fix
// after this one works from: when the fakes own and close their channels, these
// are the commands that stop parking, and this list is how that change knows it
// has covered them all. [TestTheHarnessKnowsEveryCommandThatCannotAnswer] holds
// it to the surface's own source so it cannot rot in the meantime.
var blockingCommands = []string{
	"pumpShaping",
	"waitDesign",
	"waitEvent",
	"waitPilot",
	"waitRoom",
	"waitRun",
	"waitSteerLane",
	"waitStir",
	"waitTask",
	"waitWake",
	"watchDesigns",
	"watchDriving",
	"watchFollowing",
	"watchRuns",
	"watchTasks",
	"watchWakes",
}

// budgetFor prices one command. Only the tick is priced apart, by the runtime
// symbol behind the closure.
func budgetFor(cmd tea.Cmd) time.Duration {
	if cmdSymbol(cmd) == teaTickSymbol {
		return tickBudget
	}
	return cmdBudget
}

// cmdSymbol is the fully qualified name of the function behind a command.
func cmdSymbol(cmd tea.Cmd) string {
	fn := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer())
	if fn == nil {
		return ""
	}
	return fn.Name()
}

// TestTheHarnessKnowsEveryCommandThatCannotAnswer reads the surface's own source
// and fails when it has grown a waiting command [blockingCommands] does not
// name. THE TABLE CANNOT BE KEPT BY HAND — it is the enumeration the fakes seam
// will work from, and a waiter missing from it is a waiter that seam will leave
// parking, which is how #399 grew to eight minutes without anyone adding a slow
// test.
//
// The family is named by shape: a function whose name begins wait, pump or watch
// and whose one result is a tea.Cmd. That is what every command in this package
// that parks on a channel is called, and the naming is worth keeping for that
// reason alone.
func TestTheHarnessKnowsEveryCommandThatCannotAnswer(t *testing.T) {
	// The tick symbol is a string in a table and bubbletea is a dependency that
	// moves, so it is checked against a tick this test builds itself.
	built := cmdSymbol(tea.Tick(time.Hour, func(time.Time) tea.Msg { return nil }))
	if built != teaTickSymbol {
		t.Fatalf("teaTickSymbol is %q but bubbletea's tick is now %q — every tick in the package is being billed the full %s", teaTickSymbol, built, cmdBudget)
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(f os.FileInfo) bool {
		return !strings.HasSuffix(f.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("reading the surface's source: %v", err)
	}
	known := map[string]bool{}
	for _, name := range blockingCommands {
		known[name] = true
	}
	found := map[string]bool{}
	ticks := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Tick" {
						if id, ok := sel.X.(*ast.Ident); ok && id.Name == "tea" {
							ticks++
						}
					}
					return true
				}
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Name == nil || !waitingName(fn.Name.Name) || !returnsOneCmd(fn) {
					return true
				}
				found[fn.Name.Name] = true
				if !known[fn.Name.Name] {
					t.Errorf("%s declares %s, which returns a tea.Cmd that waits, and blockingCommands does not name it — add %q to blockingCommands in tui3_test.go, or the seam that is to replace the budget cannot bill it correctly and it goes on parking a goroutine per call", filepath.Base(path), fn.Name.Name, fn.Name.Name)
				}
				return true
			})
		}
	}
	if ticks == 0 {
		t.Errorf("nothing in the package calls tea.Tick any more, so teaTickSymbol and tickBudget are dead — delete them")
	}
	var stale []string
	for _, name := range blockingCommands {
		if !found[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("blockingCommands names %s, which the surface no longer declares — remove the line", strings.Join(stale, ", "))
	}
}

// waitingName is the shape of a command that parks: wait, pump or watch, then a
// capital.
func waitingName(name string) bool {
	for _, prefix := range []string{"wait", "pump", "watch"} {
		rest, cut := strings.CutPrefix(name, prefix)
		if cut && rest != "" && rest[0] >= 'A' && rest[0] <= 'Z' {
			return true
		}
	}
	return false
}

// returnsOneCmd reports whether a declaration's only result is a tea.Cmd, which
// is what tells a waiting command apart from a predicate like app.waiting.
func returnsOneCmd(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	sel, ok := fn.Type.Results.List[0].Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Cmd" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "tea"
}

// newTestApp pins the colour profile AND the glyph tier. A test inherits
// whatever TERM and LANG the machine running it has, and a surface that
// rendered unpainted under NO_COLOR — or ASCII under LANG=C — would make every
// assertion about an escape sequence or a rail marker a test of the
// environment.
// labLedger is THE ONE LEDGER EVERY TEST APP SPENDS INTO, made once per test
// binary under the OS temp dir. The door hands the surface a path and an empty
// path means "this machine's" (usage_ledger.go's [session.UsageCache] falls back
// to [session.UsageLedgerPath]), so a test app that left it empty had the spend
// place reading the developer's real ~/.aforge — and went red the first time
// anything on the machine cost a cent, which on a box running several lanes is
// always. It is one file rather than one per test because [newTestApp] has no
// *testing.T to ask for a TempDir, and nothing here reads what another test
// wrote: the file exists only so that the fallback is never taken.
var (
	labLedgerOnce sync.Once
	labLedgerPath string
)

func labLedger() string {
	labLedgerOnce.Do(func() {
		dir, err := os.MkdirTemp("", "aforge-tui3-lab-")
		if err != nil {
			panic(err)
		}
		labLedgerPath = filepath.Join(dir, session.UsageLedgerName)
	})
	return labLedgerPath
}

func newTestApp(agent Agent) *app {
	a := newApp(context.Background(), Options{
		Agent: agent, Workspace: "/tmp/lab", UsageLedger: labLedger(),
		BashBackgroundAfterSeconds: config.DefaultBashBackgroundAfter,
		// AND IT PINS THE TERMINAL, which is the second and third pin in one
		// table, for exactly the reason the palette is pinned below: [newApp]
		// reads the environment to decide whether a clipboard write needs the
		// multiplexer's passthrough wrapper (copymode.go), how often the frame
		// clock turns over a link (link.go), and whether a path may be written
		// as an OSC 8 link at all (pathlink.go) — so a suite run inside tmux got
		// the wrapped yank, a suite run over ssh stepped every animation three
		// slots at a time, and a suite run on a CI runner with no TERM wrote no
		// links and failed every assertion that looked for one. Each of those
		// decisions has a table test of its own that states both answers; this
		// table is one ordinary terminal, at the machine, outside a multiplexer.
		Env: envOf(map[string]string{"TERM": "xterm-256color"}),
	})
	a.width, a.height = 60, 20
	a.pal = newPalette(tokens.ANSI256, false)
	// AND IT PINS THE TASK COLUMN, for the fourth time for the same reason.
	// [newApp] reads the profile to decide whether the column stands (task.go's
	// ui.task_column), so a developer who pressed ctrl+g in their own aforge would
	// run a suite with no rail in it — and every rail test would fail on their
	// machine and nowhere else. The posture has tests of its own that set the
	// profile directory they read from.
	a.railAway = false
	// AND IT PINS THE CHORD SPELLING, for the fifth time for the same reason.
	// [newApp] reads GOOS and the environment to decide whether a chord is CALLED
	// `alt+1` or `⌥1` (chords.go), so every hint assertion in this suite would
	// read one way on a Mac and another way on Linux. The spelling has a table
	// test of its own that states both, and [TestEveryPlaceSpellsItsChordsTheWayThisTerminalDoes]
	// asserts the Mac reading against every place on purpose.
	a.chords = chordSpelling{meta: chordAltWord}
	// AND IT PINS QUICK SWITCHING OFF, for the sixth time for the same reason.
	// [newApp] reads the profile to decide whether the switcher's chord switches
	// on the press or opens a card that waits (hop.go's ui.quick_switch), so a
	// developer who turned it off in their own aforge would run a different
	// suite. The quick behaviour has tests of its own that turn it on outright.
	a.hopQuick = false
	// AND IT PINS THE WORK SEAT'S QUESTION AS ALREADY ASKED, for the seventh
	// time for the reason the six pins above exist. The one line about a crew
	// older than the work seat is read from the PROFILE (crew.go's
	// [app.workSeat]), and a bare app has no profile of its own — so a suite run
	// on a machine whose own crew predates the worker row would grow a note in
	// every test that starts a node, and one run on a machine whose crew does
	// not would grow none. A test that means the line builds a profile and asks
	// for it outright (crewseat_test.go's crewSeatLab).
	a.workSeatSaid = true
	// AND IT PINS THE NOTICES TO THIS SESSION, for the eighth time for the same
	// reason. The ledger is a file in the state root now that an empty profile
	// directory resolves there like every other persisted thing (#315), and this
	// package's root is one temporary directory for the whole run (TestMain) —
	// so a bare app that showed a hint would count a showing every other bare
	// app's assertions then read, and the suite's answers would depend on the
	// order the tests ran in. A test that means the ledger names a path of its
	// own (notice_test.go's noticeApp).
	a.notices.path = ""
	a.entries = nil // drop the opening hint so tests read their own entries
	// The welcome box opens on an empty conversation, which every test here is
	// (welcome.go). It has its own tests; the ones that predate it read the
	// frame it used to have, so it is dismissed exactly as a first keystroke
	// dismisses it.
	a.welcome = welcome{spent: true}
	a.touch()
	return a
}

func key(s string) tea.KeyPressMsg {
	if len([]rune(s)) == 1 {
		return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
	}
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+,":
		return tea.KeyPressMsg{Code: ',', Mod: tea.ModCtrl}
	case "ctrl+o":
		return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "ctrl+w":
		return tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}
	case "ctrl+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	case "ctrl+e":
		return tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		// THE TWO ENDS OF A LIST, spelled out for the same reason as the two page
		// keys below: the tasks place binds both to its cursor, and without a
		// case here a test that "pressed end" pressed the zero key — which is how
		// a strip that survived them went unnoticed.
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		// THE TWO PAGE KEYS, spelled out for this switch's own stated reason: they
		// are not single runes, so without a case here they fell through to the
		// zero key and every test that "pressed pgdown" pressed nothing at all.
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case standMarkKey:
		// The marked send (standmark.go). It is spelled out here because the
		// fall-through below only builds single-rune chords, and a chord that
		// silently became the zero key would be a test pressing nothing.
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
	case bargeKey:
		// The barge-in (bargein.go), spelled out for the same reason as the chord
		// directly above it — and carrying NO Text, which is how a real terminal
		// sends it: ultraviolet gives KeyEnter the CR rune, which is not
		// printable, so its decoder leaves the text empty however the shift
		// modifier is set. A helper that invented text here would hide the one
		// thing that makes falling through this chord safe.
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}
	case "alt+backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
	case "ctrl+backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModCtrl}
	case "super+backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper}
	}
	// EVERY OTHER ctrl CHORD, spelled the way the surface spells it. Without this
	// an unlisted chord fell through to the zero key, which is a message no
	// handler claims — so the test pressed nothing at all and asserted against
	// what did not happen. A silent no-op is the one thing a key helper must not
	// be able to do.
	if chord, ok := strings.CutPrefix(s, "ctrl+"); ok && len([]rune(chord)) == 1 {
		return tea.KeyPressMsg{Code: []rune(chord)[0], Mod: tea.ModCtrl}
	}
	// AND EVERY alt CHORD, WITH NO TEXT ON IT. That is not a shortcut: a modified
	// key carries no text through ultraviolet's decoder, which clears it on the
	// esc-prefix path explicitly — and it is exactly what makes the router's
	// alt+digit and alt+letter classes safe to add under a page whose rule is
	// "every printable key is the filter" (placekeys.go). A helper that invented
	// text here would hide the one property the design depends on.
	if chord, ok := strings.CutPrefix(s, "alt+"); ok && len([]rune(chord)) == 1 {
		return tea.KeyPressMsg{Code: []rune(chord)[0], Mod: tea.ModAlt}
	}
	switch s {
	case "alt+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "ctrl+tab":
		// The switcher's alias, which a terminal that disambiguates can send
		// (hop.go). It is spelled out here because `tab` is not a rune and the
		// ctrl fall-through above only builds single-rune chords.
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModCtrl}
	case "ctrl+shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModCtrl | tea.ModShift}
	case "ctrl+shift+k":
		// The switcher's reverse (hop.go), spelled out for the reason above it:
		// the ctrl fall-through builds single-rune chords with one modifier, and
		// this one carries two.
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl | tea.ModShift}
	case "shift+left":
		return tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift}
	case "shift+right":
		return tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift}
	case "shift+up":
		return tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}
	case "shift+down":
		return tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}
	}
	return tea.KeyPressMsg{}
}

func typeLine(t *testing.T, a *app, line string) {
	t.Helper()
	for _, r := range line {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("enter"))
}

func frame(a *app) string {
	f, _, _ := a.frame()
	return f
}

// plain is the frame as a reader sees it: colour is asserted where colour is
// the subject, and nowhere else.
func plain(s string) string { return ansi.Strip(s) }

// rows lays the conversation out at the test width.
func rows(a *app) []row { return a.visible(a.width) }

func plainRows(a *app) []string {
	out := make([]string, 0, len(a.rows))
	for _, r := range rows(a) {
		out = append(out, plain(r.text))
	}
	return out
}

// unindented drops THE INDENT LAW's two-column gutter (render.go): flush-left
// is what was said to the person, two columns in is what was done on their
// behalf. A test about spacing or about the rail's own markers wants the line's
// shape, not the hierarchy the gutter carries, so it strips it first.
func unindented(r string) string { return strings.TrimPrefix(r, "  ") }

// clickHit drives a left click on the first VISIBLE row of a kind. Visible is
// the point: a click carries a screen row, and a transcript taller than the
// window has a screen row that is not its row-list index.
func clickHit(t *testing.T, a *app, want hitKind) {
	t.Helper()
	body, _ := a.window(a.width, a.viewHeight())
	for i, r := range body {
		if r.hit == want {
			drive(t, a, tea.MouseClickMsg{Y: a.bodyTop() + i, Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{Y: a.bodyTop() + i, Button: tea.MouseLeft})
			return
		}
	}
	t.Fatalf("no visible row answers to %d:\n%s", want, strings.Join(plainRows(a), "\n"))
}

// runTurn types a line, closes the stream and drains it.
func runTurn(t *testing.T, a *app, agent *fakeAgent, line string) {
	t.Helper()
	typeLine(t, a, line)
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// toolBegin is a call announced with a hint and NO payload — the degraded
// shape, which every line still has to render: the target falls back to the
// hint's gloss.
func toolBegin(tool, hint string) session.Event {
	return session.Event{Kind: session.EventToolBegin, Tool: tool, Hint: hint}
}

// toolEnd carries the result in Output, where session puts it. A successful
// call's Hint is empty on the wire (loop.go: the result belongs to the model),
// so a test that put its result there would be testing a shape that never
// arrives.
func toolEnd(tool, output string) session.Event {
	return session.Event{Kind: session.EventToolEnd, Tool: tool, Output: output}
}

func TestATurnStreamsTextToolsAndSettles(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini", turns: [][]session.Event{{
		toolBegin("read", "foo/bar.go"),
		toolEnd("read", ""),
		text(session.EventTextDelta, "it "),
		text(session.EventTextDelta, "parses."),
		{Kind: session.EventTurnDone, Usage: session.Usage{CostUSD: 0.012}},
	}}}
	a := newTestApp(agent)

	runTurn(t, a, agent, "what does bar.go do?")

	// THE WORKFOLD (workfold.go): a turn that settles on a real answer collapses
	// the machinery it took to get there into one chip, so the page reads back
	// as the exchange it was — the question, and the sentence it was answered
	// with. The read call is not gone, it is behind the key the chip names.
	got := plain(frame(a))
	for _, want := range []string{"› what does bar.go do?", "▸ worked", "1 tool call · ctrl+e", "it parses.", "idle"} {
		if !strings.Contains(got, want) {
			t.Fatalf("frame is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "read foo/bar.go") {
		t.Fatalf("a settled turn still shows its machinery:\n%s", got)
	}
	// NO SUCCESS GLYPH, EVER (D11). A quiet line is a success.
	if strings.Contains(got, "✓") {
		t.Fatalf("a settled call drew a success glyph:\n%s", got)
	}
	// And ctrl+e puts it back, drawn on the rail and indented under the chip.
	drive(t, a, key("ctrl+e"))
	if opened := plain(frame(a)); !strings.Contains(opened, "  ╰─▶ read foo/bar.go") {
		t.Fatalf("ctrl+e did not open the turn's work:\n%s", opened)
	}
	if agent.sent[0] != "what does bar.go do?" {
		t.Fatalf("submitted %q", agent.sent[0])
	}
	if !strings.Contains(got, "$0.01") {
		t.Fatalf("cost is missing from the status line:\n%s", got)
	}
}

func TestAFailedToolIsMarkedAndSaysWhy(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("bash", "go build ./..."),
		{Kind: session.EventToolFailed, Tool: "bash", Err: errors.New("exit 2")},
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "build it")

	got := plain(frame(a))
	if !strings.Contains(got, "✗") || !strings.Contains(got, "exit 2") {
		t.Fatalf("a failed tool has to say so:\n%s", got)
	}
}

func TestCompactionDrawsADivider(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventCompacted, Hint: "compacted from ~84k tokens"},
		text(session.EventTextDelta, "carrying on"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "keep going")

	// The compaction mark is machinery, so a turn that settles on an answer
	// tucks it away with the rest of the work (workfold.go). Nobody wants to be
	// told the context was squeezed while they are reading the reply; they want
	// it when they go looking for why, which is what ctrl+e is for.
	if folded := plain(frame(a)); strings.Contains(folded, "compacted from") {
		t.Fatalf("a settled turn still shows the compaction mark:\n%s", folded)
	}
	drive(t, a, key("ctrl+e"))
	got := plain(frame(a))
	if !strings.Contains(got, "⚭ compacted from ~84k tokens") || !strings.Contains(got, "──") {
		t.Fatalf("the compaction divider is missing:\n%s", got)
	}
}

// THE SPACING LAW (D11). One blank before a user message, one before a cluster
// that follows text, one after a cluster before text, zero everywhere else —
// and never two in a row.
//
// The fixture walks every rule in one transcript: user → cluster (no blank, the
// calls ARE the reply starting) → text (one blank) → cluster (one blank) →
// text (one blank) → user (one blank).
//
// It is read with the work OPEN, because that is the only state in which the
// law has anything to govern: folded, a settled turn is a chip and an answer
// (workfold.go), and the rules about what sits either side of a cluster would
// never be exercised. ui.work=open is a real setting a person can choose, so
// this is the surface they get, not a test-only rig.
func TestSpacingLaw(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("read", "foo/bar.go"),
		toolEnd("read", "ok"),
		toolBegin("grep", "func main"),
		toolEnd("grep", "main.go:3: func main()"),
		text(session.EventTextDelta, "bar.go parses the config.\n"),
		toolBegin("read", "foo/baz.go"),
		toolEnd("read", "ok"),
		text(session.EventTextDelta, "and baz.go writes it back."),
		{Kind: session.EventTurnDone},
	}, {
		text(session.EventTextDelta, "nothing else uses it."),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "what does bar.go do?")
	runTurn(t, a, agent, "and then?")
	// The posture is set AFTER the turns because [app.settle] re-reads it from
	// the profile at every turn end, so a value planted beforehand is gone by
	// the time there is a transcript to measure.
	a.workMode = config.WorkOpen
	a.touch()

	list := plainRows(a)
	if len(list) == 0 {
		t.Fatal("nothing was laid out")
	}
	shape := make([]string, 0, len(list))
	for _, r := range list {
		bare := unindented(r)
		switch {
		case strings.TrimSpace(r) == "":
			shape = append(shape, "_")
		case strings.HasPrefix(bare, "›"):
			shape = append(shape, "u")
		case strings.HasPrefix(bare, "▸ worked"):
			shape = append(shape, "w")
		case strings.HasPrefix(bare, "├─▶") || strings.HasPrefix(bare, "╰─▶"):
			shape = append(shape, "t")
		default:
			shape = append(shape, "x")
		}
	}
	got := strings.Join(collapse(shape), "")
	// _ u _ w t _ x _ t _ x _ u _ x — one row of air where the conversation
	// begins, a blank before each user message, the CHANGE-OF-SPEAKER blank
	// after each one (render.go's wasUser: the reply is a different voice and
	// does not open wedged under the question), one on each side of a cluster
	// that sits between two blocks of text, and nowhere else. The chip still
	// rides at the top of the work it stands for.
	if want := "_u_wt_x_t_x_u_x"; got != want {
		t.Fatalf("layout shape is %q, want %q:\n%s", got, want, strings.Join(list, "\n"))
	}
	for i, r := range list {
		if strings.TrimSpace(r) != "" {
			continue
		}
		// Row zero is the conversation's one deliberate opening breath
		// (render.go's [app.layout]); a blank may still not CLOSE the page.
		if i+1 >= len(list) {
			t.Fatalf("a blank row closes the transcript:\n%s", strings.Join(list, "\n"))
		}
		if i > 0 && strings.TrimSpace(list[i-1]) == "" {
			t.Fatalf("two blank rows in a row at %d:\n%s", i, strings.Join(list, "\n"))
		}
	}
}

// collapse squashes runs of the same symbol, so the shape says "text, blank,
// cluster" rather than how many rows each of them wrapped to.
func collapse(shape []string) []string {
	out := make([]string, 0, len(shape))
	for i, s := range shape {
		if i > 0 && shape[i-1] == s {
			continue
		}
		out = append(out, s)
	}
	return out
}

// A cluster that is the WHOLE turn still opens under the change-of-speaker
// blank: the person said "build it", and the surface answering with a call is
// a different voice, so exactly one row of silence sits between the message
// and the first tool line (render.go's wasUser). A turn with no trailing
// answer never folds (workfold.go), so the call is on the page to be measured.
func TestAClusterThatIsTheWholeTurnTakesTheSpeakerBlankAboveIt(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("bash", "go build ./..."),
		toolEnd("bash", "ok"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "build it")

	list := plainRows(a)
	for i, r := range list {
		if !strings.HasPrefix(unindented(r), "╰─▶") {
			continue
		}
		if i < 2 || strings.TrimSpace(list[i-1]) != "" || strings.TrimSpace(list[i-2]) == "" {
			t.Fatalf("the call does not sit one blank under the person's message:\n%s",
				strings.Join(list, "\n"))
		}
		return
	}
	t.Fatalf("no tool line was drawn:\n%s", strings.Join(list, "\n"))
}

// The tool cluster is a block, not a list with air in it.
func TestToolLinesAreOneUnbrokenCluster(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("read", "a.go"), toolEnd("read", "ok"),
		toolBegin("read", "b.go"), toolEnd("read", "ok"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "read both")

	list := plainRows(a)
	first, last := -1, -1
	for i, r := range list {
		if bare := unindented(r); strings.HasPrefix(bare, "├─▶") || strings.HasPrefix(bare, "╰─▶") {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		t.Fatalf("no tool lines were drawn:\n%s", strings.Join(list, "\n"))
	}
	for i := first; i <= last; i++ {
		if strings.TrimSpace(list[i]) == "" {
			t.Fatalf("a blank landed inside the tool cluster:\n%s", strings.Join(list, "\n"))
		}
		// THE INDENT LAW: a call is work done on the person's behalf, so it sits
		// two columns in and never flush with what was said to them.
		if !strings.HasPrefix(list[i], "  ") {
			t.Fatalf("a tool line lost its gutter:\n%s", strings.Join(list, "\n"))
		}
	}
	// The rail closes on the last call and tees on every one above it.
	if !strings.HasPrefix(unindented(list[first]), "├─▶") || !strings.HasPrefix(unindented(list[last]), "╰─▶") {
		t.Fatalf("the cluster's markers are not ├─▶ … ╰─▶:\n%s", strings.Join(list, "\n"))
	}
}

// Past three calls the older ones fold into one line under their caption, and
// ctrl+o opens them.
func TestTheClusterFoldsPastThreeCalls(t *testing.T) {
	var events []session.Event
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		events = append(events, toolBegin("read", name), toolEnd("read", "ok"))
	}
	events = append(events, session.Event{Kind: session.EventTurnDone})
	agent := &fakeAgent{model: "m", turns: [][]session.Event{events}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "read them all")

	// One parallel step: the five reads share a caption. Sequential clocks from
	// the harness would otherwise mint five captions of one call each.
	base := time.Unix(100, 0)
	for i := range a.entries {
		if a.entries[i].kind != entryTool {
			continue
		}
		a.entries[i].began = base
		a.entries[i].ended = base.Add(time.Second)
	}

	list := plainRows(a)
	page := strings.Join(list, "\n")
	if !strings.Contains(page, "reading 5 files") {
		t.Fatalf("the caption is missing:\n%s", page)
	}
	if strings.Contains(page, "earlier tool calls") {
		t.Fatalf("the old fold line survived under a caption:\n%s", page)
	}
	if n := countTools(a); n != toolWindow {
		t.Fatalf("%d tool lines are visible, want %d:\n%s", n, toolWindow, page)
	}
	if strings.Contains(strings.Join(list, "\n"), "a.go") {
		t.Fatalf("a folded call is still on screen:\n%s", strings.Join(list, "\n"))
	}

	drive(t, a, key("ctrl+o"))
	list = plainRows(a)
	if n := countTools(a); n != 5 {
		t.Fatalf("ctrl+o showed %d calls, want 5:\n%s", n, strings.Join(list, "\n"))
	}
	if strings.Contains(strings.Join(list, "\n"), "earlier tool calls") {
		t.Fatalf("the fold line survived ctrl+o:\n%s", strings.Join(list, "\n"))
	}

	drive(t, a, key("ctrl+o"))
	if n := countTools(a); n != toolWindow {
		t.Fatalf("ctrl+o did not fold back: %d calls visible", n)
	}
}

// countTools counts the distinct tool entries on the row list.
func countTools(a *app) int {
	seen, n := -1, 0
	for _, r := range rows(a) {
		if r.hit == hitTool && r.entry != seen {
			seen = r.entry
			n++
		}
	}
	return n
}

// Opening one call shows what came back, under the rail of the line it belongs
// to, bounded by that tool's window.
func TestOpeningOneCallShowsItsResultUnderTheRail(t *testing.T) {
	long := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		long = append(long, "src/f"+strconv.Itoa(i)+".go:"+strconv.Itoa(i+1)+": func main()")
	}
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("grep", "func main ./..."),
		toolEnd("grep", strings.Join(long, "\n")),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "find main")

	call := -1
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			call = i
		}
	}
	if call < 0 {
		t.Fatal("no tool entry")
	}

	// ↑ selects the call, enter opens it — the keyboard road.
	drive(t, a, key("up"))
	if a.sel != call {
		t.Fatalf("↑ selected %d, want %d", a.sel, call)
	}
	drive(t, a, key("enter"))
	if !a.entries[call].open {
		t.Fatal("enter did not open the selected call")
	}

	body := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"│ src/f0.go:1: func main()", "… 10 more lines"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the open call is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "src/f30.go") {
		t.Fatalf("the expansion window is not bounded:\n%s", body)
	}

	// The "… N more lines" foot is its own click target, and it lifts the cap
	// rather than closing the call.
	clickHit(t, a, hitMore)
	body = strings.Join(plainRows(a), "\n")
	if !a.entries[call].open || !strings.Contains(body, "src/f39.go") {
		t.Fatalf("the more-line did not lift the cap:\n%s", body)
	}

	drive(t, a, key("enter"))
	if a.entries[call].open {
		t.Fatal("enter did not close the call again")
	}
	if strings.Contains(strings.Join(plainRows(a), "\n"), "src/f0.go") {
		t.Fatal("the expansion survived the close")
	}
}

// A call that has not come back says so rather than showing an empty output.
func TestAnOpenCallSaysItIsRunning(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("bash", "go test ./..."),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "run the tests")

	call := len(a.entries) - 1
	a.openTool(call)
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "running") {
		t.Fatalf("a running call has to say so:\n%s", body)
	}
	// And it says it under the rail, like every other expansion.
	if !strings.Contains(body, "│ running") {
		t.Fatalf("the running line is not on the rail:\n%s", body)
	}
}

// A click lands on the row under the pointer, through the same mapping the
// frame draws with.
func TestAClickOpensTheCallUnderIt(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("read", "foo/bar.go"),
		toolEnd("read", "42 lines"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "read it")

	at := -1
	for i, r := range rows(a) {
		if r.hit == hitTool {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatal("no clickable tool row")
	}

	y := a.bodyTop() + at

	drive(t, a, tea.MouseClickMsg{Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{Y: y, Button: tea.MouseLeft})
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "42 lines") {
		t.Fatalf("the click did not open the call under it (y=%d):\n%s", y, body)
	}

	drive(t, a, tea.MouseClickMsg{Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{Y: y, Button: tea.MouseLeft})
	if strings.Contains(strings.Join(plainRows(a), "\n"), "│ 42 lines") {
		t.Fatal("a second click did not close the call")
	}
}

// THE DISTINCTION. The person's words carry the glyph and the accent HUE; the
// model's carry neither, and weight belongs to markdown on both sides
// (render.go's entryUser, and thinking_test's hue tests).
func TestUserAndAssistantReadDifferently(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "it parses the config file and writes it back out again"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "what does it do, in a sentence long enough to wrap across the width")

	var user, assistant []row
	for _, r := range rows(a) {
		if r.entry < 0 {
			continue
		}
		switch a.entries[r.entry].kind {
		case entryUser:
			user = append(user, r)
		case entryAssistant:
			assistant = append(assistant, r)
		}
	}
	if len(user) < 2 || len(assistant) == 0 {
		t.Fatalf("want a wrapped user message and a reply, got %d and %d rows", len(user), len(assistant))
	}
	if !strings.HasPrefix(plain(user[0].text), glyphYou) {
		t.Fatalf("the user's first row has no glyph: %q", plain(user[0].text))
	}
	if strings.HasPrefix(plain(user[1].text), glyphYou) {
		t.Fatalf("a continuation row repeated the glyph: %q", plain(user[1].text))
	}
	if !strings.HasPrefix(plain(user[1].text), "  ") {
		t.Fatalf("a continuation row is not aligned under the text: %q", plain(user[1].text))
	}
	accent := a.pal.accent("x")
	accent = accent[:strings.Index(accent, "x")]
	// THE ACCENT IS SPENT ON THE GLYPH AND NOWHERE IN THE WORDS. The `›` is the
	// identity mark; the sentence behind it is ordinary ink, so a long question
	// no longer outshines the answer it is a question about (render.go's user
	// entry states the law).
	if !strings.Contains(user[0].text, accent) {
		t.Fatalf("the user's glyph row carries no accent: %q", user[0].text)
	}
	for i, r := range user[1:] {
		if strings.Contains(r.text, accent) {
			t.Fatalf("user continuation row %d wears the accent — the glyph is the mark, the words are ink: %q", i+1, r.text)
		}
	}
	for i, r := range user {
		if strings.Contains(r.text, "\x1b[1m") {
			t.Fatalf("user row %d is bold: weight belongs to markdown: %q", i, r.text)
		}
	}
	for i, r := range assistant {
		if strings.Contains(r.text, accent) {
			t.Fatalf("assistant row %d wears the person's hue: %q", i, r.text)
		}
		if strings.Contains(r.text, "\x1b[1m") {
			t.Fatalf("assistant row %d is bold: %q", i, r.text)
		}
		if strings.HasPrefix(plain(r.text), glyphYou) || strings.HasPrefix(plain(r.text), "›") {
			t.Fatalf("assistant row %d wears a glyph: %q", i, plain(r.text))
		}
	}
}

// SNAPPINESS. A burst of deltas inside one frame interval builds one frame.
func TestDeltasCoalesceIntoOneFrame(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.state = stateWorking

	frame(a)
	built := a.builds
	if a.dirty {
		t.Fatal("a frame that was just built is still dirty")
	}

	a.say("one ")
	a.say("two ")
	a.say("three")
	if a.dirty {
		t.Fatal("a streamed delta dirtied the frame — deltas paint on the clock, not on arrival")
	}
	frame(a)
	frame(a)
	if a.builds != built {
		t.Fatalf("three deltas built %d frames, want 0", a.builds-built)
	}
	if strings.Contains(plain(frame(a)), "three") {
		t.Fatal("a delta reached the screen without a frame tick")
	}

	// One tick of the clock promotes all three at once.
	a.paint()
	if !a.dirty {
		t.Fatal("the clock did not mark the frame dirty")
	}
	if got := plain(frame(a)); !strings.Contains(got, "one two three") {
		t.Fatalf("the frame did not catch up:\n%s", got)
	}
	if a.builds != built+1 {
		t.Fatalf("one tick built %d frames, want 1", a.builds-built)
	}
}

// A settled entry is not re-rendered because the screen was asked for again.
func TestSettledEntriesAreNotReRendered(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "a settled paragraph"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "say something")

	frame(a)
	at := len(a.entries) - 1
	if !a.entries[at].built || len(a.entries[at].rows) == 0 {
		t.Fatal("the reply was never rendered")
	}
	// Row identity, not row equality: a re-render would hand back a new
	// backing array even when it produced the same characters.
	before := &a.entries[at].rows[0]
	a.touch()
	frame(a)
	if after := &a.entries[at].rows[0]; before != after {
		t.Fatal("a settled entry re-rendered on a frame that did not change it")
	}

	a.width = 40
	a.touch()
	frame(a)
	if a.entries[at].width != 40 {
		t.Fatal("a width change did not re-render the entry")
	}
}

// The markdown swap: plain while the words are still arriving, rendered once
// the turn is done.
//
// The streaming rows are plain in the sense that matters here — no markdown has
// been applied to them, so a heading is still a hash and a bold run is still a
// pair of asterisks — but they are not unpainted: the growing edge wears the
// live tier until the turn settles (styles.go's [hueLive]). The want asks
// [app.liveTail] for those rows rather than spelling the paint out, so this
// stays a test of the SWAP and settle_test.go stays the test of the ink.
func TestMarkdownArrivesOnSettle(t *testing.T) {
	body := "# Title\n\nsome **words** about it"
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, body),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "write me something")

	at := a.live
	if at < 0 {
		t.Fatal("nothing is live")
	}
	if a.entries[at].settled {
		t.Fatal("a streaming reply is already settled")
	}
	if got, want := a.entryRows(a.conversation(), at, a.width), trimBlanks(a.liveTail(body, a.width)); !sameRows(got, want) {
		t.Fatalf("a streaming reply is not plain:\n%#v\n%#v", got, want)
	}
	// And it really is unrendered: the hash and the asterisks are still there.
	for _, want := range []string{"# Title", "**words**"} {
		if !strings.Contains(plain(strings.Join(a.entries[at].rows, "\n")), want) {
			t.Fatalf("a streaming reply lost %q to the renderer", want)
		}
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTurnDone}})
	if !a.entries[at].settled {
		t.Fatal("the reply did not settle")
	}
	if got, want := a.entryRows(a.conversation(), at, a.width), trimBlanks(renderMarkdownWith(a.styler(), body, a.width)); !sameRows(got, want) {
		t.Fatalf("a settled reply is not rendered markdown:\n%#v\n%#v", got, want)
	}
}

func sameRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSlashCommandsAreConsumedLocally(t *testing.T) {
	agent := &fakeAgent{model: "start/model"}
	a := newTestApp(agent)

	typeLine(t, a, "/model openai/gpt-4.1-mini")
	if agent.model != "openai/gpt-4.1-mini" {
		t.Fatalf("model is %q", agent.model)
	}
	if !strings.Contains(plain(frame(a)), "openai/gpt-4.1-mini") {
		t.Fatalf("the status line did not follow the model:\n%s", plain(frame(a)))
	}

	typeLine(t, a, "/compact")
	if agent.packs != 1 {
		t.Fatalf("compact ran %d times", agent.packs)
	}

	typeLine(t, a, "/nonsense")
	if !strings.Contains(plain(frame(a)), unknownCommandWord("nonsense")) {
		t.Fatalf("an unknown slash has to answer:\n%s", plain(frame(a)))
	}

	typeLine(t, a, "/help")
	// The table is asserted at its source and the note at the screen: the help
	// block is taller than a twenty-row test frame, so which of its rows the
	// bottom of the screen happens to show is a fact about the terminal.
	for _, want := range []string{"/model <slug>", "/compact"} {
		if !strings.Contains(helpText(a.file, a.chords), want) {
			t.Fatalf("help is missing %q from the command table:\n%s", want, helpText(a.file, a.chords))
		}
	}
	// What the SCREEN is asserted on is the block's last row, because that is the
	// end a twenty-row frame is showing: the table above it has grown past the
	// frame twice over as the keys have.
	if !strings.Contains(plain(frame(a)), "settings") {
		t.Fatalf("help did not reach the screen:\n%s", plain(frame(a)))
	}

	if len(agent.sent) != 0 {
		t.Fatalf("a slash command reached the model: %v", agent.sent)
	}
}

// notesSaying counts the lines in the note lane whose words are exactly this.
// It reads the transcript rather than the screen because the question is how
// many times the sentence was WRITTEN — a long note wraps to four rows at a test
// frame's width, and counting rows would answer about the terminal.
func notesSaying(a *app, text string) int {
	n := 0
	for i := range a.entries {
		if a.entries[i].kind == entryNote && a.entries[i].text == text {
			n++
		}
	}
	return n
}

// THE SAME SENTENCE TWICE RUNNING IS ONE SENTENCE (app.go's [feed.note]).
//
// A person who presses a command four times because they are not sure it
// registered used to get four identical lines stacked in the transcript, which
// is the screen counting how many times it had nothing to report. The repeat is
// answered by the line that is already there.
func TestTheSameNoteTwiceRunningIsOneNote(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	unknown := unknownCommandWord("nonsense")
	for range 4 {
		typeLine(t, a, "/nonsense")
	}
	if n := notesSaying(a, unknown); n != 1 {
		t.Fatalf("four presses left %d copies of %q in the transcript", n, unknown)
	}
	// AND IT IS SAID AGAIN IN A NEW PLACE. Something else landing in between puts
	// the repeat under different words, where it is news rather than a stutter —
	// so this asks about the last entry and never about the whole transcript.
	typeLine(t, a, "/cost")
	typeLine(t, a, "/nonsense")
	if n := notesSaying(a, unknown); n != 2 {
		t.Fatalf("the answer after a line of its own was swallowed: %d copies of %q", n, unknown)
	}
}

func TestEscInterruptsAndCtrlCTwiceCloses(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "thinking about it"),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "long one")

	drive(t, a, key("esc"))
	if agent.stops != 1 {
		t.Fatalf("esc did not interrupt (%d)", agent.stops)
	}
	// The stream has not closed, so the word is the wind-down's own
	// (render.go's [stoppingWord]); `interrupted` arrives behind it at the close.
	// Asked of the status line rather than of the frame, because the note the
	// stop writes into the transcript is on the same frame.
	if !strings.Contains(plain(a.status(a.width)), stoppingWord) {
		t.Fatalf("the status line has to say %q:\n%s", stoppingWord, plain(frame(a)))
	}

	// AND THE DOOR TAKES TWO PRESSES (quitarm.go). The first one arms and closes
	// nothing; the second one inside the window leaves.
	// The first press returns the frame clock rather than nothing — the window
	// it opened has an end to reach — so what is asserted is that it is not the
	// door and that nothing was closed.
	_, cmd := a.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("the first ctrl+c quit")
		}
	}
	if agent.closes != 0 {
		t.Fatalf("the first ctrl+c closed the agent (%d)", agent.closes)
	}
	_, cmd = a.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("the second ctrl+c returned no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("the second ctrl+c has to quit")
	}
	if agent.closes != 1 {
		t.Fatalf("ctrl+c closed the agent %d times", agent.closes)
	}
}

// CMD+ENTER NEVER TOUCHES THE STREAM IT WAS TYPED AT. The message waits
// above the box (park.go) and the answer keeps coming on the same channel, at
// the same generation, into the same working state.
func TestCmdEnterDoesNotAbandonTheLiveStream(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "first"),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "one")
	generation, stream := a.gen, a.stream

	parkLine(t, a, "two")
	if a.gen != generation || a.stream != stream {
		t.Fatal("a second enter replaced the stream that was still running")
	}
	if a.state != stateWorking {
		t.Fatalf("state is %v", a.state)
	}
	if len(agent.sent) != 1 {
		t.Fatalf("the second message was sent into the running turn: %v", agent.sent)
	}
	if len(a.parks) != 1 || a.parks[0].text != "two" {
		t.Fatalf("the second message was not held for the answer: %+v", a.parks)
	}
}

func TestScrollSticksToTheBottomUntilTheReaderLeaves(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	// Numbered, because the note lane will not write the same sentence twice
	// running ([feed.note]) and this transcript has to be taller than its window.
	for i := range 40 {
		a.note("line " + itoa(i))
	}
	if !a.stick {
		t.Fatal("a surface that never scrolled has to be stuck to the bottom")
	}
	bottom := a.offsetFor(len(a.visible(a.width)), a.viewHeight())

	a.scroll(-5)
	if a.stick {
		t.Fatal("scrolling up has to release the stick")
	}
	if got := a.offsetFor(len(a.visible(a.width)), a.viewHeight()); got != bottom-5 {
		t.Fatalf("offset is %d, want %d", got, bottom-5)
	}

	a.scroll(100)
	if !a.stick {
		t.Fatal("reaching the bottom has to re-arm the stick")
	}
}

func TestTheEllipsisOnlyShowsWhileNothingElseIsMoving(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{}, {
		toolBegin("bash", "go test ./..."),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "think about it")
	if _, ok := a.ellipsis(); !ok {
		t.Fatal("a working turn with nothing on screen has to show the ellipsis")
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: toolBegin("bash", "go test ./...")})
	if _, ok := a.ellipsis(); ok {
		t.Fatal("a spinning call is already the sign of life")
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: toolEnd("bash", "ok")})

	a.say("done: ")
	if _, ok := a.ellipsis(); ok {
		t.Fatal("streaming text replaces the ellipsis")
	}

	a.state = stateIdle
	if _, ok := a.ellipsis(); ok {
		t.Fatal("an idle surface has no ellipsis")
	}
}

// /new ON A CONVERSATION NOBODY HAS USED YET REPLACES IT. Closing it costs
// nothing — there is nothing in it — and keeping it would spend a slot on a
// conversation that was never typed in.
func TestNewOnAFreshConversationReplacesIt(t *testing.T) {
	first := &fakeAgent{model: "m"}
	second := &fakeAgent{model: "m2"}
	a := newApp(context.Background(), Options{
		Agent:     first,
		Workspace: "/tmp/lab",
		Fresh:     func() (Agent, string, error) { return second, "/tmp/next.jsonl", nil },
	})
	a.width, a.height = 60, 20
	a.pal = newPalette(tokens.ANSI256, false)

	typeLine(t, a, "/new")
	if first.closes != 1 {
		t.Fatalf("the old agent was closed %d times", first.closes)
	}
	if a.agent != Agent(second) || a.file != "/tmp/next.jsonl" {
		t.Fatal("/new did not take the fresh agent")
	}
	if a.openCount() != 1 {
		t.Fatalf("replacing a fresh conversation left %d open", a.openCount())
	}
	got := plain(frame(a))
	if !strings.Contains(got, "new session · /tmp/next.jsonl") {
		t.Fatalf("/new has to name the file:\n%s", got)
	}
}

// AND /new ON A CONVERSATION SOMEBODY HAS USED ADDS ONE, leaving the first
// still open and still running. Every neighbouring door adds — home's enter,
// home's typed path, the welcome box's rows — and a /new that closed a
// conversation with work in it would be the one door that punished somebody for
// using it.
func TestNewOnAUsedConversationAddsOneAndClearsTheTranscript(t *testing.T) {
	first := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	second := &fakeAgent{model: "m2"}
	a := newApp(context.Background(), Options{
		Agent:       first,
		Workspace:   "/tmp/lab",
		SessionFile: "/tmp/lab/one/transcript.jsonl",
		Fresh:       func() (Agent, string, error) { return second, "/tmp/next.jsonl", nil },
	})
	a.width, a.height = 60, 20
	a.pal = newPalette(tokens.ANSI256, false)
	a.stirs = make(chan string, stirDepth)
	typeLine(t, a, "something old")
	drive(t, a, streamClosedMsg{gen: a.gen})

	typeLine(t, a, "/new")
	if first.closed {
		t.Fatal("/new closed a conversation somebody had used")
	}
	if a.agent != Agent(second) || a.file != "/tmp/next.jsonl" {
		t.Fatal("/new did not take the fresh agent")
	}
	if a.openCount() != 2 {
		t.Fatalf("this terminal holds %d conversations", a.openCount())
	}
	got := plain(frame(a))
	if strings.Contains(got, "something old") {
		t.Fatalf("the old conversation survived /new:\n%s", got)
	}
	if !strings.Contains(got, "new conversation · lab") {
		t.Fatalf("/new has to say which of the two happened:\n%s", got)
	}
}

// syncBuffer is the output side of the headless boot: a Bubble Tea program
// writes from its own goroutine, and the test reads while it does.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The surface comes up, draws, takes a command and goes down with no terminal
// attached at all — the same headless door tui2 is checked through.
func TestTheSurfaceBootsAndQuitsHeadlessly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	agent := &fakeAgent{model: "openai/gpt-4.1-mini"}
	in, keyboard := io.Pipe()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Agent: agent, Workspace: "/tmp/lab",
			Input: in, Output: out, Width: 80, Height: 24,
		})
	}()

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), agent.model) {
		if time.Now().After(deadline) {
			t.Fatalf("the surface never drew its status line:\n%q", out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if wire := out.String(); !strings.Contains(wire, "\x1b[?1049h") {
		t.Fatal("the surface did not enter the alt screen")
	}

	if _, err := keyboard.Write([]byte("/quit\r")); err != nil {
		t.Fatalf("write to the surface: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the surface returned %v", err)
		}
	case <-ctx.Done():
		t.Fatal("/quit did not close the surface")
	}
	if agent.closes != 1 {
		t.Fatalf("the agent was closed %d times", agent.closes)
	}
}
