package verify

// The reading held to igel s12's own shape: a definition that kept its name and
// changed what stands behind it, beside the code that still uses the old shape.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// igelConfigsBase is `igel/configs.py` as the s12 task image holds it at its
// base commit: the module binds `configs` to a plain dict.
const igelConfigsBase = `import os
from pathlib import Path

from igel.constants import Constants

res_path = Path(os.getcwd()) / Constants.stats_dir

configs = {
    "stats_dir": Constants.stats_dir,
    "results_path": res_path,
    "model_props": {"type": "classification"},
}
`

// igelConfigsAfter is the same module as the s12 patch left it: `configs` is now
// an instance of a class the run wrote, which answers to `.get` and to
// `config[key]` and NOT to `config[key] = value`. The name is unchanged, so the
// presence photograph read `compared: 8, lost: 0` and said nothing at all.
const igelConfigsAfter = `import os
from pathlib import Path

from igel.constants import Constants


class Configs:
    """Lazy configuration that resolves paths based on current working directory."""

    def __init__(self):
        self._cache = {}

    def get(self, key, default=None):
        if key not in self._cache:
            self._cache.update(self._build())
        return self._cache.get(key, default)

    def _build(self):
        res_path = Path(os.getcwd()) / Constants.stats_dir
        return {
            "stats_dir": Constants.stats_dir,
            "results_path": res_path,
            "model_props": {"type": "classification"},
        }

    def __getitem__(self, key):
        return self.get(key)


configs = Configs()
`

// baselineOf is the reading a job takes before its first change: the whole tree,
// as verify.Photograph takes it.
func baselineOf(t *testing.T, files map[string]string) (root string, baseline Surface) {
	t.Helper()
	root = tree(t, files)
	return root, PublicSurface(root)
}

// rewrite puts the finished tree in place of the one the baseline was taken of.
func rewrite(t *testing.T, root, file, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(file)), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A NAME IS NOT A CONTRACT.
//
// igel s12 kept `configs` and changed what stands behind it. The presence
// photograph compared eight names and lost none; twenty-four hidden tests then
// failed with `TypeError: 'Configs' object does not support item assignment`.
// The declaration is what moved, so the declaration is what is read — and the
// sites that still use the name are read beside it, with the shape of each use.
func TestADefinitionThatKeptItsNameAndChangedItsShapeIsRead(t *testing.T) {
	root, baseline := baselineOf(t, map[string]string{
		"igel/configs.py": igelConfigsBase,
		// The project's own code, untouched by the run, still using the name.
		"igel/utils.py": "from igel.configs import configs\n\n" +
			"def load():\n    return open(configs.get(\"results_path\"), \"rb\")\n",
		"tests/test_igel.py": "from igel.configs import configs\n\n" +
			"def test_fit(tmp_path):\n" +
			"    configs[\"results_path\"] = tmp_path\n" +
			"    configs[\"model_file\"] = tmp_path / \"model.joblib\"\n" +
			"    assert configs[\"results_path\"] == tmp_path\n",
	})
	rewrite(t, root, "igel/configs.py", igelConfigsAfter)
	changed := ChangedDefinitions(root, baseline, []string{"igel/configs.py"})
	names := make([]string, 0, len(changed))
	for _, definition := range changed {
		names = append(names, definition.Name)
	}
	held := strings.Join(names, " ")
	if !strings.Contains(held, "configs") {
		t.Fatalf("the run rebound `configs` and nothing read it as a changed definition: %v", names)
	}
	// `Configs` is a name the finished tree has and the baseline never had, so
	// it is an ADDITION and not a definition that moved. Reporting it would be a
	// finding about a class nobody could have been using.
	if strings.Contains(held, " Configs") {
		t.Errorf("a name the baseline never held was reported as rewritten: %v", names)
	}

	sites := Consumers(root, names, ChangedSources(root, []string{"igel/configs.py"}))
	shapes := map[string]int{}
	var where string
	for _, site := range sites["configs"] {
		shapes[site.Shape]++
		if site.Shape == ShapeSubscriptAssign {
			where = site.Where()
		}
	}
	if shapes[ShapeSubscriptAssign] != 2 {
		t.Errorf("two sites assign into `configs` and the reading found %d: %v",
			shapes[ShapeSubscriptAssign], shapes)
	}
	if shapes[ShapeSubscript] != 1 {
		t.Errorf("one site reads `configs` by subscript and the reading found %d: %v",
			shapes[ShapeSubscript], shapes)
	}
	if shapes[ShapeAttribute+".get"] != 1 {
		t.Errorf("one site reaches `.get` off `configs` and the reading found %d: %v",
			shapes[ShapeAttribute+".get"], shapes)
	}
	if !strings.HasPrefix(where, "tests/test_igel.py:") {
		t.Errorf("a site has to say where it is; got %q", where)
	}
}

