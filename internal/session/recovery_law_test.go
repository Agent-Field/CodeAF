package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── ONE BUDGET, SAID ABOUT THE SESSION ──────────────────────────────────────
//
// internal/provider has held this law since #858 (dispatch_law_test.go) and it
// covered that package ONLY, for a reason stated in its own comment: six
// ladders in this package still counted their own attempts, each bounded, none
// of them reading the plan under it. Those six are folded
// (docs/design/recovery/DESIGN.md §7) and the law reaches here now.
//
// WHAT IT IS FOR. A count of tries above a transport that is already bounded by
// a deadline does not bound anything — it MULTIPLIES. The census of 2026-09-10
// measured the product: chains of sixteen and seventeen identical sends running
// eleven minutes and ending refused, under budgets each of which was correct on
// its own. There is one bound and it is `lane.Role.GiveUp` in the person's own
// time, scaled by the one thing they may turn (`response.attempts`).
//
// WHAT IT IS NOT FOR, and the allowlists below are the whole of it: a count of
// THINGS is not a count of TRIES. Every entry names a function or a constant and
// says in one line what it counts instead.

// countsTries are the loops in this package that may count, each with the thing
// it is counting that is not a try.
//
// A REPAIR ROUND IS NOT A RETRY, and that is four of these five. A retry sends
// the same request again and hopes the wire behaves; a repair round sends a
// DIFFERENT request — the model's own malformed answer, handed back with a note
// saying what was wrong with it — so "one draft and one repair" is a count of
// drafts, and a deadline in its place would buy the same full-price call over
// and over for an answer that is not going to parse.
var countsTries = map[string]string{
	"judgeDecomposable":          "one ask and one repair round — the second request carries the first answer",
	"shapeBrief":                 "one ask and one repair round — the second request carries the first answer",
	"claimJobLog":                "candidate job ids in an O_EXCL create race — a count of names, not of tries",
	"spillBashOutput":            "candidate action-file names in an O_EXCL create race — a count of names, not of tries",
	"completeWithRetryReasoning": "the number is the ROW a person reads (`attempt 2`), never a bound — the bound is the deadline above it",
	"handOverRunningTurn":        "brief drafts, one and one regeneration — see checkpointBriefTries",
	"writeHarness":               "design rounds in the rig's own conversation, each re-reading the guide and writing a different page",
}

// boundsACount are the constants that may end in a counting word, for the same
// reason and with the same test: what does it count?
var boundsACount = map[string]string{
	"checkpointBriefTries":    "brief drafts — one and one regeneration, not one request twice",
	"harnessDesignRetries":    "design rounds in the rig's own conversation, each a different page",
	"truncationContinuations": "continuations of a cut-off ANSWER — more of one reply, not another attempt at it",
	"auditRestoreEntries":     "journal entries read back, which is a size and not a patience",
	"SilentCutAttempts":       "internal/taxonomy's, named here only where a test states the same figure",
	"DegenerateCutAttempts":   "internal/taxonomy's, as above",
	"BlindCutAttempts":        "internal/taxonomy's, as above",
	"programAutoRetries":      "hand-offs of one piece of work to a program that codeaf starts on its own, each a new billed run on a sharper brief — the owner's cap on spending without them, not a patience for one call",
}

// TestNoAttemptCountingLoopInTheSession refuses a loop that counts its own
// tries, in the header or in the post statement.
//
// IT READS THE POST STATEMENT TOO, which internal/provider's version does not
// need to: `for ; ; attempt++` with the initialiser lifted out is the same
// budget written so a law looking only at `for x := 0;` cannot see it, and the
// turn loop is written exactly that way now — for the row, which is why it is
// on the allowlist rather than outside the law.
func TestNoAttemptCountingLoopInTheSession(t *testing.T) {
	counting := map[string]bool{"attempt": true, "attempts": true, "tries": true, "retry": true, "retries": true}
	for name, file := range sessionSources(t) {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if _, allowed := countsTries[function.Name.Name]; allowed {
				continue
			}
			ast.Inspect(function, func(node ast.Node) bool {
				loop, ok := node.(*ast.ForStmt)
				if !ok {
					return true
				}
				for _, counted := range loopCounts(loop) {
					if counting[strings.ToLower(counted)] {
						t.Errorf("%s: %s counts %q in a loop — the one bound is the plan's deadline "+
							"(docs/design/recovery/DESIGN.md §4). If it counts THINGS rather than "+
							"tries, add it to countsTries with the line that says which",
							name, function.Name.Name, counted)
					}
				}
				return true
			})
		}
	}
}

