package lease

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const (
	policySpendTTL  = 2 * time.Second
	policySpendMost = 64
)

type policySpendKey struct {
	group, allowance, jobs string
}

type policySpendSnapshot struct {
	at    time.Time
	spent map[int]float64
}

type policySpendCache struct {
	mu      sync.Mutex
	entries map[policySpendKey]policySpendSnapshot
	// read defaults to the store's non-migrating document reader.
	read func(int) (factory.Item, error)
}

// policySpentOf reads only the run's own allowance, never the whole floor.
// The locked job's current spend replaces its cached value on every report.
func (d *Desk) policySpentOf(p factory.GroupPolicy, it factory.Item, now time.Time) func(int) float64 {
	var spent map[int]float64
	for _, a := range p.Allowances {
		if a.ID == it.PolicyRun && slices.Contains(a.Jobs, it.ID) {
			spent = d.policySpend.snapshot(d.store, p.Group, a, it, now)
			break
		}
	}
	return func(id int) float64 {
		if id == it.ID {
			return factory.JobLifetimeSpent(it)
		}
		return spent[id]
	}
}

func (c *policySpendCache) snapshot(st *store.Store, group string, a factory.Allowance, current factory.Item, now time.Time) map[int]float64 {
	var ids strings.Builder
	for _, id := range a.Jobs {
		ids.WriteString(strconv.Itoa(id))
		ids.WriteByte(',')
	}
	key := policySpendKey{group: group, allowance: a.ID, jobs: ids.String()}
	c.mu.Lock()
	defer c.mu.Unlock()
	if saved, ok := c.entries[key]; ok && !now.Before(saved.at) && now.Before(saved.at.Add(policySpendTTL)) {
		return saved.spent
	}
	read := c.read
	if read == nil {
		read = st.ReadOnly
	}
	spent := make(map[int]float64, len(a.Jobs))
	for _, id := range a.Jobs {
		if id == current.ID {
			spent[id] = factory.JobLifetimeSpent(current)
		} else if job, err := read(id); err == nil {
			spent[id] = factory.JobLifetimeSpent(job)
		}
		// Missing or unreadable listed jobs contribute zero.
	}
	// Bound memory and eviction work when many groups report on one desk.
	if c.entries == nil || len(c.entries) >= policySpendMost {
		c.entries = make(map[policySpendKey]policySpendSnapshot)
	}
	c.entries[key] = policySpendSnapshot{at: now, spent: spent}
	return spent
}
