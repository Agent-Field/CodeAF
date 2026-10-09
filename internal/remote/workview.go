package remote

// workview.go is the engine half of the desktop's file and diff tabs: read one
// workspace file as text, list files matching a query, say what changed against
// the base, and give one file's hunks. All four are read-only and answer under
// ONE boundary: the session's workspace, symlinks resolved before anything is
// compared. Unlike [MethodFetchFile] the session's own folder is NOT reachable
// here; a tab shows the person's work, not the engine's records.
//
// THE BASE IS GIT'S HEAD. The engine keeps no second ledger of "what the run
// changed": the working tree against HEAD is what a person's editor and a
// landing both see. A workspace outside git answers Git=false and no files.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	maxTextBytes     = 1 << 20 // the most one file may be shown as text
	maxDiffLines     = 5000    // the most diff lines one answer carries
	maxChangedFiles  = 500
	maxFindCandidate = 100000
	defaultFindLimit = 20
	maxFindLimit     = 100
	gitDeadline      = 8 * time.Second
	sniffBytes       = 8000
)

// ReadTextArgs names a workspace file; relative means the workspace.
type ReadTextArgs struct {
	Path string `json:"path"`
}

// TextFile is one file for the File tab. Refusal is "binary" or "too-large"
// and then Text is empty; Message is the sentence to show.
type TextFile struct {
	Path     string `json:"path"` // workspace-relative, slash separated
	Name     string `json:"name"`
	Dir      string `json:"dir"`
	Abs      string `json:"abs"` // absolute on the ENGINE's disk
	Size     int64  `json:"size"`
	Hash     string `json:"hash,omitempty"`
	Language string `json:"language,omitempty"` // lowercase extension, no dot
	Lines    int    `json:"lines"`
	Text     string `json:"text"`
	Refusal  string `json:"refusal,omitempty"`
	Message  string `json:"message,omitempty"`
}

// FindFilesArgs is a query for the command field's Matching rows.
type FindFilesArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// FoundFile is one match.
type FoundFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

// FoundFiles are the best matches first.
type FoundFiles struct {
	Files     []FoundFile `json:"files"`
	Truncated bool        `json:"truncated,omitempty"`
}

// DiffChangesArgs optionally narrows the list to the paths a conversation touched.
type DiffChangesArgs struct {
	Paths []string `json:"paths,omitempty"`
}

// ChangedFile is one row of "What changed". Status is modified, added,
// deleted or untracked.
type ChangedFile struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary,omitempty"`
}

// ChangedFiles is the list plus what it was measured against.
type ChangedFiles struct {
	Git       bool          `json:"git"`
	Base      string        `json:"base,omitempty"` // short HEAD, "" when unborn
	Branch    string        `json:"branch,omitempty"`
	Files     []ChangedFile `json:"files"`
	Added     int           `json:"added"`
	Deleted   int           `json:"deleted"`
	Truncated bool          `json:"truncated,omitempty"`
}

// DiffFileArgs names one file.
type DiffFileArgs struct {
	Path    string `json:"path"`
	Context int    `json:"context,omitempty"` // default 3
}

// DiffLine is one row of a hunk. Kind is context, add or del; Old and New are
// the two line-number columns (0 means that column is blank).
type DiffLine struct {
	Kind string `json:"kind"`
	Old  int    `json:"old,omitempty"`
	New  int    `json:"new,omitempty"`
	Text string `json:"text"`
}

// DiffHunk is one "@@" block. Section is the text after the second "@@".
type DiffHunk struct {
	Header   string     `json:"header"`
	OldStart int        `json:"oldStart"`
	OldLines int        `json:"oldLines"`
	NewStart int        `json:"newStart"`
	NewLines int        `json:"newLines"`
	Section  string     `json:"section,omitempty"`
	Lines    []DiffLine `json:"lines"`
}

// FileDiff is the Changes view of one file. Lines is the current file's line
// count (0 when deleted or unknown), so a surface can draw the trailing
// "N unchanged lines" fold from the gap after the last hunk.
type FileDiff struct {
	Path      string     `json:"path"`
	Name      string     `json:"name"`
	Dir       string     `json:"dir"`
	Abs       string     `json:"abs"`
	Git       bool       `json:"git"`
	Status    string     `json:"status"` // clean, modified, added, deleted, untracked
	Added     int        `json:"added"`
	Deleted   int        `json:"deleted"`
	Binary    bool       `json:"binary,omitempty"`
	Lines     int        `json:"lines"`
	Hunks     []DiffHunk `json:"hunks"`
	Truncated bool       `json:"truncated,omitempty"`
}

