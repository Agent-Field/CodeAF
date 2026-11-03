package session

import (
	"fmt"
	"io"
	"math"
	"math/bits"
	"os"
	"sync"
	"time"
)

// One retained ledger avoids an unbounded path registry. The lock serializes
// reports, including fallback scans, so simultaneous reports never rescan twice
// at once. Storage is O(days × models × subject identities), never O(rows),
// capped at UsageReportMaxCells and UsageReportMaxBytes across the whole cache.
// A subject identity includes kind, root/session and recorded owner.
var usageReports struct {
	sync.Mutex
	path  string
	cache *usageReportRollup
}

type usageReportRollup struct {
	info    os.FileInfo
	size    int64
	stats   UsageScanStats
	ends    string
	loc     *time.Location
	days    map[string]*usageReportBuilder
	totals  UsageLedgerTotals
	cells   int
	weight  int64
	unsafe  bool
	quantum int
}

// QueryUsageReport answers bounded reports, incrementally for date-only queries
// whose daily aggregates preserve every figure the streaming scan would return.
func QueryUsageReport(path string, q UsageQuery, loc *time.Location, budget UsageReportBudget) (UsageReport, error) {
	q, err := q.normalized()
	if err != nil {
		return UsageReport{}, err
	}
	if err = q.checkReportRange(); err != nil {
		return UsageReport{}, err
	}
	if loc == nil {
		loc = time.Local
	}
	budget = budget.bounded()
	usageReports.Lock()
	defer usageReports.Unlock()
	// Daily cells discard filter fields such as Run and Workspace, so filtered
	// questions must use the scan rather than infer membership from aggregates.
	if q != (UsageQuery{From: q.From, To: q.To}) || !reportMidnight(q.From, loc) || !reportMidnight(q.To, loc) {
		// Partial days cannot be selected exactly from whole-day aggregates.
		return scanUsageReport(path, q, loc, budget)
	}
	c, err := readUsageReportRollup(path, loc)
	if err != nil {
		return UsageReport{}, err
	}
	if c == nil {
		return UsageReport{}, nil
	}
	// A different zone can move derived days and instant/day membership.
	// Invalid figures, excessive retained groups, inconsistent stored days, or
	// inexact floating-point regrouping also require the scan's addition order
	// and range-local errors rather than an approximate daily answer.
	if c.loc != loc || c.unsafe {
		return scanUsageReport(path, q, loc, budget)
	}
	return c.report(q, budget)
}

func readUsageReportRollup(path string, loc *time.Location) (*usageReportRollup, error) {
	file, done, err := openUsageScan(path)
	if err != nil {
		return nil, err
	}
	defer done()
	if file == nil {
		usageReports.cache = nil
		return nil, nil
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	c := usageReports.cache
	reuse, err := c.reusable(file, info, path)
	if err != nil {
		return nil, err
	}
	if !reuse {
		c = &usageReportRollup{loc: loc, days: map[string]*usageReportBuilder{}, quantum: 1024}
		usageReports.path, usageReports.cache = path, c
	}
	if c.loc != loc {
		return c, nil
	}
	if err = c.refresh(file, info); err != nil {
		usageReports.cache = nil
		return nil, err
	}
	return c, nil
}

func (c *usageReportRollup) reusable(file *os.File, info os.FileInfo, path string) (bool, error) {
	// Use UsageCache's descriptor identity rule, including shrinkage of an
	// incomplete suffix, and the paging reader's already-read prefix check.
	if c == nil || usageReports.path != path {
		return false, nil
	}
	if !os.SameFile(info, c.info) || info.Size() < c.size {
		return false, nil
	}
	ends, err := usagePrefixEnds(file, c.stats.Bytes)
	return ends == c.ends, err
}

func (c *usageReportRollup) refresh(file *os.File, info os.FileInfo) error {
	if _, err := file.Seek(c.stats.Bytes, io.SeekStart); err != nil {
		return err
	}
	fresh, err := scanUsageLines(file, info.Size()-c.stats.Bytes, func(_ int64, line UsageLine) error { c.add(line); return nil })
	if err != nil {
		return err
	}
	c.stats.Bytes += fresh.Bytes
	c.stats.Malformed += fresh.Malformed
	c.stats.Oversized += fresh.Oversized
	c.ends, err = usagePrefixEnds(file, c.stats.Bytes)
	c.info, c.size = info, info.Size()
	return err
}

func reportMidnight(t time.Time, loc *time.Location) bool {
	d := t.In(loc)
	return d.Equal(time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc))
}

