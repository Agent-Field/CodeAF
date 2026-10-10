package automation

import (
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
