package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The project-local layer is a file people check into a repository, so the tests
// are about the two ways such a file can betray them: applying when it should
// not, and — far worse — not applying while looking as though it does.

// projectDir writes one <cwd>/.openaf/config.json and answers with the cwd.
func projectDir(t *testing.T, rows map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	writeProjectFile(t, dir, mustJSON(t, rows))
	return dir
}

// writeProjectFile puts arbitrary bytes at the project path, so a test can write
// something that is not valid JSON at all.
func writeProjectFile(t *testing.T, cwd string, raw []byte) {
	t.Helper()
	path := ProjectConfigPath(cwd)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// profileDirWith writes one profile config.json and answers with its directory.
func profileDirWith(t *testing.T, rows map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), mustJSON(t, rows), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func mustJSON(t *testing.T, rows map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// unpinned clears the two environment variables that outrank this layer, so a
// developer's own shell cannot decide whether the suite passes.
func unpinned(t *testing.T) {
	t.Helper()
	t.Setenv("AFORGE_HISTORY", "")
	t.Setenv("AFORGE_DRAFT_PERSIST", "")
}

// Every row this build honors, answered in all three layers at once: the project
// file, the profile, and neither. One table, because the whole promise of the
// layer is that it is the SAME promise for every row.
func TestEveryProjectRowResolvesProjectOverProfileOverDefault(t *testing.T) {
	unpinned(t)
	project := projectDir(t, map[string]any{
		KeyToolApprovalMode: "deny",
		KeyToolApprovals:    "bash:deny",
		KeyTierLowModel:     "project/cheap",
		KeyTierHighModel:    "project/capable",
		KeyModelRoles:       "title:project/title",
		KeySpendRail:        2.5,
		KeyHistoryEnabled:   false,
		KeyDraftPersist:     false,
	})
	profile := profileDirWith(t, map[string]any{
		KeyToolApprovalMode: "allow",
		KeyToolApprovals:    "bash:allow",
		KeyTierLowModel:     "profile/cheap",
		KeyTierHighModel:    "profile/capable",
		KeyModelRoles:       "title:profile/title",
		KeySpendRail:        9.0,
		KeyHistoryEnabled:   true,
		KeyDraftPersist:     true,
	})
	bare := t.TempDir()

	for _, row := range []struct {
		key                       string
		project, profile, builtin string
	}{
		{KeyToolApprovalMode, "deny", "allow", DefaultToolApprovalMode},
		{KeyToolApprovals, "bash:deny", "bash:allow", ""},
		{KeyTierLowModel, "project/cheap", "profile/cheap", ""},
		{KeyTierHighModel, "project/capable", "profile/capable", ""},
		{KeyModelRoles, "title:project/title", "title:profile/title", ""},
	} {
		got, err := ProjectStringAt(project, profile, row.key)
		if err != nil || got != row.project {
			t.Fatalf("%s with a project file → %q (%v), want %q", row.key, got, err, row.project)
		}
		got, err = ProjectStringAt(bare, profile, row.key)
		if err != nil || got != row.profile {
			t.Fatalf("%s with no project file → %q (%v), want the profile's %q", row.key, got, err, row.profile)
		}
		got, err = ProjectStringAt(bare, bare, row.key)
		if err != nil || got != row.builtin {
			t.Fatalf("%s with nothing set → %q (%v), want the default %q", row.key, got, err, row.builtin)
		}
	}

	rail, err := ProjectFloatAt(project, profile, KeySpendRail)
	if err != nil || rail != 2.5 {
		t.Fatalf("the ceiling → %v (%v), want the project's 2.5", rail, err)
	}
	if rail, err = ProjectFloatAt(bare, profile, KeySpendRail); err != nil || rail != 9.0 {
		t.Fatalf("the ceiling with no project file → %v (%v), want the profile's 9", rail, err)
	}
	if rail, err = ProjectFloatAt(bare, bare, KeySpendRail); err != nil || rail != DefaultSpendRailUSD {
		t.Fatalf("the ceiling with nothing set → %v (%v), want the default", rail, err)
	}

	for _, key := range []string{KeyHistoryEnabled, KeyDraftPersist} {
		got, err := ProjectBoolAt(project, profile, key)
		if err != nil || got {
			t.Fatalf("%s with a project file → %v (%v), want the project's off", key, got, err)
		}
		if got, err = ProjectBoolAt(bare, profile, key); err != nil || !got {
			t.Fatalf("%s with no project file → %v (%v), want the profile's on", key, got, err)
		}
		if got, err = ProjectBoolAt(bare, bare, key); err != nil || !got {
			t.Fatalf("%s with nothing set → %v (%v), want the default on", key, got, err)
		}
	}
}

// A repository with no settings costs nothing and says nothing.
func TestAMissingProjectFileIsAnEmptyLayerAndNeverAnError(t *testing.T) {
	unpinned(t)
	profile := profileDirWith(t, map[string]any{KeyTierLowModel: "profile/cheap"})

	layer, err := LoadProjectConfig(t.TempDir())
	if err != nil {
		t.Fatalf("a directory with no settings failed to load: %v", err)
	}
	if layer.Has(KeyTierLowModel) {
		t.Fatal("an absent file answered a row")
	}
	got, err := ProjectStringAt(t.TempDir(), profile, KeyTierLowModel)
	if err != nil || got != "profile/cheap" {
		t.Fatalf("→ %q (%v), want the profile's value", got, err)
	}

	// No workspace at all is the same empty layer, and NOT a lookup in whatever
	// directory the test process happens to be sitting in.
	if got, err = ProjectStringAt("", profile, KeyTierLowModel); err != nil || got != "profile/cheap" {
		t.Fatalf("an empty workspace → %q (%v)", got, err)
	}
	if path := ProjectConfigPath(""); path != "" {
		t.Fatalf("an empty workspace resolved to a path: %q", path)
	}
}

// The one thing this layer refuses to be quiet about.
func TestAMalformedProjectFileErrorsAndNamesThePath(t *testing.T) {
	unpinned(t)
	profile := profileDirWith(t, map[string]any{KeyToolApprovalMode: "allow"})

	broken := t.TempDir()
	writeProjectFile(t, broken, []byte(`{"tools.approvalMode": `))
	path := ProjectConfigPath(broken)

	if _, err := LoadProjectConfig(broken); err == nil {
		t.Fatal("a truncated project file loaded")
	} else if !strings.Contains(err.Error(), path) {
		t.Fatalf("the error does not name the file: %v", err)
	}
	// And it stops every reader, not only the one that happens to look first —
	// a gate that opened because the resolver shrugged is the whole failure.
	if _, err := ProjectStringAt(broken, profile, KeyToolApprovalMode); err == nil {
		t.Fatal("a truncated project file resolved a row anyway")
	}
	if _, err := ProjectFloatAt(broken, profile, KeySpendRail); err == nil {
		t.Fatal("a truncated project file resolved the ceiling anyway")
	}
	if _, err := ProjectBoolAt(broken, profile, KeyHistoryEnabled); err == nil {
		t.Fatal("a truncated project file resolved an on/off row anyway")
	}

	// A row of the wrong type is the same class of fault: a rule somebody wrote
	// that would otherwise be skipped in silence.
	for _, rows := range []map[string]any{
		{KeyToolApprovalMode: 3},
		{KeySpendRail: "lots"},
		{KeySpendRail: -1},
		{KeyHistoryEnabled: "sometimes"},
		{KeyTierLowModel: []string{"a", "b"}},
	} {
		dir := projectDir(t, rows)
		var err error
		for key := range rows {
			switch key {
			case KeySpendRail:
				_, err = ProjectFloatAt(dir, profile, key)
			case KeyHistoryEnabled:
				_, err = ProjectBoolAt(dir, profile, key)
			default:
				_, err = ProjectStringAt(dir, profile, key)
			}
			if err == nil {
				t.Fatalf("%v was accepted", rows)
			}
			if !strings.Contains(err.Error(), ProjectConfigPath(dir)) || !strings.Contains(err.Error(), key) {
				t.Fatalf("the error names neither the file nor the row: %v", err)
			}
		}
	}

	// An unreadable value in the GATE's own row is refused rather than forgiven,
	// unlike the same row in a personal profile: a checked-in file is a rule a
	// team believes is in force.
	dir := projectDir(t, map[string]any{KeyToolApprovalMode: "sometimes"})
	if _, err := ProjectStringAt(dir, profile, KeyToolApprovalMode); err == nil {
		t.Fatal("an unknown approval mode was accepted from a project file")
	} else if !strings.Contains(err.Error(), "sometimes") {
		t.Fatalf("the error does not quote the value: %v", err)
	}
}

// The dotted spelling is the only one, and a file written the other way is told
// so rather than loaded and ignored.
func TestANestedProjectFileIsRefusedByName(t *testing.T) {
	unpinned(t)
	dir := t.TempDir()
	writeProjectFile(t, dir, []byte(`{"models": {"tiers": {"low": "project/cheap"}}}`))

	_, err := LoadProjectConfig(dir)
	if err == nil {
		t.Fatal("a nested project file loaded, and its row would never have applied")
	}
	for _, want := range []string{ProjectConfigPath(dir), KeyTierLowModel} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not name %q: %v", want, err)
		}
	}

	// A nested section this build knows nothing about is somebody else's
	// business and never stops a launch.
	quiet := t.TempDir()
	writeProjectFile(t, quiet, []byte(`{"someOtherTool": {"deep": {"thing": 1}}, "tools.approvalMode": "deny"}`))
	got, err := ProjectStringAt(quiet, t.TempDir(), KeyToolApprovalMode)
	if err != nil || got != "deny" {
		t.Fatalf("an unknown nested section broke the file: %q (%v)", got, err)
	}
}

