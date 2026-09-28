package crewroute

import "testing"

func TestClassifyReadsTheKindOfWork(t *testing.T) {
	cases := []struct {
		name  string
		task  Task
		class Class
		sure  bool
	}{
		{"a fix: title", Task{Text: "fix: avoid an overflow when formatting very large integers"}, Bugfix, true},
		{"a bug tag", Task{Text: "[bug] export drops the last row of a table"}, Bugfix, true},
		{"a stack trace", Task{Text: "config loader\n\nTraceback (most recent call last):\n  File \"x.py\", line 3\nKeyError: 'a'"}, Bugfix, true},
		{"a crash in the title", Task{Text: "the diff tool crashes on repositories without an initial commit"}, Bugfix, true},
		{"a bug label", Task{Text: "the chart looks odd", Labels: []string{"bug"}}, Bugfix, true},
		{"a feature request", Task{Text: "Consider adding a percentile metric\n\n**Is your feature request related to a problem?**"}, OpenEnded, true},
		{"a refactor title", Task{Text: "refactor(api): split the handler registry into modules"}, OpenEnded, true},
		{"add something", Task{Text: "Add a --json flag to the status command"}, OpenEnded, true},
		{"docs", Task{Text: "docs: explain the retry settings"}, OpenEnded, true},
		{"an enhancement label", Task{Text: "the chart looks odd", Labels: []string{"enhancement"}}, OpenEnded, true},
		{"a question", Task{Text: "Why does the scheduler skip the first job?"}, Other, true},
		{"nothing to go on", Task{Text: "the widget"}, OpenEnded, false},
		{"empty", Task{}, OpenEnded, false},
	}
	for _, tc := range cases {
		got := Classify(tc.task)
		if got.Class != tc.class || got.Sure != tc.sure {
			t.Errorf("%s: %s (sure %v, %q), want %s (sure %v)", tc.name, got.Class, got.Sure, got.Why, tc.class, tc.sure)
		}
		if got.Why == "" {
			t.Errorf("%s: a reading with no reason", tc.name)
		}
	}
}

// A HARNESS'S WRAPPER IS READ PAST: every task it hands out says "Implement
// issue #N" and "make the existing test suite pass", and a signal that fires on
// everything tells the router nothing.
func TestClassifyReadsPastAHarnessWrapper(t *testing.T) {
	wrapped := "Implement issue #12: [bug] export drops the last row of a table\n\nThe loop stops one short.\n\n" +
		"Work in this repository. Implement the change and make the existing test suite pass. Do not weaken or delete tests to make them pass."
	if got := Classify(Task{Text: wrapped}); got.Class != Bugfix {
		t.Errorf("wrapped fix read as %s (%q)", got.Class, got.Why)
	}
	feature := "Implement issue #34: Remember the window size between sessions\n\nIt would be really useful to keep it for next time.\n\n" +
		"Work in this repository. Implement the change and make the existing test suite pass."
	if got := Classify(Task{Text: feature}); got.Class != OpenEnded {
		t.Errorf("wrapped feature read as %s (%q)", got.Class, got.Why)
	}
}

func TestParseEffortTakesAPersonsWords(t *testing.T) {
	for word, want := range map[string]Effort{"": EffortKnee, "best": EffortBest, "thorough": EffortBest, "cheap": EffortCheap} {
		if got, ok := ParseEffort(word); !ok || got != want {
			t.Errorf("ParseEffort(%q) = %q, %v", word, got, ok)
		}
	}
	if _, ok := ParseEffort("medium"); ok {
		t.Error("an unknown effort word was accepted")
	}
}

func TestLineageFoldsSnapshotsAndLevels(t *testing.T) {
	cases := map[string]string{
		"deepseek/deepseek-v4-flash-0731":    "deepseek/deepseek-v4-flash",
		"~deepseek/deepseek-v4-flash-latest": "deepseek/deepseek-v4-flash",
		"moonshotai/kimi-k3:high":            "moonshotai/kimi-k3",
		"Z-AI/GLM-5.3-Flash":                 "z-ai/glm-5.3-flash",
		"openai/gpt-5.5":                     "openai/gpt-5.5",
		"qwen/qwen3.8-max-0902":              "qwen/qwen3.8-max",
	}
	for in, want := range cases {
		if got := Lineage(in); got != want {
			t.Errorf("Lineage(%q) = %q, want %q", in, got, want)
		}
	}
}

// A DEFECT SAID IN PLAIN WORDS IS A FIX, a tiny edit is small whatever verb it
// opens with, and a genuinely open ask stays open-ended.
func TestClassifyPlainDefectsAndTinyEdits(t *testing.T) {
	cases := []struct {
		text  string
		class Class
	}{
		{"Fix the bug in calc.py: add(a, b) returns a - b instead of a + b.", Bugfix},
		{"Repair the export so it keeps the header row", Bugfix},
		{"The pager is broken on narrow terminals", Bugfix},
		{"sorting should keep equal items in order but it reorders them", Bugfix},
		{"parse_date: expected 2024-01-02, got 2024-02-01", Bugfix},
		{"tests/test_lists.py is failing: natural_list drops middle items", Bugfix},
		{"add a one-line docstring to the helper in utils.py", Other},
		{"fix a typo in the README", Other},
		{"Add support for YAML config files", OpenEnded},
		{"refactor the storage layer into separate packages", OpenEnded},
		{"Design a plugin system for exporters", OpenEnded},
		{"do a deep research on opensource coding agents and harnesses please", OpenEnded},
	}
	for _, tc := range cases {
		if got := Classify(Task{Text: tc.text}); got.Class != tc.class {
			t.Errorf("%q: %s (%q), want %s", tc.text, got.Class, got.Why, tc.class)
		}
	}
}
