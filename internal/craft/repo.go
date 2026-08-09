package craft

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The craft repository is an ordinary git repository, driven through the git
// CLI. There is no library here on purpose: git is already a dependency of the
// machine aforge works on, the resident already shells it, and a workflow's
// history has to be readable by the user with the tools they already have —
// `git log workflows/presentation.yaml` in the craft directory is part of the
// interface, not an implementation detail.

const (
	// DefaultHistory is how many versions a history read returns when the
	// caller does not say. A workflow's recent past is what explains its
	// present; its distant past is archaeology and has to be asked for.
	DefaultHistory = 20
	// ScriptMode is how a verifier or skill is written when the caller does
	// not name a mode. Everything in those two directories exists to be
	// executed, so the executable bit is the default rather than the exception.
	ScriptMode = os.FileMode(0o755)
	// MinReasonWords is the shortest message that can count as a reason. A
	// revert with a one-word message ("revert") records that something was
	// undone and loses why, which is the only part a later reader needs.
	MinReasonWords = 4
	// commitAuthor is the identity every craft commit carries. It is set local
	// to the repository so a version the resident writes never depends on, or
	// borrows from, whatever git identity the host happens to have configured.
	commitAuthorName  = "aforge resident"
	commitAuthorEmail = "resident@aforge.local"
)

// gitLock serializes every mutation across the process. The git index is a
// single file with no concurrency story, and the resident is one process with
// many arms — a distiller saving a revision while a forge writes a verifier is
// ordinary here, so the lock is package-level rather than per-Repo: two Repo
// values may name the same directory.
var gitLock sync.Mutex

// Repo is the craft repository at one directory.
//
// It is never copied — Open hands back a pointer and every caller keeps it —
// which is what lets the catalogue cache below live on it.
type Repo struct {
	dir string

	// The catalogue, and the state of the directory it was read from. List
	// forks git once per workflow and the Self pane calls it on every poll;
	// see listed.
	listing sync.Mutex
	listed  []Summary
	listErr error
	stamp   string

	forks atomic.Int64
}

// Version is one commit in a workflow's history.
type Version struct {
	Commit  string
	When    time.Time
	Subject string
	Body    string
}

// Summary is a workflow as the catalogue sees it: enough to choose by, without
// parsing the shape.
type Summary struct {
	Name        string
	Description string
	Commit      string
	When        time.Time
}

// Open prepares the craft repository at dir, creating and initializing it when
// it is not there yet. It is idempotent: opening an existing repository adds
// nothing and commits nothing.
func Open(dir string) (*Repo, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("craft: open needs a directory")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("craft: resolve %s: %w", dir, err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("craft: git is not on PATH — the craft repository is a real git repository")
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, fmt.Errorf("craft: create %s: %w", absolute, err)
	}
	repo := &Repo{dir: absolute}

	gitLock.Lock()
	defer gitLock.Unlock()

	if _, err := os.Stat(filepath.Join(absolute, ".git")); err != nil {
		if _, err := repo.git("init", "-q"); err != nil {
			return nil, err
		}
		// The default branch name varies by git version and host config, and a
		// repository the resident owns should not.
		if _, err := repo.git("symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
			return nil, err
		}
	}
	for key, value := range map[string]string{
		"user.name":      commitAuthorName,
		"user.email":     commitAuthorEmail,
		"commit.gpgsign": "false",
	} {
		if _, err := repo.git("config", key, value); err != nil {
			return nil, err
		}
	}
	var paths []string
	for _, name := range []string{WorkflowDir, VerifierDir, SkillDir, ExemplarDir} {
		if err := os.MkdirAll(filepath.Join(absolute, name), 0o755); err != nil {
			return nil, fmt.Errorf("craft: create %s: %w", name, err)
		}
		// Git tracks files, not directories, so the layout only survives a
		// clone if each directory carries one.
		keep := filepath.Join(name, ".gitkeep")
		if _, err := os.Stat(filepath.Join(absolute, keep)); err != nil {
			if err := os.WriteFile(filepath.Join(absolute, keep), nil, 0o644); err != nil {
				return nil, fmt.Errorf("craft: create %s: %w", keep, err)
			}
		}
		paths = append(paths, keep)
	}
	dirty, err := repo.pending(paths)
	if err != nil {
		return nil, err
	}
	if dirty {
		if _, err := repo.commit("craft: the repository the resident learns into", paths); err != nil {
			return nil, err
		}
	}
	return repo, nil
}