// lastWordOf is the final camel-cased word of an identifier, lowered —
// `checkpointBriefTries` is "tries" and `auditRestoreEntries` is "entries",
// which is the difference between a budget and a size and the reason this is
// not a suffix match.
func lastWordOf(name string) string {
	start := 0
	for index, letter := range name {
		if letter >= 'A' && letter <= 'Z' {
			start = index
		}
	}
	return strings.ToLower(name[start:])
}

// loopCounts is every name a loop advances: its initialiser's targets and its
// post statement's.
func loopCounts(loop *ast.ForStmt) []string {
	var counted []string
	if assign, ok := loop.Init.(*ast.AssignStmt); ok {
		for _, target := range assign.Lhs {
			if ident, ok := target.(*ast.Ident); ok {
				counted = append(counted, ident.Name)
			}
		}
	}
	if step, ok := loop.Post.(*ast.IncDecStmt); ok {
		if ident, ok := step.X.(*ast.Ident); ok {
			counted = append(counted, ident.Name)
		}
	}
	return counted
}

// TestNoConstantInTheSessionBoundsAnAttemptCount is the same law said about the
// numbers rather than the loops, because a budget does not have to be in a `for`
// header to be one.
func TestNoConstantInTheSessionBoundsAnAttemptCount(t *testing.T) {
	counting := map[string]bool{"attempts": true, "tries": true, "retries": true}
	for name, file := range sessionSources(t) {
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, ident := range spec.Names {
				if _, allowed := boundsACount[ident.Name]; allowed {
					continue
				}
				if counting[lastWordOf(ident.Name)] {
					t.Errorf("%s declares %s — a budget of its own is what the one deadline replaced "+
						"(docs/design/recovery/DESIGN.md §4). If it counts THINGS, add it to "+
						"boundsACount with the line that says which", name, ident.Name)
				}
			}
			return true
		})
	}
}

// TestEveryAllowedCountIsStillThere keeps the allowlists honest: an entry whose
// function or constant has gone is an exception nobody can check, and the whole
// value of these two lists is that they are short.
func TestEveryAllowedCountIsStillThere(t *testing.T) {
	names := map[string]bool{}
	for _, file := range sessionSources(t) {
		for _, decl := range file.Decls {
			if function, ok := decl.(*ast.FuncDecl); ok {
				names[function.Name.Name] = true
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if spec, ok := node.(*ast.ValueSpec); ok {
				for _, ident := range spec.Names {
					names[ident.Name] = true
				}
			}
			return true
		})
	}
	for allowed, why := range countsTries {
		if !names[allowed] {
			t.Errorf("countsTries allows %q (%s) and there is no such function any more — delete the line", allowed, why)
		}
	}
	for allowed, why := range boundsACount {
		if !names[allowed] && !strings.HasSuffix(allowed, "CutAttempts") {
			t.Errorf("boundsACount allows %q (%s) and there is no such constant any more — delete the line", allowed, why)
		}
	}
}

// sessionFset is the position table every law in this package reads through, so
// a law that wants to NAME A LINE — and a failure a lane has to go and look at
// always does — can, without parsing the tree a second time of its own.
var sessionFset = token.NewFileSet()

// sessionLine is where a node is, for a failure message.
func sessionLine(node ast.Node) int { return sessionFset.Position(node.Pos()).Line }

// sessionSources parses every non-test file of this package, keyed by base name.
func sessionSources(t *testing.T) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := sessionFset
	files := map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files[name] = parsed
	}
	if len(files) == 0 {
		t.Fatal("no sources parsed — this law would pass by finding nothing")
	}
	return files
}
