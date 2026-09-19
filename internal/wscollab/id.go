package wscollab

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// DeliveryID names one durable delivery the way the local mailbox does:
// which conversation the news is about, which life of that work, and which
// ending. Fan-out keeps one id per recipient and shares the cause; a replay
// of the same triple is the same id, so the journal can tell a second event
// from the same landing told again.
type DeliveryID string

func mintDeliveryID(recipient, cause, pattern string) DeliveryID {
	// Task 0 is the conversation itself — the mailbox's address for the main
	// chat — so a cross-session line composes rather than inventing a scheme.
	return DeliveryID(fmt.Sprintf("%s/0@%s:%s", recipient, cause, pattern))
}

// MintActorID is the software door that names a participant. The model never
// supplies this value; a representative that invented its own id would be
// choosing who it is.
func MintActorID() (string, error) {
	return mintID()
}

func mintID() (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

func inspectOrigin(origin string) (string, error) {
	switch origin {
	case OriginPerson, OriginAgent, OriginRuntime:
		return origin, nil
	}
	return "", fmt.Errorf("%w: unknown origin %q", ErrInvalid, origin)
}
