package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// THE DROP LAWS, COUNTED.
//
// dropkeys.go exists because a drop does not always arrive in the shape
// imagepaste.go was written for. The owner dropped a screenshot on a real
// terminal over `--host` and got the escaped path as TEXT, then pressed enter
// and got `unknown command: /var/folders/…/Screenshot · try /help`. The bytes
// were the ones the paste door already handles perfectly; the DELIVERY was
// keystrokes.
//
// These tests are in two halves. The first states what a person gets, in every
// shape a terminal sends a drop in. The second states what it COSTS, the way
// PERF.md's doctrine demands: a count of the timers armed, the syscalls made
// and the frames built, never a stopwatch. Both halves have to hold, because a
// drop that lands and makes typing expensive is a defect somewhere else.

// dropLab is a conversation with files on the disk and a clock that does not
// move on its own, so a burst of characters can be delivered with provably
// nothing between them and the wakeup can be settled deliberately.
func dropLab(t *testing.T, files map[string]int) (*app, string, func(time.Duration)) {
	t.Helper()
	a, _, dir := attachLab(t, files)
	at := time.Now()
	a.clock = func() time.Time { return at }
	return a, dir, func(d time.Duration) { at = at.Add(d) }
}

// typeBurst delivers a run of characters the way a terminal that TYPES a drop
// delivers it: back to back, in one write, with no clock moving between them.
// It goes through [app.Update] so the fold is reached down the real road.
func typeBurst(a *app, text string) {
	for _, r := range text {
		a.Update(key(string(r)))
	}
}

// settleDrop is the quiet the fold waits for, and then its wakeup.
func settleDrop(t *testing.T, a *app, tick func(time.Duration)) {
	t.Helper()
	tick(2 * dropQuiet)
	drive(t, a, dropMsg{})
}

// ── what a person gets ──────────────────────────────────────────────────────

