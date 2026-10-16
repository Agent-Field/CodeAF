package delegate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testBudget is a budget no test log reaches unless it means to.
var testBudget = ReadBudget{Bytes: 16 << 20, Lines: 100_000}

var launchAt = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

// writeLog writes lines to the action log in dir exactly as given.
func writeLog(t *testing.T, dir string, lines ...Action) {
	t.Helper()
	for _, line := range lines {
		if err := AppendAction(dir, line); err != nil {
			t.Fatal(err)
		}
	}
}

// appendRaw appends bytes to the action log in dir as they are.
func appendRaw(t *testing.T, dir string, raw []byte) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(dir, ActionsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(raw); err != nil {
		t.Fatal(err)
	}
}

func step(launch, command string) Action {
	return Action{At: launchAt, Kind: ActionStep, Command: command, Launch: launch}
}

func texts(shown []Shown) string {
	var words []string
	for _, line := range shown {
		words = append(words, line.Text)
	}
	return strings.Join(words, ",")
}

// countingReader is a program's reader that carries state from the launch's
// first line, as senior-dev's and review's do: each action is numbered by how
// many came before it.
func countingReader() ActionReader {
	seen := 0
	return func(action Action) (Shown, bool) {
		if action.Kind != ActionStep {
			return Shown{}, false
		}
		seen++
		return Shown{Text: fmt.Sprintf("%s#%d", action.Command, seen)}, true
	}
}

// ONE LAUNCH IS READ, AND NOTHING ELSE: an earlier launch's lines, a line
// written before launches were, an old launch's line written late after this
// launch began — none is this launch's, whatever its time.
func TestALaunchReadsOnlyItsOwnLines(t *testing.T) {
	dir := t.TempDir()
	old, mine := NewLaunch(), NewLaunch()
	writeLog(t, dir,
		Action{At: launchAt, Kind: ActionStage, Stage: "unstamped"},
		StartAction(launchAt, old),
		step(old, "old-a"),
		Action{At: launchAt.Add(time.Hour), Kind: ActionEnd, Status: StatusPass, Message: "old ending", Launch: old},
		StartAction(launchAt.Add(-time.Hour), mine), // the clock stepped back
		Action{At: launchAt.Add(-time.Hour), Kind: ActionStage, Stage: "implement", Launch: mine},
		step(mine, "a"),
		Action{At: launchAt.Add(2 * time.Hour), Kind: ActionEnd, Status: StatusFail, Message: "late old ending", Launch: old},
		step(mine, "b"),
	)
	page := ReadLaunchActions(context.Background(), dir, mine, Delegate{Name: "plain"}.Reader(), -1, 10, testBudget)
	if !page.Complete || page.Total != 3 || page.First != 0 || texts(page.Actions) != "implement,a,b" || page.LastStage != "implement" || page.Ending != nil {
		t.Fatalf("page = %+v", page)
	}
	// Its own ending, as its last line, is its ending; an old one after it is not.
	writeLog(t, dir, Action{At: launchAt, Kind: ActionEnd, Status: StatusPass, Message: "handed in", Launch: mine})
	page = ReadLaunchActions(context.Background(), dir, mine, Delegate{Name: "plain"}.Reader(), -1, 10, testBudget)
	if page.Ending == nil || page.Ending.Text != "handed in" || page.EndStatus != StatusPass {
		t.Fatalf("ending = %+v %q", page.Ending, page.EndStatus)
	}
	writeLog(t, dir, step(mine, "c"))
	if page = ReadLaunchActions(context.Background(), dir, mine, Delegate{Name: "plain"}.Reader(), -1, 10, testBudget); page.Ending != nil || page.EndStatus != "" {
		t.Fatalf("a launch that went on after its ending still wears it: %+v", page.Ending)
	}
	// A launch with no line at all, and a log that is not there, are complete
	// and empty — the one case where "nothing" is a fact.
	if page := ReadLaunchActions(context.Background(), dir, NewLaunch(), Delegate{}.Reader(), -1, 10, testBudget); !page.Complete || page.Total != 0 || page.Actions != nil {
		t.Fatalf("an unwritten launch = %+v", page)
	}
	if page := ReadLaunchActions(context.Background(), t.TempDir(), mine, Delegate{}.Reader(), -1, 10, testBudget); !page.Complete || page.Total != 0 {
		t.Fatalf("no log = %+v", page)
	}
	if page := ReadLaunchActions(context.Background(), dir, "not-a-launch", Delegate{}.Reader(), -1, 10, testBudget); page.Complete {
		t.Fatalf("a malformed launch read as complete: %+v", page)
	}
}

