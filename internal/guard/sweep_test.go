package guard

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

// guardedPackages are the trees this sweep holds to the law. Each one runs
// goroutines under a live terminal surface, so a fault in any of them is a
// fault the user would watch happen.
var guardedPackages = []string{
	"cmd/aforge",
	"internal/catalog",
	"internal/exec",
	"internal/plan",
	"internal/provider",
	"internal/resident",
	"internal/store",
	"internal/voice",
}

// spawnAllowlist names the spawn sites that carry their guard somewhere the
// sweep cannot see — inside the function being spawned — with the reason.
// Nothing else belongs here: "it cannot panic" is a claim the next edit breaks.
var spawnAllowlist = map[string]string{
	"internal/exec/jobs.go:go r.wait(job)":                                                       "wait recovers and settles the job's terminal state itself",
	"internal/exec/schedule.go:go s.work(leafCtx, id, task, retries[id], leafShape(node), done)": "work recovers and reports the fault as the leaf's completion",
	"internal/plan/ensemble.go:go write(pass, inputs, &passBrief)":                               "write recovers into the failures slot it shares with the setup brief",
	"internal/plan/ensemble.go:go write(setup, nil, &setupBrief)":                                "write recovers into the failures slot it shares with the pass brief",
	"internal/voice/recorder.go:go r.read(command, output, r.done, r.chunks, r.quit)":            "read recovers and still writes done and closes chunks, which Stop and the caption consumer wait on",
}