// THE OWNER'S FIRST REPORT. A screenshot dropped on a terminal that types the
// drop left the escaped path sitting in the box as text. It becomes the chip a
// pasted drop has always become.
func TestAScreenshotTypedInByTheTerminalBecomesAChip(t *testing.T) {
	a, dir, tick := dropLab(t, map[string]int{"Screenshot 2026-08-27 at 1.21.14 PM.png": 12})
	typeBurst(a, escapeSpaces(filepath.Join(dir, "Screenshot 2026-08-27 at 1.21.14 PM.png")))
	settleDrop(t, a, tick)

	if got := a.input.String(); got != "[image #1] " {
		t.Fatalf("the draft is %q, want the typed path replaced by its token", got)
	}
	if want := []string{"Screenshot 2026-08-27 at 1.21.14 PM.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
}

// THE OWNER'S SECOND REPORT, and the sentence it must never say again.
func TestADropTypedInAndEnteredIsNeverAnUnknownCommand(t *testing.T) {
	a, dir, _ := dropLab(t, map[string]int{"server.log": 12})
	typeBurst(a, filepath.Join(dir, "server.log"))
	// Enter INSIDE the quiet window, before the fold's own wakeup: the gesture
	// is spent on the way into the send rather than read as the text it left.
	drive(t, a, key("enter"))

	got := plain(frame(a))
	if strings.Contains(got, "unknown command") {
		t.Fatalf("a dropped file was refused as a command:\n%s", got)
	}
	// The tray emptied into the message, which is what enter does with it: the
	// sent line carries the file's name rather than the path's characters.
	if !strings.Contains(got, "[server.log]") {
		t.Fatalf("the sent line does not carry the dropped file:\n%s", got)
	}
	if strings.Contains(got, ".log ·") || strings.Contains(got, "/server.log") {
		t.Fatalf("the raw path survived into the conversation:\n%s", got)
	}
}

// AND THE NET UNDER THE FOLD. A drop that reached enter by some road the fold
// never saw — a terminal shape nobody has met yet — is still a drop, and the
// slash router says so before it refuses.
func TestTheEnterDoorCatchesADropTheFoldNeverSaw(t *testing.T) {
	a, dir, _ := dropLab(t, map[string]int{"notes.md": 12})
	a.input.setText(filepath.Join(dir, "notes.md"))
	a.drop = dropFold{}
	drive(t, a, key("enter"))

	if want := []string{"notes.md"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want the dropped file attached", chipNames(a))
	}
	if got := plain(frame(a)); !strings.Contains(got, "that was a file, not a command") {
		t.Fatalf("the surface said nothing about what it did:\n%s", got)
	}
}

// AND A COMMAND THAT NAMES NOTHING ON THE DISK REFUSES EXACTLY AS IT DID.
func TestAnUnknownCommandThatNamesNothingStillRefuses(t *testing.T) {
	a, _, _ := dropLab(t, nil)
	typeText(t, a, "/nonsense")
	drive(t, a, key("enter"))
	if got := plain(frame(a)); !strings.Contains(got, "unknown command: /nonsense") {
		t.Fatalf("an unknown command stopped refusing:\n%s", got)
	}
}

// EVERY SHAPE A TERMINAL WRITES A DROP IN, typed rather than pasted.
func TestEveryShapeOfATypedDropLandsOnTheTray(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]int
		typed func(dir string) string
		chips []string
		draft string
	}{
		{
			name:  "backslashed spaces",
			files: map[string]int{"my report.log": 8},
			typed: func(dir string) string { return escapeSpaces(filepath.Join(dir, "my report.log")) },
			chips: []string{"my report.log"},
		},
		{
			name:  "single quotes",
			files: map[string]int{"my report.log": 8},
			typed: func(dir string) string { return "'" + filepath.Join(dir, "my report.log") + "'" },
			chips: []string{"my report.log"},
		},
		{
			name:  "double quotes",
			files: map[string]int{"my report.log": 8},
			typed: func(dir string) string { return `"` + filepath.Join(dir, "my report.log") + `"` },
			chips: []string{"my report.log"},
		},
		{
			name:  "a file URL",
			files: map[string]int{"shot.png": 8},
			typed: func(dir string) string { return "file://" + filepath.Join(dir, "shot.png") },
			chips: []string{"shot.png"},
			draft: "[image #1] ",
		},
		{
			name:  "a percent-escaped file URL",
			files: map[string]int{"my report.log": 8},
			typed: func(dir string) string { return "file://" + filepath.Join(dir, "my%20report.log") },
			chips: []string{"my report.log"},
		},
		{
			name:  "two files in one drop",
			files: map[string]int{"one.log": 8, "two.log": 8},
			typed: func(dir string) string {
				return filepath.Join(dir, "one.log") + " " + filepath.Join(dir, "two.log")
			},
			chips: []string{"one.log", "two.log"},
		},
		{
			name:  "a picture and a file together",
			files: map[string]int{"shot.png": 8, "server.log": 8},
			typed: func(dir string) string {
				return filepath.Join(dir, "shot.png") + " " + filepath.Join(dir, "server.log")
			},
			chips: []string{"shot.png", "server.log"},
			draft: "[image #1] ",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, dir, tick := dropLab(t, tc.files)
			typeBurst(a, tc.typed(dir))
			settleDrop(t, a, tick)
			if !equalStrings(chipNames(a), tc.chips) {
				t.Fatalf("chips are %v, want %v", chipNames(a), tc.chips)
			}
			if got := a.input.String(); got != tc.draft {
				t.Fatalf("the draft is %q, want %q", got, tc.draft)
			}
		})
	}
}

// A DROP APPENDED AFTER WORDS ALREADY IN THE BOX keeps the words and takes the
// path, which is what somebody typing "what font is" and then dropping means.
func TestADropTypedAfterWordsKeepsTheWords(t *testing.T) {
	a, dir, tick := dropLab(t, map[string]int{"shot.png": 8})
	typeText(t, a, "what font is ")
	typeBurst(a, filepath.Join(dir, "shot.png"))
	settleDrop(t, a, tick)

	if got := a.input.String(); got != "what font is [image #1] " {
		t.Fatalf("the draft is %q, want the sentence with the picture's token in it", got)
	}
}