// Dir is where the repository lives, for the surfaces that tell the user where
// their know-how is kept.
func (r *Repo) Dir() string { return r.dir }

// Save writes one version of a workflow. It refuses an invalid file before it
// touches the working tree — a craft repository that holds a workflow which
// cannot run is worse than one that is missing it — and it clamps before it
// marshals, so the file on disk states the numbers the run will actually obey.
// The commit touches only this workflow's path: a version is one workflow's
// change and nothing else's.
func (r *Repo) Save(w *Workflow, message string) (string, error) {
	if w == nil {
		return "", errors.New("craft save: nothing to save")
	}
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("craft save %s: empty message — a version has to say what changed and on what evidence", w.Name)
	}
	if problems := w.Validate(); len(problems) > 0 {
		return "", fmt.Errorf("craft save %s: refusing an invalid workflow: %w", w.Name, errors.Join(problems...))
	}
	w.Clamp()
	data, err := w.Marshal()
	if err != nil {
		return "", err
	}
	path, err := workflowPath(w.Name)
	if err != nil {
		return "", err
	}

	gitLock.Lock()
	defer gitLock.Unlock()
	// The catalogue is about to be a version out of date. The file's own stamp
	// would say so too, but not on a filesystem that rounds modification times
	// to the second, and this is the case where that matters: the distiller
	// saves while the Self pane is watching.
	defer r.forget()

	if err := os.WriteFile(filepath.Join(r.dir, path), data, 0o644); err != nil {
		return "", fmt.Errorf("craft save %s: %w", w.Name, err)
	}
	return r.commitIfChanged(subjected(w.Name, message), path)
}

// Load reads a workflow at the working tree's version and stamps the version
// it came from, so anything measured about the run can be attributed to a
// version rather than to a name.
//
// The working tree is not the same thing as HEAD, and the difference is
// load-bearing: a run re-reads its workflow by name@commit on every advance and
// refuses to continue when that reference has moved. Stamping HEAD's hash onto
// edited bytes would make an uncommitted edit invisible to that guard — the
// same run, silently finishing on a file nobody committed. A dirty path
// therefore carries its own content into the version, so editing under a live
// run moves the reference and the guard fires; and a workflow that was never
// committed has no version at all, which is a refusal rather than a bare name
// with the guard switched off.
func (r *Repo) Load(name string) (*Workflow, error) {
	path, err := workflowPath(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(r.dir, path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, r.missing(name)
		}
		return nil, fmt.Errorf("craft load %s: %w", name, err)
	}
	w, err := r.settle(name, data)
	if err != nil {
		return nil, err
	}
	commit, err := r.git("log", "-1", "--format=%H", "--", path)
	if err != nil {
		return nil, err
	}
	w.Commit = strings.TrimSpace(commit)
	if w.Commit == "" {
		return nil, fmt.Errorf("craft load %s: this workflow has never been committed — save it before running it, so the run can name the version it ran", name)
	}
	dirty, err := r.pending([]string{path})
	if err != nil {
		return nil, err
	}
	if dirty {
		w.Commit = dirtyVersion(w.Commit, data)
	}
	return w, nil
}

// dirtyVersion names an uncommitted edit as its own version: the commit it
// departs from, and enough of the content's hash to tell two edits apart. It
// reads as what it is everywhere a version is shown, and it changes the moment
// the file does.
func dirtyVersion(commit string, data []byte) string {
	sum := sha256.Sum256(data)
	return commit + "+dirty-" + hex.EncodeToString(sum[:])[:8]
}

// LoadAt reads a workflow as of one commit. This is how a survival record is
// re-read: the stats key on name and commit, and the file that earned them may
// be several revisions behind the working tree.
func (r *Repo) LoadAt(name, commit string) (*Workflow, error) {
	path, err := workflowPath(name)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(commit) == "" {
		return nil, fmt.Errorf("craft load %s: no commit given — use Load for the current version", name)
	}
	data, err := r.gitBytes("show", strings.TrimSpace(commit)+":"+path)
	if err != nil {
		return nil, fmt.Errorf("craft load %s at %s: no such version of this workflow", name, short(commit))
	}
	w, err := r.settle(name, data)
	if err != nil {
		return nil, err
	}
	resolved, err := r.git("rev-parse", strings.TrimSpace(commit)+"^{commit}")
	if err != nil {
		return nil, err
	}
	w.Commit = strings.TrimSpace(resolved)
	return w, nil
}

