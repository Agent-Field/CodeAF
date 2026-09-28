// Package namelaw is THE NAME LAW, and it is a package of nothing but the law.
//
// A rename is finished only when a live surface cannot quietly restore the old
// name. This law reads every Go source under cmd, internal and bench; the build,
// workflow and script surfaces; both manual corpora and the session prompts;
// and the small allow-list of root and top-level documentation named below.
// THE PRODUCT HAS ONE NAME, LOWERCASE, EVERYWHERE A PERSON, MODEL OR BUILD MEETS
// IT. A failure therefore names the file and line that brought a retired
// spelling back, instead of leaving the next wide search to rediscover it.
//
// Its exemptions are records rather than holes. Changelogs and design audits
// preserve what was true; screen frames, benchmark results, research notes,
// fixtures and testdata preserve observations; third-party and embedded binary
// material is not ours to rewrite. A line explicitly marked legacy-name is a
// compatibility seam, and a manual section headed "old name" is the one place
// that explains the history to a person who asks. This package exempts itself
// because the law has to spell the words it refuses. The root Markdown set is
// deliberately an allow-list, so untracked lane specifications and notes are
// not silently promoted into product surfaces.
package namelaw

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const legacyMarker = "legacy-name"

var retiredName = regexp.MustCompile(`(?i)aforge|openaf`)

var rootFiles = map[string]bool{
	"go.mod": true, "Makefile": true, ".gitignore": true, "SIZE-BUDGET": true,
	"README.md": true, "CLAUDE.md": true, "AGENTS.md": true, "PERF.md": true,
	"BENCHMARKS.md": true,
}

// TestW4TheFinishedTreeHasNoOldProductName is the repository-sized assertion.
// The fixture tests below prove every boundary independently, so a green here
// means the named live surfaces were read rather than merely that a walk found
// nothing convenient.
func TestW4TheFinishedTreeHasNoOldProductName(t *testing.T) {
	root := repoRoot(t)
	for _, violation := range violations(root) {
		t.Error(violation)
	}
}

// TestW4EveryCoverageClassFailsWithFileAndLine keeps the coverage list honest.
// Each case is its own small checkout because go.mod is itself a covered file.
func TestW4EveryCoverageClassFailsWithFileAndLine(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
		line int
	}{
		{"cmd Go", "cmd/codeaf/main.go", "package main\nvar product = \"aforge\"\n", 2},
		{"internal Go", "internal/live/live.go", "package live\nvar product = \"aforge\"\n", 2},
		{"bench Go", "bench/live.go", "package bench\nvar product = \"aforge\"\n", 2},
		{"module", "go.mod", "module example.com/aforge\n", 1},
		{"Makefile", "Makefile", "safe:\n\t@echo aforge\n", 2},
		{"gitignore", ".gitignore", "safe\n.aforge\n", 2},
		{"size budget", "SIZE-BUDGET", "safe\naforge\n", 2},
		{"workflow", ".github/workflows/check.yml", "name: safe\nrun: aforge\n", 2},
		{"issue template", ".github/ISSUE_TEMPLATE/defect.md", "# Safe\naforge\n", 2},
		{"pull request template", ".github/PULL_REQUEST_TEMPLATE.md", "# Safe\naforge\n", 2},
		{"ruleset readme", ".github/rulesets/README.md", "# Safe\naforge\n", 2},
		{"shell script", "scripts/check.sh", "#!/bin/sh\necho aforge\n", 2},
		{"Python script", "scripts/check.py", "# safe\nname = 'aforge'\n", 2},
		{"bench shell", "bench/tools/check.sh", "#!/bin/sh\necho aforge\n", 2},
		{"bench Python", "bench/tools/check.py", "# safe\nname = 'aforge'\n", 2},
		{"bench Markdown", "bench/tools/README.md", "# Safe\naforge\n", 2},
		{"chat manual", "internal/manual/chat/page.md", "# Safe\naforge\n", 2},
		{"resident manual", "internal/manual/pages/page.md", "# Safe\naforge\n", 2},
		{"session prompt", "internal/session/prompts/system.md", "# Safe\naforge\n", 2},
		{"root readme", "README.md", "# Safe\naforge\n", 2},
		{"agent guide", "CLAUDE.md", "# Safe\naforge\n", 2},
		{"agent guide link surface", "AGENTS.md", "# Safe\naforge\n", 2},
		{"performance guide", "PERF.md", "# Safe\naforge\n", 2},
		{"benchmark guide", "BENCHMARKS.md", "# Safe\naforge\n", 2},
		{"top-level doc", "docs/LIVE.md", "# Safe\naforge\n", 2},
		{"rule doc", "docs/rules/live.md", "# Safe\naforge\n", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := fixtureTree(t, map[string]string{tc.path: tc.body})
			want := fmt.Sprintf("%s:%d spells the old product name", tc.path, tc.line)
			assertOneViolation(t, violations(root), want)
		})
	}
}

