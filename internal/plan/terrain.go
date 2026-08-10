package plan

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The planner is blind by design. Every pass in this package is one text-in,
// JSON-out completion, which is what buys the four-round build and the
// byte-stable prefix behind it, and the price is that scope gets settled by
// fiat where a fact was one directory listing away — a goal about "the survey
// responses" is planned without anyone ever learning that there are four of
// them, in two formats, next to a half-written summary.
//
// Terrain closes that gap for the cheapest possible price: no call, no network,
// no turn. It is a picture of what the run's workspace actually holds, drawn in
// code and frozen, that rides the shared prefix every pass already pays for.
//
// Two rules govern everything below.
//
// It is not a coding feature. The material may be PDFs, spreadsheets, fetched
// pages, a draft in progress, or nothing at all, so nothing here names code, and
// git is one optional line that appears only where git already is.
//
// It must be byte-stable. The block joins the frozen preamble, so the same
// workspace has to render the same bytes every time or every cache hit behind it
// is lost — which is why every list here is sorted, every cap is a fixed count,
// and no map is ever ranged over into output. That last one is not theoretical:
// calibrationEvidence in recalibrate.go handed Go's randomized map iteration
// straight into a prompt and made the whole pass unreproducible.
const (
	// terrainBytes is the whole snapshot's ceiling. It is small on purpose: this
	// block is paid for by every planning call in the run, and a listing that
	// grows with the workspace would quietly become the most expensive sentence
	// in the system on the one goal that has the most material to think about.
	terrainBytes = 2 << 10

	// terrainNamedFiles is where loose files at the top level stop being worth
	// naming one by one. Below it the names are the information; above it the
	// shape is, and the names are just budget.
	terrainNamedFiles = 10

	// terrainDirs and terrainChildren bound the two levels. Both are fixed
	// counts rather than a byte budget so that the same workspace always renders
	// the same rows, whatever the lengths of the names in it.
	terrainDirs     = 12
	terrainChildren = 8

	// terrainExtensions is how many kinds of file a rollup names. Two or three
	// says what a directory is made of; a full census says nothing extra and
	// costs a line.
	terrainExtensions = 3

	// terrainWalkLimit bounds the rollup walk per top-level directory. A
	// workspace can hold a fetched archive of a hundred thousand files, and an
	// exact count of it is worth neither the syscalls nor the wait — past this
	// the count is reported as a floor.
	terrainWalkLimit = 4000

	// terrainReadmeBytes bounds the one line taken from a README-like file.
	// Whoever wrote it may have put a paragraph on the first line, and that
	// paragraph would eat the listing it was meant to introduce.
	terrainReadmeBytes = 160

	// terrainGitTimeout keeps a wedged git from stalling a build. Terrain is a
	// courtesy; nothing may wait on it.
	terrainGitTimeout = 2 * time.Second
)

// terrainBulk are directories whose contents are somebody else's material —
// installed dependencies, vendored copies, a compiler's cache. They are skipped
// outright rather than counted, and the choice is deliberate: a rollup saying
// "node_modules/ 41,882 files" is both the largest number in the picture and the
// least relevant one, and naming it invites a planner to treat it as scope. The
// cost of the choice is that a workspace can look emptier than it is, which is
// the safer direction to be wrong in.
var terrainBulk = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".git":         true,
}

