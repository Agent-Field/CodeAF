package tui3

// The three readings this surface used to take from inside a frame, and the memo
// that took them off it (learned.go). framedisk_law_test.go is the structural
// half — nothing NEW may reach the disk from a draw — and these are the runtime
// half: the frame draws the picture, the picture is stat'd once, and `/workspace`
// answers without waiting on `git`.

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// The memo's own law, stated on a reader that counts: a name is read once, a
// miss the frame met is read by the loop that follows it, and only the tick and
// an explicit forget read a name twice.
func TestALearnedFactIsReadOncePerNameUntilSomethingSaysOtherwise(t *testing.T) {
	reads := map[string]int{}
	memo := newLearned(func(name string) string {
		reads[name]++
		return name + "!"
	})

	// THE FRAME'S DOOR READS NOTHING. It answers a miss and remembers the name.
	if got, known := memo.of("one"); known || got != "" {
		t.Fatalf("an unread name answered %q, %v", got, known)
	}
	if reads["one"] != 0 {
		t.Fatalf("the frame's door read the disk %d times", reads["one"])
	}
	// A SECOND FRAME ASKING THE SAME NAME IS STILL NOT A READING.
	memo.of("one")
	if reads["one"] != 0 {
		t.Fatalf("two frames cost %d readings", reads["one"])
	}

	// THE LOOP CATCHES UP, ONCE.
	if !memo.catchUp() {
		t.Fatal("the loop had nothing to catch up on")
	}
	if got, known := memo.of("one"); !known || got != "one!" {
		t.Fatalf("after the loop read it the frame got %q, %v", got, known)
	}
	if reads["one"] != 1 {
		t.Fatalf("catching up cost %d readings, want 1", reads["one"])
	}
	if memo.catchUp() {
		t.Fatal("the loop read a name nobody asked for")
	}

	// A HUNDRED MORE FRAMES COST NOTHING AT ALL.
	for i := 0; i < 100; i++ {
		memo.of("one")
	}
	if reads["one"] != 1 {
		t.Fatalf("a hundred frames cost %d readings", reads["one"])
	}

	// THE TICK IS THE ONE THING THAT ASKS AGAIN — that is the whole of how a file
	// somebody overwrote behind this surface's back is ever noticed.
	memo.refresh()
	if reads["one"] != 2 {
		t.Fatalf("the beat cost %d readings, want the name read again", reads["one"])
	}

	// AND SO IS A CALLER WHO KNOWS THE BYTES MOVED.
	memo.forget("one")
	if _, known := memo.of("one"); known {
		t.Fatal("a forgotten name was still answered")
	}
	memo.catchUp()
	if reads["one"] != 3 {
		t.Fatalf("forgetting cost %d readings, want the name read again", reads["one"])
	}
}

// The picture the frame draws is stat'd ONCE, whatever the paint clock does
// with it. It used to be stat'd on every frame because the modification time was
// the preview cache's key (imagepreview.go).
func TestTheFrameStatsOnePictureOnceAndNotOncePerPaint(t *testing.T) {
	a, _ := pictureApp(t, call("generate_image", `{"prompt":"a harbour"}`,
		".codeaf/images/harbour.png — 64×32 png, 1.2KB, generated on paint/model"))
	stats := 0
	// A FRESH MEMO, so what the turn's own arrival learned is not counted here:
	// what is being measured is what the FRAME costs from here on.
	a.pictures = newLearned(func(path string) pictureFact {
		stats++
		return statPictureFile(path)
	})

	rows := openFirst(t, a)
	if paintedRows(rows) != 0 {
		t.Fatal("the frame drew a picture it had not been told about")
	}
	if stats != 0 {
		t.Fatalf("the frame took %d stats", stats)
	}

	// THE LOOP ANSWERS WHAT THE FRAME ASKED FOR, once a message.
	if !a.catchUpLearning() {
		t.Fatal("the loop did not read the picture the frame asked about")
	}
	if stats != 1 {
		t.Fatalf("catching up took %d stats, want 1", stats)
	}
	a.touch()
	if paintedRows(plainRows(a)) == 0 {
		t.Fatal("the picture was not drawn from what the loop learned")
	}

	// AND THIRTY MORE FRAMES OF IT COST NOTHING.
	for i := 0; i < 30; i++ {
		a.touch()
		plainRows(a)
	}
	if stats != 1 {
		t.Fatalf("thirty frames took %d stats, want 1", stats)
	}
}

