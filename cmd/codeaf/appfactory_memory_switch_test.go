package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestMemorySwitchDefaultsOffAndChangesLive(t *testing.T) {
	s := newLocalSetup(t)
	profile := config.ProfileDir()
	vault := knowledge.Open(profile)
	if config.MemoryEnabledAt(profile) || vault.Enabled() {
		t.Error("fresh home enables automatic memory without consent")
	}
	read := func(method string, params any, enabled bool, source string) {
		t.Helper()
		var reply struct {
			Settings struct {
				Memory *struct {
					Enabled  bool   `json:"enabled"`
					Source   string `json:"source"`
					CostNote string `json:"cost_note"`
				} `json:"memory"`
			} `json:"settings"`
		}
		s.must(method, params, &reply)
		m := reply.Settings.Memory
		if m == nil || m.Enabled != enabled || m.Source != source || !strings.Contains(m.CostNote, "39% of the job's cost: $0.0124 of $0.0315.") {
			t.Fatalf("%s memory = %+v, want enabled=%v source=%s and cost disclosure", method, m, enabled, source)
		}
	}
	read("settings.get", struct{}{}, false, "default")
	if !strings.Contains(s.hello, "settings.memory") {
		t.Error("hello omits settings.memory")
	}
	read("settings.set", map[string]any{"memory_enabled": true}, true, "saved")
	if !config.MemoryEnabledAt(profile) || !vault.Enabled() {
		t.Fatal("on did not reach the existing vault")
	}
	read("settings.set", map[string]any{"memory_enabled": false}, false, "saved")
	if config.MemoryEnabledAt(profile) || vault.Enabled() {
		t.Fatal("off did not reach the existing vault")
	}
	for _, bad := range []any{"on", 1, nil} {
		s.mustFail("settings.set", map[string]any{"memory_enabled": bad}, appwire.CodeBadParams)
	}
	if err := vault.Flush(context.Background(), "fixture", false); err != knowledge.ErrDisabled {
		t.Fatalf("off compile = %v, want disabled", err)
	}
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	read("settings.get", struct{}{}, true, "saved")
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, line := range s.lines {
		var event struct {
			Event string          `json:"event"`
			Data  json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal([]byte(line), &event)
		if strings.Contains(line, "settings.changed") && strings.Contains(line, `"memory"`) {
			found = true
		}
	}
	if !found {
		t.Error("settings.changed omits memory")
	}
}
