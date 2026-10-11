package record

import (
	"context"
	"encoding/csv"
	"io"
	"strconv"
	"time"
)

// CSV uses the same filter and order as audit so an export cannot invent history.
func (r *Record) CSV(ctx context.Context, out io.Writer, since time.Time) error {
	events, err := r.Since(ctx, since)
	if err != nil {
		return err
	}
	w := csv.NewWriter(out)
	if err := w.Write([]string{"id", "at", "kind", "job", "handle", "cost", "detail", "commit"}); err != nil {
		return err
	}
	for _, e := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := w.Write([]string{e.ID, e.At.Format(time.RFC3339Nano), e.Kind, strconv.Itoa(e.Job), e.Handle, strconv.FormatFloat(e.Cost, 'f', -1, 64), e.Detail, e.Commit}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
