package automation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func commandNow(t *testing.T) (time.Time, *time.Location) {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skip("no zone database")
	}
	return time.Date(2026, 10, 10, 14, 30, 0, 0, loc), loc
}

func commandBase() Automation {
	return Automation{Workspace: "/work/project", Schedule: Schedule{Zone: "America/Toronto"}}
}

// THE THREE KINDS, TYPED. Each form a person would type reads into the
// automation it names, with the schedule read in the window's zone.
func TestTypedLinesReadIntoTheThreeKinds(t *testing.T) {
	now, loc := commandNow(t)
	cases := []struct {
		line  string
		kind  Kind
		check func(t *testing.T, a Automation)
	}{
		{`add leave at 18:00 say "time to leave"`, KindReminder, func(t *testing.T, a Automation) {
			if want := time.Date(2026, 10, 10, 18, 0, 0, 0, loc); !a.Schedule.At.Equal(want) || a.Action.Say != "time to leave" {
				t.Fatalf("reminder = %+v", a)
			}
		}},
		{`add "weekly update" every "0 9 * * 1" do "draft the weekly update" worktree time 45m usd 2.5`, KindWork, func(t *testing.T, a Automation) {
			if a.Title != "weekly update" || a.Schedule.Every != "0 9 * * 1" || !a.Worktree || a.Limits.Time != 45*time.Minute || a.Limits.USD != 2.5 {
				t.Fatalf("work = %+v", a)
			}
		}},
		{`add "ci on main" every 15m look "gh run list -b main -L 1" until "the latest run failed" once say "CI on main is red"`, KindWatch, func(t *testing.T, a Automation) {
			if a.Look == nil || a.Look.Command != "gh run list -b main -L 1" || a.Look.Condition != "the latest run failed" || !a.Look.Once {
				t.Fatalf("watch = %+v", a.Look)
			}
			if !a.Schedule.Anchor.Equal(now.Truncate(time.Second)) {
				t.Fatalf("an interval's grid starts when it was typed, got %v", a.Schedule.Anchor)
			}
		}},
		{`add stretch in 20m say stretch`, KindReminder, func(t *testing.T, a Automation) {
			if !a.Schedule.At.Equal(now.Add(20 * time.Minute)) {
				t.Fatalf("in 20m = %v", a.Schedule.At)
			}
		}},
		{`add "standup" at "tomorrow 09:15" say "standup in 15"`, KindReminder, func(t *testing.T, a Automation) {
			if want := time.Date(2026, 10, 11, 9, 15, 0, 0, loc); !a.Schedule.At.Equal(want) {
				t.Fatalf("tomorrow = %v", a.Schedule.At)
			}
		}},
	}
	for _, c := range cases {
		cmd, err := ParseCommand(c.line)
		if err != nil {
			t.Fatalf("%s: %v", c.line, err)
		}
		a, err := cmd.Apply(commandBase(), now)
		if err != nil {
			t.Fatalf("%s: %v", c.line, err)
		}
		if a.Kind() != c.kind {
			t.Fatalf("%s: kind %s, want %s", c.line, a.Kind(), c.kind)
		}
		c.check(t, a)
	}
}

