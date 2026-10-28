package session

import (
	"errors"
	"fmt"
)

// ErrSavingUnanswered means a closing request has no reply yet; its save may still finish.
var ErrSavingUnanswered = errors.New("the final saving reply has not arrived")

// UnsavedChatError keeps the local recovery location beside a saving failure.
type UnsavedChatError struct {
	Transcript string
	Cause      error
}

func (e *UnsavedChatError) Error() string {
	return fmt.Sprintf("could not save this chat; unsaved turns are kept in %s: %v", e.Transcript, e.Cause)
}

func (e *UnsavedChatError) Unwrap() error { return e.Cause }

// SavingError adds the chat's recovery location to a seal or publish failure.
func SavingError(transcript string, cause error) error {
	if cause == nil {
		return nil
	}
	var kept *UnsavedChatError
	if errors.As(cause, &kept) {
		return cause
	}
	return &UnsavedChatError{Transcript: transcript, Cause: cause}
}
