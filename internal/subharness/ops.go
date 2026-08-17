package subharness

// REVIEW BY PATCH: the small set of edits a critic is allowed to make to a page,
// and the pure function that makes them.
//
// WHY A PATCH AND NOT A PAGE. The obvious design for a second-pass critic is to
// hand it the draft and take back the revised draft, and that design has one
// defect that shows up in every real run: the critic must retype everything it
// did not intend to change. A four-thousand-byte page comes back with a brief
// that now says "whishpeR.cpp" and a description that now says "winer", and the
// typo is not a design error — it is transcription noise from a model that was
// asked to copy rather than to think. The page is then WORSE than the draft in
// exactly the places nobody reviewed.
//
// So the critic does not re-emit the page. It emits the ops below, they are
// applied to the ORIGINAL parsed harness, and text nobody touched is byte for
// byte the text the designer wrote. Two things fall out of that and both are
// worth more than the typo immunity:
//
//   - The DELTA is no longer inferred. A diff of two pages is a reconstruction of
//     what a model probably did; a list of ops IS what it did, and printing it is
//     printing the record rather than an account of the record.
//   - A bad op is CHEAP. One op that names a node nobody declared is skipped and
//     reported, and the other eleven still land — where a malformed page loses the
//     whole review turn.
//
// The result is re-validated by [Validate], the same law a page from disk passes.
// A patch cannot make a harness this package would refuse to load.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// The ops. Each one is the smallest edit that is worth naming, and there is
// deliberately no op for "replace the program": a critic that wants a different
// program is designing, not reviewing.
const (
	// OpReplaceBrief rewrites one node's brief. It is set_field's most common
	// case, named on its own because rewriting a brief is most of what a review
	// does and a critic reaching for the general form to do the common thing is
	// a critic one field name away from a skipped op.
	OpReplaceBrief = "replace_brief"
	// OpSetField writes any field of any node. An empty `text` REMOVES the
	// field, which is how a `max_turns` nobody counted goes away.
	OpSetField = "set_field"
	// OpAddNode adds a node, whole, from `node_json`.
	OpAddNode = "add_node"
	// OpDropNode removes a node AND every edge that touched it. The critic has
	// to re-link what it disconnected — the reach law is checked at the end.
	OpDropNode = "drop_node"
	// OpAddEdge and OpDropEdge carry the edge as `node` → `text`.
	OpAddEdge  = "add_edge"
	OpDropEdge = "drop_edge"
	// OpSetVerify moves the harness's verification rung, or — when `node` names
	// one — that node's own rung.
	OpSetVerify = "set_verify"
	// OpSetDyn moves the dynamism rung or its cap: `field` is "ladder" or "cap".
	OpSetDyn = "set_dyn"
	// OpSetWhitelist replaces the tool whitelist with a comma-separated list. It
	// is here because dropping the last node that used a tool leaves a grant
	// nothing uses, and a critic that could not answer for that would be refused
	// for the tidying it was asked to do.
	OpSetWhitelist = "set_whitelist"
	// OpSetDesc rewrites id.desc — the sentence detection matches on, which the
	// review's own checklist asks the critic to read against the goal.
	OpSetDesc = "set_desc"
)

// Op is one edit. The fields are a flat set rather than a union per op because
// the writer is a model: a flat shape is one thing to explain, one thing to
// validate, and a misfilled field fails as a skipped op with a sentence saying
// which field, rather than as an envelope that will not decode at all.
type Op struct {
	// Op is the verb, one of the constants above.
	Op string `json:"op"`
	// Node is the node id the edit lands on — or, for an edge, its source.
	Node string `json:"node,omitempty"`
	// Field is which field is being written, where the verb needs telling.
	Field string `json:"field,omitempty"`
	// Text is the new value — or, for an edge, its target.
	Text string `json:"text,omitempty"`
	// NodeJSON is a whole node, for add_node, in the page's own node shape.
	NodeJSON json.RawMessage `json:"node_json,omitempty"`
}

// String is how an op is printed in a review's delta: short enough for a line,
// specific enough that a person can see what was done without the page.
func (o Op) String() string {
	switch o.Op {
	case OpReplaceBrief:
		return fmt.Sprintf("%s %s (%d bytes)", o.Op, o.Node, len(o.Text))
	case OpSetField:
		if strings.TrimSpace(o.Text) == "" {
			return fmt.Sprintf("%s %s.%s removed", o.Op, o.Node, o.Field)
		}
		return fmt.Sprintf("%s %s.%s = %q", o.Op, o.Node, o.Field, clipOp(o.Text, 48))
	case OpAddNode:
		return fmt.Sprintf("%s %s", o.Op, clipOp(strings.Join(strings.Fields(string(o.NodeJSON)), " "), 72))
	case OpDropNode:
		return fmt.Sprintf("%s %s", o.Op, o.Node)
	case OpAddEdge, OpDropEdge:
		return fmt.Sprintf("%s %s->%s", o.Op, o.Node, o.Text)
	case OpSetVerify:
		if o.Node != "" {
			return fmt.Sprintf("%s %s = %s", o.Op, o.Node, o.Text)
		}
		return fmt.Sprintf("%s %s", o.Op, o.Text)
	case OpSetDyn:
		return fmt.Sprintf("%s %s = %s", o.Op, firstWord(o.Field, "ladder+cap"), o.Text)
	default:
		return fmt.Sprintf("%s %s %s %q", o.Op, o.Node, o.Field, clipOp(o.Text, 48))
	}
}

