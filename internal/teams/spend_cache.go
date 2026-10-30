package teams

import (
	"github.com/Agent-Field/codeaf/internal/filememo"
	"maps"
	"os"
	"sync"
)

type spendQuery struct{ profile, ledger, team, day string }
type spendReading struct {
	teams  string
	ledger os.FileInfo
	value  Spend
}
type teamSpendMemory struct {
	mu    sync.Mutex
	held  map[spendQuery]spendReading
	reads int
}

var teamSpendResults teamSpendMemory

// sameSpendFile compares absence as well as identity, size and modification
// time, so replacing a ledger with a larger file cannot look like an append.
func sameSpendFile(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func (m *teamSpendMemory) read(profile, ledger, team, day string) (Spend, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := spendQuery{profile, ledger, team, day}
	t := Stamp(profile)
	u, err := os.Stat(ledger)
	if err != nil && !os.IsNotExist(err) {
		return Spend{}, err
	}
	held, ok := m.held[key]
	if ok && t == held.teams && sameSpendFile(u, held.ledger) {
		out := held.value
		out.ByMember = maps.Clone(out.ByMember)
		return out, nil
	}
	m.reads++
	out, err := readTeamSpend(profile, ledger, team, day)
	if err != nil {
		return out, err
	}
	if m.held == nil {
		m.held = make(map[spendQuery]spendReading)
	}
	if _, ok := m.held[key]; !ok && len(m.held) >= filememo.MaxEntries {
		for k := range m.held {
			delete(m.held, k)
			break
		}
	}
	m.held[key] = spendReading{t, u, out}
	out.ByMember = maps.Clone(out.ByMember)
	return out, nil
}
