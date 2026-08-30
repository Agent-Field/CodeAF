package verify

// The reading held to the two shapes it was built from: igel s14's import of a
// binding a rewritten module no longer defines, and the attribute shape textual
// s15 wore — and, at least as much, to the silences that keep it honest.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unboundTree writes a tree of files and answers with its root.
func unboundTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return root
}

// named is the found references as `file:line name`, which is what a reader of a
// failure wants to see.
func named(found []UnboundName) []string {
	said := make([]string, 0, len(found))
	for _, one := range found {
		said = append(said, one.Where()+" "+one.Name)
	}
	return said
}

// igelConfigsRewritten is `igel/configs.py` as the s14 patch left it: every path
// the module used to bind at its top level is now a LOCAL inside a builder
// function, and only `configs` survives as a module name.
const igelConfigsRewritten = `import os
from pathlib import Path

from igel.constants import Constants


def _get_configs():
    """Build config dict with paths relative to current working directory."""
    res_path = Path(os.getcwd()) / Constants.stats_dir
    init_file_path = Path(os.getcwd()) / Constants.init_file
    temp_post_req_data_path = Path(os.getcwd()) / Constants.post_req_data_file
    return {"results_path": res_path, "init_file_path": init_file_path}


configs = _get_configs()
`

// igelServer is `igel/servers/fastapi_server.py`, which the run did not rewrite
// and which still opens by asking that module for a name it no longer binds.
const igelServer = `from igel.configs import configs, temp_post_req_data_path


def upload(df):
    df.to_csv(temp_post_req_data_path, index=False)
    return configs
`

func TestAnImportOfANameTheModuleNoLongerBindsIsUnbound(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"igel/__init__.py":               "",
		"igel/constants.py":              "class Constants:\n    stats_dir = 'x'\n",
		"igel/configs.py":                igelConfigsRewritten,
		"igel/servers/__init__.py":       "",
		"igel/servers/fastapi_server.py": igelServer,
	})
	found := UnboundReferences(root, []string{"igel/configs.py", "igel/servers/fastapi_server.py"})
	if len(found) != 1 {
		t.Fatalf("want exactly the one unbound import, got %v", named(found))
	}
	if found[0].Name != "temp_post_req_data_path" {
		t.Fatalf("want temp_post_req_data_path, got %q", found[0].Name)
	}
	if found[0].Where() != "igel/servers/fastapi_server.py:1" {
		t.Fatalf("want the import's own line, got %q", found[0].Where())
	}
	if !strings.Contains(found[0].Ground, "igel/configs.py") {
		t.Fatalf("the ground must name the module that was read: %q", found[0].Ground)
	}
}

// richLogUnbound is the attribute shape: a class that reads a flag on itself
// which nothing in the tree ever assigns.
const richLogUnbound = `from textual.scroll_view import ScrollView


class RichLog(ScrollView):
    def __init__(self):
        super().__init__()
        self.lines = []

    def watch_min_width(self, old_value, new_value):
        if old_value != new_value and self._size_known:
            self.refresh()

    def refresh(self):
        return None
`

// richLogBound is the same class with the flag assigned, which is what textual
// s15's own patch actually spelled — the run's defect there was the ORDER of two
// lines inside `__init__`, not a name nothing binds, and this reader is silent
// on it by design.
const richLogBound = `from textual.scroll_view import ScrollView


class RichLog(ScrollView):
    def __init__(self):
        super().__init__()
        self._size_known = False

    def watch_min_width(self, old_value, new_value):
        if old_value != new_value and self._size_known:
            self.refresh()

    def refresh(self):
        return None
`

const scrollView = `class ScrollView:
    def __init__(self):
        self.scroll_y = 0
`

func TestAnAttributeAClassReadsAndNothingAssignsIsUnbound(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"textual/scroll_view.py":       scrollView,
		"textual/widgets/_rich_log.py": richLogUnbound,
	})
	found := UnboundReferences(root, []string{"textual/widgets/_rich_log.py"})
	if len(found) != 1 || found[0].Name != "RichLog._size_known" {
		t.Fatalf("want RichLog._size_known, got %v", named(found))
	}
	if !strings.Contains(found[0].Ground, "assigned nowhere in class RichLog") {
		t.Fatalf("the ground must say where it looked: %q", found[0].Ground)
	}
	if found[0].Text == "" || !strings.Contains(found[0].Text, "_size_known") {
		t.Fatalf("a finding carries the line it was read off: %q", found[0].Text)
	}
}

