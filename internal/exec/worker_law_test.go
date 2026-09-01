package exec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ── THERE IS ONE WORKER ─────────────────────────────────────────────────────
//
// A job is done by one worker. Nothing chooses it, because there is nothing to
// choose, and these two laws are what makes that sentence true of the whole
// tree rather than true of the file somebody last edited.
//
// The argument is not tidiness. A choice surface is a promise: a menu offered to
// a model is a model that will name something, a flag offered to a person is a
// person who will pin something, and a manual page that describes a second
// worker is a document about a program that does not run. Every one of those
// costs tokens on every call and every one of them ends at a name nothing can
// construct. So the choice goes wherever it is written down — in a prompt, in a
// schema, in a tool description, on a manual page, in a Go identifier, in the
// Makefile — and not only where it was convenient to delete.
//
//	(a) nothing a model or a person reads offers a worker to choose
//	(b) nothing in the tree names the workers that were removed
//
// Both read sources and hold them to the law, in this repo's convention (see
// internal/provider/funnel_law_test.go, internal/lane/structure_test.go). Each
// failure names a file and a line.

// repoRoot is the top of the checkout, found from this package's own directory
// rather than from a working directory a test runner chose.
func repoRoot(t *testing.T) string {
	t.Helper()
	here, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(here, "go.mod")); err != nil {
		t.Fatalf("cannot find the repository root from %s: %v", here, err)
	}
	return here
}

// workerChoicePhrases are the shapes a worker choice takes in prose a model or
// a person reads. Each is forbidden by MEANING, and the meaning is written
// beside it:
//
//   - a removed worker named as a thing that could take work;
//   - "specialist", which in this tree only ever meant "a worker other than the
//     one there is";
//   - the flag and the instruction that asked for a name back.
//
// Deliberately absent, and each for a reason: "linear" is also the sequential
// adjective and the screen-reader palette (`--linear`); "worker" is the ordinary
// word for the thing doing a task and is used everywhere it should be;
// "escalate" is the model ladder — a stronger model on the same worker — which
// is a live mechanism with its own glyph; and "subharness" on its own is the
// SAVED PROGRAM, a different feature entirely, which the manual is right to
// describe at length.
var workerChoicePhrases = []struct {
	pattern *regexp.Regexp
	why     string
}{
	{regexp.MustCompile(`(?i)\bswe\b`), "the swe worker was removed; nothing may offer it"},
	{regexp.MustCompile(`(?i)swepro`), "the vendored swe engine was removed"},
	{regexp.MustCompile(`(?i)specialist`), "a specialist is a worker other than the one there is, and there is one"},
	{regexp.MustCompile(`--subharness`), "the flag that pinned a worker does not exist"},
	{regexp.MustCompile(`(?i)default worker`), "\"default\" implies one of several; there is one worker"},
	{regexp.MustCompile(`(?i)return the choice as`), "nothing asks a model to name a worker"},
	{regexp.MustCompile(`(?i)AFORGE_WORKERS`), "the worker roster setting does not exist"},
}

// promptCorpora are the files a model or a person actually reads: the session's
// own prompt fragments, every manual page, and the orchestrator's brief.
var promptCorpora = []string{
	"internal/session/prompts",
	"internal/manual",
	"internal/orchestrate/prompt.md",
}

// TestNoPromptOrManualPageOffersAWorkerChoice reads what a model and a person
// are handed and holds it to the law. Model-facing text is the expensive kind
// of wrong: a menu is paid for on every call that carries it, and it is obeyed.
func TestNoPromptOrManualPageOffersAWorkerChoice(t *testing.T) {
	root := repoRoot(t)
	for _, corpus := range promptCorpora {
		walkErr := filepath.WalkDir(filepath.Join(root, corpus), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			reportForbidden(t, root, path, string(raw))
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walking %s: %v", corpus, walkErr)
		}
	}
	for _, tree := range []string{"internal/head", "internal/plan", "internal/revision", "internal/session"} {
		for _, literal := range proseLiterals(t, filepath.Join(root, tree)) {
			reportForbidden(t, root, literal.file, literal.text)
		}
	}
}

