// Package reporead is the factory's read-only, bounded look into its own
// checkout of a watched repository: a folder's children, the lines matching a
// pattern, and a range of one file's lines, for the desk's Ask loop.
//
// EVERYTHING IS READ FROM GIT'S OBJECT STORE AT ONE COMMIT (ls-tree and
// cat-file), never from the working tree, so uncommitted files are invisible
// and a symbolic link is a recorded target, never followed. No command it runs
// writes: there is no fetch, no checkout, no index refresh (optional locks
// are off) and no replace object is honoured. The deny list ([DenyList])
// hides credential-looking paths from every answer, and every answer is
// bounded in entries, hits, lines, bytes and time.
package reporead

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// DenyList is every path a read never shows, one glob per word. A word with
// no slash is matched against each segment of a path (so `.env*` hides
// `config/.env.local`, and a folder that matches hides all it holds); a word
// with slashes is matched against consecutive segments (`.docker/config.json`).
// Matching ignores case. Denied paths are absent from tree and grep, and a
// read of one is refused with the word "denied".
const DenyList = ".env* *.pem *.key id_* .git/ .npmrc .netrc .pypirc credentials* *secret* *.p12 *.pfx " +
	".aws/ .ssh/ .docker/config.json kubeconfig"

// The bounds of one read.
const (
	MaxEntries  = 500      // children one tree answers
	MaxHits     = 50       // lines one grep answers
	MaxLines    = 400      // lines one read answers
	MaxBytes    = 32 << 10 // bytes of text one read answers
	MaxPattern  = 256      // bytes of a grep pattern
	MaxPath     = 1024     // bytes of a path
	MaxHitText  = 240      // bytes of one hit's line
	sniffBytes  = 8 << 10  // the head of a file searched for a NUL
	grepFileMax = 1 << 20  // a larger file is not searched
	grepBudget  = 32 << 20 // bytes one grep searches in all
	grepTime    = 5 * time.Second
	opTime      = 10 * time.Second
)

// The ops.
const (
	OpTree = "tree"
	OpGrep = "grep"
	OpRead = "read"
)

var (
	// ErrNotCloned: there is no git checkout at the folder, or it has no
	// default branch to read.
	ErrNotCloned = errors.New("not_cloned")
	// ErrDenied: the path is on [DenyList].
	ErrDenied = errors.New("denied")
	// ErrBinary: the file has a NUL in its first 8 KiB.
	ErrBinary = errors.New("binary")
)

// NotFound is a path or a commit the checkout does not have.
type NotFound string

func (e NotFound) Error() string { return string(e) }

// BadInput is a request the bounds refuse before git is asked.
type BadInput string

func (e BadInput) Error() string { return string(e) }

// Request is one look. Head is a job's 40-hex commit; empty reads the
// default branch. To 0 is "as far as the bounds allow".
type Request struct {
	Op      string
	Path    string
	Pattern string
	From    int
	To      int
	Head    string
}

var denyRules = func() [][]string {
	var rules [][]string
	for _, word := range strings.Fields(DenyList) {
		rules = append(rules, strings.Split(strings.Trim(word, "/"), "/"))
	}
	return rules
}()

// Denied says whether a clean, slash-separated path is on [DenyList].
func Denied(p string) bool {
	segs := strings.Split(strings.ToLower(p), "/")
	for _, rule := range denyRules {
		for i := 0; i+len(rule) <= len(segs); i++ {
			if matchRun(rule, segs[i:i+len(rule)]) {
				return true
			}
		}
	}
	return false
}

func matchRun(rule, segs []string) bool {
	for j, glob := range rule {
		if ok, _ := path.Match(glob, segs[j]); !ok {
			return false
		}
	}
	return true
}