// omp's merge law: a map replaces, it does not deep-merge. This is the test the
// whole layer is judged by, because a half-applied rule set is one nobody wrote.
func TestMapsReplaceWholesaleAndNeverMerge(t *testing.T) {
	unpinned(t)
	profile := profileDirWith(t, map[string]any{
		KeyToolApprovals: "read:allow, bash:prompt, write:allow",
		KeyModelRoles:    "title:profile/title, compaction:profile/compaction",
	})
	project := projectDir(t, map[string]any{
		KeyToolApprovals: "write:deny",
		KeyModelRoles:    "title:project/title",
	})

	got, err := ProjectStringAt(project, profile, KeyToolApprovals)
	if err != nil {
		t.Fatal(err)
	}
	if got != "write:deny" {
		t.Fatalf("the approvals map resolved to %q — the profile's entries leaked through", got)
	}
	tools, err := ParseToolApprovals(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools["write"] != "deny" {
		t.Fatalf("the resolved map is %v, want exactly one rule", tools)
	}

	got, err = ProjectStringAt(project, profile, KeyModelRoles)
	if err != nil {
		t.Fatal(err)
	}
	pins, err := ParseModelRoles(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 || pins["title"] != "project/title" {
		t.Fatalf("the resolved pins are %v, want only the project's", pins)
	}
}

// A person hand-writing a settings file writes a map as a map. It means the same
// thing, replaces the same way, and is read by the same one parser.
func TestAMapRowMayBeWrittenAsAnObject(t *testing.T) {
	unpinned(t)
	profile := profileDirWith(t, map[string]any{KeyToolApprovals: "read:allow"})
	project := projectDir(t, map[string]any{
		KeyToolApprovals: map[string]string{"write": "deny", "bash": "prompt"},
		KeyModelRoles:    map[string]string{"title": "openai/gpt-5-mini:free"},
	})

	got, err := ProjectStringAt(project, profile, KeyToolApprovals)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := ParseToolApprovals(got)
	if err != nil {
		t.Fatalf("the object form did not survive the parser: %v (%q)", err, got)
	}
	if len(tools) != 2 || tools["write"] != "deny" || tools["bash"] != "prompt" {
		t.Fatalf("the object form resolved to %v", tools)
	}

	// A model slug carries its own colon, and the flat form splits at the first
	// one only — so the round trip has to keep it whole.
	got, err = ProjectStringAt(project, profile, KeyModelRoles)
	if err != nil {
		t.Fatal(err)
	}
	pins, err := ParseModelRoles(got)
	if err != nil || pins["title"] != "openai/gpt-5-mini:free" {
		t.Fatalf("the slug came back as %v (%v)", pins, err)
	}

	// A separator inside a name or a value would come back out of that parser as
	// two rules. It is refused rather than escaped.
	broken := projectDir(t, map[string]any{
		KeyModelRoles: map[string]string{"title": "one/model, two/model"},
	})
	if _, err := ProjectStringAt(broken, profile, KeyModelRoles); err == nil {
		t.Fatal("a comma inside a value was accepted")
	}
}

// An empty project value is an ANSWER: it is how a repository turns a personal
// pin off. Treating it as "unset" would leave a repo unable to say no.
func TestAnEmptyProjectValueOverridesRatherThanFallsThrough(t *testing.T) {
	unpinned(t)
	profile := profileDirWith(t, map[string]any{
		KeyTierLowModel: "profile/cheap",
		KeyModelRoles:   "title:profile/title",
	})
	project := projectDir(t, map[string]any{KeyTierLowModel: "", KeyModelRoles: ""})

	for _, key := range []string{KeyTierLowModel, KeyModelRoles} {
		got, err := ProjectStringAt(project, profile, key)
		if err != nil || got != "" {
			t.Fatalf("%s → %q (%v), want the project's empty answer", key, got, err)
		}
	}
}

// The environment is the operator speaking about THIS process, and a file
// checked into a repository does not get to overrule it.
func TestTheEnvironmentStillOutranksTheProjectFile(t *testing.T) {
	unpinned(t)
	profile := profileDirWith(t, map[string]any{KeyHistoryEnabled: true})
	project := projectDir(t, map[string]any{KeyHistoryEnabled: false})

	got, err := ProjectBoolAt(project, profile, KeyHistoryEnabled)
	if err != nil || got {
		t.Fatalf("without a pin → %v (%v), want the project's off", got, err)
	}
	t.Setenv("AFORGE_HISTORY", "on")
	if got, err = ProjectBoolAt(project, profile, KeyHistoryEnabled); err != nil || !got {
		t.Fatalf("with the pin set → %v (%v), want the pin's on", got, err)
	}
	// A pin nobody can read is not a choice, and the file below answers.
	t.Setenv("AFORGE_HISTORY", "sometimes")
	if got, err = ProjectBoolAt(project, profile, KeyHistoryEnabled); err != nil || got {
		t.Fatalf("with an unreadable pin → %v (%v), want the project's off", got, err)
	}
}

// A file checked into a repository must not be able to move somebody's daily
// budget or their vision model. The allowlist is that boundary, and a row
// outside it is refused rather than quietly honored.
func TestOnlyTheAllowlistedRowsMayLiveInAProjectFile(t *testing.T) {
	unpinned(t)
	dir := projectDir(t, map[string]any{
		KeyDailyBudget: 500.0,
		KeyVisionModel: "expensive/model",
	})
	if _, err := ProjectStringAt(dir, t.TempDir(), KeyVisionModel); err == nil {
		t.Fatal("a project file answered a row it does not own")
	}
	if _, err := ProjectFloatAt(dir, t.TempDir(), KeyDailyBudget); err == nil {
		t.Fatal("a project file answered the daily budget")
	}
	// And the person's own rows are untouched by the file's presence.
	budget, err := DailyBudgetUSDAt(t.TempDir())
	if err != nil || budget != DefaultDailyBudgetUSD {
		t.Fatalf("the daily budget moved: %v (%v)", budget, err)
	}

	// The typed resolvers also refuse a row of the wrong shape, which is a
	// programming mistake rather than a person's.
	if _, err := ProjectBoolAt(dir, t.TempDir(), KeySpendRail); err == nil {
		t.Fatal("a dollar row resolved as on/off")
	}
	if _, err := ProjectFloatAt(dir, t.TempDir(), KeyDraftPersist); err == nil {
		t.Fatal("an on/off row resolved as dollars")
	}
	if _, err := ProjectStringAt(dir, t.TempDir(), KeySpendRail); err == nil {
		t.Fatal("a dollar row resolved as text")
	}
}

// The allowlist is documentation as much as a gate: every key in it has to be a
// key the registry actually has a row for, or the file is promising something
// the sheet cannot show.
func TestEveryProjectKeyIsARegisteredSettingsRow(t *testing.T) {
	rows := registry(t, t.TempDir())
	for _, key := range ProjectKeys {
		if _, ok := rows.Row(key); !ok {
			t.Fatalf("%s may live in a project file but is not a settings row", key)
		}
	}
}
