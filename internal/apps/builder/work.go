package builder

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/trace"
)

func (s *Jobs) start(j *job) {
	if len(j.Queue) == 0 || s.workers[j.Draft] != nil || s.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.workers[j.Draft] = cancel
	s.wg.Add(1)
	go func(id string) {
		defer s.wg.Done()
		restart := s.work(ctx, id)
		cancel()
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.workers, id)
		// Recheck the queue under the lock in case a request arrived
		// as the worker was leaving. Storage failures stay stopped.
		if restart && s.ctx.Err() == nil {
			if j, err := s.load(id); err == nil {
				s.start(j)
			}
		}
	}(j.Draft)
}

func (s *Jobs) work(ctx context.Context, id string) bool {
	for ctx.Err() == nil {
		s.mu.Lock()
		j, err := s.load(id)
		if err != nil || len(j.Queue) == 0 {
			s.mu.Unlock()
			return err == nil
		}
		t := j.Queue[0]
		// Keep a Desk choice pending if its renderer disconnects during this turn.
		if !t.DeskReuse && s.opts.CanLook != nil && s.opts.CanLook() {
			t.DeskReuse = true
			j.Queue[0].DeskReuse = true
		}
		checking := t.Round > 0 && t.Report == nil
		event := startEvent(t, checking)
		j.State.State = event.Stage
		err = s.save(j, event)
		s.mu.Unlock()
		if err != nil {
			return false
		}

		headless := !t.DeskReuse
		catalog, err := s.catalog(j.App)
		var report schema.Report
		var files map[string]string
		var reply string
		var calls BuildCalls
		if err == nil && checking {
			existing := externalTypes(catalog.Types, id, j.App)
			def := definition(j.Working.Files)
			if !headless {
				report = j.checkReuse(catalog)
			}
			if report.OK() {
				if s.opts.Check != nil {
					report = s.opts.Check(ctx, id, def, existing)
				} else {
					report = check.Definition(def, existing)
				}
			}
		} else if err == nil {
			files, reply, calls, err = s.ask(ctx, j, t, catalog, headless)
		}
		if ctx.Err() != nil {
			return true
		}
		if err = s.commitTurn(id, t, checking, headless, catalog, report, files, reply, calls, err); err != nil {
			return errors.Is(err, context.Canceled)
		}
	}
	return true
}

func startEvent(t turn, checking bool) Event {
	switch {
	case checking:
		return Event{Stage: Checking, Round: t.Round, Line: "Trying the screens and automations."}
	case t.Stage == Planning:
		return Event{Stage: Planning, Line: "Planning the app."}
	case t.Report != nil:
		return Event{Stage: Writing, Round: t.Round + 1, Line: "Fixing issues from the last check."}
	default:
		return Event{Stage: Writing, Round: 1, Line: "Making screens and automations."}
	}
}

func externalTypes(types schema.TypeSet, ids ...string) schema.TypeSet {
	result := make(schema.TypeSet, len(types))
	for name, shape := range types {
		if !slices.Contains(ids, shape.Owner) {
			result[name] = shape
		}
	}
	return result
}

type modelType struct {
	Common string                `json:"common,omitempty"`
	Name   string                `json:"name"`
	Fields map[string]modelField `json:"fields"`
	Count  int                   `json:"count"`
}

type modelField struct {
	Type     string   `json:"type"`
	To       string   `json:"to,omitempty"`
	Values   []string `json:"values,omitempty"`
	Required bool     `json:"required,omitempty"`
}

func safeTypes(types schema.TypeSet, counts map[string]int) []modelType {
	out := make([]modelType, 0, len(types))
	for _, name := range slices.Sorted(maps.Keys(types)) {
		typ := modelType{Name: name, Common: types[name].Common, Fields: map[string]modelField{}, Count: counts[name]}
		for field, shape := range types[name].Fields {
			typ.Fields[field] = modelField{Type: shape.Type, To: shape.To, Values: shape.Values, Required: shape.Required}
		}
		out = append(out, typ)
	}
	return out
}

