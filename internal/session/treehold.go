package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── WHILE A NODE HOLDS A TREE, THE CONVERSATION IS A READER OF THAT TREE ────
//
// THE MEASURED FAILURE. SWE-Marathon run s10, 10:34:28Z. A node was handed
// /workspace/rust-java-lsp to repair. There was no git in that container, so the
// node's working copy was the workspace ITSELF — [prepareTaskTree] returns
// `merge: inplace` for a directory it cannot branch, which is the honest answer
// and also a promise nobody was keeping. At 10:40:18Z, six minutes in, the
// CONVERSATION wrote src/analysis.rs whole — a full-file overwrite from a turn
// that had read the file before the node started — and it landed underneath the
// repair. Neither side did anything wrong by its own lights. Both were writing
// to the one tree, and what came out was neither of their work.
//
// THE LAW: AN IN-PLACE NODE TAKES A WRITER'S CLAIM ON ITS TREE FOR AS LONG AS IT
// RUNS, INCLUDING WHILE IT IS BEING CHECKED AND REPAIRED. A write from anywhere
// else — the chat, a fork hand, a sibling node — is REFUSED at the write guard,
// with the holder named and something the writer can actually do instead.
//
// ── WHY REFUSED AND NOT SERIALIZED ──────────────────────────────────────────
//
// The two obvious answers to "two writers, one tree" are a lock and a refusal,
// and this codebase has already picked between them twice for the same reason.
// fork.go refuses overlapping write scopes AT THE CALL, before any hand starts,
// rather than interleaving them; taskground.go refuses to merge work whose paths
// somebody else moved under it. Both because A LOCK MAKES EACH WRITE ATOMIC AND
// LEAVES THE TREE JUST AS WRONG. The measured overwrite was already atomic. What
// destroyed the repair was not two writes tearing each other's bytes, it was one
// whole-file write built from a reading of the file that the other writer had
// since replaced — and no ordering of those two calls produces a tree either
// author intended.
//
// So the second writer is told no, and told who has it.
//
// ── WHY THE CLAIM IS DERIVED AND NOT REGISTERED ─────────────────────────────
//
// There is no claims table and nothing to take or put back, because the claim is
// not a separate fact: it IS "this node is running, and its tree is not a
// worktree of its own". Both halves are already in the graph, written by
// [TaskNode.setTree] before the node's first tool call and cleared by the node
// ending — landing, failing, being stopped or being cut off by a closing
// process. A registry beside them would be a second copy of a fact that can go
// stale in exactly the case that matters most: a node killed halfway would leave
// its claim standing and lock the person out of their own repository forever.
// Deriving it means the release is not code anybody has to remember to write.
//
// ── WHO IS FIRST ────────────────────────────────────────────────────────────
//
// Two in-place nodes on one tree resolve by WHO STARTED FIRST, and the later one
// is the one refused. That has to be a total order that every agent computes
// identically without asking anybody, or the two would refuse each other and the
// tree would belong to nobody; run-start does it, and admission order breaks the
// ties (the graph keeps `order` for exactly this determinism reason).

// treeClaim is one running node's hold on one tree, in the three facts a refusal
// needs: who it is, what it is called, and where the tree is.
type treeClaim struct {
	id    uint64
	title string
	dir   string
}

