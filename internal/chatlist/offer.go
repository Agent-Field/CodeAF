package chatlist

import "fmt"

// OfferKind is what a person may do with a row.
type OfferKind int

const (
	Open OfferKind = iota
	ContinueHere
	Watching
	Merge
)

// Offer is the action a row invites and its exact copy.
type Offer struct {
	Kind OfferKind
	Line string
}

// offers maps each status to its offer; a table keeps OfferFor free of
// branching. Idle and Here open the chat with nothing said.
var offers = map[Status]func(Row) Offer{
	Off:     func(Row) Offer { return Offer{ContinueHere, OfferContinue} },
	Running: func(r Row) Offer { return Offer{Watching, fmt.Sprintf(runningOn, r.Device)} },
	Branch:  func(r Row) Offer { return Offer{Merge, StatusLine(r)} },
}

// OfferFor says what to offer for a row.
func OfferFor(r Row) Offer {
	if f, ok := offers[r.Status]; ok {
		return f(r)
	}
	return Offer{Kind: Open}
}
