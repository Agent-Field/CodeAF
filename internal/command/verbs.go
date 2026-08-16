package command

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The one door from a drawn verb to the thing it does.
//
// internal/registry is pure data: it says a page may offer `retire`, what that
// row is called, whether it asks first, and what it journals. Something has to
// turn a row the user pointed at into the same durable command the head writes
// when they say it instead — and it has to be the SAME command, or a product
// with two doors has two behaviours and only one of them is tested.
//
// So nothing here is an executor. Charter and service verbs journal exactly the
// kinds internal/head's rule and service tools journal, and internal/resident
// applies them in exactly one place; forgetting a belief goes through
// [Commander.RetractNotebook], which the belt's forget tool and the notebook
// page already share; the craft and skill verbs journal their own kinds and are
// applied beside the others. What this file owns is the mapping — id to kind to
// target — and the refusals a surface needs in words.

// ErrSteerVerb is what a verb that carries an argument answers with. Per the
// one-mouth law those verbs do not fire on a click: the surface seeds the
// composer with [registry.Entry.SteerFor] and the person finishes the sentence,
// because "make it Tuesdays instead" is a thing to say, not a form to fill.
// Firing it silently would mean this package inventing the words.
//
// It refuses even when the caller has words in hand, and that is the point: the
// head is what reads a new cadence or the reason a version is going back, and a
// second reader of the same sentence here would be a second interpretation of
// it. The three steering verbs (charter.cadence, belief.edit, craft.revert)
// reach their commands through the head's own tools.
var ErrSteerVerb = fmt.Errorf("this verb is said, not clicked")

// ErrUnknownVerb names a row this package has no wiring for. It is an error
// rather than a silent no-op so a surface drawing a verb nobody connected finds
// out at once instead of drawing a dead chip forever.
var ErrUnknownVerb = fmt.Errorf("no such verb")

// InvokeVerb fires one registry verb at one item and hands back the journaled
// command, or the zero command for the verbs that act at once.
//
// target is the item's own durable handle, and which handle that is belongs to
// the verb: a charter id, a service id, a belief's number, a workflow's name.
// words is the user's own wording when the surface has any — a reason, a new
// cadence — and is empty for the pure verbs.
//
// It does NOT confirm anything. A destructive row carries its question in
// [registry.Entry.Confirm] and the surface asks it before calling this, for the
// same reason the composer asks before a cancel: the consent belongs where the
// person is looking.
func (c *Commander) InvokeVerb(id, target, words string) (store.Command, error) {
	if c == nil || c.store == nil {
		return store.Command{}, fmt.Errorf("this window has no journal behind it")
	}
	entry, found := registry.ByID(strings.TrimSpace(id))
	if !found {
		return store.Command{}, fmt.Errorf("%w: %q", ErrUnknownVerb, id)
	}
	if entry.Steer != "" {
		return store.Command{}, fmt.Errorf("%w: %s", ErrSteerVerb, entry.Verb)
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return store.Command{}, fmt.Errorf("%s needs the thing it acts on", entry.Verb)
	}
	words = strings.TrimSpace(words)

	// The one verb that is not a journaled command: a belief is not graph work,
	// and retracting one is a fact going quiet. It goes through the method the
	// notebook and the belt already share rather than growing a second path.
	if id == "belief.forget" {
		seq, err := strconv.ParseInt(target, 10, 64)
		if err != nil || seq <= 0 {
			return store.Command{}, fmt.Errorf("forget needs the number shown beside the belief")
		}
		return store.Command{}, c.RetractNotebook(seq)
	}

	kind := entry.Journal.Kind
	if kind == "" {
		return store.Command{}, fmt.Errorf("%w: %s journals nothing", ErrUnknownVerb, id)
	}
	instruction := verbInstruction(id, kind, target, words)
	if instruction == "" {
		return store.Command{}, fmt.Errorf("%s needs to say what you want", entry.Verb)
	}
	return c.store.RequestCommand(store.Command{
		SessionID:   c.session(),
		Kind:        kind,
		Target:      target,
		Instruction: instruction,
	})
}

// verbInstruction is what the command carries. Every kind here already has an
// executor that reads this field, and each one reads it differently — the
// service kinds read an action word, the craft kinds read the user's prose, the
// charter kinds read the kind's own name when there is nothing to interpret —
// so this is one table of what each executor is expecting rather than a guess.
func verbInstruction(id string, kind store.CommandKind, target, words string) string {
	switch kind {
	case store.CommandServiceStop, store.CommandServiceRestart:
		// internal/resident's applyServiceCommand takes the stop reason from here
		// and ignores it for a restart.
		if words != "" {
			return words
		}
		return string(kind)
	case store.CommandServiceAutoRestart:
		// The one toggle. The surface knows which way it is going; the executor
		// reads the words the same way the head's own service tool spells them.
		if strings.Contains(strings.ToLower(words), "off") ||
			strings.Contains(strings.ToLower(words), "disable") {
			return "disable-auto-restart"
		}
		return "auto-restart"
	case store.CommandCraftRun:
		// The prose is where a workflow's parameters are read from. With none, the
		// plain statement of what was asked for is still an honest intent — and a
		// workflow that needs something it cannot find there asks for it.
		if words != "" {
			return words
		}
		return "run " + target
	case store.CommandCraftRetire, store.CommandSkillRetire:
		if words != "" {
			return words
		}
		return "you asked me to stop using this"
	}
	if words != "" {
		return words
	}
	return string(kind)
}
