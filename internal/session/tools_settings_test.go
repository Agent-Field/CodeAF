package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// settingsAgent is one conversation with a profile directory of its own and
// nothing else — which is every seam these two tools touch.
func settingsAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	profile := filepath.Join(t.TempDir(), "profile")
	return &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: profile}}, profile
}

// settingsHands finds the pair on a belt, by the names the wire carries.
func settingsHands(t *testing.T, agent *Agent) (read, write bare.Tool) {
	t.Helper()
	for _, tool := range agent.settingsTools() {
		switch tool.Name {
		case "settings":
			read = tool
		case "change_setting":
			write = tool
		}
	}
	if read.Execute == nil || write.Execute == nil {
		t.Fatal("the settings pair is not on the belt")
	}
	return read, write
}

// callSetting runs one settings hand and returns what the model would read.
func callSetting(t *testing.T, tool bare.Tool, args any) (string, bool) {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	text, isError, err := tool.Execute(context.Background(), encoded)
	if err != nil {
		t.Fatalf("%s returned a transport error: %v", tool.Name, err)
	}
	return text, isError
}

// profileJSON is the file the registry writes, read back raw — the only way to
// prove a change is PERMANENT rather than merely reported.
func profileJSON(t *testing.T, profile string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(profile, "config.json"))
	if os.IsNotExist(err) {
		return map[string]any{}
	}
	if err != nil {
		t.Fatalf("read the profile config: %v", err)
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("the profile config is not json: %v", err)
	}
	return values
}

// The absence law, both halves. A session with no profile has nothing to read or
// write, and a task node must not be able to change the person's machine at all.
func TestTheSettingsPairIsAbsentInsideATaskAndPresentWithTheDefaultProfile(t *testing.T) {
	// AN EMPTY PROFILE DIRECTORY IS THE ORDINARY ONE. AFORGE_PROFILE_DIR is
	// unset on very nearly every machine, and every reader treats "" as the
	// default location — so a session with no explicit profile is the common
	// case, not a broken one, and it carries the pair. Gating on it once turned
	// this feature off for everybody who had not set that variable.
	ordinary := &Agent{config: Config{Workspace: t.TempDir()}}
	if tools := ordinary.settingsTools(); len(tools) != 2 {
		t.Errorf("a session on the default profile carries %d settings tools, want 2", len(tools))
	}
	node := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir(), InTask: true}}
	if tools := node.settingsTools(); len(tools) != 0 {
		t.Errorf("a task node carries %d settings tools", len(tools))
	}
	watched, _ := settingsAgent(t)
	if tools := watched.settingsTools(); len(tools) != 2 {
		t.Errorf("a conversation with a profile carries %d settings tools, want 2", len(tools))
	}
}

// The listing is the vocabulary: every row the registry has, by the key the
// write tool takes, grouped the way the sheet groups them.
func TestSettingsListsEveryRowByItsRegistryKey(t *testing.T) {
	agent, profile := settingsAgent(t)
	agent.model = "anthropic/claude-opus-5"
	read, _ := settingsHands(t, agent)
	text, isError := callSetting(t, read, map[string]string{})
	if isError {
		t.Fatalf("listing failed: %s", text)
	}
	// The conversation's own row reads the model this session is actually on.
	// Without the live seam it reads blank, and a sheet whose first row is empty
	// is a sheet that looks broken.
	if !strings.Contains(text, config.ModelSettingKey("talk")+" · conversation · anthropic/claude-opus-5") {
		t.Errorf("the conversation row does not name the model this session is on:\n%s", text)
	}
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: profile})
	for _, row := range registry.Rows() {
		if !strings.Contains(text, row.Key) {
			t.Errorf("the listing does not carry the row %s", row.Key)
		}
	}
	for _, category := range config.SettingCategories {
		if !strings.Contains(text, category) {
			t.Errorf("the listing does not name the %q group", category)
		}
	}
	// And the marker that stops a model spending a call to find out.
	if !strings.Contains(text, config.KeyDailyBudget+" · daily budget") {
		t.Errorf("the listing does not read as `key · label · value`:\n%s", text)
	}
	if !strings.Contains(text, "yours to change") {
		t.Error("the listing does not mark the rows the model may not write")
	}
}

// One row read in full says what it takes, which is the difference between one
// call and three.
func TestSettingsReadsOneRowInFull(t *testing.T) {
	agent, _ := settingsAgent(t)
	read, _ := settingsHands(t, agent)
	text, isError := callSetting(t, read, map[string]string{"key": config.KeyTimestamps})
	if isError {
		t.Fatalf("reading one row failed: %s", text)
	}
	for _, want := range []string{config.KeyTimestamps, "timestamps", "footers", "one of:", "change_setting"} {
		if !strings.Contains(text, want) {
			t.Errorf("the row detail does not mention %q:\n%s", want, text)
		}
	}
}

