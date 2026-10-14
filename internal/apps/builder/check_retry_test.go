package builder

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/sample"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type failingSample struct {
	svc.Sample
	calls int
}

func (s *failingSample) Generate(ctx context.Context, b *spec.Bundle) (map[string][]bridge.Record, error) {
	s.calls++
	if s.calls == 1 {
		return nil, errors.New("this_week: sample evaluation failed")
	}
	return s.Sample.Generate(ctx, b)
}

type failingCheck struct{ calls int }

func (c *failingCheck) Run(context.Context, string, spec.Files, int) (bridge.CheckReport, error) {
	c.calls++
	if c.calls == 1 {
		return bridge.CheckReport{}, errors.New("render failed")
	}
	return bridge.CheckReport{Pass: true}, nil
}

func TestLateCheckFailureGetsFixRoundTwo(t *testing.T) {
	for _, stage := range []string{"sample", "render", "service"} {
		t.Run(stage, func(t *testing.T) {
			r := newRig(t)
			r.model.write = func(prompt string) string {
				return strings.ReplaceAll(goldenFiles(onlyPaths(prompt)), "records.update, gmail", "records.update, records.delete, gmail")
			}
			generator := sample.New(svc.Config{}, nil)
			all := &svc.Services{Sample: generator}
			switch stage {
			case "sample":
				all.Sample = &failingSample{Sample: generator}
				r.svc.all.Check = check.New(svc.Config{}, all)
			case "render":
				r.svc.all.Check = check.New(svc.Config{}, all)
				r.model.write = func(prompt string) string {
					reply := strings.ReplaceAll(goldenFiles(onlyPaths(prompt)), "records.update, gmail", "records.update, records.delete, gmail")
					// JSON output contains an escaped expression string.
					return strings.ReplaceAll(reply, "$selection.contacts.count == 1", "$selection.contacts.count == 1 / 0")
				}
			case "service":
				r.svc.all.Check = &failingCheck{}
			}
			r.model.fixes = []string{goldenFiles([]string{"screens/contacts.json"})}
			r.create(t, true)
			waitFor(t, "build completion", func() bool {
				state, _ := r.store.current()
				return state == bridge.StatePreview || state == bridge.StateFailed
			})
			if state, _ := r.store.current(); state != bridge.StatePreview {
				t.Fatalf("state %s events %+v", state, r.hub.events())
			}
			if r.model.calls["fix"] != 1 {
				t.Fatalf("FIX calls %d", r.model.calls["fix"])
			}
			var fix string
			for _, p := range r.model.prompts {
				if callKind(p) == "fix" {
					fix = p
				}
			}
			if !strings.Contains(fix, "fix:") || !strings.Contains(fix, "----- app.json -----") {
				t.Fatalf("FIX lacks hints or definition: %s", fix)
			}
			var passed bool
			for _, ev := range r.hub.events() {
				if ev.Step == bridge.StepCheck && ev.Round == 2 && ev.Check != nil && ev.Check.Pass {
					passed = true
				}
			}
			if !passed {
				t.Fatal("Check round 2 did not pass")
			}
		})
	}
}

type alwaysErrorCheck struct{}

func (alwaysErrorCheck) Run(context.Context, string, spec.Files, int) (bridge.CheckReport, error) {
	return bridge.CheckReport{}, errors.New("sample evaluation failed")
}
func TestCheckErrorsUseAllFixRounds(t *testing.T) {
	r := newRig(t)
	r.svc.all.Check = alwaysErrorCheck{}
	r.model.fixes = []string{goldenFiles([]string{"screens/contacts.json"})}
	r.create(t, true)
	r.waitState(t, bridge.StateFailed)
	if r.model.calls["fix"] != 2 {
		t.Fatalf("FIX calls %d", r.model.calls["fix"])
	}
	last := 0
	for _, event := range r.hub.events() {
		if event.Step == bridge.StepCheck {
			last = max(last, event.Round)
		}
	}
	if last != 3 {
		t.Fatalf("last Check round %d", last)
	}
}

func TestComputedErrorReachesFixWithSourceAndExample(t *testing.T) {
	r := newRig(t)
	r.svc.all.Check = check.New(svc.Config{}, &svc.Services{Sample: sample.New(svc.Config{}, nil)})

	files, _ := spec.GoldenCRM()
	var app spec.App
	if err := json.Unmarshal(files[spec.AppFile], &app); err != nil {
		t.Fatal(err)
	}
	app.Records["deal"].Fields["this_week"] = &spec.Field{Type: spec.FieldBool, Computed: "now - 7d + missing"}
	raw, err := json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	files[spec.AppFile] = raw
	files[spec.AgentFile] = []byte(strings.ReplaceAll(string(files[spec.AgentFile]), "records.update, gmail", "records.update, records.delete, gmail"))
	r.model.write = func(prompt string) string {
		text := map[string]string{}
		for _, name := range onlyPaths(prompt) {
			text[name] = string(files[name])
		}
		raw, _ := json.Marshal(text)
		return string(raw)
	}

	r.model.fixes = []string{goldenFiles([]string{spec.AppFile})}
	r.create(t, true)
	r.waitState(t, bridge.StatePreview)
	var fix string
	for _, p := range r.model.prompts {
		if callKind(p) == "fix" {
			fix = p
		}
	}
	for _, needle := range []string{"this_week", "now - 7d + missing", "1:12", "corrected example", "days(7)", "/records/deal/fields/this_week/computed"} {
		if !strings.Contains(fix, needle) {
			t.Errorf("FIX lacks %q", needle)
		}
	}
}
