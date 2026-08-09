// This file ports swe-pro/src/tool/repo_overview.ts:11-277 at commit 3b25a1a.
// Cached-repository reference parsing is supplied by RepositoryResolver, the
// narrow seam for the out-of-bundle repository utility.
package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

const repoStructureLimit = 200

var ignoredRepoDirectories = stringSet(
	".git", "node_modules", "__pycache__", ".venv", "dist", "build",
	".next", "target", "vendor",
)

var repoDependencyFiles = []string{
	"package.json", "package-lock.json", "bun.lock", "bun.lockb",
	"pnpm-lock.yaml", "yarn.lock", "requirements.txt", "pyproject.toml",
	"go.mod", "Cargo.toml", "Gemfile", "build.gradle", "build.gradle.kts",
	"pom.xml", "composer.json",
}

var repoCommonEntrypoints = []string{
	"index.ts", "index.tsx", "index.js", "index.mjs", "main.ts", "main.js",
	"src/index.ts", "src/index.tsx", "src/index.js", "src/main.ts", "src/main.js",
}

// RepoOverviewParams is the tool input.
type RepoOverviewParams struct {
	Repository string   `json:"repository,omitempty"`
	Path       string   `json:"path,omitempty"`
	Depth      *float64 `json:"depth,omitempty"`
}

// RepoOverviewMetadata is the model-visible metadata.
type RepoOverviewMetadata struct {
	Path            string   `json:"path"`
	Repository      string   `json:"repository,omitempty"`
	Branch          string   `json:"branch,omitempty"`
	Head            string   `json:"head,omitempty"`
	PackageManager  string   `json:"package_manager,omitempty"`
	Ecosystems      []string `json:"ecosystems"`
	DependencyFiles []string `json:"dependency_files"`
	Entrypoints     []string `json:"entrypoints"`
	Depth           int      `json:"depth"`
	Truncated       bool     `json:"truncated"`
}

type RepoStructure struct {
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
}

type RepoOverviewResult struct {
	Title    string               `json:"title"`
	Metadata RepoOverviewMetadata `json:"metadata"`
	Output   string               `json:"output"`
}

// RepositoryResolver maps a repository reference to its normalized label and
// local cache path.
type RepositoryResolver interface {
	ResolveRepository(reference string) (label, path string, ok bool)
}

type RepositoryResolverFunc func(string) (label, path string, ok bool)

func (f RepositoryResolverFunc) ResolveRepository(reference string) (string, string, bool) {
	return f(reference)
}

// RepoGit is the narrow Git service projection used by the overview tool.
type RepoGit interface {
	Branch(context.Context, string) string
	Head(context.Context, string) (string, bool)
}

type ExecRepoGit struct{}

func (ExecRepoGit) Branch(ctx context.Context, directory string) string {
	command := exec.CommandContext(ctx, "git", "branch", "--show-current")
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (ExecRepoGit) Head(ctx context.Context, directory string) (string, bool) {
	command := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(output)), true
}

// RepoPackageManager applies the lockfile precedence from repo_overview.ts.
func RepoPackageManager(files []string) string {
	set := repoStringSet(files)
	switch {
	case set["bun.lock"] || set["bun.lockb"]:
		return "bun"
	case set["pnpm-lock.yaml"]:
		return "pnpm"
	case set["yarn.lock"]:
		return "yarn"
	case set["package-lock.json"]:
		return "npm"
	default:
		return ""
	}
}

// RepoEcosystems returns ecosystem labels in the fixed TS order.
func RepoEcosystems(files []string) []string {
	set := repoStringSet(files)
	out := []string{}
	if set["package.json"] {
		out = append(out, "Node.js")
	}
	if set["pyproject.toml"] || set["requirements.txt"] {
		out = append(out, "Python")
	}
	if set["go.mod"] {
		out = append(out, "Go")
	}
	if set["Cargo.toml"] {
		out = append(out, "Rust")
	}
	if set["Gemfile"] {
		out = append(out, "Ruby")
	}
	if set["build.gradle"] || set["build.gradle.kts"] || set["pom.xml"] {
		out = append(out, "Java/Kotlin")
	}
	if set["composer.json"] {
		out = append(out, "PHP")
	}
	return out
}