// TestW4GoLegacyNameMarkerExemptsOnlyItsLine proves both sides of the inline
// compatibility exemption. go/parser locates the marker as a real comment; a
// marker-looking string literal cannot open the exemption.
func TestW4GoLegacyNameMarkerExemptsOnlyItsLine(t *testing.T) {
	root := fixtureTree(t, map[string]string{
		"internal/live/live.go": "package live\n" +
			"var kept = \"aforge\" // legacy-name\n" +
			"var markerLookingString = \"legacy-name openaf\"\n" +
			"var refused = \"openaf\"\n",
	})
	got := violations(root)
	assertViolation(t, got, "internal/live/live.go:3 spells the old product name")
	assertViolation(t, got, "internal/live/live.go:4 spells the old product name")
	if len(got) != 2 {
		t.Fatalf("inline marker should exempt its own line only; got %v", got)
	}
}

// A marker on a line of its own marks no retired spelling and cannot exempt
// any following source line.
func TestW4WholeLineLegacyNameMarkerDoesNotExemptFollowingGo(t *testing.T) {
	root := fixtureTree(t, map[string]string{
		"internal/live/live.go": "package live\n\n" +
			"// legacy-name\n" +
			"var first = \"aforge\"\n" +
			"var second = \"openaf\"\n\n\n" +
			"var refused = \"aforge\"\n",
	})
	got := violations(root)
	for _, line := range []string{"internal/live/live.go:4", "internal/live/live.go:5", "internal/live/live.go:8"} {
		assertViolation(t, got, line+" spells the old product name")
	}
	if len(got) != 3 {
		t.Fatalf("standalone marker exempted Go source: %v", got)
	}
}

// Shell markers have the same same-line boundary as Go comments.
func TestW4ShellLegacyNameMarkersExemptOnlyTheirOwnLine(t *testing.T) {
	root := fixtureTree(t, map[string]string{
		"scripts/compat.sh": "#!/bin/sh\n" +
			"echo AFORGE_HOME # legacy-name\n" +
			"# legacy-name\n" +
			"echo AFORGE_HOME\n" +
			"echo openaf\n\n" +
			"echo aforge\n",
	})
	got := violations(root)
	for _, line := range []string{"scripts/compat.sh:4", "scripts/compat.sh:5", "scripts/compat.sh:7"} {
		assertViolation(t, got, line+" spells the old product name")
	}
	if len(got) != 3 {
		t.Fatalf("standalone marker exempted shell source: %v", got)
	}
}

// TestW4MarkdownOldNameSectionIsTheOnlyProseExemption proves the section's
// start, its level-three interior, and the next level-two heading that ends it.
func TestW4MarkdownOldNameSectionIsTheOnlyProseExemption(t *testing.T) {
	root := fixtureTree(t, map[string]string{
		"internal/manual/chat/page.md": "# Page\n" +
			"outside aforge\n\n" +
			"## The old name — aforge\n" +
			"inside openaf\n" +
			"### openaf detail\n" +
			"still inside aforge\n\n" +
			"## Current name\n" +
			"outside again openaf\n",
	})
	got := violations(root)
	assertViolation(t, got, "internal/manual/chat/page.md:2 spells the old product name")
	assertViolation(t, got, "internal/manual/chat/page.md:10 spells the old product name")
	if len(got) != 2 {
		t.Fatalf("old-name section should exempt its heading and body through level three only; got %v", got)
	}
}