// TestEveryGoroutineInTheGuardedTreeIsGuarded is the standing check behind the
// law that the terminal surface never dies from a panic. A new goroutine either
// starts with a guard or is named here with a reason.
func TestEveryGoroutineInTheGuardedTreeIsGuarded(t *testing.T) {
	root := repositoryRoot(t)
	var unguarded []string

	for _, pkg := range guardedPackages {
		err := filepath.WalkDir(filepath.Join(root, pkg), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			sites, err := scanSpawns(raw)
			if err != nil {
				return fmt.Errorf("%s: %w", relative, err)
			}
			for _, site := range sites {
				if site.guarded {
					continue
				}
				if _, allowed := spawnAllowlist[relative+":"+site.statement]; allowed {
					continue
				}
				unguarded = append(unguarded, fmt.Sprintf("%s:%d: %s", relative, site.line, site.statement))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", pkg, err)
		}
	}

	sort.Strings(unguarded)
	if len(unguarded) > 0 {
		t.Fatalf("these goroutines can take the terminal surface down with them.\n"+
			"Wrap each in guard.Go, or open it with `defer guard.Recover(\"scope\")`:\n  %s",
			strings.Join(unguarded, "\n  "))
	}
}

// TestSpawnAllowlistIsStillReal keeps the escape hatch honest: an allowlisted
// site that no longer exists is a stale exemption the next spawn could inherit.
func TestSpawnAllowlistIsStillReal(t *testing.T) {
	root := repositoryRoot(t)
	for key := range spawnAllowlist {
		file, statement, found := strings.Cut(key, ":")
		if !found {
			t.Fatalf("allowlist key %q is not file:statement", key)
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			t.Fatalf("allowlisted %s: %v", file, err)
		}
		if !strings.Contains(string(raw), statement) {
			t.Fatalf("allowlisted spawn is gone from %s: %q", file, statement)
		}
	}
}

// TestSpawnSweepReadsTheDefersNotTheDistance pins the rule the sweep applies,
// on the shapes that taught it. The first fixture is internal/plan/plan.go's
// spine opener as a042286f left it: the recover is the third defer, behind a
// wait-group release, a cancel that must run after it, and a paragraph of
// prose, which put it past the ten text lines the old sweep read and had a
// guarded goroutine reported as bare. The second is spine.go's sampler, whose
// recover reads a flag declared just above it. The rest are the ways a
// goroutine is honestly open or honestly not, so a change to the checker has to
// keep every verdict rather than just the one that was wrong — including the
// two shapes that spell a recover the runtime would never reach, `if false {
// recover() }` and `for { recover() }`, which read as guards to anything that
// only asks whether the identifier appears somewhere inside the defer.
func TestSpawnSweepReadsTheDefersNotTheDistance(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		guarded bool
	}{
		{
			name: "a recover deferred after another defer and a comment, more than ten lines in",
			body: `go func() {
		defer opening.Done()
		defer func() {
			if spineErr != nil {
				stopGrounding()
			}
		}()
		// Both openers carry their fault out in the error the caller below
		// already reads: a faulted spine fails the build, as a failed one does,
		// and a faulted grounding is joined into the returned error while the
		// plan carries on without it.
		defer func() {
			if recovered := recover(); recovered != nil {
				choice, spineErr = nil, guard.Note("plan/build spine", recovered)
			}
		}()
		choice, spineErr = spine(ctx)
	}()`,
			guarded: true,
		},
		{
			name: "a recover deferred after a flag it reads, which is a statement but not work",
			body: `go func(index int) {
		defer group.Done()
		landed := false
		defer func() {
			if recovered := recover(); recovered != nil {
				if !landed {
					results[index] = result{err: guard.Note("plan/spine sample", recovered)}
				}
			}
		}()
		results[index] = sample(ctx)
		landed = true
	}(index)`,
			guarded: true,
		},
		{
			name:    "a recover deferred only after a call has been made",
			body:    "go func() {\n\tstarted := time.Now()\n\tdefer func() { _ = recover(); _ = started }()\n}()",
			guarded: false,
		},
		{
			name:    "the guard's own deferred recover",
			body:    "go func() {\n\tdefer guard.Recover(\"fixture\")\n\twork()\n}()",
			guarded: true,
		},
		{
			name:    "a recover deferred only after the work has started",
			body:    "go func() {\n\twork()\n\tdefer func() { _ = recover() }()\n}()",
			guarded: false,
		},
		{
			name:    "a recover that belongs to a nested literal, not to the goroutine",
			body:    "go func() {\n\tdefer func() {\n\t\tinner := func() { _ = recover() }\n\t\t_ = inner\n\t}()\n\twork()\n}()",
			guarded: false,
		},
		{
			name:    "the house shape, a recover in the init of the deferred literal's own if",
			body:    "go func() {\n\tdefer func() {\n\t\tif r := recover(); r != nil {\n\t\t\tnote(r)\n\t\t}\n\t}()\n\twork()\n}()",
			guarded: true,
		},
		{
			name:    "a bare recover statement, which is the whole of the deferred body",
			body:    "go func() {\n\tdefer func() {\n\t\trecover()\n\t}()\n\twork()\n}()",
			guarded: true,
		},
		{
			name:    "the same recover in the header of a switch rather than an if",
			body:    "go func() {\n\tdefer func() {\n\t\tswitch r := recover(); r {\n\t\tcase nil:\n\t\tdefault:\n\t\t\tnote(r)\n\t\t}\n\t}()\n\twork()\n}()",
			guarded: true,
		},
		{
			name:    "a recover the runtime would never reach, behind a condition that is never true",
			body:    "go func() {\n\tdefer func() {\n\t\tif false {\n\t\t\trecover()\n\t\t}\n\t}()\n\twork()\n}()",
			guarded: false,
		},
		{
			name:    "a recover in a loop body, which is a block below the top level",
			body:    "go func() {\n\tdefer func() {\n\t\tfor {\n\t\t\trecover()\n\t\t}\n\t}()\n\twork()\n}()",
			guarded: false,
		},
		{
			name:    "the word recover in a comment where the old window would have read it",
			body:    "go func() {\n\t// nothing here can panic, so no recover()\n\twork()\n}()",
			guarded: false,
		},
		{
			name:    "a named function with no literal to open",
			body:    "go work()",
			guarded: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sites, err := scanSpawns([]byte("package fixture\n\nfunc spawn() {\n\t" + tc.body + "\n}\n"))
			if err != nil {
				t.Fatalf("fixture does not parse: %v", err)
			}
			if len(sites) != 1 {
				t.Fatalf("fixture holds %d spawns, want exactly one", len(sites))
			}
			if sites[0].guarded != tc.guarded {
				t.Fatalf("guarded = %v, want %v for:\n%s", sites[0].guarded, tc.guarded, tc.body)
			}
		})
	}
}

// spawnSite is one `go` statement: the line it starts on, its first line of
// source text, which is how the allowlist names it and how the failure reads,
// and whether the goroutine opens with a guard.
type spawnSite struct {
	line      int
	statement string
	guarded   bool
}

// scanSpawns parses one file and reads every `go` statement in it. It works on
// the syntax alone, for the same reason scanLocks does: a sweep that needed
// types would need the tree to build, and this one runs on code that may not.
func scanSpawns(source []byte) ([]spawnSite, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", source, 0)
	if err != nil {
		return nil, err
	}
	var sites []spawnSite
	ast.Inspect(file, func(node ast.Node) bool {
		spawn, ok := node.(*ast.GoStmt)
		if !ok {
			return true
		}
		from, to := fset.Position(spawn.Pos()).Offset, fset.Position(spawn.End()).Offset
		statement, _, _ := strings.Cut(string(source[from:to]), "\n")
		sites = append(sites, spawnSite{
			line:      fset.Position(spawn.Pos()).Line,
			statement: strings.TrimSpace(statement),
			guarded:   spawnGuarded(spawn),
		})
		return true
	})
	return sites, nil
}

