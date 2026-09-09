package provider

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── THE FUNNEL LAWS ─────────────────────────────────────────────────────────
//
// Everything this build asks a model for goes through ONE funnel, and these
// four tests are what makes that sentence true rather than aspirational.
//
// The argument is the same one the lane registry and the response boundary are
// written to (docs/ARCHITECTURE.md Decisions 8 and 10), said about the wire. A
// second path to a chat endpoint is not merely duplicated code: it is a call
// that no lane choice steers, no watch measures, no sighting teaches, no role
// prices, no phase clock narrates and no call log records. It works perfectly
// on the day it is written and it is invisible for the rest of its life — and
// the surface then reports the machinery it CAN see as though it were the whole
// story, which is the defect the phase clock exists for (phase.go).
//
// So the four laws are:
//
//	(a) nothing outside this package talks to a completions or media endpoint
//	(b) exactly six functions in this package put a request on the wire, and
//	    exactly four read an event stream
//	(c) every role in the table has a call site that names it
//	(d) only the ladder's last rung changes the model a person asked for
//
// They are structural tests in this repo's convention — the sources are read
// with go/ast and held to a law rather than to a behaviour, as
// `internal/lane/structure_test.go` and this package's own lane_law_test.go do
// — and each failure names a file and a line.

// ── SHARED MACHINERY ────────────────────────────────────────────────────────

// funnelRepoRoot is the top of the checkout, found from this package's own
// directory rather than from a working directory a test runner chose.
func funnelRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("locate the repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("%s does not look like the repository root: %v", root, err)
	}
	return root
}

// funnelWalk parses every non-test .go file in the checkout that the law
// applies to and hands each one to visit, with the file's own bytes so a
// failure can quote the source it found.
//
// A file that does not parse is SKIPPED AND COUNTED rather than failed on.
// Several sessions work this tree at once and a half-written file in another
// lane is not this law's business; the count is reported so that a tree where
// nothing parsed cannot pass silently.
func funnelWalk(t *testing.T, root string, skipTree func(rel string) bool,
	visit func(rel string, fset *token.FileSet, file *ast.File, source []byte),
) (scanned, unparsed int) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "testdata" {
				return filepath.SkipDir
			}
			if rel != "." && skipTree != nil && skipTree(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		fset := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fset, rel, source, parser.ParseComments)
		if parseErr != nil {
			unparsed++
			t.Logf("skipped %s, which does not parse right now: %v", rel, parseErr)
			return nil
		}
		scanned++
		visit(rel, fset, parsed, source)
		return nil
	})
	if err != nil {
		t.Fatalf("walk the checkout: %v", err)
	}
	return scanned, unparsed
}

// funnelPackage parses every non-test .go file of THIS package, which is where
// laws (b) and (d) apply. Test files are exempt from both: a test double that
// stands up its own server or builds its own request is the whole point of the
// seam being a seam.
func funnelPackage(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files[name] = parsed
	}
	if len(files) == 0 {
		t.Fatal("no sources were found, so these laws would pass vacuously")
	}
	return fset, files
}

// funnelEnclosing is the name of the function a position sits inside, empty at
// package scope. It is what turns "there is a Do here" into "send does it",
// which is the only form of this law anybody can act on.
func funnelEnclosing(file *ast.File, pos token.Pos) string {
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if pos >= function.Pos() && pos <= function.End() {
			return function.Name.Name
		}
	}
	return ""
}

// funnelNames is a set of names, sorted, for a failure message that reads the
// same way twice.
func funnelNames(set map[string]bool) []string {
	list := make([]string, 0, len(set))
	for name := range set {
		list = append(list, name)
	}
	sort.Strings(list)
	return list
}

// funnelDiffer reports whether two name sets disagree.
func funnelDiffer(got map[string]bool, want []string) bool {
	if len(got) != len(want) {
		return true
	}
	for _, name := range want {
		if !got[name] {
			return true
		}
	}
	return false
}

// ── (a) NOTHING OUTSIDE THIS PACKAGE TALKS TO A MODEL ENDPOINT ──────────────

