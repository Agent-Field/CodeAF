package session

// THE CASCADE: THE ONE PLACE THIS BUILD BUYS THE EXPENSIVE MODEL ON PURPOSE.
//
// A crew is cheap by design — the shipped one runs work on a flash-class model
// and keeps the careful tier for the handful of calls whose answer decides
// something (internal/roles). What that buys in money it occasionally loses in
// quality, and until this file existed the harness had no way to spend its way
// out of a specific failure: a node that came back short was handed back to the
// SAME model that had just come back short, with the same hands and the same
// blind spot, and the second attempt was the first attempt's twin.
//
// ── WHY THE REFUTATION IS THE RIGHT MOMENT TO PAY ──
//
// Escalating a whole crew is the obvious answer and it is the wrong one: it
// pays the careful tier's price on every node, including the great majority
// that were never going to fail, and the bill lands before anybody knows
// whether it was needed. The auditor's finding is the opposite kind of moment.
// It is a MEASURED FAILURE — a fresh, read-only judge ran the repository's own
// verification against the frozen acceptance and said, with evidence, that the
// cheap hands did not finish the job — so the escalation is bought after the
// fact rather than on a guess, on exactly the nodes that turned out to need it.
//
// And it is bought on the CHEAPEST WORK THERE IS. A repair round is not the
// task again: it runs in the worktree the first attempt already filled, it is
// told the work stands, and it is handed the auditor's gaps verbatim
// (task_audit.go's repairInstruction). The expensive model therefore reads a
// focused instruction about a narrowed problem, on a tree somebody else already
// built, which is the smallest slice of a task anybody can buy.
//
// ── AND IT FLOORS BY ITSELF ──
//
// [roles.Ladder]'s last rung is the model the work is already on, so a crew that
// has configured no tiers, and a crew whose careful tier IS the flash model,
// both resolve this role to the model the node started on. That is not a special
// case anybody has to write: an all-flash install repairs on flash, no
// escalation happens, nothing is recorded, and the ladder needed no branch to
// say so.
//
// ── WHAT THE CASCADE IS NOT ALLOWED TO OVERRULE ──
//
// A model somebody NAMED for this piece of work wins. The cascade is a default —
// the harness's own answer to "which hands should close these gaps" when nobody
// said — and a default that overrode an explicit pick would be this package
// deciding it knows better than the person who typed the model's name, on work
// they are still watching. See [TaskNode.modelPicked].

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// roleRepair is the hands that fix what the cheap hands got wrong.
//
// It is DECLARED HERE rather than in internal/roles, which is the arrangement
// that package's own header asks for: the registry is open, and a package that
// adds an auxiliary call declares its role in the file that owns the call. The
// tier is HIGH and not the mastermind's, for [roles.RoleCareful]'s reason — a
// repair round is many turns of ordinary work done by a model that can be
// trusted with something subtle, not one answer that decides what every other
// call does.
const roleRepair = roles.Role("repair")

func init() {
	// The description is passed here because internal/roles cannot write a line
	// for a role it does not know about, and a settings row with a blank under
	// its name is a call somebody is paying for and cannot read.
	roles.Register(roleRepair, roles.TierHigh, "the second go at work a check found gaps in")
}

// repairModel is which hands one repair round runs on: [roleRepair]'s answer,
// with the model the node is already on as the floor.
//
// THE FLOOR IS THE WHOLE OF THE ALL-FLASH CASE. A failure to resolve is the same
// answer for the same reason internal/orchestrate's careful parts take it:
// nothing here may invent a model id, and the model the work is already on is
// the one thing that is certainly real.
//
// THE LOCKS ARE TAKEN ONE AT A TIME. The node's fields live under the graph's
// lock and the agent's under its own, and this package takes one at a time
// ([Agent.newTaskAgent] says so about the same pair), so both node reads happen
// before a.mu is touched.
func (a *Agent) repairModel(node *TaskNode) string {
	on := strings.TrimSpace(node.runModel())
	picked := node.modelPicked()
	a.mu.Lock()
	source, floor := a.config.RolesSource, a.model
	a.mu.Unlock()
	if on != "" {
		floor = on
	}
	if picked {
		// SOMEBODY NAMED THIS MODEL FOR THIS WORK. The cascade is a default and
		// never an override.
		return floor
	}
	model, err := roles.Resolve(roles.Source(source), roleRepair, floor)
	if err != nil || strings.TrimSpace(model) == "" {
		return floor
	}
	return model
}

// modelPicked reports whether this node's model was NAMED for this piece of
// work, rather than inherited from whatever the conversation happened to be on.
//
// It reads [taskSpec.modelWord] — the word somebody wrote — and it is the same
// test task.go makes to decide whether the proposal's receipt says " on
// <model>" back: ONE SOURCE OF TRUTH for "was a model asked for here". A node
// that named nothing has a resolved id in `spec.model` exactly as a node that
// named one does, so the id alone cannot tell the two apart, and a cascade that
// read it would think every node in the session had been pinned.
//
// A PERSON PICKING IN THE NODE'S OWN ROOM COUNTS TOO, which is why
// [TaskNode.retarget] writes the word as well as the id.
func (n *TaskNode) modelPicked() bool {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return strings.TrimSpace(n.spec.modelWord) != ""
}

// repairedOn records the model a repair round actually ran on, and ONLY when it
// is not the model the node itself is on.
//
// The asymmetry is the point: this field exists so a bench can see WHERE THE
// MONEY WENT, and a row saying "repaired on flash" for a node that was on flash
// all along is a fact nobody can spend. Absent means the ladder floored — no
// escalation was bought — which is the emptiness law read as a bill.
//
// It is written from the model the CHILD reports rather than from the id the
// cascade asked for, because a worker can be moved between the two (the tool-use
// rescue in [Agent.newTaskAgent]), and a row naming the model that was requested
// while another one did the work is a row that cannot be reconciled against a
// provider's invoice.
// It ANSWERS WHETHER IT WROTE ANYTHING, so that the job log and the index row
// cannot disagree about whether an escalation happened: the rule for that lives
// here, and a caller that asked the question a second way would be a second rule.
func (n *TaskNode) repairedOn(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if strings.EqualFold(model, n.runModelLocked()) {
		return false
	}
	n.repaired = model
	return true
}