// EVERY ACTION OF A LAUNCH IS REACHABLE BY PAGING, each exactly as a reader fed
// the launch from its first line reads it, and the cursor's index never moves
// as the launch goes on.
func TestEveryActionOfALaunchIsReachableByPagingInItsCanonicalWords(t *testing.T) {
	dir := t.TempDir()
	old, mine := NewLaunch(), NewLaunch()
	writeLog(t, dir, StartAction(launchAt, old), step(old, "old"), StartAction(launchAt, mine))
	var want []string
	for i := range 137 {
		writeLog(t, dir, step(mine, fmt.Sprintf("s%d", i)))
		want = append(want, fmt.Sprintf("s%d#%d", i, i+1))
	}
	var got []string
	before := -1
	for pages := 0; ; pages++ {
		page := ReadLaunchActions(context.Background(), dir, mine, countingReader(), before, 25, testBudget)
		if !page.Complete || page.Total != 137+pages || len(page.Actions) > 25 || page.Actions == nil {
			t.Fatalf("page %d = %+v", pages, page)
		}
		got = append(strings.Split(texts(page.Actions), ","), got...)
		if page.First == 0 {
			break
		}
		before = page.First
		// The launch goes on between pages; the older pages do not move.
		writeLog(t, dir, step(mine, "later"))
	}
	if pages := len(got); pages != 137 {
		t.Fatalf("paged %d actions", pages)
	}
	if strings.Join(got, ",") != strings.Join(want[:137], ",") {
		t.Fatalf("paged\n%v\nwant\n%v", got, want[:137])
	}
}

// WHAT COULD NOT BE READ IS NOT READ AS NOTHING. A damaged or overlong line
// among the launch's, a launch longer than the budget, a read past its
// deadline: the page is not complete and says why. A damaged line of an
// EARLIER launch, before this one's start, costs this launch nothing.
func TestALaunchThatCouldNotBeReadWholeSaysWhy(t *testing.T) {
	mine := NewLaunch()
	reader := Delegate{Name: "plain"}.Reader()
	t.Run("a damaged line before the launch's start costs nothing", func(t *testing.T) {
		dir := t.TempDir()
		appendRaw(t, dir, []byte("{not json\n"))
		appendRaw(t, dir, append(bytes.Repeat([]byte("x"), maxLineBytes+10), '\n'))
		writeLog(t, dir, StartAction(launchAt, mine), step(mine, "a"))
		if page := ReadLaunchActions(context.Background(), dir, mine, reader, -1, 10, testBudget); !page.Complete || texts(page.Actions) != "a" {
			t.Fatalf("page = %+v", page)
		}
	})
	for name, raw := range map[string][]byte{
		"an unparseable line": []byte("{\"kind\":\"step\",\"launch\":\"" + mine + "\",\n"),
		"an overlong line":    append(bytes.Repeat([]byte("y"), maxLineBytes+10), '\n'),
	} {
		t.Run(name+" among the launch's", func(t *testing.T) {
			dir := t.TempDir()
			writeLog(t, dir, StartAction(launchAt, mine), step(mine, "a"))
			appendRaw(t, dir, raw)
			writeLog(t, dir, step(mine, "b"))
			page := ReadLaunchActions(context.Background(), dir, mine, reader, -1, 10, testBudget)
			if page.Complete || page.Cut != ReadCutDamaged || texts(page.Actions) != "a,b" {
				t.Fatalf("page = %+v", page)
			}
		})
	}
	t.Run("a line still being written is not yet a line", func(t *testing.T) {
		dir := t.TempDir()
		writeLog(t, dir, StartAction(launchAt, mine), step(mine, "a"))
		appendRaw(t, dir, []byte(`{"at":"2026-10-16T09:00:00Z","kind":"end","launch":"`+mine))
		if page := ReadLaunchActions(context.Background(), dir, mine, reader, -1, 10, testBudget); !page.Complete || page.Total != 1 || page.Ending != nil {
			t.Fatalf("page = %+v", page)
		}
	})
	t.Run("a launch longer than the budget", func(t *testing.T) {
		dir := t.TempDir()
		writeLog(t, dir, StartAction(launchAt, mine))
		for range 200 {
			writeLog(t, dir, step(mine, strings.Repeat("z", 150)))
		}
		page := ReadLaunchActions(context.Background(), dir, mine, reader, -1, 10, ReadBudget{Bytes: 4 << 10, Lines: 100})
		if page.Complete || page.Cut != ReadCutBudget || page.Actions != nil || page.Total != 0 {
			t.Fatalf("page = %+v", page)
		}
		page = ReadLaunchActions(context.Background(), dir, mine, reader, -1, 10, ReadBudget{Bytes: 16 << 20, Lines: 50})
		if page.Complete || page.Cut != ReadCutBudget {
			t.Fatalf("line budget: page = %+v", page)
		}
	})
	t.Run("a read whose caller has left", func(t *testing.T) {
		dir := t.TempDir()
		writeLog(t, dir, StartAction(launchAt, mine), step(mine, "a"))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if page := ReadLaunchActions(ctx, dir, mine, reader, -1, 10, testBudget); page.Complete || page.Cut != ReadCutDeadline {
			t.Fatalf("page = %+v", page)
		}
	})
	t.Run("a log the disk refuses", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root reads every file")
		}
		dir := t.TempDir()
		writeLog(t, dir, StartAction(launchAt, mine), step(mine, "a"))
		if err := os.Chmod(filepath.Join(dir, ActionsFile), 0); err != nil {
			t.Fatal(err)
		}
		if page := ReadLaunchActions(context.Background(), dir, mine, reader, -1, 10, testBudget); page.Complete || page.Cut != ReadCutUnreadable {
			t.Fatalf("page = %+v", page)
		}
	})
}

