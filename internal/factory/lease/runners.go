package lease

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"time"
)

// Sleep retains leases while macOS is asleep; wake starts a fresh silence deadline.
func (d *Desk) Sleep(machine, handle string, asleep bool) error {
	return d.change(func(doc *document, now time.Time) (bool, error) {
		for i := range doc.Leases {
			held := &doc.Leases[i]
			if held.Machine == machine && held.Handle == handle {
				held.Asleep = asleep
				held.HeartbeatAt, held.ExpiresAt = now, now.Add(d.ttl)
			}
		}
		return true, nil
	})
}

// UnassignPerson revokes execution before clearing ownership so delayed reports cannot write.
func (d *Desk) UnassignPerson(handle string) error {
	handle = factory.NormalizeHandle(handle)
	return d.change(func(doc *document, now time.Time) (bool, error) {
		kept := doc.Leases[:0]
		for _, held := range doc.Leases {
			if held.Handle != handle {
				kept = append(kept, held)
			}
		}
		doc.Leases = kept
		// Persist revoked leases first, matching reserve's ordering across the two files.
		if err := d.write(*doc); err != nil {
			return false, err
		}
		items, err := d.store.List()
		if err != nil {
			return false, err
		}
		for _, it := range items {
			if it.Person() != handle {
				continue
			}
			if d.record != nil {
				if _, err := d.record.Append(context.Background(), record.Event{Kind: record.Released, Job: it.ID, Handle: handle, Detail: "sessions revoked as operator"}); err != nil {
					return false, err
				}
			}
			if err := d.store.Update(it.ID, func(cur *factory.Item) error {
				cur.Taker, cur.Starter, cur.Assignee = "", "", ""
				if cur.State != factory.StateShipped && cur.State != factory.StateDismissed {
					cur.State = factory.StateNew
				}
				cur.Question, cur.PublishStatus = "", nil
				cur.QKind = ""
				cur.Asking = nil
				cur.Publishing = false
				delete(doc.Signals, cur.ID)
				return nil
			}); err != nil {
				return false, err
			}
		}
		return true, nil
	})
}
