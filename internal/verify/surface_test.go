package verify

// The symbol-level half of the photograph, held to igel s11's own shape.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// igelBase is `igel/igel.py` as the s11 task image holds it at its base commit,
// in the part that matters: a public class whose paths are CLASS attributes.
const igelBase = `import logging

try:
    from igel.configs import configs
except ImportError:
    from configs import configs

logger = logging.getLogger(__name__)


class Igel:
    """
    Igel is the base model to use the fit, evaluate and predict functions
    """

    available_commands = ("fit", "evaluate", "predict", "experiment", "export")
    supported_types = ("regression", "classification", "clustering")
    results_path = configs.get("results_path")  # path to the results folder
    default_model_path = configs.get(
        "default_model_path"
    )  # path to the pre-fitted model
    description_file = configs.get(
        "description_file"
    )  # path to the description.json file
    model = None
    predictions = None  # store predictions as pandas df

    def __init__(self, **cli_args):
        self.command = cli_args.get("cmd")
        self._私 = None

    def _read_data_to_df(self):
        return None

    def fit(self, **kwargs):
        return None
`

// igelAfter is the same file as the s11 patch left it: the class attributes are
// gone and instance attributes are set in __init__ instead. `Igel.results_path`
// no longer exists; `Igel().results_path` does. Every one of the twenty-four
// hidden tests reaches for the first.
const igelAfter = `import logging

try:
    from igel.configs import configs
    from igel.feature_schema import FeatureSchema
except ImportError:
    from configs import configs

logger = logging.getLogger(__name__)


class Igel:
    """
    Igel is the base model to use the fit, evaluate and predict functions
    """

    available_commands = ("fit", "evaluate", "predict", "experiment", "export")
    supported_types = ("regression", "classification", "clustering")
    model = None
    predictions = None  # store predictions as pandas df

    def __init__(self, **cli_args):
        self.command = cli_args.get("cmd")
        self._私 = None

        from igel.configs import _make_configs
        _cfg = _make_configs()
        self.results_path = _cfg["results_path"]
        self.default_model_path = _cfg["default_model_path"]
        self.description_file = _cfg["description_file"]

    def _read_data_to_df(self):
        return None

    def fit(self, **kwargs):
        return None
`