func (s *Jobs) ask(ctx context.Context, j *job, t turn, catalog Catalog, headless bool) (map[string]string, string, BuildCalls, error) {
	model, err := s.opts.Model()
	if err != nil {
		return nil, "", BuildCalls{}, err
	}
	if model == nil {
		return nil, "", BuildCalls{}, errors.New("no app model is configured")
	}
	stage := t.Stage
	if t.Report != nil {
		stage = "repair"
	}
	types := externalTypes(catalog.Types, j.App, j.Draft)
	var look func(context.Context, string) ([]string, error)
	if s.opts.Look != nil && s.opts.CanLook != nil && s.opts.CanLook() {
		look = func(ctx context.Context, screen string) ([]string, error) {
			return s.opts.Look(ctx, j.Draft, screen)
		}
	}
	request := t.Request
	if request == "" {
		request = j.LastRequest
	}
	using := slices.Clone(j.Accepted)
	if headless {
		for _, offer := range j.Offers {
			if offer.App != "" && catalog.reuseSource(offer.Type) == offer.App && !slices.Contains(using, offer.Type) {
				using = append(using, offer.Type)
			}
		}
	}
	files, reply, calls, err := modelTurn(ctx, model, filepath.Join(s.root, j.Draft), modelInput{
		Editing: j.App != "", Stage: stage, Request: request, Target: t.Target, Marks: j.Marks, Types: safeTypes(types, catalog.Counts),
		Headless: headless, Using: using, Definition: j.Working.Files, Report: t.Report, Continued: t.Continuations > 0,
	}, types, s.author, func(files map[string]string) error {
		return s.showWork(ctx, j, t, files)
	}, look, func(ctx context.Context, target Target) error {
		return s.claim(ctx, j.Draft, t.ID, target)
	}, func() (*Presence, error) {
		state, err := s.State(j.Draft)
		return state.Presence, err
	})
	if err == nil || errors.Is(err, errModelCallLimit) {
		err = errors.Join(err, stampAppID(files, j))
	}
	return files, reply, calls, err
}

// A stable ID makes preview and live capability checks agree after Keep.
func stampAppID(files map[string]string, j *job) error {
	var app map[string]json.RawMessage
	if json.Unmarshal([]byte(files[schema.AppFile]), &app) != nil || app == nil {
		return nil
	}
	id := j.App
	if id == "" {
		id = j.Draft
	}
	app["id"], _ = json.Marshal(id)
	raw, err := json.MarshalIndent(app, "", "  ")
	if err == nil {
		files[schema.AppFile] = string(raw)
	}
	return err
}

// A complete tool batch can be previewed during the next model call. The
// finished turn still commits its own checkpoint as one atomic update.
func (s *Jobs) showWork(ctx context.Context, base *job, t turn, files map[string]string) error {
	if !json.Valid([]byte(files[schema.AppFile])) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.load(base.Draft)
	if err != nil {
		return err
	}
	if ctx.Err() != nil || len(current.Queue) == 0 || current.Queue[0].ID != t.ID {
		return context.Canceled
	}
	previous := current.Working.Files
	if current.WritingFiles != nil {
		previous = current.WritingFiles
	}
	for _, path := range definition(files).ScreenFiles() {
		if current.Presence == nil && previous[path] != files[path] {
			current.Presence = &Presence{Actor: t.ID, Target: Target{Screen: schema.FileID(path)}}
			break
		}
	}
	current.WritingFiles = maps.Clone(files)
	if err := stampAppID(current.WritingFiles, base); err != nil {
		return err
	}
	current.Screens = screens(files)
	if current.Presence != nil {
		presence := *current.Presence
		presence.At, presence.Revision = time.Now().UTC(), current.Revision+1
		current.Presence = &presence
	}
	return s.save(current, Event{})
}

