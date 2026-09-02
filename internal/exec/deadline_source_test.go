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
// THE LAW READS THE ACT, NOT THE SPELLING. What it looks for is the write that
// hands a leaf its room — an assignment or a struct field whose target is
// called a deadline or a watchdog, sized from a literal or from arithmetic
// instead of from the table. So `deadline := 900 * time.Second` fails exactly
// as `15 * time.Minute` does, and `defaultDoWall = 15 * time.Minute` in
// cmd/aforge passes, because a run's wall is not a leaf's room and no amount of
// respelling either figure changes which of the two it is. Matching the digits
// rather than the act got both of those backwards once, and #379 was reverted
// for the wrong one.
//
// The reflex rung is the one leaf whose room is not in the table yet, and it is
// excused by name in both directions below; moving it into the table retires
// both exceptions.
//
// The three trees are the ones a leaf's room is decided in: this package
// registers the shapes, internal/resident's reaper is measured from them, and
// cmd/aforge is every surface that dispatches a leaf.
var deadlineTrees = map[string]string{
	".":                "the subharness table is where a worker's budget shape is registered",
	"../resident":      "the claim reaper's window is measured from the generalist's floor",
	"../../cmd/aforge": "every surface that dispatches a leaf grants it its room here",
}

// deadlineShapeAuthors are the files allowed to write a leaf's room from
// numbers of their own. Everything else asks [SubharnessFor] for it.
var deadlineShapeAuthors = map[string]string{
	"subharness.go": "linearInfo IS the shape; this is the one registration of it",
}

// deadlinePadExceptions are the exact lines allowed to add a pad to a deadline
// by hand, spelled as they appear. There is one, and it is not the node
// watchdog: the reflex rung gets four exchanges and a seconds-scale backstop, a
// different bound on a deliberately tiny worker, and it is set beside the rung's
// other figures where a reader of them will find it.
var deadlinePadExceptions = map[string]string{
	"watchdog = deadline + 15*time.Second": "the reflex rung is seconds-scale on purpose",
}

// deadlineWriteExceptions are the exact lines allowed to size a room from a
// number of their own, spelled as they appear. There is one, and it is the
// other half of the rung the pad exception above excuses.
var deadlineWriteExceptions = map[string]string{
	"reflexDeadline = 90 * time.Second": "the reflex rung is seconds-scale on purpose; " +
		"its pad is excused in deadlinePadExceptions for the same reason, and both figures " +
		"sit beside the rung's other numbers",
}

var (
	// deadlineAssignment finds the seam at which a line writes something: the
	// `=` of `x = v`, `x := v`, `x, ok := v` or `x += v`, whichever operator
	// carries it. Comparisons are not writes, so `==`, `!=`, `<=` and `>=` are
	// stepped over.
	deadlineAssignment = regexp.MustCompile(`(^|[^=!<>])(:?=)([^=]|$)`)
	// deadlineFieldKey is the other way a room is handed over: a struct
	// literal's `Deadline: 20 * time.Minute,`. The key must be an identifier,
	// which keeps map literals keyed by string and `case` clauses out.
	deadlineFieldKey = regexp.MustCompile(`^([A-Za-z_]\w*)\s*:\s*(\S.*?),?$`)
	// deadlineTarget is what makes a write a LEAF-ROOM write: the thing being
	// written is called a deadline or a watchdog. A selector counts, because
	// `shaped.deadline` is the same act as `deadline`.
	deadlineTarget = regexp.MustCompile(`(?i)deadline|watchdog`)
	// deadlineSized is a right-hand side that decides a length of time for
	// itself rather than being handed one — a unit, or a duration built out of
	// a number.
	deadlineSized = regexp.MustCompile(`time\.(Minute|Second|Hour)\b|time\.Duration\(`)
	// deadlineAsks is the whole of the permitted way to come by a leaf's room:
	// the table's two doors onto the shape, and the one door onto the pad.
	deadlineAsks = regexp.MustCompile(`\.Deadline\(|\.Watchdog\(|WatchdogAbove\(`)
	// deadlineFromTokens is the floor-and-a-minute-per-fifty-thousand
	// arithmetic in any spelling at all: a token count turned into a duration.
	// It catches the copy that names its variable something else entirely,
	// which the target rule above would let through.
	deadlineFromTokens = regexp.MustCompile(`(?i)time\.Duration\([^)]*okens[^)]*\)\s*\*|okens\s*/\s*\d[\d_]*\s*\)?\s*\*\s*time\.(minute|second|hour)`)
	// deadlinePad is a pad added to something called a deadline. It is
	// deliberately not a search for "2 * time.Minute": two minutes is an
	// ordinary length of time and several unrelated ones are correct; what is
	// forbidden is deriving a watchdog from a deadline anywhere but the one
	// function that does it.
	deadlinePad = regexp.MustCompile(`(?i)deadline\b[^\n]*\+\s*\d+\s*\*\s*time\.(Minute|Second)\b`)
)

// The verdicts [leafRoomFinding] returns: a line is either allowed, or it pads
// a deadline into a watchdog by hand, or it sizes a room the table owns.
const (
	roomAllowed = ""
	roomPads    = "pad"
	roomSizes   = "size"
)

