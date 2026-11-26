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

	"github.com/Agent-Field/codeaf/internal/apps/extensions"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
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

	day := time.Now().UTC()
	holiday := advanceWorkdays(day, 1, "")
	booking := build("Desk booking. Book an office desk for a date only if it is from today through 10 working days ahead. " +
		"Use set-custom-step (see reference custom-extensions) to author a WebAssembly step counting weekdays from two dates excluding holidays. " +
		"Use the UTC current day and include holiday " + holiday + ". Exclude today and include the requested date. " +
		"Name the run book, give it the date parameter day, and write an accepted booking to type booking. " +
		"An unavailable date must never create a booking. Wire the run to a visible Book Desk action. Use fictional examples.")
	bookingDef, err := store.NewAt(engine.home, nil).Get(booking.App)
	if err != nil {
		t.Fatal(err)
	}
	rawRun, exists := bookingDef.Run("book")
	if !exists {
		t.Fatal("the booking app has no book automation")
	}
	flow, err := runs.Parse(rawRun)
	if err != nil {
		t.Fatal(err)
	}
	var custom bool
	for _, step := range flow.Steps {
		if _, ok := extensions.ToolID(step.Tool); ok {
			custom = true
		}
	}
	if !custom {
		t.Fatal("the booking automation must use a custom tool")
	}
	query := func() int {
		t.Helper()
		page, err := readReply[struct{ Total int }](engine.wire(ctx, "data.query", map[string]string{"app": booking.App, "type": "booking"}))
		if err != nil {
			t.Fatal(err)
		}
		return page.Total
	}
	for _, scenario := range []struct {
		day string
		add int
	}{
		{advanceWorkdays(day, 10, holiday), 1},
		{advanceWorkdays(day, 11, holiday), 0},
	} {
		before := query()
		started, err := readReply[struct{ ID string }](engine.wire(ctx, "runs.start", map[string]any{
			"app": booking.App, "run": "book", "params": map[string]string{"day": scenario.day},
		}))
		if err != nil {
			t.Fatal(err)
		}
		waitRun, cancel := context.WithTimeout(ctx, 15*time.Second)
		for {
			run, err := readReply[runs.Run](engine.wire(waitRun, "runs.get", map[string]string{"app": booking.App, "id": started.ID}))
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			if run.State == runs.Done || run.State == runs.Incomplete {
				if scenario.add == 1 && run.State != runs.Done {
					cancel()
					t.Fatalf("the ten-day booking failed: %s", run.Error)
				}
				break
			}
			time.Sleep(125 * time.Millisecond)
		}
		cancel()
		if after := query(); after-before != scenario.add {
			t.Fatalf("booking on %s wrote %d new bookings, want %d", scenario.day, after-before, scenario.add)
		}
	}
	t.Log("The authored booking run accepted day 10 and refused day 11, excluding the test holiday.")
}

func advanceWorkdays(start time.Time, n int, holiday string) string {
	for n > 0 {
		start = start.AddDate(0, 0, 1)
		if start.Weekday() != time.Saturday && start.Weekday() != time.Sunday && start.Format(time.DateOnly) != holiday {
			n--
		}
	}
	return start.Format(time.DateOnly)
}