// funnelEndpointPaths are the paths that produce model tokens or model media.
//
// They are spelled as the wire spells them, and the media half is spelled BOTH
// ways the router accepts because both are used: `/v1/images` is the absolute
// form and `/audio/speech` the form a client appends to a base URL that already
// carries the version. A bare `/images` or `/videos` is deliberately NOT here —
// it is a path fragment common enough in a program that handles pictures to
// make this law cry wolf, and every real caller of one carries the longer form
// somewhere in the same expression.
var funnelEndpointPaths = []string{
	"/chat/completions",
	"/v1/images",
	"/images/generations",
	"/v1/audio",
	"/audio/speech",
	"/audio/transcriptions",
	"/v1/videos",
}

// funnelExemptTrees are the directories this law does not reach, each with the
// one line of law that says why.
var funnelExemptTrees = map[string]string{
	// This package IS the funnel. The endpoints are its whole job.
	"internal/provider": "the funnel itself",
	// lanestub IS the fake router: serving `/api/v1/chat/completions` is the
	// entire reason the package exists, and it is a separate package precisely
	// so that `internal/lane` can stay a package that opens no connection.
	"internal/lane/lanestub": "the fake router every lane test measures against",
	// Measuring rigs and stubs are not the product's send path. A bench that
	// went through the funnel would be measuring the funnel.
	"bench":     "a measuring rig, not a send path",
	"test":      "container stubs for the remote-access suite",
	"harnesses": "harness fixtures, run by the harness and not by this build",
}

// funnelKnownSecondTransports are the two files that DO reach an endpoint from
// outside this package, named one by one rather than exempted by directory.
//
// A DIRECTORY EXEMPTION WOULD LET THE THIRD ONE IN SILENTLY, which is the
// failure this whole law is about, so the debt is enumerated: any file not on
// this list fails the build, and removing one of these is what closing the debt
// looks like.
//
//   - cmd/harness-design/openrouter.go says in its own header why it is a flat
//     request/response rig rather than the adapter — it measures whether a MODEL
//     can architect a sub-harness, and a wrapper whose bugs are indistinguishable
//     from the model's would be measuring the wrong thing. Its own
//     TODO-consolidate names internal/subharness's exec_model.go as where it
//     should end up.
//   - internal/voice/transcriber.go posts to /audio/transcriptions on its own,
//     which internal/provider's transcribe.go now also does — attribution.go
//     says as much in as many words. It is a duplicate transport for the v1 and
//     v2 voice input, and it is the one on this list that is simply owed a
//     rewrite onto MediaClient.Transcribe.
var funnelKnownSecondTransports = map[string]string{
	"cmd/harness-design/openrouter.go": "the harness-design rig's deliberate flat transport",
	"internal/voice/transcriber.go":    "the v1/v2 voice input's own /audio/transcriptions post",
}

