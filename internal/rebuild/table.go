package rebuild

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// A folder may stay out of a seal when it is a function of things that do
// travel, and an install folder is a function of its lockfile. Nothing else is
// guessed here: not a data folder a script filled, not a folder that is only
// git-ignored. This file is the whole of what the rule knows, as data; a new
// ecosystem is a new row and nothing else changes.

// Lock is one file that rebuilds an install folder.
type Lock struct {
	// Name is the file's name.
	Name string
	// Beside, when set, is a file that must sit next to the lock for the lock to
	// count: a Cargo.lock with no Cargo.toml beside it is not a project's.
	Beside string
	// Accepts, when set, judges the lock's content: a requirements file counts
	// only when it pins every version, since an unpinned one resolves to today's
	// packages and not to the ones the chat ran against.
	Accepts func(content string) bool
	// Command is what usually rebuilds the folder from this lock. It is a hint the
	// agent is shown when it does not know what made the folder, never run.
	Command string
}

// Kind is one install folder and the locks that can rebuild it, best first.
type Kind struct {
	Folder string
	Locks  []Lock
}

var pinned = regexp.MustCompile(`^[A-Za-z0-9_.\-\[\]]+\s*==\s*\S+`)

// kinds is the table. A Go vendor folder is tracked by its project and so fails
// the rule's tracked test; it needs no row of its own to be kept.
var kinds = []Kind{
	{"node_modules", []Lock{
		{Name: "package-lock.json", Command: "npm ci"},
		{Name: "npm-shrinkwrap.json", Command: "npm ci"},
		{Name: "pnpm-lock.yaml", Command: "pnpm install --frozen-lockfile"},
		{Name: "yarn.lock", Command: "yarn install --frozen-lockfile"},
		{Name: "bun.lock", Command: "bun install --frozen-lockfile"},
		{Name: "bun.lockb", Command: "bun install --frozen-lockfile"},
	}},
	{".venv", pythonLocks},
	{"venv", pythonLocks},
	{"target", []Lock{{Name: "Cargo.lock", Beside: "Cargo.toml", Command: "cargo build"}}},
	{"vendor", []Lock{
		{Name: "composer.lock", Command: "composer install"},
		{Name: "Gemfile.lock", Command: "bundle install"},
	}},
	{"Pods", []Lock{{Name: "Podfile.lock", Command: "pod install"}}},
}

var pythonLocks = []Lock{
	{Name: "uv.lock", Command: "uv sync"},
	{Name: "poetry.lock", Command: "poetry install"},
	{Name: "pdm.lock", Command: "pdm install"},
	{Name: "Pipfile.lock", Command: "pipenv sync"},
	{Name: "requirements.txt", Accepts: allPinned, Command: "pip install -r requirements.txt"},
}

// allPinned reports whether every requirement line pins its version with `==`.
// A file with no requirement line pins nothing and does not count.
func allPinned(content string) bool {
	seen := false
	for _, line := range strings.Split(content, "\n") {
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "\\"))
		if line == "" {
			continue
		}
		if !pinned.MatchString(line) {
			return false
		}
		seen = true
	}
	return seen
}

var (
	byFolder = map[string]Kind{}
	lockName = map[string]bool{}
)

func init() {
	for _, k := range kinds {
		byFolder[k.Folder] = k
		for _, l := range k.Locks {
			lockName[l.Name] = true
		}
	}
}

// IsInstallFolder reports whether a folder's name is one the table knows.
func IsInstallFolder(name string) bool { _, ok := byFolder[name]; return ok }

// IsLockName reports whether a file's name is a lock the table knows.
func IsLockName(name string) bool { return lockName[name] }

// usable is whether the lock at rel counts: it is a regular file, its companion
// is beside it and its content passes.
func (l Lock) usable(tree, rel string) bool {
	info, err := os.Stat(filepath.Join(tree, filepath.FromSlash(rel)))
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if l.Beside != "" && !isFile(tree, path.Join(path.Dir(rel), l.Beside)) {
		return false
	}
	if l.Accepts == nil {
		return true
	}
	raw, err := os.ReadFile(filepath.Join(tree, filepath.FromSlash(rel)))
	return err == nil && l.Accepts(string(raw))
}

func isFile(tree, rel string) bool {
	info, err := os.Stat(filepath.Join(tree, filepath.FromSlash(rel)))
	return err == nil && info.Mode().IsRegular()
}

// Hint is the command that usually rebuilds a folder from the lock at lockRel,
// and "" where the table has none.
func Hint(lockRel string) string {
	for _, k := range kinds {
		for _, l := range k.Locks {
			if l.Name == path.Base(lockRel) {
				return l.Command
			}
		}
	}
	return ""
}