// AND A DROP INTO A BOX ALREADY HOLDING A COMMAND STAYS THE ARGUMENT IT IS.
// `/attach ` followed by a dropped file is somebody using the command exactly
// as the manual documents it, and turning the argument into a chip would break
// the one line on this surface whose whole job is to take a path.
func TestADropTypedAfterACommandStaysItsArgument(t *testing.T) {
	a, dir, tick := dropLab(t, map[string]int{"server.log": 8})
	path := filepath.Join(dir, "server.log")
	typeText(t, a, "/attach ")
	typeBurst(a, path)
	settleDrop(t, a, tick)

	if got := a.input.String(); got != "/attach "+path {
		t.Fatalf("the draft is %q, want the command with its path argument intact", got)
	}
	if len(a.chips) != 0 {
		t.Fatalf("the command's argument was attached early: %v", chipNames(a))
	}
	// And the command still runs, which is the whole reason the exception exists.
	drive(t, a, key("enter"))
	if want := []string{"server.log"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("/attach with a dropped argument attached %v", chipNames(a))
	}
}

// A `~` PATH IS A PATH. Terminals that write the home-relative form get the
// same chip, resolved against this machine's home the way /image resolves it.
func TestATildePathTypedInBecomesAChip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "shot.png"), make([]byte, 8), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _, tick := dropLab(t, nil)
	typeBurst(a, "~/shot.png")
	settleDrop(t, a, tick)
	if want := []string{"shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want the home-relative picture attached", chipNames(a))
	}
}

// A DROP THAT ARRIVES ONE CHARACTER AT A TIME, with the clock moving between
// every one of them — a multiplexer replaying a paste, a terminal that paces
// itself — still lands, because the fold settles on QUIET rather than on a
// deadline. Half-typed prefixes of the path name nothing and are silent.
func TestADropTypedSlowlyStillLands(t *testing.T) {
	a, dir, tick := dropLab(t, map[string]int{"server.log": 8})
	for _, r := range filepath.Join(dir, "server.log") {
		a.Update(key(string(r)))
		tick(2 * dropQuiet)
		drive(t, a, dropMsg{})
	}
	settleDrop(t, a, tick)
	if want := []string{"server.log"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want the slowly-typed drop attached", chipNames(a))
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the draft is %q, want the path taken out of it", got)
	}
}

// A RUN THAT NAMES NOTHING IS THE TEXT IT WAS TYPED AS, exactly and in order,
// with command mode intact underneath it.
func TestARunThatNamesNothingStaysTheTextItWas(t *testing.T) {
	a, _, tick := dropLab(t, nil)
	typeBurst(a, "/var/folders/nothing/here.png")
	settleDrop(t, a, tick)
	if got := a.input.String(); got != "/var/folders/nothing/here.png" {
		t.Fatalf("the draft is %q, want the characters exactly as typed", got)
	}
	if a.drop.took != 0 {
		t.Fatalf("a run naming nothing was taken as a drop %d times", a.drop.took)
	}
}

// A FOLDER TYPED IN IS SILENT UNTIL ENTER, and then it gets /attach's own
// sentence rather than "unknown command".
func TestAFolderTypedInIsSilentUntilEnter(t *testing.T) {
	a, dir, tick := dropLab(t, nil)
	inner := filepath.Join(dir, "logs")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	typeBurst(a, inner)
	settleDrop(t, a, tick)
	if got := plain(frame(a)); strings.Contains(got, "is a folder") {
		t.Fatalf("the surface interrupted somebody mid-gesture:\n%s", got)
	}
	if got := a.input.String(); got != inner {
		t.Fatalf("the draft is %q, want the folder's path still typed in it", got)
	}
	drive(t, a, key("enter"))
	if got := plain(frame(a)); !strings.Contains(got, "logs is a folder · attach a file") {
		t.Fatalf("a dropped folder was not answered:\n%s", got)
	}
}

// ── what it costs ───────────────────────────────────────────────────────────

// ORDINARY TYPING PAYS NOTHING. Not a timer, not a syscall, not a drop taken —
// this is the law that lets the fold sit on the one line every typed character
// in the program passes through.
func TestOrdinaryTypingArmsNothingAndAsksTheDiskNothing(t *testing.T) {
	a, _, _ := dropLab(t, nil)
	typeBurst(a, "fix the roof and then the gutter before it rains for a fortnight")
	if a.drop.armed != 0 {
		t.Fatalf("a sentence armed %d wakeups, want none", a.drop.armed)
	}
	if a.drop.looked != 0 {
		t.Fatalf("a sentence asked the disk %d times, want none", a.drop.looked)
	}
	if got := a.input.String(); got != "fix the roof and then the gutter before it rains for a fortnight" {
		t.Fatalf("the draft is %q", got)
	}
}