// OpResult is what became of one op. An op that failed is a REPORT and not an
// error: the review's other findings are still worth having, and a critic that
// named a node it had already dropped has made a bookkeeping mistake, not an
// argument that should cost the whole turn.
type OpResult struct {
	Op  Op
	Err error
}

// Applied reports whether this op landed.
func (r OpResult) Applied() bool { return r.Err == nil }

// Apply is the patch, applied. It is PURE: the harness handed in is not touched,
// including its maps and slices, so a caller can print the draft beside the
// revision afterwards.
//
// The error is the LAW's, not an op's — failed ops are skipped and reported (see
// [ApplyReport]), and what comes back as an error is [Validate] refusing the
// result. A patch that would produce a page this package will not load is a
// failed review, and the critic is told so in the validator's own words.
func Apply(h Harness, ops []Op) (Harness, error) {
	out, _, err := ApplyReport(h, ops)
	return out, err
}

// ApplyReport is [Apply] with the fate of every op, for a caller that prints the
// patch as the delta.
func ApplyReport(h Harness, ops []Op) (Harness, []OpResult, error) {
	out := h.clone()
	results := make([]OpResult, 0, len(ops))
	for _, op := range ops {
		results = append(results, OpResult{Op: op, Err: applyOne(&out, op)})
	}
	out = out.Normalize()
	return out, results, Validate(out)
}

// applyOne checks its preconditions BEFORE it writes anything, so a skipped op
// leaves the page exactly as it found it. That is what makes a failed op cheap
// rather than a half-applied edit nobody can reason about.
func applyOne(h *Harness, op Op) error {
	switch op.Op {
	case OpReplaceBrief:
		if strings.TrimSpace(op.Text) == "" {
			return fmt.Errorf("replace_brief with no text: a node with an empty brief is oriented by nothing")
		}
		return setField(h, op.Node, "brief", op.Text)

	case OpSetField:
		if strings.TrimSpace(op.Field) == "" {
			return fmt.Errorf("set_field with no field named")
		}
		return setField(h, op.Node, op.Field, op.Text)

	case OpAddNode:
		return addNode(h, op)

	case OpDropNode:
		_, at := findNode(*h, op.Node)
		if at < 0 {
			return fmt.Errorf("no node %q in this page", op.Node)
		}
		h.Program.Nodes = append(h.Program.Nodes[:at:at], h.Program.Nodes[at+1:]...)
		kept := h.Program.Edges[:0:0]
		for _, edge := range h.Program.Edges {
			if edge.From() != op.Node && edge.To() != op.Node {
				kept = append(kept, edge)
			}
		}
		h.Program.Edges = kept
		return nil

	case OpAddEdge:
		from, to := op.Node, strings.TrimSpace(op.Text)
		if _, found := h.Program.Node(from); !found {
			return fmt.Errorf("no node %q to draw an edge from", from)
		}
		if _, found := h.Program.Node(to); !found {
			return fmt.Errorf("no node %q to draw an edge to", to)
		}
		if from == to {
			return fmt.Errorf("an edge from %q to itself", from)
		}
		for _, edge := range h.Program.Edges {
			if edge.From() == from && edge.To() == to {
				return fmt.Errorf("the edge %s->%s is already drawn", from, to)
			}
		}
		h.Program.Edges = append(h.Program.Edges, Edge{from, to})
		return nil

	case OpDropEdge:
		from, to := op.Node, strings.TrimSpace(op.Text)
		for at, edge := range h.Program.Edges {
			if edge.From() == from && edge.To() == to {
				h.Program.Edges = append(h.Program.Edges[:at:at], h.Program.Edges[at+1:]...)
				return nil
			}
		}
		return fmt.Errorf("no edge %s->%s in this page", from, to)

	case OpSetVerify:
		rung := strings.TrimSpace(op.Text)
		if VerifyRung(rung) < 0 {
			return fmt.Errorf("%q is not on the verify ladder (%s)", rung, strings.Join(verifyLadder, " < "))
		}
		if op.Node != "" {
			return setField(h, op.Node, "ladder", rung)
		}
		h.Verify.Ladder = rung
		return nil

	case OpSetDyn:
		return setDyn(h, op)

	case OpSetWhitelist:
		h.Whitelist = splitList(op.Text)
		return nil

	case OpSetDesc:
		if strings.TrimSpace(op.Text) == "" {
			return fmt.Errorf("set_desc with no text: detection would have nothing to match")
		}
		h.Id.Desc = strings.TrimSpace(op.Text)
		return nil

	default:
		return fmt.Errorf("%q is not an op (%s)", op.Op, strings.Join(opNames(), ", "))
	}
}