// EVERY MISTAKE IS SAID, NOT GUESSED AT. A typed line is exact, so anything it
// cannot read is a sentence naming what it could not read.
func TestATypedLineThatCannotBeReadSaysWhy(t *testing.T) {
	now, _ := commandNow(t)
	cases := map[string]string{
		`add`:                                    "needs a title",
		`add leave at 18:00 say`:                 "needs a value",
		`add leave evry 15m say hi`:              `unknown word "evry"`,
		`add leave at 12:00 say "lunch"`:         "has already passed",
		`add leave at 25:99 say x`:               "cannot read the moment",
		`add "unterminated`:                      "never closed",
		`add x every 15m say a do b`:             "",
		`pause`:                                  "needs the automation's id",
		`pause abc extra`:                        "takes only the id",
		`edit abc`:                               "at least one thing",
		`frobnicate`:                             "the forms are",
		`add x every 30s say hi`:                 "under a minute",
		`add x every 15m until "it broke" say y`: "exactly one of a command, files or a tool",
		`add x at 18:00 say y zone Mars/Base`:    "unknown zone",
	}
	for line, want := range cases {
		cmd, err := ParseCommand(line)
		if err == nil {
			_, err = cmd.Apply(commandBase(), now)
		}
		if want == "" {
			// `say a do b` keeps the last action, which is the edit semantics:
			// a later clause replaces an earlier one of the same kind.
			if err != nil {
				t.Fatalf("%s: %v", line, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error %v, want it to say %q", line, err, want)
		}
	}
}

// THE LINE THE LIST PUTS IN THE BOX IS THE LINE THAT MAKES IT. Writing an
// automation out and reading the line back onto nothing but its id gives the
// same automation.
func TestCommandLineRoundTrips(t *testing.T) {
	now, loc := commandNow(t)
	automations := []Automation{
		{ID: "a1", Title: "leave", Workspace: "/work/project", Schedule: Schedule{At: time.Date(2026, 10, 11, 18, 0, 0, 0, loc), Zone: "America/Toronto"}, Action: Action{Say: `time to "leave"`}},
		{ID: "a2", Title: "weekly update", Workspace: "/work/project", Schedule: Schedule{Every: "0 9 * * 1", Zone: "America/Toronto"}, Action: Action{Do: "draft it"}, Worktree: true, Limits: Limits{Time: 2 * time.Hour, USD: 3}},
		{ID: "a3", Title: "ci", Workspace: "/work/project", Schedule: Schedule{Every: "15m", Zone: "America/Toronto", Anchor: now}, Look: &Look{Files: "logs/**/*.log", Condition: "an error appears", Once: true}, Action: Action{Say: "look at the logs"}},
	}
	for _, original := range automations {
		line := CommandLine(original)
		cmd, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if cmd.Verb != VerbEdit || cmd.ID != original.ID {
			t.Fatalf("%s read as %s %s", line, cmd.Verb, cmd.ID)
		}
		back, err := cmd.Apply(Automation{ID: original.ID}, now)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		// An interval's anchor is when the line was typed; nothing else moves.
		back.Schedule.Anchor, original.Schedule.Anchor = time.Time{}, time.Time{}
		if CommandLine(back) != CommandLine(original) || back.Title != original.Title || back.Kind() != original.Kind() {
			t.Fatalf("round trip changed it:\n %s\n %s", CommandLine(original), CommandLine(back))
		}
		if !back.Schedule.At.Equal(original.Schedule.At) {
			t.Fatalf("the moment moved: %v → %v", original.Schedule.At, back.Schedule.At)
		}
	}
}

// AN EDIT CHANGES WHAT IT NAMES AND NOTHING ELSE.
func TestAnEditKeepsWhatTheLineDoesNotMention(t *testing.T) {
	now, _ := commandNow(t)
	base := Automation{ID: "w", Title: "ci", Workspace: "/work/project", Schedule: Schedule{Every: "15m", Zone: "America/Toronto"},
		Look: &Look{Command: "make test", Condition: "it fails"}, Action: Action{Say: "red"}, Limits: Limits{USD: 1}}
	cmd, err := ParseCommand(`edit w every 30m`)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := cmd.Apply(base, now)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Schedule.Every != "30m" || edited.Look.Command != "make test" || edited.Look.Condition != "it fails" || edited.Action.Say != "red" || edited.Limits.USD != 1 {
		t.Fatalf("edit touched more than the rhythm: %+v %+v", edited, edited.Look)
	}
	for _, verb := range []Verb{VerbRun, VerbPause, VerbResume, VerbDelete} {
		cmd, err := ParseCommand(string(verb) + " w")
		if err != nil || cmd.Verb != verb || cmd.ID != "w" {
			t.Fatalf("%s w = %+v, %v", verb, cmd, err)
		}
	}
	if cmd, err := ParseCommand("  "); err != nil || cmd.Verb != VerbList {
		t.Fatalf("a bare command is the list, got %+v, %v", cmd, err)
	}
}

// A MOMENT IS TYPED THE WAY A PERSON SAYS IT, with or without quotes: `6pm`,
// `tomorrow 9am`, `monday 9am` and `today 18:00` all read, and a day of the
// week is the next one to come. The clock is Saturday 10 October, 14:30.
func TestAMomentReadsTheWayItIsSaid(t *testing.T) {
	now, loc := commandNow(t)
	cases := map[string]time.Time{
		`add leave at 6pm say x`:                       time.Date(2026, 10, 10, 18, 0, 0, 0, loc),
		`add leave at 6:30pm say x`:                    time.Date(2026, 10, 10, 18, 30, 0, 0, loc),
		`add leave at 18 say x`:                        time.Date(2026, 10, 10, 18, 0, 0, 0, loc),
		`add leave at tomorrow 9am say x`:              time.Date(2026, 10, 11, 9, 0, 0, 0, loc),
		`add leave at "tomorrow 18:00" say x`:          time.Date(2026, 10, 11, 18, 0, 0, 0, loc),
		`add leave at today 11:59pm say x`:             time.Date(2026, 10, 10, 23, 59, 0, 0, loc),
		`add leave at monday 9am say x`:                time.Date(2026, 10, 12, 9, 0, 0, 0, loc),
		`add leave at sat 9am say x`:                   time.Date(2026, 10, 17, 9, 0, 0, 0, loc),
		`add leave at sat 4pm say x`:                   time.Date(2026, 10, 10, 16, 0, 0, 0, loc),
		`add leave at 12am say x zone America/Toronto`: time.Date(2026, 10, 10, 0, 0, 0, 0, loc),
		`add leave at 2026-10-15 9:00 say x`:           time.Date(2026, 10, 15, 9, 0, 0, 0, loc),
		`add leave at 2026-10-15T09:00 say x`:          time.Date(2026, 10, 15, 9, 0, 0, 0, loc),
		`add leave at someday say x`:                   {},
		`add leave at 13pm say x`:                      {},
	}
	for line, want := range cases {
		cmd, err := ParseCommand(line)
		if err == nil {
			var a Automation
			a, err = cmd.Apply(commandBase(), now)
			if err == nil {
				if want.IsZero() {
					t.Errorf("%s: read as %v, want it refused", line, a.Schedule.At)
					continue
				}
				if !a.Schedule.At.Equal(want) {
					t.Errorf("%s: read as %v, want %v", line, a.Schedule.At, want)
				}
				continue
			}
		}
		if !want.IsZero() && !strings.Contains(err.Error(), "already passed") {
			t.Errorf("%s: refused with %v", line, err)
		}
	}
}

// A FOLDER UNDER ~ IS SENT AS ~, and the store reads it as its own machine's
// home: a line typed over --host names the far machine's folder, not the
// laptop's.
func TestAFolderUnderHomeIsKeptForTheStoreToRead(t *testing.T) {
	now, _ := commandNow(t)
	for line, want := range map[string]string{
		`add x at 18:00 say y folder ~/app`:    "~/app",
		`add x at 18:00 say y folder ~`:        "~",
		`add x at 18:00 say y folder ~/a/../b`: "~/b",
	} {
		cmd, err := ParseCommand(line)
		if err != nil {
			t.Fatal(err)
		}
		a, err := cmd.Apply(commandBase(), now.Add(-time.Hour))
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if a.Workspace != want {
			t.Fatalf("%s: folder %q, want %q", line, a.Workspace, want)
		}
	}
	if cmd, err := ParseCommand(`add x at 18:00 say y folder ~bob/app`); err == nil {
		if _, err := cmd.Apply(commandBase(), now.Add(-time.Hour)); err == nil {
			t.Fatal("another person's ~ was taken")
		}
	}
	s, _ := openStore(t)
	item := reminder(time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC))
	item.Workspace = "~/app"
	made, err := s.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	house, _ := os.UserHomeDir()
	if made.Workspace != filepath.Join(house, "app") {
		t.Fatalf("the store kept %q, want this machine's %q", made.Workspace, filepath.Join(house, "app"))
	}
}

// A MOMENT KEEPS ITS SECONDS ON THE WAY ROUND. `in 2m` and the conversation's
// `when.in` make moments with seconds in them; an edit line written to the
// minute moved the reminder by them, so the box put a different moment in front
// of the person than the one it would run at.
func TestCommandLineKeepsTheSecondsOfAMoment(t *testing.T) {
	now, loc := commandNow(t)
	for _, at := range []time.Time{
		time.Date(2026, 10, 10, 14, 32, 27, 0, loc),
		time.Date(2026, 10, 10, 14, 32, 27, 481_000_000, loc),
		time.Date(2026, 10, 10, 14, 33, 0, 0, loc),
	} {
		original := Automation{ID: "r", Title: "stretch", Workspace: "/work/project", Schedule: Schedule{At: at, Zone: "America/Toronto"}, Action: Action{Say: "stand up"}}
		line := CommandLine(original)
		cmd, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		back, err := cmd.Apply(original, now)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if !back.Schedule.At.Equal(at) || ScheduleChanged(original.Schedule, back.Schedule) {
			t.Fatalf("%s moved the moment %v to %v", line, at, back.Schedule.At)
		}
	}
	if line := CommandLine(Automation{ID: "r", Schedule: Schedule{At: time.Date(2026, 10, 10, 14, 33, 0, 0, loc), Zone: "America/Toronto"}}); !strings.HasSuffix(line, " at 2026-10-10T14:33") {
		t.Fatalf("a whole minute is written %q, want it to the minute", line)
	}
}

// A FINISHED REMINDER'S OWN LINE IS NOT A MOMENT IN THE PAST TYPED BY MISTAKE.
// Its moment is its history, so a line that leaves it where it was — a new
// title, a new line to say — reads, and only a moment moved into the past is
// refused.
func TestAnEditThatKeepsAPassedMomentReads(t *testing.T) {
	now, loc := commandNow(t)
	finished := Automation{ID: "r", Title: "stretch", Workspace: "/work/project", Status: StatusFinished,
		Schedule: Schedule{At: now.Add(-3*time.Minute - 27*time.Second), Zone: "America/Toronto"}, Action: Action{Say: "stand up"}}
	cmd, err := ParseCommand(CommandLine(finished) + ` title "stretch again"`)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := cmd.Apply(finished, now)
	if err != nil {
		t.Fatalf("retitling a finished reminder: %v", err)
	}
	if edited.Title != "stretch again" || !edited.Schedule.At.Equal(finished.Schedule.At) {
		t.Fatalf("the edit read as %+v", edited)
	}
	earlier := finished.Schedule.At.In(loc).Add(-time.Hour).Format("2006-01-02T15:04")
	cmd, err = ParseCommand("edit r at " + earlier)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cmd.Apply(finished, now); err == nil || !strings.Contains(err.Error(), "already passed") {
		t.Fatalf("a moment moved into the past = %v, want it refused", err)
	}
}

// A MOMENT IN THE HOUR A FALL-BACK REPEATS KEEPS WHICH ONE IT IS. Toronto's
// clocks go from 01:59 EDT back to 01:00 EST on Sunday 1 November 2026, so
// `01:30` names two instants; the line carries the offset for the second.
func TestAMomentInARepeatedHourRoundTrips(t *testing.T) {
	_, loc := commandNow(t)
	second := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC).In(loc) // 01:30 EST
	first := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC).In(loc)  // 01:30 EDT
	for _, c := range []struct {
		at, now time.Time
		status  Status
		more    string
	}{
		// Saved unedited while it is still to come.
		{second, second.Add(-3 * time.Hour), StatusActive, ""},
		{first, first.Add(-3 * time.Hour), StatusActive, ""},
		// Renamed after it was said.
		{second, second.Add(time.Hour), StatusFinished, ` title "stretch again"`},
	} {
		original := Automation{ID: "r", Title: "stretch", Workspace: "/work/project", Status: c.status,
			Schedule: Schedule{At: c.at, Zone: "America/Toronto"}, Action: Action{Say: "stand up"}}
		line := CommandLine(original) + c.more
		cmd, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		back, err := cmd.Apply(original, c.now)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if !back.Schedule.At.Equal(c.at) {
			t.Fatalf("%s moved the moment %v to %v", line, c.at, back.Schedule.At)
		}
	}
	if line := CommandLine(Automation{ID: "r", Schedule: Schedule{At: first, Zone: "America/Toronto"}}); !strings.HasSuffix(line, " at 2026-11-01T01:30") {
		t.Fatalf("the first 01:30 is written %q, want the plain wall clock", line)
	}
}

// A MOMENT THAT PASSED IN THIS VERY MINUTE IS SAID TO THE SECOND, so the
// refusal does not read as 20:33 passing at 20:33.
func TestAMomentPassedThisMinuteIsSaidToTheSecond(t *testing.T) {
	now, loc := commandNow(t)
	now = now.Add(5 * time.Second)
	base := Automation{ID: "r", Title: "stretch", Workspace: "/work/project", Schedule: Schedule{At: now.Add(time.Hour), Zone: "America/Toronto"}, Action: Action{Say: "stand up"}}
	cmd, err := ParseCommand("edit r at " + now.In(loc).Format("15:04"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cmd.Apply(base, now); err == nil || !strings.Contains(err.Error(), "14:30:00 has already passed") {
		t.Fatalf("a moment passed this minute = %v", err)
	}
	cmd, _ = ParseCommand("edit r at 14:29")
	if _, err := cmd.Apply(base, now); err == nil || !strings.HasSuffix(err.Error(), "14:29 has already passed") {
		t.Fatalf("a moment passed before this minute = %v", err)
	}
}
