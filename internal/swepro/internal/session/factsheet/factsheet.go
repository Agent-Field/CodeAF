// Package factsheet ports src/session/fact-sheet.ts:1-234 from swe-pro at
// commit 3b25a1a.
//
// Detection order and Markdown bytes are observable. Directory entries use
// Bun's localeCompare ordering through jscompat, optional values retain
// explicit JSON nulls, and String.prototype.trim semantics are used for
// .nvmrc.
package factsheet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// PackageManager mirrors the TS string union.
type PackageManager string

const (
	PackageManagerBun  PackageManager = "bun"
	PackageManagerPnpm PackageManager = "pnpm"
	PackageManagerYarn PackageManager = "yarn"
	PackageManagerNpm  PackageManager = "npm"
)

// RepoFacts mirrors the TS type. Pointer and nil-slice fields serialize as
// explicit null; no field may use omitempty.
type RepoFacts struct {
	PackageManager *PackageManager `json:"packageManager"`
	TestCommand    *string         `json:"testCommand"`
	TestFramework  *string         `json:"testFramework"`
	Packages       []string        `json:"packages"`
	Language       *string         `json:"language"`
	BuildScript    *string         `json:"buildScript"`
	NodeVersion    *string         `json:"nodeVersion"`
	Hubs           []string        `json:"hubs"`
}

// FactSheetFS is the injected filesystem seam. ReadFile's bool is false for
// the TS null result.
type FactSheetFS interface {
	ReadFile(path string) (string, bool)
	Exists(path string) bool
	ListDir(path string) []string
}

// GenerateFactSheetOpts mirrors the TS options bag. A nil FS selects the real
// filesystem. Nil Hubs is undefined; a non-nil empty slice is [].
type GenerateFactSheetOpts struct {
	RootDir string
	Hubs    []string
	FS      FactSheetFS
}

// FactSheetResult preserves the TS object literal's markdown/facts order.
type FactSheetResult struct {
	Markdown string    `json:"markdown"`
	Facts    RepoFacts `json:"facts"`
}

const (
	maxMarkdownLines = 40
	maxPackages      = 8
)

var (
	testFrameworks = []string{"vitest", "jest", "tap", "jasmine", "mocha", "karma"}
	testScriptKeys = []string{"test", "unit", "test:unit"}
)

type osFS struct{}

func (osFS) ReadFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func (osFS) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (osFS) ListDir(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return []string{}
	}
	out := make([]string, len(entries))
	for i, entry := range entries {
		out[i] = entry.Name()
	}
	return out
}

