package session

// THE STRUCTURAL HALF OF loop.go's LAW.
//
// loop_speed_test.go proves the two figures on one turn; this proves the SHAPE
// on the whole package, which is the half that survives somebody writing a
// fixture the runtime test happens not to cover.
//
// THERE ARE TWO WAYS TO PUT A READING BACK IN FRONT OF THE WORK, and this holds
// both to one property:
//
//	PERFORMING one where the turn can see it — `a.readMark(ctx)` as a line of a
//	step boundary, which is the 8.1-second gap the census measured.
//	WAITING for one that is already running — `…takeAtTheEnd()`, which is the
//	same gap wearing the mechanism's own clothes.
//
// THE PROPERTY IS: THE TURN MUST ALREADY BE COMMITTED TO ENDING. A reading
// beside the work exists to be APPLIED — to this step if it lands in time, to
// the next one if it does not — so the one condition under which standing in
// front of the work is honest is that there is no next step to apply it to. The
// tree can see that: the same block goes on to call a door that moves the turn
// somewhere else, with nothing between that could leave first. A deferred body
// is the same statement read at the other end of the function — it runs when
// everything has already been decided — so it is judged against the whole
// function it is deferred in.
//
// AND THE WATCHED SET IS READ OFF THE TREE, not typed here. Whatever a
// [readBeside] `ask` calls IS a reading, by construction, so a reading added
// tomorrow under a name nobody has thought of is on this law the day it lands.
//
// IT READS THE TREE ITSELF, so it runs on the laws gate of every pull request
// (scripts/laws.sh finds it by this import).

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// theWaitingVerb is the sidecar's one blocking read. It is spelled for the only
// condition under which it is honest, which is what lets this law find every
// caller of it without a list and without confusing it for anything else in the
// package that happens to settle something.
const theWaitingVerb = "takeAtTheEnd"

// endingDoors are the calls that MOVE A TURN SOMEWHERE ELSE or close it. They
// are named for what they are rather than for where they are called from, and
// they are the whole of the exception.
//
// A DOOR IS NOT AN EXEMPTION FOR THE FUNCTION IT IS IN. It exempts the run of
// statements that reaches it, which is the difference between "this road ends
// the turn" and "this function ends the turn somewhere". Adding a call site on
// one of those roads needs no edit here; awaiting a reading anywhere a turn can
// still CARRY ON fails the build no matter what it is called.
// AND `checkpointCeiling` IS NOT ONE OF THEM ANY MORE. It was, while the ceiling
// was an unconditional hand-over and its drawing was read in line on the way out
// of the turn. It is now a rung a turn can CARRY ON PAST — it compacts in place
// and the meter re-arms (checkpoint.go) — so a reading awaited on the way to it
// is a reading awaited in front of work that is about to continue, which is
// exactly what this law is for. Its own drawing rides beside the turn with the
// two marks below it and it makes no model call of its own.
var endingDoors = map[string]bool{
	"handOverRunningTurn": true,
	"sealTurn":            true,
}

func TestEveryReadingBesideTheWorkGoesThroughTheOneDoor(t *testing.T) {
	set := token.NewFileSet()
	files := parsePackage(t, set)
	readings := readingsFrom(files)
	if len(readings) < 3 {
		t.Fatalf("only %d readings found through readBeside (%v); the law is reading the wrong tree",
			len(readings), namesOf(readings))
	}
	for _, name := range []string{"refreshMemory", "readMark", "readRouteJudge"} {
		if !readings[name] {
			t.Errorf("%s is not among the readings this law watches (%v) — it is supposed to be "+
				"derived from every readBeside ask in the package", name, namesOf(readings))
		}
	}
	for _, file := range files {
		for _, complaint := range besideLaw(set, file, readings) {
			t.Error(complaint)
		}
	}
}