var head40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Read answers one look at the checkout in dir. The answer's Factory, Repo
// and Job are the caller's to fill.
func Read(ctx context.Context, dir string, req Request) (api.RepoRead, error) {
	out := api.RepoRead{Op: req.Op, Untrusted: true, At: "main"}
	p, err := cleanPath(req.Path)
	if err != nil {
		return out, err
	}
	out.Path = p
	if p != "" && Denied(p) {
		return out, ErrDenied
	}
	var re *regexp.Regexp
	switch req.Op {
	case OpTree:
	case OpGrep:
		if re, err = pattern(req.Pattern); err != nil {
			return out, err
		}
		out.Pattern = req.Pattern
	case OpRead:
		if p == "" {
			return out, BadInput("read needs a file's path")
		}
		if req.From < 0 || req.To < 0 || (req.To > 0 && req.To < max(req.From, 1)) {
			return out, BadInput("from and to must be line numbers, from no later than to")
		}
	default:
		return out, BadInput("op must be tree, grep or read")
	}
	if req.Head != "" && !head40.MatchString(req.Head) {
		return out, BadInput("a job's head must be a 40-character commit")
	}
	ctx, cancel := context.WithTimeout(ctx, opTime)
	defer cancel()
	if out.Ref, out.Commit, err = commitOf(ctx, dir, req.Head); err != nil {
		return out, err
	}
	if req.Head != "" {
		out.At = "job"
	}
	switch req.Op {
	case OpTree:
		err = tree(ctx, dir, &out)
	case OpGrep:
		err = grep(ctx, dir, re, &out)
	default:
		err = readFile(ctx, dir, req, &out)
	}
	return out, err
}

// cleanPath is p relative to the repository's root, slash-separated, with
// "." and empty segments dropped. An absolute path, a "..", a backslash or a
// control character is refused, never resolved.
func cleanPath(p string) (string, error) {
	if len(p) > MaxPath || strings.HasPrefix(p, "/") || strings.ContainsRune(p, '\\') || !utf8.ValidString(p) {
		return "", BadInput("path must be relative to the repository, with forward slashes")
	}
	var segs []string
	for _, s := range strings.Split(p, "/") {
		switch {
		case s == "" || s == ".":
		case s == "..":
			return "", BadInput(`path may not contain ".."`)
		case strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0:
			return "", BadInput("path may not contain control characters")
		default:
			segs = append(segs, s)
		}
	}
	return strings.Join(segs, "/"), nil
}

func pattern(p string) (*regexp.Regexp, error) {
	if p == "" || len(p) > MaxPattern {
		return nil, BadInput(fmt.Sprintf("pattern must be 1 to %d bytes", MaxPattern))
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, BadInput("pattern is not an RE2 regular expression")
	}
	return re, nil
}