// RepoCommonEntrypoints filters the fixed likely-entrypoint list.
func RepoCommonEntrypoints(files []string) []string {
	set := repoStringSet(files)
	out := []string{}
	for _, file := range repoCommonEntrypoints {
		if set[file] {
			out = append(out, file)
		}
	}
	return out
}

type repoEntry struct {
	name      string
	full      string
	directory bool
}

// BuildRepoStructure walks directories depth-first, directories before files,
// with stable localeCompare ordering and the 200-line cap.
func BuildRepoStructure(root string, depth int) RepoStructure {
	result := RepoStructure{Lines: []string{}}
	var visit func(string, int)
	visit = func(directory string, level int) {
		if level >= depth || len(result.Lines) >= repoStructureLimit {
			result.Truncated = result.Truncated || len(result.Lines) >= repoStructureLimit
			return
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return
		}
		sorted := make([]repoEntry, 0, len(entries))
		for _, entry := range entries {
			if ignoredRepoDirectories[entry.Name()] {
				continue
			}
			full := filepath.Join(directory, entry.Name())
			info, err := os.Stat(full)
			if err != nil {
				continue
			}
			sorted = append(sorted, repoEntry{name: entry.Name(), full: full, directory: info.IsDir()})
		}
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].directory != sorted[j].directory {
				return sorted[i].directory
			}
			return jscompat.LocaleCompare(sorted[i].name, sorted[j].name) < 0
		})
		for _, entry := range sorted {
			if len(result.Lines) >= repoStructureLimit {
				result.Truncated = true
				return
			}
			line := strings.Repeat("  ", level) + entry.name
			if entry.directory {
				line += "/"
			}
			result.Lines = append(result.Lines, line)
			if entry.directory {
				visit(entry.full, level+1)
			}
		}
	}
	visit(root, 0)
	return result
}

func resolveOverviewDepth(value *float64) int {
	if value == nil || *value == 0 || *value != float64(int(*value)) || *value < 1 || *value > 6 {
		return 3
	}
	return int(*value)
}

func packageObjectKeys(raw json.RawMessage) []string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil
	}
	out := []string{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return out
		}
		key, _ := keyToken.(string)
		out = append(out, key)
		var discard json.RawMessage
		if decoder.Decode(&discard) != nil {
			return out
		}
	}
	return out
}

func packageEntrypoints(data []byte) []string {
	var object struct {
		Main    json.RawMessage `json:"main"`
		Module  json.RawMessage `json:"module"`
		Types   json.RawMessage `json:"types"`
		Bin     json.RawMessage `json:"bin"`
		Exports json.RawMessage `json:"exports"`
	}
	if json.Unmarshal(data, &object) != nil {
		return []string{}
	}
	out := []string{}
	addString := func(label string, raw json.RawMessage) {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			out = append(out, label+": "+value)
		}
	}
	addString("main", object.Main)
	addString("module", object.Module)
	addString("types", object.Types)
	var binString string
	if json.Unmarshal(object.Bin, &binString) == nil {
		out = append(out, "bin: "+binString)
	} else {
		for _, key := range packageObjectKeys(object.Bin) {
			out = append(out, "bin: "+key)
		}
	}
	keys := packageObjectKeys(object.Exports)
	if len(keys) > 10 {
		keys = keys[:10]
	}
	for _, key := range keys {
		out = append(out, "exports: "+key)
	}
	return out
}

// AssembleRepoOverview formats the final output from already-collected data.
func AssembleRepoOverview(metadata RepoOverviewMetadata, structure RepoStructure) RepoOverviewResult {
	title := metadata.Repository
	if title == "" {
		title = filepath.Base(metadata.Path)
	}
	lines := []string{"Path: " + metadata.Path}
	if metadata.Repository != "" {
		lines = append(lines, "Repository: "+metadata.Repository)
	}
	if metadata.Branch != "" {
		lines = append(lines, "Branch: "+metadata.Branch)
	}
	if metadata.Head != "" {
		lines = append(lines, "HEAD: "+metadata.Head)
	}
	if len(metadata.Ecosystems) > 0 {
		lines = append(lines, "Ecosystems: "+strings.Join(metadata.Ecosystems, ", "))
	}
	if metadata.PackageManager != "" {
		lines = append(lines, "Package manager: "+metadata.PackageManager)
	}
	if len(metadata.DependencyFiles) > 0 {
		lines = append(lines, "Dependency files: "+strings.Join(metadata.DependencyFiles, ", "))
	}
	if len(metadata.Entrypoints) > 0 {
		lines = append(lines, "Likely entrypoints:")
		for _, entrypoint := range metadata.Entrypoints {
			lines = append(lines, "- "+entrypoint)
		}
	}
	lines = append(lines, "Top-level structure:")
	lines = append(lines, structure.Lines...)
	if structure.Truncated {
		lines = append(lines, "(Structure truncated)")
	}
	return RepoOverviewResult{Title: title, Metadata: metadata, Output: strings.Join(lines, "\n")}
}

