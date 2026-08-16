package main

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// leafSpec is the one reader of a leaf's task object.
//
// There is exactly one because there used to be two authors, chosen by scale
// rather than by worker: a task-scale job's spec was the compiler's goal plus
// the method it wrote in the same breath, and a project-scale job's was a plan
// brief plus a plan contract. Two authors and no object is how a retry could
// silently produce a third spec — a title and a summary — with none of the
// specificity the first two had.
//
// The order below is the order of authority, and the last answer is the empty
// spec, which renders to the empty string and leaves every caller on the path
// it took before specs existed. That is what makes the wave a read swap to roll
// back rather than a migration.
func leafSpec(plans *jobPlans, planNode *plan.Node, node store.Node) plan.Spec {
	// The plan document is the live copy: a spec re-aimed by a revision this
	// second is here before it is anywhere else.
	if planNode != nil && !planNode.Spec.Empty() {
		return withStoreSources(planNode.Spec, node)
	}
	// The durable copy. It is what a restarted process has, what a rehydrated
	// job has, and what a node admitted by a splice rather than a plan has.
	if spec := resident.DecodeSpec(node.Spec); !spec.Empty() {
		return withStoreSources(spec, node)
	}
	// Nothing was ever written. Not an error: a reflex, a bare splice and a
	// pre-spec store all land here, and all three ran on Brief alone before.
	return plan.Spec{}
}

// withStoreSources fills the one field the store knows better than the plan
// does — nothing today, and the seam is here because Sources is the field a
// later wave hands to a foreign engine as its file scope. It is a pass-through
// so the read stays one function.
func withStoreSources(spec plan.Spec, node store.Node) plan.Spec {
	if strings.TrimSpace(spec.Instruction) == "" {
		// A spec whose instruction never landed still carries a criterion worth
		// having; the brief is what the worker was actually given, so it stands
		// in as the instruction rather than leaving the object half-written.
		spec.Instruction = strings.TrimSpace(node.Brief)
	}
	return spec
}

// taskSpecGraph is the one-node plan a task-scale job gets.
//
// A task-scale ask is spliced as one leaf, so for as long as "has a plan" meant
// "was decomposed", these jobs had no plan document, no journaled structure and
// nowhere to hang a spec: the working method was carried in a memory map that a
// restart emptied, and the criterion had nowhere to live at all. One node is a
// plan. Journaling it costs one event and buys the task-scale job everything
// the project-scale job already had — a durable spec, a criterion that survives
// a retry, and a structure the revision pass can read.
//
// The graph is minted, not planned: no call is made here, and the two strings
// are the ones the compile call already produced.
func taskSpecGraph(goal, method string) *plan.Graph {
	goal = strings.TrimSpace(goal)
	method = strings.TrimSpace(method)
	graph := &plan.Graph{Goal: goal}
	graph.Nodes = append(graph.Nodes, plan.Node{
		ID:       1,
		Stage:    1,
		Title:    firstLine(goal),
		Summary:  firstLine(goal),
		Kind:     plan.KindWork,
		State:    plan.StatePending,
		Brief:    goal,
		Contract: method,
		Spec:     plan.Spec{Instruction: goal, Method: method},
	})
	return graph
}