// RenderTerrain draws what a workspace holds, for the planner to stand on.
//
// It is pure code: no model, no network, and no failure it can pass to its
// caller. Anything that goes wrong — a missing directory, an unreadable child,
// no git, no material at all — renders as fewer lines or as the empty string,
// because a caller with nothing to say about its workspace must produce prompts
// byte-identical to the ones it produced before terrain existed.
//
// The goal is read only as cues. Directories whose names the goal already says
// out loud are opened one level further, which is the whole of the "relevance"
// judgment here — deterministic string matching, so the same goal and the same
// workspace always draw the same picture.
func RenderTerrain(dir, goal string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	cues := terrainCues(goal)

	var lines []string
	if line := terrainGitLine(dir); line != "" {
		lines = append(lines, line)
	}
	if line := terrainReadmeLine(dir, entries); line != "" {
		lines = append(lines, line)
	}

	// os.ReadDir returns entries already sorted by name, which is the ordering
	// the whole render inherits — including which rows survive a cap.
	var directories, files []os.DirEntry
	for _, entry := range entries {
		if terrainSkipped(entry.Name()) {
			continue
		}
		if entry.IsDir() {
			directories = append(directories, entry)
			continue
		}
		files = append(files, entry)
	}

	shown := directories
	if len(shown) > terrainDirs {
		shown = shown[:terrainDirs]
	}
	for _, entry := range shown {
		path := filepath.Join(dir, entry.Name())
		lines = append(lines, terrainDirLine("", entry.Name(), terrainRollupOf(path)))
		if terrainCued(cues, entry.Name()) {
			lines = append(lines, terrainChildLines(path)...)
		}
	}
	if hidden := len(directories) - len(shown); hidden > 0 {
		lines = append(lines, fmt.Sprintf("… %d more %s", hidden, terrainDirectoryNoun(hidden)))
	}

	switch {
	case len(files) == 0:
	case len(files) <= terrainNamedFiles:
		for _, entry := range files {
			lines = append(lines, terrainFileLine("", entry))
		}
	default:
		lines = append(lines, terrainFilesRollup(files))
	}

	if len(lines) == 0 {
		return ""
	}
	// clipRunes is the same truncation the sentinel's prompt uses, and it is
	// here for the same reason: a snapshot cut mid-character would put a mangled
	// rune in front of every planning call in the run.
	return clipRunes(strings.Join(lines, "\n"), terrainBytes)
}

// terrainChildLines opens one directory a level further. The children are
// rendered exactly as the level above them is — a rollup for a directory, a name
// and a size for a file — so a reader learns the shape of the deeper level
// without learning a second notation for it.
func terrainChildLines(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var kept []os.DirEntry
	for _, entry := range entries {
		if !terrainSkipped(entry.Name()) {
			kept = append(kept, entry)
		}
	}
	shown := kept
	if len(shown) > terrainChildren {
		shown = shown[:terrainChildren]
	}
	lines := make([]string, 0, len(shown)+1)
	for _, entry := range shown {
		if entry.IsDir() {
			lines = append(lines, terrainDirLine("  ", entry.Name(), terrainRollupOf(filepath.Join(dir, entry.Name()))))
			continue
		}
		lines = append(lines, terrainFileLine("  ", entry))
	}
	if hidden := len(kept) - len(shown); hidden > 0 {
		lines = append(lines, fmt.Sprintf("  … %d more", hidden))
	}
	return lines
}

// terrainRollup is one directory said in a single line: how much is in it and
// what kind of thing it is made of.
type terrainRollup struct {
	files      int
	extensions []string
	// clipped says the walk stopped at terrainWalkLimit, so files is a floor
	// rather than a count. Saying which one it is matters: a planner told "4000
	// files" plans differently from one told "over 4000".
	clipped bool
}

// terrainRollupOf counts a directory whole, because the interesting number is
// what is in there rather than what happens to sit at its top. The walk is
// breadth-first over already-sorted entries so that hitting the limit truncates
// the same way every time.
func terrainRollupOf(root string) terrainRollup {
	var rollup terrainRollup
	counts := map[string]int{}
	visited := 0
	queue := []string{root}
	for len(queue) > 0 && !rollup.clipped {
		current := queue[0]
		queue = queue[1:]
		entries, err := os.ReadDir(current)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if terrainSkipped(entry.Name()) {
				continue
			}
			if visited >= terrainWalkLimit {
				rollup.clipped = true
				break
			}
			visited++
			if entry.IsDir() {
				queue = append(queue, filepath.Join(current, entry.Name()))
				continue
			}
			rollup.files++
			if extension := terrainExtensionOf(entry.Name()); extension != "" {
				counts[extension]++
			}
		}
	}
	rollup.extensions = terrainTopExtensions(counts)
	return rollup
}

