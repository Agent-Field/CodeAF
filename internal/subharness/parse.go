package subharness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// namePattern is the law for a harness name and for a node id alike. A name
// becomes a path segment and an id becomes a word in a trail, so both are held
// to a slug that cannot escape a directory, confuse a diff, or need quoting.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ValidName reports whether a name or node id obeys the slug law.
func ValidName(name string) bool {
	return len(name) <= MaxIdBytes && namePattern.MatchString(name)
}

// Decode reads a saved page. The wire format is strict JSON with unknown
// fields refused: a page is written by this package and by the distiller, and
// a field neither of them knows is a version skew worth an error rather than a
// silently dropped instruction.
func Decode(data []byte) (Harness, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var h Harness
	if err := decoder.Decode(&h); err != nil {
		return Harness{}, fmt.Errorf("subharness: decode: %w", err)
	}
	return h.Normalize(), nil
}

// integerOrString admits the one harmless liberty a person or designer takes
// when writing a page by hand. Keeping the coercion here leaves every other
// type strict, and Encode still has one canonical spelling for integers.
func integerOrString(data json.RawMessage, field string) (int, error) {
	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		return number, nil
	}
	var word string
	if err := json.Unmarshal(data, &word); err != nil {
		return 0, fmt.Errorf("%s must be an integer or an integer string", field)
	}
	value, err := strconv.Atoi(strings.TrimSpace(word))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer or an integer string, got %q", field, word)
	}
	return value, nil
}

// UnmarshalJSON accepts the one liberty a designer takes with a node's
// arguments: scalars spelled the JSON way - "max_turns": 6 rather than "6".
// Numbers and booleans are coerced to their canonical string form, so the map
// stays strings on the wire and Encode keeps one spelling. Objects, arrays,
// and null are refused with the field named.
func (f *Fields) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	fields := make(Fields, len(raw))
	for name, value := range raw {
		switch trimmed := bytes.TrimSpace(value); {
		case len(trimmed) == 0:
			return fmt.Errorf("fields.%s must be a string, number, or boolean", name)
		case trimmed[0] == '"':
			var word string
			if err := json.Unmarshal(trimmed, &word); err != nil {
				return fmt.Errorf("fields.%s: %w", name, err)
			}
			fields[name] = word
		case trimmed[0] == '-' || trimmed[0] >= '0' && trimmed[0] <= '9':
			var number json.Number
			if err := json.Unmarshal(trimmed, &number); err != nil {
				return fmt.Errorf("fields.%s: %w", name, err)
			}
			fields[name] = number.String()
		case string(trimmed) == "true" || string(trimmed) == "false":
			fields[name] = string(trimmed)
		default:
			return fmt.Errorf("fields.%s must be a string, number, or boolean", name)
		}
	}
	*f = fields
	return nil
}

// UnmarshalJSON accepts a hand-written string for the identity's integer while
// preserving the page's refusal of fields this version does not know.
func (id *Id) UnmarshalJSON(data []byte) error {
	var page struct {
		Name    string          `json:"name"`
		Desc    string          `json:"desc,omitempty"`
		Author  string          `json:"author,omitempty"`
		Version json.RawMessage `json:"version"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&page); err != nil {
		return err
	}
	version := 0
	if len(page.Version) != 0 {
		var err error
		version, err = integerOrString(page.Version, "id.version")
		if err != nil {
			return err
		}
	}
	*id = Id{Name: page.Name, Desc: page.Desc, Author: page.Author, Version: version}
	return nil
}

// UnmarshalJSON gives the dynamism budget the same narrow hand-written form as
// version without making the rest of the page permissive.
func (dyn *Dyn) UnmarshalJSON(data []byte) error {
	var page struct {
		Ladder string          `json:"ladder"`
		Cap    json.RawMessage `json:"cap,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&page); err != nil {
		return err
	}
	cap := 0
	if len(page.Cap) != 0 {
		var err error
		cap, err = integerOrString(page.Cap, "dyn.cap")
		if err != nil {
			return err
		}
	}
	*dyn = Dyn{Ladder: page.Ladder, Cap: cap}
	return nil
}