// A search narrows the sheet without the model having to read all of it.
func TestSettingsSearchNarrowsToTheRowsThatMentionIt(t *testing.T) {
	agent, _ := settingsAgent(t)
	read, _ := settingsHands(t, agent)
	text, isError := callSetting(t, read, map[string]string{"search": "timestamps"})
	if isError {
		t.Fatalf("search failed: %s", text)
	}
	if !strings.Contains(text, config.KeyTimestamps) {
		t.Errorf("a search for timestamps did not find the row:\n%s", text)
	}
	if strings.Contains(text, config.KeyNerdFont) {
		t.Errorf("a search for timestamps returned the nerd font row too:\n%s", text)
	}
	// A search that matches nothing is an honest empty answer and not a fault —
	// the manual tool's own posture for the same question — but it must say so
	// rather than returning a listing with no rows in it.
	empty, isError := callSetting(t, read, map[string]string{"search": "quantum tunnelling"})
	if isError {
		t.Errorf("a search matching nothing was reported as an error: %s", empty)
	}
	if !strings.Contains(empty, "No setting mentions") {
		t.Errorf("a search matching nothing answered %q", empty)
	}
}

// ── the refusals ────────────────────────────────────────────────────────────

// THE REFUSAL THAT MATTERS MOST. Every row that restrains the model is refused
// by name, and — the half a mistake would hide — the file is not touched.
func TestChangeSettingRefusesEveryRowThatRestrainsIt(t *testing.T) {
	guarded := []struct {
		key   string
		value string
	}{
		{config.KeyToolApprovalMode, "allow"},
		{config.KeyToolApprovals, "bash:allow"},
		{config.KeyBashApprovals, "allow rm -rf *"},
		{config.KeyGuardian, "on"},
		{config.KeyConsentTimeout, "1"},
		{config.KeyTaskAutoApprove, "0"},
		{config.KeyDailyBudget, "500"},
		{config.KeyPlanConsent, "0"},
		{config.KeyPracticeBudget, "50"},
		{config.KeySpendRail, "0"},
		{config.KeyTaskRepairRounds, "9"},
		{config.KeyWorkingSet, "900000"},
		{config.KeyContextReuse, "1000"},
		{config.KeyTaskParallel, "64"},
		{config.KeyTaskMaxLoad, "0"},
		{config.KeyTaskMinFreeMB, "0"},
		{config.KeyTaskAudit, "off"},
		{config.KeyAttribution, "off"},
		{config.KeyExaKey, "sk-invented"},
		{config.KeyJinaKey, "jina-invented"},
		{config.KeyGoogleOAuthClient, "invented.apps.googleusercontent.com"},
		{config.KeyGoogleOAuthSecret, "invented"},
	}
	for _, guard := range guarded {
		agent, profile := settingsAgent(t)
		_, write := settingsHands(t, agent)
		text, isError := callSetting(t, write, map[string]string{"key": guard.key, "value": guard.value})
		if !isError {
			t.Errorf("%s was written by the model: %s", guard.key, text)
		}
		if !strings.Contains(text, guard.key) || !strings.Contains(text, "/settings") {
			t.Errorf("%s refuses with %q, which does not name the row and the door", guard.key, text)
		}
		if values := profileJSON(t, profile); len(values) != 0 {
			t.Errorf("%s was refused and the profile was written anyway: %v", guard.key, values)
		}
	}
}

// A role model slot is refused for a different reason — there is nothing in the
// profile to write — and the refusal names the roads that do exist rather than
// the registry's "model switching is unavailable here", which names none.
func TestChangeSettingRefusesTheRoleModelSlotsAndNamesTheRealDoor(t *testing.T) {
	agent, profile := settingsAgent(t)
	_, write := settingsHands(t, agent)

	text, isError := callSetting(t, write, map[string]string{
		"key": config.ModelSettingKey("talk"), "value": "deepseek/deepseek-v4-pro",
	})
	if !isError {
		t.Errorf("the conversation's model was written through the settings tool: %s", text)
	}
	if !strings.Contains(text, "/model") {
		t.Errorf("the conversation slot refuses with %q, which does not name /model", text)
	}

	text, isError = callSetting(t, write, map[string]string{
		"key": config.ModelSettingKey("plan"), "value": "deepseek/deepseek-v4-pro",
	})
	if !isError {
		t.Errorf("a role slot was written through the settings tool: %s", text)
	}
	for _, want := range []string{config.KeyTierHighModel, config.KeyModelRoles} {
		if !strings.Contains(text, want) {
			t.Errorf("the role slot refusal does not point at %s:\n%s", want, text)
		}
	}
	if values := profileJSON(t, profile); len(values) != 0 {
		t.Errorf("a refused model slot wrote the profile anyway: %v", values)
	}
}