// spawnGuarded is the rule. A `go` statement is guarded when it spawns
// guard.Go itself, or when its function literal registers a recover in its
// OPENING: the run of statements before the first one that does any work,
// where work is any statement that makes a call. A recover is `defer
// guard.Recover(...)` or a deferred literal that calls recover() in its own
// body.
//
// The opening may be any length, and comments do not count, because a recover
// has to be registered before the work and is otherwise free to sit behind
// other defers — plan.go's spine opener needs its recover registered LAST so it
// runs first and settles the error a sibling defer then reads — or behind a
// flag the recover will read, which is the `landed := false` in spine.go. A
// recover registered after a call has been made protects only what came after
// it, so a defer that follows one is not read at all. A defer is registration
// rather than a call, and is read for its recover and then stepped over.
func spawnGuarded(spawn *ast.GoStmt) bool {
	if guardCall(spawn.Call, "Go") {
		return true
	}
	literal, ok := spawn.Call.Fun.(*ast.FuncLit)
	if !ok {
		return false
	}
	for _, statement := range literal.Body.List {
		deferred, isDefer := statement.(*ast.DeferStmt)
		if isDefer && recovers(deferred.Call) {
			return true
		}
		if !isDefer && calls(statement) {
			return false
		}
	}
	return false
}

// calls reports whether a statement makes any call at all. It is the sweep's
// whole notion of work: a panic in a goroutine's own opening arrives through a
// call, and a statement that makes none — a flag, a counter, a zero value — is
// set-up the recover may follow.
func calls(statement ast.Stmt) bool {
	found := false
	ast.Inspect(statement, func(node ast.Node) bool {
		if _, isCall := node.(*ast.CallExpr); isCall {
			found = true
		}
		return !found
	})
	return found
}

// recovers reports whether a deferred call absorbs a panic: it is the guard's
// own Recover, or a literal that calls the builtin at the TOP LEVEL of its own
// body — as a statement of its own, as the right-hand side of an assignment, or
// in the header of a top-level `if` or `switch`, which is where the house shape
// `if r := recover(); r != nil { … }` puts it (internal/plan/plan.go and
// spine.go both write it that way). A header is the init and the condition of an
// `if`, and the init and the tag — or the type-switch assignment — of a
// `switch`: `switch r := recover(); r {` is as real a guard as the `if`, and the
// runtime honours it for the same reason, so the rule reads both rather than
// picking a favourite spelling.
//
// The rule is that narrow because a recover the runtime honours is one the
// deferred function reaches unconditionally while the panic is running, and
// anywhere else the word appears is a recover that may never be called at all:
// `if false { recover() }` and `for { recover() }` both read as guards to a
// checker that only asks whether the identifier is present, and neither stops
// anything. A recover inside a nested literal is not counted either, for the
// same reason from the other direction — it is called by the inner function
// rather than by the deferred one, and does not stop the panic.
func recovers(call *ast.CallExpr) bool {
	if guardCall(call, "Recover") {
		return true
	}
	literal, ok := call.Fun.(*ast.FuncLit)
	if !ok {
		return false
	}
	for _, statement := range literal.Body.List {
		switch shape := statement.(type) {
		case *ast.ExprStmt:
			if callsRecover(shape.X) {
				return true
			}
		case *ast.AssignStmt:
			if callsRecover(shape) {
				return true
			}
		case *ast.IfStmt:
			if header(shape.Init, shape.Cond) {
				return true
			}
		case *ast.SwitchStmt:
			if header(shape.Init, shape.Tag) {
				return true
			}
		case *ast.TypeSwitchStmt:
			if header(shape.Init, shape.Assign) {
				return true
			}
		}
	}
	return false
}

// header reads the two slots a branching statement evaluates before it branches,
// either of which may be nil. Both run unconditionally when the statement is
// reached, which is what makes a recover in one of them a recover the runtime
// honours, and neither is a block.
func header(parts ...ast.Node) bool {
	for _, part := range parts {
		// An absent init or tag arrives as a nil node, and ast.Inspect panics
		// on one rather than ignoring it.
		if part == nil {
			continue
		}
		if callsRecover(part) {
			return true
		}
	}
	return false
}

// callsRecover reports whether this fragment of syntax calls the builtin
// itself. It does not descend into a function literal, because a recover that
// belongs to an inner function is that function's, not the deferred one's.
func callsRecover(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(node ast.Node) bool {
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		if inner, isCall := node.(*ast.CallExpr); isCall {
			if name, isIdent := inner.Fun.(*ast.Ident); isIdent && name.Name == "recover" {
				found = true
			}
		}
		return !found
	})
	return found
}

// guardCall reports a call spelled `guard.<method>(...)`.
func guardCall(call *ast.CallExpr, method string) bool {
	chosen, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, isIdent := chosen.X.(*ast.Ident)
	return isIdent && pkg.Name == "guard" && chosen.Sel.Name == method
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the guard package")
		}
		dir = parent
	}
}