// TestW4FrozenAndNonProductPathsStayExempt proves every frozen class and the
// deliberate root Markdown allow-list. These files retain history or recorded
// bytes; rewriting them would make the record less true.
func TestW4FrozenAndNonProductPathsStayExempt(t *testing.T) {
	root := fixtureTree(t, map[string]string{
		"CHANGELOG.md":                     "aforge\n",
		"docs/changes/unreleased/x.md":     "aforge\n",
		"docs/design/x/DESIGN.md":          "aforge\n",
		"docs/design/x/frame.txt":          "aforge\n",
		"bench-results/run.txt":            "aforge\n",
		"audit-notes/run.md":               "aforge\n",
		"harness-research-notes.md":        "aforge\n",
		"internal/live/testdata/record.go": "aforge\n",
		"bench/tool/fixtures/record.go":    "aforge\n",
		"third_party/example/live.go":      "aforge\n",
		"bin/codeaf":                       "aforge\n",
		"internal/furrowbin/generated.go":  "aforge\n",
		"internal/manual/chat.pack.gz":     "aforge\n",
		"internal/namelaw/self.go":         "aforge\n",
		".git/config":                      "aforge\n",
		"NOTES-WORDS.md":                   "aforge\n",
	})
	if got := violations(root); len(got) != 0 {
		t.Fatalf("frozen and non-product paths must preserve their recorded words; got %v", got)
	}
}

// TestW6AgentGuidesAreByteIdenticalAndCarryTheNameSection proves the agent-facing
// half of the rename without depending on whether one checkout uses a symlink.
func TestW6AgentGuidesAreByteIdenticalAndCarryTheNameSection(t *testing.T) {
	root := repoRoot(t)
	claude := readFile(t, filepath.Join(root, "CLAUDE.md"))
	agents := readFile(t, filepath.Join(root, "AGENTS.md"))
	if !bytes.Equal(claude, agents) {
		t.Fatal("CLAUDE.md and AGENTS.md differ; the two agent-facing guides must be byte-identical")
	}
	if !bytes.Contains(claude, []byte("\n## The name\n")) {
		t.Fatal("CLAUDE.md and AGENTS.md do not carry the required `## The name` section")
	}
}

// TestW7ThePromptNamesCodeafOnceAndNamesNoRetiredProduct proves the complete
// prompt corpus, including the exact set of fragments the session assembles.
func TestW7ThePromptNamesCodeafOnceAndNamesNoRetiredProduct(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "internal", "session", "prompts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []string{"bashrules.md", "bashtask.md", "bashworker.md", "discipline.md", "divide.md", "fanout.md", "landing-answer.md", "program-outcome.md", "quick.md", "revise.md", "runask.md", "runsummary.md", "shape.md", "system.md", "worker.md"}
	var gotFiles []string
	var corpus []byte
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		gotFiles = append(gotFiles, entry.Name())
		body := readFile(t, filepath.Join(dir, entry.Name()))
		if match := retiredName.Find(body); match != nil {
			t.Errorf("internal/session/prompts/%s spells a retired product name %q", entry.Name(), match)
		}
		corpus = append(corpus, body...)
	}
	sort.Strings(gotFiles)
	if strings.Join(gotFiles, "\n") != strings.Join(wantFiles, "\n") {
		t.Fatalf("prompt fragments are %v, want exactly %v", gotFiles, wantFiles)
	}
	const identity = "You are codeaf: a working colleague in a conversation."
	if got := bytes.Count(corpus, []byte(identity)); got != 1 {
		t.Fatalf("the prompt names the product with %q %d times, want exactly once", identity, got)
	}
	system := readFile(t, filepath.Join(dir, "system.md"))
	if !bytes.HasPrefix(system, []byte(identity)) {
		t.Fatalf("system.md does not open with the one product identity: %q", identity)
	}
}

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