// An unknown key is a refusal that NAMES THE NEAR MISSES, because a model that
// is only told "no" tries again in different words.
func TestChangeSettingRefusesAnInventedKeyAndOffersTheNearMisses(t *testing.T) {
	agent, profile := settingsAgent(t)
	read, write := settingsHands(t, agent)

	text, isError := callSetting(t, write, map[string]string{"key": "daily budget", "value": "5"})
	if !isError {
		t.Fatalf("an invented key was accepted: %s", text)
	}
	if !strings.Contains(text, config.KeyDailyBudget) {
		t.Errorf("the refusal does not offer %s:\n%s", config.KeyDailyBudget, text)
	}
	if values := profileJSON(t, profile); len(values) != 0 {
		t.Errorf("an invented key wrote something: %v", values)
	}

	// A key that resembles nothing still says where to look, rather than
	// leaving the model with a dead end.
	text, isError = callSetting(t, write, map[string]string{"key": "zzzz", "value": "5"})
	if !isError {
		t.Fatalf("an unrecognisable key was accepted: %s", text)
	}
	if !strings.Contains(text, "settings") {
		t.Errorf("the refusal does not name the tool that lists the rows:\n%s", text)
	}

	// And the read half refuses the same way.
	if text, isError := callSetting(t, read, map[string]string{"key": "tools.approval_mode"}); !isError {
		t.Errorf("reading an invented key was not an error: %s", text)
	} else if !strings.Contains(text, config.KeyToolApprovalMode) {
		t.Errorf("reading an invented key does not offer the near miss:\n%s", text)
	}
}

// A value the row cannot take comes back in the REGISTRY's own words — the
// sentence the panel shows for the same mistake.
func TestChangeSettingRefusesABadValueInTheRegistrysOwnWords(t *testing.T) {
	agent, profile := settingsAgent(t)
	_, write := settingsHands(t, agent)
	text, isError := callSetting(t, write, map[string]string{"key": config.KeyTimestamps, "value": "sometimes"})
	if !isError {
		t.Fatalf("a value outside the row's choices was accepted: %s", text)
	}
	if !strings.Contains(text, "pick one of:") {
		t.Errorf("the refusal is not the registry's own wording: %q", text)
	}
	if values := profileJSON(t, profile); len(values) != 0 {
		t.Errorf("a refused value wrote the profile anyway: %v", values)
	}
}

// The environment still outranks everybody, this tool included.
func TestChangeSettingHonorsAnEnvironmentPin(t *testing.T) {
	t.Setenv("AFORGE_NERD_FONT", "0")
	agent, profile := settingsAgent(t)
	_, write := settingsHands(t, agent)
	text, isError := callSetting(t, write, map[string]string{"key": config.KeyNerdFont, "value": "on"})
	if !isError {
		t.Fatalf("a pinned row was written: %s", text)
	}
	if !strings.Contains(text, "AFORGE_NERD_FONT") {
		t.Errorf("the refusal does not name the variable holding the row: %q", text)
	}
	if values := profileJSON(t, profile); len(values) != 0 {
		t.Errorf("a pinned row wrote the profile anyway: %v", values)
	}
}

// ── the write that lands ────────────────────────────────────────────────────

