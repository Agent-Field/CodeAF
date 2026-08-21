package session

// The preflight's whole contract is a decision and a sentence, so this file
// tests both: which of the four answers one situation earns, and — for the two
// that say nothing — that nothing is what comes back. The emptiness cases are
// the ones worth the most, because the failure they guard against is not a
// missing warning but a FALSE ALL-CLEAR: a person told nothing and reading it as
// "I have these files to myself".

import (
	"strings"
	"testing"
	"time"
)

// awayWindow is one other window, fresh at now, holding one running node.
func awayWindow(now time.Time, id, title string, files ...string) SessionPresence {
	return SessionPresence{
		Schema:    presenceSchema,
		SessionID: id,
		UpdatedAt: now,
		RunningTasks: []PresenceTask{{
			ID: "1", Title: title, State: "running", StartedAt: now.Add(-time.Minute), Files: files,
		}},
	}
}

func TestPreflightNamesTheFileAndTheWorkAlreadyInIt(t *testing.T) {
	now := time.Now()
	away := NewElsewhere(now, map[string]string{"other": "aforge-v2"},
		awayWindow(now, "other", "Port the picker", "internal/tui3/home.go"))

	line := PreflightNote("", away, "Rewrite the picker in internal/tui3/home.go and keep the tests green")
	want := "another window is already in internal/tui3/home.go · Port the picker"
	if line != want {
		t.Fatalf("preflight said %q, want %q", line, want)
	}
}

func TestPreflightSaysNothingWhenNoOtherWindowHasWorkOut(t *testing.T) {
	if line := PreflightNote("", Elsewhere{}, "touch internal/tui3/home.go"); line != "" {
		t.Fatalf("a session alone on the machine was warned about something: %q", line)
	}
}

func TestPreflightSaysNothingWhenTheOtherWindowIsInOtherFiles(t *testing.T) {
	now := time.Now()
	away := NewElsewhere(now, nil,
		awayWindow(now, "other", "Port the picker", "internal/tui3/home.go"))

	if line := PreflightNote("", away, "rewrite internal/session/task_index.go"); line != "" {
		t.Fatalf("work in a different file raised a warning: %q", line)
	}
}

// THE ONE THAT MUST NOT SAY "ALONE". The other window's node has written nothing
// yet, so nobody can say whether it is in these files — and the cautious line is
// the only honest answer.
func TestPreflightIsCautiousWhenTheOtherWindowHasNamedNoFiles(t *testing.T) {
	now := time.Now()
	away := NewElsewhere(now, nil, awayWindow(now, "other", "Port the picker"))

	line := PreflightNote("", away, "rewrite internal/session/task_index.go")
	want := "another window has work out in this project · it has not said which files yet"
	if line != want {
		t.Fatalf("preflight said %q, want %q", line, want)
	}
	if strings.Contains(line, "alone") || strings.Contains(line, "only") {
		t.Fatalf("a silence was reported as clearance: %q", line)
	}
}

// A BRIEF THAT SPELLS NO PATHS gets the fact and no guess: somebody else is
// working here, and here is where they have been so far.
func TestPreflightSaysWhereTheOtherWindowIsWhenTheBriefNamesNothing(t *testing.T) {
	now := time.Now()
	away := NewElsewhere(now, nil,
		awayWindow(now, "other", "Port the picker", "internal/tui3/home.go", "internal/tui3/task.go"))

	line := PreflightNote("", away, "make the picker feel calmer")
	want := "another window has work out in this project · internal/tui3/home.go, internal/tui3/task.go"
	if line != want {
		t.Fatalf("preflight said %q, want %q", line, want)
	}
}

func TestPreflightSaysTheBareFactWhenNobodyHasNamedAFile(t *testing.T) {
	now := time.Now()
	away := NewElsewhere(now, nil, awayWindow(now, "other", "Port the picker"))

	line := PreflightNote("", away, "make the picker feel calmer")
	if line != "another window has work out in this project" {
		t.Fatalf("preflight said %q", line)
	}
}

// THE WALLPAPER TEST. Everything touches go.mod, so an overlap that is only
// there is not an overlap worth a line.
func TestPreflightStaysQuietWhenTheOnlySharedFileIsOneEverythingTouches(t *testing.T) {
	now := time.Now()
	away := NewElsewhere(now, nil,
		awayWindow(now, "other", "Bump the deps", "go.mod", "go.sum"))

	if line := PreflightNote("", away, "add a dependency in ./go.mod and wire internal/x/y.go"); line != "" {
		t.Fatalf("a shared go.mod raised a warning: %q", line)
	}
}