// workspaceRoot is the resolved workspace, or an error when it cannot be seen.
func (s *server) workspaceRoot() (string, error) {
	workspace, _ := s.session.folder()
	real, err := filepath.EvalSymlinks(workspace)
	if err != nil || strings.TrimSpace(workspace) == "" {
		return "", errors.New("engine: this conversation has no readable workspace")
	}
	return real, nil
}

// confine resolves a name to a path inside the workspace. A path that does not
// exist (a deleted file) is judged by its nearest existing parent, so a missing
// leaf can still be diffed while a traversal or an escaping link cannot.
func confine(root, asked string) (abs, rel string, err error) {
	path := strings.TrimSpace(asked)
	if path == "" {
		return "", "", errors.New("engine: nothing was named")
	}
	if strings.ContainsRune(path, 0) {
		return "", "", errors.New("engine: that is not a path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	tail := ""
	probe := path
	for {
		real, e := filepath.EvalSymlinks(probe)
		if e == nil {
			probe = real
			break
		}
		if !errors.Is(e, fs.ErrNotExist) || probe == filepath.Dir(probe) {
			return "", "", fmt.Errorf("engine: no such file: %s", asked)
		}
		tail = filepath.Join(filepath.Base(probe), tail)
		probe = filepath.Dir(probe)
	}
	abs = filepath.Join(probe, tail)
	r, e := filepath.Rel(root, abs)
	if e != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("engine: %s is outside this conversation's workspace, and nothing outside it crosses this connection", asked)
	}
	return abs, filepath.ToSlash(r), nil
}

func splitPath(rel string) (name, dir string) {
	name = rel
	if at := strings.LastIndex(rel, "/"); at >= 0 {
		name, dir = rel[at+1:], rel[:at]
	}
	return
}

func extLanguage(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

func looksBinary(data []byte) bool {
	head := data
	if len(head) > sniffBytes {
		head = head[:sniffBytes]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	// A cut can land inside a rune; trim up to 3 trailing bytes before judging.
	for cut := 0; cut < 4 && cut <= len(head); cut++ {
		if utf8.Valid(head[:len(head)-cut]) {
			return false
		}
	}
	return true
}

func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}

func (s *server) readText(call Frame) (json.RawMessage, error) {
	args, err := arg[ReadTextArgs](call)
	if err != nil {
		return nil, err
	}
	root, err := s.workspaceRoot()
	if err != nil {
		return nil, err
	}
	// Workspace only: the zero Place has no folder, so handOver's second root is empty.
	real, info, err := handOver(session.Place{}, root, args.Path)
	if err != nil {
		return nil, err
	}
	abs, rel, err := confine(root, real)
	if err != nil {
		return nil, err
	}
	name, dir := splitPath(rel)
	file := TextFile{Path: rel, Name: name, Dir: dir, Abs: abs, Size: info.Size(), Language: extLanguage(name)}
	if info.Size() > maxTextBytes {
		file.Refusal = "too-large"
		file.Message = fmt.Sprintf("%s is %.1f MB; files over %d MB are not shown here. Open it in your editor.", name, float64(info.Size())/(1<<20), maxTextBytes>>20)
		return json.Marshal(file)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("engine: could not read %s", name)
	}
	if len(data) > maxTextBytes {
		file.Size, file.Refusal = int64(len(data)), "too-large"
		file.Message = fmt.Sprintf("%s grew past %d MB; open it in your editor.", name, maxTextBytes>>20)
		return json.Marshal(file)
	}
	if looksBinary(data) {
		file.Refusal = "binary"
		file.Message = name + " is not text; open it in your editor."
		return json.Marshal(file)
	}
	file.Text, file.Lines = string(data), countLines(data)
	file.Hash = shortHash(data)
	return json.Marshal(file)
}

// ── matching names ──────────────────────────────────────────────────────────

func (s *server) findFiles(call Frame) (json.RawMessage, error) {
	args, err := arg[FindFilesArgs](call)
	if err != nil {
		return nil, err
	}
	root, err := s.workspaceRoot()
	if err != nil {
		return nil, err
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultFindLimit
	}
	if limit > maxFindLimit {
		limit = maxFindLimit
	}
	query := strings.ToLower(strings.TrimSpace(args.Query))
	out := FoundFiles{Files: []FoundFile{}}
	if query == "" {
		return json.Marshal(out)
	}
	type scored struct {
		path  string
		score int
	}
	var hits []scored
	for _, path := range workspaceFiles(root) {
		if score, ok := matchScore(query, path); ok {
			hits = append(hits, scored{path, score})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].path < hits[j].path
	})
	for _, hit := range hits {
		// ls-files lists paths deleted from the tree; a row must be a real file.
		if info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(hit.path))); err != nil || info.IsDir() {
			continue
		}
		if len(out.Files) == limit {
			out.Truncated = true
			break
		}
		name, dir := splitPath(hit.path)
		out.Files = append(out.Files, FoundFile{Path: hit.path, Name: name, Dir: dir})
	}
	return json.Marshal(out)
}

