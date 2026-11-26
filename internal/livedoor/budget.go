package livedoor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
)

const (
	budgetPersistInterval = 30 * time.Second
	budgetEnvDailyMB      = "CODEAF_REMOTE_DAILY_MB"
)

var budgetDiskMu sync.Mutex

type budgetDisk struct {
	Day   string `json:"day"`
	Bytes int64  `json:"bytes"`
}

// Budget counts carrier bytes across all live door tunnels per UTC day and
// degrades sessions to watch-only once the daily allowance is exhausted.
type Budget struct {
	path  string
	clk   Clock
	limit int64

	mu                sync.Mutex
	day               string
	bytes             int64
	dirty             bool
	closed            bool
	exhaustedNotified bool
	timer             Timer
	done              chan struct{}
	nextHookID        uint64
	hooks             map[uint64]func()
}

// OpenBudget loads or initializes the daily byte budget stored at
// <home>/v3/remote/budget.json. A missing, corrupt, or previous-day file starts
// at zero bytes for the current UTC date without scheduling any background timer.
func OpenBudget(homeDir string, clk Clock, limit int64) *Budget {
	if clk == nil {
		clk = RealClock()
	}
	effectiveLimit := resolveBudgetLimit(limit)
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		homeDir = home.Dir()
	}
	var path string
	if strings.TrimSpace(homeDir) != "" {
		path = filepath.Join(homeDir, "v3", "remote", "budget.json")
	}

	today := utcDay(clk.Now())
	var initialBytes int64
	if path != "" {
		budgetDiskMu.Lock()
		raw, err := os.ReadFile(path)
		budgetDiskMu.Unlock()
		if err == nil {
			var d budgetDisk
			if json.Unmarshal(raw, &d) == nil && d.Day == today && d.Bytes > 0 {
				initialBytes = d.Bytes
			}
		}
	}

	return &Budget{
		path:  path,
		clk:   clk,
		limit: effectiveLimit,
		day:   today,
		bytes: initialBytes,
		done:  make(chan struct{}),
		hooks: make(map[uint64]func()),
	}
}

func resolveBudgetLimit(limit int64) int64 {
	if limit > 0 {
		return limit
	}
	if limit == 0 {
		if raw := strings.TrimSpace(env.Get(budgetEnvDailyMB)); raw != "" {
			if mb, err := strconv.ParseInt(raw, 10, 64); err == nil && mb > 0 {
				return mb << 20
			}
		}
	}
	return DailyBudget
}

func utcDay(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func (b *Budget) rollDayLocked() {
	today := utcDay(b.clk.Now())
	if today != b.day {
		b.day = today
		b.bytes = 0
		b.exhaustedNotified = false
	}
}

func (b *Budget) addOnExhausted(fn func()) func() {
	if b == nil || fn == nil {
		return func() {}
	}
	b.mu.Lock()
	b.nextHookID++
	id := b.nextHookID
	b.hooks[id] = fn
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.hooks, id)
			b.mu.Unlock()
		})
	}
}

// Charge adds n carrier bytes to the current UTC day's count, lazily arming the
// thirty-second persistence timer on the first charge and firing registered
// exhaustion callbacks once when the day's total reaches the limit.
func (b *Budget) Charge(n int64) (over bool) {
	if b == nil {
		return false
	}
	b.mu.Lock()
	b.rollDayLocked()
	if n > 0 {
		b.bytes += n
		b.dirty = true
		if !b.closed && b.timer == nil {
			t := b.clk.NewTimer(budgetPersistInterval)
			b.timer = t
			guard.Go("livedoor.budget.persist", func() {
				b.waitPersist(t)
			})
		}
	}
	over = b.bytes >= b.limit
	var callbacks []func()
	if over && !b.exhaustedNotified {
		b.exhaustedNotified = true
		for _, fn := range b.hooks {
			if fn != nil {
				callbacks = append(callbacks, fn)
			}
		}
	}
	b.mu.Unlock()

	for _, fn := range callbacks {
		fn()
	}
	return over
}

// Exhausted reports whether the current UTC day's byte allowance has been used up.
func (b *Budget) Exhausted() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollDayLocked()
	return b.bytes >= b.limit
}

// Bytes returns the carrier bytes charged so far during the current UTC day.
func (b *Budget) Bytes() int64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollDayLocked()
	return b.bytes
}

// Close stops any pending persistence timer and flushes the current day's byte
// count atomically to <home>/v3/remote/budget.json.
func (b *Budget) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	close(b.done)
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.rollDayLocked()
	day, bytes := b.day, b.bytes
	b.dirty = false
	b.mu.Unlock()

	_ = b.writeDisk(day, bytes)
}

func (b *Budget) waitPersist(t Timer) {
	select {
	case <-t.C():
		budgetDiskMu.Lock()
		defer budgetDiskMu.Unlock()

		b.mu.Lock()
		if b.timer == t {
			b.timer = nil
		}
		if b.closed || !b.dirty {
			b.mu.Unlock()
			return
		}
		b.rollDayLocked()
		day, bytes := b.day, b.bytes
		b.dirty = false
		b.mu.Unlock()

		_ = b.writeDiskLocked(day, bytes)
	case <-b.done:
		t.Stop()
	}
}

func (b *Budget) writeDisk(day string, bytes int64) error {
	budgetDiskMu.Lock()
	defer budgetDiskMu.Unlock()
	return b.writeDiskLocked(day, bytes)
}

func (b *Budget) writeDiskLocked(day string, bytes int64) error {
	if b.path == "" {
		return nil
	}
	dir := filepath.Dir(b.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)
	raw, err := json.Marshal(budgetDisk{
		Day:   day,
		Bytes: bytes,
	})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".budget-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, b.path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(b.path, 0o600)
	return nil
}
