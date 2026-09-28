package config

import (
	"reflect"
	"strings"
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

// EVERY ADD MINTS AN ID THE PROFILE DOES NOT ALREADY HOLD, which is the whole
// reason two custom connections can coexist: persistConnectedSource matches a
// row BY ID, so a mint that answered the taken id a second time would replace
// the first connection instead of adding one, and the feature's own acceptance
// ("connect a second custom service; both coexist") would break with nothing
// failing. The first instance keeps the vendored id so profiles written before
// instances existed read back unchanged; later ones take custom-<name>, and
// the numeric tiebreak carries on from there when that is taken too.
func TestPrepareCustomSourceMintsAFreeIdForEveryAdd(t *testing.T) {
	for _, probe := range []struct {
		name    string
		held    []PersistedSource
		written string
		want    string
	}{{
		name:    "an empty profile keeps the vendored id",
		written: "homelab",
		want:    modelsource.CustomID,
	}, {
		name: "a profile already holding custom mints under the name",
		held: []PersistedSource{
			{ID: modelsource.CustomID, Written: "mybox", Address: "http://127.0.0.1:9001/v1", Order: 1},
		},
		written: "homelab",
		want:    modelsource.CustomID + "-homelab",
	}, {
		name: "the same name again takes the numeric tiebreak",
		held: []PersistedSource{
			{ID: modelsource.CustomID, Written: "mybox", Address: "http://127.0.0.1:9001/v1", Order: 1},
			{ID: modelsource.CustomID + "-homelab", Written: "homelab", Address: "http://127.0.0.1:9002/v1", Order: 2},
		},
		written: "homelab",
		want:    modelsource.CustomID + "-homelab-2",
	}, {
		name: "and carries on past the first tiebreak",
		held: []PersistedSource{
			{ID: modelsource.CustomID, Written: "mybox", Address: "http://127.0.0.1:9001/v1", Order: 1},
			{ID: modelsource.CustomID + "-homelab", Written: "homelab", Address: "http://127.0.0.1:9002/v1", Order: 2},
			{ID: modelsource.CustomID + "-homelab-2", Written: "homelab", Address: "http://127.0.0.1:9003/v1", Order: 3},
		},
		written: "homelab",
		want:    modelsource.CustomID + "-homelab-3",
	}, {
		name: "a written name with spaces and punctuation mints the plain word",
		held: []PersistedSource{
			{ID: modelsource.CustomID, Written: "mybox", Address: "http://127.0.0.1:9001/v1", Order: 1},
		},
		written: "My Lab!",
		want:    modelsource.CustomID + "-my-lab",
	}, {
		name: "a non-ASCII name mints the fallback word",
		held: []PersistedSource{
			{ID: modelsource.CustomID, Written: "mybox", Address: "http://127.0.0.1:9001/v1", Order: 1},
		},
		written: "\u7814\u7a76",
		want:    modelsource.CustomID + "-connection",
	}, {
		name: "the fallback word takes the numeric tiebreak on a repeat",
		held: []PersistedSource{
			{ID: modelsource.CustomID, Written: "mybox", Address: "http://127.0.0.1:9001/v1", Order: 1},
			{ID: modelsource.CustomID + "-connection", Written: "\u7814\u7a76", Address: "http://127.0.0.1:9002/v1", Order: 2},
		},
		written: "\u7814\u7a76",
		want:    modelsource.CustomID + "-connection-2",
	}} {
		t.Run(probe.name, func(t *testing.T) {
			dir := t.TempDir()
			if len(probe.held) > 0 {
				if err := WriteSources(dir, probe.held); err != nil {
					t.Fatal(err)
				}
			}
			row := PrepareCustomSource(dir, "http://127.0.0.1:9100/v1", probe.written)
			if row.ID != probe.want {
				t.Fatalf("the mint answered %q, want %q", row.ID, probe.want)
			}
			// THE MINTED ID IS FREE: an id already on a row would replace that
			// connection at persist time rather than add one.
			for _, taken := range probe.held {
				if strings.EqualFold(taken.ID, row.ID) {
					t.Fatalf("the mint answered %q, which the profile already holds", row.ID)
				}
			}
			// The name is the routing prefix and is untouched by the tiebreak,
			// which is persistence vocabulary only; the row sorts after the
			// ones already held.
			if row.Written != probe.written {
				t.Fatalf("the mint moved the name to %q", row.Written)
			}
			if row.Order != len(probe.held)+1 {
				t.Fatalf("the mint gave order %d on a profile holding %d rows", row.Order, len(probe.held))
			}
		})
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
	// The checker row is a crew seat, and a value the tier gate refuses is not
	// a pin it runs — so the row is read as it stands on disk.
	if got, _ := persistedString(dir, KeyTierHighModel); got != "homelab/a:mid" {
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

// A PADDED ROW THE RENAME DOES NOT MOVE IS A ROW IT DOES NOT REWRITE.
// RenameConnectionModels trims a tier row's value to COMPARE it, so a stored
// row with surrounding whitespace that does not carry the old name must come
// back byte-identical and unreported — not rewritten to its trimmed spelling.
// The seed goes through writeProfileValues, the one writer that does not
// trim: writeTierModel and writeText both trim on the way in, so no test
// seeding through them can produce the row this pins.
func TestRenameConnectionLeavesAPaddedRowThatDoesNotCarryTheOldNameAlone(t *testing.T) {
	dir := t.TempDir()
	if err := writeProfileValues(dir, map[string]any{
		KeyTierHighModel:   "  openai/a  ",
		KeyTierWorkerModel: "  homelab/b  ",
	}); err != nil {
		t.Fatal(err)
	}

	changed, err := RenameConnectionModels(dir, "homelab", "lab")
	if err != nil {
		t.Fatal(err)
	}
	// Only the row under the old name moved; the padded row under another
	// connection's name is absent from the report.
	if want := []string{KeyTierWorkerModel}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("the rename named the wrong keys: %v", changed)
	}
	if got, held := persistedString(dir, KeyTierHighModel); !held || got != "  openai/a  " {
		t.Fatalf("the padded row on another connection was rewritten: %q", got)
	}
	// The row under the old name moved, and landed trimmed under the new one.
	if got, _ := persistedString(dir, KeyTierWorkerModel); got != "lab/b" {
		t.Fatalf("the padded row under the old name did not land trimmed under the new one: %q", got)
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

// A CONNECTION'S NAME IS ROUTING VOCABULARY, AND THE ROUTING IS THE SLASH:
// nothing else about a name is special. Punctuation, non-ASCII letters and a
// long name all persist as typed, and the model ids the connection qualifies
// (Written + "/" + bare) cut back to the same connection and the same bare
// model through Set.For — never onto the default service. A name that broke
// here would strand every pick on it.
func TestConnectionNamesWithPunctuationUnicodeAndLengthRouteBackToTheirConnection(t *testing.T) {
	defaultService := modelsource.Connected{
		Source: modelsource.DefaultSource("https://router.example/v1"),
		Key:    "router-key", Address: "https://router.example/v1",
	}
	names := []string{
		"my.box", "box:2", "me@home", "gpu%1", "münchen",
		strings.Repeat("x", 64),
	}
	for _, name := range names {
		row := PrepareCustomSource(t.TempDir(), "http://127.0.0.1:9000/v1", name)
		if row.Written != name {
			t.Fatalf("the name %q was kept as %q", name, row.Written)
		}
		if !modelsource.IsCustomID(row.ID) {
			t.Fatalf("the name %q minted the id %q, which is not a custom connection id", name, row.ID)
		}
		connected := modelsource.Connected{
			Source:  modelsource.Source{ID: row.ID, Written: row.Written, Address: row.Address},
			Address: row.Address,
		}
		qualified := connected.Qualify("glm-5.3")
		if qualified != name+"/glm-5.3" {
			t.Fatalf("Qualify spelled %q instead of %q", qualified, name+"/glm-5.3")
		}
		service, bare := modelsource.NewSet(defaultService, connected).For(qualified)
		if service.Source.ID != row.ID {
			t.Fatalf("the id %q routed to %q, not to its own connection %q", qualified, service.Source.ID, row.ID)
		}
		if bare != "glm-5.3" {
			t.Fatalf("the id %q stripped to %q instead of glm-5.3", qualified, bare)
		}
	}
}
