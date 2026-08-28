package session

// THE ANSWER THAT HAS NOT CHANGED SINCE YOU LAST LOOKED.
//
// A model handed work off and then called `tasks` eleven times in one turn
// waiting for it. It did not need to: work that lands WAKES this session with a
// note naming what happened (task_run.go's [Agent.reportTaskNode] and
// [Agent.deliverTaskNote]), so the report comes to the model rather than the
// other way round. The handoff now says so in its own receipt, and `tasks` says
// so in its description — but a model mid-poll is not re-reading a description
// it was given twenty steps ago, so the fact is put where it will actually be
// read: ON THE ANSWER ITSELF, the moment the answer stops moving.
//
//	nothing has changed since your last look · you will be told when it lands
//
// IT COSTS NOTHING NEW. The answer was going to be built anyway; what is kept is
// a hash of it, on the turn's own episode (hooks.go) — so the memory dies with
// the turn, which is the only span over which "your last look" means anything.
// Nothing is read, no file is opened and no second answer is computed.
//
// A LIVE TASK'S ANSWER IS NEVER UNCHANGED and correctly never gets the line: it
// carries how long the work has been going, which moves every second. The line
// is for the shape the owner actually saw — the same list, asked for again.

import (
	"context"
	"hash/fnv"
	"strconv"
	"strings"
	"sync"
)

// taskLookUnchanged is the fact line, and it is two clauses because a model
// that has just been told nothing changed needs the second half — the reason
// there is nothing to wait for — in the same breath.
const taskLookUnchanged = "nothing has changed since your last look · you will be told when it lands"

// taskLookMemory bounds how many distinct answers one turn remembers. Eight is
// more looks than any working turn takes; past it the memory is cleared rather
// than grown, because a turn that has asked eight different questions of the
// task record is not the polling shape this exists for.
const taskLookMemory = 8

// lookMemory is one turn's memory of what it has already been told.
//
// It carries a mutex for [loopWatch]'s reason: a batch runs its calls in
// goroutines, so two `tasks` calls in one batch reach this from two places at
// once, and a memory whose safety rested on that never happening is a memory
// that breaks silently the first time it does.
type lookMemory struct {
	mu   sync.Mutex
	seen map[string]bool
}

func newLookMemory() *lookMemory { return &lookMemory{seen: make(map[string]bool, 2)} }

// fresh records one answer and reports whether the turn had not seen it before.
func (m *lookMemory) fresh(answer string) bool {
	if m == nil {
		return true
	}
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(strings.TrimSpace(answer)))
	key := strconv.FormatUint(digest.Sum64(), 16)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen == nil {
		m.seen = make(map[string]bool, 2)
	}
	if m.seen[key] {
		return false
	}
	if len(m.seen) >= taskLookMemory {
		clear(m.seen)
	}
	m.seen[key] = true
	return true
}

// markTaskLook puts the fact line on an answer this turn has already been given,
// and leaves a first answer exactly as it was.
func markTaskLook(ctx context.Context, answer string) string {
	if strings.TrimSpace(answer) == "" {
		return answer
	}
	ep := episodeFrom(ctx)
	if ep == nil || ep.looks == nil || ep.looks.fresh(answer) {
		return answer
	}
	return taskLookUnchanged + "\n" + answer
}

// ── the turn, reachable from inside a tool ──────────────────────────────────

// episodeCarrier is the context key the turn's episode rides on. It is a type
// rather than a string for the usual reason: nothing outside this package can
// name it, so nothing outside this package can collide with it.
type episodeCarrier struct{}

// withEpisode puts the turn's control plane on the context every tool in the
// batch runs under ([Agent.runToolsWarm]).
//
// A TOOL THAT WANTS TURN-SHAPED MEMORY HAS NOWHERE ELSE TO PUT IT. The episode
// is deliberately not held on the Agent — a detector that remembered yesterday's
// repetitions would nudge a model for a call it is making for the first time
// today (loop.go says this where the episode is built) — and the context is the
// one thing already threaded from the turn into every Execute.
func withEpisode(ctx context.Context, ep *episode) context.Context {
	if ep == nil {
		return ctx
	}
	return context.WithValue(ctx, episodeCarrier{}, ep)
}

// episodeFrom answers the turn's episode, or nil where there is none — a tool
// called from a test, or from a path that runs no turn. Every reader treats nil
// as "no memory", so nothing here can fail a call.
func episodeFrom(ctx context.Context) *episode {
	if ctx == nil {
		return nil
	}
	ep, _ := ctx.Value(episodeCarrier{}).(*episode)
	return ep
}