// AND SO DOES EVERY SLASH COMMAND. A dropped path is told from a command by a
// SEPARATOR INSIDE IT, which is string work on runes already in memory — so
// `/help` never reaches a clock or a syscall, however slowly it is typed.
func TestTypingASlashCommandArmsNothingAndAsksTheDiskNothing(t *testing.T) {
	for _, line := range []string{"/help", "/model", "/export", "/quit", "/nonsense", "/attach server.log"} {
		a, _, _ := dropLab(t, nil)
		typeBurst(a, line)
		if a.drop.armed != 0 {
			t.Fatalf("typing %q armed %d wakeups, want none", line, a.drop.armed)
		}
		if a.drop.looked != 0 {
			t.Fatalf("typing %q asked the disk %d times, want none", line, a.drop.looked)
		}
	}
	// And the command still runs, typed by hand, one character at a time.
	a, _, _ := dropLab(t, nil)
	typeText(t, a, "/help")
	drive(t, a, key("enter"))
	if got := plain(frame(a)); !strings.Contains(got, "settings") || strings.Contains(got, "unknown command") {
		t.Fatalf("/help stopped being a command:\n%s", got)
	}
}

// A BURST ARMS ONE TIMER, however many characters it holds. This is the pointer
// fold's own law (coalesce.go) restated for the keyboard: a wakeup is in flight
// or none is, and the one in flight re-arms itself rather than a second being
// asked for.
func TestATypedDropOfAnyLengthArmsOneWakeup(t *testing.T) {
	a, dir, tick := dropLab(t, map[string]int{"a/very/deeply/nested/place/server.log": 8})
	typeBurst(a, filepath.Join(dir, "a/very/deeply/nested/place/server.log"))
	if a.drop.armed != 1 {
		t.Fatalf("a burst of %d characters armed %d wakeups, want one",
			len(filepath.Join(dir, "a/very/deeply/nested/place/server.log")), a.drop.armed)
	}
	if a.drop.looked != 0 {
		t.Fatalf("a burst still arriving asked the disk %d times, want none", a.drop.looked)
	}
	settleDrop(t, a, tick)
	if a.drop.looked != 1 {
		t.Fatalf("a settled burst asked the disk %d times, want one", a.drop.looked)
	}
	if a.drop.took != 1 {
		t.Fatalf("a settled burst took %d drops, want one", a.drop.took)
	}
}

// A WAKEUP THAT FINDS THE BURST STILL ARRIVING WAITS AGAIN, and it is STILL one
// timer at a time — the fold never asks for a second while one is in flight.
func TestAWakeupInsideABurstWaitsAgainAndArmsNoSecondTimer(t *testing.T) {
	a, dir, _ := dropLab(t, map[string]int{"server.log": 8})
	path := filepath.Join(dir, "server.log")
	typeBurst(a, path[:len(path)-4])
	armed := a.drop.armed
	// The wakeup lands while the terminal is still sending.
	a.Update(dropMsg{})
	if a.drop.looked != 0 {
		t.Fatalf("a wakeup inside a burst asked the disk %d times, want none", a.drop.looked)
	}
	typeBurst(a, path[len(path)-4:])
	if a.drop.armed != armed+1 {
		t.Fatalf("the re-arm asked for %d wakeups, want exactly one more", a.drop.armed-armed)
	}
}

// A SETTLED BURST THAT NAMES NOTHING BUILDS NO FRAME. It provably mutated
// nothing [app.View] reads — the characters were already in the draft, typed by
// the keys that carried them — so the frame Bubble Tea asks for next is the one
// it was already given.
func TestASettledBurstThatNamesNothingBuildsNoFrame(t *testing.T) {
	a, _, tick := dropLab(t, nil)
	typeBurst(a, "/var/folders/nothing/here.png")
	a.frame()
	tick(2 * dropQuiet)
	a.Update(dropMsg{})
	if !a.ptr.still {
		t.Fatal("a wakeup that changed nothing asked for a frame")
	}
}