// Encode writes a page: indented, newline-terminated, and with HTML escaping
// off so a brief that contains an ampersand reads back the way it was written.
func Encode(h Harness) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(h.Normalize()); err != nil {
		return nil, fmt.Errorf("subharness: encode: %w", err)
	}
	return buffer.Bytes(), nil
}

// Normalize fills the two defaults a file may leave out. An omitted rung means
// the least of that ladder — believe the output, decide nothing — because the
// safe reading of silence is the timid one.
func (h Harness) Normalize() Harness {
	if strings.TrimSpace(h.Verify.Ladder) == "" {
		h.Verify.Ladder = VerifyAccept
	}
	if strings.TrimSpace(h.Dyn.Ladder) == "" {
		h.Dyn.Ladder = DynFixed
	}
	return h
}

// Validate is the whole law of a sub-harness, and it is one function because
// the checks are not independent: a node's kind decides which fields it may
// carry, the harness's dynamism rung decides which kinds it may carry at all,
// and the whitelist decides which tools those fields may name. Splitting them
// would let a caller run two thirds of the law.
func Validate(h Harness) error {
	h = h.Normalize()

	if !ValidName(h.Id.Name) {
		return fmt.Errorf("subharness: name %q is not a slug of at most %d bytes", h.Id.Name, MaxIdBytes)
	}
	if h.Id.Version < 0 || h.Id.Version > MaxVersion {
		return fmt.Errorf("subharness: %s: version %d is outside 0..%d", h.Id.Name, h.Id.Version, MaxVersion)
	}
	if VerifyRung(h.Verify.Ladder) < 0 {
		return fmt.Errorf("subharness: %s: verify ladder %q is not one of %s",
			h.Id.Name, h.Verify.Ladder, strings.Join(verifyLadder, "|"))
	}
	dyn := DynRung(h.Dyn.Ladder)
	if dyn < 0 {
		return fmt.Errorf("subharness: %s: dynamism ladder %q is not one of %s",
			h.Id.Name, h.Dyn.Ladder, strings.Join(dynLadder, "|"))
	}
	// A fixed harness decides nothing, so a budget for deciding is a
	// contradiction rather than a harmless extra. Everything above fixed has
	// to say what it may spend: an unbounded ladder rung is the failure mode
	// this whole field exists to prevent.
	switch {
	case dyn == DynRung(DynFixed) && h.Dyn.Cap != 0:
		return fmt.Errorf("subharness: %s: a fixed harness may not carry a dynamism cap (%d)", h.Id.Name, h.Dyn.Cap)
	case dyn > DynRung(DynFixed) && (h.Dyn.Cap < 1 || h.Dyn.Cap > MaxDynCap):
		return fmt.Errorf("subharness: %s: dynamism cap %d is outside 1..%d", h.Id.Name, h.Dyn.Cap, MaxDynCap)
	}

	if err := validateWhitelist(h); err != nil {
		return err
	}
	if err := validateNodes(h, dyn); err != nil {
		return err
	}
	if err := validateEdges(h); err != nil {
		return err
	}
	if err := validateReach(h); err != nil {
		return err
	}
	if err := validateLadderEvidence(h); err != nil {
		return err
	}
	return validateTests(h)
}

func validateWhitelist(h Harness) error {
	seen := make(map[string]bool, len(h.Whitelist))
	for _, tool := range h.Whitelist {
		switch {
		case strings.TrimSpace(tool) == "":
			return fmt.Errorf("subharness: %s: whitelist carries a blank tool", h.Id.Name)
		case tool != strings.TrimSpace(tool):
			return fmt.Errorf("subharness: %s: whitelisted tool %q is padded with whitespace", h.Id.Name, tool)
		case seen[tool]:
			return fmt.Errorf("subharness: %s: whitelisted tool %q is listed twice", h.Id.Name, tool)
		}
		seen[tool] = true
	}
	return nil
}

