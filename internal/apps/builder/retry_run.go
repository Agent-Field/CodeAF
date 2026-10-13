package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// RetryRun keeps the working app and gives WRITE/FIX only the selected run.
func (s *Service) RetryRun(ctx context.Context, scope svc.Scope, file string) error {
	b, err := s.buildFor(ctx, scope.App)
	if err != nil {
		return err
	}
	detail, err := s.all.Store.Get(ctx, scope.App)
	if err != nil {
		return err
	}
	if detail.State != bridge.StatePreview && detail.State != bridge.StateFailed {
		return bridge.Errorf(bridge.CodeConflict, "an automation can be fixed after the build finishes")
	}
	found := false
	for _, item := range b.result().LeftOut {
		if item.File == file {
			found = true
		}
	}
	if !found {
		return bridge.Errorf(bridge.CodeInvalid, "name a left-out automation file")
	}
	files := spec.Files{}
	for name, text := range detail.Files {
		files[name] = []byte(text)
	}
	return s.start(b, func() error { return s.setState(ctx, b, bridge.StateBuilding) }, func(ctx context.Context) error {
		return s.retryRun(ctx, b, files, file)
	})
}

func (s *Service) retryRun(ctx context.Context, b *build, files spec.Files, file string) error {
	plan, err := retryPlan(b.currentPlan(), b.result(), file)
	if err != nil {
		return err
	}
	planRaw, err := planJSON(plan)
	if err != nil {
		return err
	}
	changed, err := s.writeUnit(ctx, b, plan, string(planRaw), writeUnit{
		only: []string{file}, already: asText(files), schemas: schemaFor(spec.RunSchemaURL),
	})
	if err != nil {
		return s.failWrite(ctx, b, err)
	}
	next, err := retryFiles(files, b.result(), file)
	if err != nil {
		return err
	}
	merge(next, changed)
	if err := s.all.Store.WriteFiles(ctx, b.app, next); err != nil {
		return err
	}
	s.announce(b, changed, "Wrote "+file)
	return s.checkAndFix(ctx, b, next, file)
}

func retryFiles(files spec.Files, saved savedResult, file string) (spec.Files, error) {
	remaining := withoutRun(saved.LeftOut, file)
	base, err := omitRuns(saved.Original, saved.LeftOut)
	if err != nil {
		return nil, err
	}
	restored, err := omitRuns(saved.Original, remaining)
	if err != nil {
		return nil, err
	}
	next := spec.Files{}
	merge(next, files)
	// Restore references only where the owner has not changed the reduced file.
	for name, raw := range restored {
		if name == spec.AppFile || name == spec.AgentFile || strings.HasPrefix(name, "screens/") {
			if bytes.Equal(files[name], base[name]) {
				next[name] = raw
			}
		}
	}
	var plan spec.Plan
	if err := json.Unmarshal(files[spec.PlanFile], &plan); err != nil {
		return nil, err
	}
	var original spec.Plan
	if err := json.Unmarshal(saved.Original[spec.PlanFile], &original); err != nil {
		return nil, err
	}
	id := strings.TrimSuffix(path.Base(file), ".yaml")
	present := false
	for _, line := range plan.Automation {
		if line.ID == id {
			present = true
		}
	}
	for _, line := range original.Automation {
		if line.ID == id && !present {
			plan.Automation = append(plan.Automation, line)
		}
	}
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, err
	}
	next[spec.PlanFile] = append(raw, '\n')
	return next, nil
}

func withoutRun(items []LeftOutAutomation, file string) []LeftOutAutomation {
	result := []LeftOutAutomation{}
	for _, item := range items {
		if item.File != file {
			result = append(result, item)
		}
	}
	return result
}

func (s *Service) completeRetry(b *build, file string) error {
	result := b.result()
	result.LeftOut = withoutRun(result.LeftOut, file)
	if len(result.LeftOut) == 0 {
		result.Original = nil
	}
	return s.saveResult(b, result)
}

func retryPlan(plan prompt.Plan, saved savedResult, file string) (prompt.Plan, error) {
	id := strings.TrimSuffix(path.Base(file), ".yaml")
	for _, line := range plan.Automation {
		if line.ID == id {
			return plan, nil
		}
	}
	original, err := decodePlan(string(saved.Original[spec.PlanFile]))
	if err != nil {
		return plan, err
	}
	for _, line := range original.Automation {
		if line.ID == id {
			plan.Automation = append(plan.Automation, line)
		}
	}
	return plan, nil
}
