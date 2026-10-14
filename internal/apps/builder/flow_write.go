package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	files, pending, err := s.writeAll(ctx, b)
	if err != nil {
		return s.failWrite(ctx, b, err)
	}
	return s.checkAndFix(ctx, b, files, "", pending)
}

// checkAndFix runs Check and the FIX rounds. pending are issues Check cannot
// see, such as a file whose WRITE reply never took the right shape; they join
// the next round's report so FIX repairs them like any other finding.
func (s *Service) checkAndFix(ctx context.Context, b *build, files spec.Files, only string, pending []spec.Issue) error {
	loop := prompt.NewLoop()
	_ = loop.PlanWritten()
	_ = loop.FilesWritten()
	for round := 1; ; round++ {
		report, err := s.checkRound(ctx, b, files, round, pending)
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
		if files, pending, err = s.fixOnly(ctx, b, files, report.Issues, loop.LastReport(), only); err != nil {
			return s.failWrite(ctx, b, err)
		}
		_ = loop.FixApplied()
	}
}

func (s *Service) failWrite(ctx context.Context, b *build, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	debugLog(b.app, "write failed", err)
	s.emit(b, bridge.BuildEvent{Step: bridge.StepWrite, Status: bridge.BuildFailed, Log: ownerLog(err)})
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
func (s *Service) writeAll(ctx context.Context, b *build) (spec.Files, []spec.Issue, error) {
	plan := b.currentPlan()
	planRaw, err := planJSON(plan)
	if err != nil {
		return nil, nil, err
	}
	return s.writeMissing(ctx, b, spec.Files{spec.PlanFile: append(planRaw, '\n')})
}

// writeMissing writes every file of the plan that files does not hold yet,
// so Fix it after a failed write resumes instead of starting over. A screen or
// run whose reply never took the right shape becomes a pending FIX issue; the
// records and agent are written first and must succeed, because every other
// file is written against app.json. What was written is saved even when a
// later call fails.
func (s *Service) writeMissing(ctx context.Context, b *build, files spec.Files) (spec.Files, []spec.Issue, error) {
	plan := b.currentPlan()
	planRaw := string(files[spec.PlanFile])
	if files[spec.AppFile] == nil || files[spec.AgentFile] == nil {
		s.step(b, bridge.StepWrite, bridge.BuildRunning, "Writing the records and the agent")
		core, err := s.writeUnit(ctx, b, plan, planRaw, writeUnit{only: []string{spec.AppFile, spec.AgentFile}})
		if err != nil {
			return nil, nil, err
		}
		s.announce(b, core, "Records and agent written")
		merge(files, core)
	}
	rest, pending, err := s.writeUnits(ctx, b, plan, planRaw, missingUnits(restUnits(plan, files[spec.AppFile]), files))
	merge(files, rest)
	if storeErr := s.all.Store.WriteFiles(ctx, b.app, files); storeErr != nil || err != nil {
		return nil, nil, errors.Join(err, storeErr)
	}
	s.emit(b, bridge.BuildEvent{Step: bridge.StepWrite, Status: bridge.BuildDone, Files: files.Paths(), Log: "All files written"})
	return files, pending, nil
}

func missingUnits(units []writeUnit, files spec.Files) []writeUnit {
	var missing []writeUnit
	for _, u := range units {
		if files[u.only[0]] == nil {
			missing = append(missing, u)
		}
	}
	return missing
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

// writeUnits writes screens and runs side by side. A reply in the wrong shape
// becomes a pending issue and the others carry on; any other error stops them
// at their next model call.
func (s *Service) writeUnits(ctx context.Context, b *build, plan prompt.Plan, planRaw string, units []writeUnit) (map[string][]byte, []spec.Issue, error) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
		pending  []spec.Issue
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
			if issues, ok := shapeIssues(err, u.only); ok {
				pending = append(pending, issues...)
				return
			}
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
	sortIssues(pending)
	return out, pending, firstErr
}

func sortIssues(issues []spec.Issue) {
	sort.Slice(issues, func(i, j int) bool { return issues[i].File < issues[j].File })
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
		return expectFiles(b.app, reply, u.only)
	})
	if err != nil {
		return nil, err
	}
	return b.patch(files)
}