func TestAnAttributeAssignedAnywhereInTheTreeIsBound(t *testing.T) {
	// Assigned in the class itself.
	root := unboundTree(t, map[string]string{
		"textual/scroll_view.py":       scrollView,
		"textual/widgets/_rich_log.py": richLogBound,
	})
	if found := UnboundReferences(root, []string{"textual/widgets/_rich_log.py"}); len(found) != 0 {
		t.Fatalf("an assigned attribute is bound: %v", named(found))
	}
	// And assigned by a check, somewhere else entirely. A name a test writes
	// onto an instance is a name that exists at run time, and the index counts a
	// binding wherever the repository spells it.
	root = unboundTree(t, map[string]string{
		"textual/scroll_view.py":       scrollView,
		"textual/widgets/_rich_log.py": richLogUnbound,
		"tests/test_rich_log.py":       "def test_flag(widget):\n    widget._size_known = False\n",
	})
	if found := UnboundReferences(root, []string{"textual/widgets/_rich_log.py"}); len(found) != 0 {
		t.Fatalf("a binding elsewhere in the tree is a binding: %v", named(found))
	}
}

func TestSetattrInTheClassSilencesIt(t *testing.T) {
	dynamic := strings.Replace(richLogUnbound,
		"        self.lines = []",
		"        for key, value in {}.items():\n            setattr(self, key, value)", 1)
	root := unboundTree(t, map[string]string{
		"textual/scroll_view.py":       scrollView,
		"textual/widgets/_rich_log.py": dynamic,
	})
	if found := UnboundReferences(root, []string{"textual/widgets/_rich_log.py"}); len(found) != 0 {
		t.Fatalf("a class that binds by setattr is unreadable, not broken: %v", named(found))
	}
}

func TestABaseClassTheTreeDoesNotDeclareSilencesTheClass(t *testing.T) {
	// `NamedTuple` gets `_replace` from the standard library and `Handler` gets
	// `format` from `logging`; a reader that did not know this reported four
	// members of textual's own tree as names nothing assigns.
	root := unboundTree(t, map[string]string{
		"textual/layout.py": "from typing import NamedTuple\n\n\n" +
			"class WidgetPlacement(NamedTuple):\n" +
			"    region: int\n\n" +
			"    def reset_offset(self):\n" +
			"        return self._replace(region=0)\n",
	})
	if found := UnboundReferences(root, []string{"textual/layout.py"}); len(found) != 0 {
		t.Fatalf("a base this tree cannot read may bind anything: %v", named(found))
	}
}

func TestADataclassFieldIsABinding(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"pkg/model.py": "from dataclasses import dataclass\n\n\n" +
			"@dataclass\nclass Job:\n" +
			"    name: str\n" +
			"    tries: int = 0\n\n" +
			"    def describe(self):\n" +
			"        return f\"{self.name} {self.tries}\"\n",
	})
	if found := UnboundReferences(root, []string{"pkg/model.py"}); len(found) != 0 {
		t.Fatalf("a declared field is a binding: %v", named(found))
	}
}

func TestASlotsEntryIsABinding(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"pkg/point.py": "class Point:\n" +
			"    __slots__ = (\n        \"x\",\n        \"y\",\n    )\n\n" +
			"    def total(self):\n        return self.x + self.y\n",
	})
	if found := UnboundReferences(root, []string{"pkg/point.py"}); len(found) != 0 {
		t.Fatalf("a slot is a binding: %v", named(found))
	}
}

func TestAReExportedNameIsBound(t *testing.T) {
	// `__all__` is a module's own statement of what it hands out, and a name in
	// it is re-exported from wherever the module got it.
	root := unboundTree(t, map[string]string{
		"pkg/__init__.py": "__all__ = [\"Engine\", \"VERSION\"]\n\nfrom pkg.core import Engine\n",
		"pkg/core.py":     "class Engine:\n    pass\n",
		"app/run.py":      "from pkg import Engine, VERSION\n\n\ndef go():\n    return Engine, VERSION\n",
	})
	if found := UnboundReferences(root, []string{"app/run.py"}); len(found) != 0 {
		t.Fatalf("a name in __all__ is bound: %v", named(found))
	}
}

