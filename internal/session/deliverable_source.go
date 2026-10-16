package session

// deliverable_source.go is the ONE answer to "where are this deliverable's
// bytes", which get, read and Keep all ask and none may answer for itself.
//
// A file at a deliverable's recorded path is not the deliverable because it is
// there. For a row the engine recorded an identity for (a chat write, a media
// tool, a landing that settled), the identity decides, as [ResolveDeliverable]
// always has, and a kept copy outlives its source. A task worker's row made in
// a worktree has NO checkout identity - its path is where the file will be once
// merged - and for it, THE LAW:
//   - While its branch is unmerged and holds a regular file at rel, the source
//     is that blob, whatever now sits at the future checkout path: on_branch,
//     pinned to the blob id, nothing to open, nothing Keep may copy.
//   - Otherwise the file at the checkout path counts only when its bytes,
//     hashed through one descriptor that is still the file at that path
//     afterwards, are the sha256 recorded when the worker wrote it. Anything
//     else is changed (or unreadable when it could not be checked in time),
//     with nothing to open, and read and Keep refuse.
// No state is invented here: a row a check proved is still the row it was.
//
// THE CHECK IS BOUNDED. It costs at most one streamed read of a file whose size
// already matches the recorded one, under a deadline and one of a few permits,
// and its verdict is remembered against the file's identity so a listing does
// not hash the same unchanged file again.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sync"
	"time"
)

// DeliverLiveOnBranch is the live state of a deliverable whose source is a
// regular file on its unmerged task branch.
const DeliverLiveOnBranch = "on_branch"

var (
	// deliverSourceMax bounds one resolution: the git questions and the check.
	deliverSourceMax = 3 * time.Second
	// deliverCheckPermits bounds how many checkout files are hashed at once.
	deliverCheckPermits = make(chan struct{}, 2)
)

// deliverCheckMemo is the most remembered verdicts; past it they are dropped.
const deliverCheckMemo = 4096

// DeliverableSource is where a deliverable's bytes are now.
type DeliverableSource struct {
	// Live is the live account. On a branch it has the on_branch state, the
	// blob's version and no Path; a file source has the Path to open.
	Live MediaLive
	// Branch, Rel, Blob and Workspace name the blob when the source is one.
	Branch, Rel, Blob, Workspace string
	// Expect is the sha256 the bytes must hash to, when it is known; a copy
	// that hashes to anything else is not this deliverable.
	Expect string
	// Undecided says git could not say in time whether the source is a
	// branch: Live is then unreadable, never gone, and a listing stops here
	// and continues later rather than describe the row.
	Undecided bool
}

// OnBranch says the source is a blob on an unmerged task branch.
func (s DeliverableSource) OnBranch() bool { return s.Blob != "" }

// ResolveDeliverableSource decides where row's bytes are. workspace is the
// owning conversation's checkout, the repository a task branch belongs to.
func ResolveDeliverableSource(ctx context.Context, row DeliverableRow, roots []string, workspace string) DeliverableSource {
	return ResolveDeliverableSourceWith(ctx, nil, row, roots, workspace)
}

// ResolveDeliverableSourceWith is [ResolveDeliverableSource] sharing what git
// said with the rest of one request through cache (nil shares nothing).
func ResolveDeliverableSourceWith(ctx context.Context, cache *DeliverSourceCache, row DeliverableRow, roots []string, workspace string) DeliverableSource {
	ctx, cancel := context.WithTimeout(ctx, deliverSourceMax)
	defer cancel()
	f := row.Fact
	if f.Branch == "" || f.Rel == "" || (f.File != nil && f.File.Canonical != "") {
		// Not a worker's branch row, or one whose checkout identity a landing
		// recorded: the identity decides.
		return identitySource(row, roots)
	}
	switch oid, verdict := branchBlobIn(ctx, cache, workspace, f.Branch, f.Rel); verdict {
	case branchUnmerged:
		return DeliverableSource{Live: MediaLive{State: DeliverLiveOnBranch, Version: "git:" + oid}, Branch: f.Branch, Rel: f.Rel, Blob: oid, Workspace: workspace}
	case branchUnknown:
		return DeliverableSource{Live: MediaLive{State: MediaUnreadable}, Undecided: true}
	}
	out := DeliverableSource{Live: MediaLive{State: MediaUnreadable}}
	if f.File != nil && f.File.SHA256 != "" {
		out = DeliverableSource{Live: checkoutSource(ctx, row, roots), Expect: f.File.SHA256}
		if out.Live.State == MediaAvailable {
			return out
		}
		// A check the time ran out on proved nothing either way.
		out.Undecided = out.Live.State == MediaUnreadable && ctx.Err() != nil
	}
	// The checkout does not hold it, but a copy this engine made may: judged
	// by the copy's own recorded identity, never by the checkout path.
	if kept := f.Kept; kept != nil && kept.How == KeptCopied && kept.File != nil && kept.File.Canonical != "" {
		if copy := resolveKeptCopy(kept); copy.State == MediaAvailable || copy.State == MediaChanged {
			copy.Copy = true
			return DeliverableSource{Live: copy, Expect: kept.SHA256}
		}
	}
	return out
}

