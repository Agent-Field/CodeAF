package plan

// A fold is a node whose material is entirely what fed it.
//
// The shape is common and was, until now, invisible: three finished parts and a
// node whose job is to put them side by side. Measured on one report job, that
// node — read three files, write one list — ran as a thirteen-turn exploration
// costing 181,354 tokens where a single pass over the same material is about
// 7,700. It ran that way because nothing in the system could tell it apart from
// a leaf that has to go and find things, so it got the same envelope as one:
// two hundred turns and a hundred and fifty thousand tokens, and an agent with
// two hundred turns spends them.
//
// Telling them apart is structural. It is not "does the title say synthesis" —
// that is a keyword rule wearing a plan's clothes, and it would fire on
// "synthesise a benchmark" and miss "the final list". It is two facts the plan
// document already states about the node, and one the graph states:
//
//   - it gathers: two or more finished results feed it;
//   - the plan named nothing for it to go and touch;
//   - the criterion it is judged by asks it to read, never to run.
//
// Anything that fails one of those has discovery to do — material to reach, a
// command whose output nobody has yet — and discovery is turns.

// Folds reports whether this node's whole obligation is to assemble what
// already fed it.
//
// The inbound count is taken from the plan document here. The dispatcher asks
// the same question of the durable graph at claim time, where it can also see
// that every input actually arrived whole; both must agree before a node is run
// as a fold, and this half is the half that is a fact about the plan.
func Folds(node *Node) bool {
	if node == nil || len(node.Needs) < 2 {
		return false
	}
	// A criterion settled by running something is an obligation to run it, and
	// nothing that has to run a command can be one model call. This is checked
	// for every kind of node, including the ones the harness owns: a spliced
	// parent inherits its own done-criterion, and "the suite is green" stays its
	// job however its children divided the work.
	for _, condition := range node.Spec.Done.Conditions {
		if condition.Kind == CheckRun {
			return false
		}
	}
	// A synthesis node is the harness's own, not the planner's: it is appended
	// to gather a plan that nothing else gathers, or it is what a parent becomes
	// when its work is spliced into children. Either way its sources — if it
	// ever had any — were handed to the children, and what comes back to it is
	// their results. That is the definition of a fold and it needs no further
	// evidence.
	if node.Kind == KindSynthesis {
		return true
	}
	// For an ordinary work node the evidence has to be the plan's own: sources
	// are "the distinct things this node must touch to be done", so a node that
	// names any has been told to go and touch them. Naming none, with two or
	// more results already in hand, is a node whose material is those results.
	return len(node.Sources) == 0 && len(node.Spec.Sources) == 0
}
