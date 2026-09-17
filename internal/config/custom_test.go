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
