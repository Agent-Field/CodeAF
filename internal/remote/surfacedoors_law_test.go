package remote

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ── THE LAW: A DOOR THE ENGINE HAS EITHER CROSSES THE WIRE OR IS WRITTEN DOWN ─
//
// internal/tui3 reaches for almost everything through an OPTIONAL interface —
// `a.agent.(typingAgent)`, `a.agent.(memoryAgent)` — and that is the right
// shape: a capability with nothing behind it must be ABSENT rather than present
// and failing (the design law in CLAUDE.md). What it cannot do is tell you when
// the thing behind it went missing, because a failed assertion looks exactly
// like a capability that was never meant to be there.
//
// AND THE DEFAULT ROAD IS THE REMOTE ONE. A bare `codeaf` holds a
// `*remote.Agent` talking to a detached engine — `v3TakeHostRoad` in
// cmd/codeaf/chatv3_local.go is true for every launch but `--no-host`,
// `--debug`, first-run setup and a hostless `--once`. So a door that
// `*session.Agent` has and `*remote.Agent` does not is a feature that works in
// every test and in nobody's terminal. [MethodTyping] was one: the probe that
// exists so a think-pause does not cost a TLS handshake was wired to a keystroke
// the default road could not deliver, and the whole mechanism was dead where
// people actually run.
//
// SO THE GAPS ARE A LEDGER AND THE LEDGER ONLY SHRINKS. Every door the local
// engine has and the wire does not must be named below with the reason it has
// not crossed yet; a door that DOES cross must be deleted from the list; and
// the count is ratcheted, so the next gap cannot be added silently. This is the
// known-red discipline (internal/ci), applied to the one asymmetry that has
// twice shipped a feature nobody could reach.

// absentDoor is one door the wire does not carry, and WHAT A PERSON MEETS
// BECAUSE OF IT — which is the half the first version of this ledger left out
// and the half that decides which of these gets fixed first.
//
// A capability that cannot work is ABSENT, NOT BROKEN (CLAUDE.md's design law),
// and about half of these obey it: the page, the section or the count is simply
// not drawn, and a person meets nothing. The other half do the inverse. The
// surface's `a.hosted()` guard is `a.host != ""` and is set only on the
// `--host <dest>` road, so on a BARE `codeaf` — the default launch, and the one
// that holds a `*remote.Agent` — every one of those guards is skipped, the local
// branch runs, and the person is told a reason that is not the reason. `/remember`
// says memory is switched off. `/land` says nothing is waiting. `/autonomy` says
// the conversation has no project. All three are false, and all three send a
// person to a settings page that will not help them.
type absentDoor struct {
	// says is the sentence a person meets, quoted as the code spells it, and
	// EMPTY WHERE THE DOOR IS SILENTLY ABSENT. Empty is the law being obeyed;
	// a sentence is the law being inverted, and those are the ones to land
	// first.
	says string
	// loses is what does not work.
	loses string
}