func TestASubmoduleOfAPackageIsBound(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"pkg/__init__.py": "",
		"pkg/core.py":     "VALUE = 1\n",
		"app/run.py":      "from pkg import core\n\n\ndef go():\n    return core.VALUE\n",
	})
	if found := UnboundReferences(root, []string{"app/run.py"}); len(found) != 0 {
		t.Fatalf("a package's own module is bound by importing it: %v", named(found))
	}
}

func TestAModuleWithAStarImportSaysNothing(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"pkg/__init__.py": "",
		"pkg/facade.py":   "from pkg.core import *\n",
		"pkg/core.py":     "VALUE = 1\n",
		"app/run.py":      "from pkg.facade import anything\n",
	})
	if found := UnboundReferences(root, []string{"app/run.py"}); len(found) != 0 {
		t.Fatalf("a module whose surface cannot be enumerated says nothing: %v", named(found))
	}
}

func TestAModuleOutsideTheTreeSaysNothing(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"app/run.py": "from numpy import invented_name\n\n\ndef go():\n    return invented_name\n",
	})
	if found := UnboundReferences(root, []string{"app/run.py"}); len(found) != 0 {
		t.Fatalf("this reader has no site-packages and must say so by silence: %v", named(found))
	}
}

func TestAConditionalTopLevelBindingIsBound(t *testing.T) {
	// `try: from x import y / except ImportError: y = None` binds a real name
	// four spaces in, and reading indent alone would call it missing.
	root := unboundTree(t, map[string]string{
		"pkg/__init__.py": "",
		"pkg/optional.py": "try:\n    from pkg.core import Engine\nexcept ImportError:\n    Engine = None\n",
		"pkg/core.py":     "class Engine:\n    pass\n",
		"app/run.py":      "from pkg.optional import Engine\n",
	})
	if found := UnboundReferences(root, []string{"app/run.py"}); len(found) != 0 {
		t.Fatalf("a conditional binding is a binding: %v", named(found))
	}
}

// ── typescript and javascript ────────────────────────────────────────────────

func TestANamedImportAModuleDoesNotExportIsUnbound(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"src/limits.ts": "export const MAX = 5;\n\nconst hidden = 1;\n",
		"src/fetch.ts":  "import { MAX, WINDOW } from './limits';\n\nexport const total = MAX + WINDOW;\n",
	})
	found := UnboundReferences(root, []string{"src/fetch.ts"})
	if len(found) != 1 || found[0].Name != "WINDOW" {
		t.Fatalf("want WINDOW, got %v", named(found))
	}
	if found[0].Scope != "src/limits.ts" {
		t.Fatalf("the scope is the module that was read: %q", found[0].Scope)
	}
}

func TestABarrelReExportSilencesTheModule(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"src/index.ts": "export * from './limits';\nexport const MAX = 5;\n",
		"src/fetch.ts": "import { WINDOW } from './index';\n",
	})
	if found := UnboundReferences(root, []string{"src/fetch.ts"}); len(found) != 0 {
		t.Fatalf("a barrel's exports cannot be enumerated: %v", named(found))
	}
}

func TestAMemberAClassReadsAndNothingDeclaresIsUnbound(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"src/observer.ts": "export class Observer {\n" +
			"\tpublic entries: string[] = [];\n\n" +
			"\tpublic flush(): number {\n" +
			"\t\treturn this.entries.length + this.threshold;\n" +
			"\t}\n" +
			"}\n",
	})
	found := UnboundReferences(root, []string{"src/observer.ts"})
	if len(found) != 1 || found[0].Name != "Observer.threshold" {
		t.Fatalf("want Observer.threshold, got %v", named(found))
	}
}

func TestAnIndexSignatureSilencesTheClass(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"src/observer.ts": "export class Observer {\n" +
			"\t[key: string]: unknown;\n\n" +
			"\tpublic flush(): number {\n" +
			"\t\treturn this.threshold;\n" +
			"\t}\n" +
			"}\n",
	})
	if found := UnboundReferences(root, []string{"src/observer.ts"}); len(found) != 0 {
		t.Fatalf("a class with an index signature declares everything: %v", named(found))
	}
}

