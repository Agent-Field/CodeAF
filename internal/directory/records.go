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

import "github.com/Agent-Field/codeaf/internal/clusterstate"

// These aliases preserve the public API and legacy JSON while the pure records
// remain usable without linking the directory transports and stores.
type IdentityRec = clusterstate.IdentityRec
type Caps = clusterstate.Caps
type Device = clusterstate.Device
type Lease = clusterstate.Lease
type FrameHint = clusterstate.FrameHint

const MaxFrameHints = clusterstate.MaxFrameHints
const FrameHintBudget = clusterstate.FrameHintBudget

type Cell = clusterstate.Cell
type Listing = clusterstate.Listing
type CellView = clusterstate.CellView
type CellInit = clusterstate.CellInit
type Beat = clusterstate.Beat
type Publish = clusterstate.Publish

// withoutFrames drops every cell's frames from a list answer: the list is the
// home screen's hot read, and frame plans are for takers, who read the cell.
func withoutFrames(l Listing) Listing {
	for id, c := range l.Cells {
		c.Frames = nil
		c.FrameHints = nil
		l.Cells[id] = c
	}
	return l
}
