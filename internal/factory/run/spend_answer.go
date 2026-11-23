package run

import (
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// SpendingAnswer refuses every question except the person's spending hold.
func SpendingAnswer(it factory.Item) error {
	if it.State != factory.StateNeedsYou || it.QKind != factory.QKindSpendStopped || it.Question == "" {
		return fmt.Errorf("%s is not waiting on a spending choice", it.Ref())
	}
	return nil
}

// SpendAnswer validates the revision, question and caller inside the item write.
// A live run consumes its pending question while holding the loop lock; a saved
// run resumes through the same path as an ordinary spending answer.
func (r *Runner) SpendAnswer(id, revision int, yes bool, may func(factory.Item) error) (factory.Item, error) {
	lp := r.loop()
	lp.mu.Lock()
	c := lp.ctls[id]
	live := c != nil && c.pending == factory.QKindSpendStopped
	var before factory.Item
	err := r.opts.Store.UpdateExpected(id, revision, func(it *factory.Item) error {
		if err := SpendingAnswer(*it); err != nil {
			return err
		}
		if may != nil {
			if err := may(*it); err != nil {
				return err
			}
		}
		raw, err := json.Marshal(it)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &before); err != nil {
			return err
		}
		if live {
			return nil
		}
		if c != nil {
			return fmt.Errorf("%s is not waiting on a spending choice", it.Ref())
		}
		if err := AnswerUnheld(it, yes, "", r.now()); err != nil {
			return err
		}
		if yes && it.Stream != nil {
			it.Stream.Paused = true
		}
		return nil
	})
	if err == nil && live {
		c.pending = ""
	}
	lp.mu.Unlock()
	if err != nil {
		return factory.Item{}, err
	}
	if live {
		c.answers <- loopAnswer{yes: yes}
	} else if yes {
		err = r.Hold(id, false)
	}
	return before, err
}