func setField(h *Harness, id, field, text string) error {
	_, at := findNode(*h, id)
	if at < 0 {
		return fmt.Errorf("no node %q in this page", id)
	}
	node := h.Program.Nodes[at]
	if strings.TrimSpace(text) == "" {
		delete(node.Fields, field)
		return nil
	}
	if node.Fields == nil {
		node.Fields = Fields{}
		h.Program.Nodes[at] = node
	}
	node.Fields[field] = text
	return nil
}

// addNode decodes the node the same way a page from disk is decoded — unknown
// fields refused — because a node that arrives by patch and a node that arrives
// by file are the same object and one of them being read loosely is how a page
// acquires a field nothing reads.
func addNode(h *Harness, op Op) error {
	if len(op.NodeJSON) == 0 {
		return fmt.Errorf("add_node with no node_json")
	}
	decoder := json.NewDecoder(strings.NewReader(string(op.NodeJSON)))
	decoder.DisallowUnknownFields()
	var node Node
	if err := decoder.Decode(&node); err != nil {
		return fmt.Errorf("node_json does not read as a node: %w", err)
	}
	if !ValidName(node.Id) {
		return fmt.Errorf("node id %q is not a slug of at most %d bytes", node.Id, MaxIdBytes)
	}
	if _, taken := h.Program.Node(node.Id); taken {
		return fmt.Errorf("node %q is already in this page", node.Id)
	}
	if _, registered := Lookup(node.Kind); !registered {
		return fmt.Errorf("node %q has an unregistered kind %q (kinds: %s)", node.Id, node.Kind, strings.Join(kindNames(), ", "))
	}
	node.Fields = cloneFields(node.Fields)
	h.Program.Nodes = append(h.Program.Nodes, node)
	return nil
}

// setDyn takes the rung and the budget separately, because they change for
// different reasons — a rung is an argument about autonomy, a cap is a count —
// and it accepts the two together as "width 3" for the critic that thinks of
// them as one move.
func setDyn(h *Harness, op Op) error {
	field, text := strings.ToLower(strings.TrimSpace(op.Field)), strings.TrimSpace(op.Text)
	if field == "" {
		words := strings.Fields(text)
		switch len(words) {
		case 1:
			field, text = "ladder", words[0]
		case 2:
			if err := setDyn(h, Op{Op: OpSetDyn, Field: "ladder", Text: words[0]}); err != nil {
				return err
			}
			field, text = "cap", words[1]
		default:
			return fmt.Errorf("set_dyn wants field \"ladder\" or \"cap\" (or a text like %q)", "width 3")
		}
	}
	switch field {
	case "ladder":
		if DynRung(text) < 0 {
			return fmt.Errorf("%q is not on the dynamism ladder (%s)", text, strings.Join(dynLadder, " < "))
		}
		h.Dyn.Ladder = text
		return nil
	case "cap":
		budget, err := strconv.Atoi(text)
		if err != nil {
			return fmt.Errorf("the cap %q is not an integer", text)
		}
		if budget < 0 || budget > MaxDynCap {
			return fmt.Errorf("the cap %d is outside 0..%d", budget, MaxDynCap)
		}
		h.Dyn.Cap = budget
		return nil
	default:
		return fmt.Errorf("set_dyn takes field \"ladder\" or \"cap\", not %q", op.Field)
	}
}

func findNode(h Harness, id string) (Node, int) {
	for at, node := range h.Program.Nodes {
		if node.Id == id {
			return node, at
		}
	}
	return Node{}, -1
}

// clone is what makes [Apply] pure. A shallow copy of a Harness shares its
// nodes' Fields maps with the original, and a patch that wrote through one of
// them would edit the draft a caller is about to print beside the revision.
func (h Harness) clone() Harness {
	out := h
	out.Program.Nodes = make([]Node, len(h.Program.Nodes))
	for at, node := range h.Program.Nodes {
		node.Fields = cloneFields(node.Fields)
		out.Program.Nodes[at] = node
	}
	out.Program.Edges = append([]Edge(nil), h.Program.Edges...)
	out.Whitelist = append([]string(nil), h.Whitelist...)
	out.Tests = append([]Test(nil), h.Tests...)
	return out
}

func cloneFields(f Fields) Fields {
	if f == nil {
		return nil
	}
	out := make(Fields, len(f))
	for name, value := range f {
		out[name] = value
	}
	return out
}

func opNames() []string {
	return []string{
		OpReplaceBrief, OpSetField, OpAddNode, OpDropNode,
		OpAddEdge, OpDropEdge, OpSetVerify, OpSetDyn,
		OpSetWhitelist, OpSetDesc,
	}
}

// OpNames lists the verbs, for a brief that has to tell a model what it may do.
func OpNames() []string { return opNames() }

func firstWord(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func clipOp(text string, at int) string {
	text = strings.TrimSpace(text)
	if len(text) <= at {
		return text
	}
	return text[:at] + "…"
}