func TestALanguageOwnedMemberIsNotAFinding(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"src/task.ts": "export class Task {\n" +
			"\tpublic id = 0;\n\n" +
			"\tpublic name(): string {\n" +
			"\t\treturn (<typeof Task>this.constructor).toString();\n" +
			"\t}\n" +
			"}\n",
	})
	if found := UnboundReferences(root, []string{"src/task.ts"}); len(found) != 0 {
		t.Fatalf("the language owns these and no repository spells them: %v", named(found))
	}
}

// ── what the reading refuses to be asked ─────────────────────────────────────

func TestGoAndRustAreNotRead(t *testing.T) {
	// `go build` and `cargo build` are the reading for those two, and a second
	// reader guessing at the same question could only disagree with a compiler.
	root := unboundTree(t, map[string]string{
		"main.go": "package main\n\nfunc main() { println(missing) }\n",
		"lib.rs":  "fn main() { let _ = missing; }\n",
	})
	if found := UnboundReferences(root, []string{"main.go", "lib.rs"}); len(found) != 0 {
		t.Fatalf("go and rust are absent on purpose: %v", named(found))
	}
}

func TestAVendoredSourceIsNotAsked(t *testing.T) {
	// The binding index does not walk `node_modules`, so a reference inside one
	// has its declarations in files nothing read. Asked anyway over happy-dom's
	// own tree, this reported twenty-four members declared eighty lines below
	// the line that used them.
	root := unboundTree(t, map[string]string{
		"node_modules/entities/decode.js": "class EntityDecoder {\n" +
			"\tstep() {\n\t\treturn this.stateNumericStart();\n\t}\n" +
			"\tstateNumericStart() {\n\t\treturn 1;\n\t}\n}\n",
	})
	if found := UnboundReferences(root, []string{"node_modules/entities/decode.js"}); len(found) != 0 {
		t.Fatalf("a check may only be asked where the index looked: %v", named(found))
	}
}

func TestAFileTheRunDidNotChangeIsNotRead(t *testing.T) {
	// The reading is of the WORK. A dangling reference in a file nobody touched
	// was dangling before the run started.
	root := unboundTree(t, map[string]string{
		"textual/scroll_view.py":       scrollView,
		"textual/widgets/_rich_log.py": richLogUnbound,
		"textual/other.py":             "VALUE = 1\n",
	})
	if found := UnboundReferences(root, []string{"textual/other.py"}); len(found) != 0 {
		t.Fatalf("only the record is read: %v", named(found))
	}
	if found := UnboundReferences(root, nil); len(found) != 0 {
		t.Fatalf("no record is no claim: %v", named(found))
	}
}

func TestADocstringThatSpellsANameIsNotCode(t *testing.T) {
	prose := strings.Replace(richLogBound,
		"        self._size_known = False",
		"        \"\"\"Sets self._invented on the way past.\"\"\"\n        self._size_known = False", 1)
	root := unboundTree(t, map[string]string{
		"textual/scroll_view.py":       scrollView,
		"textual/widgets/_rich_log.py": prose,
	})
	if found := UnboundReferences(root, []string{"textual/widgets/_rich_log.py"}); len(found) != 0 {
		t.Fatalf("a docstring is not a reference: %v", named(found))
	}
}

func TestTheSameTreeAnswersTheSameWayTwice(t *testing.T) {
	root := unboundTree(t, map[string]string{
		"igel/__init__.py":               "",
		"igel/constants.py":              "class Constants:\n    stats_dir = 'x'\n",
		"igel/configs.py":                igelConfigsRewritten,
		"igel/servers/__init__.py":       "",
		"igel/servers/fastapi_server.py": igelServer,
	})
	record := []string{"igel/servers/fastapi_server.py", "igel/configs.py"}
	first := named(UnboundReferences(root, record))
	second := named(UnboundReferences(root, record))
	if strings.Join(first, "|") != strings.Join(second, "|") {
		t.Fatalf("one tree, two answers: %v then %v", first, second)
	}
}