// AND THE LAW BITES. Three shapes that each restore a wait in front of the
// work, each of which passed the earlier version of this file, planted here so
// that the law is held to naming them rather than to being green.
func TestTheLawNamesAWaitPutBackInFrontOfTheWork(t *testing.T) {
	readings := map[string]bool{"readMark": true, "refreshMemory": true}
	for _, plant := range []struct {
		what   string
		source string
		want   string
	}{{
		what: "a reading performed at the top of a function that ends the turn much later",
		source: `package session
func (a *Agent) checkpointRound(ctx context.Context) bool {
	read := a.readMark(ctx)
	if read.failed {
		return false
	}
	if a.mark == 0 {
		return false
	}
	return a.checkpointCeiling(ctx, read)
}`,
		want: "readMark",
	}, {
		what: "a reading started and awaited in the same breath, on a road that does end the turn",
		source: `package session
func (a *Agent) runTurn(ctx context.Context) bool {
	block, _ := readBeside(ctx, func(c context.Context) bool {
		return a.refreshMemory(c)
	}, nil).takeAtTheEnd()
	_ = block
	return a.sealTurn()
}`,
		want: "readBeside",
	}, {
		what: "a wait on a handle in a branch whose sibling is the one that ends the turn",
		source: `package session
func (a *Agent) checkpointRound(ctx context.Context, aside *markAside) bool {
	if a.mark < 3 {
		landed, _ := aside.takeAtTheEnd()
		_ = landed
		return false
	}
	return a.checkpointCeiling(ctx)
}`,
		want: theWaitingVerb,
	}} {
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, "planted.go", plant.source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing the plant for %s: %v", plant.what, err)
		}
		complaints := besideLaw(set, file, readings)
		if len(complaints) == 0 {
			t.Errorf("the law said nothing about %s — it is green on a turn that waits", plant.what)
			continue
		}
		if !strings.Contains(strings.Join(complaints, "\n"), plant.want) {
			t.Errorf("the law complained about %s without naming %q:\n%s",
				plant.what, plant.want, strings.Join(complaints, "\n"))
		}
	}
}

// AND THE LAW IS NOT MERELY LOUD. The shapes that are honest — the handovers,
// which really do move the turn somewhere else, and the turn's own exit — must
// pass it, or a law nobody can satisfy is a law somebody deletes.
func TestTheLawLetsAnEndingReadInLine(t *testing.T) {
	readings := map[string]bool{"readMark": true}
	for _, honest := range []struct {
		what   string
		source string
	}{{
		what: "a handover that reads the work it is about to move",
		source: `package session
func (a *Agent) checkpointWriting(ctx context.Context) bool {
	read := a.readMark(ctx)
	a.journalMarkRead(read)
	return a.handOverRunningTurn(ctx, read).moved
}`,
	}, {
		what: "the turn's own exit, where everything has already been decided",
		source: `package session
func (a *Agent) runTurn(ctx context.Context) bool {
	marked := &markAside{}
	defer func() {
		if landed, ok := marked.takeAtTheEnd(); ok {
			a.journalMarkRead(landed.read)
		}
		marked.end()
	}()
	if a.stopped {
		return false
	}
	return a.sealTurn()
}`,
	}} {
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, "honest.go", honest.source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", honest.what, err)
		}
		if complaints := besideLaw(set, file, readings); len(complaints) > 0 {
			t.Errorf("the law refused %s:\n%s", honest.what, strings.Join(complaints, "\n"))
		}
	}
}

// ── the law itself ──────────────────────────────────────────────────────────

