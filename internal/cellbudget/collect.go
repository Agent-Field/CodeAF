package cellbudget

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/env"
)

// BudgetEnv is the one setting: the disk the working files of all cells may
// use, in GiB, fractions allowed. 0 means no limit.
const BudgetEnv = "CODEAF_CELL_BUDGET_GB"

const defaultGiB = 20

// Limit is the budget in bytes; 0 means unlimited. An unreadable value is the
// default rather than an error, so a typo cannot disable the budget silently
// or refuse to start.
func Limit() int64 {
	gib, err := strconv.ParseFloat(strings.TrimSpace(env.Get(BudgetEnv)), 64)
	if err != nil || gib < 0 {
		gib = defaultGiB
	}
	return int64(gib * (1 << 30))
}

// Outcome is what a sweep did, or would do, with one cell.
type Outcome string

const (
	Evicted    Outcome = "evicted"
	WouldEvict Outcome = "would evict"
	Skipped    Outcome = "skipped"
)

// Action is one cell's part of a sweep. Why is set for a skipped cell.
type Action struct {
	ID      string
	Bytes   int64
	Outcome Outcome
	Why     string
}

// Report is a sweep's result. Total is what the cells held before it ran.
type Report struct {
	Total, Limit int64
	Actions      []Action
}

// held is a cell found through its ledger.
type held struct {
	cell  cell.Cell
	entry Entry
}

// Collect brings the cells under the budget, least recently opened first. With
// dry set it changes nothing and reports what it would evict.
func (m Manager) Collect(ctx context.Context, dry bool) (Report, error) {
	cells, err := m.measured()
	if err != nil {
		return Report{}, err
	}
	rep := Report{Total: total(cells), Limit: m.Limit}
	if m.Limit <= 0 || rep.Total <= m.Limit {
		return rep, nil
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i].entry.OpenedMs < cells[j].entry.OpenedMs })
	over := rep.Total - m.Limit
	for _, h := range cells {
		if over <= 0 {
			break
		}
		a := m.consider(ctx, h, dry)
		rep.Actions = append(rep.Actions, a)
		if a.Outcome != Skipped {
			over -= a.Bytes
		}
	}
	return rep, nil
}

func total(cells []held) (n int64) {
	for _, h := range cells {
		n += h.entry.Bytes
	}
	return n
}

func (m Manager) consider(ctx context.Context, h held, dry bool) Action {
	a := Action{ID: h.cell.ID, Bytes: h.entry.Bytes}
	skip := func(err error) Action { a.Outcome, a.Why = Skipped, err.Error(); return a }
	if err := m.accepts(h); err != nil {
		return skip(err)
	}
	if dry {
		a.Outcome = WouldEvict
		return a
	}
	if err := m.evictLocked(ctx, h); err != nil {
		return skip(err)
	}
	a.Outcome = Evicted
	return a
}

func (m Manager) accepts(h held) error {
	for _, r := range m.rules() {
		if err := r(h.cell, h.entry); err != nil {
			return err
		}
	}
	return nil
}

// evictLocked evicts under the cell's lock, judging the rules again there:
// the sweep's first look was without it.
func (m Manager) evictLocked(ctx context.Context, h held) error {
	release, err := lock(m.Engine.LocalDir(h.cell), false)
	if err != nil {
		return err
	}
	defer release()
	e, err := readEntry(m.Engine.LocalDir(h.cell))
	if err != nil {
		return err
	}
	h.entry = e
	if e.Evicted != nil {
		return errors.New("already evicted")
	}
	if err := m.accepts(h); err != nil {
		return err
	}
	return m.evict(ctx, h.cell, e)
}

// measured lists the cells that still hold their working files, each with a
// current size. A size is cached and re-measured only when the transcript
// moved after it was taken.
func (m Manager) measured() ([]held, error) {
	dirs, err := os.ReadDir(m.Engine.Root())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []held
	for _, d := range dirs {
		if h, ok := m.load(filepath.Join(m.Engine.Root(), d.Name()), d.Name()); ok {
			out = append(out, h)
		}
	}
	return out, nil
}

// load reads one ledger and refreshes its size. A cell that is gone, evicted or
// unmeasurable is not part of the total.
func (m Manager) load(dir, id string) (held, bool) {
	e, err := readEntry(dir)
	if err != nil || e.Root == "" || e.Evicted != nil {
		return held{}, false
	}
	c, err := cell.OpenAt(e.Root, id)
	if err != nil {
		return held{}, false
	}
	if e, err = m.refreshed(dir, c, e); err != nil {
		return held{}, false
	}
	return held{cell: c, entry: e}, true
}

func (m Manager) refreshed(dir string, c cell.Cell, e Entry) (Entry, error) {
	if !m.stale(c, e) {
		return e, nil
	}
	n, err := m.held(c)
	if err != nil {
		return e, err
	}
	e.Bytes, e.MeasuredMs = n, m.nowMs()
	return e, writeEntry(dir, e)
}

// held is what eviction would free from the cell: the size of a workspace the
// harness owns. A person's folder is never walked and never counted.
func (m Manager) held(c cell.Cell) (int64, error) {
	dir := m.Workspace(c)
	if !harnessOwned(c, dir) {
		return 0, nil
	}
	return workingSize(dir)
}

func (m Manager) stale(c cell.Cell, e Entry) bool {
	info, err := os.Stat(filepath.Join(c.Root, cell.TranscriptPath))
	return e.MeasuredMs == 0 || err != nil || info.ModTime().UnixMilli() > e.MeasuredMs
}

// Auto is the sweep a start runs: at most once per interval, and free when
// under budget, because sizes are cached.
func (m Manager) Auto(ctx context.Context, every time.Duration) error {
	stamp := filepath.Join(m.Engine.Root(), "sweep.stamp")
	if info, err := os.Stat(stamp); err == nil && m.Now().Sub(info.ModTime()) < every {
		return nil
	}
	if err := os.MkdirAll(m.Engine.Root(), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(stamp, nil, 0o600); err != nil {
		return err
	}
	_, err := m.Collect(ctx, false)
	return err
}