func (s *Jobs) commitTurn(id string, t turn, checking, headless bool, catalog Catalog, report schema.Report, files map[string]string, reply string, calls BuildCalls, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, loadErr := s.load(id)
	if loadErr != nil {
		return loadErr
	}
	if len(j.Queue) == 0 || j.Queue[0].ID != t.ID || s.ctx.Err() != nil {
		return context.Canceled
	}
	j.WritingFiles = nil
	j.Screens = screens(j.Working.Files)
	if calls.Limit > 0 {
		stage := startEvent(t, false)
		stage.Draft = id
		stage.Line = "Finished this pass."
		stage.Calls = &calls
		stage.Detail = briefText(string(trace.Scrub([]byte(reply))), 2400)
		j.Progress = append(j.Progress, stage)
	}
	if errors.Is(err, errModelCallLimit) {
		advanced := !maps.Equal(j.Working.Files, files)
		j.Working = snapshot{Files: files}
		j.describeDraft(catalog, headless, t.Stage == Planning)
		j.Presence = nil
		const maxContinuations = 2
		if t.Stage != Planning {
			report = check.Definition(definition(files), externalTypes(catalog.Types, id, j.App))
		}
		if t.Stage != Planning && report.OK() {
			// The last allowed call can finish the draft even if the model
			// never had a chance to say so. Run the full sample and Desk checks.
			j.Queue[0].Round++
			j.Queue[0].Report = nil
			j.State.State = Checking
			return s.save(j, Event{Stage: Checking, Round: t.Round + 1, Line: "Progress saved. Checking the draft."})
		}
		if t.Stage != Planning && advanced && t.Continuations < maxContinuations {
			j.Queue[0].Continuations++
			j.Queue[0].Report = &report
			return s.save(j, Event{Stage: Writing, Round: t.Round + 1, Line: "Progress saved. Continuing the build.", Report: &report})
		}
		j.State.State = Failed
		j.Problem = "I reached the build limit. Your draft is saved."
		return s.finish(j, Event{Line: j.Problem, Report: &schema.Report{Problems: []schema.Problem{
			{Code: "build_failed", Message: j.Problem},
		}}})
	}

	if errors.Is(err, errCompilerUnavailable) {
		j.State.State = NeedsYou
		j.Problem = "This engine needs the screen compiler before it can finish the app."
		return s.finish(j, Event{Line: j.Problem})
	}
	if err != nil {
		j.State.State = Failed
		reason := strings.Join(strings.Fields(string(trace.Scrub([]byte(err.Error())))), " ")
		reason = briefText(reason, 1800)
		action := "finish building the app"
		if t.Stage == Planning {
			action = "make the plan"
		}
		var api *provider.APIError
		if errors.As(err, &api) && api.Overflow {
			j.Problem = "I couldn't " + action + " because the app instructions and conversation were too long for the model."
		} else {
			j.Problem = "I couldn't " + action + " because " + strings.TrimRight(reason, ".") + "."
		}
		return s.finish(j, Event{Line: j.Problem, Report: &schema.Report{Problems: []schema.Problem{
			{Code: "build_failed", Message: reason},
		}}})
	}
	if checking {
		ok := report.OK()
		event := Event{Stage: Checking, Round: t.Round, OK: &ok, Report: &report}
		if ok {
			j.describeDraft(catalog, headless, t.Stage == Planning)
			j.Working.Passed = true
			j.Working.CheckComplete = true
			j.Working.Look = report.Look
			if err := j.checkpoint(s.displaySummary(j, t)); err != nil {
				return err
			}
			j.State.State = Ready
			event.Stage, event.Line = Ready, "Ready to try."
			if note := report.LookNote(); note != "" {
				event.Line += " " + note
			}
			return s.finish(j, event)
		}
		j.Working.Passed, j.Working.CheckComplete = false, false
		j.Working.Look = report.Look
		pendingReuse := slices.ContainsFunc(report.Problems, func(p schema.Problem) bool { return p.Code == "choose_reuse" })
		unavailable := slices.ContainsFunc(report.Problems, func(p schema.Problem) bool {
			return strings.HasSuffix(p.Code, "_unavailable")
		})
		if t.Round >= maxRounds || sameFindings(t.PreviousFindings, report.Problems) || unavailable || pendingReuse || t.CheckOnly {
			j.State.State = NeedsYou
			j.Problem = "Check found an issue. " + report.Problems[0].Message
			if pendingReuse {
				j.Problem = "Choose whether this app should use your existing records."
			}
			if unavailable {
				for _, issue := range report.Problems {
					if strings.HasSuffix(issue.Code, "_unavailable") {
						j.Problem = issue.Message
						break
					}
				}
			}
			event.Stage, event.Line = NeedsYou, j.Problem
			return s.finish(j, event)
		}
		j.Queue[0].Report = &report
		j.Queue[0].PreviousFindings = report.Problems
		event.Line = "Some parts need another pass."
		return s.save(j, event)
	}

	j.Working = snapshot{Files: files}
	j.Presence = nil
	j.describeDraft(catalog, headless, t.Stage == Planning)
	if reply == "" {
		reply = "I updated your app."
	}
	if t.Stage == Planning {
		if len(j.Plan) == 0 || j.Working.Files[schema.AppFile] == "" {
			j.State.State, j.Problem = NeedsYou, reply
			return s.finish(j, Event{Line: reply})
		}
		if err := j.checkpoint("Here is the plan for " + displayName(j.Name, "your app") + "."); err != nil {
			return err
		}
		j.State.State = Ready
		return s.finish(j, Event{Stage: Planning, Line: "Plan ready to review."})
	}
	j.Queue[0].Round++
	j.Queue[0].Report = nil
	j.State.State = Checking
	return s.save(j, Event{})
}

// Repeating the same set of findings means the last repair did not move Check forward.
func sameFindings(previous, current []schema.Problem) bool {
	if len(previous) == 0 || len(previous) != len(current) {
		return false
	}
	type address struct{ File, Path, Code string }
	counts := map[address]int{}
	for _, p := range previous {
		counts[address{p.File, p.Path, p.Code}]++
	}
	for _, p := range current {
		key := address{p.File, p.Path, p.Code}
		if counts[key] == 0 {
			return false
		}
		counts[key]--
	}
	return true
}

