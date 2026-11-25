// Package check exercises definitions on sample records and fake connectors.
package check

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// RunProblem carries a step failure from an executor using generated samples
// and fake replies. Engine or filesystem errors omit this detail.
type RunProblem struct{ Detail string }

func (p *RunProblem) Error() string { return p.Detail }

// Pipeline validates the same files the kit and the store will open, then
// exercises their behavior in isolated sample storage.
type Pipeline struct {
	Look   func(context.Context, string, string) ([]string, error)
	Seed   func(draft string, app []byte, rows map[string][]map[string]any) error
	DryRun func(ctx context.Context, draft, run string, def schema.Definition) error
}

func (p Pipeline) Run(ctx context.Context, draft string, def schema.Definition, types schema.TypeSet) schema.Report {
	report := schema.Validate(def, types)
	if !report.OK() {
		return report
	}
	if len(def.ScreenFiles()) == 0 {
		report.Problems = append(report.Problems, problem(schema.ScreensDir, "screen_required", "Add a screen so the app can be tried."))
		return report
	}
	rows, err := Sample(def)
	if err != nil || len(rows) == 0 {
		report.Problems = append(report.Problems, problem(schema.SampleFile, "sample_invalid", "Add example records for the app's lists and required fields."))
		return report
	}
	if p.Seed == nil {
		report.Problems = append(report.Problems, problem(schema.SampleFile, "sample_unavailable", "The example data store is unavailable."))
		return report
	}
	if err := p.Seed(draft, def.Files[schema.AppFile], rows); err != nil {
		// Seeding only consumes generated records. A backend error can
		// still carry an engine path or catalog details, so report the
		// issue without including its internal error text in model input.
		report.Problems = append(report.Problems, problem(schema.SampleFile, "sample_seed", "The example rows could not be loaded. Check required fields, IDs, references, values and types against app.json."))
		return report
	}
	for i, file := range def.RunFiles() {
		if ctx.Err() != nil {
			break
		}
		if p.DryRun == nil {
			report.Problems = append(report.Problems, problem(file, "run_check_unavailable", "Automations can't be tried on this engine yet."))
			continue
		}
		if i > 0 && p.Seed(draft, def.Files[schema.AppFile], rows) != nil {
			report.Problems = append(report.Problems, problem(file, "sample_seed", "The example rows could not be reset for this automation."))
			break
		}
		if err := p.DryRun(ctx, draft, schema.FileID(file), def); err != nil {
			message := "The automation stopped when tried on example records. Check its steps, queries, parameters and references."
			var runProblem *RunProblem
			if errors.As(err, &runProblem) {
				message = runProblem.Detail
			}
			report.Problems = append(report.Problems, problem(file, "run_failed", message))
		}
	}
	if report.OK() && p.Look != nil {
		for _, file := range def.ScreenFiles() {
			problems, err := p.Look(ctx, draft, schema.FileID(file))
			if err != nil {
				problems = append(problems, "The desktop could not draw this screen.")
			}
			for _, message := range problems {
				report.Problems = append(report.Problems, problem(file, "screen_layout", message))
			}
		}
	}
	return report
}

func problem(file, code, message string) schema.Problem {
	return schema.Problem{File: file, Code: code, Message: message}
}

// Sample reads only the fictional examples supplied with the definition.
// Preview can seed an empty store while the author is still adding examples.
func Sample(def schema.Definition) (map[string][]map[string]any, error) {
	raw := def.Files[schema.SampleFile]
	if len(raw) == 0 {
		return nil, nil
	}
	var sample schema.Sample
	if err := json.Unmarshal(raw, &sample); err != nil {
		return nil, err
	}
	return sample.Records, nil
}
