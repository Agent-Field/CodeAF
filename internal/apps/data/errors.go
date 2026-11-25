package data

import (
	"errors"
	"fmt"
)

// The codes of an [Error]. They are the wire's codes, so the app door passes
// them through unchanged.
const (
	// CodeNotAllowed is a call an app's capabilities do not cover. Need names
	// the capability it would take, "records.contact.rw".
	CodeNotAllowed = "not_allowed"
	// CodeNotFound is a type, record, batch or draft that does not exist.
	CodeNotFound = "not_found"
	// CodeInvalid is a value, query or parameter that cannot be accepted.
	CodeInvalid = "bad_params"
	// CodeFrozen is a write to a record a freeze rule has made read-only.
	CodeFrozen = "frozen"
	// CodeStale is an undo of a batch whose records have changed since, or
	// that was already undone, or a write naming a revision the record has
	// moved past.
	CodeStale = "stale"
	// CodeLifecycle is a write to a field a lifecycle owns, which only a
	// transition moves, or a transition that cannot fire: the record is not
	// in a state it starts from, its guard does not hold, or this writer may
	// not fire it.
	CodeLifecycle = "lifecycle"
	// CodeLedger is a change or deletion of a ledger entry, which is only
	// ever reversed.
	CodeLedger = "ledger"
	// CodeDuplicate is a value a unique field already holds on another
	// record.
	CodeDuplicate = "duplicate"
	// CodeClosed is a call made after the store was closed.
	CodeClosed = "closed"
)

// Error is a refusal with a code a client can act on. Field names the field
// a validation refusal is about; Need names the capability a not_allowed
// refusal would take.
type Error struct {
	Code    string
	Message string
	Field   string
	Need    string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func fieldErr(field, format string, args ...any) *Error {
	return &Error{Code: CodeInvalid, Field: field, Message: fmt.Sprintf(format, args...)}
}

// notAllowed is the refusal for a capability an app did not declare.
func notAllowed(app, typ string, write bool) *Error {
	need := "records." + typ + ".r"
	verb := "read"
	if write {
		need += "w"
		verb = "change"
	}
	return &Error{Code: CodeNotAllowed, Need: need, Message: fmt.Sprintf("%s may not %s %s records", app, verb, typ)}
}

// AsError answers err as an [*Error] when it is one.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
