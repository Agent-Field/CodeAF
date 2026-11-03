package factory

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

const automaticStandingWait = 5 * time.Second

type automaticKey struct {
	root, item string
}

type automaticStanding struct {
	done   chan struct{}
	closed bool
}

var automaticStandings sync.Map

// ClosedForAutomation remembers a closed answer for this process, shared by
// intake and arrival reads. Failures and open answers never stop work. A
// store without a root can still check, but cannot share remembered answers.
func ClosedForAutomation(ctx context.Context, root string, it Item, standing func(context.Context, Item) (string, error)) bool {
	if standing == nil || it.Origin != OriginForge || it.Num <= 0 {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, automaticStandingWait)
	defer cancel()
	entry := &automaticStanding{done: make(chan struct{})}
	var key automaticKey
	if root != "" {
		root, _ = filepath.Abs(root)
		key = automaticKey{root, fmt.Sprintf("%s/%s/%s#%d", it.Origin, it.Product, it.Repo, it.Num)}
		value, loaded := automaticStandings.LoadOrStore(key, entry)
		if loaded {
			entry = value.(*automaticStanding)
			select {
			case <-ctx.Done():
				return false
			case <-entry.done:
				return entry.closed
			}
		}
	}
	defer close(entry.done)
	how, err := standing(ctx, it)
	entry.closed = err == nil && ctx.Err() == nil && (how == "closed" || how == "merged")
	if !entry.closed && root != "" {
		automaticStandings.Delete(key)
	}
	return entry.closed
}