// doorsThatHaveNotCrossed is every optional door internal/tui3 asserts, that
// `*session.Agent` answers and `*remote.Agent` does not, with what a person on
// the default road meets instead. NEVER ADD A LINE. Landing a door removes one,
// and [surfaceDoorLedger] comes down in the same commit.
//
// THEY ARE A FINDING AND NOT A DESIGN. Nothing on this list was decided to be
// local-only; each is a door somebody wired to `*session.Agent` and nobody
// carried across. Writing them down is what turns a silence into a number
// somebody can watch fall, and #901 is the public account of it — the same
// split, grouped into the five families they would actually be built in, with
// the ruling it asks for first: a family at a time, not a door at a time.
var doorsThatHaveNotCrossed = map[string]absentDoor{
	// ── SAYS SOMETHING THAT IS NOT TRUE. Land these first.
	"memoryAgent": {
		says:  "memory is off for this session · turn it on under /settings",
		loses: "/remember, /forget, /memories, /memory <query> and the memory place — memory is not off, it is unreachable",
	},
	"folderLander": {
		says:  "nothing is waiting · what this conversation writes in the folder it is standing in is already there",
		loses: "/land; and the `changes for … · /land` row above the box goes quiet too",
	},
	"subharnessAgent": {
		says:  "no subharnesses here yet — a subharness is a saved program for work that comes round again.",
		loses: "the subharness list, its intake form and running one",
	},
	"orchAgent": {
		says:  "adaptive runs unavailable — this session has no orchestrator",
		loses: "the orchestration room: its snapshot, a node's journal, steering it, answering it",
	},
	"harnessRunner": {
		says:  "harnesses are unavailable here",
		loses: "running a harness the picker offered",
	},
	"standingHereAgent": {
		says:  "this window cannot change it",
		loses: "the standing page's four doors — what holds here, an exception, standing one down, pausing one; the page draws only the elsewhere shelf",
	},
	"taskRoomAgent": {
		says:  "this session has no task rooms",
		loses: "watching one task and reading its journal; the room's steer door crosses and its two reading doors do not, so a room falls back to a read-only far reading, and a room the roster does not know meets this sentence with `· say it to main` on the end of it",
	},

	// ── SILENTLY ABSENT, which is the law obeyed. Still missing, still owed.
	"abandonAgent":   {loses: "abandoning a turn's second stage; the surface falls back to an ordinary stop"},
	"elsewhereAgent": {loses: "the work this conversation started that is running somewhere else"},
	"leavableRunner": {loses: "watching orchestration runs, which leaves the run page dark"},
	"leavableWaker":  {loses: "watching wakes, which leaves a woken turn unannounced"},

	"promoteAgent":                    {loses: "promoting a call out of the background"},
	"runAgent":                        {loses: "listing orchestration runs"},
	"spellOutAgent":                   {loses: "spelling a reply out again in longer form"},
	"standingCountAgent":              {loses: "the `◦ n standing orders` count the margin draws; there is no section at all"},
	"taskMentionAgent":                {loses: "the task index an @-mention completes from, where the far reading is nil too"},
	"taskWeightDoor":                  {loses: "one task's context tokens (the conversation's own ContextTokens crosses; the task's does not)"},
	"turnResumer":                     {loses: "resuming a turn that was stopped"},
	"wakeAgent":                       {loses: "reading wakes"},
	"interface{ LandingFor/1/2 }":     {loses: "the landing a folder already has, beside folderLander"},
	"interface{ PendingConsent/0/1 }": {loses: "which approvals are still open when a surface detaches"},
}

// surfaceDoorLedger is the ratchet: the ledger above may shrink and may never
// grow, and shrinking it without lowering this number in the same commit is a
// red as well ([ratchetComplaint]).
const surfaceDoorLedger = 21