// besideLaw is one file's violations, in source order. It is a function rather
// than a test body so that the plants above can be held to it.
func besideLaw(set *token.FileSet, file *ast.File, readings map[string]bool) []string {
	var complaints []string
	spans := besideRanges(file)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// A READING MAY CALL A READING, and a handle's wait may forward to the
		// sidecar's. What the law is about is the WORK doing either, and neither a
		// reading's own implementation nor the mechanism's own plumbing is the
		// work: a method SPELLED `takeAtTheEnd` is one of the handles that carries
		// the wait, and it is its CALLERS this law is written about.
		ownReading := readings[fn.Name.Name]
		ownWait := fn.Name.Name == theWaitingVerb
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			// A reading started and consumed in the same expression never ran
			// beside anything, whatever the road it stands on.
			if inner, consumed := consumesAReadBesideInline(call); consumed {
				complaints = append(complaints, fmt.Sprintf(
					"%s: readBeside is started and %s in the same breath — a reading nobody ever "+
						"ran beside is a reading in front of the work (sidecar.go)",
					set.Position(inner.Pos()), inner.Sel.Name))
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			name := selector.Sel.Name
			watched := readings[name] || name == theWaitingVerb
			if !watched || insideSpan(spans, call.Pos()) {
				return true
			}
			if (ownReading && name != theWaitingVerb) || (ownWait && name == theWaitingVerb) {
				return true
			}
			if committedToAnEnding(fn, call.Pos()) {
				return true
			}
			verb := "is performed"
			if name == theWaitingVerb {
				verb = "is waited for"
			}
			complaints = append(complaints, fmt.Sprintf(
				"%s: %s %s in %s, on a road where the turn can still carry on — a reading runs "+
					"BESIDE the work through readBeside and is spent at the next boundary, never in "+
					"front of it (sidecar.go, loop.go's header)",
				set.Position(call.Pos()), name, verb, fn.Name.Name))
			return true
		})
	}
	sort.Strings(complaints)
	return complaints
}

// consumesAReadBesideInline reports a `readBeside(…).something()` — the shape
// that satisfies every other clause of this law and restores the whole gate.
func consumesAReadBesideInline(call *ast.CallExpr) (*ast.SelectorExpr, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	inner, ok := selector.X.(*ast.CallExpr)
	if !ok || !isReadBeside(inner) {
		return nil, false
	}
	return selector, true
}

// committedToAnEnding reports whether the statement at `at` is on a run of
// statements that reaches an ending door with nothing between that could leave
// the function first.
//
// A DEFERRED BODY IS JUDGED AGAINST ITS WHOLE FUNCTION, because that is when it
// runs: everything the function was going to decide has been decided by then, so
// the question is only whether this function is one that ends a turn.
func committedToAnEnding(fn *ast.FuncDecl, at token.Pos) bool {
	path := pathTo(fn.Body, at)
	for _, node := range path {
		if lit, ok := node.(*ast.FuncLit); ok && deferredIn(fn, lit) {
			return callsAnEndingDoor(fn.Body)
		}
	}
	block, index := innermostStatement(path, at)
	if block == nil {
		return false
	}
	for i := index; i < len(block.List); i++ {
		if callsAnEndingDoor(block.List[i]) {
			return true
		}
		// Anything that can leave before the door is reached breaks the run: the
		// turn was never committed to ending at all.
		if i > index && leavesTheRun(block.List[i]) {
			return false
		}
	}
	return false
}

// innermostStatement is the block that holds the statement `at` sits in, and
// that statement's index in it.
func innermostStatement(path []ast.Node, at token.Pos) (*ast.BlockStmt, int) {
	for i := len(path) - 1; i >= 0; i-- {
		block, ok := path[i].(*ast.BlockStmt)
		if !ok {
			continue
		}
		for index, statement := range block.List {
			if statement.Pos() <= at && at <= statement.End() {
				return block, index
			}
		}
	}
	return nil, 0
}

// leavesTheRun reports whether a statement can hand control back before the
// statements under it are reached.
func leavesTheRun(node ast.Node) bool {
	leaves := false
	ast.Inspect(node, func(inner ast.Node) bool {
		switch inner.(type) {
		case *ast.ReturnStmt:
			leaves = true
		case *ast.BranchStmt:
			leaves = true
		case *ast.FuncLit:
			// A closure's own return leaves the closure, not this run.
			return false
		}
		return !leaves
	})
	return leaves
}

func callsAnEndingDoor(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(inner ast.Node) bool {
		call, ok := inner.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && endingDoors[selector.Sel.Name] {
			found = true
		}
		return !found
	})
	return found
}

func deferredIn(fn *ast.FuncDecl, lit *ast.FuncLit) bool {
	deferred := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		statement, ok := node.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if statement.Call.Fun == ast.Expr(lit) {
			deferred = true
		}
		return !deferred
	})
	return deferred
}

