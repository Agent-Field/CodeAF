// Package clientproto defines the shared bounds and signed shapes of the
// scoped-client wire: the numbers the relays enforce, the enrollment a phone is
// admitted by, and the key binding of its pairing request. Executors and both
// relays read these from here, so a value one side accepts cannot be one the
// other side refuses, and the Go and TypeScript ports are held to one text.
package clientproto

import "time"

const (
	MaxSlotBytes = 64 << 10
	// MaxClientSlots allows each computer a card, an outcome slot and up to ten
	// read snapshots, so twelve per computer; 128 serves ten computers. The first
	// wire allowed 64, which is five.
	MaxClientSlots   = 128
	MaxQueueBytes    = 4 << 10
	MaxClientQueue   = 128
	MaxFabricBody    = 96 << 10
	MaxClientMethods = 64
	MaxAge           = 24 * time.Hour

	// MaxEnrollments is how many phones one identity may have enrolled at the relay.
	MaxEnrollments = 16
	// MaxEnrollNames bounds the methods and the reads of one enrollment.
	MaxEnrollNames = 64

	// MaxSlotWait is the longest a slot wait is held, in seconds.
	MaxSlotWait = 25
	// SlotWaitersPerClient lets a phone replace a dying connection without
	// letting one client exhaust the relay.
	SlotWaitersPerClient = 2
	// SlotWaitersPerRelay bounds the held waits of one Go relay process; the
	// Worker, whose identities are separate objects, bounds them per identity
	// with SlotWaitersPerIdentity.
	SlotWaitersPerRelay    = 1024
	SlotWaitersPerIdentity = 32
)

// The watching contract (design 3.7 and 3.9). A phone is watching while a slot
// wait is parked for it, or for WatchingHeaderFor after its last slot read or
// wait; the directory watch announces it for WatchingEventFor after that same
// moment, and sends at most one change per phone every WatchingDampen.
const (
	WatchingHeader     = "Codeaf-Client-Watching"
	WatchingHeaderFor  = 30 * time.Second
	WatchingEventFor   = 45 * time.Second
	WatchingDampen     = 2 * time.Second
	ServingParam       = "serving"
	WatchingEventType  = "watching"
	ClientLinkClass    = "client"
	ClientLinkCodeLen  = 10
	ClientLinkBindLen  = 16
	ClientLinkKeyBytes = 16
)
