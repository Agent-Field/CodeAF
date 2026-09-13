package session

// THE SECOND-EVER QUICK CHILD OF A TURN GETS ONE REVIEW, at admission, before
// the turn's fan-out grows past one.
//
// THE FIRST CALL IS FREE, the way every charge in a cascade is free: a turn
// asking for a single quick task spends nothing. When the same turn admits a
// second concurrent quick child, the family is read TOGETHER — once — and the
// verdict applies before the gate closes on the second admission. A fail-open
// is the same as every review this package's roads have: a review that cannot
// be had admits as written, and a review that says no is an ordinary refusal
// the model can answer again.
//
// ROLE AND ROW NAME THE JUDGE'S OWN TIER, and the family snapshot is the one
// piece of evidence the reviewer is decided on (graph nodes under graph.mu,
// listed the way the admission index prints them).
//
// https://github.com/Agent-Field/aforge-v2/issues/1016 — wave B1.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func init() { roles.Register(roles.RoleQuickReview, roles.TierMastermind) }

// quickAdmissionMarked says whether this turn has already spent its fan-out
// review. It is read and written on the same lock's side as consultCalls
// (session.go), because the answer it bounds is "has this turn already bought
// its family read" and nothing else.
// (written once here for the same reason [Agent.consultCalls] exists)
func (a *Agent) considerQuickAdmission(ctx context.Context, spec taskSpec) string {
	family := a.quickFamilySnapshot(a.graph())
	if len(family) == 0 {
		// THE FIRST IS FREE, the way every charge in a cascade is free: with
		// nothing to read the ask against, there is nothing to call.
		return ""
	}
	// ONCE A TURN, and not one-per-budget — the family that was still there
	// at admission time is what this turn's gate settled. Reset beside
	// consultCalls.
	a.mu.Lock()
	if a.quickAdmissionMarked {
		a.mu.Unlock()
		return ""
	}
	a.quickAdmissionMarked = true
	a.mu.Unlock()

	var user strings.Builder
	user.WriteString("FAMILY:\n")
	for _, member := range family {
		user.WriteString("· " + member + "\n")
	}
	user.WriteString("\nNEW ASK:\n" + spec.brief)

	response, from, err := a.callRole(ctx, roles.RoleQuickReview, a.model,
		[]ai.Message{
			textMessage("system", "You review one quick task's joining a family:\nThe family below is what the turn already handed out. Answer ONLY where you can say it should not start as it is — the asks overlap, or they patch one thing trivially. One JSON object: {\"ok\":true} or {\"ok\":false,\"why\":\"one sentence\"}."),
			textMessage("user", user.String()),
		})
	if err != nil || response == nil {
		return "" // FAIL OPEN, as every review this package's roads have
	}
	a.addAuxiliaryUsage(response, from, 1)

	var verdict struct {
		Ok  bool   `json:"ok"`
		Why string `json:"why"`
	}
	raw, salvageErr := subharness.Salvage(response.Text())
	if err := json.Unmarshal(raw, &verdict); salvageErr != nil || err != nil {
		return ""
	}
	if !verdict.Ok {
		return fmt.Sprintf("quick admission refused: %s", verdict.Why)
	}
	return ""
}

// quickFamilySnapshot is the sub-slice of the graph we consider a running or
// queued family: non-terminal nodes of kind quick in the same workspace. It is
// a rather cheap gate (each [TaskNode.doingNow] tells it) and it is computed
// under the graph's lock, so a gate that never passes a single admission
// still names what it is what couples for.
func (a *Agent) quickFamilySnapshot(graph *TaskGraph) []string {
	graph.mu.Lock()
	defer graph.mu.Unlock()
	var family []string
	for _, node := range graph.nodes {
		if node.spec.quick == nil || node.state.settled() {
			continue
		}
		// SPEC FIELD ONLY, NEVER [TaskNode.instruction]: THAT CALL TAKES
		// graph.mu ITSELF (task_run.go), and from inside a snapshot that holds
		// the same mutex it deadlocks a turn dead on its own word.
		family = append(family, fmt.Sprintf("task %d: %s", node.id, node.spec.brief))
	}
	return family
}
