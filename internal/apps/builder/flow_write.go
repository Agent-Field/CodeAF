package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// writeWorkers is how many screen and run files are written at once.
const writeWorkers = 6

// writeAndCheck is Write: the files, then Check with up to MaxFixRounds fixes,
// then Preview or failed.
func (s *Service) writeAndCheck(ctx context.Context, b *build) error {
	if err := s.saveResult(b, savedResult{}); err != nil {
		return err
	}
	files, err := s.writeAll(ctx, b)
	if err != nil {
		return s.failWrite(ctx, b, err)
	}
	return s.checkAndFix(ctx, b, files, "")
}

func (s *Service) checkAndFix(ctx context.Context, b *build, files spec.Files, only string) error {
	loop := prompt.NewLoop()
	_ = loop.PlanWritten()
	_ = loop.FilesWritten()
	for round := 1; ; round++ {
		report, err := s.checkRound(ctx, b, files, round)
		if err != nil {
			return err
		}
		phase, _ := loop.CheckResult(report.Pass, formatReport(report))
		switch phase {
		case prompt.PhasePreview:
			if only != "" {
				if err := s.completeRetry(b, only); err != nil {
					return err
				}
			}
			return s.preview(ctx, b, files)
		case prompt.PhaseCheckFailed:
			return s.runFallback(ctx, b, files, report)
		}
		if files, err = s.fixOnly(ctx, b, files, loop.LastReport(), only); err != nil {
			return s.failWrite(ctx, b, err)
		}
		_ = loop.FixApplied()
	}
}

func (s *Service) failWrite(ctx context.Context, b *build, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.emit(b, bridge.BuildEvent{Step: bridge.StepWrite, Status: bridge.BuildFailed, Log: err.Error()})
	return s.setState(ctx, b, bridge.StateFailed)
}

func (s *Service) giveUp(ctx context.Context, b *build, report bridge.CheckReport) error {
	s.emit(b, bridge.BuildEvent{Step: bridge.StepCheck, Status: bridge.BuildFailed, Round: report.Round,
		Log: "The checks did not pass after " + fmt.Sprint(report.Round) + " rounds", Check: &report})
	return s.setState(ctx, b, bridge.StateFailed)
}

// writeAll writes the app: app.json and agent.md first, because every screen
// and run is written against the record types in app.json, then those files
// side by side with a progress event as each one lands.
func (s *Service) writeAll(ctx context.Context, b *build) (spec.Files, error) {
	s.step(b, bridge.StepWrite, bridge.BuildRunning, "Writing the records and the agent")
	plan := b.currentPlan()
	planRaw, err := planJSON(plan)
	if err != nil {
		return nil, err
	}
	core, err := s.writeUnit(ctx, b, plan, string(planRaw), writeUnit{only: []string{spec.AppFile, spec.AgentFile}})
	if err != nil {
		return nil, err
	}
	s.announce(b, core, "Records and agent written")
	files := spec.Files{spec.PlanFile: append(planRaw, '\n')}
	merge(files, core)
	rest, err := s.writeRest(ctx, b, plan, string(planRaw), core[spec.AppFile])
	if err != nil {
		return nil, err
	}
	merge(files, rest)
	if err := s.all.Store.WriteFiles(ctx, b.app, files); err != nil {
		return nil, err
	}
	s.emit(b, bridge.BuildEvent{Step: bridge.StepWrite, Status: bridge.BuildDone, Files: files.Paths(), Log: "All files written"})
	return files, nil
}

func merge(into spec.Files, from map[string][]byte) {
	for path, data := range from {
		into[path] = data
	}
}

func (s *Service) announce(b *build, files map[string][]byte, log string) {
	paths := spec.Files(files).Paths()
	s.emit(b, bridge.BuildEvent{Step: bridge.StepWrite, Status: bridge.BuildRunning, Files: paths, Log: log})
}

