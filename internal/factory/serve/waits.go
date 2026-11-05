package serve

import (
	"context"
	"errors"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const waitsScanEvery = 2 * time.Second
const waitsSafetyEvery = time.Minute

type waitsScheduler struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

var errWaitsUnchanged = errors.New("waits did not change")

// scanWaits lists once, then locks only the child being changed. Parent reads
// belong to that snapshot, so no two job locks are ever held together.
func (h *Handler) scanWaits() error {
	h.waits.mu.Lock()
	defer h.waits.mu.Unlock()
	st := h.config.Store
	if st == nil {
		return nil
	}
	items, err := st.List()
	if err != nil {
		return err
	}
	var parents map[int]factory.Item
	for _, it := range items {
		if len(it.WaitsOn) != 0 && !it.WaitsReleased {
			parents = make(map[int]factory.Item, len(items))
			break
		}
	}
	if parents == nil {
		return nil
	}
	for _, it := range items {
		parents[it.ID] = it
	}
	var errs []error
	for _, it := range items {
		if it.WaitsReleased || len(it.WaitsOn) == 0 {
			continue
		}
		if !waitsChange(it, factory.EvaluateWaits(it, parents)) {
			continue
		}
		err := st.Update(it.ID, func(cur *factory.Item) error {
			if cur.WaitsReleased || len(cur.WaitsOn) == 0 {
				return errWaitsUnchanged
			}
			rule := factory.EvaluateWaits(*cur, parents)
			if !waitsChange(*cur, rule) {
				return errWaitsUnchanged
			}
			if rule.Released {
				h.releaseWaits(cur, parents)
				return nil
			}
			cur.WaitingOn = rule.WaitingOn
			if !sameWaitsBlock(cur.Blocked, rule.Blocked) {
				cur.Blocked = rule.Blocked
				if cur.Blocked != nil {
					cur.Blocked.Since = h.clock().UTC()
				}
			}
			return nil
		})
		if err != nil && !errors.Is(err, errWaitsUnchanged) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func waitsChange(it factory.Item, rule factory.WaitsRule) bool {
	return rule.Released || !slices.Equal(it.WaitingOn, rule.WaitingOn) || !sameWaitsBlock(it.Blocked, rule.Blocked)
}

func sameWaitsBlock(a, b *factory.JobBlock) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.By == b.By && a.Job == b.Job && a.Reason == b.Reason
}

// releaseWaits is the one release decision. It makes the job startable;
// a person's earlier start only retires the waits, without a release event.
func (h *Handler) releaseWaits(it *factory.Item, parents map[int]factory.Item) {
	it.WaitsReleased, it.Blocked = true, nil
	it.WaitingOn = nil
	if factory.WaitsOverridden(*it) {
		return
	}
	words := make([]string, 0, len(it.WaitsOn))
	for _, id := range it.WaitsOn {
		parent := parents[id]
		why := "merged"
		if parent.PullRequest == nil {
			why = "shipped with nothing to merge"
		}
		words = append(words, factory.JobRef(parent)+" "+why)
	}
	it.Note(h.clock().UTC(), "waits over: "+strings.Join(words, ", "))
}

func (h *Handler) startWaits() {
	if h.config.Store == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.waits.cancel, h.waits.done = cancel, make(chan struct{})
	var changes chan string
	if h.watch != nil {
		changes = h.watch.floor.join()
	}
	scan := func() {
		if err := h.scanWaits(); err != nil {
			log.Printf("factory: could not check job waits: %v", err)
		}
	}
	scan()
	go func() {
		defer close(h.waits.done)
		if changes != nil {
			defer h.watch.floor.leave(changes)
		}
		tick := time.NewTicker(waitsSafetyEvery)
		defer tick.Stop()
		last := time.Now()
		var timer *time.Timer
		var due <-chan time.Time
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-changes:
			case <-tick.C:
			case <-due:
				timer, due = nil, nil
			}
			if ctx.Err() != nil {
				return
			}
			if left := waitsScanEvery - time.Since(last); left > 0 {
				if timer == nil {
					timer = time.NewTimer(left)
					due = timer.C
				}
				continue
			}
			scan()
			last = time.Now()
		}
	}()
}

func (h *Handler) stopWaits() {
	if h.waits.cancel != nil {
		h.waits.cancel()
		<-h.waits.done
	}
}
