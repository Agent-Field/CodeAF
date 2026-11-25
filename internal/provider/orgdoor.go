package provider

import "strings"

// ── THE ORGANIZATION'S MODEL DOOR SPEAKS FOR ITSELF ─────────────────────────
//
// A hosted chat reaches its model through the organization's server, and that
// server REFUSES a turn in its own words when a limit it keeps is reached: the
// chat's spending limit, the person's monthly limit. It refuses with an HTTP 402
// whose body is shaped like a provider's, so the client read it as a provider's.
// A 402 is "the account cannot pay", and the sentence a person got was
//
//	openrouter accepted the key but the account cannot pay — This chat has
//	reached its spending limit of $0.00, so it is paused. …
//
// which blames the vendor and the person's own account for a limit the
// ORGANIZATION chose, and puts a service name in front of a sentence that already
// says everything the person needs. The door's sentence is the whole answer.
//
// THE DOOR SAYS WHO IS SPEAKING, AND THE CLIENT NEVER GUESSES FROM THE PROSE. Every
// error the organization's door writes carries [DoorKey] set to
// [OrganizationDoor] inside the error envelope; a provider's own refusal never
// does, so a genuine "account cannot pay" from OpenRouter keeps the sentence it
// has always had. Reading the field rather than the sentence is the same rule the
// rest of this package keeps (refusalobject.go): a decision made by matching
// words is a decision that breaks the day somebody rewords them. The server
// (internal/factory/serve) and this reader both spell the field through these two
// constants, and a test feeds the real handler's bytes to the real reader so a
// drift between them fails there.

const (
	// DoorKey is the field of the error envelope that names who wrote the error.
	// It is the JSON tag of [errorBody]'s Door field, which a test pins to this.
	DoorKey = "door"
	// OrganizationDoor is the value an organization's model door writes under
	// [DoorKey]: the sentence in this error is the organization's own.
	OrganizationDoor = "organization"
)

// OrgLimitWords is the organization's own sentence when this refusal is its model
// door saying a limit it keeps was reached, and false for every other refusal —
// a provider's own payment refusal included. It reads the door's mark and the
// refusal's payment shape together, so the door's other refusals (a stopped chat,
// too many requests at once) keep the endings they already had.
func (e *APIError) OrgLimitWords() (string, bool) {
	if e == nil || !e.OrgDoor || !e.AccountCannotPay() {
		return "", false
	}
	if said := strings.TrimSpace(e.Message); said != "" {
		return said, true
	}
	return "", false
}