// writeRest writes every screen and run side by side. The first error stops
// the others at their next model call.
func (s *Service) writeRest(ctx context.Context, b *build, plan prompt.Plan, planRaw string, appJSON []byte) (map[string][]byte, error) {
	units := restUnits(plan, appJSON)
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
		out      = map[string][]byte{}
		slots    = make(chan struct{}, writeWorkers)
	)
	for _, u := range units {
		wg.Add(1)
		go func(u writeUnit) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			files, err := s.writeUnit(ctx, b, plan, planRaw, u)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					stop()
				}
				return
			}
			merge(out, files)
			s.announce(b, files, "Wrote "+u.only[0])
		}(u)
	}
	wg.Wait()
	return out, firstErr
}

// writeUnit is one model call that writes the files in only.
type writeUnit struct {
	only    []string
	already map[string]string
	schemas string
}

func coreSchemas() string { return schemaFor(spec.AppSchemaURL) }

func restUnits(plan prompt.Plan, appJSON []byte) []writeUnit {
	already := map[string]string{spec.AppFile: string(appJSON)}
	var units []writeUnit
	for _, l := range plan.Screens {
		units = append(units, writeUnit{
			only: []string{"screens/" + l.ID + ".json"}, already: already, schemas: schemaFor(spec.ScreenSchemaURL)})
	}
	for _, l := range plan.Automation {
		units = append(units, writeUnit{
			only: []string{"runs/" + l.ID + ".yaml"}, already: already, schemas: schemaFor(spec.RunSchemaURL)})
	}
	return units
}

func (s *Service) writeUnit(ctx context.Context, b *build, plan prompt.Plan, planRaw string, u writeUnit) (map[string][]byte, error) {
	if u.schemas == "" {
		u.schemas = coreSchemas()
	}
	text, err := prompt.Render(prompt.KindWrite, prompt.Inputs{
		Plan: planRaw, Catalog: catalogText(), Schemas: u.schemas,
		Space: personalSpace, Budget: budgetWords(b.budget), Connectors: b.connectors,
		AgentModel: b.agentModel, Only: u.only, Files: u.already,
	})
	if err != nil {
		return nil, err
	}
	files, err := converse(ctx, s, b, text, func(reply string) (map[string][]byte, error) {
		return expectFiles(reply, u.only)
	})
	if err != nil {
		return nil, err
	}
	return b.patch(files)
}

// expectFiles parses a WRITE reply and keeps exactly the files asked for.
func expectFiles(reply string, only []string) (map[string][]byte, error) {
	parsed, err := prompt.ParseFiles(reply)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(only))
	for _, path := range only {
		data, ok := parsed[path]
		if !ok {
			return nil, fmt.Errorf("the reply has no %s", path)
		}
		out[path] = data
	}
	return out, nil
}

// patch fixes the fields the model cannot know: the id the store gave the app,
// its space, and the name the owner approved on the plan card.
func (b *build) patch(files map[string][]byte) (map[string][]byte, error) {
	raw, ok := files[spec.AppFile]
	if !ok {
		return files, nil
	}
	var app map[string]any
	if err := json.Unmarshal(raw, &app); err != nil {
		return nil, fmt.Errorf("app.json: %w", err)
	}
	app["id"], app["space"] = b.app, personalSpace
	setIn(app, "budget", "month", b.budget)
	setIn(app, "agent", "model", b.agentModel)
	if name := strings.TrimSpace(b.currentPlan().Name); name != "" {
		app["name"] = name
	}
	patched, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		return nil, err
	}
	files[spec.AppFile] = append(patched, '\n')
	return files, nil
}

// setIn sets key on the object at app[group], leaving it alone when the model
// wrote no such object (Check names the missing part).
func setIn(app map[string]any, group, key string, value any) {
	if obj, ok := app[group].(map[string]any); ok {
		obj[key] = value
	}
}