// TestEverySurfaceDoorTheEngineHasCrossesTheWire is the law above.
func TestEverySurfaceDoorTheEngineHasCrossesTheWire(t *testing.T) {
	root := repoRoot(t)
	set := token.NewFileSet()

	surfaceInterfaces := interfacesIn(t, set, filepath.Join(root, "internal", "tui3"))
	sessionInterfaces := interfacesIn(t, set, filepath.Join(root, "internal", "session"))
	known := map[string]map[string]bool{}
	for name, methods := range sessionInterfaces {
		known[name] = methods
	}
	for name, methods := range surfaceInterfaces {
		// The surface's own spelling wins where both packages declare a name:
		// the assertion sites being read are its, so its declarations are the
		// ones they mean.
		known[name] = methods
	}
	known = resolveEmbedded(known)

	sessionDoors := methodsOfAgent(t, set, filepath.Join(root, "internal", "session"))
	remoteDoors := methodsOfAgent(t, set, filepath.Join(root, "internal", "remote"))

	gaps := map[string][]string{}
	for _, asserted := range agentAssertions(t, set, filepath.Join(root, "internal", "tui3"), known) {
		if len(asserted.methods) == 0 {
			continue
		}
		if !holds(sessionDoors, asserted.methods) {
			// A DOOR THE LOCAL ENGINE DOES NOT HAVE EITHER IS NOT AN ASYMMETRY.
			// The surface is reaching for something nothing implements — a test
			// double, an engine that has not been written — and the wire owes it
			// nothing.
			continue
		}
		if holds(remoteDoors, asserted.methods) {
			continue
		}
		gaps[asserted.name] = missing(remoteDoors, asserted.methods)
	}

	for name, absent := range gaps {
		if _, written := doorsThatHaveNotCrossed[name]; !written {
			t.Errorf("internal/tui3 asserts %s, the engine answers it, and *remote.Agent does not (missing %s).\n"+
				"A door the engine has must cross the wire, or be written into doorsThatHaveNotCrossed with the reason it cannot yet.",
				name, strings.Join(absent, ", "))
		}
	}
	for name := range doorsThatHaveNotCrossed {
		if _, still := gaps[name]; !still {
			t.Errorf("doorsThatHaveNotCrossed still lists %s, which now crosses the wire. Delete the line and lower surfaceDoorLedger.", name)
		}
	}
	// THE RATCHET FAILS IN BOTH DIRECTIONS, which is the whole of what makes it
	// one. internal/ci's own says the second half out loud and it is the half
	// that is easy to leave off: a ratchet left slack would let the next change
	// put an entry back with the gate green throughout. So a ledger that has
	// SHRUNK is a red too, until the number under it comes down in the same
	// commit.
	if got := len(doorsThatHaveNotCrossed); got != surfaceDoorLedger {
		t.Error(ratchetComplaint("doorsThatHaveNotCrossed", got, surfaceDoorLedger))
	}
	if _, listed := doorsThatHaveNotCrossed["typingAgent"]; listed {
		t.Error("typingAgent is the door this law was written for and it crosses the wire now (typing.go)")
	}
	// AND EVERY ENTRY SAYS WHAT IT COSTS. A line with no `loses` is a name
	// somebody wrote down and did not look at, which is the ledger becoming the
	// silence it was written to end.
	surface := surfaceProse(t, filepath.Join(root, "internal", "tui3"))
	for name, door := range doorsThatHaveNotCrossed {
		if strings.TrimSpace(door.loses) == "" {
			t.Errorf("%s is on the ledger with nothing said about what a person loses by it", name)
		}
		// AND A QUOTED SENTENCE IS ONE THE SURFACE ACTUALLY SPELLS. This is what
		// keeps the silent/false split a fact rather than a note: respell the
		// string, or delete the branch that prints it, and the entry that claims
		// a person meets it fails here instead of quietly becoming fiction.
		if door.says != "" && !strings.Contains(surface, door.says) {
			t.Errorf("%s is on the ledger as saying %q and internal/tui3 no longer spells that.\n"+
				"Either quote what it says now, or — if the sentence is gone — this door may have become silently absent, which is a different entry.",
				name, door.says)
		}
	}
}

// surfaceProse is internal/tui3's own source as one blob, for the one question
// the ledger asks of it: does the surface still spell this sentence.
func surfaceProse(t *testing.T, dir string) string {
	t.Helper()
	var prose strings.Builder
	listing, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range listing {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		prose.Write(body)
	}
	return prose.String()
}

// TestEveryStreamOpenerIsOrdered refuses a method that names a stream and is
// not ordered. [server.pending] is one slot with no lock, and the whole of what
// makes that safe is that one goroutine owns it ([callClass.opensAStream]); a
// stream opened from any other class would be racing it.
func TestEveryStreamOpenerIsOrdered(t *testing.T) {
	root := repoRoot(t)
	opened := map[string]bool{}
	walkGo(t, filepath.Join(root, "internal", "remote"), func(path string, file *ast.File) {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(function, func(node ast.Node) bool {
				// A method is a stream opener when its body mentions the one
				// slot or the one helper that writes it.
				switch shape := node.(type) {
				case *ast.SelectorExpr:
					if shape.Sel.Name == "pending" || shape.Sel.Name == "stream" {
						for _, name := range methodNamesIn(function) {
							opened[name] = true
						}
					}
				}
				return true
			})
		}
	})
	for method := range opened {
		if !classify(method).opensAStream() {
			t.Errorf("%s names a stream from a class that may not: it would race server.pending", method)
		}
	}
	// And the classification itself must not have drifted empty.
	for _, method := range []string{MethodSubmit, MethodSubmitImage, MethodSubmitFiles, MethodFollowUp, MethodSteer, MethodObserve} {
		if !classify(method).opensAStream() {
			t.Errorf("%s opens a stream and must be ordered", method)
		}
	}
	// AND NO CLASS AT ALL RUNS ON THE GOROUTINE THAT READS THE SOCKET. The road
	// type has no third value, so this is a statement about the two it has
	// rather than a check somebody could forget to extend.
	for _, class := range []callClass{classOrdered, classGetter, classAct} {
		if class.road() != inOrder && class.road() != onItsOwn {
			t.Errorf("a class found a road that is not one of the two")
		}
	}
}

