package exec

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A LEAF'S ROOM IS SIZED IN ONE PLACE, AND THIS IS THE TEST OF IT.
//
// The same two figures — a fifteen-minute floor and a minute for every fifty
// thousand tokens above it — were written out longhand in five files, and the
// two minutes the node watchdog sits above the result in seven. None of the
// copies was wrong; what was wrong is that they had to be kept in step by hand
// across a change none of them could see. The claim reaper's window is this
// floor plus that pad plus five minutes, so a floor raised in the chat surface
// and nowhere else put the backstop BELOW the deadline it exists to sit above,
// which is not a backstop but the thing that fires first. That is the shape of
// the seventh failure in docs/design/failsafe/FAILSAFE.md, and it cost two
// benchmark runs their whole ninety-minute wall.
//
// So there is one author: [SubharnessInfo.Deadline] for the room, and
// [WatchdogAbove] for the pad over it. Every other file asks. A fifth copy of
// the arithmetic, or an eighth hand-written pad, fails the build here.
//
// The three trees are the ones a leaf's room is decided in: this package
// registers the shapes, internal/resident's reaper is measured from them, and
// cmd/aforge is every surface that dispatches a leaf.
var deadlineTrees = map[string]string{
	".":                "the subharness table is where a worker's budget shape is registered",
	"../resident":      "the claim reaper's window is measured from the generalist's floor",
	"../../cmd/aforge": "every surface that dispatches a leaf grants it its room here",
}

// deadlineShapeAuthors are the files allowed to write the linear shape's own
// numbers. Everything else asks [SubharnessFor] for them.
var deadlineShapeAuthors = map[string]string{
	"subharness.go": "linearInfo IS the shape; this is the one registration of it",
	// Not a leaf's room at all, and it only looks like one because fifteen
	// minutes is a common length of time.
	"resident.go": "settledFoldGrace is how long a settled job stays folded open",
}

// deadlinePadExceptions are the exact lines allowed to add a pad to a deadline
// by hand, spelled as they appear. There is one, and it is not the node
// watchdog: the reflex rung gets four exchanges and a seconds-scale backstop, a
// different bound on a deliberately tiny worker, and it is set beside the rung's
// other figures where a reader of them will find it.
var deadlinePadExceptions = map[string]string{
	"watchdog = deadline + 15*time.Second": "the reflex rung is seconds-scale on purpose",
}

var (
	// The linear shape's own two numbers, in either spelling. The floor is
	// word-bounded so that a 150_000-token budget is not mistaken for it.
	deadlineShape = regexp.MustCompile(`\b15\s*\*\s*time\.Minute\b|\b50_?000\b`)
	// A pad added to something called a deadline. It is deliberately not a
	// search for "2 * time.Minute": two minutes is an ordinary length of time
	// and several unrelated ones are correct; what is forbidden is deriving a
	// watchdog from a deadline anywhere but the one function that does it.
	deadlinePad = regexp.MustCompile(`(?i)deadline\b[^\n]*\+\s*\d+\s*\*\s*time\.(Minute|Second)\b`)
)

func TestOnlyTheSubharnessTableSizesALeafsRoom(t *testing.T) {
	for tree, why := range deadlineTrees {
		walkErr := filepath.WalkDir(tree, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			_, shapeAllowed := deadlineShapeAuthors[filepath.Base(path)]
			for index, line := range strings.Split(string(raw), "\n") {
				code := strings.TrimSpace(line)
				// A comment may quote the arithmetic; only code authors it.
				if code == "" || strings.HasPrefix(code, "//") {
					continue
				}
				if !shapeAllowed && deadlineShape.MatchString(code) {
					t.Errorf("%s:%d writes the leaf deadline's own shape — %s\n  %s\n"+
						"ask exec.SubharnessFor(name).Deadline(tokens) for it: "+
						"internal/exec/subharness.go is the only place in the process that sizes a leaf's room.",
						path, index+1, why, code)
				}
				if _, excused := deadlinePadExceptions[code]; excused {
					continue
				}
				if deadlinePad.MatchString(code) && !shapeAllowed {
					t.Errorf("%s:%d pads a deadline into a watchdog by hand\n  %s\n"+
						"ask exec.WatchdogAbove(deadline), or SubharnessInfo.Watchdog(tokens) "+
						"when the token grant is what you hold.",
						path, index+1, code)
				}
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walking %s: %v", tree, walkErr)
		}
	}
}

// The other half of the same law, so it cannot rot into "nobody writes fifteen
// minutes anywhere": the one author still says what it always said, and the
// watchdog still sits above it rather than under it.
func TestTheGeneralistsRoomIsStillTheShapeEverySurfaceWasBuiltOn(t *testing.T) {
	linear := SubharnessFor(LinearSubharness)
	if got := linear.Deadline(0); got != 15*time.Minute {
		t.Errorf("the generalist's floor is %s, want the 15m every surface used to write out", got)
	}
	if got := linear.Deadline(1_000_000); got != 20*time.Minute {
		t.Errorf("a million tokens buys %s, want 20m — a minute per fifty thousand", got)
	}
	if got := linear.Watchdog(0); got != 17*time.Minute {
		t.Errorf("the watchdog over the floor is %s, want 17m", got)
	}
	if WatchdogAbove(linear.Deadline(0)) != linear.Watchdog(0) {
		t.Error("the two doors onto the pad disagree; there is meant to be one pad")
	}
}
