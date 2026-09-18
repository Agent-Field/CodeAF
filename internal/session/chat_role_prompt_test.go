package session

import (
	"strings"
	"testing"
	"time"
)

// The three-verbs page belongs to the conversation whose task door launches
// bash-belt runs, not to a run worker. The flag-off arm pins the conversation
// page that ships today so enabling this account cannot rewrite every chat.
func TestChatRoleRulesFollowTheTaskBelt(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
	config := Config{Workspace: t.TempDir(), Model: "test/model"}

	t.Setenv("CODEAF_TASK_BELT", "")
	plain := renderSystemAt(config, now)
	for _, existing := range []string{"## Specialized Tools", "THERE IS NO PLANNER ON YOUR BELT"} {
		if !strings.Contains(plain, existing) {
			t.Errorf("the conversation without the task belt lost today's page text %q", existing)
		}
	}
	for _, added := range []string{"Hand off", "Add to", "Ask about"} {
		if strings.Contains(plain, added) {
			t.Errorf("the conversation without the task belt gained the %q rule", added)
		}
	}

	t.Setenv("CODEAF_TASK_BELT", "bash")
	belt := renderSystemAt(config, now)
	for _, rule := range []string{"Hand off", "Add to", "Ask about"} {
		if !strings.Contains(belt, rule) {
			t.Errorf("the conversation under the task belt has no %q rule", rule)
		}
	}
	addTo := sectionContaining(belt, "Add to")
	if !strings.Contains(addTo, "live root") {
		t.Errorf("the add-to rule does not name the live root: %q", addTo)
	}
}

func sectionContaining(page, phrase string) string {
	for _, line := range strings.Split(page, "\n") {
		if strings.Contains(line, phrase) {
			return line
		}
	}
	return ""
}