// commitOf is the commit a look reads: head when the checkout has it, else
// the default branch's tip. A folder that is not itself the top of a git
// checkout is not cloned, so a plain folder inside another repository never
// reads that repository.
func commitOf(ctx context.Context, dir, head string) (ref, commit string, err error) {
	if strings.TrimSpace(dir) == "" {
		return "", "", ErrNotCloned
	}
	top, err := gitOut(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || !sameDir(top, dir) {
		return "", "", ErrNotCloned
	}
	if head != "" {
		if _, err := gitOut(ctx, dir, "cat-file", "-e", head+"^{commit}"); err != nil {
			return "", "", NotFound("the job's head commit is not in this checkout")
		}
		return "", head, nil
	}
	if ref = factory.DefaultBranch(dir); ref == "" {
		return "", "", ErrNotCloned
	}
	commit, err = gitOut(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil || !head40.MatchString(commit) {
		return "", "", ErrNotCloned
	}
	return ref, commit, nil
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && filepath.Clean(ra) == filepath.Clean(rb)
}

// entry is one ls-tree -l record.
type entry struct {
	mode, kind, object, path string
	size                     int64
}

func (e entry) regular() bool { return e.kind == "blob" && (e.mode == "100644" || e.mode == "100755") }

func (e entry) wireType() string {
	switch e.mode {
	case "040000":
		return "dir"
	case "120000":
		return "link"
	case "160000":
		return "submodule"
	}
	return "file"
}

// lsTree is the records of `git ls-tree -z -l commit [-- p]`, each path
// from the repository's root.
func lsTree(ctx context.Context, dir string, args ...string) ([]entry, error) {
	raw, err := gitOut(ctx, dir, append([]string{"ls-tree", "-z", "-l"}, args...)...)
	if err != nil {
		return nil, err
	}
	var out []entry
	for _, rec := range strings.Split(raw, "\x00") {
		meta, name, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 4 {
			continue
		}
		size, _ := strconv.ParseInt(f[3], 10, 64)
		out = append(out, entry{mode: f[0], kind: f[1], object: f[2], size: size, path: name})
	}
	return out, nil
}

func tree(ctx context.Context, dir string, out *api.RepoRead) error {
	args := []string{out.Commit}
	if out.Path != "" {
		args = append(args, "--", out.Path+"/")
	}
	entries, err := lsTree(ctx, dir, args...)
	if err != nil {
		return err
	}
	if len(entries) == 0 && out.Path != "" {
		return NotFound("no such folder at this commit")
	}
	out.Entries = []api.RepoEntry{}
	for _, e := range entries {
		if Denied(e.path) {
			continue
		}
		if len(out.Entries) == MaxEntries {
			out.Truncated = true
			break
		}
		row := api.RepoEntry{Path: e.path, Type: e.wireType()}
		if row.Type == "file" {
			row.Size = e.size
		}
		out.Entries = append(out.Entries, row)
	}
	return nil
}

func readFile(ctx context.Context, dir string, req Request, out *api.RepoRead) error {
	entries, err := lsTree(ctx, dir, out.Commit, "--", out.Path)
	if err != nil {
		return err
	}
	var file *entry
	for i := range entries {
		if entries[i].path == out.Path {
			file = &entries[i]
		}
	}
	switch {
	case file == nil:
		return NotFound("no such file at this commit")
	case !file.regular():
		return BadInput("not a file: " + file.wireType())
	}
	out.Size = file.size
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := git(ctx, dir, "cat-file", "blob", file.object)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	r := bufio.NewReaderSize(pipe, 64<<10)
	if head, _ := r.Peek(sniffBytes); bytes.IndexByte(head, 0) >= 0 {
		return ErrBinary
	}
	from := max(req.From, 1)
	to := from + MaxLines - 1
	if req.To > 0 && req.To < to {
		to = req.To
	}
	text, last, eof, cut, err := lines(r, from, to, MaxBytes)
	if err != nil {
		return err
	}
	if last < from && !(last == 0 && from == 1) {
		return BadInput(fmt.Sprintf("from is past the file's last line (%d)", last))
	}
	out.From, out.To, out.EOF, out.Text = from, last, eof, string(text)
	out.Truncated = cut || (!eof && last == to && (req.To == 0 || req.To > to))
	return nil
}

// lines is lines from..to of r, at most max bytes of them, whole lines only
// unless the first line alone is longer. last is the last line answered (or
// the file's last line when it ended first); eof says nothing follows last;
// cut says the byte bound stopped it.
func lines(r *bufio.Reader, from, to, max int) (text []byte, last int, eof, cut bool, err error) {
	for n := 1; ; n++ {
		keep := 0
		if n >= from {
			keep = max - len(text) + 1
		}
		kept, size, err := line(r, keep)
		if size == 0 {
			return text, n - 1, true, false, nil
		}
		if n >= from {
			if len(text)+size > max {
				if n == from {
					return cutUTF8(kept, max), n, false, true, nil
				}
				return text, n - 1, false, true, nil
			}
			text = append(text, kept...)
		}
		switch {
		case err == io.EOF:
			return text, n, true, false, nil
		case err != nil:
			return nil, 0, false, false, err
		case n == to:
			_, perr := r.Peek(1)
			return text, n, perr == io.EOF, false, nil
		}
	}
}

// line reads through the next newline, keeping at most keep bytes; size is
// the whole line's length.
func line(r *bufio.Reader, keep int) (kept []byte, size int, err error) {
	for {
		chunk, err := r.ReadSlice('\n')
		size += len(chunk)
		if room := keep - len(kept); room > 0 {
			kept = append(kept, chunk[:min(room, len(chunk))]...)
		}
		if err != bufio.ErrBufferFull {
			return kept, size, err
		}
	}
}

func cutUTF8(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	b = b[:n]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return b
}

// grep searches every regular, allowed file under the path at the commit,
// through one `git cat-file --batch`, in the RE2 engine, so a pattern's
// cost is linear in what it reads. It stops at the 51st hit, the byte budget
// or the time bound, and says so with Truncated.
func grep(ctx context.Context, dir string, re *regexp.Regexp, out *api.RepoRead) error {
	args := []string{"-r", out.Commit}
	if out.Path != "" {
		args = append(args, "--", out.Path)
	}
	entries, err := lsTree(ctx, dir, args...)
	if err != nil {
		return err
	}
	var files []entry
	budget := int64(grepBudget)
	for _, e := range entries {
		if !e.regular() || Denied(e.path) || e.size > grepFileMax {
			continue
		}
		if budget -= e.size; budget < 0 {
			out.Truncated = true
			break
		}
		files = append(files, e)
	}
	ctx, cancel := context.WithTimeout(ctx, grepTime)
	defer cancel()
	cmd := git(ctx, dir, "cat-file", "--batch")
	var ids bytes.Buffer
	for _, f := range files {
		ids.WriteString(f.object + "\n")
	}
	cmd.Stdin = &ids
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	r := bufio.NewReaderSize(pipe, 64<<10)
	out.Hits = []api.RepoHit{}
	for _, f := range files {
		body, err := batchObject(r)
		if err != nil {
			// The time bound ended the search: what was found stands.
			out.Truncated = true
			return nil
		}
		if bytes.IndexByte(body[:min(len(body), sniffBytes)], 0) >= 0 {
			continue
		}
		if !hits(re, f.path, body, out) {
			return nil
		}
	}
	return nil
}

// batchObject reads one `<object> <type> <size>\n<body>\n` record.
func batchObject(r *bufio.Reader) ([]byte, error) {
	header, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	f := strings.Fields(header)
	if len(f) != 3 {
		return nil, errors.New("unexpected cat-file record")
	}
	size, err := strconv.Atoi(f[2])
	if err != nil || size > grepFileMax {
		return nil, errors.New("unexpected cat-file size")
	}
	body := make([]byte, size+1)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body[:size], nil
}

// hits adds body's matching lines; false once a hit past MaxHits was found.
func hits(re *regexp.Regexp, name string, body []byte, out *api.RepoRead) bool {
	for n := 1; len(body) > 0; n++ {
		text, rest, _ := bytes.Cut(body, []byte{'\n'})
		body = rest
		if !re.Match(text) {
			continue
		}
		if len(out.Hits) == MaxHits {
			out.Truncated = true
			return false
		}
		out.Hits = append(out.Hits, api.RepoHit{Path: name, Line: n, Text: string(cutUTF8(bytes.TrimRight(text, "\r"), MaxHitText))})
	}
	return true
}

// git is one read-only git command in dir. The caller's GIT_* environment is
// dropped so dir is the repository read; optional locks are off so not even
// an index refresh is written; replace objects and pathspec magic are off;
// fsmonitor never runs; and git never prompts.
func git(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.fsmonitor=false",
		"--no-replace-objects", "--literal-pathspecs"}, args...)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	return cmd
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	raw, err := git(ctx, dir, args...).Output()
	return strings.TrimRight(string(raw), "\n"), err
}