// TestNothingOutsideTheFunnelTalksToAModelEndpoint is law (a).
//
// It reads STRING LITERALS out of the AST rather than scanning lines, for a
// reason this package has been bitten by before: a line scan that strips
// comments by cutting at `//` cuts `https://openrouter.ai/api/v1/chat/completions`
// in half and finds nothing, while a scan that does not strip them fails on
// every file that merely MENTIONS the endpoint in prose — and this codebase
// comments heavily and deliberately. A BasicLit is neither: a comment never
// becomes one, and a constant assembled from a literal still carries it.
//
// The second half names the CALL rather than the constant, because a request
// built out of a base URL and a path appended somewhere else is a violation
// whose file and line a person needs, and the constant's line is not it.
func TestNothingOutsideTheFunnelTalksToAModelEndpoint(t *testing.T) {
	root := funnelRepoRoot(t)
	skip := func(rel string) bool {
		for tree := range funnelExemptTrees {
			if rel == tree || strings.HasPrefix(rel, tree+"/") {
				return true
			}
		}
		return false
	}
	hit := func(text string) string {
		for _, path := range funnelEndpointPaths {
			if strings.Contains(text, path) {
				return path
			}
		}
		return ""
	}
	scanned, _ := funnelWalk(t, root, skip, func(rel string, fset *token.FileSet, file *ast.File, source []byte) {
		known := funnelKnownSecondTransports[rel] != ""
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.BasicLit:
				if typed.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(typed.Value)
				if err != nil {
					return true
				}
				path := hit(text)
				if path == "" || known {
					return true
				}
				t.Errorf("%s names %s — every model endpoint is internal/provider's, and a second path to one is a call no lane choice steers, no watch measures and no log records",
					fset.Position(typed.Pos()), path)
			case *ast.CallExpr:
				// An `http.NewRequest…` whose URL expression mentions a path,
				// reported at the request rather than at the string.
				selector, ok := typed.Fun.(*ast.SelectorExpr)
				if !ok || !strings.HasPrefix(selector.Sel.Name, "NewRequest") {
					return true
				}
				pkg, ok := selector.X.(*ast.Ident)
				if !ok || pkg.Name != "http" || known {
					return true
				}
				for _, argument := range typed.Args {
					offset := fset.Position(argument.Pos()).Offset
					end := fset.Position(argument.End()).Offset
					if offset < 0 || end > len(source) || end <= offset {
						continue
					}
					if path := hit(string(source[offset:end])); path != "" {
						t.Errorf("%s builds a request for %s outside internal/provider",
							fset.Position(typed.Pos()), path)
						break
					}
				}
			}
			return true
		})
	})
	if scanned < 100 {
		t.Fatalf("only %d sources were scanned, so this law passed vacuously", scanned)
	}
	// And the exemptions are about something rather than about an empty set: a
	// file that stops reaching an endpoint should come OFF the list, and a list
	// naming a file that no longer exists is a law with a hole in it nobody can
	// see.
	for rel, why := range funnelKnownSecondTransports {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("%s is exempted as %q and is not there any more — take it off the list", rel, why)
		}
	}
}

// ── (b) ONE FUNCTION PUTS A COMPLETION ON THE WIRE ──────────────────────────

// funnelWireSenders is every function in this package that hands a request to
// an http.Client, and there are six because there are six kinds of thing this
// adapter fetches.
//
//	send         every chat completion, and the only one with the retry loop,
//	             the limiter, the backoff and the call log around it
//	probeLane    the one-token measurement, deliberately outside all of that:
//	             a probe that was retried would time the retry (probe.go)
//	doEndpoint   MediaClient's images, speech, video and transcription posts,
//	             which are request/response and carry no stream at all
//	Fetch        sheetFetcher's GET of the lane sheet, which is not a model call
//	             — it is the belief the choice is made from (lanes.go)
//	fetchReceipt the bounded background GET for a cut stream's exact generation
//	             receipt; it creates no model work and never runs on the turn
//
// probeConnection is a credential-free HEAD of the configured origin. Its
// contract tests forbid a prompt, a request body or redirect following.
var funnelWireSenders = []string{"Fetch", "doEndpoint", "fetchReceipt", "probeConnection", "probeLane", "send"}

// funnelSendCallers is every function that reaches [Client.send].
//
//	sendRepaired   the ordinary path: encode, send, repair a 400 we caused
//	resend         the repaired retry, which the 400 made free
//	attemptShaped  one rung of the endpoint-refusal ladder
//	ParseDocument  the file-parser completion, which is a completion in every
//	               respect but rides its own wire shape (document.go)
//
// [Client.sendShaped] is not here because it does not call send: it calls
// sendRecovered, which calls sendRepaired. The law is about the SET being
// closed, not about the depth — a fifth caller would be a fifth place the
// endpoint pin, the ladder and the log could be skipped.
var funnelSendCallers = []string{"ParseDocument", "attemptShaped", "resend", "sendRepaired"}

// funnelStreamReaders is every function that runs an SSE decode loop.
//
//	completeWithMessagesStreaming  the guarded chat stream: the watch, the
//	                               phase clock, the stall wall and the hedge
//	StreamComplete                 the one-prompt stream internal/router hands
//	                               out; it cascades nothing because a stream is
//	                               committed at its first byte (router.go)
//	probeLane                      reads exactly far enough to see one token
//	readMusicStream                music is composed through chat completions
//	                               and arrives as an event stream (music.go)
var funnelStreamReaders = []string{"StreamComplete", "completeWithMessagesStreaming", "probeLane", "readMusicStream"}

