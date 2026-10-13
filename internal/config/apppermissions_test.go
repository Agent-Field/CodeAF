package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func appBash(command string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"command": command})
	return raw
}
func TestAppPermissionExactScopeRemovalAndWhy(t *testing.T) {
	profile := t.TempDir()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	r, err := SaveAppPermissionRule(profile, "one", "bash", appBash("printf '*.go'"), "exact", "agent", "ask_1", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		agent, command string
		hit            bool
	}{{"one", "printf '*.go'", true}, {"two", "printf '*.go'", false}, {"one", "printf 'other.go'", false}} {
		matched, err := MatchAppPermissionRule(profile, probe.agent, "bash", appBash(probe.command), false, "call")
		if err != nil {
			t.Fatal(err)
		}
		if (matched != nil) != probe.hit {
			t.Fatalf("%+v matched %+v", probe, matched)
		}
	}
	_, err = ChangeAppPermissionRule(profile, r.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	matched, err := MatchAppPermissionRule(profile, "one", "bash", appBash("printf '*.go'"), false, "call")
	if err != nil || matched != nil {
		t.Fatal("removed rule still matches")
	}
	_, err = ChangeAppPermissionRule(profile, r.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	matched, err = MatchAppPermissionRule(profile, "one", "bash", appBash("printf '*.go'"), false, "call_2")
	if err != nil || matched == nil || matched.ID != r.ID || matched.MatchedID != "call_2" {
		t.Fatal("restore lost rule identity or Why")
	}
	info, err := os.Stat(filepath.Join(profile, "app-permissions.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("rule document is not private")
	}
}
func TestAppPermissionClassFloorsAndAlwaysAsk(t *testing.T) {
	profile := t.TempDir()
	r, err := SaveAppPermissionRule(profile, "one", "bash", appBash("git status --short"), "class", "all_agents", "ask", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r.Pattern != "git status*" {
		t.Fatalf("class %q", r.Pattern)
	}
	for _, probe := range []struct {
		command string
		hit     bool
	}{{"git status --porcelain", true}, {"git diff", false}, {"git status && rm -rf /", false}, {"git status; printf hello", false}} {
		match, err := MatchAppPermissionRule(profile, "two", "bash", appBash(probe.command), false, "call")
		if err != nil {
			t.Fatal(err)
		}
		if (match != nil) != probe.hit {
			t.Fatalf("%q: %v", probe.command, match)
		}
	}
	for _, call := range []struct {
		tool string
		args json.RawMessage
	}{{"bash", appBash("rm -rf /")}, {"gmail_send", json.RawMessage(`{"to":"x"}`)}, {"stripe_request", json.RawMessage(`{"method":"POST","path":"/charges"}`)}} {
		if _, err := SaveAppPermissionRule(profile, "one", call.tool, call.args, "exact", "all_agents", "ask", time.Now()); err == nil {
			t.Fatalf("irreversible rule saved for %s", call.tool)
		}
	}
	floor, err := AppPermissionAlwaysAsk(profile, "bash", appBash("rm -rf .turbo"), false)
	if err != nil || !floor {
		t.Fatal("deleting outside a task did not ask")
	}
	floor, err = AppPermissionAlwaysAsk(profile, "bash", appBash("rm -rf .turbo"), true)
	if err != nil || floor {
		t.Fatal("task deletion incorrectly treated as outside a task")
	}
	patch := map[string]json.RawMessage{"permissions.always_ask": json.RawMessage(`["acting in your name","deleting outside a task","payments","git status*"]`), "permissions.question_clock_min": json.RawMessage(`20`)}
	if err := ApplyAppSettings(profile, patch, ""); err != nil {
		t.Fatal(err)
	}
	match, err := MatchAppPermissionRule(profile, "one", "bash", appBash("git status --short"), false, "call")
	if err != nil || match != nil {
		t.Fatal("saved rule lifted the extra always-ask row")
	}
	clock, err := AppQuestionClock(profile)
	if err != nil || clock != 20*time.Minute {
		t.Fatalf("saved clock %s %v", clock, err)
	}
}
func TestAppPermissionConcurrentSavesAndMalformedStore(t *testing.T) {
	profile := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := SaveAppPermissionRule(profile, "one", "bash", appBash("git status"), "exact", "agent", "ask", time.Now()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	rows, err := AppPermissionRules(profile, false)
	if err != nil || len(rows) != 1 {
		t.Fatalf("duplicates: %+v %v", rows, err)
	}
	path := filepath.Join(profile, "app-permissions.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveAppPermissionRule(profile, "one", "bash", appBash("git diff"), "exact", "agent", "ask", time.Now()); err == nil {
		t.Fatal("malformed rules overwritten")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "broken" {
		t.Fatal("malformed file clobbered")
	}
}