// pathTo is the chain of nodes enclosing a position, outermost first.
func pathTo(root ast.Node, at token.Pos) []ast.Node {
	var path []ast.Node
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			return false
		}
		if node.Pos() <= at && at <= node.End() {
			path = append(path, node)
			return true
		}
		return false
	})
	return path
}

// ── what a reading IS, read off the tree ────────────────────────────────────

// readingsFrom is every call a [readBeside] `ask` makes. That is the definition
// of a reading in this package — something the harness asks on its own behalf,
// beside the work — so deriving the set here is what keeps this law from being a
// list of the three names somebody happened to measure.
func readingsFrom(files []*ast.File) map[string]bool {
	readings := map[string]bool{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isReadBeside(call) || len(call.Args) < 2 {
				return true
			}
			ast.Inspect(call.Args[1], func(inner ast.Node) bool {
				asked, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				if selector, ok := asked.Fun.(*ast.SelectorExpr); ok {
					readings[selector.Sel.Name] = true
				}
				return true
			})
			return true
		})
	}
	return readings
}

func namesOf(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// isReadBeside reports the door, spelled either way: a generic call is written
// `readBeside[T](…)` when the type cannot be inferred.
func isReadBeside(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name == "readBeside"
	case *ast.IndexExpr:
		inner, ok := fun.X.(*ast.Ident)
		return ok && inner.Name == "readBeside"
	}
	return false
}

// besideRanges is every span of source that is an argument to [readBeside],
// which is the one place a reading is allowed to be named.
func besideRanges(file *ast.File) [][2]token.Pos {
	var spans [][2]token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isReadBeside(call) {
			return true
		}
		spans = append(spans, [2]token.Pos{call.Pos(), call.End()})
		return true
	})
	return spans
}

func insideSpan(spans [][2]token.Pos, at token.Pos) bool {
	for _, span := range spans {
		if at >= span[0] && at <= span[1] {
			return true
		}
	}
	return false
}

// ── THE SAME LAW READ FROM THE OTHER END ────────────────────────────────────
//
// A reading may not stand in front of the work; neither may a LISTENER. The
// news doors of this package are told by the engine's own goroutine at the exit
// of every model call, and the surface's reader asks Bubble Tea for a frame
// down an unbuffered channel — so a straight call there put a whole draw on the
// critical path of every call, which is loop.go's law broken from the far side
// (the measured seam: `defer phase.done()` → postPhase → forwardPhase →
// postPhaseNews → the surface's reader → `p.Send`).
//
// THE PROPERTY: A REGISTERED LISTENER IS ONLY EVER CALLED FROM A DESK. Not
// "postPhaseNews uses a desk" — that is a fact about one function and it was
// true of the doc comment while the code did the opposite. Whatever this
// package hands a person's reader, it hands it through [desk.tell].
//
// AND THE WATCHED SET IS READ OFF THE TREE. Every package-level `…Reader` of
// function type IS a listener, so a third news channel added tomorrow is on this
// law the day it lands, with no edit here.
func TestAListenerIsAlwaysToldFromADesk(t *testing.T) {
	set := token.NewFileSet()
	files := parsePackage(t, set)
	listeners := listenersFrom(files)
	if len(listeners) < 2 {
		t.Fatalf("only %d registered listeners found (%v); the law is reading the wrong tree",
			len(listeners), namesOf(listeners))
	}
	for _, name := range []string{"phaseReader", "laneNewsReader"} {
		if !listeners[name] {
			t.Errorf("%s is not among the listeners this law watches (%v) — it is supposed to be "+
				"derived from the package's own reader variables", name, namesOf(listeners))
		}
	}
	for _, file := range files {
		for _, complaint := range deskLaw(set, file, listeners) {
			t.Error(complaint)
		}
	}
}

