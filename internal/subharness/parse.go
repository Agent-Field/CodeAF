package subharness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// READING AND WRITING AN ENTRY.
//
// The file is harnesses/<name>.hjson and the format is HJSON's JSON subset: this
// package WRITES strict, indented JSON — which is valid HJSON — and READS a
// little more than it writes, so that a file somebody opened and annotated still
// loads. What "a little more" means is exactly two things, and they are the two
// people actually add by hand:
//
//   - COMMENTS. A # or // line, and a /* … */ block.
//   - TRAILING COMMAS, before a } or a ].
//
// It is not a general HJSON reader — unquoted keys and quoteless strings are not
// accepted — and that is a deliberate stopping point rather than a to-do. The
// value of the extension is that a person may open the file and write down WHY;
// the cost of the rest of the grammar is a hand-written parser standing between
// a registry entry and the disk, which is a place this product cannot afford a
// bug. If the day comes that somebody wants the whole grammar, it arrives as a
// dependency, not as more of this file.

// Extension is what a registry entry is called on disk.
const Extension = ".hjson"

// Decode reads one entry. The error names the harness where it can, because the
// caller is usually walking a directory and "invalid character" with no filename
// in it is a message about nothing.
func Decode(data []byte) (Harness, error) {
	var harness Harness
	stripped, err := stripComments(data)
	if err != nil {
		return harness, err
	}
	decoder := json.NewDecoder(bytes.NewReader(stripped))
	if err := decoder.Decode(&harness); err != nil {
		return harness, fmt.Errorf("subharness: %w", err)
	}
	return harness, nil
}

// Encode is the entry as it is written: strict JSON, indented, one trailing
// newline. Stable field order comes from the struct, which is what makes two
// saves of an unchanged harness produce identical bytes — a version bump should
// be visible in a diff as the one line that changed.
func Encode(h Harness) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(h); err != nil {
		return nil, fmt.Errorf("subharness: %w", err)
	}
	return out.Bytes(), nil
}

// stripComments removes the two things this reader accepts beyond JSON. It walks
// the bytes rather than using a regular expression because a # inside a string
// is a #, not a comment, and the only way to know which one you are looking at
// is to have tracked the quotes.
func stripComments(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data))
	for at := 0; at < len(data); {
		char := data[at]
		switch {
		case char == '"':
			// A string, copied verbatim to its closing quote — escapes included,
			// so a \" does not end it.
			start := at
			at++
			for at < len(data) {
				if data[at] == '\\' {
					at += 2
					continue
				}
				if data[at] == '"' {
					at++
					break
				}
				at++
			}
			if at > len(data) {
				at = len(data)
			}
			out = append(out, data[start:at]...)

		case char == '#' || (char == '/' && at+1 < len(data) && data[at+1] == '/'):
			for at < len(data) && data[at] != '\n' {
				at++
			}

		case char == '/' && at+1 < len(data) && data[at+1] == '*':
			end := bytes.Index(data[at+2:], []byte("*/"))
			if end < 0 {
				return nil, fmt.Errorf("subharness: a /* comment is never closed")
			}
			at += 2 + end + 2

		case char == ',':
			// A trailing comma is one whose next non-space character closes a
			// container. Dropping it here is what lets a person delete the last
			// node of a program without also having to delete a comma.
			next := at + 1
			for next < len(data) && isSpace(data[next]) {
				next++
			}
			if next < len(data) && (data[next] == '}' || data[next] == ']') {
				at++
				continue
			}
			out = append(out, char)
			at++

		default:
			out = append(out, char)
			at++
		}
	}
	return out, nil
}

func isSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r'
}

// ── validation ──────────────────────────────────────────────────────────────

// Validate is the whole admission rule for a program, and it is what stands
// between "the model wrote a harness" and "a person is looking at a card for
// it". Everything it can refuse, it refuses in a sentence a builder can act on:
// the caller hands these straight back to the model as a tool result.
//
// The order is the order the answers matter in. Identity first — a nameless
// harness has nothing to say about. Then the ladders, because every later
// judgement is made against them. Then the program, node by node, depth-first,
// with the path to the node in the message.
func Validate(h *Harness) error {
	if err := ValidName(h.Name); err != nil {
		return err
	}
	if !h.Verify.Valid() {
		return fmt.Errorf("verify %q is not on the ladder (%s)", h.Verify, rungWords())
	}
	if !h.Dynamism.Valid() {
		return fmt.Errorf("dynamism %q is not a rung (%s)", h.Dynamism, dynamismWords())
	}
	if h.Cap < 0 {
		return fmt.Errorf("cap cannot be negative")
	}
	if h.Version < 0 {
		return fmt.Errorf("version cannot be negative")
	}
	for _, tool := range h.Tools {
		if strings.TrimSpace(tool) == "" {
			return fmt.Errorf("the tool whitelist holds an empty name")
		}
	}
	if len(h.Program) == 0 {
		return fmt.Errorf("a harness needs at least one step")
	}
	if count := countNodes(h.Program); count > MaxNodes {
		return fmt.Errorf("has %d steps, over the ceiling of %d", count, MaxNodes)
	}
	// A TRIGGER IS A HEADER, NOT A STEP. It says what starts the harness, so it
	// can only be the thing at the top; one in the middle would be a program
	// claiming to be started by something halfway through itself.
	for at, node := range h.Program {
		if node.Kind == KindTrigger && at != 0 {
			return fmt.Errorf("step %d: a trigger may only be the first step", at+1)
		}
	}
	seen := map[string]bool{}
	for _, test := range h.Tests {
		if strings.TrimSpace(test.Name) == "" {
			return fmt.Errorf("every test needs a name")
		}
		if strings.TrimSpace(test.Expect) == "" {
			return fmt.Errorf("test %q needs an expectation", test.Name)
		}
	}
	return validateSteps(h, h.Program, "", 1, seen)
}

