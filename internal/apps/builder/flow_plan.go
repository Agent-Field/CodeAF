package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// planThenMaybeWrite is apps.create's background work: the PLAN call, the plan
// card event, and, when the owner skipped the card, the write run.
func (s *Service) planThenMaybeWrite(ctx context.Context, b *build, justBuild bool) error {
	s.step(b, bridge.StepPlan, bridge.BuildRunning, "Reading what you asked for")
	plan, err := s.planFromParagraph(ctx, b)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		debugLog(b.app, "plan failed", err)
		s.step(b, bridge.StepPlan, bridge.BuildFailed, ownerLog(err))
		return nil
	}
	if err := s.publishPlan(ctx, b, plan); err != nil {
		return err
	}
	if !justBuild {
		return nil
	}
	if err := s.setState(ctx, b, bridge.StateBuilding); err != nil {
		return err
	}
	return s.writeAndCheck(ctx, b)
}

func (s *Service) planFromParagraph(ctx context.Context, b *build) (prompt.Plan, error) {
	in := s.planInputs(b)
	in.Paragraph = b.currentPlan().Ask
	return s.runPlanCall(ctx, b, in)
}

func (s *Service) planInputs(b *build) prompt.Inputs {
	return prompt.Inputs{
		Space: personalSpace, Budget: budgetWords(b.budget),
		Connectors: b.connectors, Catalog: catalogText(),
	}
}

// runPlanCall renders and sends one PLAN call and keeps the owner's own words
// as plan.ask, whatever the model wrote there.
func (s *Service) runPlanCall(ctx context.Context, b *build, in prompt.Inputs) (prompt.Plan, error) {
	text, err := prompt.Render(prompt.KindPlan, in)
	if err != nil {
		return prompt.Plan{}, err
	}
	plan, err := converse(ctx, s, b, text, prompt.ParsePlan)
	if err != nil {
		return prompt.Plan{}, err
	}
	plan.Ask = b.currentPlan().Ask
	if strings.TrimSpace(plan.Name) == "" {
		plan.Name = deriveName(plan.Ask)
	}
	plan.Needs = "computed"
	return plan, nil
}

// publishPlan stores plan.json on the draft and shows the plan card.
func (s *Service) publishPlan(ctx context.Context, b *build, plan prompt.Plan) error {
	if err := s.storePlan(ctx, b, plan); err != nil {
		return err
	}
	sp := specPlan(plan)
	s.emit(b, bridge.BuildEvent{
		Step: bridge.StepPlan, Status: bridge.BuildDone, Log: "Plan ready",
		Plan: &sp, Needs: planNeeds(plan, b),
	})
	return nil
}

func (s *Service) storePlan(ctx context.Context, b *build, plan prompt.Plan) error {
	raw, err := planJSON(plan)
	if err != nil {
		return err
	}
	b.setPlan(plan)
	return s.all.Store.WriteFiles(ctx, b.app, spec.Files{spec.PlanFile: append(raw, '\n')})
}

// EditPlan applies the plan card's edits. A line edit changes that line in
// place; an edit with no line to name, or an empty section, is a request in the
// owner's words and goes back to the model.
func (s *Service) EditPlan(ctx context.Context, scope svc.Scope, p bridge.PlanEditParams) (bridge.PlanEditResult, error) {
	b, err := s.buildFor(ctx, scope.App)
	if err != nil {
		return bridge.PlanEditResult{}, err
	}
	if detail, err := s.all.Store.Get(ctx, scope.App); err != nil {
		return bridge.PlanEditResult{}, err
	} else if detail.State != bridge.StatePlanning {
		return bridge.PlanEditResult{}, bridge.Errorf(bridge.CodeConflict, "the plan can only change before Write")
	}
	plan, err := s.applyEdits(ctx, b, p.Edits)
	if err != nil {
		return bridge.PlanEditResult{}, err
	}
	if err := s.storePlan(ctx, b, plan); err != nil {
		return bridge.PlanEditResult{}, err
	}
	return bridge.PlanEditResult{Plan: specPlan(plan), Needs: planNeeds(plan, b)}, nil
}

func (s *Service) applyEdits(ctx context.Context, b *build, edits []bridge.PlanEdit) (prompt.Plan, error) {
	lines, requests := splitEdits(b.currentPlan(), edits)
	plan, err := prompt.ApplyPlanEdits(b.currentPlan(), lines)
	if err != nil {
		return prompt.Plan{}, bridge.Errorf(bridge.CodeInvalid, err.Error())
	}
	if len(requests) == 0 {
		return plan, nil
	}
	b.setPlan(plan)
	current, err := json.Marshal(specPlan(plan))
	if err != nil {
		return prompt.Plan{}, err
	}
	in := s.planInputs(b)
	in.Plan, in.Request = string(current), strings.Join(requests, "\n")
	return s.runPlanCall(ctx, b, in)
}

// splitEdits sorts the card's edits into line edits and free requests.
func splitEdits(plan prompt.Plan, edits []bridge.PlanEdit) (lines []prompt.Edit, requests []string) {
	for _, e := range edits {
		switch {
		case e.Section == prompt.GroupAgent:
			lines = append(lines, prompt.Edit{Op: prompt.OpSet, Group: e.Section, Line: e.Text})
		case e.Section == "" || e.ID == "":
			requests = append(requests, e.Text)
		default:
			lines = append(lines, lineEdit(plan, e))
		}
	}
	return lines, requests
}

// lineEdit sets an existing line, adds an unknown id, and removes on empty text.
func lineEdit(plan prompt.Plan, e bridge.PlanEdit) prompt.Edit {
	edit := prompt.Edit{Group: e.Section, ID: e.ID, Line: e.Text}
	switch {
	case strings.TrimSpace(e.Text) == "":
		edit.Op = prompt.OpRemove
	case planHasLine(plan, e.Section, e.ID):
		edit.Op = prompt.OpSet
	default:
		edit.Op = prompt.OpAdd
	}
	return edit
}

func planHasLine(plan prompt.Plan, group, id string) bool {
	for _, l := range groupLines(plan, group) {
		if l.ID == id {
			return true
		}
	}
	return false
}

func groupLines(plan prompt.Plan, group string) []prompt.Line {
	switch group {
	case prompt.GroupData:
		return plan.Data
	case prompt.GroupAutomation:
		return plan.Automation
	case prompt.GroupScreens:
		return plan.Screens
	}
	return nil
}

func decodePlan(raw string) (prompt.Plan, error) {
	var sp spec.Plan
	if err := json.Unmarshal([]byte(raw), &sp); err != nil {
		return prompt.Plan{}, err
	}
	return planFromSpec(sp), nil
}

// deriveName makes a first name for the app folder from the owner's words;
// the plan's own name replaces it in app.json.
func deriveName(paragraph string) string {
	var words []string
	for _, w := range strings.FieldsFunc(paragraph, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if !skipWords[strings.ToLower(w)] {
			words = append(words, w)
		}
		if len(words) == 3 {
			break
		}
	}
	if len(words) == 0 {
		return "New app"
	}
	return strings.ToUpper(words[0][:1]) + words[0][1:] + titleRest(words[1:])
}

func titleRest(words []string) string {
	if len(words) == 0 {
		return ""
	}
	return " " + strings.Join(words, " ")
}

var skipWords = map[string]bool{"a": true, "an": true, "the": true, "my": true, "our": true, "for": true, "to": true, "of": true}

func budgetWords(month float64) string {
	return fmt.Sprintf("up to $%.0f a month, at most $%.2f per run, ask at the cap", month, month/10)
}

const personalSpace = "personal"