// AND IT BITES. The shape below is what the code did before the desk, and it is
// what a well-meaning edit puts back — the reader is resolved under the lock and
// then simply called, which reads like nothing at all.
func TestTheLawNamesAListenerCalledOnTheTurnsOwnGoroutine(t *testing.T) {
	listeners := map[string]bool{"phaseReader": true}
	for _, plant := range []struct {
		what   string
		source string
		want   string
	}{{
		what: "the reader called straight down the caller's stack",
		source: `package session
func postPhaseNews(news PhaseNews) {
	phaseMu.RLock()
	reader := phaseReader
	phaseMu.RUnlock()
	if reader == nil {
		return
	}
	reader(news)
}`,
		want: "postPhaseNews",
	}, {
		what: "the reader called by name, with no local to hide behind",
		source: `package session
func postPhaseNews(news PhaseNews) {
	phaseMu.RLock()
	defer phaseMu.RUnlock()
	phaseReader(news)
}`,
		want: "postPhaseNews",
	}, {
		what: "a desk told to call it, and then it called anyway",
		source: `package session
func postPhaseNews(news PhaseNews) {
	reader := phaseReader
	phaseDesk.tell(func() { reader(news) })
	reader(news)
}`,
		want: "postPhaseNews",
	}} {
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, "planted.go", plant.source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: parsing the plant: %v", plant.what, err)
		}
		complaints := deskLaw(set, file, listeners)
		if len(complaints) == 0 {
			t.Errorf("%s: the law let it through — a listener called on the turn's own goroutine "+
				"is the seam this desk exists for", plant.what)
			continue
		}
		if !strings.Contains(strings.Join(complaints, "\n"), plant.want) {
			t.Errorf("%s: the law complained but never named %s: %v", plant.what, plant.want, complaints)
		}
	}
}

// AND IT LETS THE HONEST SHAPE THROUGH, because a law nobody can satisfy is a
// law somebody deletes.
func TestTheLawLetsADeskCarryTheNews(t *testing.T) {
	listeners := map[string]bool{"phaseReader": true, "laneNewsReader": true}
	source := `package session
func postPhaseNews(news PhaseNews) {
	phaseMu.RLock()
	reader := phaseReader
	phaseMu.RUnlock()
	if reader == nil {
		return
	}
	phaseDesk.tell(func() { reader(news) })
}

func postLaneNews(news LaneNews) {
	reader := laneNewsReader
	laneNewsDesk.tell(func() { reader(news) })
}`
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "honest.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if complaints := deskLaw(set, file, listeners); len(complaints) != 0 {
		t.Errorf("the law refused the shape the package is written in: %v", complaints)
	}
}

// listenersFrom is the watched set, derived: every package-level variable of
// function type whose name says it is somebody's reader.
func listenersFrom(files []*ast.File) map[string]bool {
	listeners := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if _, isFunc := value.Type.(*ast.FuncType); !isFunc {
					continue
				}
				for _, name := range value.Names {
					if strings.HasSuffix(name.Name, "Reader") {
						listeners[name.Name] = true
					}
				}
			}
		}
	}
	return listeners
}

// deskLaw is the property itself, extracted so the plants above are judged by
// the same code the package is.
func deskLaw(set *token.FileSet, file *ast.File, listeners map[string]bool) []string {
	var complaints []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// A LOCAL IS THE LISTENER UNDER ANOTHER NAME. Resolving the reader under
		// the lock and calling the local is the shape the code actually had, so a
		// law that only watched the package name would have watched nothing.
		held := map[string]bool{}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			switch stmt := node.(type) {
			case *ast.AssignStmt:
				for at, rhs := range stmt.Rhs {
					ident, ok := rhs.(*ast.Ident)
					if !ok || !listeners[ident.Name] || at >= len(stmt.Lhs) {
						continue
					}
					if to, ok := stmt.Lhs[at].(*ast.Ident); ok {
						held[to.Name] = true
					}
				}
			}
			return true
		})
		for _, at := range calledOutsideADesk(fn.Body, listeners, held) {
			complaints = append(complaints, fmt.Sprintf(
				"%s: %s calls a registered listener on the turn's own goroutine. Every reader this "+
					"package installs is told from a desk (sidecar.go's desk, loop.go's law read from "+
					"the other end) — a surface's reader asks for a frame down an unbuffered channel, "+
					"so a straight call here is a whole draw on the critical path of every model call",
				set.Position(at).String(), fn.Name.Name))
		}
	}
	return complaints
}