func (j *job) checkpoint(reply string) error {
	id, err := cell.NewID(time.Now())
	if err != nil {
		return err
	}
	summary, _, _ := strings.Cut(strings.TrimSpace(reply), "\n")
	summary = strings.Join(strings.Fields(summary), " ")
	if runes := []rune(summary); len(runes) > 200 {
		summary = string(runes[:197]) + "…"
	}
	j.Checkpoints = append(j.Checkpoints, Checkpoint{ID: id, Summary: summary, Screens: j.Screens})
	j.Saved[id] = j.Working
	j.Transcript = append(j.Transcript, Message{ID: id, Role: "assistant", Text: reply, Checkpoint: id})
	return nil
}

func (s *Jobs) finish(j *job, event Event) error {
	finished := j.Queue[0]
	j.Queue = j.Queue[1:]
	j.Presence = nil
	if len(j.Queue) != 0 {
		j.State.State = j.Queue[0].Stage
		j.Problem = ""
	}
	if err := s.save(j, event); err != nil {
		return err
	}
	if finished.Proposal != "" && s.opts.Proposed != nil {
		p := Proposal{App: j.App, Draft: j.Draft, ID: finished.Proposal, Kind: "big", Source: "assistant", BaseVersion: j.Base, SummaryLines: []string{"Open the builder to review the change."}}
		if j.State.State == Ready && j.Working.Passed {
			p.Display = &DisplayChange{Part: displayName(j.Name, "App"), Before: "Current app", After: "Updated app"}
		}
		if finished.Small && j.State.State == Ready && len(j.Queue) == 0 && j.Working.Passed {
			p.Screen = s.proposalScreen(j)
			if p.Screen != "" && len(j.Checkpoints) != 0 {
				p.Kind = "small"
				p.Checkpoint = j.Checkpoints[len(j.Checkpoints)-1].ID
				if live, err := s.opts.Store.Get(j.App); err == nil {
					change := screenChange(live.Files(), j.Working.Files, "screens/"+p.Screen+".json", finished.Target)
					p.Display = &change
					p.SummaryLines = []string{change.Part}
				}
			}
		}
		s.opts.Proposed(p)
	}
	if len(j.Queue) == 0 && s.opts.Notify != nil && ((j.State.State == Ready && j.Working.Passed) || j.State.State == NeedsYou) {
		s.opts.Notify(j.Summary)
	}
	return nil
}

// An in-place Apply card is limited to one existing screen, with optional
// fields allowed alongside the screen change. Other edits open the builder.
func (s *Jobs) proposalScreen(j *job) string {
	kept, err := s.opts.Store.Get(j.App)
	if err != nil || kept.Meta.Version != j.Base {
		return ""
	}
	before, after := kept.Files(), j.Working.Files
	screen := ""
	for path, text := range before {
		next, exists := after[path]
		if !exists {
			return ""
		}
		if text == next {
			continue
		}
		switch {
		case path == schema.AppFile:
			if !onlyOptionalFields(text, next) {
				return ""
			}
		case path == schema.PlanFile || path == schema.SampleFile:
			// Neither file changes the behaviour of a kept app.
		case strings.HasPrefix(path, "screens/") && screen == "":
			screen = schema.FileID(path)
		default:
			return ""
		}
	}
	for path := range after {
		if _, exists := before[path]; !exists {
			return ""
		}
	}
	return screen
}

func onlyOptionalFields(before, after string) bool {
	kept, err := schema.ParseApp([]byte(before))
	if err != nil {
		return false
	}
	draft, err := schema.ParseApp([]byte(after))
	if err != nil {
		return false
	}
	for name, record := range draft.Records {
		prior, exists := kept.Records[name]
		if !exists {
			return false
		}
		for name, field := range record.Fields {
			if _, exists := prior.Fields[name]; exists {
				continue
			}
			if field.Required || field.Unique || field.Default != nil || field.Computed != "" || field.Sequence != nil || field.Timer != nil {
				return false
			}
			delete(record.Fields, name)
		}
	}
	return reflect.DeepEqual(kept, draft)
}

func (j *job) checkReuse(catalog Catalog) schema.Report {
	app, _ := schema.ParseApp([]byte(j.Working.Files[schema.AppFile]))
	var report schema.Report
	for _, name := range slices.Sorted(maps.Keys(app.Records)) {
		if typ := app.Records[name]; typ.Uses && !j.reuseAccepted(name, typ, catalog) {
			report.Problems = append(report.Problems, schema.Problem{
				File: schema.AppFile, Path: "/records/" + name, Code: "choose_reuse",
				Message: "The person needs to confirm sharing their " + name + " records.",
			})
		}
	}
	return report
}

