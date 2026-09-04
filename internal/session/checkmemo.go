package session

// The session check memory is the same idea as internal/verify/baseline.go's
// remembered photograph, one level out: an answer belongs to one command over
// one state of one tree, while the time it took remains useful after that tree
// has moved.

import (
	"sync"
	"time"
)

// rememberedSessionChecks bounds the answers one session keeps at once.
//
// THE BOUND IS ON MEMORY, NOT CORRECTNESS. Thirty-two commands is four full
// session check rosters, beyond what one terminal reading can name; past it the
// oldest answer is dropped, and losing an answer costs one fresh run rather
// than allowing a stale one through.
const rememberedSessionChecks = 32

// checkMemory is what one command last said about one tree, and what it cost to
// find out. The answer is reusable only while state is the same and non-empty;
// took remains useful whatever became of that answer.
type checkMemory struct {
	state  string
	answer CheckRun
	took   time.Duration
}

// checkMemories is one agent's bounded set of check answers. execution is held
// across the process itself so two readers cannot both pay for the same answer;
// mutex guards the short reads and writes, including the terminal unread list,
// without holding the agent's already delicate lock across a shell command.
type checkMemories struct {
	execution sync.Mutex
	mutex     sync.Mutex
	taken     map[string]checkMemory
	order     []string
	unread    []string
}

func checkMemoryKey(tree, command string) string { return tree + "\x00" + command }

func (m *checkMemories) answerFor(tree, command, state string) (CheckRun, bool) {
	if m == nil || state == "" {
		return CheckRun{}, false
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	held, ok := m.taken[checkMemoryKey(tree, command)]
	if !ok || held.state == "" || held.state != state {
		return CheckRun{}, false
	}
	return held.answer, true
}

func (m *checkMemories) paceFor(tree, command string) (time.Duration, bool) {
	if m == nil {
		return 0, false
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	held, ok := m.taken[checkMemoryKey(tree, command)]
	return held.took, ok
}

func (m *checkMemories) remember(tree, command, state string, run CheckRun, took time.Duration, moved bool) {
	if m == nil {
		return
	}
	key := checkMemoryKey(tree, command)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.taken == nil {
		m.taken = map[string]checkMemory{}
	}
	if _, held := m.taken[key]; !held {
		m.order = append(m.order, key)
		for len(m.order) > rememberedSessionChecks {
			delete(m.taken, m.order[0])
			m.order = m.order[1:]
		}
	}
	memory := checkMemory{took: took}
	if !moved && state != "" {
		memory.state = state
		memory.answer = run
	}
	m.taken[key] = memory
}

// terminalUnread records the checks the latest terminal reading did not start.
// They live beside the answers because both facts are produced by that one
// serialized reading and consumed by the ending immediately after it.
func (m *checkMemories) terminalUnread(commands []string) {
	if m == nil {
		return
	}
	m.mutex.Lock()
	m.unread = append(m.unread[:0], commands...)
	m.mutex.Unlock()
}

func (m *checkMemories) unreadNow() []string {
	if m == nil {
		return nil
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return append([]string(nil), m.unread...)
}

// rememberTerminalUnread and terminalUnreadNow use the sidecar's own mutex as
// their guard. They deliberately do not take a.mu: the unread list belongs to
// checkMemories, and adding the agent lock would create an unnecessary lock
// order on an ending road.
func (a *Agent) rememberTerminalUnread(commands []string) {
	a.sessionCheckMemories().terminalUnread(commands)
}

func (a *Agent) terminalUnreadNow() []string {
	return a.sessionCheckMemories().unreadNow()
}

// sessionCheckMemories reaches the store held by this agent's Steward. A
// person's session has no store because [Agent.openBaseline] returns before a
// reading without a Steward, and [Person.Acceptance] keeps it out of the
// decideOverTheChecks road that reaches the terminal reading.
func (a *Agent) sessionCheckMemories() *checkMemories {
	steward := a.steward()
	if steward == nil {
		return nil
	}
	return &steward.checks
}