// calledOutsideADesk is every call of a listener that is not lexically inside
// the closure handed to a [desk.tell].
func calledOutsideADesk(body *ast.BlockStmt, listeners, held map[string]bool) []token.Pos {
	var loose []token.Pos
	var walk func(node ast.Node, onADesk bool)
	walk = func(node ast.Node, onADesk bool) {
		if node == nil {
			return
		}
		if call, ok := node.(*ast.CallExpr); ok {
			if isDeskTell(call) {
				for _, arg := range call.Args {
					walk(arg, true)
				}
				walk(call.Fun, onADesk)
				return
			}
			if fun, ok := call.Fun.(*ast.Ident); ok && (listeners[fun.Name] || held[fun.Name]) && !onADesk {
				loose = append(loose, call.Pos())
			}
		}
		for _, child := range children(node) {
			walk(child, onADesk)
		}
	}
	walk(body, false)
	return loose
}

func isDeskTell(call *ast.CallExpr) bool {
	fun, ok := call.Fun.(*ast.SelectorExpr)
	return ok && fun.Sel.Name == "tell"
}

// children is one level of the tree, which [ast.Inspect] will not give while a
// walk is carrying state of its own.
func children(node ast.Node) []ast.Node {
	var kids []ast.Node
	first := true
	ast.Inspect(node, func(inner ast.Node) bool {
		if first {
			first = false
			return true
		}
		if inner != nil {
			kids = append(kids, inner)
		}
		return false
	})
	return kids
}

func parsePackage(t *testing.T, set *token.FileSet) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, filepath.Join(".", name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files = append(files, file)
	}
	return files
}

// AND THE DOOR ITSELF IS ONE DOOR. A second copy of [readBeside] — a `go func()`
// with its own channel, written because somebody wanted one field more — is the
// spaghetti this file exists to prevent, so the package is allowed exactly one
// declaration of it, and exactly one blocking read.
func TestThereIsOneDoorForAReadingBesideTheWork(t *testing.T) {
	set := token.NewFileSet()
	doors, waits := 0, 0
	for _, file := range parsePackage(t, set) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Recv == nil && fn.Name.Name == "readBeside" {
				doors++
			}
			if fn.Recv != nil && fn.Name.Name == theWaitingVerb && receiverNames(fn.Recv, "sidecar") {
				waits++
			}
		}
	}
	if doors != 1 {
		t.Fatalf("%d declarations of readBeside, want exactly one (sidecar.go)", doors)
	}
	if waits != 1 {
		t.Fatalf("%d blocking reads declared on the sidecar itself, want exactly one — the wait is "+
			"the exception and a second spelling of it is a second exception", waits)
	}
}

// AND THE WATCH IS A TEST'S, NEVER THE WORK'S. [besideWatch] lets a test pin an
// order between a reading and the work without guessing at the scheduler, and
// that is all it may do: the product carrying one, or waiting on one, is a
// reading standing in front of the work again under another name. So no file of
// the product calls the door that carries a watch or the verb that waits on one.
func TestTheRunningProductNeverWaitsOnAReadingsLanding(t *testing.T) {
	set := token.NewFileSet()
	for _, file := range parsePackage(t, set) {
		for _, complaint := range watchLaw(set, file) {
			t.Error(complaint)
		}
	}
	// AND IT BITES, on the two ways the product would reach for it.
	const planted = `package session
func (a *Agent) runTurn(ctx context.Context) {
	ctx = withBesideWatch(ctx, &besideWatch{})
	besideWatchOn(ctx).quiet(ctx)
}`
	file, err := parser.ParseFile(set, "planted.go", planted, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing the plant: %v", err)
	}
	if complaints := watchLaw(set, file); len(complaints) != 2 {
		t.Errorf("the law named %d of the two planted uses: %v", len(complaints), complaints)
	}
}