// deadlineWriteTarget returns the thing a line of code writes to, and the value
// it writes, for the two shapes a leaf's room is ever handed over in: an
// assignment, and a struct literal's field. It returns false for a line that
// writes nothing.
//
// It is deliberately textual rather than a parse of the file. The law is about
// one line a reader's eye lands on, the failure names that line back to them,
// and the fixtures below can then be exactly the strings a person would write.
func deadlineWriteTarget(code string) (target, value string, writes bool) {
	if seam := deadlineAssignment.FindStringSubmatchIndex(code); seam != nil {
		// Group two is the operator itself; everything before it is the target,
		// with any compound-assignment operator trimmed off the end.
		left := strings.TrimRight(code[:seam[4]], " \t+-*/%&|^")
		return strings.TrimSpace(left), strings.TrimSpace(code[seam[5]:]), true
	}
	if field := deadlineFieldKey.FindStringSubmatch(code); field != nil {
		return field[1], field[2], true
	}
	return "", "", false
}

// leafRoomFinding is the whole predicate, on one line of code, with the file it
// came from left out on purpose: the act is the same act wherever it is
// written, and only the answer to "is this file an author?" differs.
func leafRoomFinding(code string) string {
	if deadlinePad.MatchString(code) {
		return roomPads
	}
	if target, value, writes := deadlineWriteTarget(code); writes &&
		deadlineTarget.MatchString(target) &&
		deadlineSized.MatchString(value) &&
		!deadlineAsks.MatchString(value) {
		return roomSizes
	}
	if deadlineFromTokens.MatchString(code) {
		return roomSizes
	}
	return roomAllowed
}

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
			if _, shapeAllowed := deadlineShapeAuthors[filepath.Base(path)]; shapeAllowed {
				return nil
			}
			for index, line := range strings.Split(string(raw), "\n") {
				code := strings.TrimSpace(line)
				// A comment may quote the arithmetic; only code authors it.
				if code == "" || strings.HasPrefix(code, "//") {
					continue
				}
				if _, excused := deadlinePadExceptions[code]; excused {
					continue
				}
				switch leafRoomFinding(code) {
				case roomPads:
					t.Errorf("%s:%d pads a deadline into a watchdog by hand\n  %s\n"+
						"ask exec.WatchdogAbove(deadline), or SubharnessInfo.Watchdog(tokens) "+
						"when the token grant is what you hold.",
						path, index+1, code)
				case roomSizes:
					// The rung excused by name, checked here rather than beside
					// the pad exception because it is a write and not a pad.
					if _, excused := deadlineWriteExceptions[code]; excused {
						continue
					}
					t.Errorf("%s:%d sizes a leaf's room from numbers of its own — %s\n  %s\n"+
						"ask exec.SubharnessFor(name).Deadline(tokens) for it: "+
						"internal/exec/subharness.go is the only place in the process that sizes a leaf's room.",
						path, index+1, why, code)
				}
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walking %s: %v", tree, walkErr)
		}
	}
}

// The predicate's own fixtures, because a structural law that reads source is
// only as good as what it thinks it is reading, and the walk above is silent
// when it is looking at the wrong thing. Every string here is one a person
// would plausibly write; each is judged by exactly the function the walk calls.
func TestTheLeafRoomLawReadsTheActNotTheSpelling(t *testing.T) {
	for _, fixture := range []struct {
		code    string
		finding string
		why     string
	}{
		{"defaultDoWall = 15 * time.Minute", roomAllowed,
			"a run's wall is not a leaf's room, whatever it is spelled as"},
		{"deadline := leafRoom.Deadline(tokens)", roomAllowed,
			"asking the table is the whole permitted way to come by a room"},
		{"deadline, ok := ctx.Deadline()", roomAllowed,
			"reading a context's deadline hands out nothing"},
		{"shaped.deadline = info.Deadline(build.maxTokens)", roomAllowed,
			"a selector target still asks"},
		{"watchdog := WatchdogAbove(deadline)", roomAllowed,
			"the one door onto the pad"},
		{"walled, release := context.WithDeadline(ctx, time.Now().Add(b.wall))", roomAllowed,
			"nothing here is called a deadline; the room came from elsewhere"},
		{"for giveUpAt := time.Now().Add(5 * time.Second); r.promotingNow() && time.Now().Before(giveUpAt); {", roomAllowed,
			"an instant a poll loop gives up at is not a room; naming it so is the fix, not an exception"},
		{"deadline := 900 * time.Second", roomSizes,
			"the old predicate read digits and this spelling walked past it"},
		{"shaped.deadline = 15 * time.Minute", roomSizes,
			"a leaf's room written out longhand, which is the whole law"},
		{"Deadline: 20 * time.Minute,", roomSizes,
			"a struct field hands a room over exactly as an assignment does"},
		{"leafDeadline = time.Duration(tokens/50_000) * time.Minute", roomSizes,
			"the table's own arithmetic, copied"},
		{"watchdog := deadline + 2*time.Minute", roomPads,
			"a watchdog derived from a deadline by hand"},
	} {
		if got := leafRoomFinding(fixture.code); got != fixture.finding {
			t.Errorf("leafRoomFinding(%q) = %q, want %q — %s",
				fixture.code, got, fixture.finding, fixture.why)
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