func parseJSON(raw string, ok bool) map[string]any {
	if !ok {
		return nil
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return object
}

func detectPackageManager(root string, fs FactSheetFS) *PackageManager {
	checks := []struct {
		files []string
		value PackageManager
	}{
		{[]string{"bun.lockb", "bun.lock"}, PackageManagerBun},
		{[]string{"pnpm-lock.yaml"}, PackageManagerPnpm},
		{[]string{"yarn.lock"}, PackageManagerYarn},
		{[]string{"package-lock.json"}, PackageManagerNpm},
	}
	for _, check := range checks {
		for _, file := range check.files {
			if fs.Exists(filepath.Join(root, file)) {
				value := check.value
				return &value
			}
		}
	}
	return nil
}

func objectAt(object map[string]any, key string) map[string]any {
	if object == nil {
		return nil
	}
	value, ok := object[key]
	if !ok {
		return nil
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return nested
}

func detectTestCommand(pkg map[string]any) *string {
	scripts := objectAt(pkg, "scripts")
	if scripts == nil {
		return nil
	}
	for _, key := range testScriptKeys {
		value, ok := scripts[key].(string)
		if ok && value != "" {
			return &value
		}
	}
	return nil
}

func detectTestFramework(pkg map[string]any) *string {
	bags := []map[string]any{
		objectAt(pkg, "devDependencies"),
		objectAt(pkg, "dependencies"),
	}
	for _, framework := range testFrameworks {
		for _, bag := range bags {
			if bag == nil {
				continue
			}
			if _, ok := bag[framework]; ok {
				value := framework
				return &value
			}
		}
	}
	return nil
}

func workspaceGlobs(pkg map[string]any) []string {
	if pkg == nil {
		return []string{}
	}
	workspaces := pkg["workspaces"]
	if array, ok := workspaces.([]any); ok {
		return stringElements(array)
	}
	if object, ok := workspaces.(map[string]any); ok {
		if array, ok := object["packages"].([]any); ok {
			return stringElements(array)
		}
	}
	return []string{}
}

func stringElements(values []any) []string {
	out := []string{}
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func packageNameAt(root, rel string, fs FactSheetFS) *string {
	raw, ok := fs.ReadFile(filepath.Join(root, rel, "package.json"))
	nested := parseJSON(raw, ok)
	if nested != nil {
		if name, ok := nested["name"].(string); ok && name != "" {
			return &name
		}
	}
	if fs.Exists(filepath.Join(root, rel)) || nested != nil {
		name := nodePathBase(rel)
		return &name
	}
	return nil
}

// nodePathBase is node:path.basename on this Linux source platform. Unlike
// filepath.Base, Node returns "" (not ".") for an empty path.
func nodePathBase(path string) string {
	end := len(path)
	for end > 0 && path[end-1] == '/' {
		end--
	}
	if end == 0 {
		return ""
	}
	start := strings.LastIndex(path[:end], "/") + 1
	return path[start:end]
}

func expandWorkspaceGlob(root, glob string, fs FactSheetFS) []string {
	if strings.HasSuffix(glob, "/*") {
		parent := glob[:len(glob)-2]
		parentPath := filepath.Join(root, parent)
		if !fs.Exists(parentPath) {
			return []string{}
		}
		entries := append([]string{}, fs.ListDir(parentPath)...)
		sort.SliceStable(entries, func(i, j int) bool {
			return jscompat.LocaleCompare(entries[i], entries[j]) < 0
		})
		out := []string{}
		for _, name := range entries {
			rel := filepath.Join(parent, name)
			if fs.Exists(filepath.Join(root, rel, "package.json")) ||
				fs.Exists(filepath.Join(root, rel)) {
				out = append(out, rel)
			}
		}
		return out
	}
	return []string{glob}
}

func detectPackages(root string, pkg map[string]any, fs FactSheetFS) []string {
	names := []string{}
	seen := map[string]bool{}
	push := func(name *string) {
		if name == nil || *name == "" || seen[*name] || len(names) >= maxPackages {
			return
		}
		seen[*name] = true
		names = append(names, *name)
	}

	for _, glob := range workspaceGlobs(pkg) {
		for _, rel := range expandWorkspaceGlob(root, glob, fs) {
			push(packageNameAt(root, rel, fs))
			if len(names) >= maxPackages {
				break
			}
		}
		if len(names) >= maxPackages {
			break
		}
	}

	packagesDir := filepath.Join(root, "packages")
	if len(names) == 0 && fs.Exists(packagesDir) {
		entries := append([]string{}, fs.ListDir(packagesDir)...)
		sort.SliceStable(entries, func(i, j int) bool {
			return jscompat.LocaleCompare(entries[i], entries[j]) < 0
		})
		for _, entry := range entries {
			push(packageNameAt(root, filepath.Join("packages", entry), fs))
			if len(names) >= maxPackages {
				break
			}
		}
	}

	if len(names) == 0 {
		return nil
	}
	return names
}

func detectNodeVersion(root string, pkg map[string]any, fs FactSheetFS) *string {
	if engines := objectAt(pkg, "engines"); engines != nil {
		if node, ok := engines["node"].(string); ok && node != "" {
			return &node
		}
	}
	if raw, ok := fs.ReadFile(filepath.Join(root, ".nvmrc")); ok {
		if trimmed := jscompat.Trim(raw); trimmed != "" {
			return &trimmed
		}
	}
	return nil
}

func renderMarkdown(facts RepoFacts) string {
	lines := []string{"## Repo facts (precomputed — do not re-derive)"}
	push := func(line string) {
		if len(lines) < maxMarkdownLines {
			lines = append(lines, line)
		}
	}

	if facts.PackageManager != nil && *facts.PackageManager != "" {
		push("- Package manager: " + string(*facts.PackageManager))
	}
	if facts.Language != nil && *facts.Language != "" {
		push("- Language: " + *facts.Language)
	}
	if facts.TestCommand != nil && *facts.TestCommand != "" {
		push("- Test command: " + *facts.TestCommand)
	}
	if facts.TestFramework != nil && *facts.TestFramework != "" {
		push("- Test framework: " + *facts.TestFramework)
	}
	if facts.BuildScript != nil && *facts.BuildScript != "" {
		push("- Build script: " + *facts.BuildScript)
	}
	if facts.NodeVersion != nil && *facts.NodeVersion != "" {
		push("- Node version: " + *facts.NodeVersion)
	}

	if len(facts.Packages) > 0 {
		push("- Packages (" + jscompat.FormatNumber(float64(len(facts.Packages))) + "):")
		for _, name := range facts.Packages {
			if len(lines) >= maxMarkdownLines {
				break
			}
			push("  - " + name)
		}
	}
	if len(facts.Hubs) > 0 {
		push("- Hubs: " + strings.Join(facts.Hubs, ", "))
	}
	if len(lines) == 1 {
		push("- (no facts detected)")
	}
	return strings.Join(lines, "\n")
}

// GenerateFactSheet precomputes stable repository facts and their Markdown
// rendering.
func GenerateFactSheet(opts GenerateFactSheetOpts) FactSheetResult {
	fs := opts.FS
	if fs == nil {
		fs = osFS{}
	}
	root := opts.RootDir
	raw, ok := fs.ReadFile(filepath.Join(root, "package.json"))
	pkg := parseJSON(raw, ok)

	var buildScript *string
	if scripts := objectAt(pkg, "scripts"); scripts != nil {
		if build, ok := scripts["build"].(string); ok {
			buildScript = &build
		}
	}

	var language *string
	if fs.Exists(filepath.Join(root, "tsconfig.json")) {
		value := "TypeScript"
		language = &value
	}

	var hubs []string
	if opts.Hubs != nil {
		hubs = append([]string{}, opts.Hubs...)
	}
	facts := RepoFacts{
		PackageManager: detectPackageManager(root, fs),
		TestCommand:    detectTestCommand(pkg),
		TestFramework:  detectTestFramework(pkg),
		Packages:       detectPackages(root, pkg, fs),
		Language:       language,
		BuildScript:    buildScript,
		NodeVersion:    detectNodeVersion(root, pkg, fs),
		Hubs:           hubs,
	}
	return FactSheetResult{Markdown: renderMarkdown(facts), Facts: facts}
}
