package pipeline

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── set_adapt, set_fixed, set_start and the labels list ─────────────────────
//
// editBase says no adapt, fixes [approve, proof], starts on [codeaf] up to size
// L and $4.

func b(v bool) *bool { return &v }

func TestInspectCarriesTheFilesSettings(t *testing.T) {
	src, _, ok := Inspect(editBase)
	if !ok {
		t.Fatal("editBase does not inspect")
	}
	if src.Adapt != "" || !reflect.DeepEqual(src.Labels, []string{"codeaf"}) || src.MaxSize != "L" || src.MaxCost != 4 || !reflect.DeepEqual(src.Fixed, []string{"approve", "proof"}) {
		t.Fatalf("%+v", src)
	}
	// The shipped defaults say what a repository with no file starts under.
	d := InspectDefaults()
	if d.MaxSize != DefaultMaxSize || d.MaxCost != DefaultMaxCost || d.Labels == nil {
		t.Fatalf("defaults %+v", d)
	}
}

func TestSettingsEditsSetAdaptRequiredStepsAndStartLimits(t *testing.T) {
	got := mustApply(t, editBase,
		EditOp{Op: OpSetAdapt, Adapt: s("ask")},
		EditOp{Op: OpSetFixed, Name: "test", Fixed: b(true)},
		EditOp{Op: OpSetFixed, Name: "approve", Fixed: b(false)},
		EditOp{Op: OpSetStart, MaxSize: s("m"), MaxCost: f(12.5)},
		setList(ListLabels, "codeaf", "agent-ready"))
	if !got.Changed || !got.SettingsEdited || !got.Listed || got.Structural || len(got.Steps) != 0 {
		t.Fatalf("%+v", got)
	}
	back := reread(t, got.Text)
	if back.Adapt != factory.AdaptAsk || !reflect.DeepEqual(back.Fixed, []string{"proof", "test"}) || back.Start.MaxSize != "M" || back.Start.MaxCost != 12.5 ||
		!reflect.DeepEqual(back.Start.Labels, []string{"codeaf", "agent-ready"}) {
		t.Fatalf("read back adapt %q fixed %q size %q cost %v labels %q", back.Adapt, back.Fixed, back.Start.MaxSize, back.Start.MaxCost, back.Start.Labels)
	}
	// A REQUIRED STEP IS ONE THE RUNNER HOLDS FIXED.
	for _, st := range back.Recipe().For(factory.KindIssue) {
		if want := st.Name == "test" || st.Name == "proof"; st.Fixed != want {
			t.Fatalf("%s fixed %v", st.Name, st.Fixed)
		}
	}
	wantSettings := []SettingChange{
		{Setting: SettingAdapt, Before: "adapt", After: "ask"},
		{Setting: SettingFixed, Before: "approve, proof", After: "proof, test"},
		{Setting: SettingMaxSize, Before: "L", After: "M"},
		{Setting: SettingMaxCost, Before: "4", After: "12.5"},
	}
	if !reflect.DeepEqual(got.Settings, wantSettings) {
		t.Fatalf("settings %+v", got.Settings)
	}
	if !reflect.DeepEqual(got.Lists, []ListChange{{List: ListLabels, Before: []string{"codeaf"}, After: []string{"codeaf", "agent-ready"}}}) {
		t.Fatalf("lists %+v", got.Lists)
	}

	// ADAPT BACK TO ITS DEFAULT IS NO KEY, and a size of "" is the default M.
	again := mustApply(t, got.Text, EditOp{Op: OpSetAdapt, Adapt: s("adapt")}, EditOp{Op: OpSetStart, MaxSize: s("")})
	if strings.Contains(again.Text, "adapt:") || reread(t, again.Text).Start.MaxSize != DefaultMaxSize {
		t.Fatalf("defaults written back:\n%s", again.Text)
	}
	// A setting set to what it is changes nothing.
	if same := mustApply(t, editBase, EditOp{Op: OpSetStart, MaxCost: f(4)}); same.Changed || !same.SettingsEdited || len(same.Settings) != 0 {
		t.Fatalf("an unchanged setting: %+v", same)
	}
}

func TestSettingsEditRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		ops  []EditOp
		code string
	}{
		"an adapt word that is not one":     {[]EditOp{{Op: OpSetAdapt, Adapt: s("never")}}, EditBadValue},
		"set_adapt with nothing":            {[]EditOp{{Op: OpSetAdapt}}, EditBadValue},
		"proof made optional":               {[]EditOp{{Op: OpSetFixed, Name: "proof", Fixed: b(false)}}, EditLocked},
		"a step no pipeline runs":           {[]EditOp{{Op: OpSetFixed, Name: "nosuch", Fixed: b(true)}}, EditUnknownStep},
		"set_fixed without fixed":           {[]EditOp{{Op: OpSetFixed, Name: "test"}}, EditBadValue},
		"a size that is not one":            {[]EditOp{{Op: OpSetStart, MaxSize: s("huge")}}, EditBadValue},
		"a negative cost":                   {[]EditOp{{Op: OpSetStart, MaxCost: f(-1)}}, EditBadValue},
		"set_start with nothing":            {[]EditOp{{Op: OpSetStart}}, EditBadValue},
		"a step edit that names adapt":      {[]EditOp{{Op: OpSetStep, Name: "test", Prompt: s("x"), Adapt: s("ask")}}, EditBadOp},
		"a settings edit with a prompt":     {[]EditOp{{Op: OpSetAdapt, Adapt: s("ask"), Prompt: s("x")}}, EditBadOp},
		"adapt set twice":                   {[]EditOp{{Op: OpSetAdapt, Adapt: s("ask")}, {Op: OpSetAdapt, Adapt: s("fixed")}}, EditBadOp},
		"one step marked twice":             {[]EditOp{{Op: OpSetFixed, Name: "test", Fixed: b(true)}, {Op: OpSetFixed, Name: "test", Fixed: b(false)}}, EditBadOp},
		"a labels list that repeats a name": {[]EditOp{setList(ListLabels, "codeaf", "CodeAF")}, EditBadValue},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ApplyEdits(editBase, tc.ops)
			bad, ok := err.(*EditError)
			if !ok || bad.Code != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}
	// Two steps may each be marked once.
	mustApply(t, editBase, EditOp{Op: OpSetFixed, Name: "test", Fixed: b(true)}, EditOp{Op: OpSetFixed, Name: "write", Fixed: b(true)})
}

// A REPOSITORY WITH NO FILE GETS THE SETTING IT ASKED FOR AND THE DEFAULTS FOR
// THE REST, as every other edit of an empty base does.
func TestASettingOnAnEmptyBaseWritesTheDefaultsWithIt(t *testing.T) {
	got := mustApply(t, "", EditOp{Op: OpSetAdapt, Adapt: s("fixed")})
	back := reread(t, got.Text)
	if back.Adapt != factory.AdaptFixed || back.Start.MaxCost != DefaultMaxCost || len(back.Recipe().For(factory.KindIssue)) != len(Defaults().Recipe().For(factory.KindIssue)) {
		t.Fatalf("%+v", back)
	}
}
