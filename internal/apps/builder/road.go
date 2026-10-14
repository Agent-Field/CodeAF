package builder

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/session"
)

// RoadEnv picks the road the Builder builds on. It is read ONCE, in New, and
// nowhere else: a second reader would be a switch two doors could disagree
// about (TestTheRoadSwitchIsReadInOnePlace holds that).
const RoadEnv = "CODEAF_APPS_BUILDER_ROAD"

// The road names double as the values of RoadEnv and as the road tag that
// result.json records for every build.
const (
	roadCalls   = "calls"
	roadLoop    = "loop"
	roadLoopFix = "loop+fix"
)

// road is one way to build an app: plan the paragraph, write and check the
// files, and apply the owner's edits to the plan card. The calls road is the
// single-call pipeline the Builder had first; the loop road hosts a task
// worker that types small verbs (docs/apps/builder-loop/DESIGN.md).
type road struct {
	plan  func(s *Service, ctx context.Context, b *build, justBuild bool) error
	write func(s *Service, ctx context.Context, b *build) error
	edit  func(s *Service, ctx context.Context, b *build, edits []bridge.PlanEdit) (prompt.Plan, error)
	// restore re-reads what a restart lost from the road's own store.
	restore func(s *Service, b *build)
	// editable lists the app states in which the plan card may still change.
	editable map[bridge.AppState]bool
}

// roads is the registry; adding a road is adding a row.
var roads = map[string]road{
	roadCalls:  callsRoad,
	roadLoop:   loopRoad,
	roadRescue: rescueRoad,
}

var callsRoad = road{
	plan:     (*Service).planThenMaybeWrite,
	write:    (*Service).callsWrite,
	edit:     (*Service).callsEdit,
	restore:  func(*Service, *build) {},
	editable: map[bridge.AppState]bool{bridge.StatePlanning: true},
}

// chooseRoad resolves the road from its setting. An unknown name, and a loop
// the task belt cannot host, both fall back to calls with one log line, so a
// bad setting never leaves the Builder without a road.
func chooseRoad(setting string, beltAsked bool) (string, road) {
	name := strings.ToLower(strings.TrimSpace(setting))
	switch {
	case name == "":
		name = roadCalls
	case roads[name].plan == nil:
		log.Printf("apps builder: %s=%q is not a road; building on the calls road", RoadEnv, setting)
		name = roadCalls
	case (name == roadLoop || name == roadRescue) && !beltAsked:
		log.Printf("apps builder: the %s road needs the bash task belt; building on the calls road", name)
		name = roadCalls
	}
	return name, roads[name]
}

// roadFromEnv is the one reader of RoadEnv.
func roadFromEnv() (string, road) {
	return chooseRoad(os.Getenv(RoadEnv), session.BashBeltAsked())
}

// roadFor is the road one build runs on: the service's road, unless the loop
// gave up on this build's plan and handed it to the calls road for good, so a
// plan the journal never held is not overwritten by a render of the journal.
func (s *Service) roadFor(b *build) road {
	if b.isDemoted() {
		return callsRoad
	}
	return s.road
}

// callsWrite is Write on the calls road: the road tag, then today's pipeline.
func (s *Service) callsWrite(ctx context.Context, b *build) error {
	if err := s.tagRoad(b, roadCalls); err != nil {
		return err
	}
	return s.writeAndCheck(ctx, b)
}

// callsEdit is today's plan-card edit: apply it to the plan lines, then keep
// plan.json on the draft.
func (s *Service) callsEdit(ctx context.Context, b *build, edits []bridge.PlanEdit) (prompt.Plan, error) {
	plan, err := s.applyEdits(ctx, b, edits)
	if err != nil {
		return prompt.Plan{}, err
	}
	return plan, s.storePlan(ctx, b, plan)
}

// tagRoad records which road built the app in result.json, so the measurement
// can count rescues.
func (s *Service) tagRoad(b *build, name string) error {
	result := b.result()
	result.Road = name
	return s.saveResult(b, result)
}
