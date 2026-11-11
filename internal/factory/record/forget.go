package record

import (
	"context"
	"encoding/json"
	"os"
)

// ForgetJobs is the explicit deprecation cleanup for erased standalone PR
// jobs. It does not write a tombstone or retain their approval/event payloads.
func (r *Record) ForgetJobs(ctx context.Context, ids map[int]bool) error {
	return r.locked(ctx, true, func() error {
		events, err := r.read(ctx)
		if err != nil {
			return err
		}
		file, err := os.CreateTemp(r.root, ".forget-events-*")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		defer file.Close()
		encoder := json.NewEncoder(file)
		for _, e := range events {
			if !ids[e.Job] {
				if err := encoder.Encode(e); err != nil {
					return err
				}
			}
		}
		if err := file.Sync(); err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return os.Rename(file.Name(), r.path())
	})
}
