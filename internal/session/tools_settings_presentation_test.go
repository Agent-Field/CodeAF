package session

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatSettingsPresentationDiscoveryAndCompatibility(t *testing.T) {
	agent, _ := settingsAgent(t)
	read, _ := settingsHands(t, agent)
	for _, tc := range []struct{ search, key string }{
		{"completed tool details", config.KeyWork}, {"show hints", config.KeyHints},
		{"Privacy", config.KeyHistoryEnabled}, {"concurrent tasks", config.KeyTaskParallel},
		{"disable hints", config.KeyHints}, {"remember across chats", config.KeyMemoryEnabled},
	} {
		text, bad := callSetting(t, read, map[string]string{"search": tc.search})
		if bad || !strings.Contains(text, tc.key) {
			t.Fatalf("%q did not discover %s: %s", tc.search, tc.key, text)
		}
	}
	text, _ := callSetting(t, read, map[string]string{})
	for _, key := range []string{config.KeyPracticeIdle, config.KeyPracticeBudget, config.KeyBriefAfter, config.KeyTenureAfter, config.KeySplitPct} {
		if strings.Contains(text, key+" ·") {
			t.Fatalf("resident-only %s leaked into chat list", key)
		}
	}
}

func TestChatSettingsReadWriteReadKeepsRawSemantics(t *testing.T) {
	agent, profile := settingsAgent(t)
	read, write := settingsHands(t, agent)
	hints, _ := callSetting(t, read, map[string]string{"key": config.KeyHints})
	for _, want := range []string{`"on" => show hints: off`, `"off" => show hints: on`} {
		if !strings.Contains(hints, want) {
			t.Fatalf("missing inverse hints mapping %s: %s", want, hints)
		}
	}
	for _, tc := range []struct{ key, raw, display string }{
		{config.KeyHints, "on", "off"}, {config.KeyHints, "off", "on"},
		{config.KeyWork, "open", "expanded"}, {config.KeyWork, "fold", "collapsed"},
		{config.KeyMemoryEnabled, "off", "off"}, {config.KeyMemoryEnabled, "on", "on"},
	} {
		receipt, bad := callSetting(t, write, map[string]string{"key": tc.key, "value": tc.raw})
		if bad || !strings.Contains(receipt, "saved") {
			t.Fatalf("write %s failed: %s", tc.key, receipt)
		}
		detail, bad := callSetting(t, read, map[string]string{"key": tc.key})
		if bad || !strings.Contains(detail, "now: "+tc.display) {
			t.Fatalf("readback %s: %s", tc.key, detail)
		}
		row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(tc.key)
		if row.Value() != tc.raw {
			t.Fatalf("registry raw semantics changed: %s = %s", tc.key, row.Value())
		}
		if tc.key == config.KeyHints && config.HintsAt(profile) != (tc.raw == "off") {
			t.Fatal("hints inversion changed")
		}
		if tc.key == config.KeyMemoryEnabled && !strings.Contains(receipt, "Restart the CLI") {
			t.Fatalf("memory falsely implies immediate activation: %s", receipt)
		}
	}
	values := profileJSON(t, profile)
	if values[config.KeyHints] != true || values[config.KeyWork] != "fold" || values[config.KeyMemoryEnabled] != "on" {
		t.Fatalf("wrong persisted values: %v", values)
	}
}

func TestChatSettingsRefusalsPreserveProfile(t *testing.T) {
	agent, profile := settingsAgent(t)
	read, write := settingsHands(t, agent)
	callSetting(t, write, map[string]string{"key": config.KeyWork, "value": "open"})
	before, _ := json.Marshal(profileJSON(t, profile))
	for _, tc := range []struct{ key, value string }{
		{config.KeyWork, "expanded"}, // readable labels do not redefine the old API
		{config.KeyMemoryEnabled, "sometimes"},
		{config.KeyToolApprovalMode, "allow"},
		{config.KeyTaskParallel, "100"},
		{config.KeyPracticeIdle, "on"},
	} {
		text, bad := callSetting(t, write, map[string]string{"key": tc.key, "value": tc.value})
		if !bad {
			t.Fatalf("invalid/guarded %s accepted: %s", tc.key, text)
		}
		after, _ := json.Marshal(profileJSON(t, profile))
		if string(before) != string(after) {
			t.Fatalf("refusal for %s changed profile", tc.key)
		}
	}
	detail, bad := callSetting(t, read, map[string]string{"key": config.KeyPracticeIdle})
	if bad || !strings.Contains(detail, "Unavailable in chat settings") || strings.Contains(detail, "I can change this one") {
		t.Fatalf("hidden exact-key detail misleading: %s", detail)
	}
}

