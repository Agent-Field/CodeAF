// Package directory is the small shared table that says which device holds
// which chat and where its last durable turn is. Every write is a
// compare-and-swap decided by the pure rules in rules.go; the fake and every
// real implementation share those rules and one conformance suite.
//
// # Link pairing: the client API
//
// A new device joins an identity by asking, and an already-paired device
// answers. The wire is docs/ux-pairing-contract.md; this is how Go calls it.
//
// The new device has no identity yet, so it uses Requests (open, unsigned).
// Over HTTP that is NewLinkHTTP(base, hc); in process it is Links.From(peer).
//
//	asked, _ := reqs.CreateRequest(ctx, directory.NewRequest{
//		Pubkey: pub, X25519: box, NameSealed: sealed, Platform: "linux"})
//	// show asked.Code, asked.Check and the link https://codeaf.agentfield.ai/p/<code>#<k>
//	for {
//		r, err := reqs.GetRequest(ctx, asked.Code, 25*time.Second)
//		if errors.Is(err, directory.ErrStillPending) { continue }
//		// r.State is RequestApproved (r.Grant holds the sealed grant) or RequestDenied
//	}
//
// An already-paired device is a Client (signed). It reads the request with
// GetRequest on the same open Requests, compares Request.Check with the one
// the new device shows, and then answers once:
//
//	err := client.ApproveRequest(ctx, code, directory.Approval{Device: d, Cert: cert, Grant: grant})
//	err = client.DenyRequest(ctx, code)
//
// The first decision wins. A repeat of the same decision succeeds; the other
// decision is ErrAlreadyDecided. ErrRequestGone covers an unknown, expired or
// deleted code. Rate limits answer ErrRateLimited (wireauth.After says when to
// retry); the numbers are LinkLimits, read with LinkHTTP.Limits.
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

	// Added by link pairing. Old records decode with zero values, and a client
	// value for Created or LastSeen is ignored: the directory sets both.
	Platform string `json:"platform,omitempty"`  // darwin|linux|windows|ios|android|other
	Created  int64  `json:"created,omitempty"`   // directory ms when the device joined
	LastSeen int64  `json:"last_seen,omitempty"` // directory ms of the last watch socket close or hello
}

// visibleAt hides LastSeen: a device coming and going is not a change a person
// sees in the list, so it moves no directory version.
func (d Device) visibleAt(int64) any {
	d.LastSeen = 0
	return d
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
}

// Beat renews a lease the caller holds.
type Beat struct {
	Fence   uint64 `json:"fence"`
	Pending uint32 `json:"pending"`
}

// Publish moves a cell's durable head under a lease the caller holds.
type Publish struct {
	Fence   uint64 `json:"fence"`
	OldHead string `json:"old_head"`
	Head    string `json:"head"`
	Size    uint64 `json:"size"`
	Class   string `json:"class"`
	Title   string `json:"title,omitempty"` // empty keeps the current title
	Pending uint32 `json:"pending"`         // turns still not durable after this one
}