// tree writes a set of files under one root and hands the root back.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// A CHECK IS EVIDENCE THAT SOMETHING IS EXERCISED; IT IS NOT EVIDENCE THAT
// NOTHING ELSE EXISTS.
//
// igel s11 deleted `Igel.results_path` and seven siblings — public class
// attributes — and set instance attributes of the same words in `__init__`. No
// check that project owns touches any of them, so the check-level reading of the
// finished tree came back an IMPROVEMENT: named 2 → 14, red 2 → 0. All
// twenty-four hidden tests failed at setup on `Igel.results_path`.
//
// The two ways a python class carries a name are two names, and this is the
// whole reason: `Igel.results_path` is reached on the class and
// `Igel().results_path` on an instance, and a reader that matched the bare word
// would call this change no change at all.
func TestAPublicNameThisWorkDeletedIsNamed(t *testing.T) {
	root := tree(t, map[string]string{"igel/igel.py": igelBase})
	baseline := PublicSurface(root)
	if names := baseline.Names("igel/igel.py"); len(names) == 0 {
		t.Fatal("the baseline read no public name out of a public class")
	}
	if err := os.WriteFile(filepath.Join(root, "igel/igel.py"), []byte(igelAfter), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := Focus{"igel/igel.py"}
	removed := baseline.Removed(SurfaceOf(root, changed), changed)

	held := strings.Join(removed, " ")
	for _, wanted := range []string{
		"Igel.results_path", "Igel.default_model_path", "Igel.description_file",
	} {
		if !strings.Contains(held, wanted) {
			t.Errorf("%s was deleted and nothing said so: %v", wanted, removed)
		}
	}
	// What the work kept is not reported, and neither is the class itself.
	for _, kept := range []string{"Igel", "Igel.model", "Igel.fit", "Igel.available_commands"} {
		if namedIn(removed, kept) {
			t.Errorf("%s survived the change and was reported removed: %v", kept, removed)
		}
	}
	// The instance attribute the work ADDED is a different name, and its
	// arrival is not a removal.
	for _, name := range removed {
		if strings.Contains(name, "()") {
			t.Errorf("an instance attribute was reported as removed: %q", name)
		}
	}
	// A private name is the author saying it is theirs, and this takes them at
	// their word.
	for _, name := range append(append([]string{}, removed...), baseline.Names("igel/igel.py")...) {
		if strings.Contains(name, "_read_data_to_df") || strings.Contains(name, "_私") {
			t.Errorf("a private name reached the public surface: %q", name)
		}
	}
}

// The same measurement in the other three language shapes, each spelling
// "public" its own way.
func TestThePublicSurfaceIsReadByLanguageShape(t *testing.T) {
	base := map[string]string{
		"src/index.ts": "" +
			"export function fetchOrigin(url: string) { return url }\n" +
			"export const RETRIES = 3\n" +
			"function helper() { return 1 }\n" +
			"export class Breaker {\n" +
			"  cooldown = 30000\n" +
			"  private secret = 1\n" +
			"  #hidden = 2\n" +
			"  open() { if (true) { return } }\n" +
			"}\n" +
			"export { helper }\n",
		"store/gate.go": "" +
			"package store\n\n" +
			"type Gate struct {\n\tPass bool\n\thidden int\n}\n\n" +
			"func (g Gate) Whole() bool { return g.Pass }\n" +
			"func Open() {}\n" +
			"func closed() {}\n",
		"src/lib.rs": "" +
			"pub struct Breaker {\n    pub cooldown: u64,\n    secret: u64,\n}\n\n" +
			"impl Breaker {\n    pub fn open(&self) {}\n    fn shut(&self) {}\n}\n\n" +
			"pub fn build() {}\nfn helper() {}\n",
	}
	root := tree(t, base)
	baseline := PublicSurface(root)
	for file, wanted := range map[string][]string{
		"src/index.ts":  {"fetchOrigin", "RETRIES", "Breaker", "Breaker.cooldown", "Breaker.open", "helper"},
		"store/gate.go": {"Gate", "Gate.Pass", "Gate.Whole", "Open"},
		"src/lib.rs":    {"Breaker", "Breaker.cooldown", "Breaker::open", "build"},
	} {
		held := strings.Join(baseline.Names(file), " ")
		for _, name := range wanted {
			if !namedIn(baseline.Names(file), name) {
				t.Errorf("%s: %s is public and was not read: %v", file, name, baseline.Names(file))
			}
		}
		for _, private := range []string{"secret", "hidden", "closed", "shut"} {
			if strings.Contains(held, private) {
				t.Errorf("%s: a private name reached the surface: %v", file, baseline.Names(file))
			}
		}
	}

	// Now the work: one export removed, one exported func removed, one pub fn
	// removed, and one name RENAMED.
	after := map[string]string{
		"src/index.ts": "" +
			"export const RETRIES = 3\n" +
			"export class Breaker {\n" +
			"  cooldown = 30000\n" +
			"  open() { return }\n" +
			"}\n",
		"store/gate.go": "" +
			"package store\n\n" +
			"type Gate struct {\n\tPass bool\n}\n\n" +
			"func (g Gate) Settled() bool { return g.Pass }\n",
		"src/lib.rs": "" +
			"pub struct Breaker {\n    pub cooldown: u64,\n}\n\n" +
			"impl Breaker {\n    fn open(&self) {}\n}\n\n" +
			"pub fn build() {}\n",
	}
	for path, body := range after {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)),
			[]byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	changed := Focus{"src/index.ts", "store/gate.go", "src/lib.rs"}
	removed := baseline.Removed(SurfaceOf(root, changed), changed)
	for _, name := range []string{
		"fetchOrigin", // an export deleted outright
		"helper",      // an export deleted from an export list
		"Open",        // an exported go func deleted
		"Gate.Whole",  // RENAMED to Settled: the old name is gone, and that is what a caller sees
		"Breaker::open",
	} {
		if !namedIn(removed, name) {
			t.Errorf("%s was removed and nothing said so: %v", name, removed)
		}
	}
	// The rename's new name is not a removal, and what survived is not either.
	for _, kept := range []string{"Gate.Settled", "RETRIES", "Breaker.cooldown", "build", "Gate.Pass"} {
		if namedIn(removed, kept) {
			t.Errorf("%s is still there and was reported removed: %v", kept, removed)
		}
	}
	// And the new name IS in the finished tree's reading, so the pair reads as
	// removed-and-added rather than as a mystery.
	if !namedIn(SurfaceOf(root, changed).Names("store/gate.go"), "Gate.Settled") {
		t.Error("the renamed-to name was not read on the finished tree")
	}
}