func TestChatSettingsReceiptNamesRestartAndProjectOverride(t *testing.T) {
	agent, profile := settingsAgent(t)
	path := config.ProjectConfigPath(agent.config.Workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{config.KeyHistoryEnabled: true})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, write := settingsHands(t, agent)
	receipt, bad := callSetting(t, write, map[string]string{"key": config.KeyHistoryEnabled, "value": "off"})
	if bad || !strings.Contains(receipt, "Restart the CLI") || !strings.Contains(receipt, "outranks the profile") || !strings.Contains(receipt, path) {
		t.Fatalf("receipt omits actual scope/timing: %s", receipt)
	}
	if config.HistoryEnabledAt(profile) {
		t.Fatal("profile value did not save")
	}
}

func TestChatSettingsHintNoticeUsesVisibleMeaning(t *testing.T) {
	agent, _ := settingsAgent(t)
	agent.hub = newEventHub()
	events := agent.hub.subscribe()
	_, write := settingsHands(t, agent)
	receipt, bad := callSetting(t, write, map[string]string{"key": config.KeyHints, "value": "on"})
	if bad {
		t.Fatal(receipt)
	}
	notice := waitForNotice(t, events)
	if !strings.Contains(notice, "show hints · on → off") || strings.Contains(notice, "raw value") {
		t.Fatalf("user notice exposes raw mapping or wrong direction: %s", notice)
	}
	if !strings.Contains(receipt, "raw value: on") {
		t.Fatalf("model receipt lost compatibility mapping: %s", receipt)
	}
}

func TestChatSettingsTransportChangesNameTheirActivation(t *testing.T) {
	agent, profile := settingsAgent(t)
	read, write := settingsHands(t, agent)
	const timing = "Restart the CLI to ensure already-open chats use this change."
	for _, tc := range []struct{ key, value string }{
		{config.KeyRouting, "price"},
		{config.KeyLaneGuard, "off"},
		{config.LaneSettingKey(config.LaneSlotTalk), "pinned: cloudflare, borrow when slow"},
	} {
		detail, bad := callSetting(t, read, map[string]string{"key": tc.key})
		if bad || !strings.Contains(detail, timing) {
			t.Fatalf("read %s omits tool-specific activation: %s", tc.key, detail)
		}
		for n := 0; n < 2; n++ {
			receipt, bad := callSetting(t, write, map[string]string{"key": tc.key, "value": tc.value})
			if bad || !strings.Contains(receipt, timing) || !strings.Contains(receipt, "saved") {
				t.Fatalf("write %s omits scope/timing: %s", tc.key, receipt)
			}
		}
	}
	values := profileJSON(t, profile)
	if values[config.KeyRouting] != "price" || values[config.KeyLaneGuard] != false || values[config.LaneBorrowKey(config.LaneSlotTalk)] != true || values[config.LaneSettingKey(config.LaneSlotTalk)] != "cloudflare" {
		t.Fatalf("transport values not persisted: %v", values)
	}
	// Borrow is a persisted companion field, not a separately exposed registry row.
	// It stays unavailable by exact key; the composite lane choice above owns it.
	borrow := config.LaneBorrowKey(config.LaneSlotTalk)
	if detail, bad := callSetting(t, read, map[string]string{"key": borrow}); !bad {
		t.Fatalf("invented borrow row became readable: %s", detail)
	}
	if receipt, bad := callSetting(t, write, map[string]string{"key": borrow, "value": "off"}); !bad {
		t.Fatalf("invented borrow row became writable: %s", receipt)
	}
	if !config.LaneBorrowAt(profile, config.LaneSlotTalk) {
		t.Fatal("refused companion write altered borrow")
	}
	if toolSettingActivation(config.Setting{Key: borrow}) != timing {
		t.Fatal("borrow timing differs if exposed later")
	}
}