// TestOneFunctionSendsACompletionOnTheWire is law (b).
//
// Each of the three sets below is asserted to be EXACTLY what it is rather than
// merely to contain what it should. A law that only forbade new members would
// pass forever on a function that quietly stopped being reachable, and this
// package carried one of those until the wave that wrote this test:
// `Client.completeOnce` — a whole second request/response completion path — had
// no caller at all, test files included, and was deleted rather than exempted.
func TestOneFunctionSendsACompletionOnTheWire(t *testing.T) {
	fset, files := funnelPackage(t)

	// Package-level function names, so that a `sync.Once.Do(buildTransports)`
	// can be told from an `http.Client.Do(request)` by what it is handed. That
	// is the honest distinction between the two: one is given work to run once
	// and the other is given a request to send.
	declared := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			if function, ok := decl.(*ast.FuncDecl); ok {
				declared[function.Name.Name] = true
			}
		}
	}

	senders := map[string]bool{}
	callers := map[string]bool{}
	readers := map[string]bool{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				switch fun.Sel.Name {
				case "Do":
					if len(call.Args) != 1 {
						return true
					}
					if _, isFunc := call.Args[0].(*ast.FuncLit); isFunc {
						return true
					}
					if ident, isIdent := call.Args[0].(*ast.Ident); isIdent && declared[ident.Name] {
						return true
					}
					where := funnelEnclosing(file, call.Pos())
					senders[where] = true
					if where == "" {
						t.Errorf("%s sends a request at package scope", fset.Position(call.Pos()))
					}
				case "send":
					callers[funnelEnclosing(file, call.Pos())] = true
				}
			case *ast.Ident:
				if fun.Name == "newSSEDecoder" {
					readers[funnelEnclosing(file, call.Pos())] = true
				}
			}
			return true
		})
	}

	if funnelDiffer(senders, funnelWireSenders) {
		t.Errorf("the functions that put a request on the wire are %v; the law names %v — an unnamed sender is a request whose bounds and record are unknown",
			funnelNames(senders), funnelWireSenders)
	}
	if funnelDiffer(callers, funnelSendCallers) {
		t.Errorf("the functions that reach Client.send are %v; the law names %v — every one of them is a place the endpoint pin, the refusal ladder and the log can be skipped",
			funnelNames(callers), funnelSendCallers)
	}
	if funnelDiffer(readers, funnelStreamReaders) {
		t.Errorf("the functions that read an event stream are %v; the law names %v — a stream read anywhere else has no watch on it, no phase clock and no stall wall",
			funnelNames(readers), funnelStreamReaders)
	}
}

// ── (c) EVERY ROLE IN THE TABLE IS EXERCISED ────────────────────────────────

