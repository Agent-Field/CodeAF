package calllog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// TEST TRAFFIC NEVER LANDS IN A PERSON'S LEDGER.
//
// The log is always on and works out its own path when nobody hands it one,
// which is exactly right for the product and exactly wrong under `go test`: a
// test binary inherits the AFORGE_HOME of whoever started it, so a package that
// streams scripted answers through a client appends its fiction to the ledger
// of the person at the keyboard. It has happened twice — 356 rows of
// `vendor/vision-model` priced at $4.25 beside one run's real rows (#286), and
// `test/model` / `cheap/one` rows the day before that — and the cost is not the
// disk space: anything that sums that file afterwards reports money nobody
// spent, and the fake vision row alone dwarfed the run it was interleaved with.
//
// So under test this package will not resolve a path it was not given. A log
// that would land in the state root the environment named is refused and the
// log is simply off; a test that WANTS one says where it goes — `Open` with a
// directory of its own, or the AFORGE_CALL_LOG pin — and gets one.
//
// THE GATE IS HERE RATHER THAN IN THE HELPERS THAT BUILD CLIENTS because a
// helper can be forgotten. `internal/provider` and `cmd/aforge` each pin the
// log off in their own TestMain, and every other package that reaches a client
// through them did not, which is the whole defect: the fix that lives in a test
// helper protects the packages somebody remembered and no others, and a package
// written next month starts unprotected again. There is exactly one place a
// path is resolved, and a gate on it holds for tests nobody has written yet.
var underTest = testing.Testing()

// profileDirEnv moves the profile — the key, the measured behaviour, and this
// log with them — out from under the state root, and it is the second root a
// test binary can inherit without asking for it. The name is spelled here
// rather than taken from internal/config, which owns it as
// config.ProfileDirEnv: config opens this log, so the import would be a cycle.
// `aforge logs` and `aforge doctor` spell it out for the same reason.
const profileDirEnv = "AFORGE_PROFILE_DIR"

// chosenPath is the path its caller may write to, and "" for one that would
// land in a root whoever started a test binary named. Outside a test it is the
// identity: the product's log resolves exactly as it always has.
func chosenPath(path string) string {
	if !underTest || path == "" {
		return path
	}
	// Both roots, because either can be the person's: the profile moves out
	// from under the state root when AFORGE_PROFILE_DIR is exported, and a log
	// refused at one of them while landing in the other would be the same
	// defect with a rarer environment.
	for _, root := range []string{home.Dir(), os.Getenv(profileDirEnv)} {
		if inside(root, path) {
			return ""
		}
	}
	return path
}

// inside reports whether path is the directory root or something under it. It
// compares by path elements rather than by string prefix, so a sibling named
// like the root — /state/root-2 beside /state/root — is not mistaken for a
// child of it.
func inside(root, path string) bool {
	if root = strings.TrimSpace(root); root == "" {
		return false
	}
	root, path = absolute(root), absolute(path)
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// absolute is filepath.Abs with the error swallowed, because the only caller
// wants two paths it can compare and a working directory that cannot be read is
// not a reason to answer the question wrongly in the permissive direction — an
// unresolvable path stays as it is and fails the comparison.
func absolute(path string) string {
	if resolved, err := filepath.Abs(path); err == nil {
		return resolved
	}
	return path
}
