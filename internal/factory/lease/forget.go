package lease

import "time"

// ForgetJobs withdraws obsolete work before its records and local files go.
// A worker holding one learns that its lease is gone on its next heartbeat.
func (d *Desk) ForgetJobs(ids map[int]bool) error {
	return d.locked(func(doc *document, _ time.Time) error {
		for id := range ids {
			release(doc, id)
		}
		return d.write(*doc)
	})
}
