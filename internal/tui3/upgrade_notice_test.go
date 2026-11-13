package tui3

import (
	"context"
	"strings"
	"testing"
)

func TestUpgradeLaunchNoticeVisibleOnHomeAndInConversation(t *testing.T) {
	for _, line := range []string{
		"Found 1 rule from an earlier version of codeaf. They were not carried over. Recreate rules with /remember. Your list is in ~/test-home/v3/standing-upgrade-report.txt.",
		"2 turns typed in an older codeaf are kept in ~/test-home/original/transcript.jsonl and are not shown here.",
		"1 turn typed in an older codeaf is kept in ~/test-home/original/transcript.jsonl and is not shown here.",
	} {
		t.Run(strings.SplitN(line, ".", 2)[0], func(t *testing.T) {
			a := newApp(context.Background(), Options{Agent: &fakeAgent{model: "m"}, Notice: line, ProfileDir: t.TempDir(), UsageLedger: labLedger(), WorldRoot: t.TempDir(), Env: envOf(map[string]string{"TERM": "xterm-256color"})})
			a.width, a.height = 220, 45
			a.route(noticesMsg{})
			count := 0
			for _, entry := range a.entries {
				if entry.kind == entryNote && entry.text == line {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("conversation showed recovery notice %d times", count)
			}
			drain(t, a, a.openHome())
			if !strings.Contains(homeText(a), strings.SplitN(line, ".", 2)[0]) {
				t.Fatalf("Home hid upgrade notice:\n%s", homeText(a))
			}
			a.route(noticesMsg{})
			count = 0
			for _, entry := range a.entries {
				if entry.kind == entryNote && entry.text == line {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("notice repeated %d times", count)
			}
		})
	}
}