// FIX reads the whole definition for context. A run retry accepts changes to
// only that file, so a repair cannot overwrite an already working screen.
func (s *Service) fixOnly(ctx context.Context, b *build, files spec.Files, report, only string) (spec.Files, error) {
	if only != "" {
		report += "\nChange ONLY " + only + ". Keep every other file as it is."
	}
	s.emit(b, bridge.BuildEvent{Step: bridge.StepWrite, Status: bridge.BuildRunning, Log: "Fixing what the checks found"})
	text, err := prompt.Render(prompt.KindFix, prompt.Inputs{
		Report: report, Files: asText(files), Catalog: catalogText(),
		Schemas: schemaFor(spec.AppSchemaURL, spec.ScreenSchemaURL, spec.RunSchemaURL),
	})
	if err != nil {
		return nil, err
	}
	changed, err := converse(ctx, s, b, text, prompt.ParseFiles)
	if err != nil {
		return nil, err
	}
	if only != "" {
		data, ok := changed[only]
		if !ok {
			return nil, fmt.Errorf("the FIX reply has no %s", only)
		}
		changed = map[string][]byte{only: data}
	}
	delete(changed, spec.PlanFile)
	if changed, err = b.patch(changed); err != nil {
		return nil, err
	}
	next := spec.Files{}
	merge(next, files)
	merge(next, changed)
	if err := s.all.Store.WriteFiles(ctx, b.app, next); err != nil {
		return nil, err
	}
	s.announce(b, changed, "Fixed "+strings.Join(spec.Files(changed).Paths(), ", "))
	return next, nil
}

func asText(files spec.Files) map[string]string {
	out := make(map[string]string, len(files))
	for path, data := range files {
		out[path] = string(data)
	}
	return out
}

// checkRound runs Check on the files and publishes its report.
func (s *Service) checkRound(ctx context.Context, b *build, files spec.Files, round int) (bridge.CheckReport, error) {
	if err := s.shortenPlanLines(ctx, b, files); err != nil {
		return bridge.CheckReport{}, err
	}
	s.emit(b, bridge.BuildEvent{Step: bridge.StepCheck, Status: bridge.BuildRunning, Round: round, Log: "Opening every screen with sample data"})
	report, err := s.all.Check.Run(ctx, b.app, files, round)
	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	if err != nil {
		// Check errors still belong to a repair round; only cancellation stops the loop.
		report.Pass = false
		report.Issues = append(report.Issues, spec.Issue{File: spec.AppFile, Rule: "check/error", Message: err.Error() + "; fix: repair the definition named by Check and rerun every check"})
	}
	if !report.Pass && len(report.Issues) == 0 {
		report.Issues = append(report.Issues, spec.Issue{File: spec.AppFile, Rule: "check/failed", Message: "Check did not pass; fix: repair the current app definition against the supplied schemas and kit"})
	}
	report.Round = round
	status := bridge.BuildDone
	if !report.Pass {
		status = bridge.BuildFailed
	}
	s.emit(b, bridge.BuildEvent{Step: bridge.StepCheck, Status: status, Round: round, Check: &report,
		Log: checkLog(report)})
	return report, nil
}

func checkLog(r bridge.CheckReport) string {
	if r.Pass {
		return "Every screen opens"
	}
	return fmt.Sprintf("%d things to fix", len(r.Issues))
}

// formatReport is the check report as the FIX call reads it, one issue a line.
func formatReport(r bridge.CheckReport) string {
	lines := make([]string, 0, len(r.Issues))
	for _, issue := range r.Issues {
		lines = append(lines, issue.String()+lengthHint(issue))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// preview seeds sample records and opens the preview. The Builder writes only
// sample rows, never real ones, and never reads them back.
func (s *Service) preview(ctx context.Context, b *build, files spec.Files) error {
	s.step(b, bridge.StepPreview, bridge.BuildRunning, "Filling the app with sample data")
	def, err := spec.Decode(files)
	if err != nil {
		return s.failWrite(ctx, b, err)
	}
	rows, err := s.all.Sample.Generate(ctx, def)
	if err != nil {
		return s.failWrite(ctx, b, err)
	}
	if err := s.seeder().Seed(ctx, b.app, rows); err != nil {
		return s.failWrite(ctx, b, err)
	}
	if err := s.setState(ctx, b, bridge.StatePreview); err != nil {
		return err
	}
	s.emit(b, bridge.BuildEvent{Step: bridge.StepPreview, Status: bridge.BuildDone,
		Needs: appNeeds(def.App), Log: "Ready to preview"})
	return nil
}
