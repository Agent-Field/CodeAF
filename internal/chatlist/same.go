package chatlist

import (
	"slices"
	"time"
)

// Same reports whether two listings say the same thing about the chats.
//
// DurableAgo is left out on purpose: it is measured against the directory's
// clock at the moment of the read, so it differs between two reads of an
// unchanged list. DurableAt is the fact it comes from, and it is compared, so a
// turn that landed in between still counts as a change. This is how a client
// tells an unchanged answer without any help from the wire, which carries no
// version or etag.
func Same(a, b []Row) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if timeless(a[i]) != timeless(b[i]) || !slices.Equal(a[i].Copies, b[i].Copies) {
			return false
		}
	}
	return true
}

type timelessRow struct {
	cell, title, device, deviceID, parent string
	head                                  string
	status                                Status
	durableAt                             int64
	pending, orphanTurns                  uint32
	fence                                 uint64
	mine, released                        bool
	releasedAgo                           time.Duration
	holderRemoved, branchRedundant        bool
}

// timeless is a row with the read-time-dependent age and slice fields taken out.
func timeless(r Row) timelessRow {
	return timelessRow{
		cell:            r.Cell,
		title:           r.Title,
		device:          r.Device,
		deviceID:        r.DeviceID,
		parent:          r.Parent,
		head:            r.Head,
		status:          r.Status,
		durableAt:       r.DurableAt,
		pending:         r.Pending,
		orphanTurns:     r.OrphanTurns,
		fence:           r.Fence,
		mine:            r.Mine,
		released:        r.Released,
		releasedAgo:     r.ReleasedAgo,
		holderRemoved:   r.HolderRemoved,
		branchRedundant: r.BranchRedundant,
	}
}
