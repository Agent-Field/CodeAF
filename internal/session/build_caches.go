package session

// build_caches.go is the one list of BUILD CACHES a run's landing leaves out
// when the run leaves them untracked, and the rule that reads it.
//
// A RUN'S LANDING COMMITS WHAT THE COPY HOLDS ([beltTreeWork]), because a shell
// worker keeps no ledger and git's status is the record. In a repository with
// no .gitignore, that record includes what the interpreter and the tools wrote
// while the worker ran the tests: a fresh-install run landed its one-line fix
// with two new `__pycache__/*.pyc` files beside it, on the person's branch.
// Those files are never anybody's work. The machine made them, the next run
// makes them again, and no reviewer wants them in a diff.
//
// IT IS NOT [harnessWrote], AND IT IS NARROWER ON PURPOSE. That rule answers
// which paths are codeaf's own, and its law is that a path is the harness's
// only because the harness wrote it there. A rule that went by what a file of
// that kind is usually called dropped every lockfile a run changed. This list
// is a different question, which files are caches, answered by exact names
// and never by a resemblance:
//   - an entry ending in `/` is a directory, matched as a whole path segment
//     anywhere in the path;
//   - an entry starting with `*` is a file's ending, matched on the basename;
//   - any other entry is a whole basename.
// And it applies ONLY to an untracked file. A cache the repository already
// tracks lands when it changes, and one the worker staged or committed itself
// lands too: that is the only way a shell worker says it meant a file.

import "strings"

// buildCacheNames is THE LIST, spelled the way the manual quotes it.
var buildCacheNames = []string{
	"__pycache__/",
	"*.pyc",
	".pytest_cache/",
	".mypy_cache/",
	".ruff_cache/",
	".DS_Store",
}

// buildCache says whether a slash-separated path inside a working copy is one
// of the caches [buildCacheNames] lists. A path whose last segment only
// shares a cache's name as part of a longer one — `pycache_notes.md`,
// `my.pyc.txt`, `DS_Store.md` — is not.
func buildCache(path string) bool {
	segments := strings.Split(path, "/")
	base := segments[len(segments)-1]
	folders := segments[:len(segments)-1]
	for _, name := range buildCacheNames {
		switch {
		case strings.HasSuffix(name, "/"):
			for _, folder := range folders {
				if folder == strings.TrimSuffix(name, "/") {
					return true
				}
			}
		case strings.HasPrefix(name, "*"):
			ending := strings.TrimPrefix(name, "*")
			if len(base) > len(ending) && strings.HasSuffix(base, ending) {
				return true
			}
		case base == name:
			return true
		}
	}
	return false
}