// THE START LINE IS FOUND BY ITS OWN BYTES, NEVER BY WORDS INSIDE ANOTHER LINE:
// a program that writes the start line's spelling into a command has it
// escaped, and the launch is still read from its real start.
func TestAStartLineSpelledInsideACommandIsNotAStart(t *testing.T) {
	dir := t.TempDir()
	mine := NewLaunch()
	writeLog(t, dir, StartAction(launchAt, mine), step(mine, "a"),
		step(mine, `"kind":"start","launch":"`+mine+`"`), step(mine, "c"))
	page := ReadLaunchActions(context.Background(), dir, mine, countingReader(), -1, 10, testBudget)
	if !page.Complete || page.Total != 3 || page.Actions[0].Text != "a#1" {
		t.Fatalf("page = %+v", page)
	}
}

// A LONG HISTORY COSTS THE LAUNCH, NOT THE HISTORY. Forty-eight megabytes of
// earlier launches sit before a short current one: the read finds the launch's
// start from the end and allocates a bounded amount, however long the log.
func TestALaunchAtTheEndOfALongHistoryIsReadInBoundedMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a 48 MiB log")
	}
	dir := t.TempDir()
	old, mine := NewLaunch(), NewLaunch()
	file, err := os.Create(filepath.Join(dir, ActionsFile))
	if err != nil {
		t.Fatal(err)
	}
	line := []byte(`{"at":"2026-10-16T09:00:00Z","kind":"step","launch":"` + old + `","command":"` + strings.Repeat("o", 2000) + `"}` + "\n")
	chunk := bytes.Repeat(line, 512)
	for written := 0; written < 48<<20; written += len(chunk) {
		if _, err := file.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	_ = file.Close()
	writeLog(t, dir, StartAction(launchAt, mine))
	for i := range 100 {
		writeLog(t, dir, step(mine, fmt.Sprintf("s%d", i)))
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	page := ReadLaunchActions(context.Background(), dir, mine, countingReader(), -1, 60, testBudget)
	runtime.ReadMemStats(&after)
	if !page.Complete || page.Total != 100 || page.First != 40 || page.Actions[0].Text != "s40#41" {
		t.Fatalf("page = %+v", page)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4<<20 {
		t.Fatalf("reading a 100-line launch at the end of a 48 MiB log allocated %d bytes", allocated)
	}
	// And a budget smaller than the history but larger than the launch is
	// still a complete read: only the launch counts against it.
	if page := ReadLaunchActions(context.Background(), dir, mine, countingReader(), -1, 60, ReadBudget{Bytes: 1 << 20, Lines: 1000}); !page.Complete || page.Total != 100 {
		t.Fatalf("budgeted page = %+v", page)
	}
}

// A CALL COUNT IS EVERY ONE OF THE LAUNCH'S CALLS OR IT IS NOT A COUNT: each
// call once as its latest record says, codeaf's refusals left out, another
// launch's calls — which reuse the same numbers — never among them; and a log
// longer than the budget, or with a line that does not parse, is unknown.
func TestALaunchsCallsAreCountedOnlyWhenEveryOneWasRead(t *testing.T) {
	dir := t.TempDir()
	old, mine := NewLaunch(), NewLaunch()
	for _, turn := range []Turn{
		{Seq: 1, Launch: old}, {Seq: 2, Launch: old}, {Seq: 3, Launch: old},
		{Seq: 1}, // before launches were
		{Seq: 1, Launch: mine}, {Seq: 1, Launch: mine, Ended: launchAt},
		{Seq: 2, Launch: mine, Refused: "the run's dollar ceiling is reached"},
		{Seq: 3, Launch: mine},
	} {
		if err := AppendTurn(dir, turn); err != nil {
			t.Fatal(err)
		}
	}
	if calls, known := CountLaunchCalls(context.Background(), dir, mine, testBudget); !known || calls != 2 {
		t.Fatalf("calls = %d known %v, want 2", calls, known)
	}
	if calls, known := CountLaunchCalls(context.Background(), dir, NewLaunch(), testBudget); !known || calls != 0 {
		t.Fatalf("a launch with no calls = %d %v", calls, known)
	}
	if calls, known := CountLaunchCalls(context.Background(), t.TempDir(), mine, testBudget); !known || calls != 0 {
		t.Fatalf("no log = %d %v", calls, known)
	}
	if _, known := CountLaunchCalls(context.Background(), dir, mine, ReadBudget{Bytes: 64, Lines: 100}); known {
		t.Fatal("a log over the budget was counted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, known := CountLaunchCalls(ctx, dir, mine, testBudget); known {
		t.Fatal("a cancelled count was known")
	}
	file, err := os.OpenFile(filepath.Join(dir, ConversationFile), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("{\"seq\":\n")
	_ = file.Close()
	if _, known := CountLaunchCalls(context.Background(), dir, mine, testBudget); known {
		t.Fatal("a log with a damaged line was counted")
	}
}
