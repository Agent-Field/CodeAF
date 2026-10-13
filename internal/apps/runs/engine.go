package runs

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// defaultModel is the only model wave 1 spends budget on.
const defaultModel = "deepseek/deepseek-v4.1-flash"

// maxDriveSteps stops a stuck interpreter from spinning the sidecar.
const maxDriveSteps = 10000

// engines is one journal per apps root. New, NewApprovals and NewConnectors
// share it: a decision has to wake the run that opened the approval.
var (
	enginesMu sync.Mutex
	engines   = map[string]*engine{}
)

type engine struct {
	ctx  context.Context
	root string
	all  *svc.Services

	// now and sleep are atomic so a test can pin the clock without racing the ticker.
	now     atomic.Value // func() time.Time
	sleeper atomic.Value // func(context.Context, time.Duration) error

	mu       sync.Mutex
	dbs      map[string]*dbConn
	busy     map[string]bool
	seen     map[string]map[string]any // app/type/id → last field snapshot
	lastFire map[string]time.Time      // app/trigger → last schedule minute
	calls    []recorded
	replies  map[string]any
	started  chan string // test hook; nil in the server
	ready    chan struct{}
	wg       sync.WaitGroup
}

type recorded struct {
	App  string
	Tool string
	Args map[string]any
}

func shared(cfg svc.Config, all *svc.Services) *engine {
	enginesMu.Lock()
	defer enginesMu.Unlock()
	if e, ok := engines[cfg.Root]; ok {
		return e
	}
	e := newEngine(cfg, all)
	engines[cfg.Root] = e
	return e
}

func newEngine(cfg svc.Config, all *svc.Services) *engine {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	e := &engine{
		ctx:      ctx,
		root:     cfg.Root,
		all:      all,
		dbs:      map[string]*dbConn{},
		busy:     map[string]bool{},
		seen:     map[string]map[string]any{},
		lastFire: map[string]time.Time{},
		replies:  map[string]any{},
		ready:    make(chan struct{}),
	}
	e.now.Store(func() time.Time { return time.Now() })
	e.sleeper.Store(realSleep)
	e.snapshot(ctx)
	// Resume before the ticker starts, so a start that lands in the same
	// moment is not also claimed by the startup sweep.
	e.resumeRunning()
	e.wg.Add(2)
	go func() { defer e.wg.Done(); e.loop() }()
	go func() { defer e.wg.Done(); e.listen() }()
	<-e.ready
	return e
}

func (e *engine) clock() time.Time {
	return e.now.Load().(func() time.Time)()
}

func (e *engine) nap(ctx context.Context, d time.Duration) error {
	return e.sleeper.Load().(func(context.Context, time.Duration) error)(ctx, d)
}

func realSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (e *engine) loop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.pump(e.ctx)
		}
	}
}

func (e *engine) pump(ctx context.Context) {
	e.resumeDue(ctx)
	e.fireSchedules(ctx)
}

func (e *engine) begin(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.busy[id] {
		return false
	}
	e.busy[id] = true
	return true
}

func (e *engine) end(id string) {
	e.mu.Lock()
	delete(e.busy, id)
	e.mu.Unlock()
}

func (e *engine) noteStarted(id string) {
	e.mu.Lock()
	ch := e.started
	e.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- id:
	default:
	}
}

func (e *engine) closeDBs() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, conn := range e.dbs {
		_ = conn.db.Close()
		delete(e.dbs, id)
	}
}