// ── the tree readers ────────────────────────────────────────────────────────

func repoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the surface-door law")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
}

func walkGo(t *testing.T, dir string, visit func(string, *ast.File)) {
	t.Helper()
	set := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != dir {
				// THE WALK IS ONE PACKAGE DEEP AND SAYS SO WHEN THAT STOPS
				// BEING ENOUGH. internal/tui3 is flat today; a sub-package
				// appearing under it would take its assertions off this law
				// silently, so the day one lands is a red rather than a
				// coverage hole nobody meets.
				if sub, _ := filepath.Glob(filepath.Join(path, "*.go")); len(sub) > 0 {
					t.Errorf("%s has grown a sub-package with Go in it; this law walks one package deep", path)
				}
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return err
		}
		visit(path, file)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// interfacesIn is every interface declared in one package, as its method names
// plus the names of the interfaces it embeds (resolved afterwards).
func interfacesIn(t *testing.T, _ *token.FileSet, dir string) map[string]map[string]bool {
	found := map[string]map[string]bool{}
	walkGo(t, dir, func(_ string, file *ast.File) {
		for _, decl := range file.Decls {
			general, ok := decl.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typed, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				shape, ok := typed.Type.(*ast.InterfaceType)
				if ok {
					found[typed.Name.Name] = interfaceMethods(shape)
					continue
				}
				// An alias is still the surface declaration an assertion names.
				// Following its selected interface keeps this law effective when a
				// capability moves to one shared package instead of duplicating methods.
				if typed.Assign.IsValid() {
					if selected, selectedOK := typed.Type.(*ast.SelectorExpr); selectedOK {
						found[typed.Name.Name] = map[string]bool{"embed:" + selected.Sel.Name: true}
					}
				}
			}
		}
	})
	return found
}

// interfaceMethods is a method set keyed by the name and shape of each method,
// with an embedded interface carried through as its own name for
// [resolveEmbedded] to expand.
func interfaceMethods(shape *ast.InterfaceType) map[string]bool {
	methods := map[string]bool{}
	if shape.Methods == nil {
		return methods
	}
	for _, field := range shape.Methods.List {
		if len(field.Names) == 0 {
			if name, ok := field.Type.(*ast.Ident); ok {
				methods["embed:"+name.Name] = true
			}
			if selected, ok := field.Type.(*ast.SelectorExpr); ok {
				methods["embed:"+selected.Sel.Name] = true
			}
			continue
		}
		function, ok := field.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		for _, name := range field.Names {
			methods[signature(name.Name, function)] = true
		}
	}
	return methods
}

// signature is the one spelling a method is compared by: its name and how many
// things go in and come out.
//
// IT IS DELIBERATELY NOT THE TYPES. The same method is written `Answer` in
// internal/session and `session.Answer` in internal/remote, and a comparison
// that read the spelling would report every door as missing. What this catches
// is a door that is absent or has the wrong shape, which is the whole of the
// class.
func signature(name string, function *ast.FuncType) string {
	in, out := 0, 0
	if function.Params != nil {
		for _, field := range function.Params.List {
			in += max(1, len(field.Names))
		}
	}
	if function.Results != nil {
		for _, field := range function.Results.List {
			out += max(1, len(field.Names))
		}
	}
	return name + "/" + strconv.Itoa(in) + "/" + strconv.Itoa(out)
}