// BuildRepoOverview inspects a local path or an already-cached repository.
func BuildRepoOverview(ctx context.Context, params RepoOverviewParams, workspace string, resolver RepositoryResolver, git RepoGit) (RepoOverviewResult, error) {
	targetPath := ""
	repository := params.Repository
	if params.Path != "" {
		if filepath.IsAbs(params.Path) {
			targetPath = params.Path
		} else {
			targetPath = filepath.Join(workspace, params.Path)
		}
		targetPath = filepath.Clean(targetPath)
	} else {
		if params.Repository == "" {
			return RepoOverviewResult{}, fmt.Errorf("Either repository or path is required")
		}
		if resolver == nil {
			return RepoOverviewResult{}, fmt.Errorf("Repository must be a git URL, host/path reference, or GitHub owner/repo shorthand")
		}
		label, path, ok := resolver.ResolveRepository(params.Repository)
		if !ok {
			return RepoOverviewResult{}, fmt.Errorf("Repository must be a git URL, host/path reference, or GitHub owner/repo shorthand")
		}
		repository, targetPath = label, path
	}
	depth := resolveOverviewDepth(params.Depth)
	info, err := os.Stat(targetPath)
	if os.IsNotExist(err) {
		if repository != "" {
			return RepoOverviewResult{}, fmt.Errorf("Repository is not cloned: %s. Use repo_clone first.", repository)
		}
		return RepoOverviewResult{}, fmt.Errorf("Directory not found: %s", targetPath)
	}
	if err != nil {
		return RepoOverviewResult{}, err
	}
	if !info.IsDir() {
		return RepoOverviewResult{}, fmt.Errorf("Path is not a directory: %s", targetPath)
	}
	entries, _ := os.ReadDir(targetPath)
	topLevel := make([]string, 0, len(entries))
	for _, entry := range entries {
		topLevel = append(topLevel, entry.Name())
	}
	topSet := repoStringSet(topLevel)
	dependencyFiles := []string{}
	for _, file := range repoDependencyFiles {
		if topSet[file] {
			dependencyFiles = append(dependencyFiles, file)
		}
	}
	entrypoints := []string{}
	if topSet["package.json"] {
		if data, err := os.ReadFile(filepath.Join(targetPath, "package.json")); err == nil {
			entrypoints = packageEntrypoints(data)
		}
	}
	commonFiles := append([]string{}, topLevel...)
	if topSet["src"] {
		commonFiles = append(commonFiles,
			"src/index.ts", "src/index.tsx", "src/index.js", "src/main.ts", "src/main.js",
		)
	}
	for _, file := range RepoCommonEntrypoints(commonFiles) {
		entrypoints = append(entrypoints, "file: "+file)
	}
	structure := BuildRepoStructure(targetPath, depth)
	branch, head := "", ""
	if git != nil {
		branch = git.Branch(ctx, targetPath)
		if value, ok := git.Head(ctx, targetPath); ok {
			head = value
		}
	}
	metadata := RepoOverviewMetadata{
		Path: targetPath, Repository: repository, Branch: branch, Head: head,
		PackageManager: RepoPackageManager(topLevel),
		Ecosystems:     RepoEcosystems(topLevel), DependencyFiles: dependencyFiles,
		Entrypoints: entrypoints, Depth: depth, Truncated: structure.Truncated,
	}
	return AssembleRepoOverview(metadata, structure), nil
}

func repoStringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