// watchLaw is one file's uses of the watch, in source order.
func watchLaw(set *token.FileSet, file *ast.File) []string {
	var complaints []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		}
		if name == "withBesideWatch" || name == "quiet" {
			complaints = append(complaints, fmt.Sprintf(
				"%s: %s in the product — a watch on a reading's landing is how a TEST pins an order "+
					"(sidecar.go's besideWatch), and the work never carries one or waits on one",
				set.Position(call.Pos()), name))
		}
		return true
	})
	return complaints
}

// ── AND THE ANSWER IS READABLE BEFORE THE INTERRUPTION IS DELIVERED ─────────
//
// #956 was one statement in the wrong order. [readBeside] published its answer
// only once `act` had RETURNED, and `act` is the interruption — so the boundary
// the cut bought arrived at a [sidecar.take] that is non-blocking by
// construction, read the reading as still in flight, and let that boundary pass.
// The turn paid for a whole extra step, and where that step was its last, a
// drawing that said the work had independent parts in it was written down as a
// carry-on and nothing was handed anywhere.
//
// THE PROPERTY IS STRUCTURAL AND SO IS THE LAW: inside the one door, the settle
// closes BEFORE the `act` is called. It is written here rather than as a comment
// in sidecar.go because the old order had a paragraph of its own explaining why
// it was right, and the paragraph was what survived the reading.
func TestTheAnswerIsReadableBeforeTheInterruptionIsDelivered(t *testing.T) {
	set := token.NewFileSet()
	var checked bool
	for _, file := range parsePackage(t, set) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "readBeside" || fn.Body == nil {
				continue
			}
			checked = true
			for _, complaint := range settleBeforeActLaw(set, fn) {
				t.Error(complaint)
			}
		}
	}
	if !checked {
		t.Fatal("readBeside was not found in this package; the law is reading the wrong tree")
	}
}

// AND IT BITES. The order the door was written in before #956, which reads like
// nothing at all and cost a drawing the boundary its own cut had just bought.
func TestTheLawNamesAnAnswerPublishedAfterItsInterruption(t *testing.T) {
	source := `package session
func readBeside[T any](ctx context.Context, ask func(context.Context) T, act func(T)) *sidecar[T] {
	readCtx, stop := context.WithCancel(ctx)
	side := &sidecar[T]{stop: stop, settled: make(chan struct{})}
	go func() {
		answer := ask(readCtx)
		side.answer = answer
		if act != nil && readCtx.Err() == nil {
			act(answer)
		}
		close(side.settled)
	}()
	return side
}`
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "planted.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing the plant: %v", err)
	}
	fn := file.Decls[0].(*ast.FuncDecl)
	complaints := settleBeforeActLaw(set, fn)
	if len(complaints) == 0 {
		t.Fatal("the law said nothing about an answer published after its own interruption — " +
			"that is the order #956 was, and a law that is green on it is a comment")
	}
	if !strings.Contains(strings.Join(complaints, "\n"), "settled") {
		t.Errorf("the law complained without naming the settle: %v", complaints)
	}
}

// AND IT BITES ON THE RENAME, which is the way a structural law usually stops
// guarding: nobody reorders the body, somebody renames the parameter, and a law
// that matched the old spelling goes quiet while the door it guards keeps its
// shape. The interruption here is called `raise`, is never called, and the
// settle is closed on something that is not the settle — three ways for the law
// to be green by accident, and it must be green on none of them.
func TestTheLawNamesADoorThatNeverDeliversItsInterruption(t *testing.T) {
	source := `package session
func readBeside[T any](ctx context.Context, ask func(context.Context) T, raise func(T)) *sidecar[T] {
	readCtx, stop := context.WithCancel(ctx)
	side := &sidecar[T]{stop: stop, settled: make(chan struct{})}
	go func() {
		answer := ask(readCtx)
		side.answer = answer
		close(side.done)
	}()
	return side
}`
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "planted.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing the plant: %v", err)
	}
	complaints := settleBeforeActLaw(set, file.Decls[0].(*ast.FuncDecl))
	joined := strings.Join(complaints, "\n")
	if !strings.Contains(joined, "raise") {
		t.Errorf("the law did not name the interruption it never delivers: %v", complaints)
	}
	if !strings.Contains(joined, "settled") {
		t.Errorf("the law did not name the settle nobody closes: %v", complaints)
	}
}