// terrainTopExtensions names what a pile is made of, commonest first, ties
// broken alphabetically. The sort is the point — the counts arrive in a map, and
// ranging over one into a prompt is the reproducibility bug this file's header
// cites.
func terrainTopExtensions(counts map[string]int) []string {
	type tally struct {
		extension string
		count     int
	}
	tallies := make([]tally, 0, len(counts))
	for extension, count := range counts {
		tallies = append(tallies, tally{extension, count})
	}
	sort.Slice(tallies, func(i, j int) bool {
		if tallies[i].count != tallies[j].count {
			return tallies[i].count > tallies[j].count
		}
		return tallies[i].extension < tallies[j].extension
	})
	if len(tallies) > terrainExtensions {
		tallies = tallies[:terrainExtensions]
	}
	top := make([]string, 0, len(tallies))
	for _, entry := range tallies {
		top = append(top, entry.extension)
	}
	return top
}

func terrainDirLine(indent, name string, rollup terrainRollup) string {
	label := indent + name + "/"
	body := "empty"
	if rollup.files == 1 {
		body = "1 file"
	} else if rollup.files > 1 {
		body = fmt.Sprintf("%d files", rollup.files)
	}
	if rollup.clipped {
		body = "over " + body
	}
	if len(rollup.extensions) > 0 {
		body += " (" + strings.Join(rollup.extensions, ", ") + ")"
	}
	return fmt.Sprintf("%-24s %s", label, body)
}

// terrainFileLine names a file and says how big it is. The size is the second
// half of the fact: "notes.md" says almost nothing, and "notes.md 41 B" says it
// is a stub while "notes.md 84.2 KB" says it is the material.
func terrainFileLine(indent string, entry os.DirEntry) string {
	label := indent + entry.Name()
	info, err := entry.Info()
	if err != nil {
		// The entry was there a moment ago and is not now, or cannot be stat'ed.
		// The name is still true, and half a fact beats dropping the row.
		return label
	}
	return fmt.Sprintf("%-24s %s", label, terrainSize(info.Size()))
}

func terrainFilesRollup(files []os.DirEntry) string {
	counts := map[string]int{}
	for _, entry := range files {
		if extension := terrainExtensionOf(entry.Name()); extension != "" {
			counts[extension]++
		}
	}
	line := fmt.Sprintf("%d files at the top level", len(files))
	if top := terrainTopExtensions(counts); len(top) > 0 {
		line += " (" + strings.Join(top, ", ") + ")"
	}
	return line
}

func terrainSize(size int64) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(size)/(1<<10))
	default:
		return fmt.Sprintf("%d B", size)
	}
}

// terrainExtensionOf reads the kind of a file off its name, lower-cased so that
// .CSV and .csv are one kind rather than two. A dotfile has no extension here —
// it is skipped before this is ever asked — and a name with no dot at all is
// reported as no kind rather than as itself, which keeps a directory of
// extensionless files from rendering a rollup listing every one of them.
func terrainExtensionOf(name string) string {
	extension := strings.ToLower(filepath.Ext(name))
	if extension == "" || extension == "." {
		return ""
	}
	return extension
}

// terrainSkipped is the one exclusion rule, applied at every level: nothing that
// starts with a dot, and nothing in terrainBulk. Dotfiles go because they are
// the workspace's plumbing rather than its material, and because .git in
// particular would otherwise dwarf everything the run is actually about.
func terrainSkipped(name string) bool {
	return strings.HasPrefix(name, ".") || terrainBulk[name]
}

