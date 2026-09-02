package lane

import (
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// A TEST NEVER READS THE STATE OF WHOEVER RAN IT.
//
// This package keeps two files under the state root — the belief store
// (`v3/lanes.json`) and one sheet cache per model (`v3/lanes/{model}.json`) —
// and both resolve their own path when nobody hands them one, which is exactly
// right for the product and exactly wrong under `go test`: a test binary
// inherits the AFORGE_HOME of whoever started it, so a test that asks this
// package a question is answered out of the person's own router state.
//
// It happened. `TestAnEmptyLedgerIsAnEmptyChoice` asserts the honest answer on
// a fresh machine — no beliefs, therefore no opinion — and it went red for good
// on this build box the first time a real run wrote
// `~/.aforge/v3/lanes/deepseek%2Fdeepseek-v4-flash.json` (#475). Nothing in the
// test named a file, or a home, or a sheet: it built a chooser with a fake
// ledger, and the chooser reached the live registry's sheet for the rows the
// ledger had none of. That is the whole class — the reach-through nobody wrote
// down — and it is why the guard cannot live in the test.
//
// THE GATE IS AT THE RESOLUTION SEAM rather than in the tests, for the reason
// #352 gave about the call log: a test-side fix protects the tests somebody
// remembered, and a test written next month starts unprotected again. The law
// this directory already carried (`TestNoTestWritesTheRealHome`) is syntactic —
// it asks whether a test function mentions the registry — and a chooser that
// reaches the registry from inside slips straight past it. There are exactly
// two places a path is resolved here, and a gate on them holds for tests
// nobody has written yet.
//
// A TEST THAT WANTS A FILE STILL GETS ONE, because what is refused is an
// INHERITED root and never a root the test chose. The deliberate ways through
// are acts rather than omissions, and all three already exist: `t.Setenv` of
// AFORGE_HOME to a directory of the test's own (which is what most of this
// directory's tests do), `store.at` for the belief file, and `sheet.cacheIn`
// for the cache. None of them can be reached by forgetting.
var underTest = testing.Testing()

// profileDirEnv moves the profile — the key, the measured behaviour and this
// package's files with them — out from under the state root, and it is the
// second root a test binary is handed without asking. It is spelled here rather
// than taken from internal/config, which routes through this package.
const profileDirEnv = "AFORGE_PROFILE_DIR"

// inheritedRoots are the roots this process was STARTED with, read once at
// package initialisation — before any test has run, because `t.Setenv` can only
// happen inside one.
//
// The capture is the whole difference between this gate and the call log's,
// which compares against the live root instead. That is right for a log nobody
// wants under test and wrong here: a lane test that points AFORGE_HOME at its
// own `t.TempDir()` and then reads the store back is doing the correct thing,
// and a gate that watched the live root would refuse it. So what is refused is
// the root nobody chose.
var inheritedRoots = []string{home.Dir(), os.Getenv(profileDirEnv)}

// stateFile names a file under the state root, and answers "" for one that
// would land in a root a test binary merely inherited.
//
// Both this package's readers already treat "" as "there is nothing there" —
// the store answers [ErrNoStore] and the sheet reads no cache — so the refusal
// needs no new state anywhere: a test binary simply runs on a machine that has
// never routed anything, which is the state every one of these tests describes.
//
// Outside a test binary it is [home.Join] and nothing else: the product's files
// resolve exactly as they always have.
func stateFile(elements ...string) string {
	path := home.Join(elements...)
	if !underTest {
		return path
	}
	for _, root := range inheritedRoots {
		if home.Contains(root, path) {
			return ""
		}
	}
	return path
}