// claimOver answers who holds the tree that `path` is inside, from the point of
// view of `writer` — the node id the writing agent belongs to, and 0 for the
// conversation itself.
//
// It answers false, meaning "write away", in every case but one: some OTHER
// running node is working in a tree it did not get isolated, and the path is
// inside it.
//
// THREE THINGS ARE NOT SOMEBODY ELSE, and each is a rule this package already
// states elsewhere:
//
//   - The holder itself. A node writing in its own tree is the whole point of
//     having one.
//   - The holder's own family. taskground.go's second law, in its own words:
//     "THE NODE'S OWN FAMILY IS NOT SOMEBODY ELSE" — a part cut out of a node's
//     work, or a hand of that node's worker, is that node writing.
//   - A writer whose OWN tree sits strictly inside the holder's. That is a
//     sibling working in an isolated worktree that happens to live under the
//     workspace, and it is writing in its own copy, not in the holder's.
//
// Paths are compared as they are spelled, cleaned. Nothing here evaluates
// symlinks, and it does not need to: an in-place tree's directory IS the
// Workspace string it was built from ([prepareTaskTree]), and a write's path is
// resolved against the writing agent's own Workspace ([Agent.mutatingPath]), so
// the two sides of this comparison are the same string by construction.
func (g *TaskGraph) claimOver(path string, writer uint64, writerTree string) (treeClaim, bool) {
	if g == nil || strings.TrimSpace(path) == "" {
		return treeClaim{}, false
	}
	var (
		holder  *TaskNode
		claim   treeClaim
		started time.Time
	)
	g.mu.Lock()
	for _, id := range g.order {
		node := g.nodes[id]
		if node == nil || node.state != TaskRunning {
			continue
		}
		// A NODE WITH A BRANCH OF ITS OWN CLAIMS NOTHING. Its writes land in a
		// worktree nobody else is in and come home through a merge, which is the
		// machinery this whole question exists because the in-place road lacks.
		if node.merge != mergeInPlace && strings.TrimSpace(node.branch) != "" {
			continue
		}
		dir := strings.TrimSpace(node.worktree)
		if dir == "" || !treeCovers(dir, path) {
			continue
		}
		// Earliest run-start wins, and ties fall to admission order because this
		// loop walks it. A node rehydrated from a checkpoint has a zero start,
		// which sorts before every real instant — it has been there longest.
		if holder != nil && !node.started.Before(started) {
			continue
		}
		holder, started = node, node.started
		claim = treeClaim{id: node.id, title: node.spec.title, dir: dir}
	}
	g.mu.Unlock()

	if holder == nil || claim.id == writer {
		return treeClaim{}, false
	}
	// The writer's own tree sits inside the holder's: it has a copy of its own
	// and is not in the holder's.
	if strictlyInside(writerTree, claim.dir) {
		return treeClaim{}, false
	}
	// And the family question, asked outside the graph's lock because the walk
	// takes it per rung ([TaskNode.family]).
	if writer != 0 && holder.family()[strconv.FormatUint(writer, 10)] {
		return treeClaim{}, false
	}
	return claim, true
}

// treeCovers reports whether `path` is the tree `dir` or something under it.
func treeCovers(dir, path string) bool {
	relative, err := filepath.Rel(canonicalPath(dir), canonicalPath(path))
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// strictlyInside is treeCovers with the equal case taken out: the same directory
// is not a copy of its own.
func strictlyInside(inner, outer string) bool {
	inner, outer = strings.TrimSpace(inner), strings.TrimSpace(outer)
	if inner == "" || outer == "" || canonicalPath(inner) == canonicalPath(outer) {
		return false
	}
	return treeCovers(outer, inner)
}

// ── the citizen ─────────────────────────────────────────────────────────────

// treeClaimGuard refuses a write into a tree another running node is working in.
//
// It is a pre-action citizen for the reason every other guard here is one:
// [Agent.executeTool] is the single door every execution passes through, warm
// early starts included, and a check written into the belt's write tool instead
// would be a check the next tool appended does not have (hooks.go).
//
// IT BINDS EXACTLY WHAT [writeGuard] BINDS — edit and write, the two hands whose
// target is a path this process can read before the call runs (recovery.go's
// mutatingTools). A shell command's effects are whatever it did, and a guard
// that pattern-matched commands would be promising something it cannot keep. The
// hole is the same hole, named in the same place, and it is smaller than it
// looks: the measured failure was an `edit`.
type treeClaimGuard struct{ agent *Agent }

func (treeClaimGuard) Name() string { return "tree-claim" }

func (g treeClaimGuard) PreAction(_ context.Context, _ *episode, _ *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	graph := g.agent.tasker()
	if graph == nil {
		return call, toolResult{}, true
	}
	path, shown, ok := g.agent.mutatingPath(call)
	if !ok {
		return call, toolResult{}, true
	}
	claim, held := graph.claimOver(path, g.agent.config.taskID, g.agent.config.Workspace)
	if !held {
		return call, toolResult{}, true
	}
	return call, toolResult{text: treeHeldRefusal(claim, shown), isError: true}, false
}

// treeHeldRefusal is what the model reads instead of a write.
//
// It names the holder the way every other line about a task names one
// ([taskStopName]: "task 4 (repair the parser)"), says plainly that the refusal
// is temporary, and gives the two things that actually work — wait for the
// report and build on what it did, or change something outside that tree. A
// refusal that only says no is a refusal a worker spends its remaining rounds
// arguing with.
func treeHeldRefusal(claim treeClaim, shown string) string {
	return fmt.Sprintf(
		"%s is in the working copy %s is using right now, so nothing was written. "+
			"That work is writing there until it finishes — wait for its report and make this change "+
			"on top of what it did, or change something outside %s.",
		shown, taskStopName(claim.id, claim.title), filepath.ToSlash(filepath.Clean(claim.dir)))
}
