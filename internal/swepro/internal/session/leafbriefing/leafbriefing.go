// Package leafbriefing ports src/session/leaf-briefing.ts:1-344 from swe-pro
// at commit 3b25a1a.
//
// Model-visible strings are reproduced byte-for-byte. JavaScript string
// ordering, length, slicing, trimming, and lowercasing are load-bearing here:
// paths sort by UTF-16 code units, stem similarity is measured in UTF-16, and
// String.prototype.toLowerCase uses Unicode full lowercase mappings.
//
// RepoMapBuilder and ContextRefs keep the graph and conversation registries
// injectable while NewDefaultBuilder composes their ported implementations.
package leafbriefing

import (
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/cochange"
	"github.com/Agent-Field/swe-pro-go/internal/session/contextrefs"
	"github.com/Agent-Field/swe-pro-go/internal/session/importgraph"
)

const exemplarHeader = "# Analogous precedents (adopt their conventions for anything the spec leaves unstated)"

// AuditFixDeltaArgs mirrors buildAuditFixDelta's argument object.
type AuditFixDeltaArgs struct {
	Goal         string   `json:"goal"`
	Blockers     []string `json:"blockers"`
	Evidence     []string `json:"evidence"`
	ChangedFiles []string `json:"changedFiles"`
}

// BuildAuditFixDelta builds the compact audit-fix handoff.
func BuildAuditFixDelta(args AuditFixDeltaArgs) string {
	blockers := []string{"- (none recorded)"}
	if len(args.Blockers) > 0 {
		blockers = make([]string, len(args.Blockers))
		for i, item := range args.Blockers {
			blockers[i] = "- " + item
		}
	}
	evidence := []string{"- (none recorded)"}
	if len(args.Evidence) > 0 {
		evidence = make([]string, len(args.Evidence))
		for i, item := range args.Evidence {
			evidence[i] = "- " + item
		}
	}
	changedFiles := []string{"- (not supplied)"}
	if len(args.ChangedFiles) > 0 {
		changedFiles = make([]string, len(args.ChangedFiles))
		for i, item := range args.ChangedFiles {
			changedFiles[i] = "- " + item
		}
	}

	lines := []string{"## Goal", jscompat.Trim(args.Goal), "", "## Audit blockers"}
	lines = append(lines, blockers...)
	lines = append(lines, "", "## Audit evidence")
	lines = append(lines, evidence...)
	lines = append(lines, "", "## Changed files")
	lines = append(lines, changedFiles...)
	lines = append(lines, "", "Continue in the same worktree.")
	return strings.Join(lines, "\n")
}

// ContextRegistration is the subset of context-refs' register result consumed
// by formatLeafBriefing.
type ContextRegistration = contextrefs.RegisterResult

// ContextRefs is the narrow context-refs integration seam.
type ContextRefs interface {
	Register(kind, content string) ContextRegistration
	Render(refID string) string
}

// LeafBriefingSections mirrors the TS type of the same name.
type LeafBriefingSections struct {
	RepoMap   string `json:"repoMap"`
	CoChange  string `json:"coChange"`
	Imports   string `json:"imports"`
	Exemplars string `json:"exemplars"`
}

// FormatLeafBriefingArgs mirrors formatLeafBriefing's argument object.
type FormatLeafBriefingArgs struct {
	Workspace string               `json:"workspace"`
	Sections  LeafBriefingSections `json:"sections"`
	Refs      ContextRefs          `json:"-"`
}

func embedSection(refs ContextRefs, kind, content string) string {
	if refs == nil {
		return content
	}
	registration := refs.Register(kind, content)
	if registration.RefID == "" {
		return content
	}
	if registration.FirstMention {
		return registration.RefID + "\n" + content
	}
	return refs.Render(registration.RefID)
}

// FormatLeafBriefing renders the model-visible repository briefing.
func FormatLeafBriefing(args FormatLeafBriefingArgs) string {
	ws := args.Workspace
	sections := args.Sections
	return strings.Join([]string{
		"<leaf-briefing>",
		"Repository context is advisory; verify it against the current worktree.",
		"",
		"## Focused repository map",
		embedSection(args.Refs, "leaf-briefing:repo-map:"+ws, sections.RepoMap),
		"",
		"## Top co-change neighbors (last 200 commits)",
		embedSection(args.Refs, "leaf-briefing:co-change:"+ws, sections.CoChange),
		"",
		"## Direct import dependencies",
		embedSection(args.Refs, "leaf-briefing:imports:"+ws, sections.Imports),
		"",
		exemplarHeader,
		embedSection(args.Refs, "leaf-briefing:exemplars:"+ws, sections.Exemplars),
		"</leaf-briefing>",
	}, "\n")
}