// proseLiteral is one Go string literal that reads as prose — a prompt, a
// schema description, a tool's own account of itself.
type proseLiteral struct {
	file string
	text string
}

// proseLiterals collects the string literals in a tree that a model could
// plausibly be shown. The filter is three words or more, which is what keeps
// this law about PROSE: an identifier, a map key, a JSON field name and a
// struct tag are none of them a sentence, and a tree's worth of them would make
// the law unreadable and its failures unactionable.
func proseLiterals(t *testing.T, tree string) []proseLiteral {
	t.Helper()
	var found []proseLiteral
	walkErr := filepath.WalkDir(tree, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		tags := map[token.Pos]bool{}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if field, ok := node.(*ast.Field); ok && field.Tag != nil {
				tags[field.Tag.Pos()] = true
			}
			return true
		})
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING || tags[literal.Pos()] {
				return true
			}
			text, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil || strings.Count(text, " ") < 2 {
				return true
			}
			found = append(found, proseLiteral{
				file: fileSet.Position(literal.Pos()).String(),
				text: text,
			})
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("reading %s: %v", tree, walkErr)
	}
	return found
}

func reportForbidden(t *testing.T, root, where, text string) {
	t.Helper()
	where = strings.TrimPrefix(where, root+string(filepath.Separator))
	for index, line := range strings.Split(text, "\n") {
		for _, forbidden := range workerChoicePhrases {
			if forbidden.pattern.MatchString(line) {
				t.Errorf("%s (+%d) offers a worker choice — %s\n  %s",
					where, index+1, forbidden.why, strings.TrimSpace(line))
			}
		}
	}
}

// removedNames are the identifiers, import paths, environment variables and
// build targets of the workers that no longer exist. A name that survives its
// worker is a name somebody will wire something to.
var removedNames = []string{
	"swepro",
	"SWESubharness",
	"NewSWE",
	"BareSubharness",
	"AFORGE_SWEPRO",
	"AFORGE_SWE_MAX_COST",
	"internal/swepro",
}

// namesLawSkipped are the trees a name may still appear in, and every one of
// them is a RECORD rather than a program: changelog entries and benchmark
// reports say what was measured on the day it was measured, and rewriting them
// would be falsifying the record rather than removing a feature.
var namesLawSkipped = map[string]string{
	"docs/changes":                     "the changelog says what was removed, by name",
	"BENCHMARKS.md":                    "dated measurements of what ran at the time",
	"perf-report":                      "captured reports of runs that happened",
	"audit-notes":                      "an audit of the tree as it was",
	"bench/deepswe":                    "a benchmark track, named for the task set and not for a worker",
	"bench/oneroad":                    "the SWE-bench track for aforge, pi and opencode",
	".git":                             "not the tree",
	"internal/exec/worker_law_test.go": "this law has to be able to say the names it forbids",
}

// TestNothingInTheTreeNamesARemovedWorker is the other half. Prose is what a
// model obeys; a name is what a program reaches for, and a build target or an
// environment variable that outlives its feature is a door onto nothing that
// somebody will one day walk through.
func TestNothingInTheTreeNamesARemovedWorker(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "internal", "swepro")); err == nil {
		t.Error("internal/swepro exists")
	}
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		for skipped := range namesLawSkipped {
			if relative == skipped || strings.HasPrefix(relative, skipped+string(filepath.Separator)) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if entry.IsDir() {
			return nil
		}
		if !nameLawFile(relative) {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for index, line := range strings.Split(string(raw), "\n") {
			for _, name := range removedNames {
				if strings.Contains(line, name) {
					t.Errorf("%s:%d names %s, which no longer exists\n  %s",
						relative, index+1, name, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking the tree: %v", walkErr)
	}
}

// nameLawFile is where a name would actually be wired to something: Go source,
// the build, CI, and the documents that tell a person what to type.
func nameLawFile(relative string) bool {
	switch filepath.Base(relative) {
	case "Makefile":
		return true
	}
	switch filepath.Ext(relative) {
	case ".go", ".yml", ".yaml", ".md", ".sh", ".json", ".ts", ".js":
		return true
	}
	return false
}