func TestPreflightStillWarnsWhenAHotFileTravelsWithARealOne(t *testing.T) {
	now := time.Now()
	away := NewElsewhere(now, nil,
		awayWindow(now, "other", "Bump the deps", "go.mod", "internal/x/y.go"))

	line := PreflightNote("", away, "edit internal/x/y.go and go.mod")
	want := "another window is already in internal/x/y.go · Bump the deps"
	if line != want {
		t.Fatalf("preflight said %q, want %q", line, want)
	}
}

// A STALE WINDOW IS NOT A WINDOW. [NewElsewhere] applies the freshness rule, and
// this is the preflight standing on that rather than repeating it.
func TestPreflightIgnoresAWindowThatWentAway(t *testing.T) {
	now := time.Now()
	stale := awayWindow(now, "other", "Port the picker", "internal/tui3/home.go")
	stale.UpdatedAt = now.Add(-time.Hour)
	away := NewElsewhere(now, nil, stale)

	if line := PreflightNote("", away, "rewrite internal/tui3/home.go"); line != "" {
		t.Fatalf("a dead window's claim was reported as live: %q", line)
	}
}

func TestPreflightBoundsThePathsAndTheNamesItSpells(t *testing.T) {
	now := time.Now()
	files := []string{"a/one.go", "a/two.go", "a/three.go", "a/four.go", "a/five.go"}
	away := NewElsewhere(now, nil, awayWindow(now, "other", "Sweep the call sites", files...))

	line := PreflightNote("", away, strings.Join(files, " "))
	want := "another window is already in a/one.go, a/two.go, a/three.go +2 more · Sweep the call sites"
	if line != want {
		t.Fatalf("preflight said %q, want %q", line, want)
	}
}

func TestPreflightNamesEveryWindowThatIsInTheseFiles(t *testing.T) {
	now := time.Now()
	away := NewElsewhere(now, nil,
		awayWindow(now, "one", "Port the picker", "internal/tui3/home.go"),
		awayWindow(now, "two", "Fix the nil-map crash", "internal/tui3/home.go"),
	)
	line := PreflightNote("", away, "rewrite internal/tui3/home.go")
	if !strings.Contains(line, "Port the picker") || !strings.Contains(line, "Fix the nil-map crash") {
		t.Fatalf("only one of two windows was named: %q", line)
	}
	if strings.Count(line, "internal/tui3/home.go") != 1 {
		t.Fatalf("the shared path was spelled twice: %q", line)
	}
}

// ── reading places out of prose ─────────────────────────────────────────────

func TestBriefFilesReadsThePathsABriefSpells(t *testing.T) {
	brief := "Rewrite `internal/tui3/home.go` and internal/session/task_index.go:112, " +
		"then check docs/coordination-design.md. Do not touch " +
		"https://example.com/internal/x.go or github.com/Agent-Field/aforge-v2/internal/session. " +
		"home.go on its own is not a place, and neither is internal/tui3."
	got := briefFiles("", brief)
	want := []string{
		"internal/tui3/home.go",
		"internal/session/task_index.go",
		"docs/coordination-design.md",
	}
	if len(got) != len(want) {
		t.Fatalf("read %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("read %v, want %v", got, want)
		}
	}
}

func TestBriefFilesPlacesAnAbsolutePathAgainstTheWorkspace(t *testing.T) {
	got := briefFiles("/repo", "edit /repo/internal/tui3/home.go and /elsewhere/other.go")
	if len(got) != 1 || got[0] != "internal/tui3/home.go" {
		t.Fatalf("read %v, want just internal/tui3/home.go", got)
	}
}

func TestBriefFilesDropsAnAbsolutePathWithNoWorkspaceToPlaceItAgainst(t *testing.T) {
	if got := briefFiles("", "edit /repo/internal/tui3/home.go"); len(got) != 0 {
		t.Fatalf("an unplaceable path was read as a claim: %v", got)
	}
}

func TestBriefFilesReadsEachPathOnce(t *testing.T) {
	got := briefFiles("", "internal/x.go", "see internal/x.go again, and ./internal/x.go")
	if len(got) != 1 || got[0] != "internal/x.go" {
		t.Fatalf("read %v, want one internal/x.go", got)
	}
}
