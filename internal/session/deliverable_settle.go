package session

// deliverable_settle.go is what a merged landing may say about the documents
// the node's worker created on its branch.
//
// KEPT IS CLAIMED ONLY ON PROOF THAT EXACTLY THE AUTHORED BYTES ARRIVED
// THROUGH THIS LANDING. A regular file existing at the checkout path proves
// nothing: it may be the person's own file, an older one the work replaced,
// or the worker's file edited after it was made. A row is settled only when
// all of these hold, each read from one named object:
//   - the checkout's HEAD holds a regular-file blob at the row's rel, and it
//     is the very blob of the checkout file's bytes (its git id is computed
//     from the same read that hashes them), which hash to the sha256 recorded
//     when the worker created the file;
//   - the commit the task was cut from (the node's HomeSha, captured at the
//     cut) held NOTHING at that rel, so the file is new and not a replacement;
//   - that file is a regular file and no link, reached by a path with no
//     linked folder in it, read through its own descriptor, and still the file
//     at that path afterwards.
// Short of that the row stays exactly as it was - state unknown - and the
// branch it names is still on the record.
//
// IT IS BOUNDED AS ONE QUESTION. One context covers the whole settling
// (deliverSettleMax), the rows are found by ONE paged walk of the index (not a
// whole-history search per row), git is asked about a whole page in two calls,
// and a settling that runs out of time leaves
// every row it did not reach unknown. Settling again is always safe: a settled
// row is no longer unknown and is not visited twice.

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// deliverSettleMax is the whole time a landing may spend settling its rows.
var deliverSettleMax = 10 * time.Second

// settleDeliverables settles, after a merged landing, the rows this node's
// worker created. Called after the landing is written, with no lock held.
func (n *TaskNode) settleDeliverables(merge string) {
	ctx, cancel := context.WithTimeout(context.Background(), deliverSettleMax)
	defer cancel()
	n.settleDeliverablesIn(ctx, merge)
}

// settleDeliverablesIn is settleDeliverables under the caller's context, and
// answers how many rows it settled.
func (n *TaskNode) settleDeliverablesIn(ctx context.Context, merge string) int {
	if merge != mergeMerged {
		return 0
	}
	n.graph.mu.Lock()
	sink, base, task := n.deliverSink, n.HomeSha, strconv.FormatUint(n.id, 10)
	n.graph.mu.Unlock()
	if sink == nil || base == "" {
		return 0
	}
	top := sink.repoTop()
	if top == "" {
		return 0
	}
	settled := 0
	q := DeliverableQuery{Sessions: map[string]bool{sink.session: true}, States: map[string]bool{"unknown": true}}
	for cursor := ""; ctx.Err() == nil; {
		deadline, _ := ctx.Deadline()
		page, err := ListDeliverables(sink.index, q, cursor, MediaPageMax, deadline, 0, nil)
		if err != nil {
			return settled
		}
		settled += settlePage(ctx, sink.index, top, base, task, page.Rows)
		if page.Next == "" {
			return settled
		}
		cursor = page.Next
	}
	return settled
}

// settlePage settles one page's rows of this node, asking git about all of
// them in two calls: what HEAD holds at their paths, and what the commit the
// task was cut from held there.
func settlePage(ctx context.Context, index, top, base, task string, rows []DeliverableRow) int {
	var candidates []DeliverableRow
	var rels []string
	for _, row := range rows {
		if settleCandidate(row, task) && validBranchRel(row.Fact.Rel) {
			candidates = append(candidates, row)
			rels = append(rels, row.Fact.Rel)
		}
	}
	if len(candidates) == 0 {
		return 0
	}
	before, err := treeEntries(ctx, top, base+"^{commit}", rels)
	if err != nil {
		return 0
	}
	head, err := treeEntries(ctx, top, "HEAD", rels)
	if err != nil {
		return 0
	}
	settled := 0
	for _, row := range candidates {
		if ctx.Err() != nil {
			return settled
		}
		_, existed := before[row.Fact.Rel]
		landed, ok := head[row.Fact.Rel]
		if !existed && ok && landed.regular() && settleRow(index, top, landed.oid, row) {
			settled++
		}
	}
	return settled
}

// settleCandidate is a row this node's worker created on its branch.
func settleCandidate(row DeliverableRow, task string) bool {
	f := row.Fact
	return f.Task == task && f.Branch != "" && f.Rel != "" && f.State == "" && f.File != nil && f.File.SHA256 != "" && !f.Removed
}

// settleRow records the row kept when the file at its checkout path holds
// exactly the authored bytes and they are the blob HEAD holds there.
func settleRow(index, top, landed string, row DeliverableRow) bool {
	path := filepath.Join(top, filepath.FromSlash(row.Fact.Rel))
	want := row.Fact.File.SHA256
	if path != row.Artifact.Path {
		return false
	}
	identity, blob, ok := checkoutCopy(path, want, len(landed) == 64)
	if !ok || blob != landed {
		return false
	}
	// No event follows: this is a state settling, not a creation, and the
	// landing itself is announced on the task's own lane.
	_, written := appendDeliverableUpdate(index, row, func(f *DeliverableFact) {
		f.State = DeliverKept
		f.File = identity
		f.Kept = &DeliverableKept{Path: path, At: time.Now().UTC(), Bytes: identity.Bytes, SHA256: want, How: KeptCreatedInRepo, File: identity}
	})
	return written
}

// checkoutCopy is the identity of the file at path when it is a regular file,
// reached through no link, whose bytes hash to want through one descriptor,
// together with the git blob id those same bytes have (sha256 object format
// when sha256Objects, else sha1), so one read proves the file is the blob.
func checkoutCopy(path, want string, sha256Objects bool) (*DeliverableFile, string, bool) {
	if canonicalFile(path) != path {
		return nil, "", false // a link somewhere on the way
	}
	named, err := os.Lstat(path)
	if err != nil || !named.Mode().IsRegular() {
		return nil, "", false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(named, info) {
		return nil, "", false
	}
	content := sha256.New()
	var blob hash.Hash = sha1.New()
	if sha256Objects {
		blob = sha256.New()
	}
	fmt.Fprintf(blob, "blob %d\x00", info.Size())
	if n, err := io.Copy(io.MultiWriter(content, blob), io.LimitReader(file, info.Size()+1)); err != nil || n != info.Size() {
		return nil, "", false
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(after, info) || hex.EncodeToString(content.Sum(nil)) != want {
		return nil, "", false
	}
	identity := identityOf(path, info)
	identity.SHA256 = want
	return identity, hex.EncodeToString(blob.Sum(nil)), true
}
