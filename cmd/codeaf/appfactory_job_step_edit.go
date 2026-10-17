package main

// ── codeaf app: PER-JOB STEP EDITS ON AN ATTACHED FACTORY ───────────────────
//
// The engine's half of appwire.FactoryJobStepEditor. Two fixed routes, spelled
// here and never taken from the app: GET and POST /v1/jobs/{id}/step-edits. They
// edit the steps of ONE job; nothing here reads or writes a repository's
// .codeaf/factory.yaml (that is factory.pipeline_*).
//
// THE FACTORY DECIDES. Who the caller is comes from the bearer on this route and
// nowhere else: the body has no author field, and the factory forces the edit to
// be the signed-in person's. Staleness, ownership, finished or publishing jobs,
// steps that already ran and the nine-step cap are the factory's too, and its
// refusal is carried back whole.
//
// THE WRITE IS SENT ONCE. No retry, no replay, no second route. An answer that
// cannot say whether the factory acted (a dropped connection, a timeout, any
// 5xx, or a 2xx whose receipt is missing steps, lines or a version at least the
// one the edit was made from) is appwire.CodeUncertain, and the person reads the
// job's steps again before anything is sent. The one extra request after a whole
// receipt is a GET of the job; its failure, another job, or a version older than
// the receipt leaves Current out and says nothing about the edit.

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryJobStepEditor = (*appBackend)(nil)

// stepEditsPath is the one route both methods use.
func stepEditsPath(job int) string { return jobPath(job, "step-edits") }

func (b *appBackend) FactoryJobStepEditsRead(ctx context.Context, p appwire.FactoryJobStepEditsParams) (appwire.FactoryJobStepEdits, error) {
	var got api.StepEdits
	if err := b.factoryHub().read(ctx, p.Factory, stepEditsPath(p.Job), appFactoryBody, &got); err != nil {
		return appwire.FactoryJobStepEdits{}, err
	}
	return appwire.FactoryJobStepEdits{Factory: got.Factory, Job: p.Job, Version: got.Version, Steps: orSteps(got.Steps), Lines: orLines(got.Lines)}, nil
}

// FactoryJobStepEdit sends the person's edit once. The body is built field by
// field from the checked params, so a part the wire does not carry cannot
// travel. A step is placed only by `after`; there is no reorder or removal.
func (b *appBackend) FactoryJobStepEdit(ctx context.Context, p appwire.FactoryJobStepEditParams) (appwire.FactoryJobStepEdited, error) {
	body := api.StepEditsRequest{
		Ask: p.Edit.Ask, Thinking: p.Edit.Thinking, On: p.Edit.On, Skip: p.Edit.Skip, Why: p.Edit.Why, Expect: p.Expect,
	}
	for _, a := range p.Edit.Add {
		body.Add = append(body.Add, api.StepAddition{
			Step:  api.StepDef{Name: a.Step.Name, Kind: a.Step.Kind, Ask: a.Step.Ask, Until: a.Step.Until, Effort: a.Step.Effort},
			After: a.After,
		})
	}
	h := b.factoryHub()
	data, err := h.post(ctx, p.Factory, stepEditsPath(p.Job), body)
	if err != nil {
		return appwire.FactoryJobStepEdited{}, err
	}
	got, ok := decodeStepEditReceipt(data, p.Expect)
	if !ok {
		// A 2xx that is not a whole receipt cannot say whether the factory acted
		// or what it changed. It is NEVER an empty edit (empty lines mean "changed
		// nothing"), and nothing is sent again: no retry and no reread here; the
		// caller reads the steps before it decides.
		return appwire.FactoryJobStepEdited{}, appwire.Errorf(appwire.CodeUncertain, "the factory answered yes without a receipt this app can read; the edit may have happened, so read the job's steps before trying again")
	}
	out := appwire.FactoryJobStepEdited{Factory: got.Factory, Job: p.Job, Version: got.Version, Steps: orSteps(got.Steps), Lines: orLines(got.Lines)}
	var current api.Job
	// A reread that is another job, or older than the receipt, is not the job as
	// it reads after this edit, so it is left out rather than shown as current.
	if err := h.read(ctx, p.Factory, "/v1/jobs/"+strconv.Itoa(p.Job), appFactoryBody, &current); err == nil &&
		current.ID == p.Job && current.Version >= got.Version {
		out.Current = &current
	}
	return out, nil
}

// decodeStepEditReceipt reads a factory's yes. It is whole only when steps and
// lines are present (an array, or null that the factory normalized from empty)
// and version is a positive integer not below the version the edit was made
// from. An equal version is a real no-op and is accepted.
func decodeStepEditReceipt(data []byte, expect *api.Expect) (api.StepEdits, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return api.StepEdits{}, false
	}
	for _, k := range []string{"steps", "lines", "version"} {
		if _, present := fields[k]; !present {
			return api.StepEdits{}, false
		}
	}
	var got api.StepEdits
	if json.Unmarshal(data, &got) != nil || got.Version < 1 {
		return api.StepEdits{}, false
	}
	if expect != nil && expect.Version != nil && got.Version < *expect.Version {
		return api.StepEdits{}, false
	}
	return got, true
}

func orSteps(s []api.Step) []api.Step {
	if s == nil {
		return []api.Step{}
	}
	return s
}

func orLines(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}