func violations(root string) []string {
	var found []string
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel != "." && exemptDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if exemptFile(rel) || !coveredFile(rel) {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		lines := strings.Split(string(body), "\n")
		exempt := make([]bool, len(lines)+1)
		if filepath.Ext(rel) == ".go" {
			fset := token.NewFileSet()
			parsed, parseErr := parser.ParseFile(fset, path, body, parser.ParseComments)
			if parseErr != nil {
				found = append(found, fmt.Sprintf("%s does not parse for the name law: %v", rel, parseErr))
				return nil
			}
			for _, group := range parsed.Comments {
				for _, comment := range group.List {
					start := fset.Position(comment.Pos())
					end := fset.Position(comment.End())
					commentLines := strings.Split(string(body[start.Offset:end.Offset]), "\n")
					for offset, text := range commentLines {
						line := start.Line + offset
						if !strings.Contains(text, legacyMarker) || line > len(lines) {
							continue
						}
						exempt[line] = true
					}
				}
			}
		} else {
			for index, line := range lines {
				if strings.Contains(line, legacyMarker) {
					exempt[index+1] = true
				}
			}
		}
		if filepath.Ext(rel) == ".md" {
			inOldNameSection := false
			for index, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") {
					inOldNameSection = strings.HasPrefix(trimmed, "## ") && strings.Contains(strings.ToLower(trimmed), "old name")
				}
				if inOldNameSection {
					exempt[index+1] = true
				}
			}
		}
		for index, line := range lines {
			if exempt[index+1] {
				continue
			}
			if old := retiredName.FindString(line); old != "" {
				found = append(found, fmt.Sprintf("%s:%d spells the old product name %q — the product is codeaf everywhere a person, model or build meets it\n  %s", rel, index+1, old, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if walkErr != nil {
		found = append(found, fmt.Sprintf("walking name-law surfaces: %v", walkErr))
	}
	sort.Strings(found)
	return found
}

func coveredFile(rel string) bool {
	ext := filepath.Ext(rel)
	if ext == ".go" && (hasPathPrefix(rel, "cmd") || hasPathPrefix(rel, "internal") || hasPathPrefix(rel, "bench")) {
		return true
	}
	if rootFiles[rel] {
		return true
	}
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == ".github/workflows" && ext == ".yml" {
		return true
	}
	if dir == ".github/ISSUE_TEMPLATE" && ext == ".md" {
		return true
	}
	if rel == ".github/PULL_REQUEST_TEMPLATE.md" || rel == ".github/rulesets/README.md" {
		return true
	}
	if dir == "scripts" && (ext == ".sh" || ext == ".py") {
		return true
	}
	if hasPathPrefix(rel, "bench") && (ext == ".sh" || ext == ".py" || ext == ".md") {
		return true
	}
	if (dir == "internal/manual/chat" || dir == "internal/manual/pages" || dir == "internal/session/prompts") && ext == ".md" {
		return true
	}
	return ext == ".md" && (dir == "docs" || dir == "docs/rules")
}

func exemptDir(rel string) bool {
	for _, dir := range []string{
		".git", "docs/changes", "docs/design", "bench-results", "audit-notes",
		"third_party", "bin", "internal/furrowbin", "internal/namelaw",
	} {
		if rel == dir || hasPathPrefix(rel, dir) {
			return true
		}
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for index, part := range parts {
		if part == "testdata" || (part == "fixtures" && index > 0 && parts[0] == "bench") {
			return true
		}
	}
	return false
}

func exemptFile(rel string) bool {
	if rel == "CHANGELOG.md" || rel == "harness-research-notes.md" {
		return true
	}
	return strings.HasPrefix(rel, "internal/manual/") && strings.HasSuffix(rel, ".pack.gz")
}

func hasPathPrefix(rel, prefix string) bool {
	return rel == prefix || strings.HasPrefix(rel, prefix+"/")
}

func fixtureTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module example.com/fixture\n\ngo 1.26\n"
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func assertOneViolation(t *testing.T, got []string, want string) {
	t.Helper()
	if len(got) != 1 || !strings.Contains(got[0], want) {
		t.Fatalf("violations = %v, want one containing %q", got, want)
	}
}

func assertViolation(t *testing.T, got []string, want string) {
	t.Helper()
	for _, violation := range got {
		if strings.Contains(violation, want) {
			return
		}
	}
	t.Errorf("violations = %v, want one containing %q", got, want)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
