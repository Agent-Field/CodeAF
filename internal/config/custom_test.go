package config

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// The surfaces' action rows ("add custom connection", "active connection")
// carry sentinel ids that must never belong to a connection too: cursor and
// armed state are keyed by row id, and IsCustomID-based code would read a
// colliding row as a connection. THE NAMESPACE IS THE GUARANTEE, NOT A BAN:
// a sentinel id lives outside the custom/custom- namespace (modelsource.
// IsCustomID is false for it), and PrepareCustomSource — which mints custom
// or custom-<name> — can therefore never produce one, whatever a person
// names a connection. The literals are the tui3 sentinels' values; config
// cannot import tui3 to read the constants themselves.
func TestPrepareCustomSourceNeverMintsASentinelRowId(t *testing.T) {
	pre := [][]PersistedSource{
		nil,
		{{ID: modelsource.CustomID, Written: "homelab", Address: "http://127.0.0.1:9001/v1", Order: 1}},
	}
	for _, name := range []string{"add", "new-custom-connection", "switch-connection", "custom"} {
		for _, rows := range pre {
			dir := t.TempDir()
			if rows != nil {
				if err := WriteSources(dir, rows); err != nil {
					t.Fatal(err)
				}
			}
			row := PrepareCustomSource(dir, "http://127.0.0.1:9000/v1", name)
			if !modelsource.IsCustomID(row.ID) {
				t.Fatalf("name %q minted id %q, which is outside the custom namespace", name, row.ID)
			}
			for _, sentinel := range []string{"new-custom-connection", "switch-connection"} {
				if row.ID == sentinel {
					t.Fatalf("name %q minted id %q, which is a sentinel row id", name, row.ID)
				}
			}
		}
	}
}
func TestRenameConnectionMovesEveryStoredModelIdIncludingTheTierRows(t *testing.T) {
	dir := t.TempDir()
	// The tier rows are written through the crew's own writer, so what a
	// rename reads back is exactly what a person's /crew write produced.
	for _, row := range []struct{ tier, value string }{
		{ModelTierHigh, "homelab/a"},
		{ModelTierWorker, "homelab/b:low"},
		{ModelTierReflex, "openai/c"},
	} {
		if err := writeTierModel(dir, row.tier, row.value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeText(dir, KeyModelFallbacks, "homelab/a, openai/x"); err != nil {
		t.Fatal(err)
	}
	if err := writeModelRoles(dir, "planner:homelab/a, worker:openai/y"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(dir, ModelSettingKey("image"), "homelab/img"); err != nil {
		t.Fatal(err)
	}

	changed, err := RenameConnectionModels(dir, "homelab", "lab")
	if err != nil {
		t.Fatal(err)
	}
	// The tier rows move first, in [ModelTiers]'s own order, then the fallback
	// chain, the role pins and the capability slots.
	if want := []string{
		KeyTierWorkerModel, KeyTierHighModel, KeyModelFallbacks, KeyModelRoles, ModelSettingKey("image"),
	}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("the rename named the wrong keys: %v", changed)
	}
	if got := TierModelAt(dir, ModelTierHigh); got != "lab/a" {
		t.Fatalf("the high tier did not follow the rename: %q", got)
	}
	if got := TierModelAt(dir, ModelTierWorker); got != "lab/b:low" {
		t.Fatalf("the worker tier lost the level it carried: %q", got)
	}
	if got := TierModelAt(dir, ModelTierReflex); got != "openai/c" {
		t.Fatalf("the reflex tier answered on another connection's name: %q", got)
	}
	if got := ModelFallbacksAt(dir); got != "lab/a, openai/x" {
		t.Fatalf("the fallback chain did not follow the rename: %q", got)
	}
	if got := ModelRolesAt(dir); got != "planner:lab/a, worker:openai/y" {
		t.Fatalf("the role pins did not follow the rename: %q", got)
	}
	if got, _ := persistedString(dir, ModelSettingKey("image")); got != "lab/img" {
		t.Fatalf("the capability slot did not follow the rename: %q", got)
	}
	// A ROW THAT WAS NEVER HELD STAYS NEVER HELD: writing it would turn an
	// inherited tier into a pinned one.
	if _, held := persistedString(dir, KeyTierMastermindModel); held {
		t.Fatal("the rename pinned a tier row nobody had written")
	}
}

// A RENAME THAT CANNOT LAND MOVES NOTHING: every re-prefixed value goes
// through the tier gate before anything is written ([ValidateTierValue], the
// gate [writeTierModel] applies), so a row whose value the rename would make
// invalid fails the rename whole. The invalid suffix is seeded through
// writeText — the raw row, the way a profile written before the gate existed
// could carry it; a tier row before it in [ModelTiers] order holds a value the
// rename would move, which is exactly the row the one-write law protects.
func TestRenameConnectionWithAnInvalidTierValueLeavesEveryRowUnchanged(t *testing.T) {
	dir := t.TempDir()
	if err := writeText(dir, KeyTierLowModel, "homelab/b:low"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(dir, KeyTierHighModel, "homelab/a:mid"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(dir, KeyModelFallbacks, "homelab/a, openai/x"); err != nil {
		t.Fatal(err)
	}
	if err := writeModelRoles(dir, "planner:homelab/a"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(dir, ModelSettingKey("image"), "homelab/img"); err != nil {
		t.Fatal(err)
	}
	before, err := readProfileConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := RenameConnectionModels(dir, "homelab", "lab")
	if err == nil {
		t.Fatal("a rename that produced an invalid tier value did not fail")
	}
	if changed != nil {
		t.Fatalf("a failed rename reported rows moved: %v", changed)
	}
	after, err := readProfileConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a failed rename rewrote the profile: %v -> %v", before, after)
	}
	if got := TierModelAt(dir, ModelTierLow); got != "homelab/b:low" {
		t.Fatalf("the low tier moved before the failure: %q", got)
	}
	if got := TierModelAt(dir, ModelTierHigh); got != "homelab/a:mid" {
		t.Fatalf("the high tier did not keep the value the rename refused: %q", got)
	}
	if got := ModelFallbacksAt(dir); got != "homelab/a, openai/x" {
		t.Fatalf("the fallback chain moved before the failure: %q", got)
	}
	if got := ModelRolesAt(dir); got != "planner:homelab/a" {
		t.Fatalf("the role pins moved before the failure: %q", got)
	}
	if got, _ := persistedString(dir, ModelSettingKey("image")); got != "homelab/img" {
		t.Fatalf("the capability slot moved before the failure: %q", got)
	}
}

func TestRenameConnectionTwiceIsANoOp(t *testing.T) {
	dir := t.TempDir()
	if err := writeTierModel(dir, ModelTierLow, "homelab/a:high"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(dir, KeyModelFallbacks, "homelab/a"); err != nil {
		t.Fatal(err)
	}
	changed, err := RenameConnectionModels(dir, "homelab", "lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) == 0 {
		t.Fatal("the first rename reported nothing moved")
	}
	if changed, err := RenameConnectionModels(dir, "homelab", "lab"); err != nil || changed != nil {
		t.Fatalf("the second rename was not a no-op: %v, %v", changed, err)
	}
}

func TestRenameConnectionRefusesNothingAndNoChange(t *testing.T) {
	dir := t.TempDir()
	if err := writeText(dir, KeyModelFallbacks, "homelab/a"); err != nil {
		t.Fatal(err)
	}
	if changed, err := RenameConnectionModels(dir, "", "lab"); err != nil || changed != nil {
		t.Fatalf("a blank old name was not a no-op: %v, %v", changed, err)
	}
	if changed, err := RenameConnectionModels(dir, "homelab", ""); err != nil || changed != nil {
		t.Fatalf("a blank new name was not a no-op: %v, %v", changed, err)
	}
	if changed, err := RenameConnectionModels(dir, "HOMELAB", "homelab"); err != nil || changed != nil {
		t.Fatalf("a name that only changed its case was not a no-op: %v, %v", changed, err)
	}
	if got := ModelFallbacksAt(dir); got != "homelab/a" {
		t.Fatalf("a no-op rename rewrote the row: %q", got)
	}
}

func TestRenameConnectionLeavesAnUnrelatedProfileUntouched(t *testing.T) {
	dir := t.TempDir()
	if err := writeText(dir, KeyModelFallbacks, "openai/a"); err != nil {
		t.Fatal(err)
	}
	if err := writeProfileValue(dir, "home.somewhere.else", "kept"); err != nil {
		t.Fatal(err)
	}
	before, err := readProfileConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := RenameConnectionModels(dir, "homelab", "lab")
	if err != nil {
		t.Fatal(err)
	}
	if changed != nil {
		t.Fatalf("a profile that held nothing under the old name changed rows: %v", changed)
	}
	after, err := readProfileConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a profile that held nothing under the old name was rewritten: %v -> %v", before, after)
	}
}

// ActiveConnectionFor is the pure half of the active-connection derivation:
// the surfaces hand it the model THIS conversation runs ([app.model] in the
// talk surface, the deferred target while a move waits out a working turn),
// because [ChatModelAt] is the last model ANY conversation settled on and is
// written asynchronously. A PREFIXED ID NAMES THE CUSTOM CONNECTION IT CARRIES,
// and everything else — bare ids, ids on other Written names, unknown
// prefixes — resolves through [Set.For] onto the default service, which is the
// same road the conversation itself takes. The table pins both sides: the
// identity that comes back, not just the ok.
func TestActiveConnectionForResolvesTheConversationModelThroughTheSet(t *testing.T) {
	defaultService := modelsource.Connected{
		Source: modelsource.DefaultSource("https://router.example/v1"),
		Key:    "router-key", Address: "https://router.example/v1",
	}
	customService := modelsource.Connected{
		Source: modelsource.Source{
			ID: modelsource.CustomID, Written: "homelab",
			Address: "http://127.0.0.1:9001/v1", KeyOptional: true,
		},
		Key: "homelab-key", Address: "http://127.0.0.1:9001/v1",
	}
	sources := modelsource.NewSet(defaultService, customService)
	for _, row := range []struct {
		model  string
		want   modelsource.Connected
		active bool
	}{
		// A blank model is a conversation that has settled on nothing: no
		// service is claimed, and the default never stands in.
		{"", modelsource.Connected{}, false},
		{"   ", modelsource.Connected{}, false},
		// A bare id, and a prefix no connected Written claims, answer on the
		// default service the way the conversation itself resolves them.
		{"deepseek-chat", defaultService, true},
		{"someone-else/deepseek-chat", defaultService, true},
		// The custom connection's Written prefix carries the answer.
		{"homelab/local-model", customService, true},
		{"HOMELAB/local-model", customService, true},
	} {
		got, ok := ActiveConnectionFor(row.model, sources)
		if ok != row.active {
			t.Fatalf("ActiveConnectionFor(%q) ok = %v, want %v", row.model, ok, row.active)
		}
		if got.Source.ID != row.want.Source.ID || got.Key != row.want.Key {
			t.Fatalf("ActiveConnectionFor(%q) = %q/%q, want %q/%q",
				row.model, got.Source.ID, got.Key, row.want.Source.ID, row.want.Key)
		}
	}
	// A SET WITH NO SERVICES NAMES NO SERVICE: the default never stands in for
	// an empty set, and a blank model on it stays false too.
	if service, ok := ActiveConnectionFor("deepseek-chat", modelsource.NewSet()); ok || service.Source.ID != "" {
		t.Fatalf("an empty set answered with %q, ok %v", service.Source.ID, ok)
	}
	if _, ok := ActiveConnectionFor("", modelsource.NewSet()); ok {
		t.Fatal("an empty set with a blank model answered active")
	}
}

// ActiveConnection keeps its profile-reading door on the same derivation: a
// conversation model the profile holds comes back as the service that serves
// it, and a profile that has settled on nothing answers false.
func TestActiveConnectionReadsTheProfileConversationSlot(t *testing.T) {
	dir := t.TempDir()
	defaultService := modelsource.Connected{
		Source: modelsource.DefaultSource("https://router.example/v1"),
		Key:    "router-key", Address: "https://router.example/v1",
	}
	customService := modelsource.Connected{
		Source: modelsource.Source{
			ID: modelsource.CustomID, Written: "homelab",
			Address: "http://127.0.0.1:9001/v1", KeyOptional: true,
		},
		Key: "homelab-key", Address: "http://127.0.0.1:9001/v1",
	}
	sources := modelsource.NewSet(defaultService, customService)
	if _, ok := ActiveConnection(dir, sources); ok {
		t.Fatal("a profile that settled on no model answered active")
	}
	if err := WriteChatModel(dir, "homelab/local-model"); err != nil {
		t.Fatal(err)
	}
	if service, ok := ActiveConnection(dir, sources); !ok || service.Source.ID != modelsource.CustomID {
		t.Fatalf("the profile's conversation model resolved to %q, ok %v", service.Source.ID, ok)
	}
	if _, ok := ActiveCustomSource(dir, sources); !ok {
		t.Fatal("ActiveCustomSource lost the custom connection its wrapper derives")
	}
}