// A file the record names and the tree no longer holds has lost every public
// name it had. Losing a module is losing all of it.
func TestDeletingAModuleRemovesEveryPublicNameInIt(t *testing.T) {
	root := tree(t, map[string]string{
		"pkg/api.go": "package pkg\n\nfunc Serve() {}\nfunc Stop() {}\n",
	})
	baseline := PublicSurface(root)
	if err := os.Remove(filepath.Join(root, "pkg/api.go")); err != nil {
		t.Fatal(err)
	}
	record := []string{"pkg/api.go"}
	if held := ChangedSources(root, record); len(held) != 0 {
		t.Fatalf("a deleted file was handed to a test runner: %v", held)
	}
	gone := MissingFrom(root, record)
	if len(gone) != 1 || gone[0] != "pkg/api.go" {
		t.Fatalf("the deletion was not noticed: %v", gone)
	}
	removed := baseline.Removed(SurfaceOf(root, gone), gone)
	for _, name := range []string{"Serve", "Stop"} {
		if !namedIn(removed, name) {
			t.Errorf("%s went with its module and nothing said so: %v", name, removed)
		}
	}
}

// A name that vanished from a file NOBODY TOUCHED vanished some other way, and a
// finding about it would be a finding about work this run never did.
func TestOnlyTheFilesTheRunChangedAreCompared(t *testing.T) {
	root := tree(t, map[string]string{
		"a/one.go": "package a\n\nfunc Alpha() {}\n",
		"b/two.go": "package b\n\nfunc Beta() {}\n",
	})
	baseline := PublicSurface(root)
	for _, path := range []string{"a/one.go", "b/two.go"} {
		body := "package " + strings.Split(path, "/")[0] + "\n"
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)),
			[]byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	only := Focus{"a/one.go"}
	removed := baseline.Removed(SurfaceOf(root, only), only)
	if !namedIn(removed, "Alpha") {
		t.Errorf("the file the run changed lost a name and nothing said so: %v", removed)
	}
	if namedIn(removed, "Beta") {
		t.Errorf("a file this run never touched was reported on: %v", removed)
	}
}

// A CONSERVATIVE READER SAYS NOTHING RATHER THAN SOMETHING WRONG. A file that
// does not parse, a language with no reader, and a test file are all left alone:
// a false name here is a blocker on real work.
func TestTheSurfaceReaderNeverGuesses(t *testing.T) {
	root := tree(t, map[string]string{
		"broken.go":       "package p\n\nfunc Half( {\n",
		"notes.md":        "# Public API\n\nfunc Serve()\n",
		"pkg/api_test.go": "package pkg\n\nfunc TestServe() {}\n",
	})
	surface := PublicSurface(root)
	for file := range surface {
		t.Errorf("a file this cannot read with certainty was read anyway: %s -> %v",
			file, surface[file])
	}
}

func namedIn(names []string, wanted string) bool {
	for _, name := range names {
		if name == wanted {
			return true
		}
	}
	return false
}

func TestTheSurfaceIsReadAsAVocabulary(t *testing.T) {
	index := IndexSurface(Surface{"src/scrollbar.py": []Declaration{
		{Name: "ScrollBar"}, {Name: "ScrollBar.position"}, {Name: "Log.is_following_end"}}})
	if spoken := index.Spoken("the vertical scrollbar position for both widgets"); len(spoken) != 1 ||
		spoken[0] != "ScrollBar.position" {
		t.Errorf("a name spelled in words was not read: %v", spoken)
	}
	// One word is a word; a bare type is not a value to weigh.
	if spoken := index.Spoken("the scrollbar reports its position"); len(spoken) != 0 {
		t.Errorf("single words became names: %v", spoken)
	}
	// A symbol the request already spelled is confirmed through the member it
	// names, because a person writes is_following_end and leaves the class implied.
	if name, held := index.Holds("is_following_end"); !held || name != "Log.is_following_end" {
		t.Errorf("a member name was not confirmed: %q %v", name, held)
	}
	if _, held := index.Holds("full-width"); held {
		t.Errorf("a compound the tree does not declare was confirmed")
	}
}
