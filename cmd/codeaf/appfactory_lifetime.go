package main

import (
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// factoryLifetimeCosts reads this computer's tagged ledger once when its file
// changes. The returned map is immutable. A missing or shortened ledger cannot
// erase money already observed by this backend. Remote factories use only
// their saved job totals; a repository name alone cannot identify their ledger.
func (b *appBackend) factoryLifetimeCosts(id string) map[string]float64 {
	if _, err := b.localHostOf(id); err != nil {
		return nil
	}
	c := &b.factorySpend
	c.Lock()
	defer c.Unlock()
	path := appLedgerPath()
	if c.path != path {
		c.path = path
		c.totals = nil
		c.size = -1
		c.modified = time.Time{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return c.totals
	}
	if info.Size() == c.size && info.ModTime().Equal(c.modified) {
		return c.totals
	}
	lines, err := session.ReadUsage(path, time.Time{})
	if err != nil {
		return c.totals
	}
	sums := map[string]float64{}
	for _, row := range lines {
		if row.FactoryJob != "" && row.USD > 0 {
			sums[strings.ToLower(row.FactoryJob)] += row.USD
		}
	}
	for name, usd := range c.totals {
		sums[name] = max(sums[name], usd)
	}
	c.totals, c.size, c.modified = sums, info.Size(), info.ModTime()
	return c.totals
}
