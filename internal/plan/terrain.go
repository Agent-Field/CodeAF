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

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
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
// Three rules govern everything below.
//
// It is not a coding feature. The material may be PDFs, spreadsheets, fetched
// pages, a draft in progress, or nothing at all, so nothing here names code, and
// git is one optional line that appears only where git already is.
//
// It must be byte-stable, and that is a hard requirement rather than a
// preference. The block joins the frozen preamble, so the same workspace has to
// render the same bytes every time or every cache hit behind it is lost — which
// is why every list here is sorted before anything is dropped from it, every cap
// is a fixed count, and no map is ever ranged over into output. That last one is
// not theoretical: calibrationEvidence in recalibrate.go handed Go's randomized
// map iteration straight into a prompt and made the whole pass unreproducible.
// Reading a prefix of a directory in filesystem order is the same bug wearing
// different clothes, which is why nothing here does it.
//
// It must be cheap. Directories are read for names and types only — the byte
// sizes that need a stat are fetched for the handful of rows that actually get
// rendered — and one absurd directory is counted rather than described.
const (
	// terrainBytes is the whole snapshot's ceiling, ellipsis included. It is
	// small on purpose: this block is paid for by every planning call in the run,
	// and a listing that grows with the workspace would quietly become the most
	// expensive sentence in the system on the one goal that has the most material
	// to think about.
	terrainBytes = 2 << 10

	// terrainNamedFiles is where loose files at the top level stop being worth
	// naming one by one. Below it the names are the information; above it the
	// shape is, and the names are just budget.
	terrainNamedFiles = 10

	// terrainDirs and terrainChildren bound the two levels. Both are fixed
	// counts rather than a byte budget so that the same workspace always renders
	// the same rows, whatever the lengths of the names in it. They are applied to
	// a fully-read, sorted listing — never to whatever the filesystem happened to
	// hand over first.
	terrainDirs     = 12
	terrainChildren = 8

	// terrainExtensions is how many kinds of file a rollup names. Two or three
	// says what a directory is made of; a full census says nothing extra and
	// costs a line.
	terrainExtensions = 3

	// terrainNameCap is the one directory size this refuses to describe. Names
	// are cheap — a hundred thousand of them is a few megabytes of strings and no
	// stat calls — so the cap is set where holding the list stops being sensible
	// rather than where reading it does. Past it the directory is still counted
	// to the last entry, because a count does not depend on the order the
	// filesystem returns, and a count is therefore something that can be said
	// about it reproducibly. A listing is not.
	terrainNameCap = 100_000

	// terrainReadBatch is how many entries are pulled per call. Batching is what
	// makes the cap enforceable: os.ReadDir would read and sort the whole
	// directory before any guard could run.
	terrainReadBatch = 4096

	// terrainWalkLimit bounds the rollup walk under one top-level directory. A
	// workspace can hold a fetched archive of a hundred thousand files, and an
	// exact count of it is worth neither the syscalls nor the wait. Stopping here
	// is deterministic because the walk consumes fully-sorted directories in a
	// fixed order, so the same tree always stops at the same place — and the line
	// it renders says plainly that it stopped.
	terrainWalkLimit = 4000

	// terrainReadmeBytes bounds the one line taken from a README-like file,
	// ellipsis included. Whoever wrote it may have put a paragraph on the first
	// line, and that paragraph would eat the listing it was meant to introduce.
	terrainReadmeBytes = 160

	// terrainReadmeHead is how much of such a file is read to find that line.
	terrainReadmeHead = 4 << 10

	// terrainGitTimeout keeps a wedged git from stalling a build, and
	// terrainGitWaitDelay bounds the wait after it is killed. Terrain is a
	// courtesy; nothing may wait on it.
	terrainGitTimeout   = 2 * time.Second
	terrainGitWaitDelay = 500 * time.Millisecond
)

// terrainClipped is what a truncation leaves behind. It is one rune, and it is
// charged against every budget it appears in — a cap that excludes its own
// marker is not a cap.
const terrainClipped = "…"

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
	return renderTerrain(dir, goal, terrainNameCap)
}