// TestEveryRoleInTheTableHasACallSite is law (c), and it holds the table honest
// in both directions at once.
//
// A ROLE WITHOUT A CALL SITE IS A NUMBER NOBODY IS USING. `internal/lane`'s
// roles.go is where the four routing numbers live, and its whole claim is that
// a call site names a role instead of naming a λ, a quality bar and a horizon
// of its own. A role added to the table and wired to nothing looks in review
// exactly like a role that is in force everywhere — the table reads complete —
// while the calls it was written for go on reaching past it. Equally, a call
// site deleted without its role leaves a row in the table that documents a
// decision this build no longer makes.
//
// [lane.RoleUnknown] is the one exemption, and it is exempt because it is the
// opposite of a call site: it is what a context that named NOTHING reads as, so
// requiring somebody to write it down would be requiring the absence to be
// spelled out. It has a row in the table for the conservative default it
// supplies and no caller by design.
func TestEveryRoleInTheTableHasACallSite(t *testing.T) {
	root := funnelRepoRoot(t)

	// The identifier each role value is spelled with, read out of the table
	// itself. Deriving it from the value ("leaf.attached" → RoleLeafAttached)
	// would be a second spelling of the same fact, and the first time the two
	// disagreed this law would pass by looking for a name nobody wrote.
	fset := token.NewFileSet()
	table, err := parser.ParseFile(fset, filepath.Join(root, "internal", "lane", "roles.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse the roles table: %v", err)
	}
	spelled := map[lanes.Role]string{}
	ast.Inspect(table, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 1 {
			return true
		}
		if named, ok := spec.Type.(*ast.Ident); !ok || named.Name != "Role" {
			return true
		}
		literal, ok := spec.Values[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		spelled[lanes.Role(value)] = spec.Names[0].Name
		return true
	})

	// Every role selector written anywhere in the build outside the table's own
	// package.
	//
	// It is a SELECTOR AND NOT A TEXT MATCH so that a role named in a comment —
	// which is where a role goes to be discussed rather than used — does not
	// count as a call site. And the selector has to be qualified by THIS
	// package: `internal/effort` and `internal/roles` both have their own Role
	// constants, several of them spelled identically (RoleStanding, RoleDesign,
	// RoleJudge), and a law satisfied by an unrelated package's enum would be a
	// law that passes on a build where nothing routes by role at all.
	written := map[string]bool{}
	skip := func(rel string) bool { return rel == "internal/lane" || strings.HasPrefix(rel, "internal/lane/") }
	scanned, _ := funnelWalk(t, root, skip, func(rel string, fset *token.FileSet, file *ast.File, source []byte) {
		local := funnelLaneImport(file)
		if local == "" {
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if base, ok := selector.X.(*ast.Ident); ok && base.Name == local {
				written[selector.Sel.Name] = true
			}
			return true
		})
	})
	if scanned < 100 {
		t.Fatalf("only %d sources were scanned, so this law passed vacuously", scanned)
	}

	for _, role := range lanes.Roles() {
		if role == lanes.RoleUnknown {
			continue
		}
		identifier, ok := spelled[role]
		if !ok {
			t.Errorf("role %q is in the table returned by lane.Roles() and is not declared as a constant in internal/lane/roles.go", role)
			continue
		}
		if !written[identifier] {
			t.Errorf("no call site names lane.%s — a role with nobody asking for it is four routing numbers this build never uses, so either a caller is reaching past the table or the row is owed a deletion",
				identifier)
		}
	}
}

// funnelLaneImport is the name `internal/lane` is spelled by inside one file,
// empty when the file does not import it at all. Two spellings are in use —
// `lane` where a package reads the table and `lanes` where the identifier would
// otherwise collide, as in this package — and both are the same import, so the
// alias is read rather than assumed.
func funnelLaneImport(file *ast.File) string {
	const path = `"github.com/Agent-Field/aforge-v2/internal/lane"`
	for _, imported := range file.Imports {
		if imported.Path.Value != path {
			continue
		}
		if imported.Name != nil {
			return imported.Name.Name
		}
		return "lane"
	}
	return ""
}

// ── (d) ONLY THE LADDER CHANGES THE MODEL ───────────────────────────────────

// funnelModelSetters are the functions that may put a model on an ai.Request.
//
// Three BUILD one — the encoder for an ordinary call, the probe for its own
// deliberately-bare body, and the document parser for the metadata its wire
// shape is recorded under — and each does it once, at the moment the request
// comes into existence.
var funnelModelSetters = []string{"ParseDocument", "newRequest", "probeLane"}

// funnelModelChangers are the functions that may change the model on a request
// that already has one, and they are rung four.
var funnelModelChangers = []string{"recoverFromPacing", "recoverFromRefusal"}

