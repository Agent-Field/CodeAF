package cellbudget

import (
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

// Rule accepts a cell for eviction by returning nil, or says why not.
type Rule func(c cell.Cell, e Entry) error

func idle(busy func(cell.Cell) bool) Rule {
	return func(c cell.Cell, _ Entry) error {
		if busy(c) {
			return errors.New("running")
		}
		return nil
	}
}

// justOpened protects a cell between the moment it was opened and the moment
// its session takes the transcript lock.
func justOpened(grace time.Duration, now func() time.Time) Rule {
	return func(_ cell.Cell, e Entry) error {
		if now().Sub(time.UnixMilli(e.OpenedMs)) < grace {
			return errors.New("opened recently")
		}
		return nil
	}
}

// sealed accepts a cell whose head is sealed and whose call log holds nothing
// pending: no call in flight, no finished call the next seal has yet to take.
func sealed(store cellstore.Engine) Rule {
	return func(c cell.Cell, _ Entry) error {
		head, err := cellstore.Head(c)
		if err != nil {
			return fmt.Errorf("head unreadable: %w", err)
		}
		if head == nil {
			return errors.New("never sealed")
		}
		return pendingCalls(store, c)
	}
}

func pendingCalls(store cellstore.Engine, c cell.Cell) error {
	_, rec, err := cellstore.OpenWAL(store.WALPath(c))
	if err != nil {
		return fmt.Errorf("call log unreadable: %w", err)
	}
	if len(rec.Incomplete)+len(rec.Completed) > 0 {
		return errors.New("unsealed calls")
	}
	return nil
}
