package session

// WHAT A MANAGER ALREADY KNOWS ABOUT ITS MEMBERS' JOURNALS.
//
// A manager's digest is refreshed beside every turn it takes, and a status is
// asked whenever the model wants one; both read each member's journal to learn
// what it is doing. The read is bounded ([journalTail], 256 KiB from the end of
// each file, and only a line's type, role, time and tool calls are decoded),
// but a team of five idle members would still pay five of those per turn for
// nothing new. So what a journal said is kept against the file's stamp (its
// size and modification time), and a journal whose stamp has not moved costs a
// stat. The clock is applied after the cache ([journalFacts.state]), so a
// member going quiet still reads as idle once [journalStale] passes.

import (
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// journalCache is the facts last read from each member journal, by path.
type journalCache struct {
	mu    sync.Mutex
	files map[string]cachedJournal
}

type cachedJournal struct {
	at    fileStamp
	facts journalFacts
}

// read is the facts for path: the kept ones when its stamp has not moved,
// read again otherwise. A nil cache reads every time.
func (c *journalCache) read(path string) (journalFacts, bool) {
	if c == nil {
		return readJournalFacts(path)
	}
	stamp := stampOf(path)
	if !stamp.present {
		c.forget(path)
		return journalFacts{}, false
	}
	c.mu.Lock()
	kept, ok := c.files[path]
	c.mu.Unlock()
	if ok && kept.at == stamp {
		return kept.facts, true
	}
	facts, ok := readJournalFacts(path)
	if !ok {
		return journalFacts{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.files == nil {
		c.files = map[string]cachedJournal{}
	}
	c.files[path] = cachedJournal{at: stamp, facts: facts}
	return facts, true
}

func (c *journalCache) forget(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.files, path)
}

// stateOf is one member's state and its journal's last instant, off the cache.
func (c *journalCache) stateOf(path string, now time.Time) (teams.MemberState, time.Time, bool) {
	facts, ok := c.read(path)
	if !ok {
		return teams.MemberState{}, time.Time{}, false
	}
	return facts.state(now), facts.last, true
}
