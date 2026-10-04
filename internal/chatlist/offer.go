package chatlist

// OfferKind is what a person may do with a row.
type OfferKind int

const (
	Open OfferKind = iota
	ContinueHere
	Merge
)

// Offer is the action a row invites and its exact copy.
type Offer struct {
	Kind OfferKind
	Line string
}

// offers maps each status to its offer; a table keeps OfferFor free of
// branching. Idle and Here open the chat with nothing said. A chat running on
// another device is continued like one that went off: the person who opens it
// decides it moves, and the takeover screen says where it runs now.
var offers = map[Status]func(Row) Offer{
	Off:     func(Row) Offer { return Offer{ContinueHere, OfferContinue} },
	Running: func(Row) Offer { return Offer{ContinueHere, OfferContinue} },
	Branch:  func(r Row) Offer { return Offer{Merge, StatusLine(r)} },
}

// OfferFor says what to offer for a row.
func OfferFor(r Row) Offer {
	if f, ok := offers[r.Status]; ok {
		return f(r)
	}
	return Offer{Kind: Open}
}
