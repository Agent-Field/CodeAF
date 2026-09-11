package standing

// revise.go is the person changing what an item IS after it stands: the words,
// the brief, the cadence, the rails, the grant, the reach. Until this existed
// the only change was a stop and a fresh card, which threw away the history a
// person was changing the item in the light of.
//
// ── WHAT A REVISION TOUCHES, AND WHAT IT NEVER DOES ──
//
// Specification only. The runtime half — what was checked, what fired, what it
// cost, what is waiting for the person — is the item's past, and a revision is
// a statement about its future. Status is a control with its own door
// ([Store.SetStatus]) and exceptions have theirs ([Store.AddException]), so a
// revision can neither resume a paused item nor erase a "not here".
//
// ── THE FENCE IS THE SPEC REVISION, NOT THE DOCUMENT'S ──
//
// The document's own revision moves on every quiet check, so a person's edit
// fenced on it would fail every few minutes for no reason they could see. The
// fence is [Item.SpecRevision]: the caller says which version of the
// instructions it read, and a concurrent revision by somebody else is refused
// rather than silently overwritten.
//
// ── A RUN ALREADY UNDER WAY KEEPS WHAT IT WAS GIVEN ──
//
// A firing reads its brief when it starts, and its occurrence record names the
// spec revision it ran on ([Occurrence.Spec]). A revision reaches the NEXT
// occurrence. The pass's own writeback already refuses to replace a schedule
// that changed under it ([Store.recordRuntime]), so a new cadence saved while
// a run is working survives that run's return.

import (
	"errors"
	"reflect"
)

// ErrUnchanged means a revision asked for nothing different.
var ErrUnchanged = errors.New("nothing about the item would change")

// Revise applies change to the item's specification when the caller's reading
// of it is still current, and answers the item as written plus the names of
// the parts that changed. expected is the [Item.SpecRevision] the caller read.
func (s *Store) Revise(id string, expected uint64, change func(*Item) error) (Item, []string, error) {
	var changed []string
	item, err := s.mutate(id, func(current *Item) error {
		if current.Status == StatusRetired {
			return errors.New("a stopped item must be set up afresh")
		}
		if current.SpecRevision != expected {
			return ErrConflict
		}
		draft := *current
		if current.Scope != nil {
			scope := *current.Scope
			scope.CollectionIDs = append([]string(nil), current.Scope.CollectionIDs...)
			draft.Scope = &scope
		}
		if err := change(&draft); err != nil {
			return err
		}
		changed = SpecChanges(*current, draft)
		if len(changed) == 0 {
			return ErrUnchanged
		}
		schedule := !reflect.DeepEqual(current.When, draft.When)
		current.Words = draft.Words
		current.When = draft.When
		current.Does = draft.Does
		current.Rails = draft.Rails
		current.Brief = draft.Brief
		current.Grant = draft.Grant
		current.Scope = draft.Scope
		current.Altitude = draft.Altitude
		if schedule {
			// A NEW CADENCE STARTS FROM NOW. The old rhythm's next moment
			// belongs to a schedule the person has just replaced, and firing
			// on it would be the old order speaking once more after it was
			// changed. The fingerprint goes for the same reason: a file watch
			// on a new glob has no baseline yet.
			due, err := firstDue(*current, s.now())
			if err != nil {
				return err
			}
			current.NextDue = due
			current.Fingerprint = ""
			if err := current.CheckWatch(); err != nil {
				return err
			}
		}
		current.SpecRevision = expected + 1
		return nil
	})
	if err != nil {
		return Item{}, nil, err
	}
	return item, changed, nil
}

// SpecChanges names the specification parts that differ, in a fixed order so
// a log line reads the same way every time. THE NAMES ARE READ BY THE PERSON —
// in the item's log, in the terminal's receipt and on the chat's edit card —
// so they are the words a person would use for each part, never the field
// names. It is exported for that card, which says before the yes what
// [Store.Revise] will say after it.
func SpecChanges(before, after Item) []string {
	var changed []string
	add := func(name string, differs bool) {
		if differs {
			changed = append(changed, name)
		}
	}
	add("your words", before.Words != after.Words)
	add("what wakes it", !reflect.DeepEqual(before.When, after.When))
	add("instructions", before.Does.Brief != after.Does.Brief || before.Does.Say != after.Does.Say || before.Brief != after.Brief)
	add("how it runs", before.Does.Kind != after.Does.Kind || before.Does.Acceptance != after.Does.Acceptance ||
		before.Does.Model != after.Does.Model || before.Does.MaxSteps != after.Does.MaxSteps || before.Does.Effort != after.Does.Effort)
	add("report", before.Does.Report != after.Does.Report)
	add("limits", before.Rails != after.Rails)
	add("what it may do", before.Grant != after.Grant)
	add("which rules reach it", before.Altitude != after.Altitude || !reflect.DeepEqual(before.Scope, after.Scope))
	return changed
}