// settleBeforeActLaw is the property itself: within [readBeside], the statement
// that closes `settled` precedes the one that delivers the interruption.
//
// IT FINDS THE INTERRUPTION BY ITS SHAPE AND NEVER BY ITS NAME, and that is not
// fussiness. A law that matched the identifier `act` was satisfied by renaming
// the parameter — the door would keep its shape, the body would keep the bug,
// and nothing would say so. The reading and the interruption are told apart by
// what they are: [readBeside] takes exactly two function parameters, the reading
// ANSWERS (it has a result, and its call is what produces the answer) and the
// interruption does NOT (it is called for what it does to the work). So the law
// reads the signature, and every complaint below is a way of being green by
// accident rather than by construction.
func settleBeforeActLaw(set *token.FileSet, fn *ast.FuncDecl) []string {
	var complaints []string
	// The two function parameters, told apart by whether they answer.
	reading, interruption := "", ""
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			signature, ok := field.Type.(*ast.FuncType)
			if !ok {
				continue
			}
			answers := signature.Results != nil && len(signature.Results.List) > 0
			for _, name := range field.Names {
				if answers {
					reading = name.Name
				} else {
					interruption = name.Name
				}
			}
		}
	}
	if reading == "" || interruption == "" {
		return append(complaints, fmt.Sprintf("%s: readBeside no longer takes both a reading that answers "+
			"and an interruption that does not — this law tells them apart by that and cannot read the "+
			"door it guards (sidecar.go)", set.Position(fn.Pos())))
	}
	// The first close of the settle, and the first call of each parameter.
	calls := map[string]token.Pos{}
	var closed token.Pos
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if name.Name == "close" {
			// AND IT MUST BE THE SETTLE, not any channel that happens to be closed
			// here: the answer becomes takeable when `settled` closes and nowhere else.
			if len(call.Args) == 1 && closed == token.NoPos {
				if field, ok := call.Args[0].(*ast.SelectorExpr); ok && field.Sel.Name == "settled" {
					closed = call.Pos()
				}
			}
			return true
		}
		if _, seen := calls[name.Name]; !seen {
			calls[name.Name] = call.Pos()
		}
		return true
	})
	if closed == token.NoPos {
		complaints = append(complaints, fmt.Sprintf("%s: readBeside closes no `settled` at all — a reading "+
			"nobody can take is not a reading beside the work (sidecar.go)", set.Position(fn.Pos())))
	}
	if _, asked := calls[reading]; !asked {
		complaints = append(complaints, fmt.Sprintf("%s: readBeside never calls its reading %q (sidecar.go)",
			set.Position(fn.Pos()), reading))
	}
	acted, delivered := calls[interruption]
	if !delivered {
		// FAILING CLOSED IS THE POINT. An interruption the door never delivers is
		// either a power a reading has lost or a law that has stopped looking.
		return append(complaints, fmt.Sprintf("%s: readBeside never calls its interruption %q, so this law "+
			"is guarding nothing — a reading's one power over the work is delivered here or nowhere "+
			"(sidecar.go)", set.Position(fn.Pos()), interruption))
	}
	if closed != token.NoPos && closed > acted {
		complaints = append(complaints, fmt.Sprintf("%s: readBeside delivers the reading's interruption %q "+
			"before it closes `settled`, so the boundary that interruption opens can read the answer as "+
			"still in flight and let it pass (#956). The answer is published first; sidecar.spent is what "+
			"keeps a late interruption out of the next turn", set.Position(acted), interruption))
	}
	return complaints
}

func receiverNames(recv *ast.FieldList, want string) bool {
	if recv == nil || len(recv.List) == 0 {
		return false
	}
	return strings.Contains(typeText(recv.List[0].Type), want)
}

func typeText(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.StarExpr:
		return typeText(node.X)
	case *ast.IndexExpr:
		return typeText(node.X)
	}
	return ""
}