func validateNodes(h Harness, dyn int) error {
	switch {
	case len(h.Program.Nodes) == 0:
		return fmt.Errorf("subharness: %s: a program with no nodes", h.Id.Name)
	case len(h.Program.Nodes) > MaxNodes:
		return fmt.Errorf("subharness: %s: %d nodes is past the cap of %d",
			h.Id.Name, len(h.Program.Nodes), MaxNodes)
	}
	seen := make(map[string]bool, len(h.Program.Nodes))
	for _, node := range h.Program.Nodes {
		if !ValidName(node.Id) {
			return fmt.Errorf("subharness: %s: node id %q is not a slug of at most %d bytes",
				h.Id.Name, node.Id, MaxIdBytes)
		}
		if seen[node.Id] {
			return fmt.Errorf("subharness: %s: node %q is declared twice", h.Id.Name, node.Id)
		}
		seen[node.Id] = true

		kind, registered := Lookup(node.Kind)
		if !registered {
			return fmt.Errorf("subharness: %s: node %q has an unregistered kind %q (kinds: %s)",
				h.Id.Name, node.Id, node.Kind, strings.Join(kindNames(), ", "))
		}
		if DynRung(kind.MinDyn) > dyn {
			return fmt.Errorf("subharness: %s: node %q is a %s, which needs dynamism %q, but the harness declared %q",
				h.Id.Name, node.Id, node.Kind, kind.MinDyn, h.Dyn.Ladder)
		}
		if err := kind.Valid(node.Fields); err != nil {
			return fmt.Errorf("subharness: %s: node %q (%s): %w", h.Id.Name, node.Id, node.Kind, err)
		}
		if err := validateNodeAgainstHarness(h, node); err != nil {
			return err
		}
	}
	return nil
}

// validateNodeAgainstHarness holds the three laws a node cannot check alone,
// because each one reads something outside the node: the whitelist, and the
// harness's own verification rung.
func validateNodeAgainstHarness(h Harness, node Node) error {
	switch node.Kind {
	case KindToolCall:
		tool := node.Fields.Get("tool")
		if !h.Allows(tool) {
			return fmt.Errorf("subharness: %s: node %q calls %q, which is not on the whitelist", h.Id.Name, node.Id, tool)
		}
	case KindAgentLoop:
		for _, tool := range splitList(node.Fields.Get("tools")) {
			if !h.Allows(tool) {
				return fmt.Errorf("subharness: %s: node %q hands out %q, which is not on the whitelist", h.Id.Name, node.Id, tool)
			}
		}
	case KindVerify:
		// A node may verify less hard than the harness — a cheap schema check
		// early, the adversarial pass at the end — but it may not verify
		// harder, because then the harness's declared rung is a lie about the
		// strongest thing it does.
		if word := node.Fields.Get("ladder"); word != "" && VerifyRung(word) > VerifyRung(h.Verify.Ladder) {
			return fmt.Errorf("subharness: %s: node %q verifies at %q, above the harness's %q",
				h.Id.Name, node.Id, word, h.Verify.Ladder)
		}
	case KindSubharnessCall:
		if name := node.Fields.Get("name"); !ValidName(name) {
			return fmt.Errorf("subharness: %s: node %q calls %q, which is not a harness name", h.Id.Name, node.Id, name)
		}
	case KindTrigger:
		// The trigger's law reads the edges as well as the node, so it is
		// written where the other things about hosting are (trigger.go).
		return validateTrigger(h, node)
	}
	return nil
}

func validateEdges(h Harness) error {
	seen := make(map[Edge]bool, len(h.Program.Edges))
	for _, edge := range h.Program.Edges {
		if _, found := h.Program.Node(edge.From()); !found {
			return fmt.Errorf("subharness: %s: edge %s leaves a node that does not exist", h.Id.Name, edge)
		}
		if _, found := h.Program.Node(edge.To()); !found {
			return fmt.Errorf("subharness: %s: edge %s enters a node that does not exist", h.Id.Name, edge)
		}
		if edge.From() == edge.To() {
			return fmt.Errorf("subharness: %s: node %q edges to itself", h.Id.Name, edge.From())
		}
		if seen[edge] {
			return fmt.Errorf("subharness: %s: edge %s is declared twice", h.Id.Name, edge)
		}
		seen[edge] = true
	}
	return nil
}

