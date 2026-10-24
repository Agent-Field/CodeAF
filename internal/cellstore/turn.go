package cellstore

import "time"

// newTurn is the ONLY constructor of Turn (law L2): a turn exists because the
// harness sealed a snapshot, and every field is derived from that seal.
func newTurn(snapshot, parent string, at time.Time, info TurnInfo, receipt string, id Identity) Turn {
	return Turn{
		V:          schemaV,
		ID:         snapshot,
		Parent:     parent,
		SealedAtMs: at.UnixMilli(),
		Quality:    quality(info),
		Trigger:    triggerOf(info),
		Device:     id.Device,
		Fence:      id.Fence,
		Receipt:    receipt,
	}
}

func triggerOf(info TurnInfo) Trigger {
	if info.Trigger == "" {
		return AgentRun
	}
	return info.Trigger
}

// Identity names the writer of a seal: the device key and the lease fence it
// seals under. Stage 0 has neither a key nor a lease, so the zero Identity
// (an all-zero device, fence 0) is what it uses.
type Identity struct {
	Device string
	Fence  uint64
}

// zeroDevice is a device key of 32 zero bytes, in hex.
const zeroDevice = "0000000000000000000000000000000000000000000000000000000000000000"