// SelectSiblingExemplarsArgs mirrors selectSiblingExemplars' argument object.
// Limit nil represents both undefined and null, which take the `?? 3` default.
type SelectSiblingExemplarsArgs struct {
	FocusPaths []string           `json:"focusPaths"`
	AllPaths   []string           `json:"allPaths"`
	Limit      *float64           `json:"limit"`
	Activity   map[string]float64 `json:"activity"`
}

type scoredPath struct {
	path  string
	score float64
	order int
}

// SelectSiblingExemplars picks convention precedents using the source's
// directory/suffix eligibility and stable score/activity/path ordering.
func SelectSiblingExemplars(args SelectSiblingExemplarsArgs) []string {
	limit := 3.0
	if args.Limit != nil {
		limit = *args.Limit
	}
	if limit <= 0 {
		return []string{}
	}

	focus := normalizeNonempty(args.FocusPaths)
	focusSet := make(map[string]bool, len(focus))
	for _, path := range focus {
		focusSet[path] = true
	}
	all := normalizeNonempty(args.AllPaths)
	if len(focus) == 0 || len(all) == 0 {
		return []string{}
	}

	best := map[string]*scoredPath{}
	order := 0
	for _, focused := range focus {
		focusedDir := pathDir(focused)
		focusedExt := pathExt(focused)
		focusedSuffix := categorySuffix(focused)
		focusedStem := pathStem(focused)
		focusedCompound := strings.Contains(focusedSuffix, ".")
		for _, candidate := range all {
			if focusSet[candidate] {
				continue
			}
			sameDir := pathDir(candidate) == focusedDir && pathExt(candidate) == focusedExt
			sameSuffix := focusedCompound && categorySuffix(candidate) == focusedSuffix
			if !sameDir && !sameSuffix {
				continue
			}
			score := stemSimilarity(focusedStem, pathStem(candidate))
			if sameDir {
				score++
			}
			previous, ok := best[candidate]
			if !ok {
				best[candidate] = &scoredPath{path: candidate, score: score, order: order}
				order++
			} else if score > previous.score {
				previous.score = score
			}
		}
	}

	ranked := make([]scoredPath, 0, len(best))
	for _, candidate := range best {
		ranked = append(ranked, *candidate)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.order != b.order {
			// Restore JS Map insertion order before applying the stable sort.
			return a.order < b.order
		}
		return false
	})
	sort.SliceStable(ranked, func(i, j int) bool {
		return compareCandidates(ranked[i], ranked[j], args.Activity) < 0
	})

	ranked = sliceScored(ranked, limit)
	out := make([]string, len(ranked))
	for i, candidate := range ranked {
		out[i] = candidate.path
	}
	return out
}

func compareCandidates(a, b scoredPath, activity map[string]float64) float64 {
	if b.score != a.score {
		return b.score - a.score
	}
	av := activity[a.path]
	bv := activity[b.path]
	if bv != av {
		return bv - av
	}
	return float64(compareUTF16(a.path, b.path))
}

func sliceScored(items []scoredPath, end float64) []scoredPath {
	if !math.IsNaN(end) {
		n := float64(len(items))
		if end > n {
			end = n
		} else if end < -n {
			end = -n
		}
	}
	return jscompat.SliceTo(items, end)
}

