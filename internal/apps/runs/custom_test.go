package runs_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/extensions"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestCustomWorkingDaysControlsBookingWrites(t *testing.T) {
	source, err := os.ReadFile("testdata/custom-booking/working_days.wat")
	if err != nil {
		t.Fatal(err)
	}
	step := extensions.Step{Version: 1, Summary: "Count working days", Reason: "No built-in step skips weekends and supplied holidays.",
		Inputs: []extensions.Input{{Name: "from", Type: "date"}, {Name: "to", Type: "date"}, {Name: "holidays", Type: "dates"}},
		Output: "integer", Effects: []string{}, TimeoutMS: 200, MemoryPages: 1, Source: string(source)}
	bundle, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	yaml := []byte(`
run: book
params:
  day: {type: date}
steps:
  - id: working_days
    tool: custom.working_days
    args: {from: "2026-11-26", to: "$params.day", holidays: ["2026-11-27"]}
  - id: book
    if: "$steps.working_days.output >= 0 && $steps.working_days.output <= 10"
    records.create: {type: booking, set: {day: "$params.day"}}
`)
	var booked []any
	fake := runs.NewFakeConnector(nil)
	engine, err := runs.Open(runs.Config{
		Root: t.TempDir(),
		Load: func(_, _ string) (runs.Definition, error) {
			def, err := runs.Parse(yaml)
			if err != nil {
				return def, err
			}
			app := schema.App{Kit: 2}
			app.Capabilities.Tools = []string{"custom.working_days"}
			err = runs.AttachCustomTools(&def, app, func(name string) ([]byte, error) {
				if name != "custom/working_days.step" {
					t.Fatalf("unexpected tool %s", name)
				}
				return bundle, nil
			})
			return def, err
		},
		Connector: fake,
		Records: func(_ context.Context, c runs.RecordCall) (runs.Reply, error) {
			booked = append(booked, c.Set["day"])
			return runs.Reply{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	// The Friday holiday excludes it from the ten-day count ending December 11.
	for _, day := range []string{"2026-12-11", "2026-12-14", "2026-11-25"} {
		r, err := engine.Start("office", "book", map[string]any{"day": day})
		if err != nil {
			t.Fatal(err)
		}
		tick(t, engine)
		if got := state(t, engine, r.ID); got.State != runs.Done {
			t.Fatalf("%s: %s, %s", day, got.State, got.Error)
		}
	}
	if !reflect.DeepEqual(booked, []any{"2026-12-11"}) || len(fake.Calls()) != 0 {
		t.Fatalf("bookings = %v, connector calls = %d", booked, len(fake.Calls()))
	}
}
