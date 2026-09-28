package tui3

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// A SEAT'S HINT NAMES ONLY WHAT IT COULD BE GIVEN NOW. The history said
// inkling-small for the worker, but no offer reaches it any more (its free
// pool is switched off); the checker's usual model sits behind a provider
// that is off; the planner's is outside the rule. Each gives way to the
// router's reachable pick, and where that is unreachable too, to nothing —
// which the seat draws as "nothing allowed can sit this seat".
func TestTheSeatHintNamesOnlyAReachableModel(t *testing.T) {
	offer := func(id string, allowed, served bool) config.CrewOffer {
		return config.CrewOffer{Model: crewroute.Model{ID: id}, Allowed: allowed, Served: served}
	}
	offers := []config.CrewOffer{
		offer("z-ai/glm-5.3-flash", true, true),
		offer("moonshotai/kimi-k3", true, false),
		offer("anthropic/claude-opus-5", false, true),
	}
	usual := map[crewroute.Seat]string{
		crewroute.Worker:  "inkling-small",
		crewroute.Checker: "kimi-k3",
		crewroute.Planner: "claude-opus-5",
	}
	seen := map[crewroute.Seat]bool{crewroute.Worker: true, crewroute.Checker: true, crewroute.Planner: true}
	suggest := map[crewroute.Seat]string{
		crewroute.Worker:  "z-ai/glm-5.3-flash",
		crewroute.Checker: "moonshotai/kimi-k3",
		crewroute.Planner: "thinkingmachines/inkling-small:free",
	}
	crewReachableUsual(usual, seen, suggest, offers)
	if usual[crewroute.Worker] != "glm-5.3-flash" || seen[crewroute.Worker] {
		t.Errorf("worker hint %q (usually=%v), want likely glm-5.3-flash", usual[crewroute.Worker], seen[crewroute.Worker])
	}
	if usual[crewroute.Checker] != "" || usual[crewroute.Planner] != "" {
		t.Errorf("checker %q, planner %q: want no hint where nothing reachable remains", usual[crewroute.Checker], usual[crewroute.Planner])
	}

	kept := map[crewroute.Seat]string{crewroute.Worker: "glm-5.3-flash"}
	keptSeen := map[crewroute.Seat]bool{crewroute.Worker: true}
	crewReachableUsual(kept, keptSeen, map[crewroute.Seat]string{}, offers)
	if kept[crewroute.Worker] != "glm-5.3-flash" || !keptSeen[crewroute.Worker] {
		t.Errorf("a reachable usual model was dropped: %q %v", kept[crewroute.Worker], keptSeen[crewroute.Worker])
	}
}
