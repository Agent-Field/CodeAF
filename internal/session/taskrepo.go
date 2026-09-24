package session

// WHICH REPOSITORY A PIECE OF WORK WAS ON, AND WHICH OTHER CHATS WERE ON IT TOO.
//
// The project folder a conversation is filed under is the folder codeaf was
// launched in, and that is not the repository the work was about. A chat opened
// in the home folder that works on a repository is filed under a different
// project than a chat opened inside that repository, so a reading scoped by the
// folder alone never lets the two see each other. Measured on 2026-09-24: 17 of
// the 26 cases where two chats touched the same file within a day were exactly
// this, and the `<elsewhere>` block (taskdelta.go) could show none of them.
//
// So the block is scoped by the repository AS WELL AS by the folder. Every
// project folder on the machine is read for work on a repository this chat is
// on, and the rows are ranked by how much they have to do with this chat
// ([elsewhereScope.score]): shared files first, then the same repository, then
// the same project folder. A row that has nothing to do with this chat is not
// news here and is never shown.
//
// ── THE IDENTITY IS READ OFF THE DISK, NEVER OUT OF A SUBPROCESS ──
//
// A repository is its git common directory: the `.git` folder of the main
// checkout, which every linked worktree of the same repository points back at.
// Two chats in two worktrees of one repository are on the same repository, and
// the common directory is the one path they share. It is read by walking up to
// the nearest `.git` and following a worktree's link file, which is a handful of
// stats and at most two small reads per directory — cheap enough to take for
// every row of a reading, and nothing a turn has to wait on a child process for.
//
// ── A REPOSITORY THAT COULD NOT BE READ IS NOT "NO REPOSITORY" ──
//
// A folder with no `.git` above it is not a repository, and that is a fact:
// nothing on the machine can be on the same repository as it, and saying
// nothing is the truth. A `.git` link that points at a folder that is gone, or a
// file that could not be read, is a different fact: the repository is there and
// could not be looked at. The two must not read the same ([errNotRepository]
// is the only answer that means the first), because a reading that went quiet
// over the second would tell the model nobody else is on its repository.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// errNotRepository is the one answer from [repoIdentity] that means the folder
// simply is not in a repository. Every other error means it could not be told.
var errNotRepository = errors.New("not in a git repository")

// repoIdentity is the repository dir is in, spelled as its git common directory,
// or [errNotRepository] when no `.git` stands above it.
//
// A `.git` FOLDER is a main checkout and is its own common directory. A `.git`
// FILE is a link — a linked worktree, or a submodule — and names the folder
// git keeps that checkout's state in; a worktree's state folder holds a
// `commondir` file naming the repository it belongs to, and a submodule's does
// not, because a submodule is a repository of its own.
func repoIdentity(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", errNotRepository
	}
	dir = filepath.Clean(dir)
	for {
		link := filepath.Join(dir, ".git")
		info, err := os.Stat(link)
		switch {
		case err == nil && info.IsDir():
			return canonicalPath(link), nil
		case err == nil:
			return gitLinkCommonDir(link)
		case !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR):
			return "", fmt.Errorf("%s could not be read: %w", link, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errNotRepository
		}
		dir = parent
	}
}

// gitLinkCommonDir follows one `.git` link file to the repository it belongs to.
func gitLinkCommonDir(link string) (string, error) {
	raw, err := os.ReadFile(link)
	if err != nil {
		return "", fmt.Errorf("%s could not be read: %w", link, err)
	}
	line := strings.TrimSpace(firstLine(string(raw)))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", fmt.Errorf("%s is not a git link", link)
	}
	state := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if state == "" {
		return "", fmt.Errorf("%s names no folder", link)
	}
	if !filepath.IsAbs(state) {
		state = filepath.Join(filepath.Dir(link), state)
	}
	if info, err := os.Stat(state); err != nil || !info.IsDir() {
		return "", fmt.Errorf("the git link %s points at %s, which is not there", link, state)
	}
	common := state
	raw, err = os.ReadFile(filepath.Join(state, "commondir"))
	switch {
	case err == nil:
		named := strings.TrimSpace(string(raw))
		if named == "" {
			return "", fmt.Errorf("%s names no repository", filepath.Join(state, "commondir"))
		}
		if !filepath.IsAbs(named) {
			named = filepath.Join(state, named)
		}
		common = named
	case !errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("%s could not be read: %w", filepath.Join(state, "commondir"), err)
	}
	return canonicalPath(common), nil
}

// repoResolver answers [repoIdentity] once per folder for one reading. The same
// few grounds recur on every row a project has run, and a reading that walked
// up from each of them once per row would pay for the same answer hundreds of
// times.
type repoResolver map[string]repoAnswer

type repoAnswer struct {
	repo string
	err  error
}

