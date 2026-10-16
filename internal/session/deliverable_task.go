package session

// deliverable_task.go is how a TASK WORKER's new files reach the deliverables
// index, under the conversation that owns the work.
//
// OWNERSHIP IS PROVED BY CONSTRUCTION, never by looking at a path or a clock. A
// worker has no session of its own (session.go: a Place is a session's), so its
// own journal id is not a conversation and its Config.ArtifactsIndex stays empty
// - which is why [Agent.recordWritten] has always refused InTask. What lets a
// worker record is a [deliverableSink], made ONLY by [Agent.deliverSinkLocked]
// while the owning conversation builds the worker, naming that conversation's
// index and session, the worker's directory and the node. Every row it writes
// is then the conversation's, carrying the node's id, the node's captured
// source as its asking message, the worker's own run, and the node's branch as
// the branch actually cut.
//
// WHAT IS KNOWN WHEN, stated plainly, and each from a fact and not a place:
//   - Created in a worktree: the row names the path the file will have in the
//     checkout once the work lands (the repository's top, plus rel), with
//     branch and rel. Its state is unknown: it is not kept until it is merged,
//     and "not merged" is a live fact of git.
//   - Created where the worker stood in the checkout itself: kept, created in
//     the repository, ONLY when git lists the new file as untracked and not
//     ignored in a real repository. A folder that is not a repository (the
//     in-place tree), or a file git ignores, is a new file whose state is
//     unknown: the folder's name is never the proof.
//   - Landed: when the node's work lands merged, a row it created is settled
//     as kept only when git and the file both prove that EXACTLY the authored
//     bytes arrived through this landing (see [TaskNode.settleDeliverables]).
//     Anything short of that proof leaves the row as it was: unknown, never a
//     guess, and settling again later is safe.
//
// WHAT IS NOT COVERED: a worker on the bash belt creates files with a shell,
// which nothing observes; a media file made in a worktree is not recorded
// (its identity would be the worktree's, not the checkout's).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type deliverableSink struct {
	owner     *Agent // the conversation: events and the index are its
	index     string
	session   string
	workspace string // the checkout the work will land in, canonical
	dir       string // where this worker stands, canonical
	node      *TaskNode

	// top is the top of the repository the checkout is inside, asked of git
	// once (topOnce); "" when it is not inside one.
	topOnce sync.Once
	top     string
}

// repoTop is the checkout's repository top, asked of git the first time it is
// needed and never again.
func (s *deliverableSink) repoTop() string {
	s.topOnce.Do(func() { s.top = repoTop(s.workspace) })
	return s.top
}

// inPlace says the worker stood in the checkout itself.
func (s *deliverableSink) inPlace() bool { return s.dir == s.workspace }

// deliverSinkLocked is the sink for the worker being built for node in dir, or
// nil when this agent has no conversation to record under. a.mu is held.
func (a *Agent) deliverSinkLocked(dir string, node *TaskNode) *deliverableSink {
	var owner *Agent
	var index, session, workspace string
	switch base := a.config.deliverSink; {
	case base != nil:
		owner, index, session, workspace = base.owner, base.index, base.session, base.workspace
	case !a.config.InTask && a.config.ArtifactsIndex != "" && a.file != nil && a.file.ID() != "":
		owner, index, session, workspace = a, a.config.ArtifactsIndex, a.file.ID(), canonicalFile(a.config.Workspace)
	default:
		return nil
	}
	if strings.TrimSpace(dir) == "" || node == nil {
		return nil
	}
	return &deliverableSink{owner: owner, index: index, session: session, workspace: workspace, dir: canonicalFile(dir), node: node}
}

func (n *TaskNode) setDeliverSink(sink *deliverableSink) {
	if sink == nil {
		return
	}
	n.graph.mu.Lock()
	n.deliverSink = sink
	n.graph.mu.Unlock()
}

// recordTaskWritten records a file a task worker's write tool just created.
func (a *Agent) recordTaskWritten(ctx context.Context, event writtenFile) {
	sink := a.config.deliverSink
	if sink == nil || sink.node == nil {
		return
	}
	canonical := canonicalFile(event.Path)
	kind := deliverableKindExt(canonical)
	if kind == "" || !withinAny([]string{sink.dir}, canonical) {
		return
	}
	rel, err := filepath.Rel(sink.dir, canonical)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return
	}
	source := sink.node.source()
	fact := DeliverableFact{
		ID: NewDeliverableID(), At: time.Now().UTC(), Run: runIdentity(ctx), Task: strconv.FormatUint(sink.node.id, 10),
		Origin: DeliverOriginAgent, Authored: true,
	}
	if source.valid() && source.Kind == TaskSourceMessage {
		fact.Turn = source.ID
	}
	sum := sha256.Sum256(event.Content)
	hexsum := hex.EncodeToString(sum[:])
	fact.Version = "sha256:" + hexsum
	artifact := Artifact{Title: filepath.Base(canonical), Session: sink.session, Kind: kind, Created: time.Now()}
	if sink.inPlace() {
		artifact.Path = canonical
		fact.File = identityOf(canonical, event.Info)
		fact.File.SHA256 = hexsum
		if sink.createdInRepo(ctx, canonical) {
			fact.State, fact.Kept = DeliverKept, &DeliverableKept{Path: canonical, At: time.Now().UTC(), How: KeptCreatedInRepo}
		}
	} else {
		if kind == "image" || kind == "audio" || kind == "video" {
			return // a worktree's inode is not the checkout's: not recorded
		}
		// The path it will have once merged is the repository's, so without a
		// repository there is no path to name and nothing is recorded. Its
		// identity is the worktree's and is deliberately NOT recorded - only
		// the content hash and size are.
		top := sink.repoTop()
		if top == "" {
			return
		}
		artifact.Path = filepath.Join(top, rel)
		fact.Rel = filepath.ToSlash(rel)
		fact.File = &DeliverableFile{Bytes: int64(len(event.Content)), SHA256: hexsum}
		fact.Branch = sink.node.currentBranch()
	}
	artifact.Deliver = &fact
	written, ok := recordArtifactRow(sink.index, artifact)
	if !ok {
		return
	}
	sink.owner.emitArtifactChange(written)
}

func (n *TaskNode) currentBranch() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.branch
}

// createdInRepo is the proof a file the worker created where it stood in the
// checkout was created in a REPOSITORY: the node's tree is not the in-place
// tree a non-repository gets, and git lists the file as untracked and not
// ignored under the repository's top.
func (s *deliverableSink) createdInRepo(ctx context.Context, canonical string) bool {
	if s.node.treeMerge() == mergeInPlace {
		return false
	}
	top := s.repoTop()
	if top == "" {
		return false
	}
	rel, err := filepath.Rel(top, canonical)
	return err == nil && untrackedInRepo(ctx, top, filepath.ToSlash(rel))
}

func (n *TaskNode) treeMerge() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.merge
}
