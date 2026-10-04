package cellstore

import (
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// reapGrace is how long a group is given to leave after it is asked to, before
// it is made to.
const reapGrace = 2 * time.Second

const reapPoll = 25 * time.Millisecond

// GroupRec is a process group a call started and the process that started it,
// each with the identity it had then. A pid alone is not enough to act on later:
// numbers are handed out again, and ending a group that is somebody else's would
// destroy work that is not this cell's.
type GroupRec struct {
	PGID       int    `json:"pgid"`
	Start      uint64 `json:"start"`
	Owner      int    `json:"owner"`
	OwnerStart uint64 `json:"owner_start"`
}

// groupRec names a group this process just started, and this process as its owner.
func groupRec(pgid int) GroupRec {
	self := os.Getpid()
	return GroupRec{PGID: pgid, Start: processgroup.StartOf(pgid), Owner: self, OwnerStart: processgroup.StartOf(self)}
}

// orphaned reports whether the process that started the group is gone.
func (g GroupRec) orphaned() bool { return !processgroup.Running(g.Owner, g.OwnerStart) }

func (g GroupRec) group() processgroup.Group { return processgroup.Recorded(g.PGID, g.Start) }

// reapOrphans ends the process groups that unfinished calls left running after
// the session that owned them died: asked to stop, then made to once the grace
// is over. It only ever names groups this log recorded, and a group whose owner
// still runs, or whose leader is no longer the process recorded, is not touched.
func reapOrphans(intents []Intent, grace time.Duration) {
	live := liveOrphans(intents)
	for _, g := range live {
		_ = g.Terminate()
	}
	waitUntil(grace, func() bool { return len(stillAlive(live)) == 0 })
	for _, g := range stillAlive(live) {
		_ = g.Kill()
	}
}

func liveOrphans(intents []Intent) []processgroup.Group {
	var out []processgroup.Group
	for _, in := range intents {
		for _, rec := range in.Groups {
			if g := rec.group(); rec.orphaned() && g.Alive() {
				out = append(out, g)
			}
		}
	}
	return out
}

func stillAlive(groups []processgroup.Group) []processgroup.Group {
	var out []processgroup.Group
	for _, g := range groups {
		if g.Alive() {
			out = append(out, g)
		}
	}
	return out
}

func waitUntil(limit time.Duration, done func() bool) {
	for deadline := time.Now().Add(limit); !done() && time.Now().Before(deadline); {
		time.Sleep(reapPoll)
	}
}