// History is a workflow's versions, newest first.
func (r *Repo) History(name string, limit int) ([]Version, error) {
	path, err := workflowPath(name)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = DefaultHistory
	}
	// Unit and record separators rather than newlines: a commit body carries
	// the evidence, and evidence has newlines in it.
	out, err := r.git("log", fmt.Sprintf("-%d", limit), "--format=%H%x1f%aI%x1f%s%x1f%b%x1e", "--", path)
	if err != nil {
		return nil, err
	}
	var versions []Version
	for _, record := range strings.Split(out, "\x1e") {
		record = strings.TrimLeft(record, "\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		fields := strings.Split(record, "\x1f")
		if len(fields) < 4 {
			continue
		}
		when, _ := time.Parse(time.RFC3339, strings.TrimSpace(fields[1]))
		versions = append(versions, Version{
			Commit:  strings.TrimSpace(fields[0]),
			When:    when,
			Subject: strings.TrimSpace(fields[2]),
			Body:    strings.TrimSpace(fields[3]),
		})
	}
	if len(versions) == 0 {
		if _, err := os.Stat(filepath.Join(r.dir, path)); err != nil {
			return nil, r.missing(name)
		}
	}
	return versions, nil
}

// Revert restores an older version as a NEW commit. History is never rewritten
// here: the version that failed is part of what the workflow knows about
// itself, and a repository that can erase its own mistakes cannot be measured.
// The message has to carry a reason — a revert whose commit says only "revert"
// throws away the one fact a later reader needs.
func (r *Repo) Revert(name, toCommit, message string) (string, error) {
	path, err := workflowPath(name)
	if err != nil {
		return "", err
	}
	if len(strings.Fields(message)) < MinReasonWords {
		return "", fmt.Errorf("craft revert %s: the message has to say why this version is going back — what the newer one did wrong", name)
	}
	old, err := r.LoadAt(name, toCommit)
	if err != nil {
		return "", err
	}
	data, err := old.Marshal()
	if err != nil {
		return "", err
	}

	gitLock.Lock()
	defer gitLock.Unlock()
	defer r.forget()

	if err := os.WriteFile(filepath.Join(r.dir, path), data, 0o644); err != nil {
		return "", fmt.Errorf("craft revert %s: %w", name, err)
	}
	subject, body, _ := strings.Cut(message, "\n")
	// The commit a revert restores is evidence, so it goes in the body whether
	// or not the caller thought to name it.
	body = strings.TrimSpace(strings.TrimSpace(body) + "\n\nrestores " + short(old.Commit))
	return r.commitIfChanged(subjected(name, strings.TrimSpace(subject)+"\n\n"+body), path)
}