// renderTerrain is the body, with the one size the tests cannot afford to build
// for real left as a parameter.
func renderTerrain(dir, goal string, nameCap int) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	scan := terrainScan{cues: terrainCues(goal), nameCap: nameCap}
	listing := scan.read(dir)
	if listing.count == 0 {
		return ""
	}

	var lines []string
	if line := terrainGitLine(dir); line != "" {
		lines = append(lines, line)
	}
	// A directory too large to hold is described by the one thing that can be
	// said about it reproducibly. Nothing is listed and nothing is opened: any
	// selection would be a selection from an order the filesystem chose.
	if listing.capped {
		return terrainClip(strings.Join(append(lines,
			fmt.Sprintf("%d entries at the top level, too many to list", listing.count)), "\n"), terrainBytes)
	}
	if line := terrainReadmeLine(dir, listing.entries); line != "" {
		lines = append(lines, line)
	}

	var directories, files []os.DirEntry
	for _, entry := range listing.entries {
		if entry.IsDir() {
			directories = append(directories, entry)
			continue
		}
		files = append(files, entry)
	}

	shown := scan.choose(directories)
	for _, entry := range shown {
		path := filepath.Join(dir, entry.Name())
		lines = append(lines, terrainDirLine("", entry.Name(), scan.rollup(path)))
		if scan.cued(entry.Name()) {
			lines = append(lines, scan.children(path)...)
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
	return terrainClip(strings.Join(lines, "\n"), terrainBytes)
}

// terrainScan is one render's reading of the disk: the goal's cues and the size
// past which a directory is counted rather than described.
type terrainScan struct {
	cues    map[string]bool
	nameCap int
}

// terrainListing is one directory as read: everything in it, sorted, unless
// there was too much to hold — in which case there is only the count.
type terrainListing struct {
	entries []os.DirEntry
	count   int
	capped  bool
}

// read takes a directory whole and sorts it before anything is dropped.
//
// The order matters more than the cost here. An earlier version read a bounded
// prefix and sorted that, which meant the entries that survived were whichever
// ones the filesystem happened to return first — so the same unchanged workspace
// could render different bytes on two consecutive calls, and a directory the
// goal had named by name could be discarded before the cue match was ever asked
// about it. Reading everything and sorting first makes both impossible.
//
// It is affordable because it is names and types only: os.File.ReadDir carries
// the kind of each entry back with its name, and the only stat in this file is
// the one that fetches a size for a row that is actually being rendered.
func (s terrainScan) read(dir string) terrainListing {
	handle, err := os.Open(dir)
	if err != nil {
		return terrainListing{}
	}
	defer handle.Close()

	var listing terrainListing
	for {
		batch, err := handle.ReadDir(terrainReadBatch)
		for _, entry := range batch {
			if terrainSkipped(entry.Name()) {
				continue
			}
			listing.count++
			if listing.capped {
				continue
			}
			if len(listing.entries) >= s.nameCap {
				// Past the cap the list is dropped rather than trimmed. A
				// trimmed list is a sample of an arbitrary order; the count
				// keeps running because a count is order-independent and can
				// therefore still be reported reproducibly.
				listing.entries, listing.capped = nil, true
				continue
			}
			listing.entries = append(listing.entries, entry)
		}
		if err != nil || len(batch) == 0 {
			break
		}
	}
	sort.Slice(listing.entries, func(i, j int) bool {
		return listing.entries[i].Name() < listing.entries[j].Name()
	})
	return listing
}

// choose picks which directories get a row when there are more than fit.
//
// The cue-matched ones are taken first and the rest fills alphabetically, which
// is the fix for a real hole: the selection used to truncate alphabetically and
// only then ask which names the goal had mentioned, so in a workspace with more
// directories than fit — which is every workspace the cue was written for — the
// one the goal was about could be dropped before it was ever considered. Cue
// matching now runs over the whole sorted listing, and the rows are re-sorted
// afterwards so the render stays alphabetical whichever way the cues fell.
func (s terrainScan) choose(directories []os.DirEntry) []os.DirEntry {
	if len(directories) <= terrainDirs {
		return directories
	}
	shown := make([]os.DirEntry, 0, terrainDirs)
	for _, entry := range directories {
		if len(shown) < terrainDirs && s.cued(entry.Name()) {
			shown = append(shown, entry)
		}
	}
	for _, entry := range directories {
		if len(shown) >= terrainDirs {
			break
		}
		if !s.cued(entry.Name()) {
			shown = append(shown, entry)
		}
	}
	sort.Slice(shown, func(i, j int) bool { return shown[i].Name() < shown[j].Name() })
	return shown
}

// children opens one directory a level further. The children are rendered
// exactly as the level above them is — a rollup for a directory, a name and a
// size for a file — so a reader learns the shape of the deeper level without
// learning a second notation for it.
func (s terrainScan) children(dir string) []string {
	listing := s.read(dir)
	if listing.capped {
		return []string{fmt.Sprintf("  %d entries, too many to list", listing.count)}
	}
	shown := listing.entries
	if len(shown) > terrainChildren {
		shown = shown[:terrainChildren]
	}
	lines := make([]string, 0, len(shown)+1)
	for _, entry := range shown {
		if entry.IsDir() {
			lines = append(lines, terrainDirLine("  ", entry.Name(), s.rollup(filepath.Join(dir, entry.Name()))))
			continue
		}
		lines = append(lines, terrainFileLine("  ", entry))
	}
	if hidden := len(listing.entries) - len(shown); hidden > 0 {
		lines = append(lines, fmt.Sprintf("  … %d more", hidden))
	}
	return lines
}

// terrainRollup is one directory said in a single line: how much is in it and
// what kind of thing it is made of.
type terrainRollup struct {
	files       int
	directories int
	extensions  []string
	// unread says the walk did not reach the bottom, so the counts above are
	// floors. unreadEntries is how many entries are known to be under there
	// unaccounted for, which is knowable only where a directory was counted but
	// not held; zero means the shortfall has no number.
	unread        bool
	unreadEntries int
}

// label says exactly what was counted and nothing beyond it.
//
// It used to turn "the walk stopped early" into "over N files", which is an
// inference the walk never made: a tree of thousands of directories holding one
// file rendered as "over 1 file", and a stop before any file was seen rendered
// as "over 0 empty" — the opposite of the truth in both directions. What is
// known is the counts that were actually taken and the fact that something was
// not reached, so that is what it says, in that order.
func (r terrainRollup) label() string {
	var body string
	switch {
	case r.files == 1:
		body = "1 file"
	case r.files > 1:
		body = fmt.Sprintf("%d files", r.files)
	case r.directories > 0:
		body = "no files, " + terrainDirectoryCount(r.directories)
	case r.unread:
		body = "not read"
	default:
		body = "empty"
	}
	if len(r.extensions) > 0 {
		body += " (" + strings.Join(r.extensions, ", ") + ")"
	}
	switch {
	case !r.unread || body == "not read":
	case r.unreadEntries > 0:
		body += fmt.Sprintf(", %d entries unread", r.unreadEntries)
	default:
		body += ", more unread"
	}
	return body
}

// rollup counts a directory whole, because the interesting number is what is in
// there rather than what happens to sit at its top.
//
// The walk is breadth-first over directories that were each read and sorted in
// full, and the queue is filled in that sorted order, so the traversal order is
// a property of the tree rather than of the filesystem's mood. That is what
// makes stopping at terrainWalkLimit safe: the same tree stops at the same
// entry every time, and the line says that it stopped.
func (s terrainScan) rollup(root string) terrainRollup {
	var rollup terrainRollup
	counts := map[string]int{}
	remaining := terrainWalkLimit
	queue := []string{root}
	for len(queue) > 0 {
		if remaining <= 0 {
			rollup.unread = true
			break
		}
		current := queue[0]
		queue = queue[1:]
		listing := s.read(current)
		if listing.capped {
			// Counted but not held. The entries are real and their number is
			// exact; what they are is the thing that cannot be said.
			rollup.unread = true
			rollup.unreadEntries += listing.count
			continue
		}
		for _, entry := range listing.entries {
			if remaining <= 0 {
				rollup.unread = true
				break
			}
			remaining--
			if entry.IsDir() {
				rollup.directories++
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
	return fmt.Sprintf("%-24s %s", indent+name+"/", rollup.label())
}

// terrainFileLine names a file and says how big it is. The size is the second
// half of the fact: "notes.md" says almost nothing, and "notes.md 41 B" says it
// is a stub while "notes.md 84.2 KB" says it is the material. This is the only
// place anything is stat'ed, and it runs on rendered rows alone.
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

// terrainClip enforces a byte ceiling that includes the marker it adds.
//
// The package's clipRunes cuts at a rune boundary but appends its ellipsis
// afterwards, so its result can exceed the limit it was given by three bytes.
// That is harmless where it is used — a per-node slice of a much larger budget —
// and it is not harmless here, where the number is the promise made to every
// planning call in the run. So the marker is charged first and clipRunes is
// asked for the smaller number.
func terrainClip(body string, limit int) string {
	if len(body) <= limit {
		return body
	}
	if limit <= len(terrainClipped) {
		return ""
	}
	return clipRunes(body, limit-len(terrainClipped))
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

// terrainWords splits a phrase into the words a name could answer to, and it is
// the single tokenizer both sides of the cue match go through.
//
// Both sides is the correction that matters. Only the goal used to be split, and
// a directory name was compared whole, so "survey-responses/" could not match a
// goal that said "survey responses" in any form — the separator alone defeated
// it.
//
// The normalisation is a fold, not a lowercasing, and the difference is not
// pedantic: strings.ToLower leaves "Straße" and "STRASSE" as different words and
// leaves a Greek word ending in a final sigma different from the same word
// ending in a medial one, so a directory named in one form could never answer to
// a goal that named it in the other. NFC first, because two byte sequences for
// one accented character are the same word and a byte comparison would call them
// strangers.
func terrainWords(phrase string) []string {
	folded := cases.Fold().String(norm.NFC.String(phrase))
	fields := strings.FieldsFunc(folded, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	words := make([]string, 0, len(fields))
	for _, field := range fields {
		// Short words match everything: a two-letter cue would open a directory
		// per goal on nothing but coincidence.
		if utf8.RuneCountInString(field) >= 3 {
			words = append(words, field)
		}
	}
	return words
}

// terrainCues is the goal's half of the match. The map is only ever read, so it
// costs no determinism.
func terrainCues(goal string) map[string]bool {
	cues := map[string]bool{}
	for _, word := range terrainWords(goal) {
		cues[word] = true
	}
	return cues
}

// cued decides whether the goal already named this directory.
//
// The match is equality between fold-keys, with one plural fold, and it is
// strict on purpose. A substring rule reads far more generously than it sounds:
// "data" occurs inside "metadata" and "updates", so a goal mentioning it would
// open directories the goal never referred to, and the budget for the level that
// matters would go to the level that does not.
//
// The whole trade is deliberately lopsided. A miss costs one unopened directory
// in a picture that is admittedly incomplete and says so; a false expansion
// spends the cap — the budget every planning call in the run pays for — on
// material nobody asked about, and pushes out the rows that were the point. So
// every rule here is written to prefer missing.
func (s terrainScan) cued(name string) bool {
	for _, word := range terrainWords(name) {
		if terrainMatchesCue(s.cues, word) {
			return true
		}
	}
	return false
}

// terrainMatchesCue is equality, then one plural either way — but only on stems
// long enough for a trailing "s" to be a plural rather than the word.
//
// Without that floor the fold is a false-expansion engine: a goal saying
// "report" opened "news/", because dropping the s leaves "new", and a goal
// saying "the" would reach anything ending in "thes". Four runes is where the
// accidents stop and the real plurals ("responses"/"response",
// "archive"/"archives") all still land.
func terrainMatchesCue(cues map[string]bool, word string) bool {
	if cues[word] {
		return true
	}
	if stem, found := strings.CutSuffix(word, "s"); found &&
		utf8.RuneCountInString(stem) >= 4 && cues[stem] {
		return true
	}
	return utf8.RuneCountInString(word) >= 4 && cues[word+"s"]
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
		return entry.Name() + ": " + terrainClip(opening, terrainReadmeBytes)
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
//
// That head is a byte count, so it can land in the middle of a character — a
// first line of accented prose, cut at 4096 bytes, ends in half a rune. The
// whole render must be valid UTF-8 or every planning call in the run carries the
// mangled byte, so the fragment is dropped before anything is read out of it.
func terrainOpeningLine(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	head := make([]byte, terrainReadmeHead)
	read, _ := file.Read(head)
	if read <= 0 {
		return ""
	}
	for _, line := range strings.Split(strings.ToValidUTF8(string(head[:read]), ""), "\n") {
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
//
// Whether git is there is asked of git rather than of the filesystem. Statting
// .git answered only for a directory that is itself the top of a tree, so a run
// pointed at any directory inside one lost the line entirely — and a linked
// worktree, where .git is a file, was a coin flip.
func terrainGitLine(dir string) string {
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), terrainGitTimeout)
	defer cancel()

	if inside, ok := terrainGitOutput(ctx, dir, "rev-parse", "--is-inside-work-tree"); !ok || inside != "true" {
		return ""
	}
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
// is usable. A non-zero exit, a directory that is not in a tree, a timeout and a
// binary that is not really git all arrive here the same way and all mean the
// same thing: say nothing about git at all.
func terrainGitOutput(ctx context.Context, dir string, args ...string) (string, bool) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// Git reads configuration and, for some subcommands, opens a pager. Neither
	// belongs in a snapshot render, and a pager waiting for a terminal is how a
	// courtesy becomes a hang.
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_OPTIONAL_LOCKS=0")
	// The timeout alone does not bound this. Killing git on deadline leaves any
	// child of git — a credential helper, a hook, a pager that ignored the
	// environment — still holding the write end of the pipe, and Output() waits
	// on the pipe rather than on the process it killed. WaitDelay is the only
	// thing that closes that door.
	command.WaitDelay = terrainGitWaitDelay
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

func terrainDirectoryCount(count int) string {
	return fmt.Sprintf("%d %s", count, terrainDirectoryNoun(count))
}