// identitySource is [ResolveDeliverable]'s answer, with the recorded content
// hash as what a copy must hash to while the file is the one recorded.
func identitySource(row DeliverableRow, roots []string) DeliverableSource {
	live := ResolveDeliverable(row, roots)
	out := DeliverableSource{Live: live}
	switch {
	case live.State != MediaAvailable:
	case live.Copy && row.Fact.Kept.SHA256 != "":
		out.Expect = row.Fact.Kept.SHA256
	case !live.Copy && row.Fact.File != nil && row.Fact.File.Canonical != "":
		out.Expect = row.Fact.File.SHA256
	}
	return out
}

// checkoutSource is the file at a worker row's checkout path, available only
// when its bytes are the authored ones.
func checkoutSource(ctx context.Context, row DeliverableRow, roots []string) MediaLive {
	live := ResolveMedia(MediaRow{Fact: MediaFact{Origin: MediaGenerated, Canonical: canonicalFile(row.Artifact.Path)}}, roots)
	if live.State != MediaAvailable {
		return MediaLive{State: live.State}
	}
	want := row.Fact.File
	if want.Bytes > 0 && live.Bytes != want.Bytes {
		return MediaLive{State: MediaChanged, Version: live.Version, Bytes: live.Bytes, HasBytes: true}
	}
	switch matchesDigest(ctx, live.Path, live.Version, want.SHA256) {
	case digestSame:
		return live
	case digestOther:
		return MediaLive{State: MediaChanged, Version: live.Version, Bytes: live.Bytes, HasBytes: true}
	}
	return MediaLive{State: MediaUnreadable}
}

type digestVerdict int

const (
	digestUnproved digestVerdict = iota
	digestSame
	digestOther
)

var deliverDigests = struct {
	sync.Mutex
	seen map[string]bool
}{seen: map[string]bool{}}

// matchesDigest says whether the file at path, whose version was observed as
// version, hashes to want. The verdict is remembered against that version.
func matchesDigest(ctx context.Context, path, version, want string) digestVerdict {
	key := path + "\x00" + version + "\x00" + want
	if verdict, ok := rememberedDigest(key); ok {
		return verdict
	}
	select {
	case deliverCheckPermits <- struct{}{}:
		defer func() { <-deliverCheckPermits }()
	case <-ctx.Done():
		return digestUnproved
	}
	sum, ok := hashPinned(ctx, path, version)
	if !ok {
		return digestUnproved
	}
	verdict := digestOther
	if sum == want {
		verdict = digestSame
	}
	rememberDigest(key, verdict)
	return verdict
}

func rememberedDigest(key string) (digestVerdict, bool) {
	deliverDigests.Lock()
	defer deliverDigests.Unlock()
	match, ok := deliverDigests.seen[key]
	switch {
	case !ok:
		return digestUnproved, false
	case match:
		return digestSame, true
	}
	return digestOther, true
}

func rememberDigest(key string, verdict digestVerdict) {
	deliverDigests.Lock()
	defer deliverDigests.Unlock()
	if len(deliverDigests.seen) >= deliverCheckMemo {
		deliverDigests.seen = map[string]bool{}
	}
	deliverDigests.seen[key] = verdict == digestSame
}

// hashPinned is the sha256 of the file at path, read through one descriptor
// that must have the observed version and still be the file at path after the
// whole read. ok is false when that could not be shown.
func hashPinned(ctx context.Context, path, version string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || pinnedVersion(before) != version {
		return "", false
	}
	if named, err := os.Lstat(path); err != nil || !os.SameFile(named, before) {
		return "", false
	}
	hash := sha256.New()
	if _, err := keepStream(ctx, hash, file, before.Size()); err != nil {
		return "", false
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(after, before) || pinnedVersion(after) != version {
		return "", false
	}
	return hex.EncodeToString(hash.Sum(nil)), true
}

func pinnedVersion(info os.FileInfo) string {
	ctime, dev, ino := fileIdentity(info)
	return fileVersion(info.Size(), info.ModTime().UnixNano(), ctime, dev, ino)
}

// ReadDeliverableBlob reads at most max bytes of the source's pinned blob: the
// very version the source named, never whatever the branch says now.
func ReadDeliverableBlob(ctx context.Context, source DeliverableSource, max int) ([]byte, bool, error) {
	if !source.OnBranch() {
		return nil, false, os.ErrNotExist
	}
	return readBlob(ctx, source.Workspace, source.Blob, max)
}