// The whole point: a change is PERMANENT, it is the registry's own write, and
// the person is told on screen what moved.
func TestChangeSettingWritesTheProfileAndSaysSoOnScreen(t *testing.T) {
	agent, profile := settingsAgent(t)
	read, write := settingsHands(t, agent)

	hub := newEventHub()
	agent.hub = hub
	events := hub.subscribe()

	text, isError := callSetting(t, write, map[string]string{"key": config.KeyTimestamps, "value": "off"})
	if isError {
		t.Fatalf("a plain preference was refused: %s", text)
	}
	for _, want := range []string{"timestamps", config.KeyTimestamps, "off", "footers"} {
		if !strings.Contains(text, want) {
			t.Errorf("the answer does not mention %q:\n%s", want, text)
		}
	}

	// On disk, through the registry's own writer.
	if got := config.TimestampsAt(profile); got != config.TimestampsOff {
		t.Errorf("the profile reads %q after the write, want %q", got, config.TimestampsOff)
	}
	if values := profileJSON(t, profile); values[config.KeyTimestamps] != "off" {
		t.Errorf("config.json holds %v under %s", values[config.KeyTimestamps], config.KeyTimestamps)
	}

	// And in front of the person, on the turn's own fan-out.
	notice := waitForNotice(t, events)
	if !strings.Contains(notice, "timestamps") || !strings.Contains(notice, "footers") || !strings.Contains(notice, "off") {
		t.Errorf("the transcript note does not say what changed: %q", notice)
	}

	// The read half sees the new value straight away — the rows read off disk,
	// so nothing is cached into disagreeing with the file.
	listing, _ := callSetting(t, read, map[string]string{"key": config.KeyTimestamps})
	if !strings.Contains(listing, "now: off") {
		t.Errorf("the row still reads its old value:\n%s", listing)
	}
}

// The two rows the person asked for by name, and the shape their value takes.
func TestChangeSettingCanPinTheRolesAndSetTheTiers(t *testing.T) {
	agent, profile := settingsAgent(t)
	_, write := settingsHands(t, agent)

	pins := "designer:deepseek/deepseek-v4-pro, planner:deepseek/deepseek-v4-pro"
	if text, isError := callSetting(t, write, map[string]string{"key": config.KeyModelRoles, "value": pins}); isError {
		t.Fatalf("the role pins were refused: %s", text)
	}
	parsed, err := config.ParseModelRoles(config.ModelRolesAt(profile))
	if err != nil {
		t.Fatalf("the pins did not round-trip: %v", err)
	}
	for _, role := range []string{"designer", "planner"} {
		if parsed[role] != "deepseek/deepseek-v4-pro" {
			t.Errorf("the %s role reads %q", role, parsed[role])
		}
	}

	if text, isError := callSetting(t, write, map[string]string{"key": config.KeyTierHighModel, "value": "deepseek/deepseek-v4-pro"}); isError {
		t.Fatalf("the careful-work tier was refused: %s", text)
	}
	if got := config.TierModelAt(profile, config.ModelTierHigh); got != "deepseek/deepseek-v4-pro" {
		t.Errorf("the careful-work tier reads %q", got)
	}
}

// An empty value clears a row back to its default, which is what "stop pinning
// that" means and the only way to say it.
func TestChangeSettingClearsARowWithAnEmptyValue(t *testing.T) {
	agent, profile := settingsAgent(t)
	_, write := settingsHands(t, agent)
	if text, isError := callSetting(t, write, map[string]string{"key": config.KeyTaskModel, "value": "openai/gpt-5-mini"}); isError {
		t.Fatalf("setting the task model was refused: %s", text)
	}
	if text, isError := callSetting(t, write, map[string]string{"key": config.KeyTaskModel, "value": ""}); isError {
		t.Fatalf("clearing the task model was refused: %s", text)
	}
	if got := config.TaskModelAt(profile); got != "" {
		t.Errorf("the task model still reads %q after being cleared", got)
	}
}

// Writing what is already there is not an error, and it does not claim a change
// nobody made.
func TestChangeSettingSaysNothingChangedWhenNothingDid(t *testing.T) {
	agent, _ := settingsAgent(t)
	_, write := settingsHands(t, agent)
	text, isError := callSetting(t, write, map[string]string{"key": config.KeyTimestamps, "value": config.DefaultTimestamps})
	if isError {
		t.Fatalf("re-writing a row's current value was an error: %s", text)
	}
	if !strings.Contains(text, "already") {
		t.Errorf("re-writing a row's current value reads as a change: %q", text)
	}
}

// A missing key is caught before anything else, because "" would otherwise walk
// into the near-miss search and come back with a suggestion nobody asked for.
func TestChangeSettingNeedsAKey(t *testing.T) {
	agent, _ := settingsAgent(t)
	_, write := settingsHands(t, agent)
	text, isError := callSetting(t, write, map[string]string{"value": "5"})
	if !isError || !strings.Contains(text, "key is required") {
		t.Errorf("a call with no key answered %q", text)
	}
}

// waitForNotice reads the one EventNotice the write emits, or fails. The turn's
// fan-out is a channel and a test that read it with a bare receive would hang
// the whole package if the event were ever dropped.
func waitForNotice(t *testing.T, events <-chan Event) string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				t.Fatal("the event stream closed before a notice arrived")
			}
			if event.Kind == EventNotice {
				return event.Text
			}
		case <-deadline:
			t.Fatal("no notice reached the transcript")
		}
	}
}