// workspaceFiles is every file a person would expect to find: git's tracked and
// untracked-but-not-ignored files, or a bounded walk outside git.
func workspaceFiles(root string) []string {
	if out, err := runGit(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard"); err == nil {
		files := []string{}
		for _, p := range strings.Split(string(out), "\x00") {
			if p != "" {
				files = append(files, p)
			}
		}
		return files
	}
	files := []string{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "target") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			if rel, e := filepath.Rel(root, path); e == nil {
				files = append(files, filepath.ToSlash(rel))
			}
		}
		if len(files) >= maxFindCandidate {
			return filepath.SkipAll
		}
		return nil
	})
	return files
}

// matchScore ranks a path for a lowercase query: the query must appear as a
// subsequence of the path; a whole-name or name-prefix or name-substring hit
// beats a scattered one, and shorter paths beat longer.
func matchScore(query, path string) (int, bool) {
	lower := strings.ToLower(path)
	name := lower[strings.LastIndex(lower, "/")+1:]
	score := 0
	switch {
	case name == query:
		score = 1000
	case strings.HasPrefix(name, query):
		score = 800
	case strings.Contains(name, query):
		score = 600
	case strings.Contains(lower, query):
		score = 400
	default:
		at := 0
		for _, r := range query {
			i := strings.IndexRune(lower[at:], r)
			if i < 0 {
				return 0, false
			}
			at += i + utf8.RuneLen(r)
		}
		score = 100
	}
	return score - len(path)/4, true
}

// ── what changed ────────────────────────────────────────────────────────────