// validateSteps walks one list of steps at one depth. path is how a person would
// point at where they are — "3.b.1" — and it is built as the walk descends so an
// error about a node buried in a lane says where the node is.
func validateSteps(h *Harness, steps []Node, path string, depth int, seen map[string]bool) error {
	if depth > MaxDepth {
		return fmt.Errorf("step %s: nested %d deep, over the ceiling of %d", path, depth, MaxDepth)
	}
	for at, node := range steps {
		here := fmt.Sprintf("%d", at+1)
		if path != "" {
			here = path + "." + here
		}
		if err := validateNode(h, node, here, depth, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateNode(h *Harness, node Node, path string, depth int, seen map[string]bool) error {
	id := strings.TrimSpace(node.ID)
	if id == "" {
		return fmt.Errorf("step %s (%s) has no id", path, node.Kind)
	}
	// IDS ARE UNIQUE ACROSS THE WHOLE PROGRAM, nesting included. The trace keys
	// on them (run.go), and a run whose DAG has two nodes called "check" is a
	// run nobody can read afterwards.
	if seen[id] {
		return fmt.Errorf("step %s: two steps are both called %q", path, id)
	}
	seen[id] = true

	if err := h.permits(node.Kind); err != nil {
		return fmt.Errorf("step %s (%s): %w", path, id, err)
	}
	info, _ := lookup(node.Kind)
	if err := info.validate(h, node); err != nil {
		return fmt.Errorf("step %s (%s) %s: %w", path, id, node.Kind, err)
	}
	// A kind that does not nest may not carry children. Without this a branch
	// written with a `steps` field instead of `cases` would validate, register,
	// and then silently run nothing.
	if !info.nests && len(children(node)) > 0 {
		return fmt.Errorf("step %s (%s): %s carries nested steps, which it cannot run",
			path, id, node.Kind)
	}
	if !info.nests {
		return nil
	}
	for at, one := range node.Cases {
		if err := validateSteps(h, one.Steps, fmt.Sprintf("%s.%s", path, letter(at)), depth+1, seen); err != nil {
			return err
		}
	}
	if len(node.Else) > 0 {
		if err := validateSteps(h, node.Else, path+".else", depth+1, seen); err != nil {
			return err
		}
	}
	if err := validateSteps(h, node.Steps, path, depth+1, seen); err != nil {
		return err
	}
	for _, lane := range node.Lanes {
		if err := validateSteps(h, lane.Steps, path+"."+lane.Name, depth+1, seen); err != nil {
			return err
		}
	}
	return nil
}

// countNodes is the whole program's size, nesting included.
func countNodes(steps []Node) int {
	total := 0
	for _, node := range steps {
		total += 1 + countNodes(children(node))
	}
	return total
}

// letter is a case's label on the card and in an error: a, b, c…
func letter(at int) string {
	if at < 0 || at > 25 {
		return fmt.Sprintf("case%d", at+1)
	}
	return string(rune('a' + at))
}

// ── clamping ────────────────────────────────────────────────────────────────

// Clamp settles every number a file left silent or asked too much for, so that
// everything downstream — the card, the runner, the trace — reads settled values
// instead of each deciding for itself what a zero meant.
//
// It clamps rather than refuses, and only where a nearest legal answer is
// obviously what was meant: a loop that asked for a hundred rounds gets the
// ceiling, because "as many as you'll give me" has a right answer. Everything
// where the nearest legal answer would be a guess — an unknown kind, a tool
// outside the whitelist — is [Validate]'s to refuse.
func Clamp(h *Harness) {
	h.Name = strings.TrimSpace(strings.ToLower(h.Name))
	h.Description = strings.TrimSpace(h.Description)
	h.Author = strings.TrimSpace(h.Author)
	h.Verify = h.Verify.Or(RungAccept)
	h.Dynamism = h.Dynamism.Or(DynFixed)
	if h.Cap < 0 {
		h.Cap = 0
	}
	tools := make([]string, 0, len(h.Tools))
	for _, tool := range h.Tools {
		if tool = strings.TrimSpace(tool); tool != "" {
			tools = append(tools, tool)
		}
	}
	h.Tools = tools
	clampSteps(h.Program)
}

func clampSteps(steps []Node) {
	for at := range steps {
		clampNode(&steps[at])
	}
}

func clampNode(node *Node) {
	node.ID = strings.TrimSpace(node.ID)
	node.Note = strings.TrimSpace(node.Note)
	switch node.Kind {
	case KindAgentLoop:
		if node.MaxTurns <= 0 {
			node.MaxTurns = DefaultTurns
		}
		if node.MaxTurns > MaxTurns {
			node.MaxTurns = MaxTurns
		}
	case KindLoopUntil:
		if node.Max <= 0 {
			node.Max = DefaultRounds
		}
		if node.Max > MaxRounds {
			node.Max = MaxRounds
		}
	case KindParallelSplit:
		node.Join = node.Join.Or()
	case KindVerify:
		node.Rung = node.Rung.Or(RungAccept)
	}
	for at := range node.Cases {
		node.Cases[at].When = strings.TrimSpace(node.Cases[at].When)
		clampSteps(node.Cases[at].Steps)
	}
	clampSteps(node.Else)
	clampSteps(node.Steps)
	for at := range node.Lanes {
		node.Lanes[at].Name = strings.TrimSpace(node.Lanes[at].Name)
		clampSteps(node.Lanes[at].Steps)
	}
}

func dynamismWords() string {
	words := make([]string, 0, len(dynOrder))
	for _, rung := range dynOrder {
		words = append(words, string(rung))
	}
	return strings.Join(words, " < ")
}