func (r repoResolver) of(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", errNotRepository
	}
	if answer, held := r[dir]; held {
		return answer.repo, answer.err
	}
	repo, err := repoIdentity(dir)
	r[dir] = repoAnswer{repo: repo, err: err}
	return repo, err
}

// rowRepo is the repository one index row was about: the identity it recorded,
// or — for a row written before rows carried one — the identity of the ground it
// names. "" is a row whose repository nobody can say, and a reading keyed by the
// repository leaves such a row out rather than guessing it into or out of scope.
func (r repoResolver) rowRepo(row TaskIndexEntry) string {
	if repo := strings.TrimSpace(row.Repo); repo != "" {
		return repo
	}
	repo, err := r.of(row.Ground)
	if err != nil {
		return ""
	}
	return repo
}

// ── HOW MUCH A ROW HAS TO DO WITH THIS CHAT ─────────────────────────────────

// The weights of [elsewhereScope.score]. A shared file is the strongest signal
// there is — the measurement found every overlapping pair by its files — so it
// outranks the repository, and the repository outranks the folder, which is the
// weakest link two pieces of work can have and still be news to each other.
const (
	elsewhereSharedFileWeight = 4
	elsewhereSharedFileCap    = 12
	elsewhereSameRepoWeight   = 3
	elsewhereSameFolderWeight = 1
)

// elsewhereScope is what this chat is on: the repositories its working
// directory and its own work resolve to, and the files its own work touched.
type elsewhereScope struct {
	repos   map[string]bool
	files   map[string]bool
	resolve repoResolver
}

// score is how much one row has to do with this chat, and 0 is nothing at all:
// a row with no file, repository or folder in common is not shown.
func (s elsewhereScope) score(sameFolder bool, repo string, files []string) int {
	score := 0
	shared := 0
	for _, path := range files {
		if s.files[strings.TrimSpace(path)] {
			shared++
		}
	}
	if shared > 0 {
		score += min(shared*elsewhereSharedFileWeight, elsewhereSharedFileCap)
	}
	if repo != "" && s.repos[repo] {
		score += elsewhereSameRepoWeight
	}
	if sameFolder {
		score += elsewhereSameFolderWeight
	}
	return score
}

// elsewhereReading is everything one reading hands the block: the landings and
// the live work, each already scored, and the lines that say what could not be
// looked at.
type elsewhereReading struct {
	landed []TaskIndexEntry
	scores map[string]int
	names  map[string]string
	live   []ElsewhereTask
	notes  []string
}

// elsewhereReader is one reading in progress: what this chat is on, which ids
// are its own, how far back the past half reaches, and what has been found.
// It is a type so each step of the reading is one short method rather than one
// long function with every step's decisions in it.
type elsewhereReader struct {
	scope elsewhereScope
	own   map[string]bool
	mine  []string
	since time.Time
	now   time.Time
	out   elsewhereReading
}

// readElsewhereWide is one reading of every project folder beside this chat's
// own, for work on a repository this chat is on.
//
// THE CHAT'S OWN FOLDER IS READ AS IT ALWAYS WAS: every other window in it is
// in scope by the folder alone. Every OTHER folder is read only for rows on one
// of this chat's repositories, and only when this chat is on one at all — a
// chat in a folder that is not a repository pays for no reading of the rest of
// the machine.
func (a *Agent) readElsewhereWide(dir string, mine []string, since, now time.Time) elsewhereReading {
	bucket := filepath.Dir(dir)
	reader := &elsewhereReader{
		scope: elsewhereScope{repos: map[string]bool{}, files: map[string]bool{}, resolve: repoResolver{}},
		own:   make(map[string]bool, len(mine)),
		mine:  mine,
		since: since,
		now:   now,
		out:   elsewhereReading{scores: map[string]int{}, names: map[string]string{}},
	}
	for _, id := range mine {
		if id = strings.TrimSpace(id); id != "" {
			reader.own[id] = true
		}
	}
	rows := ReadTaskIndex(filepath.Join(bucket, taskIndexName))
	reader.learnScope(a, rows)
	for _, row := range rows {
		reader.keep(row, true, "")
	}
	// THE PRESENT HALF of this folder: its other windows, as they always were.
	for _, task := range a.Elsewhere().Tasks() {
		task.score = reader.scope.score(true, "", task.Task.Files)
		reader.out.live = append(reader.out.live, task)
	}
	if len(reader.scope.repos) > 0 {
		reader.readOtherFolders(filepath.Dir(bucket), bucket)
	}
	return reader.out
}

