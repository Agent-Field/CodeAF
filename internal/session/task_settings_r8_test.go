package session

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// R7-1 and S10 keep unavailable placement out of both settings hands.
func TestTheSettingsToolSaysNothingAboutRunTasksOnWithoutTaskComputers(t *testing.T) {
	a, profile := settingsAgent(t)
	read, write := settingsHands(t, a)
	text, bad := callSetting(t, read, map[string]string{})
	if bad || strings.Contains(text, "Run tasks on") || strings.Contains(text, config.KeyTaskRunsOn) {
		t.Fatalf("unsupported listing: %s", text)
	}
	for _, tool := range []string{"read", "write"} {
		hand := read
		if tool == "write" {
			hand = write
		}
		args := map[string]string{"key": config.KeyTaskRunsOn}
		if tool == "write" {
			args["value"] = "dev_0123456789abcdef0123456789abcdef"
		}
		text, bad = callSetting(t, hand, args)
		if !bad {
			t.Fatalf("unsupported %s accepted: %s", tool, text)
		}
	}
	if config.TaskRunsOnAt(profile) != "" {
		t.Fatal("unsupported write changed the preference")
	}
}

// R7-1 retains the paired conversation's opt-in setting.
func TestTheSettingsToolListsRunTasksOnWhenTheConversationHasTaskComputers(t *testing.T) {
	a, _ := settingsAgent(t)
	a.config.TaskComputers = &testTaskComputers{}
	read, _ := settingsHands(t, a)
	text, bad := callSetting(t, read, map[string]string{})
	if bad || !strings.Contains(text, "Run tasks on") {
		t.Fatalf("paired listing: %s", text)
	}
}
