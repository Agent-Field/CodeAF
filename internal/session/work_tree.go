package session

import "time"

// THE WORK TREE IS ONE SNAPSHOT BOTH SURFACES READ. The rail's folded window
// and a room's full board are two depths of the same picture, and the moment
// they render from two accountings they will disagree about what is running —
// so every surface that says "what is working right now" reads this door and
// nothing else. (docs/HOME-BRIDGE.md states the one-source-of-truth law; this
// file is that law applied to live work.)
//
// A node is a WORKER: a task's own thread, a planned node of an orchestrated
// run, or a child born mid-run when the division gate fired. The tree does not
// say which road created a child — to the person a hand at work is a hand at
// work, and the room is where provenance lives.

// WorkState is the little a row needs to draw a worker: it is moving, it is
// waiting for something (a free hand, an answer), or it has landed.
type WorkState string

const (
	WorkRunning WorkState = "running"
	WorkWaiting WorkState = "waiting"
	WorkDone    WorkState = "done"
)

// WorkNode is one worker and the workers under it.
type WorkNode struct {
	ID       string
	Title    string
	State    WorkState
	Born     time.Time
	Children []WorkNode
}

// WorkingNow is the session's live work as one tree: the session's tasks at
// the top, their workers beneath, born children included the moment they
// exist. An idle session answers nil, and nil renders as nothing — the
// emptiness law. THE ENGINE LANE FILLS THIS DOOR (issue #29); until then the
// honest answer is that nothing is known, and every reader must draw exactly
// what it drew before this door existed.
func (a *Agent) WorkingNow() []WorkNode {
	return nil
}

// CountWorking is the one number the head of a rail or a pulse line quotes:
// every node in the tree that is running right now, at every depth.
func CountWorking(nodes []WorkNode) int {
	count := 0
	for _, node := range nodes {
		if node.State == WorkRunning {
			count++
		}
		count += CountWorking(node.Children)
	}
	return count
}