func normalizeNonempty(paths []string) []string {
	out := []string{}
	for _, path := range paths {
		if normalized := normalizePath(path); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func pathDir(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}

func pathBase(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func pathExt(path string) string {
	base := pathBase(path)
	dot := strings.LastIndex(base, ".")
	if dot <= 0 {
		return ""
	}
	return jsLowerCase(base[dot:])
}

func categorySuffix(path string) string {
	base := pathBase(path)
	dot := strings.Index(base, ".")
	if dot < 0 {
		return ""
	}
	return jsLowerCase(base[dot+1:])
}

func pathStem(path string) []uint16 {
	base := pathBase(path)
	ext := pathExt(path)
	units := utf16.Encode([]rune(base))
	if ext != "" {
		// The TS subtracts the LOWERCASED extension's UTF-16 length from the
		// original basename. Full lowercasing can grow U+0130, so this odd
		// formulation is intentional.
		end := len(units) - utf16Length(ext)
		if end < 0 {
			end = 0
		}
		units = units[:end]
	}
	return lowerUTF16Prefix(units)
}

// lowerUTF16Prefix lowercases a prefix sliced at an arbitrary UTF-16 boundary.
// A prefix of a well-formed Go string can only be ill-formed by ending in a
// lone high surrogate; JS preserves that code unit through toLowerCase.
func lowerUTF16Prefix(units []uint16) []uint16 {
	if len(units) == 0 {
		return []uint16{}
	}
	var trailingHigh uint16
	if last := units[len(units)-1]; last >= 0xd800 && last <= 0xdbff {
		trailingHigh = last
		units = units[:len(units)-1]
	}
	lowered := utf16.Encode([]rune(jsLowerCase(string(utf16.Decode(units)))))
	if trailingHigh != 0 {
		lowered = append(lowered, trailingHigh)
	}
	return lowered
}

func stemSimilarity(a, b []uint16) float64 {
	n := min(len(a), len(b))
	common := 0
	for common < n && a[common] == b[common] {
		common++
	}
	longer := max(len(a), len(b))
	if longer == 0 {
		return 0
	}
	return float64(common) / float64(longer)
}

const jsSpace = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var focusPathRE = regexp.MustCompile(
	`(?:\A|[` + "\t\n\v\f\r " + `\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}` + "`\"'(<\\[]" + `)` +
		`((?:[A-Za-z0-9_.-]+/)+[A-Za-z0-9_.-]+\.[A-Za-z][A-Za-z0-9_]{0,5})`,
)

// ExtractFocusPaths mechanically extracts relative source paths. An omitted
// limit defaults to 12; extra variadic values are ignored like extra JS args.
func ExtractFocusPaths(text string, limits ...float64) []string {
	if text == "" {
		return []string{}
	}
	limit := 12.0
	if len(limits) > 0 {
		limit = limits[0]
	}
	out := []string{}
	seen := map[string]bool{}
	for _, match := range focusPathRE.FindAllStringSubmatch(text, -1) {
		path := normalizePath(match[1])
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
		if float64(len(out)) >= limit {
			break
		}
	}
	return out
}

func normalizePath(value string) string {
	value = strings.ReplaceAll(value, `\`, "/")
	value = strings.TrimPrefix(value, "./")
	return jscompat.Trim(value)
}

// RepoMapOptions is the subset of repo-map options leaf-briefing supplies.
type RepoMapOptions struct {
	BudgetChars float64
	FocusPaths  []string
}

// RepoMapBuilder is the narrow seam for the not-yet-ported repo-map module.
type RepoMapBuilder interface {
	BuildRepoMap(files []importgraph.SourceFile, opts RepoMapOptions) string
}

// WorkspaceGraphs is the cached graph set consumed by the pure rendering core.
type WorkspaceGraphs struct {
	RepoMapFiles []importgraph.SourceFile
	CoChange     *cochange.CoChangeGraph
	Imports      importgraph.ImportGraph
}

// WorkspaceGraphLoader isolates git and filesystem I/O for hand-written tests.
type WorkspaceGraphLoader interface {
	HeadSHA(workspace string) string
	BuildWorkspaceGraphs(workspace string) (WorkspaceGraphs, error)
}

// BuildLeafBriefingSectionsArgs mirrors the TS argument object.
type BuildLeafBriefingSectionsArgs struct {
	Workspace   string
	RunKey      string
	FocusPaths  []string
	BudgetChars *float64
}

type graphCacheEntry struct {
	done   chan struct{}
	graphs WorkspaceGraphs
	err    error
}

// Builder owns the promise-equivalent workspace graph cache.
type Builder struct {
	repoMap RepoMapBuilder
	loader  WorkspaceGraphLoader
	mu      sync.Mutex
	cache   map[string]*graphCacheEntry
}

// NewBuilder creates a leaf briefing builder. A nil loader selects the real
// git/filesystem shell.
func NewBuilder(repoMap RepoMapBuilder, loader WorkspaceGraphLoader) *Builder {
	if loader == nil {
		loader = defaultWorkspaceGraphLoader{}
	}
	return &Builder{
		repoMap: repoMap,
		loader:  loader,
		cache:   map[string]*graphCacheEntry{},
	}
}

func (b *Builder) cachedGraphs(key, workspace string) (WorkspaceGraphs, error) {
	b.mu.Lock()
	entry := b.cache[key]
	if entry == nil {
		entry = &graphCacheEntry{done: make(chan struct{})}
		b.cache[key] = entry
		b.mu.Unlock()
		entry.graphs, entry.err = b.loader.BuildWorkspaceGraphs(workspace)
		close(entry.done)
		return entry.graphs, entry.err
	}
	b.mu.Unlock()
	<-entry.done
	return entry.graphs, entry.err
}

// BuildLeafBriefingSections builds the cached graph-backed sections. Like the
// TS catch block, graph/render failures become nil.
func (b *Builder) BuildLeafBriefingSections(args BuildLeafBriefingSectionsArgs) (result *LeafBriefingSections) {
	defer func() {
		if recover() != nil {
			result = nil
		}
	}()
	if b == nil || b.repoMap == nil || b.loader == nil {
		return nil
	}
	key := args.Workspace + "\x00" + b.loader.HeadSHA(args.Workspace) + "\x00" + args.RunKey
	graphs, err := b.cachedGraphs(key, args.Workspace)
	if err != nil {
		return nil
	}

	focusPaths := normalizeNonempty(args.FocusPaths)
	activity := map[string]float64{}
	if graphs.CoChange != nil && graphs.CoChange.Totals != nil {
		for _, entry := range graphs.CoChange.Totals.Entries() {
			activity[entry.Key] = entry.Val
		}
	}
	exemplarPaths := SelectSiblingExemplars(SelectSiblingExemplarsArgs{
		FocusPaths: focusPaths,
		AllPaths: func() []string {
			out := make([]string, len(graphs.RepoMapFiles))
			for i, file := range graphs.RepoMapFiles {
				out[i] = file.Path
			}
			return out
		}(),
		Activity: activity,
	})
	exemplars := "(no analogous sibling files found)"
	if len(exemplarPaths) > 0 {
		lines := make([]string, len(exemplarPaths))
		for i, path := range exemplarPaths {
			lines[i] = "- " + path
		}
		exemplars = strings.Join(lines, "\n")
	}

	totalBudget := 1500.0
	if args.BudgetChars != nil {
		totalBudget = *args.BudgetChars
	}
	repoBudget := jsMax(300, totalBudget-float64(utf16Length(exemplars)))
	repoMap := b.repoMap.BuildRepoMap(graphs.RepoMapFiles, RepoMapOptions{
		BudgetChars: repoBudget,
		FocusPaths:  focusPaths,
	})
	if repoMap == "" {
		repoMap = "(no symbol map entries for the focused paths)"
	}

	relatedLines := []string{}
	for _, file := range focusPaths {
		k := 5.0
		neighbors := cochange.RelatedFiles(graphs.CoChange, file, &k)
		if len(neighbors) == 0 {
			continue
		}
		relatedLines = append(relatedLines, file+":")
		for _, item := range neighbors {
			relatedLines = append(relatedLines, "- "+item.Path+" ("+jscompat.ToFixed(float64(item.Score), 3)+")")
		}
	}
	if len(relatedLines) == 0 {
		relatedLines = []string{"(no co-change neighbors found)"}
	}

	importLines := []string{}
	for _, file := range focusPaths {
		deps := importgraph.DependenciesOf(graphs.Imports, file)
		if len(deps) == 0 {
			continue
		}
		importLines = append(importLines, file+":")
		for _, dep := range deps {
			importLines = append(importLines, "- "+dep)
		}
	}
	if len(importLines) == 0 {
		importLines = []string{"(no direct internal imports found)"}
	}

	return &LeafBriefingSections{
		RepoMap:   repoMap,
		CoChange:  strings.Join(relatedLines, "\n"),
		Imports:   strings.Join(importLines, "\n"),
		Exemplars: exemplars,
	}
}

// BuildLeafBriefing builds and formats the full briefing; nil is TS undefined.
func (b *Builder) BuildLeafBriefing(args BuildLeafBriefingSectionsArgs, refs ContextRefs) *string {
	sections := b.BuildLeafBriefingSections(args)
	if sections == nil {
		return nil
	}
	rendered := FormatLeafBriefing(FormatLeafBriefingArgs{
		Workspace: args.Workspace,
		Sections:  *sections,
		Refs:      refs,
	})
	return &rendered
}

func jsMax(values ...float64) float64 {
	for _, value := range values {
		if math.IsNaN(value) {
			return math.NaN()
		}
	}
	out := math.Inf(-1)
	for _, value := range values {
		if value > out || (value == 0 && out == 0 && !math.Signbit(value)) {
			out = value
		}
	}
	return out
}

type defaultWorkspaceGraphLoader struct{}

func (defaultWorkspaceGraphLoader) HeadSHA(workspace string) string {
	stdout, _ := runGit(workspace, "rev-parse", "HEAD")
	if trimmed := jscompat.Trim(stdout); trimmed != "" {
		return trimmed
	}
	return "no-head"
}

func (defaultWorkspaceGraphLoader) BuildWorkspaceGraphs(workspace string) (WorkspaceGraphs, error) {
	filesOutput, err := runGit(workspace, "ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return WorkspaceGraphs{}, err
	}
	paths := []string{}
	for _, raw := range regexp.MustCompile(`\r?\n`).Split(filesOutput, -1) {
		path := normalizePath(raw)
		if path == "" ||
			strings.HasPrefix(path, ".git/") ||
			strings.HasPrefix(path, ".codeaf/") ||
			strings.Contains(path, "/node_modules/") ||
			!sourceExtensions[pathExt(path)] {
			continue
		}
		paths = append(paths, path)
	}

	files := []importgraph.SourceFile{}
	for _, path := range paths {
		data, readErr := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(path)))
		if readErr != nil {
			continue
		}
		content := utf16SliceTo(string(data), 200_000)
		files = append(files, importgraph.SourceFile{Path: path, Content: content})
	}

	logOutput, err := runGit(workspace, "log", "--name-only", "--pretty=format:%H", "-200")
	if err != nil {
		return WorkspaceGraphs{}, err
	}
	return WorkspaceGraphs{
		RepoMapFiles: files,
		CoChange:     cochange.BuildCoChangeGraph(cochange.ParseGitLog(logOutput), cochange.BuildCoChangeOptions{}),
		Imports:      importgraph.BuildImportGraph(files, nil),
	}, nil
}

func runGit(workspace string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workspace
	stdout, err := cmd.Output()
	if err == nil {
		return string(stdout), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// Process.run({nothrow:true}) resolves on non-zero exit.
		return string(stdout), nil
	}
	return "", err
}

var sourceExtensions = map[string]bool{
	".c": true, ".cc": true, ".cpp": true, ".go": true, ".h": true,
	".hpp": true, ".java": true, ".js": true, ".jsx": true, ".json": true,
	".kt": true, ".mjs": true, ".py": true, ".rb": true, ".rs": true,
	".sql": true, ".swift": true, ".ts": true, ".tsx": true,
}

func utf16Length(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func utf16SliceTo(s string, n int) string {
	if n <= 0 {
		return ""
	}
	units := utf16.Encode([]rune(s))
	if n >= len(units) {
		return s
	}
	return string(utf16.Decode(units[:n]))
}

func compareUTF16(a, b string) int {
	au := utf16.Encode([]rune(a))
	bu := utf16.Encode([]rune(b))
	for i := 0; i < min(len(au), len(bu)); i++ {
		if au[i] < bu[i] {
			return -1
		}
		if au[i] > bu[i] {
			return 1
		}
	}
	switch {
	case len(au) < len(bu):
		return -1
	case len(au) > len(bu):
		return 1
	default:
		return 0
	}
}

func jsLowerCase(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}
	runes := []rune(s)
	var out strings.Builder
	out.Grow(len(s))
	for i, r := range runes {
		switch {
		case r == 0x0130:
			out.WriteString("i\u0307")
		case r == 0x03a3 && isFinalSigma(runes, i):
			out.WriteRune(0x03c2)
		default:
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
}

func isFinalSigma(runes []rune, i int) bool {
	j := i - 1
	for j >= 0 && isCaseIgnorable(runes[j]) {
		j--
	}
	if j < 0 || !isCased(runes[j]) {
		return false
	}
	k := i + 1
	for k < len(runes) && isCaseIgnorable(runes[k]) {
		k++
	}
	return k >= len(runes) || !isCased(runes[k])
}

func isCased(r rune) bool {
	return unicode.IsLower(r) || unicode.IsUpper(r) ||
		unicode.Is(unicode.Lt, r) ||
		unicode.Is(unicode.Other_Lowercase, r) ||
		unicode.Is(unicode.Other_Uppercase, r)
}

var caseIgnorablePunct = map[rune]bool{
	0x0027: true, 0x002e: true, 0x003a: true, 0x00b7: true, 0x0387: true,
	0x055f: true, 0x05f4: true, 0x2018: true, 0x2019: true, 0x2024: true,
	0x2027: true, 0xfe13: true, 0xfe52: true, 0xfe55: true, 0xff07: true,
	0xff0e: true, 0xff1a: true,
}

func isCaseIgnorable(r rune) bool {
	return caseIgnorablePunct[r] ||
		unicode.Is(unicode.Mn, r) ||
		unicode.Is(unicode.Me, r) ||
		unicode.Is(unicode.Cf, r) ||
		unicode.Is(unicode.Lm, r) ||
		unicode.Is(unicode.Sk, r)
}