// A MODULE-LEVEL BINDING IS A NAME THE MODULE PUBLISHES, and the python reader
// was the only one of the four that did not say so. Go reports an exported var,
// typescript an exported const, rust a `pub static`; python's singleton was read
// by nothing, which is why the name that moved in s12 was never in the
// photograph at all.
func TestAModuleLevelBindingIsPartOfThePublicSurface(t *testing.T) {
	root := tree(t, map[string]string{"igel/configs.py": igelConfigsBase})
	baseline := PublicSurface(root)
	names := baseline.Names("igel/configs.py")
	held := strings.Join(names, " ")
	for _, wanted := range []string{"configs", "res_path"} {
		if !strings.Contains(held, wanted) {
			t.Errorf("`%s` is a name this module publishes and the reading missed it: %v",
				wanted, names)
		}
	}
}

// The same reading in the other direction: a typescript export whose signature
// the run changed, and the call sites that still pass the old number of
// arguments. Nothing here knows what an argument means — the arity is counted
// off the brackets on the line, which is the whole of the claim.
func TestATypescriptExportsCallSitesCarryTheirArity(t *testing.T) {
	root, baseline := baselineOf(t, map[string]string{
		"src/retry.ts": "export function withRetry(fn, attempts) {\n  return fn;\n}\n",
		"src/fetch.ts": "import { withRetry } from \"./retry\";\n\n" +
			"export const load = () => withRetry(get, 3);\n",
		"docs/guide.md": "Call it like this:\n\n```ts\nwithRetry(get, 3);\n```\n\n" +
			"Prose that says withRetry is not a call site.\n",
	})
	rewrite(t, root, "src/retry.ts",
		"export function withRetry(fn, attempts, backoff) {\n  return fn;\n}\n")
	changed := ChangedDefinitions(root, baseline, []string{"src/retry.ts"})
	if len(changed) != 1 || changed[0].Name != "withRetry" {
		t.Fatalf("the run rewrote the declaration of withRetry: %+v", changed)
	}
	sites := Consumers(root, []string{"withRetry"}, ChangedSources(root, []string{"src/retry.ts"}))["withRetry"]
	calls, files := 0, map[string]bool{}
	for _, site := range sites {
		files[site.File] = true
		if site.Shape == ShapeCall+"(2 args)" {
			calls++
		}
	}
	if calls != 2 {
		t.Errorf("two places call it with two arguments — one of them in a document's own "+
			"code block — and the reading found %d: %+v", calls, sites)
	}
	// A README sentence that spells the name is not a caller, and counting one
	// would be a number nobody could act on.
	for _, site := range sites {
		if site.File == "docs/guide.md" && !strings.Contains(site.Text, "(") {
			t.Errorf("prose outside a fenced block was read as a usage site: %+v", site)
		}
	}
}

// EVERY SILENCE FAVOURS THE WORK. A file the run edited without moving any
// declaration says nothing, so the judge sees exactly what it saw before this
// existed.
func TestAChangeThatMovesNoDeclarationIsSilent(t *testing.T) {
	const body = "from igel.configs import configs\n%s\n\n" +
		"def load():\n    return configs.get(\"results_path\")\n"
	root, baseline := baselineOf(t, map[string]string{
		"igel/utils.py": strings.Replace(body, "%s", "# an old comment", 1),
	})
	rewrite(t, root, "igel/utils.py", strings.Replace(body, "%s", "# a new comment", 1))
	if changed := ChangedDefinitions(root, baseline, []string{"igel/utils.py"}); len(changed) != 0 {
		t.Errorf("a comment between two definitions moved neither, and this named %+v", changed)
	}
	// And so does a job that took no baseline, and a language with no reader.
	if changed := ChangedDefinitions(root, nil, []string{"igel/utils.py"}); len(changed) != 0 {
		t.Errorf("no baseline is no claim, and this named %+v", changed)
	}
	if changed := ChangedDefinitions(root, baseline, []string{"notes/plan.txt"}); len(changed) != 0 {
		t.Errorf("a file with no reader declares nothing, and this named %+v", changed)
	}
}

// A file this run changed is not a consumer of this run's own work. Counting one
// would report the change as evidence against itself.
func TestAFileTheRunChangedIsNotItsOwnConsumer(t *testing.T) {
	root := tree(t, map[string]string{
		"pkg/api.go":  "package pkg\n\nvar Fallback = Client{}\n\ntype Client struct{}\n",
		"pkg/read.go": "package pkg\n\nfunc Read() Client { return Client{} }\n",
	})
	sites := Consumers(root, []string{"Client"}, ChangedSources(root, []string{"pkg/api.go"}))["Client"]
	for _, site := range sites {
		if site.File == "pkg/api.go" {
			t.Errorf("a line of a file the run changed was counted as a consumer: %+v", site)
		}
	}
	if len(sites) == 0 {
		t.Error("the file the run did not touch uses the name and was not counted")
	}
}
