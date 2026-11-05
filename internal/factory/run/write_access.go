package run

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// CheckWriteAccess refuses work that can write on a job without permission.
func CheckWriteAccess(it factory.Item, step string) error {
	if it.RefusesWrite() {
		reason := ""
		if it.Tier == "" && it.Failure != nil && strings.HasPrefix(it.Failure.Detail, factory.AuthorAccessUnknownPrefix) {
			reason = it.Failure.Detail
		}
		return WriteAccessRefusal{Step: step, Reason: reason}
	}
	return nil
}

func (r *Runner) allowWrite(ctx context.Context, it factory.Item, st factory.Stage) error {
	if !st.CanWrite() {
		return nil
	}
	if err := CheckWriteAccess(it, st.Name); err != nil {
		return err
	}
	if check, ok := r.publishChecks.Load(it.ID); ok {
		err := check.(func(context.Context) error)(ctx)
		if err != nil && err.Error() == refuseStrangerWrite {
			return WriteAccessRefusal{Step: st.Name}
		}
		return err
	}
	return nil
}

// RefuseWrite saves the reason while leaving the job's run and spending intact.
func RefuseWrite(it *factory.Item, step string, now time.Time) error {
	err := CheckWriteAccess(*it, step)
	var refused WriteAccessRefusal
	if errors.As(err, &refused) {
		factory.StampFailure(it, refused.Failure(*it, now))
	}
	return err
}

func (r *Runner) refuseWrite(id int, step string) error {
	it, err := r.opts.Store.Get(id)
	if err != nil {
		return err
	}
	if CheckWriteAccess(it, step) == nil {
		return nil
	}
	var refusal error
	err = r.opts.Store.Update(id, func(it *factory.Item) error {
		refusal = RefuseWrite(it, step, r.now())
		return nil
	})
	if err != nil {
		return err
	}
	return refusal
}

// CheckJobChat checks permission before opening a manager turn.
func (r *Runner) CheckJobChat(ctx context.Context, it factory.Item) error {
	return r.allowWrite(ctx, it, factory.Stage{Name: "chat", Kind: factory.StageChat})
}