// expectFiles parses a WRITE reply and keeps exactly the files asked for. A
// reply without one of them is a shape error, so the correction names every
// path expected and the ones that came back.
func expectFiles(app, reply string, only []string) (map[string][]byte, error) {
	parsed, err := parseFiles(app, reply, only)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(only))
	for _, path := range only {
		data, ok := parsed[path]
		if !ok {
			return nil, &prompt.ShapeError{Keys: spec.Files(parsed).Paths(), Want: only}
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
// only that file, so a repair cannot overwrite an already working screen. A
// FIX reply in the wrong shape changes nothing and comes back as pending
// issues for the next round, which spends that round rather than the build.
func (s *Service) fixOnly(ctx context.Context, b *build, files spec.Files, issues []spec.Issue, report, only string) (spec.Files, []spec.Issue, error) {
	if only != "" {
		report += "\nChange ONLY " + only + ". Keep every other file as it is."
	}
	s.emit(b, bridge.BuildEvent{Step: bridge.StepWrite, Status: bridge.BuildRunning, Log: "Fixing what the checks found"})
	text, err := prompt.Render(prompt.KindFix, prompt.Inputs{
		Report: report, Files: asText(files), Catalog: catalogText(),
		Schemas: schemaFor(spec.AppSchemaURL, spec.ScreenSchemaURL, spec.RunSchemaURL),
	})
	if err != nil {
		return nil, nil, err
	}
	want := fixPaths(issues, only)
	changed, err := converse(ctx, s, b, text, func(reply string) (map[string][]byte, error) {
		return fixReply(b.app, reply, want, only)
	})
	if pending, ok := shapeIssues(err, want); ok {
		return files, pending, nil
	}
	if err != nil {
		return nil, nil, err
	}
	delete(changed, spec.PlanFile)
	if changed, err = b.patch(changed); err != nil {
		return nil, nil, err
	}
	next := spec.Files{}
	merge(next, files)
	merge(next, changed)
	if err := s.all.Store.WriteFiles(ctx, b.app, next); err != nil {
		return nil, nil, err
	}
	s.announce(b, changed, "Fixed "+strings.Join(spec.Files(changed).Paths(), ", "))
	return next, nil, nil
}

// fixPaths are the files a FIX reply is expected to hold: the run being
// retried, or every file the report names.
func fixPaths(issues []spec.Issue, only string) []string {
	if only != "" {
		return []string{only}
	}
	seen := map[string]bool{}
	var paths []string
	for _, issue := range issues {
		if prompt.ValidPath(issue.File) && !seen[issue.File] {
			seen[issue.File] = true
			paths = append(paths, issue.File)
		}
	}
	sort.Strings(paths)
	return paths
}

// fixReply parses a FIX reply; a run retry keeps only its own file.
func fixReply(app, reply string, want []string, only string) (map[string][]byte, error) {
	if only != "" {
		return expectFiles(app, reply, want)
	}
	return parseFiles(app, reply, want)
}

// parseFiles parses a files reply, noting in the debug log when it had to
// place a wrong shape.
func parseFiles(app, reply string, want []string) (map[string][]byte, error) {
	files, shape, err := prompt.ParseFilesShaped(reply, want)
	if shape != "" {
		slog.Info("apps builder: placed a mis-shaped reply", "app", app, "shape", shape, "files", spec.Files(files).Paths())
	}
	return files, err
}

func asText(files spec.Files) map[string]string {
	out := make(map[string]string, len(files))
	for path, data := range files {
		out[path] = string(data)
	}
	return out
}

// checkRound runs Check on the files and publishes its report.
func (s *Service) checkRound(ctx context.Context, b *build, files spec.Files, round int, pending []spec.Issue) (bridge.CheckReport, error) {
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
	if len(pending) > 0 {
		report.Pass = false
		report.Issues = append(report.Issues, pending...)
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

// repair is Fix it on a failed build: it keeps the files already written,
// writes only the ones still missing, then runs the same Check and FIX rounds
// as the build itself. Starting over would send the same prompts again and
// usually fail the same way.
func (s *Service) repair(ctx context.Context, b *build, stored spec.Files) error {
	if stored[spec.PlanFile] == nil {
		return s.writeAndCheck(ctx, b)
	}
	s.step(b, bridge.StepWrite, bridge.BuildRunning, "Repairing the app")
	files, pending, err := s.writeMissing(ctx, b, stored)
	if err != nil {
		return s.failWrite(ctx, b, err)
	}
	return s.checkAndFix(ctx, b, files, "", pending)
}