// List is the catalogue. A file that will not parse is skipped and reported
// rather than fatal: one corrupt workflow must not hide the rest, and the
// listing is the surface the resident chooses from. The returned error names
// what was skipped — the summaries are complete either way.
//
// The answer is remembered against the state of the workflows directory,
// because this is not an occasional call: each summary costs a `git log -1`,
// which is a process fork of about six milliseconds, and the Self pane asks for
// the whole catalogue on every poll for as long as it is open. Unchanged files
// give back the same summaries without touching git at all.
//
// The stamp is every workflow file's name, size and modification time, plus the
// directory's own — so a save, a revert, an added file and a removed one all
// invalidate it, since every one of them writes. What it cannot see is a commit
// made behind the files' backs: committing a workflow by hand, in the craft
// directory, with git, leaves the listing showing the version it had a moment
// ago until the file itself next changes. Everything that writes here goes
// through Save or Revert, both of which write the file first.
func (r *Repo) List() ([]Summary, error) {
	entries, err := os.ReadDir(filepath.Join(r.dir, WorkflowDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("craft list: %w", err)
	}

	stamp := r.stampOf(entries)
	r.listing.Lock()
	defer r.listing.Unlock()
	if stamp != "" && stamp == r.stamp {
		return append([]Summary(nil), r.listed...), r.listErr
	}

	var summaries []Summary
	var skipped []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		path := WorkflowDir + "/" + entry.Name()
		data, err := os.ReadFile(filepath.Join(r.dir, path))
		if err != nil {
			skipped = append(skipped, fmt.Errorf("craft list: skipped %s: %w", entry.Name(), err))
			continue
		}
		w, err := Parse(data)
		if err != nil {
			skipped = append(skipped, fmt.Errorf("craft list: skipped %s: %w", entry.Name(), err))
			continue
		}
		summary := Summary{Name: name, Description: w.Description}
		if line, err := r.git("log", "-1", "--format=%H%x1f%aI", "--", path); err == nil {
			fields := strings.Split(strings.TrimSpace(line), "\x1f")
			summary.Commit = fields[0]
			if len(fields) > 1 {
				summary.When, _ = time.Parse(time.RFC3339, fields[1])
			}
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(a, b int) bool { return summaries[a].Name < summaries[b].Name })
	problems := errors.Join(skipped...)
	r.listed, r.listErr, r.stamp = summaries, problems, stamp
	// A copy, because the caller of a function that used to build a fresh slice
	// every time is entitled to keep sorting or trimming what it is given.
	return append([]Summary(nil), summaries...), problems
}

// forget drops the remembered catalogue, for the writers that know they have
// just invalidated it.
func (r *Repo) forget() {
	r.listing.Lock()
	defer r.listing.Unlock()
	r.listed, r.listErr, r.stamp = nil, nil, ""
}

// stampOf describes the workflows directory closely enough that an unchanged
// one is recognisable. It returns "" when the directory will not describe
// itself, which reads as "changed" and costs a re-read rather than a wrong
// answer.
func (r *Repo) stampOf(entries []os.DirEntry) string {
	directory, err := os.Stat(filepath.Join(r.dir, WorkflowDir))
	if err != nil {
		return ""
	}
	stamp := &strings.Builder{}
	fmt.Fprintf(stamp, "%d\x1f%d", directory.ModTime().UnixNano(), len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return ""
		}
		fmt.Fprintf(stamp, "\x1e%s\x1f%d\x1f%d", entry.Name(), info.Size(), info.ModTime().UnixNano())
	}
	return stamp.String()
}

// WriteVerifier commits one executable check. The path law is the same one the
// validator enforces on verify.script, applied here so a file can never be
// written outside the directory a workflow is allowed to point at.
func (r *Repo) WriteVerifier(relPath string, content []byte, mode os.FileMode, message string) (string, error) {
	return r.writeExecutable(VerifierDir, relPath, content, mode, message)
}

// WriteSkill commits one forged executable, under the same law.
func (r *Repo) WriteSkill(relPath string, content []byte, mode os.FileMode, message string) (string, error) {
	return r.writeExecutable(SkillDir, relPath, content, mode, message)
}

func (r *Repo) writeExecutable(dir, relPath string, content []byte, mode os.FileMode, message string) (string, error) {
	path, err := underDir(dir, relPath)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("craft write %s: empty message — a commit has to say what this is for", path)
	}
	if mode == 0 {
		mode = ScriptMode
	}
	full := filepath.Join(r.dir, path)

	gitLock.Lock()
	defer gitLock.Unlock()

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", fmt.Errorf("craft write %s: %w", path, err)
	}
	if err := os.WriteFile(full, content, mode); err != nil {
		return "", fmt.Errorf("craft write %s: %w", path, err)
	}
	// WriteFile honours the mode only when it creates the file; an overwrite
	// keeps whatever the old one had, and a verifier that lost its executable
	// bit fails as if the check failed.
	if err := os.Chmod(full, mode); err != nil {
		return "", fmt.Errorf("craft write %s: %w", path, err)
	}
	return r.commitIfChanged(subjected(path, message), path)
}

// settle is the one read path: parse, refuse an invalid file by name, clamp.
func (r *Repo) settle(name string, data []byte) (*Workflow, error) {
	w, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("craft load %s: %w", name, err)
	}
	if w.Name != name {
		return nil, fmt.Errorf("craft load %s: the file names itself %s — a workflow's name is its filename", name, w.Name)
	}
	if problems := w.Validate(); len(problems) > 0 {
		return nil, fmt.Errorf("craft load %s: %w", name, errors.Join(problems...))
	}
	w.Clamp()
	return w, nil
}

func (r *Repo) missing(name string) error {
	summaries, _ := r.List()
	names := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		names = append(names, summary.Name)
	}
	return fmt.Errorf("craft: no workflow named %s%s", name, didYouMean(name, names))
}