// terrainCues splits the goal into the words a directory name could plausibly
// answer to. Short words are dropped because they match everything: a two-letter
// cue would open a directory per goal on nothing but coincidence.
func terrainCues(goal string) map[string]bool {
	cues := map[string]bool{}
	fields := strings.FieldsFunc(strings.ToLower(goal), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, field := range fields {
		if utf8.RuneCountInString(field) >= 3 {
			cues[field] = true
		}
	}
	return cues
}

// terrainCued decides whether the goal already named this directory.
//
// The match is equality, with one plural either way, and it is strict on
// purpose. A substring rule reads far more generously than it sounds: "data"
// occurs inside "metadata" and "updates", so a goal mentioning it would open
// directories the goal never referred to, and the budget for the level that
// matters would go to the level that does not.
//
// The map here is read, never ranged over, so it costs no determinism.
func terrainCued(cues map[string]bool, name string) bool {
	name = strings.ToLower(name)
	if utf8.RuneCountInString(name) < 3 {
		return false
	}
	if cues[name] || cues[name+"s"] {
		return true
	}
	return strings.HasSuffix(name, "s") && cues[strings.TrimSuffix(name, "s")]
}

// terrainReadmeLine takes the one sentence whoever built this workspace wrote to
// explain it. It is worth more per byte than any listing: a directory of forty
// PDFs says forty PDFs, and its first README line says what they are.
func terrainReadmeLine(dir string, entries []os.DirEntry) string {
	for _, entry := range entries {
		if entry.IsDir() || !terrainIsReadme(entry.Name()) {
			continue
		}
		opening := terrainOpeningLine(filepath.Join(dir, entry.Name()))
		if opening == "" {
			continue
		}
		return entry.Name() + ": " + clipRunes(opening, terrainReadmeBytes)
	}
	return ""
}

func terrainIsReadme(name string) bool {
	lowered := strings.ToLower(name)
	return strings.HasPrefix(lowered, "readme") || lowered == "claude.md"
}

// terrainOpeningLine reads the first line with anything on it, with heading
// punctuation stripped so that "# The 2024 filings" arrives as the sentence it
// is. Only the head of the file is read: the first line is all that is wanted,
// and a workspace is allowed to contain a very large file with a very long one.
func terrainOpeningLine(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	head := make([]byte, 4<<10)
	read, _ := file.Read(head)
	if read <= 0 {
		return ""
	}
	for _, line := range strings.Split(string(head[:read]), "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#>*- \t"))
		if line != "" {
			return line
		}
	}
	return ""
}

// terrainGitLine is the one line about version control, and it is written to be
// absent by default. A workspace holding a corpus of documents has no git in it,
// most machines running this have no reason to, and neither case is a fault — so
// every path out of here that is not a clean answer is the empty string.
func terrainGitLine(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return ""
	}
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), terrainGitTimeout)
	defer cancel()

	branch, ok := terrainGitOutput(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if !ok || branch == "" {
		return ""
	}
	where := "branch " + branch
	if branch == "HEAD" {
		where = "detached head"
	}
	status, ok := terrainGitOutput(ctx, dir, "status", "--porcelain")
	if !ok {
		return ""
	}
	changed, untracked := 0, 0
	for _, line := range strings.Split(status, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
		case strings.HasPrefix(line, "??"):
			untracked++
		default:
			changed++
		}
	}
	if changed == 0 && untracked == 0 {
		return "git: " + where + ", nothing changed"
	}
	return fmt.Sprintf("git: %s, %d changed, %d untracked", where, changed, untracked)
}

// terrainGitOutput runs one read-only git command and reports whether the answer
// is usable. A non-zero exit, a missing repository, a timeout and a binary that
// is not really git all arrive here the same way and all mean the same thing:
// say nothing about git at all.
func terrainGitOutput(ctx context.Context, dir string, args ...string) (string, bool) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// Git reads configuration and, for some subcommands, opens a pager. Neither
	// belongs in a snapshot render, and a pager waiting for a terminal is how a
	// courtesy becomes a hang.
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_OPTIONAL_LOCKS=0")
	output, err := command.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(output)), true
}

func terrainDirectoryNoun(count int) string {
	if count == 1 {
		return "directory"
	}
	return "directories"
}