func reuseOffers(app schema.App, liveID string, accepted []string, catalog Catalog) []Offer {
	offers := []Offer{}
	proposedCommon := map[string]schema.CommonTypeOffer{}
	libraryOffers := map[string]bool{}
	if app.Kit == 2 {
		for name, record := range app.Records {
			if offer, ok := catalog.Types.OfferCommonType(name, record); ok {
				proposedCommon[name] = offer
				libraryOffers[offer.Key] = true
			}
		}
	}

	for _, name := range slices.Sorted(maps.Keys(catalog.Types)) {
		existing := catalog.Types[name]
		source := catalog.reuseSource(name)
		if source == "" || source == liveID || source == app.ID {
			continue
		}
		if slices.Contains(accepted, name) || existing.Common != "" && slices.Contains(accepted, "common:"+existing.Common) {
			continue
		}
		offer := Offer{Type: name, Count: catalog.Counts[name], App: source, Common: existing.Common, Line: "Use " + catalog.reuseText(name)}
		for proposed, shape := range app.Records {
			if shape.Common != "" && shape.Common != existing.Common {
				continue
			}
			if proposedCommon[proposed].Existing == name || reusable(proposed, name, shape.Fields, existing.Fields) {
				offers = append(offers, offer)
				break
			}
		}
		// A candidate is distinct from a contact, but a recruiting app
		// can link the two without copying anybody's name or email.
		if len(offers) == 0 || offers[len(offers)-1].Type != name {
			if candidate, ok := app.Records["candidate"]; ok && strings.TrimSuffix(name, "s") == "contact" &&
				(candidate.Fields["name"].Type == "text" && existing.Fields["name"].Type == "text" ||
					reusable("person", "contact", candidate.Fields, existing.Fields)) {
				offers = append(offers, offer)
			}
		}
	}
	if len(libraryOffers) > 0 {
		library := schema.CommonTypes()
		for _, key := range slices.Sorted(maps.Keys(libraryOffers)) {
			offers = append(offers, Offer{Type: key, Common: key, Line: "Start with " + library.Types[key].Label})
		}
	}
	return offers
}

func reusable(a, b string, as, bs map[string]schema.Field) bool {
	if strings.TrimSuffix(a, "s") == strings.TrimSuffix(b, "s") {
		return true
	}
	matched := 0
	for key, f := range as {
		if other, ok := bs[key]; ok && f.Type == other.Type &&
			(f.Type != "ref" && f.Type != "refs" || f.To == other.To) {
			matched++
		}
	}
	return matched >= 2 && matched*2 >= min(len(as), len(bs))
}

// classify lets additive requests queue without canceling an active model
// call. It sees the requests and the plan, with no tools, and cannot change
// the queue after the step it evaluated has finished.
func (s *Jobs) classify(j *job, active turn, id, text string) {
	input, err := json.Marshal(map[string]any{"active_request": active.Request, "plan": j.Plan, "new_request": text})
	if err != nil {
		return
	}
	draft := j.Draft
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
		defer cancel()
		model, err := s.opts.Model()
		if err != nil || model == nil {
			return
		}
		instruction := "Classify whether the new request directly contradicts or cancels the work in progress. Additions and small refinements can wait. Reply REDIRECT or QUEUE only."
		catalog, err := s.catalog(j.App)
		if err != nil {
			return
		}
		types := externalTypes(catalog.Types, j.App, j.Draft)
		ctx = context.WithValue(ctx, classSourceKey{}, func() ([]schema.DataClass, error) {
			return definitionClasses(j.Working.Files, types)
		})
		modelID := builderModelID(model)
		response, err := session.ScopedCommandTurn(ctx, filepath.Join(s.root, draft), modelID, instruction, string(input), model,
			func(context.Context, string) (string, error) {
				return "", errors.New("This turn only classifies the request.")
			})
		if err != nil || !strings.EqualFold(strings.TrimSpace(response), "REDIRECT") {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		current, err := s.load(draft)
		if err != nil || len(current.Queue) < 2 || current.Queue[0].ID != active.ID || s.ctx.Err() != nil {
			return
		}
		idx := slices.IndexFunc(current.Queue, func(t turn) bool { return t.ID == id })
		if idx < 0 {
			return
		}
		current.Queue[idx].Stage = active.Stage
		current.Queue = current.Queue[1:]
		_ = s.prioritize(current, id)
	}()
}