// commitIfChanged commits exactly the given path, or reports the version that
// already holds this content. Saving what is already saved is not a new
// version, and a repository that grows an empty commit every time a distiller
// re-derives the same file is a history nobody can read.
func (r *Repo) commitIfChanged(message string, path string) (string, error) {
	dirty, err := r.pending([]string{path})
	if err != nil {
		return "", err
	}
	if !dirty {
		commit, err := r.git("log", "-1", "--format=%H", "--", path)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(commit), nil
	}
	return r.commit(message, []string{path})
}

func (r *Repo) commit(message string, paths []string) (string, error) {
	args := append([]string{"add", "--"}, paths...)
	if _, err := r.git(args...); err != nil {
		return "", err
	}
	args = append([]string{"commit", "-q", "-m", message, "--"}, paths...)
	if _, err := r.git(args...); err != nil {
		return "", err
	}
	commit, err := r.git("rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(commit), nil
}

// pending reports whether any of the paths differ from HEAD or the index.
func (r *Repo) pending(paths []string) (bool, error) {
	args := append([]string{"status", "--porcelain", "--"}, paths...)
	out, err := r.git(args...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (r *Repo) git(args ...string) (string, error) {
	out, err := r.gitBytes(args...)
	return string(out), err
}

func (r *Repo) gitBytes(args ...string) ([]byte, error) {
	// Counted because forks are the cost this package has to keep an eye on:
	// one is about six milliseconds, and the surfaces that read the catalogue
	// read it on a timer.
	r.forks.Add(1)
	command := exec.Command("git", args...)
	command.Dir = r.dir
	command.Env = append(os.Environ(),
		// The craft repository answers to the resident's configuration, not to
		// the host's: a machine-wide gitconfig must not decide who authored a
		// version or whether a commit waits on a signing key.
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
	)
	var out, problem bytes.Buffer
	command.Stdout = &out
	command.Stderr = &problem
	if err := command.Run(); err != nil {
		said := strings.TrimSpace(problem.String())
		if said == "" {
			said = strings.TrimSpace(out.String())
		}
		if said == "" {
			said = err.Error()
		}
		return nil, fmt.Errorf("craft: git %s: %s", strings.Join(args, " "), said)
	}
	return out.Bytes(), nil
}

// workflowPath is the name law: a workflow name becomes a path, so it is held
// to the same slug the validator holds it to, before anything opens a file.
func workflowPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("craft: no workflow name given")
	}
	if !namePattern.MatchString(name) {
		return "", fmt.Errorf("craft: %q is not a workflow name — lowercase letters, digits, dash or underscore", name)
	}
	return WorkflowDir + "/" + name + ".yaml", nil
}

// underDir resolves a written path against the one directory it is allowed to
// live in. A bare name lands there; a path that already says the directory is
// accepted as written; anything that names somewhere else, climbs out, or
// arrives absolute is refused before it is opened.
func underDir(dir, relPath string) (string, error) {
	clean := strings.TrimSpace(strings.ReplaceAll(relPath, `\`, "/"))
	if clean == "" {
		return "", fmt.Errorf("craft: no path given for a file under %s/", dir)
	}
	if strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "~") {
		return "", fmt.Errorf("craft: %q must be repo-relative, not absolute", relPath)
	}
	if clean == dir {
		return "", fmt.Errorf("craft: %q names the directory, not a file in it", relPath)
	}
	trimmed := strings.TrimPrefix(clean, dir+"/")
	for _, part := range strings.Split(trimmed, "/") {
		switch part {
		case "", ".":
			return "", fmt.Errorf("craft: %q is not a clean path", relPath)
		case "..":
			return "", fmt.Errorf("craft: %q must not climb out of %s/ with ..", relPath, dir)
		}
	}
	if trimmed == clean && strings.Contains(clean, "/") {
		return "", fmt.Errorf("craft: %q must live under %s/", relPath, dir)
	}
	return dir + "/" + trimmed, nil
}

// subjected enforces the message discipline: the first line names what the
// version is about, then says what changed; everything the caller supplied as
// evidence — job ids, the failure that prompted this — stays in the body.
func subjected(name, message string) string {
	subject, body, _ := strings.Cut(strings.TrimSpace(message), "\n")
	subject = strings.TrimSpace(subject)
	if !strings.HasPrefix(subject, name+":") {
		subject = name + ": " + subject
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return subject
	}
	return subject + "\n\n" + body
}

func short(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