// AND THE DISK IS ASKED ONCE PER WORD AND NEVER PER CHARACTER, even when the
// burst holds several files.
func TestASettledBurstAsksTheDiskOncePerWordItHolds(t *testing.T) {
	a, dir, tick := dropLab(t, map[string]int{"one.log": 8, "two.log": 8})
	typeBurst(a, filepath.Join(dir, "one.log")+" "+filepath.Join(dir, "two.log"))
	settleDrop(t, a, tick)
	if a.drop.looked != 2 {
		t.Fatalf("a two-file drop went to the disk %d times, want once per file", a.drop.looked)
	}
	if a.drop.watched < 20 {
		t.Fatalf("the fold saw %d characters, want the whole burst", a.drop.watched)
	}
}

// ── the shape gate, stated on its own ───────────────────────────────────────

// THE STRING GATE IS WHAT KEEPS THE SYSCALLS AWAY FROM THE KEYBOARD, so it is
// stated outright rather than only through the surface.
func TestWhatALineHasToLookLikeBeforeTheDiskIsAsked(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"/var/folders/x/Screenshot.png", true},
		{`/var/folders/x/Screenshot\ 2026.png`, true},
		{"'/var/folders/x/Screen shot.png'", true},
		{"~/Desktop/shot.png", true},
		{"file:///var/folders/x/shot.png", true},
		{"/a/one.log /a/two.log", true},
		{"/help", false},
		{"/model", false},
		{"/export ~/chat.md", false},
		{"/attach /a/b.log", false},
		{"/", false},
		{"~", false},
		{"~/", false},
		{"file://", false},
		{"look at /a/b.png", false},
		{"", false},
	} {
		if got := droppedPathShape(tc.line); got != tc.want {
			t.Errorf("droppedPathShape(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// escapeSpaces writes a path the way a terminal writes a dropped one: for the
// shell it is pretending to type into.
func escapeSpaces(path string) string { return strings.ReplaceAll(path, " ", `\ `) }

var _ tea.Msg = dropMsg{}

// AND THE SAME TWO LAWS ON HOME'S OWN BOX, which is where every keystroke goes
// while the fullscreen list is up. Typing there is already a query over every
// project on the machine; it must not also be a timer and a syscall.
func TestTypingProseOnHomeArmsNothingAndAsksTheDiskNothing(t *testing.T) {
	a, _, _ := homeDropKeys(t)
	typeBurst(a, "fix the roof before it rains for a fortnight")
	if a.drop.armed != 0 {
		t.Fatalf("a sentence typed on home armed %d wakeups, want none", a.drop.armed)
	}
	if a.drop.looked != 0 {
		t.Fatalf("a sentence typed on home asked the disk %d times, want none", a.drop.looked)
	}
	if got := a.home.box.String(); got != "fix the roof before it rains for a fortnight" {
		t.Fatalf("home's box holds %q", got)
	}
}

// AND A SLASH COMMAND TYPED ON HOME COSTS WHAT IT COST BEFORE THE FOLD EXISTED.
// A dropped path is told from a command by a SEPARATOR INSIDE IT, which is
// string work on runes already in memory.
func TestTypingASlashCommandOnHomeArmsNothing(t *testing.T) {
	for _, line := range []string{"/help", "/model", "/quit"} {
		a, _, _ := homeDropKeys(t)
		typeBurst(a, line)
		if a.drop.armed != 0 {
			t.Fatalf("typing %q on home armed %d wakeups, want none", line, a.drop.armed)
		}
		if a.drop.looked != 0 {
			t.Fatalf("typing %q on home asked the disk %d times, want none", line, a.drop.looked)
		}
	}
}

// AND A TYPED DROP ON HOME PAYS EXACTLY WHAT ONE IN THE DRAFT PAYS: one timer
// for the whole burst, one syscall per word, one drop taken.
func TestATypedDropOnHomeArmsOneWakeupAndOneLook(t *testing.T) {
	a, drop, tick := homeDropKeys(t, "server.log")
	typeBurst(a, filepath.Join(drop, "server.log"))
	if a.drop.armed != 1 {
		t.Fatalf("a burst on home armed %d wakeups, want one", a.drop.armed)
	}
	if a.drop.looked != 0 {
		t.Fatalf("a burst still arriving asked the disk %d times, want none", a.drop.looked)
	}
	settleDrop(t, a, tick)
	if a.drop.looked != 1 {
		t.Fatalf("a settled burst on home asked the disk %d times, want one", a.drop.looked)
	}
	if a.drop.took != 1 {
		t.Fatalf("a settled burst on home took %d drops, want one", a.drop.took)
	}
}
