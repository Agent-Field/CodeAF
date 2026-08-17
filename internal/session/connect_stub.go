// STUB(connect): replaced by the owner branch on merge.
package session

// THE CONNECT LANE, as the surface needs it to exist.
//
// internal/tui3 draws the ask, opens the browser and reports the outcome; the
// session owns the three events and the two answers below. This file is the
// contract standing in for that work so the surface can be built and tested
// against it — every body here is inert, and the owner branch replaces the file
// whole.
//
// The three kinds carry their payload on Event's own connect fields, which are
// declared beside Event in session.go for the one reason Go gives: a struct
// cannot be extended from another file.

// The three kinds are APPENDED to the block in session.go — 18, 19, 20 as that
// block stands — which is where the owner branch declares them. They are spelled
// with the arithmetic written out because a stub that guessed a round number
// would be a stub whose events are a different three events from the real ones.
const (
	// EventConnectAsk asks the person whether aforge may connect one SaaS
	// account. It carries ConnectID — the token a surface hands back to
	// [Agent.ResolveConnect] — the service's id in Service, and the name a
	// person reads in ServiceName.
	EventConnectAsk EventKind = EventNotice + 1 + iota
	// EventConnectAuth says the sign-in has started and the browser is where it
	// finishes: Service names it, AuthURL is the link.
	EventConnectAuth
	// EventConnectDone closes one attempt. Service names it, Account is who was
	// connected — empty when nobody said — and Failed says it did not complete.
	EventConnectDone
)

// ResolveConnect answers one EventConnectAsk. It is idempotent: a second answer
// to the same id changes nothing.
func (a *Agent) ResolveConnect(id string, approve bool) {}

// PendingConnect is every offer still waiting for an answer, by id — what a
// surface that has just attached to a running session would have to draw.
func (a *Agent) PendingConnect() []string { return nil }

// NoteConnected tells the session an account is connected, so a conversation
// that was idle while the person connected it does not have to ask again.
func (a *Agent) NoteConnected(service, account string) {}
