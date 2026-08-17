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
// needs the other's types to agree on it. That is why Source is one method wide.

// TriggerMode says what makes a trigger fire.
type TriggerMode string

const (
	// TriggerHosted is mounted on a source as `/harness <name>`.
	TriggerHosted TriggerMode = "hosted"
	// TriggerIdle fires when the resident has nothing else to do. Spec carries
	// how long the quiet must last.
	TriggerIdle TriggerMode = "idle"
	// TriggerWatch fires on a change the run cares about. Spec carries what is
	// watched, in the watching surface's own words.
	TriggerWatch TriggerMode = "watch"
	// TriggerSource fires on a command a source already has, rather than on one
	// this harness asked it to mount.
	TriggerSource TriggerMode = "source.command"
)

var triggerModes = map[TriggerMode]bool{
	TriggerHosted: true, TriggerIdle: true, TriggerWatch: true, TriggerSource: true,
}

// Trigger is a trigger node's payload.
type Trigger struct {
	Mode TriggerMode `json:"mode"`

	// Entry names the node the trigger hands control to. For a hosted trigger it
	// must be an agent.loop: what a person types is a sentence, and a sentence
	// needs a reader. A hosted trigger pointed at a tool.call would be a command
	// whose arguments nothing interprets.
	Entry string `json:"entry"`

	// Args is the allowed-args whitelist — the argument words this trigger
	// accepts after the harness name. Empty means the command takes none: an
	// empty whitelist grants nothing, the same way the entry's tool whitelist
	// does. A harness that wants free text takes it from the conversation the
	// agent.loop entry is already reading, not from an unbounded argv.
	Args []string `json:"args,omitempty"`

	// Source names the source for a source.command trigger, and Command is that
	// source's own command word. Hosted triggers leave both empty: their command
	// is derived, not chosen, so two harnesses can never claim one line.
	Source  string `json:"source,omitempty"`
	Command string `json:"command,omitempty"`

	// Spec is the idle interval or the watch expression, read by whoever hosts
	// that mode. This package does not interpret it; it only insists there is one.
	Spec string `json:"spec,omitempty"`
}

// Hosted is one hosted trigger as the source receives it: everything needed to
// mount the command and route it back, and nothing about how the harness runs.
type Hosted struct {
	// Harness and Revision identify the entry, pinned. The source stores the
	// revision it mounted so a run started from this command is the harness the
	// person read about, not whatever was saved since.
	Harness  string `json:"harness"`
	Revision int    `json:"revision"`

	// Command is the line, always `/harness <name>`.
	Command string `json:"command"`

	// AllowedArgs is the trigger's whitelist, copied so the source can complete
	// and refuse arguments without reading the entry.
	AllowedArgs []string `json:"allowed_args,omitempty"`

	// Entry is the agent.loop node the command enters at; Node is the trigger
	// node that offered it, so a run can be attributed to the line that started it.
	Entry string `json:"entry"`
	Node  string `json:"node"`

	// Desc is the entry's own one-liner, for whatever help the source renders.
	Desc string `json:"desc,omitempty"`
}

// Source is anything that can host a command: a chat surface, the TUI, an
// external command source. One method, because that is the entire seam — the
// source learns a command exists and where to send it, and nothing else about
// sub-harnesses at all.
type Source interface {
	AddTrigger(Hosted) error
}

// CommandFor is the hosted command line for an entry name. It is derived and
// never configurable: `/harness <name>` means one harness by construction, so
// two entries cannot collide on a line and no file can squat a word that
// belongs to the product.
func CommandFor(name string) string { return "/harness " + name }

// Host offers every hosted trigger in an entry to a source and returns what was
// mounted. The entry is validated first: a source should never be handed a
// command whose harness would refuse to run, because the failure would surface
// as a broken command line long after the file was written.
//
// An entry with no hosted triggers mounts nothing and is not an error — plenty
// of harnesses are started by a watch, by idle, or by another harness calling
// them, and none of them should have to say so.
func Host(src Source, e Entry) ([]Hosted, error) {
	if src == nil {
		return nil, fmt.Errorf("subharness: no source to host %q on", e.Name)
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	var mounted []Hosted
	for _, n := range e.Nodes {
		if n.Kind != KindTrigger || n.Trigger == nil || n.Trigger.Mode != TriggerHosted {
			continue
		}
		h := Hosted{
			Harness:     e.Name,
			Revision:    e.Revision,
			Command:     CommandFor(e.Name),
			AllowedArgs: append([]string(nil), n.Trigger.Args...),
			Entry:       n.Trigger.Entry,
			Node:        n.ID,
			Desc:        e.Desc,
		}
		if err := src.AddTrigger(h); err != nil {
			return mounted, fmt.Errorf("subharness %q: hosting %s: %w", e.Name, n.ID, err)
		}
		mounted = append(mounted, h)
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

// validateTrigger is the trigger kind's half of Entry.Validate.
func validateTrigger(_ Entry, n Node, byID map[string]Node) error {
	if err := onlyPayload(n); err != nil {
		return err
	}
	t := n.Trigger
	if t == nil {
		return fmt.Errorf("has no trigger")
	}
	if !triggerModes[t.Mode] {
		return fmt.Errorf("unknown trigger mode %q", t.Mode)
	}
	// A trigger is entered, never reached. Needs on a trigger would mean a start
	// that waits for a finish, and the DAG would have no root at all.
	if len(n.Needs) > 0 {
		return fmt.Errorf("a trigger starts a run and so may not need anything")
	}
	entry, ok := byID[t.Entry]
	if !ok {
		return fmt.Errorf("enters at unknown node %q", t.Entry)
	}
	if entry.Kind == KindTrigger {
		return fmt.Errorf("enters at %q, which is itself a trigger", t.Entry)
	}
	seen := make(map[string]bool, len(t.Args))
	for _, arg := range t.Args {
		name := argName(arg)
		if name == "" {
			return fmt.Errorf("has a blank allowed argument")
		}
		if seen[name] {
			return fmt.Errorf("allows the argument %q twice", name)
		}
		seen[name] = true
	}
	switch t.Mode {
	case TriggerHosted:
		if entry.Kind != KindAgentLoop {
			return fmt.Errorf("a hosted trigger must enter at an agent.loop, but %q is a %s", t.Entry, entry.Kind)
		}
		if t.Source != "" || t.Command != "" {
			return fmt.Errorf("a hosted trigger's command is %s and may not be named in the file", CommandFor("<name>"))
		}
	case TriggerSource:
		if strings.TrimSpace(t.Source) == "" {
			return fmt.Errorf("names no source")
		}
		if strings.TrimSpace(t.Command) == "" {
			return fmt.Errorf("names no command on source %q", t.Source)
		}
	case TriggerIdle, TriggerWatch:
		if strings.TrimSpace(t.Spec) == "" {
			return fmt.Errorf("a %s trigger needs a spec saying what it waits for", t.Mode)
		}
	}
	return nil
}
