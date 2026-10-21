package vaultsync

import (
	"errors"
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
)

// ErrDamaged is what a Medium answers when what it holds cannot be trusted as a
// copy: the vault then neither takes it nor replaces it.
var ErrDamaged = errors.New("vaultsync: the local copy is damaged")

// Medium is one machine's home for a carried slot: a file, or some fields of
// one. The vault does not care which, so each kind of state that travels is a
// Medium and there is one rule for all of them.
type Medium interface {
	// Read answers the content, and when it was last saved. os.ErrNotExist means
	// the machine holds nothing; ErrDamaged means what it holds is unusable.
	Read() (string, time.Time, error)
	// Write makes the medium hold content, and nothing else about it changes.
	Write(content string) error
	// Clear makes the medium hold nothing.
	Clear() error
	// Realized is the content Read would answer after Write(content): a medium
	// that cannot hold every part of content answers the part it can, so that
	// capture does not mistake the difference for a local edit.
	Realized(content string) string
}

// Carried is one slot of the vault and the Medium it is kept in here. The ID is
// the same on every machine, which is what lets merge compare their copies; the
// Scope matches no project, so a workspace .env never receives it.
type Carried struct {
	ID, Scope, Name string
	Medium          Medium
}

// capture copies the medium into the slot when it differs from the slot,
// stamped with the medium's save time rather than the time of capture: an edit
// made here yesterday must not beat a newer one another machine sent today just
// because this machine looked later. Nothing held tombstones the slot, which is
// how a removal travels; the vault leaves an already absent slot alone. A
// damaged medium is left out, since storing it would replace a good copy with
// one nobody can read.
func (c Carried) capture(v VaultFile) error { return c.record(v, c.removed) }

// captureEdits is capture without the removals: a medium that holds nothing is
// left alone. It is what runs before a pull, when this machine may simply not
// have been given the slot yet. A slot merged into the vault by another window
// (one that only starts up and syncs) has no file here, and reading that gap as
// a removal would tombstone the slot with a newer stamp than the machine that
// wrote it, so the pull would then delete the very file it came to bring.
func (c Carried) captureEdits(v VaultFile) error {
	return c.record(v, func(VaultFile) error { return nil })
}

// record copies what the medium holds into the slot, and asks absent what to do
// when it holds nothing.
func (c Carried) record(v VaultFile, absent func(VaultFile) error) error {
	content, at, err := c.Medium.Read()
	if errors.Is(err, ErrDamaged) {
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return absent(v)
	}
	if err != nil {
		return err
	}
	if held, err := v.Get(c.ID); err == nil && c.Medium.Realized(held.Value) == content {
		return nil
	}
	return v.PutAt(c.ID, keys.Entry{Name: c.Name, Value: content, Scope: c.Scope}, at)
}

// removed tombstones the slot, unless the slot holds nothing this machine could
// hold: an empty medium is then no removal, only a machine that lacks the part.
func (c Carried) removed(v VaultFile) error {
	if held, err := v.Get(c.ID); err == nil && c.Medium.Realized(held.Value) == "" {
		return nil
	}
	return v.Delete(c.ID)
}

// restore makes the medium match the slot: a live slot is written, a tombstone
// clears it, and a slot that never existed leaves the medium alone.
func (c Carried) restore(v VaultFile) error {
	held, err := v.Get(c.ID)
	if errors.Is(err, keys.ErrNotFound) {
		return c.restoreAbsence(v)
	}
	if err != nil {
		return err
	}
	return ignoreDamaged(c.Medium.Write(held.Value))
}

func (c Carried) restoreAbsence(v VaultFile) error {
	gone, err := v.Deleted(c.ID)
	if err != nil || !gone {
		return err
	}
	return ignoreDamaged(c.Medium.Clear())
}

// ignoreDamaged leaves a damaged medium as it is: the person may be mid-edit,
// and the next good save is captured as usual.
func ignoreDamaged(err error) error {
	if errors.Is(err, ErrDamaged) {
		return nil
	}
	return err
}
