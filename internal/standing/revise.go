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
//
// A REPORT MOVED ONTO A PATH IS A CLAIM ON IT (owner.go): refused while another
// live item keeps that path, decided under the path's lock with the revision
// inside it. So the path the change leads to has to be known before the item's
// lock is taken — a path's lock is never taken inside an item's — and change is
// applied once to a reading to learn it. CHANGE MUST THEREFORE ONLY ASSIGN: it
// is applied again, under the lock, to the item as it then is, and the fence on
// the spec revision makes the two the same specification.
func (s *Store) Revise(id string, expected uint64, change func(*Item) error) (Item, []string, error) {
	before, err := s.Get(id)
	if err != nil {
		return Item{}, nil, err
	}
	draft := before.withOwnScope()
	from, to := ReportPath(before), ""
	if change(&draft) == nil {
		to = ReportPath(draft)
	}
	if to == from || before.SpecRevision != expected {
		return s.revise(id, expected, change)
	}
	var (
		item    Item
		changed []string
	)
	claim := func() error {
		item, changed, err = s.revise(id, expected, change)
		return err
	}
	if to == "" {
		err = claim()
	} else {
		draft.ID = id
		err = s.claimReport(draft, claim)
	}
	if err != nil {
		return Item{}, nil, err
	}
	s.releaseReport(from, id)
	return item, changed, nil
}

// revise is [Store.Revise] under the item's lock.
func (s *Store) revise(id string, expected uint64, change func(*Item) error) (Item, []string, error) {
	var changed []string
	item, err := s.mutate(id, func(current *Item) error {
		if current.Status == StatusRetired {
			return ErrStopped
		}
		if current.SpecRevision != expected {
			return ErrConflict
		}
		draft := current.withOwnScope()
		if err := change(&draft); err != nil {
			return err
		}
		changed = SpecChanges(*current, draft)
		if len(changed) == 0 {
			return ErrUnchanged
		}
		schedule := !reflect.DeepEqual(current.When, draft.When)
		line := current.Does.Say != draft.Does.Say
		watched := current.When
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
			// changed. So does the count of failed checks and the wait they
			// earned, which were checks of a waking that no longer exists.
			due, err := firstDue(*current, s.now())
			if err != nil {
				return err
			}
			current.NextDue = due
			current.FailedChecks = 0
		}
		// A NEW WAKING OR A NEW LINE IS ASKED WHAT A NEW ITEM IS ASKED: whether
		// its pattern can be read, whether its condition has anything to be
		// judged against, and whether its line's placeholders are ones a firing
		// fills ([Item.CheckWatch]).
		if schedule || line {
			if err := current.CheckWatch(); err != nil {
				return err
			}
		}
		// A NEW PATTERN TAKES ITS BASELINE HERE, AT THE EDIT'S YES
		// ([Store.baseline]). THE SAME PATTERN KEEPS ITS READING (L3): an edit
		// of the condition alone, made because the judge kept failing, is not
		// a request to forget the changes those failures were holding.
		if schedule && (watched.Kind != WhenFile || current.When.Kind != WhenFile || watched.Glob != current.When.Glob) {
			s.baseline(current)
		}
		current.SpecRevision = expected + 1
		return nil
	})
	if err != nil {
		return Item{}, nil, err
	}
	return item, changed, nil
}

// withOwnScope is a copy of it whose folder scope a change can write without
// writing the original's.
func (it Item) withOwnScope() Item {
	if it.Scope != nil {
		scope := *it.Scope
		scope.CollectionIDs = append([]string(nil), it.Scope.CollectionIDs...)
		it.Scope = &scope
	}
	return it
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
