//go:build e2e

package evals

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestLiveExtensionBuilder(t *testing.T) {
	key := evalKey()
	if !strings.HasPrefix(key, "sk-or-v1-") {
		t.Skip("the profile did not resolve an OpenRouter key")
	}
	binary := os.Getenv("CODEAF_APP_EVAL_BINARY")
	if binary == "" {
		t.Skip("set CODEAF_APP_EVAL_BINARY to the built app engine")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	engine, err := launch(ctx, binary, t.TempDir(), key, builderModel)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	build := func(prompt string) BuildState {
		t.Helper()
		body, err := engine.cli(ctx, "plan", map[string]string{"ask": prompt})
		plan, err := readReply[draftReply](body, err)
		if err != nil || plan.Draft == "" {
			t.Fatalf("plan: %v", err)
		}
		params := map[string]string{"draft": plan.Draft}
		var out BuildState
		for range 3 {
			if _, err := engine.cli(ctx, "build", params); err != nil {
				t.Fatal(err)
			}
			out, err = engine.waitCheck(ctx, params)
			if err == nil || !strings.Contains(err.Error(), "ran out of model calls") {
				break
			}
			t.Log("Continue the same draft after the builder used its turn budget.")
		}
		if err != nil || !out.Checked {
			status, _ := engine.cli(ctx, "build_state", params)
			var detail struct {
				Progress []struct {
					Report schema.Report
				}
			}
			_ = json.Unmarshal(status, &detail)
			for _, event := range detail.Progress {
				if !event.Report.OK() {
					t.Log(event.Report.String())
				}
			}
			t.Fatalf("build failed after %d Check rounds: %v", out.Rounds, err)
		}
		applied, err := readReply[struct {
			Meta struct{ ID string }
		}](engine.wire(ctx, "apps.apply", params))
		if err != nil || applied.Meta.ID == "" {
			t.Fatalf("apply: %v", err)
		}
		out.App = applied.Meta.ID
		return out
	}

	parts := func(app string) []schema.Part {
		t.Helper()
		def, err := readReply[struct{ Screens []string }](engine.wire(ctx, "apps.get", map[string]string{"app": app}))
		if err != nil {
			t.Fatal(err)
		}
		var parts []schema.Part
		for _, screen := range def.Screens {
			got, err := readReply[struct {
				Programs map[string]schema.PartProgram `json:"custom_parts"`
			}](engine.wire(ctx, "apps.screen", map[string]string{"app": app, "screen": screen}))
			if err != nil {
				t.Fatal(err)
			}
			for _, program := range got.Programs {
				parts = append(parts, program.Manifest)
			}
		}
		return parts
	}

	first := build("A print shop needs tactile Braille label proof. Render each Braille cell as a physical proof diagram with all six dot positions, raised dots filled and flat dots faint. Ordinary Unicode text does not show the flat dot positions, so use a custom part with dot-cells (see reference custom-extensions) and explain why it is needed. Bind it to label records and let me approve a label. Use synthetic examples.")
	var part schema.Part
	for _, candidate := range parts(first.App) {
		if candidate.Reason != "" {
			part = candidate
			break
		}
	}
	if part.Name == "" {
		t.Fatal("the builder did not declare and use a custom part")
	}
	t.Logf("The print shop built %s v%d because %s", part.Name, part.Version, part.Reason)
	id := strings.TrimPrefix(part.Name, "custom/")
	promoted, err := readReply[schema.Part](engine.wire(ctx, "apps.promote_part", map[string]string{"app": first.App, "id": id}))
	if err != nil || promoted.Origin == nil || promoted.Origin.App != first.App {
		t.Fatalf("promotion lineage = %+v, %v", promoted.Origin, err)
	}

	second := build("Facilities signage: show tactile Braille proof for building signs. Reuse the exact " + part.Name + " part from my Kit. Bind it to new synthetic sign records and let me approve them.")
	reused := false
	for _, other := range parts(second.App) {
		if other.Name == part.Name && other.Version == part.Version && other.Origin != nil && *other.Origin == *promoted.Origin {
			reused = true
		}
	}
	if !reused {
		t.Fatal("the facilities app did not preserve the promoted part's lineage")
	}
	t.Logf("Facilities reused %s from version %d of %s.", part.Name, promoted.Origin.Version, first.App)

	booking := build("Desk booking. Book an office desk for a date only if it is from today through 10 working days ahead. Use set-custom-step (see reference custom-extensions) to author a WebAssembly step that counts weekdays between two dates excluding holiday dates and give it a clear reason. The counted range excludes today and includes the requested booking date. Use an Actions button to run the booking automation and examples to illustrate the data.")
	got, err := engine.wire(ctx, "apps.get", map[string]string{"app": booking.App})
	var def struct{ Runs []string }
	if err != nil || json.Unmarshal(got, &def) != nil || len(def.Runs) == 0 {
		t.Fatalf("booking app needs an automation: %v", err)
	}
	t.Logf("Desk booking published %d automation(s).", len(def.Runs))
}