func runGit(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-c", "core.quotepath=off"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// gitBase is what the diff is measured against: HEAD, or the empty tree on an
// unborn branch.
func gitBase(root string) (ref, short string, err error) {
	if _, err = runGit(root, "rev-parse", "--is-inside-work-tree"); err != nil {
		return "", "", err
	}
	if out, e := runGit(root, "rev-parse", "--verify", "--short", "HEAD"); e == nil {
		return "HEAD", strings.TrimSpace(string(out)), nil
	}
	out, err := runGit(root, "hash-object", "-t", "tree", "/dev/null")
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(string(out)), "", nil
}

// gitTop converts git's top-level-relative path to workspace-relative, false
// when it lies outside the workspace.
func gitRel(root, top, path string) (string, bool) {
	abs := filepath.Join(top, filepath.FromSlash(path))
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func (s *server) diffChanges(call Frame) (json.RawMessage, error) {
	args, err := arg[DiffChangesArgs](call)
	if err != nil {
		return nil, err
	}
	root, err := s.workspaceRoot()
	if err != nil {
		return nil, err
	}
	out := ChangedFiles{Files: []ChangedFile{}}
	base, short, err := gitBase(root)
	if err != nil {
		return json.Marshal(out) // not a work tree: Git=false, no files
	}
	out.Git, out.Base = true, short
	if branch, e := runGit(root, "rev-parse", "--abbrev-ref", "HEAD"); e == nil && short != "" {
		out.Branch = strings.TrimSpace(string(branch))
	}
	topOut, err := runGit(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	top, _ := filepath.EvalSymlinks(strings.TrimSpace(string(topOut)))

	want := map[string]bool{}
	for _, p := range args.Paths {
		if _, rel, e := confine(root, p); e == nil {
			want[rel] = true
		}
	}
	rows := map[string]*ChangedFile{}
	var order []string
	add := func(rel, status string) {
		if _, seen := rows[rel]; seen || (len(want) > 0 && !want[rel]) {
			return
		}
		name, dir := splitPath(rel)
		rows[rel] = &ChangedFile{Path: rel, Name: name, Dir: dir, Status: status}
		order = append(order, rel)
	}
	status, err := runGit(root, "status", "--porcelain=v1", "-z", "-uall", "--no-renames", "--", ".")
	if err != nil {
		return nil, err
	}
	for _, entry := range strings.Split(string(status), "\x00") {
		if len(entry) < 4 {
			continue
		}
		xy, path := entry[:2], entry[3:]
		rel, ok := gitRel(root, top, path)
		if !ok {
			continue
		}
		switch {
		case xy == "??":
			add(rel, "untracked")
		case strings.Contains(xy, "D"):
			add(rel, "deleted")
		case strings.Contains(xy, "A"):
			add(rel, "added")
		default:
			add(rel, "modified")
		}
	}
	numstat, err := runGit(root, "diff", base, "--numstat", "-z", "--no-renames", "--no-ext-diff", "--", ".")
	if err != nil {
		return nil, err
	}
	for _, entry := range strings.Split(string(numstat), "\x00") {
		cols := strings.SplitN(entry, "\t", 3)
		if len(cols) != 3 {
			continue
		}
		rel, ok := gitRel(root, top, cols[2])
		row := rows[rel]
		if !ok || row == nil {
			continue
		}
		if cols[0] == "-" {
			row.Binary = true
			continue
		}
		row.Added, _ = strconv.Atoi(cols[0])
		row.Deleted, _ = strconv.Atoi(cols[1])
	}
	sort.Strings(order)
	for _, rel := range order {
		if len(out.Files) == maxChangedFiles {
			out.Truncated = true
			break
		}
		row := rows[rel]
		if row.Status == "untracked" {
			row.Added, row.Binary = untrackedCount(filepath.Join(root, filepath.FromSlash(rel)))
		}
		out.Added += row.Added
		out.Deleted += row.Deleted
		out.Files = append(out.Files, *row)
	}
	return json.Marshal(out)
}

// untrackedCount reads a new file's added-line count; binary or oversize files
// count as binary with no lines.
func untrackedCount(path string) (lines int, binary bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0, false
	}
	if info.Size() > maxTextBytes {
		return 0, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	if looksBinary(data) {
		return 0, true
	}
	return countLines(data), false
}

// ── one file's diff ─────────────────────────────────────────────────────────

func (s *server) diffFile(call Frame) (json.RawMessage, error) {
	args, err := arg[DiffFileArgs](call)
	if err != nil {
		return nil, err
	}
	root, err := s.workspaceRoot()
	if err != nil {
		return nil, err
	}
	abs, rel, err := confine(root, args.Path)
	if err != nil {
		return nil, err
	}
	name, dir := splitPath(rel)
	diff := FileDiff{Path: rel, Name: name, Dir: dir, Abs: abs, Hunks: []DiffHunk{}, Status: "clean"}
	info, statErr := os.Lstat(abs)
	exists := statErr == nil
	if exists && info.IsDir() {
		return nil, fmt.Errorf("engine: %s is not a file", args.Path)
	}
	if exists && info.Mode().IsRegular() && info.Size() <= maxTextBytes {
		if data, e := os.ReadFile(abs); e == nil && !looksBinary(data) {
			diff.Lines = countLines(data)
		}
	}
	base, _, err := gitBase(root)
	if err != nil {
		return json.Marshal(diff)
	}
	diff.Git = true
	state, err := runGit(root, "status", "--porcelain=v1", "-z", "--no-renames", "--", rel)
	if err != nil {
		return nil, err
	}
	xy := ""
	if len(state) >= 2 {
		xy = string(state[:2])
	}
	switch {
	case xy == "":
		return json.Marshal(diff)
	case xy == "??":
		return json.Marshal(untrackedDiff(diff, abs, info))
	case strings.Contains(xy, "D"):
		diff.Status = "deleted"
	case strings.Contains(xy, "A"):
		diff.Status = "added"
	default:
		diff.Status = "modified"
	}
	context := args.Context
	if context <= 0 || context > 50 {
		context = 3
	}
	raw, err := runGit(root, "diff", base, "--no-color", "--no-ext-diff", "--no-renames", "-U"+strconv.Itoa(context), "--", rel)
	if err != nil {
		return nil, err
	}
	parseUnifiedDiff(&diff, string(raw))
	return json.Marshal(diff)
}

func untrackedDiff(diff FileDiff, abs string, info os.FileInfo) FileDiff {
	diff.Status = "untracked"
	if info == nil || !info.Mode().IsRegular() {
		return diff
	}
	if info.Size() > maxTextBytes {
		diff.Binary = true
		return diff
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return diff
	}
	if looksBinary(data) {
		diff.Binary = true
		return diff
	}
	text := strings.TrimSuffix(string(data), "\n")
	if len(data) == 0 {
		return diff
	}
	hunk := DiffHunk{NewStart: 1, NewLines: countLines(data)}
	hunk.Header = fmt.Sprintf("@@ -0,0 +1,%d @@", hunk.NewLines)
	for i, line := range strings.Split(text, "\n") {
		if i >= maxDiffLines {
			diff.Truncated = true
			break
		}
		hunk.Lines = append(hunk.Lines, DiffLine{Kind: "add", New: i + 1, Text: line})
	}
	diff.Added = hunk.NewLines
	diff.Hunks = []DiffHunk{hunk}
	return diff
}

// parseUnifiedDiff fills hunks and counts from `git diff` output.
func parseUnifiedDiff(diff *FileDiff, raw string) {
	var cur *DiffHunk
	oldN, newN, total := 0, 0, 0
	flush := func() {
		if cur != nil {
			diff.Hunks = append(diff.Hunks, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch") {
			diff.Binary = true
			continue
		}
		if strings.HasPrefix(line, "@@ ") {
			flush()
			h, ok := parseHunkHeader(line)
			if !ok {
				continue
			}
			cur, oldN, newN = &h, h.OldStart, h.NewStart
			continue
		}
		if cur == nil {
			continue
		}
		if total >= maxDiffLines {
			diff.Truncated = true
			break
		}
		switch {
		case strings.HasPrefix(line, "+"):
			cur.Lines = append(cur.Lines, DiffLine{Kind: "add", New: newN, Text: line[1:]})
			newN++
			diff.Added++
		case strings.HasPrefix(line, "-"):
			cur.Lines = append(cur.Lines, DiffLine{Kind: "del", Old: oldN, Text: line[1:]})
			oldN++
			diff.Deleted++
		case strings.HasPrefix(line, " "):
			cur.Lines = append(cur.Lines, DiffLine{Kind: "context", Old: oldN, New: newN, Text: line[1:]})
			oldN++
			newN++
		default:
			continue // "\ No newline at end of file" and the trailing blank
		}
		total++
	}
	flush()
}

// parseHunkHeader reads "@@ -a,b +c,d @@ section".
func parseHunkHeader(line string) (DiffHunk, bool) {
	rest := strings.TrimPrefix(line, "@@ ")
	end := strings.Index(rest, " @@")
	if end < 0 {
		return DiffHunk{}, false
	}
	ranges, section := rest[:end], strings.TrimSpace(rest[end+3:])
	parts := strings.Fields(ranges)
	if len(parts) != 2 || parts[0][0] != '-' || parts[1][0] != '+' {
		return DiffHunk{}, false
	}
	h := DiffHunk{Header: line, Section: section, Lines: []DiffLine{}}
	var ok1, ok2 bool
	h.OldStart, h.OldLines, ok1 = parseRange(parts[0][1:])
	h.NewStart, h.NewLines, ok2 = parseRange(parts[1][1:])
	return h, ok1 && ok2
}

func parseRange(text string) (start, count int, ok bool) {
	a, b, found := strings.Cut(text, ",")
	start, err := strconv.Atoi(a)
	if err != nil {
		return 0, 0, false
	}
	count = 1
	if found {
		if count, err = strconv.Atoi(b); err != nil {
			return 0, 0, false
		}
	}
	return start, count, true
}

func shortHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