// validateReach is the DAG law in one place: a program has one way in, no way
// back, and no node that cannot be got to. The single entry is what makes
// "run this harness" a sentence with one meaning — a second entry is either a
// second harness that got pasted in, or a node whose edge was forgotten, and
// both are worth refusing at save time rather than discovering in a trace.
func validateReach(h Harness) error {
	order, err := topo(h.Program)
	if err != nil {
		return fmt.Errorf("subharness: %s: %w", h.Id.Name, err)
	}
	var entries []string
	for _, node := range h.Program.Nodes {
		if len(h.Program.Predecessors(node.Id)) == 0 {
			entries = append(entries, node.Id)
		}
	}
	if len(entries) != 1 {
		return fmt.Errorf("subharness: %s: a program has one entry, this one has %d (%s)",
			h.Id.Name, len(entries), strings.Join(entries, ", "))
	}
	reached := map[string]bool{entries[0]: true}
	for _, id := range order {
		if !reached[id] {
			continue
		}
		for _, next := range h.Program.Successors(id) {
			reached[next] = true
		}
	}
	for _, node := range h.Program.Nodes {
		if !reached[node.Id] {
			return fmt.Errorf("subharness: %s: node %q cannot be reached from %q", h.Id.Name, node.Id, entries[0])
		}
	}
	return nil
}

// validateLadderEvidence refuses a harness that claims a verification rung and
// then contains nothing that could do the verifying. A rung is a promise about
// what a run's output went through, and a program with no verify node at
// `adversarial` has made that promise with no way to keep it.
func validateLadderEvidence(h Harness) error {
	if VerifyRung(h.Verify.Ladder) <= VerifyRung(VerifyAccept) {
		return nil
	}
	var verifies, gates int
	for _, node := range h.Program.Nodes {
		switch node.Kind {
		case KindVerify:
			verifies++
		case KindHumanGate:
			gates++
		}
	}
	if verifies == 0 {
		return fmt.Errorf("subharness: %s: verify ladder %q with no verify node in the program",
			h.Id.Name, h.Verify.Ladder)
	}
	if h.Verify.Ladder == VerifyHuman && gates == 0 {
		return fmt.Errorf("subharness: %s: verify ladder %q with no human.gate in the program",
			h.Id.Name, h.Verify.Ladder)
	}
	return nil
}

func validateTests(h Harness) error {
	seen := make(map[string]bool, len(h.Tests))
	for index, test := range h.Tests {
		name := strings.TrimSpace(test.Name)
		if name == "" {
			return fmt.Errorf("subharness: %s: test %d has no name", h.Id.Name, index)
		}
		if seen[name] {
			return fmt.Errorf("subharness: %s: test %q is declared twice", h.Id.Name, name)
		}
		seen[name] = true
	}
	return nil
}

// topo orders the program so that every node follows its predecessors, and
// reports a cycle as an error naming the nodes still standing when the walk
// ran out of ready work. Ties break on program order, which is what makes a
// run reproducible across two decodings of the same page.
func topo(p Program) ([]string, error) {
	remaining := make(map[string]int, len(p.Nodes))
	for _, node := range p.Nodes {
		remaining[node.Id] = 0
	}
	for _, edge := range p.Edges {
		remaining[edge.To()]++
	}
	order := make([]string, 0, len(p.Nodes))
	done := make(map[string]bool, len(p.Nodes))
	for len(order) < len(p.Nodes) {
		progress := false
		for _, node := range p.Nodes {
			if done[node.Id] || remaining[node.Id] > 0 {
				continue
			}
			done[node.Id] = true
			order = append(order, node.Id)
			for _, next := range p.Successors(node.Id) {
				remaining[next]--
			}
			progress = true
			break
		}
		if !progress {
			var stuck []string
			for _, node := range p.Nodes {
				if !done[node.Id] {
					stuck = append(stuck, node.Id)
				}
			}
			return nil, fmt.Errorf("the program is not a DAG: %s cycle back on each other", strings.Join(stuck, ", "))
		}
	}
	return order, nil
}

// splitList reads a comma-separated field into its words.
func splitList(value string) []string {
	var out []string
	for _, word := range strings.Split(value, ",") {
		if word = strings.TrimSpace(word); word != "" {
			out = append(out, word)
		}
	}
	return out
}

func kindNames() []string {
	all := Kinds()
	names := make([]string, len(all))
	for i, kind := range all {
		names[i] = kind.Name
	}
	return names
}
