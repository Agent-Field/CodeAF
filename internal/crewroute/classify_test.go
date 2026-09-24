package crewroute

import "testing"

func TestClassifyReadsTheKindOfWork(t *testing.T) {
	cases := []struct {
		name  string
		task  Task
		class Class
		sure  bool
	}{
		{"a fix: title", Task{Text: "fix: avoid OverflowError when formatting very large integer parameters"}, Bugfix, true},
		{"a bug tag", Task{Text: "[bug] merge_list call drops vlan interfaces"}, Bugfix, true},
		{"a stack trace", Task{Text: "config loader\n\nTraceback (most recent call last):\n  File \"x.py\", line 3\nKeyError: 'a'"}, Bugfix, true},
		{"a crash in the title", Task{Text: "git_diff.py crashes with fatal exit code 128 on repositories without an initial commit"}, Bugfix, true},
		{"a bug label", Task{Text: "the chart looks odd", Labels: []string{"bug"}}, Bugfix, true},
		{"a feature request", Task{Text: "Consider adding strictly proper scoring rule metrics\n\n**Is your feature request related to a problem?**"}, OpenEnded, true},
		{"a refactor title", Task{Text: "refactor(gateway): enrich model type registry metadata for UI consumption"}, OpenEnded, true},
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
	wrapped := "Implement issue #412: [bug] merge_list call drops vlan interfaces\n\nThe merge uses the wrong key.\n\n" +
		"Work in this repository. Implement the change and make the existing test suite pass. Do not weaken or delete tests to make them pass."
	if got := Classify(Task{Text: wrapped}); got.Class != Bugfix {
		t.Errorf("wrapped fix read as %s (%q)", got.Class, got.Why)
	}
	feature := "Implement issue #617: Save sizing of part search window\n\nIt would be really useful to save these for the next search.\n\n" +
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
