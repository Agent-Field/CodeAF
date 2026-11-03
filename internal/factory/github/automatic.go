package github

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const automaticStandingWait = 5 * time.Second

type automaticKey struct {
	root, repo string
	kind       factory.Kind
	number     int
}

type automaticStanding struct {
	sync.Mutex
	closed bool
}

var automaticStandings sync.Map

// ClosedForAutomation remembers a closed answer for this process, shared by
// intake and arrival reads. Failures and open answers never stop work. A
// store without a root can still check, but cannot share remembered answers.
func ClosedForAutomation(ctx context.Context, root string, it factory.Item, standing func(context.Context, factory.Item) (string, error)) bool {
	if standing == nil || it.Origin != factory.OriginForge || it.Num <= 0 {
		return false
	}
	entry := &automaticStanding{}
	if root != "" {
		root, _ = filepath.Abs(root)
		value, _ := automaticStandings.LoadOrStore(automaticKey{root, it.Repo, it.Kind, it.Num}, entry)
		entry = value.(*automaticStanding)
	}
	entry.Lock()
	defer entry.Unlock()
	if entry.closed {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, automaticStandingWait)
	defer cancel()
	how, err := standing(ctx, it)
	if err != nil || ctx.Err() != nil {
		return false
	}
	entry.closed = how == StandingClosed || how == StandingMerged
	return entry.closed
}
