// Package directory is the small shared table that says which device holds
// which chat and where its last durable turn is. Every write is a
// compare-and-swap decided by the pure rules in rules.go; the fake and every
// real implementation share those rules and one conformance suite.
package directory

// IdentityRec is the per-identity root record.
type IdentityRec struct {
	V        uint16 `json:"V"`
	Identity string `json:"identity"`        // id_…, derived from the signing key
	Vault    string `json:"vault,omitempty"` // rid of the current vault object
	// Rotation is set once the identity has been replaced by a rotation: it
	// takes no writes from then on. Nil means live.
	Rotation *Rotation `json:"rotation,omitempty"`
}

// Caps says what a device can do.
type Caps struct {
	OS        string  `json:"os"`        // runtime.GOOS
	Arch      string  `json:"arch"`      // runtime.GOARCH
	Sandbox   *string `json:"sandbox"`   // "seatbelt" | "landlock" | null
	Container *string `json:"container"` // null in Stage 1
	GPU       *string `json:"gpu"`       // null in Stage 1
	Cow       string  `json:"cow"`       // "apfs" | "btrfs" | "xfs-reflink" | "overlayfs" | "none"
}

// Device is keyed by device id (dev_…).
type Device struct {
	V       uint16 `json:"V"`
	Name    string `json:"name"`     // b64, sealed under the metadata key
	AddedBy string `json:"added_by"` // identity id that signed the device cert
	Revoked bool   `json:"revoked"`  // set only by Revoke; a fresh pairing makes a new device id
	Caps    Caps   `json:"caps"`
}

// Lease says who may write a cell. Only the directory clock sets Expires.
type Lease struct {
	Device  string `json:"device"`  // device id of the last holder, kept after release
	Fence   uint64 `json:"fence"`   // +1 on every acquire, never decreases
	Expires int64  `json:"expires"` // directory ms; held iff Expires > now; release sets 0
	Pending uint32 `json:"pending"` // turns sealed on the holder and not yet durable
}

// Cell is keyed by cell ULID. It exists only once it has a durable head.
type Cell struct {
	V           uint16                       `json:"V"`
	Head        string                       `json:"head"`       // last durable turn id (hex64)
	DurableAt   int64                        `json:"durable_at"` // directory ms
	Class       string                       `json:"class"`
	Size        uint64                       `json:"size"`
	ParentCell  string                       `json:"parent_cell,omitempty"`
	Title       string                       `json:"title,omitempty"` // b64, sealed
	Keys        map[string]map[string]string `json:"keys"`            // cell_key_id -> identity -> wrapped
	Lease       Lease                        `json:"lease"`
	OrphanTurns uint32                       `json:"orphan_turns,omitempty"` // turns in a death branch
	Archived    bool                         `json:"archived,omitempty"`     // a merged or discarded branch
	Frames      []string                     `json:"frames,omitempty"`       // hex64 ids of the frames holding the head's closure; a hint, never a condition
}

// Listing is everything a home list needs, in one read.
type Listing struct {
	Now      int64             `json:"now"` // directory ms when read
	Identity IdentityRec       `json:"identity"`
	Devices  map[string]Device `json:"devices"`
	Cells    map[string]Cell   `json:"cells"`
	// Version is the directory version the records were read at, which is
	// sent as the VersionHeader and not in the body.
	Version uint64 `json:"-"`
}

// withoutFrames drops every cell's frames from a list answer: the list is the
// home screen's hot read, and frame plans are for takers, who read the cell.
func withoutFrames(l Listing) Listing {
	for id, c := range l.Cells {
		c.Frames = nil
		l.Cells[id] = c
	}
	return l
}

// CellView is one cell with the directory time it was read at.
type CellView struct {
	Now  int64 `json:"now"`
	Cell Cell  `json:"cell"`
}

// CellInit creates a cell. The calling device holds the new lease at fence 1.
type CellInit struct {
	Head        string                       `json:"head"`
	Class       string                       `json:"class"`
	Title       string                       `json:"title,omitempty"`
	ParentCell  string                       `json:"parent_cell,omitempty"`
	Size        uint64                       `json:"size"`
	Keys        map[string]map[string]string `json:"keys"`
	OrphanTurns uint32                       `json:"orphan_turns,omitempty"`
	Frames      []string                     `json:"frames,omitempty"`
}

// Beat renews a lease the caller holds.
type Beat struct {
	Fence   uint64 `json:"fence"`
	Pending uint32 `json:"pending"`
}

// Publish moves a cell's durable head under a lease the caller holds.
type Publish struct {
	Fence   uint64   `json:"fence"`
	OldHead string   `json:"old_head"`
	Head    string   `json:"head"`
	Size    uint64   `json:"size"`
	Class   string   `json:"class"`
	Title   string   `json:"title,omitempty"`  // empty keeps the current title
	Pending uint32   `json:"pending"`          // turns still not durable after this one
	Frames  []string `json:"frames,omitempty"` // empty or absent replaces the record's plan with none
}