// TestOnlyTheLadderChangesTheModel is law (d).
//
// RUNG FOUR IS THE ONLY RUNG THAT CHANGES WHAT A PERSON ASKED FOR, and it comes
// after every lane of the asked-for model has been tried. The rungs before it
// all relax the SHAPE of the request — the endpoint filter, the price ceiling,
// the reasoning knob, the attachments — and a person who asked for a model is
// still being answered by it. Rung four is a different answer written by a
// different model, which is why it has a phase word of its own
// ([PhaseSwitchingModel]) instead of sharing [PhaseSwitching]'s.
//
// A model swapped anywhere else would be that same substitution made silently:
// no notice, no phase, no row saying which model actually wrote the reply, and
// an attribution line naming the model that did not. The two rungs here both
// write the winning model back onto the caller's request precisely so that
// nothing downstream has to guess.
func TestOnlyTheLadderChangesTheModel(t *testing.T) {
	fset, files := funnelPackage(t)
	built := map[string]bool{}
	changed := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			requests := funnelRequestIdents(function)
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.CompositeLit:
					if !funnelIsAIRequest(typed.Type) {
						return true
					}
					for _, element := range typed.Elts {
						pair, ok := element.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						if key, ok := pair.Key.(*ast.Ident); ok && key.Name == "Model" {
							built[function.Name.Name] = true
						}
					}
				case *ast.AssignStmt:
					for _, target := range typed.Lhs {
						selector, ok := target.(*ast.SelectorExpr)
						if !ok || selector.Sel.Name != "Model" {
							continue
						}
						base, ok := selector.X.(*ast.Ident)
						if !ok || !requests[base.Name] {
							continue
						}
						changed[function.Name.Name] = true
						if !funnelIn(funnelModelChangers, function.Name.Name) {
							t.Errorf("%s changes the model on a request that already had one — rung four of the ladder is the only rung that may, and it is the only one a person is told about",
								fset.Position(selector.Pos()))
						}
					}
				}
				return true
			})
		}
	}
	if funnelDiffer(built, funnelModelSetters) {
		t.Errorf("the functions that build an ai.Request with a model on it are %v; the law names %v",
			funnelNames(built), funnelModelSetters)
	}
	if funnelDiffer(changed, funnelModelChangers) {
		t.Errorf("the functions that change a request's model are %v; the law names %v",
			funnelNames(changed), funnelModelChangers)
	}
}

// funnelRequestIdents is every name inside one function that holds an
// ai.Request.
//
// It is a local reading and not a type check on purpose: these laws parse
// sources rather than type-check a package, so that a tree another lane has
// half-edited still yields an answer about the files that do parse. The four
// shapes below are every way this package actually comes by a request — a
// parameter, a literal, a copy of one (`candidate := *request`, which is how
// both ladder rungs avoid mutating the caller's), and a var — and a fifth way
// would show up as a model assignment this law could not attribute, which is
// the safe direction to be wrong in.
func funnelRequestIdents(function *ast.FuncDecl) map[string]bool {
	held := map[string]bool{}
	if function.Type.Params != nil {
		for _, field := range function.Type.Params.List {
			if !funnelIsAIRequest(field.Type) {
				continue
			}
			for _, name := range field.Names {
				held[name.Name] = true
			}
		}
	}
	if function.Body == nil {
		return held
	}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			for index, target := range typed.Lhs {
				name, ok := target.(*ast.Ident)
				if !ok || index >= len(typed.Rhs) {
					continue
				}
				if funnelYieldsRequest(typed.Rhs[index], held) {
					held[name.Name] = true
				}
			}
		case *ast.ValueSpec:
			if !funnelIsAIRequest(typed.Type) {
				return true
			}
			for _, name := range typed.Names {
				held[name.Name] = true
			}
		}
		return true
	})
	return held
}

// funnelYieldsRequest reports whether an expression produces an ai.Request:
// a literal, the address of one, or a dereference of a name already known to
// hold one.
func funnelYieldsRequest(expression ast.Expr, held map[string]bool) bool {
	switch typed := expression.(type) {
	case *ast.CompositeLit:
		return funnelIsAIRequest(typed.Type)
	case *ast.UnaryExpr:
		return typed.Op == token.AND && funnelYieldsRequest(typed.X, held)
	case *ast.StarExpr:
		ident, ok := typed.X.(*ast.Ident)
		return ok && held[ident.Name]
	}
	return false
}

// funnelIsAIRequest reports whether a type expression is ai.Request or a
// pointer to one.
func funnelIsAIRequest(expression ast.Expr) bool {
	switch typed := expression.(type) {
	case *ast.StarExpr:
		return funnelIsAIRequest(typed.X)
	case *ast.SelectorExpr:
		pkg, ok := typed.X.(*ast.Ident)
		return ok && pkg.Name == "ai" && typed.Sel.Name == "Request"
	}
	return false
}

// funnelIn reports membership, so a failure can be raised at the offending line
// as well as counted in the set comparison.
func funnelIn(list []string, name string) bool {
	for _, member := range list {
		if member == name {
			return true
		}
	}
	return false
}