// resolveEmbedded expands every `embed:Name` into the method set of the
// interface it names, repeatedly, so an interface built out of two others
// carries both.
func resolveEmbedded(known map[string]map[string]bool) map[string]map[string]bool {
	for range 8 {
		settled := true
		for name, methods := range known {
			for entry := range methods {
				embedded, is := strings.CutPrefix(entry, "embed:")
				if !is {
					continue
				}
				delete(methods, entry)
				settled = false
				for inherited := range known[embedded] {
					methods[inherited] = true
				}
			}
			known[name] = methods
		}
		if settled {
			break
		}
	}
	return known
}

type assertion struct {
	name    string
	methods map[string]bool
}

// agentAssertions is every `…agent.(T)` in the surface, as the method set the
// surface is asking for.
func agentAssertions(t *testing.T, _ *token.FileSet, dir string, known map[string]map[string]bool) []assertion {
	var found []assertion
	seen := map[string]bool{}
	walkGo(t, dir, func(where string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			asserted, ok := node.(*ast.TypeAssertExpr)
			if !ok || asserted.Type == nil {
				return true
			}
			selected, ok := asserted.X.(*ast.SelectorExpr)
			if !ok || selected.Sel.Name != "agent" {
				return true
			}
			switch shape := asserted.Type.(type) {
			case *ast.Ident:
				methods, declared := known[shape.Name]
				if !declared {
					// AN ASSERTION THIS LAW CANNOT RESOLVE IS NOT ONE IT MAY
					// SKIP. Moving a door's interface into a sub-package would
					// otherwise take it off the law with the gate green
					// throughout, which is the evasion the ledger exists to
					// stop.
					t.Errorf("%s asserts %s on the agent and this law cannot find its declaration: "+
						"an optional door must be declared where the law can read it", filepath.Base(where), shape.Name)
					return true
				}
				if !seen[shape.Name] {
					seen[shape.Name] = true
					found = append(found, assertion{name: shape.Name, methods: methods})
				}
			case *ast.InterfaceType:
				methods := resolveEmbedded(map[string]map[string]bool{"inline": interfaceMethods(shape)})["inline"]
				name := "interface{ " + strings.Join(sorted(methods), "; ") + " }"
				if !seen[name] {
					seen[name] = true
					found = append(found, assertion{name: name, methods: methods})
				}
			}
			return true
		})
	})
	sort.Slice(found, func(i, j int) bool { return found[i].name < found[j].name })
	return found
}

// methodsOfAgent is every method on `*Agent` in one package.
func methodsOfAgent(t *testing.T, _ *token.FileSet, dir string) map[string]bool {
	doors := map[string]bool{}
	walkGo(t, dir, func(_ string, file *ast.File) {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Recv == nil || len(function.Recv.List) != 1 {
				continue
			}
			pointer, ok := function.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			name, ok := pointer.X.(*ast.Ident)
			if !ok || name.Name != "Agent" {
				continue
			}
			doors[signature(function.Name.Name, function.Type)] = true
		}
	})
	return doors
}

// methodNamesIn is every method name a function declaration belongs to, which
// for a method is its own name and for a plain function is nothing.
func methodNamesIn(function *ast.FuncDecl) []string {
	if function.Recv == nil {
		return nil
	}
	// The engine's doors are named by the wire constant the switch matches, not
	// by the Go method, so what this returns is the method's own name for the
	// few helpers that carry it (server.observe → MethodObserve).
	switch function.Name.Name {
	case "observe":
		return []string{MethodObserve}
	case "stream", "release", "dispatch", "serve":
		return nil
	}
	return nil
}

func holds(doors map[string]bool, wanted map[string]bool) bool {
	for method := range wanted {
		if !doors[method] {
			return false
		}
	}
	return true
}

func missing(doors map[string]bool, wanted map[string]bool) []string {
	var absent []string
	for method := range wanted {
		if !doors[method] {
			absent = append(absent, method)
		}
	}
	sort.Strings(absent)
	return absent
}

func sorted(methods map[string]bool) []string {
	names := make([]string, 0, len(methods))
	for method := range methods {
		names = append(names, method)
	}
	sort.Strings(names)
	return names
}