// learnScope is WHAT THIS CHAT IS ON: its working directory, the ground of the
// run it has out, the repositories and files its own landed work recorded, and
// the files its own running tasks have written so far.
func (r *elsewhereReader) learnScope(a *Agent, rows []TaskIndexEntry) {
	workspace := strings.TrimSpace(a.config.Workspace)
	if workspace == "" {
		workspace = strings.TrimSpace(a.config.Place.Workspace)
	}
	if repo, err := r.scope.resolve.of(workspace); err == nil {
		r.scope.repos[repo] = true
	} else if !errors.Is(err, errNotRepository) {
		r.out.notes = append(r.out.notes, "other project folders were not searched: this conversation's repository could not be resolved ("+deltaLine(err.Error())+")")
	}
	a.beltMu.Lock()
	ground := ""
	if a.beltRun != nil {
		ground = a.beltRun.ground
	}
	a.beltMu.Unlock()
	if repo, err := r.scope.resolve.of(ground); err == nil {
		r.scope.repos[repo] = true
	}
	for _, row := range rows {
		if !r.own[strings.TrimSpace(row.SessionID)] {
			continue
		}
		if repo := r.scope.resolve.rowRepo(row); repo != "" {
			r.scope.repos[repo] = true
		}
		r.learnFiles(row.Files)
	}
	for _, task := range a.presenceTasks() {
		r.learnFiles(task.Files)
	}
}

func (r *elsewhereReader) learnFiles(files []string) {
	for _, path := range files {
		if path = strings.TrimSpace(path); path != "" {
			r.scope.files[path] = true
		}
	}
}

// keep takes one landed row into the past half when it is another chat's, ended
// since the reach, and has something to do with this chat. A row from another
// folder has to be on one of this chat's repositories to count at all.
func (r *elsewhereReader) keep(row TaskIndexEntry, sameFolder bool, project string) {
	if row.Live() || row.EndedAt.IsZero() || !row.EndedAt.After(r.since) {
		return
	}
	if id := strings.TrimSpace(row.SessionID); id == "" || r.own[id] {
		return
	}
	repo := r.scope.resolve.rowRepo(row)
	if !sameFolder && (repo == "" || !r.scope.repos[repo]) {
		return
	}
	score := r.scope.score(sameFolder, repo, row.Files)
	if score == 0 {
		return
	}
	key := row.SessionID + "\x00" + row.ID
	r.out.scores[key] = score
	if project != "" {
		r.out.names[key] = project
	}
	r.out.landed = append(r.out.landed, row)
}

// readOtherFolders reads every project folder under root except this chat's own.
func (r *elsewhereReader) readOtherFolders(root, bucket string) {
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		r.out.notes = append(r.out.notes, "other project folders could not be listed ("+deltaLine(err.Error())+")")
	}
	for _, entry := range entries {
		other := filepath.Join(root, entry.Name())
		if !entry.IsDir() || filepath.Clean(other) == filepath.Clean(bucket) {
			continue
		}
		r.readOtherFolder(other)
	}
}

// readOtherFolder reads one other project folder: its landed rows and its live
// windows, each kept only when it is on one of this chat's repositories. The
// folder's name is looked up once, and only when something in it is kept or
// could not be read.
func (r *elsewhereReader) readOtherFolder(other string) {
	name := ""
	named := func() string {
		if name == "" {
			name = elsewhereProjectName(other)
		}
		return name
	}
	theirs, err := readTaskIndexChecked(filepath.Join(other, taskIndexName))
	if err != nil {
		r.out.notes = append(r.out.notes, "the record of project "+named()+" could not be read ("+deltaLine(err.Error())+")")
	}
	for _, row := range theirs {
		if repo := r.scope.resolve.rowRepo(row); repo != "" && r.scope.repos[repo] {
			r.keep(row, false, named())
		}
	}
	for _, window := range ReadProjectPresence(other, r.now, r.mine...) {
		repo, err := r.scope.resolve.of(window.Workspace)
		if err != nil || !r.scope.repos[repo] {
			continue
		}
		meta, _ := LoadMeta(window.Dir)
		for _, task := range window.RunningTasks {
			r.out.live = append(r.out.live, ElsewhereTask{
				SessionID: window.SessionID,
				Session:   strings.TrimSpace(meta.Title),
				Project:   named(),
				Task:      task,
				score:     r.scope.score(false, repo, task.Files),
			})
		}
	}
}

// elsewhereProjectName is what a row from another project folder calls that
// folder: world.go's own naming ([projectName]) of the workspace its first
// conversation recorded, and the folder's own name when none of them said.
func elsewhereProjectName(bucket string) string {
	path := ""
	if entries, err := os.ReadDir(bucket); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if meta, err := LoadMeta(filepath.Join(bucket, entry.Name())); err == nil && strings.TrimSpace(meta.Workspace) != "" {
				path = meta.Workspace
				break
			}
		}
	}
	return projectName(path, filepath.Base(bucket))
}

// readTaskIndexChecked is [ReadTaskIndex] that says when the file is there and
// could not be opened. A missing record is a project that never ran a task and
// answers nothing; a record that could not be read is a project whose history
// this reading did not see, and the block says so rather than reading it as a
// project with nothing in it.
func readTaskIndexChecked(path string) ([]TaskIndexEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	_ = file.Close()
	return ReadTaskIndex(path), nil
}