func (c *usageReportRollup) add(line UsageLine) {
	if c.unsafe {
		return
	}
	day := line.At.In(c.loc).Format(usageDayLayout)
	if validStoredDay(line.Day) && line.Day != day {
		c.unsafe = true
		return
	}
	if !c.exactCost(line.USD) || c.totals.count(line) != nil {
		c.unsafe = true
		return
	}
	b := c.days[day]
	if b == nil {
		b = newUsageReportBuilder(c.loc, UsageReportBudget{}.bounded())
		c.days[day] = b
	}
	// Apply the remaining cache budget before copying any new key strings.
	b.budget = UsageReportBudget{MaxCells: len(b.cells) + UsageReportMaxCells - c.cells, MaxBytes: b.weight + UsageReportMaxBytes - c.weight}
	before, weight := len(b.cells), b.weight
	if b.add(line) != nil {
		c.unsafe = true
		c.days = nil
		return
	}
	c.cells += len(b.cells) - before
	c.weight += b.weight - weight
}

// Positive binary floats are integer multiples of a power of two. If the
// entire ledger's sum in its smallest such unit fits 53 bits, every subset and
// every addition order is exact. Otherwise retain the scan's rounding order.
func (c *usageReportRollup) exactCost(usd float64) bool {
	if usd < 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return false
	}
	if usd == 0 {
		return true
	}
	raw := math.Float64bits(usd)
	mantissa := raw & ((1 << 52) - 1)
	exponent := int(raw>>52) - 1023 - 52
	if raw>>52 != 0 {
		mantissa |= 1 << 52
	} else {
		exponent = -1074
	}
	exponent += bits.TrailingZeros64(mantissa)
	c.quantum = min(c.quantum, exponent)
	return math.Ldexp(usd, -c.quantum) <= (1<<53)-math.Ldexp(c.totals.USD, -c.quantum)
}

func (c *usageReportRollup) report(q UsageQuery, budget UsageReportBudget) (UsageReport, error) {
	b := newUsageReportBuilder(c.loc, budget)
	b.report.Stats = c.stats
	from, to := q.From.In(c.loc).Format(usageDayLayout), q.To.In(c.loc).Format(usageDayLayout)
	for day, d := range c.days {
		if day < from || day >= to {
			continue
		}
		for key, cell := range d.cells {
			cost := reportCellWeight(key)
			if len(b.cells) >= budget.MaxCells || b.weight+cost > budget.MaxBytes {
				return UsageReport{}, fmt.Errorf("%w (more than %d groups or %d estimated bytes)", ErrUsageReportTooLarge, budget.MaxCells, budget.MaxBytes)
			}
			b.cells[key] = cell
			b.weight += cost
		}
		t, s := &b.report.Totals, d.report.Totals
		t.Records += s.Records
		t.Calls += s.Calls
		t.Input += s.Input
		t.Output += s.Output
		t.USD += s.USD
		t.RunUSD += s.RunUSD
		t.UnknownRunUSD += s.UnknownRunUSD
		t.UnknownRunRecords += s.UnknownRunRecords
		t.Unpriced += s.Unpriced
		t.Unbilled += s.Unbilled
		b.report.DaysDerived += d.report.DaysDerived
	}
	return b.finish(), nil
}

func reportCellWeight(key usageReportKey) int64 {
	return int64(usageReportCellBytes + len(key.day) + len(key.model) + len(key.kind) + len(key.subject) + len(key.root) + len(key.session) + len(key.agent))
}