// The picture call's own ending is where the stat is taken, so the first frame
// after it already has the file — no window, no message, no beat in between.
func TestAPictureIsStatdWhenItsCallFinishes(t *testing.T) {
	a, _ := pictureApp(t, call("generate_image", `{"prompt":"a harbour"}`,
		".codeaf/images/harbour.png — 64×32 png, 1.2KB, generated on paint/model"))
	if paintedRows(openFirst(t, a)) == 0 {
		t.Fatal("the first frame after the call drew no picture")
	}
}

// The model cache is read at `open` and on the beat, never by a frame. It used
// to be re-read in full by every paint that had no catalog behind it, which is
// the first-run screen and every window opened with no key.
func TestTheModelCacheIsReadAtOpenAndNotOnEveryFrame(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	if err := WriteModelCache([]Model{{ID: "cached/one", ContextLength: 32_000}}); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(&fakeAgent{model: "m"})
	reads := 0
	a.modelLists = newLearned(func(name string) []Model {
		reads++
		return readModelCacheName(name)
	})
	a.learnModelLists()
	if reads == 0 {
		t.Fatal("`open` read nothing")
	}
	atOpen := reads

	a.models = nil
	for i := 0; i < 30; i++ {
		if got := a.modelList(); len(got) == 0 || got[0].ID != "cached/one" {
			t.Fatalf("the list came through as %v", got)
		}
	}
	if reads != atOpen {
		t.Fatalf("thirty readings of the list cost %d file reads, want the %d `open` took",
			reads, atOpen)
	}

	// AND THE BEAT IS WHAT NOTICES A FILE SOMEBODY ELSE REWROTE.
	if err := WriteModelCache([]Model{{ID: "cached/two"}}); err != nil {
		t.Fatal(err)
	}
	a.refreshLearning()
	if got := a.modelList(); len(got) == 0 || got[0].ID != "cached/two" {
		t.Fatalf("the beat did not pick the rewritten cache up: %v", got)
	}
}

// `/workspace <path>` takes the repository the way every other reading of it is
// taken: as a command, off the loop. It used to run `git rev-parse` and `git
// status` inline, under one four-hundred-millisecond ceiling, so the surface
// stopped dead for up to four tenths of a second on the keystroke that set it.
func TestWorkspaceAsksTheRepositoryOffTheLoop(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	dir := t.TempDir()
	a.anchorWorkspace = func(string) (string, error) { return dir, nil }
	probes := 0
	a.gitProbe = func(string) (string, bool, bool) {
		probes++
		return "trunk", true, true
	}

	cmd := a.slash("/workspace " + dir)
	if probes != 0 {
		t.Fatalf("the loop waited on %d repository probes", probes)
	}
	if a.workspace != dir {
		t.Fatalf("the workspace is %q, want %q", a.workspace, dir)
	}
	if a.branch != "" {
		t.Fatalf("a branch was drawn before anything answered: %q", a.branch)
	}
	if cmd == nil {
		t.Fatal("nothing was asked of the repository at all")
	}

	// AND THE ANSWER ARRIVES THE WAY EVERY OTHER PROBE'S ANSWER ARRIVES.
	msg, ok := run(cmd).(gitMsg)
	if !ok {
		t.Fatalf("the command answered %T, want a gitMsg", msg)
	}
	if probes != 1 {
		t.Fatalf("the command made %d probes, want 1", probes)
	}
	if !msg.ok || msg.branch != "trunk" || !msg.dirty {
		t.Fatalf("the probe answered %+v", msg)
	}
	a.update(msg)
	if a.branch != "trunk" {
		t.Fatalf("the branch is %q after the answer landed", a.branch)
	}
}

// The fourth reading the loop used to take is a PROCESS, and the door that
// starts it keeps one answer on the loop's own frame: there is nothing on this
// machine that opens a link. exec.Command records a PATH miss without forking,
// which is what lets the fork itself go to a goroutine (opener.go) while the six
// doors that call this still get their sentence on the keystroke that asked.
func TestNoOpenerOnPathIsAnAnswerAndNotAFork(t *testing.T) {
	if name, _ := openerCommand(); name == "" {
		t.Skip("this platform has no opener to miss")
	}
	t.Setenv("PATH", t.TempDir())
	if err := startOpener("https://example.invalid/"); err == nil {
		t.Fatal("a machine with no opener on PATH opened something anyway")
	}
}
