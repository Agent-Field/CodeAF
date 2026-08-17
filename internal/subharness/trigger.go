package subharness

import (
	"fmt"
	"strings"
)

// The trigger is the only node kind that faces outward. Every other kind is
// reached because something upstream finished; a trigger is reached because
// something outside the harness happened — a person typed a command, the
// resident went idle, a watch fired, a source raised one of its own events.
//
// The hosted mode is the one with a seam in it. A hosted trigger is offered to a
// source, which mounts it as the command `/harness <name>`: the source owns the
// line the person types, this package owns what the words mean, and neither
// needs the other's types to agree on it. That is why [Source] is one method
// wide.
//
// WHAT THE TRIGGER ENTERS AT IS ITS SUCCESSOR, and it is not a field. The
// program already says what follows a node — that is what the edges are
// (registry.go) — and a trigger that named its entry in a field would be a
// second answer to a question the DAG has already answered, free to disagree
// with it the first time somebody rewired an edge.

// Hosted is one hosted trigger as the source receives it: everything needed to
// mount the command and route it back, and nothing about how the harness runs.
type Hosted struct {
	// Harness and Version identify the entry, pinned. The source stores the
	// version it mounted so a run started from this command is the harness the
	// person read about, not whatever was saved since.
	Harness string `json:"harness"`
	Version int    `json:"version"`

	// Command is the line, always `/harness <name>`.
	Command string `json:"command"`

	// AllowedArgs is the trigger's whitelist, copied so the source can complete
	// and refuse arguments without reading the harness.
	AllowedArgs []string `json:"allowed_args,omitempty"`

	// Entry is the node the command enters at — the trigger's successor — and
	// Node is the trigger itself, so a run can be attributed to the line that
	// started it.
	Entry string `json:"entry"`
	Node  string `json:"node"`

	// Desc is the harness's own one-liner, for whatever help the source renders.
	Desc string `json:"desc,omitempty"`
}

// Source is anything that can host a command: a chat surface, the TUI, an
// external command source. One method, because that is the entire seam — the
// source learns a command exists and where to send it, and nothing else about
// sub-harnesses at all.
type Source interface {
	AddTrigger(Hosted) error
}

// CommandFor is the hosted command line for a harness name. It is derived and
// never configurable: `/harness <name>` means one harness by construction, so
// two entries cannot collide on a line and no file can squat a word that belongs
// to the product.
func CommandFor(name string) string { return "/harness " + name }

// Host offers every hosted trigger in a harness to a source and returns what was
// mounted. The harness is validated first: a source should never be handed a
// command whose harness would refuse to run, because the failure would surface
// as a broken command line long after the file was written.
//
// A harness with no hosted trigger mounts nothing and is not an error — plenty
// of them are started by a watch, by idle, or by another harness calling them,
// and none of those should have to say so.
func Host(src Source, h Harness) ([]Hosted, error) {
	if src == nil {
		return nil, fmt.Errorf("subharness: no source to host %q on", h.Id.Name)
	}
	h = h.Normalize()
	if err := Validate(h); err != nil {
		return nil, err
	}
	var mounted []Hosted
	for _, node := range h.Program.Nodes {
		if node.Kind != KindTrigger || node.Fields.Get("source") != TriggerHosted {
			continue
		}
		entry := ""
		if successors := h.Program.Successors(node.Id); len(successors) > 0 {
			entry = successors[0]
		}
		one := Hosted{
			Harness:     h.Id.Name,
			Version:     h.Id.Version,
			Command:     CommandFor(h.Id.Name),
			AllowedArgs: splitList(node.Fields.Get("args")),
			Entry:       entry,
			Node:        node.Id,
			Desc:        h.Id.Desc,
		}
		if err := src.AddTrigger(one); err != nil {
			return mounted, fmt.Errorf("subharness %q: hosting %s: %w", h.Id.Name, node.Id, err)
		}
		mounted = append(mounted, one)
	}
	return mounted, nil
}

// HostAll offers every registered harness's head version to a source. It is the
// boot path: a surface that mounts commands asks once, and gets the registry as
// it stands on disk rather than as it stood when the process started.
//
// A harness that cannot be read or does not validate is SKIPPED rather than
// failing the whole mount, because one broken page must not be able to take
// every other harness's command off the surface with it.
func (s *Store) HostAll(src Source) ([]Hosted, error) {
	names, err := s.Names()
	if err != nil {
		return nil, err
	}
	var mounted []Hosted
	for _, name := range names {
		h, err := s.Load(name, 0)
		if err != nil {
			continue
		}
		one, err := Host(src, h)
		mounted = append(mounted, one...)
		if err != nil && len(one) > 0 {
			// The source itself refused a command it was offered. That is not a
			// bad page, it is a surface saying no, and carrying on would keep
			// asking it the same way.
			return mounted, err
		}
	}
	return mounted, nil
}

// Allows reports whether one argument word is on the whitelist. A `name=value`
// or `--name=value` word is judged by its name: the whitelist is about which
// arguments exist, and a value is the caller's business.
func (h Hosted) Allows(arg string) bool {
	name := argName(arg)
	if name == "" {
		return false
	}
	for _, allowed := range h.AllowedArgs {
		if argName(allowed) == name {
			return true
		}
	}
	return false
}

// Accepts checks a whole argument list and names the first word that is not
// allowed. It is the source's refusal, written once here so every source refuses
// the same way and with the same sentence.
func (h Hosted) Accepts(args []string) error {
	for _, arg := range args {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		if !h.Allows(arg) {
			if len(h.AllowedArgs) == 0 {
				return fmt.Errorf("%s takes no arguments", h.Command)
			}
			return fmt.Errorf("%s does not take %q; it takes %s",
				h.Command, argName(arg), strings.Join(h.AllowedArgs, ", "))
		}
	}
	return nil
}

func argName(arg string) string {
	arg = strings.TrimSpace(arg)
	arg = strings.TrimLeft(arg, "-")
	if eq := strings.IndexByte(arg, '='); eq >= 0 {
		arg = arg[:eq]
	}
	return arg
}

// validateTrigger is the trigger kind's half of Validate that the kind's own
// field table cannot state, because every line of it reads something outside the
// node: the edges, and what the node on the other end of them is.
func validateTrigger(h Harness, node Node) error {
	seen := make(map[string]bool)
	for _, arg := range splitList(node.Fields.Get("args")) {
		name := argName(arg)
		if name == "" {
			return fmt.Errorf("subharness: %s: node %q allows a blank argument", h.Id.Name, node.Id)
		}
		if seen[name] {
			return fmt.Errorf("subharness: %s: node %q allows the argument %q twice", h.Id.Name, node.Id, name)
		}
		seen[name] = true
	}
	if node.Fields.Get("source") != TriggerHosted {
		return nil
	}
	// A HOSTED TRIGGER ENTERS AT AN agent.loop. What a person types on a command
	// line is a sentence, and a sentence needs a reader: a hosted trigger wired
	// into a tool.call would be a command whose arguments nothing interprets.
	successors := h.Program.Successors(node.Id)
	if len(successors) == 0 {
		return nil
	}
	entry, found := h.Program.Node(successors[0])
	if !found {
		return nil
	}
	if entry.Kind != KindAgentLoop {
		return fmt.Errorf("subharness: %s: node %q is hosted as a command and enters at %q, which is a %s rather than an %s",
			h.Id.Name, node.Id, entry.Id, entry.Kind, KindAgentLoop)
	}
	return nil
}
