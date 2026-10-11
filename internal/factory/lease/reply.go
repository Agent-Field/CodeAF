package lease

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Reply keeps the person's words on the job before a worker can collect them.
// No lease is needed: the machine that owns its manager can pull just this turn.
func (d *Desk) Reply(id int, from, words string) error {
	words = strings.TrimSpace(words)
	if words == "" {
		return fmt.Errorf("nothing to say")
	}
	if len(words) > factory.MessageTextMost {
		return fmt.Errorf("reply exceeds %d bytes", factory.MessageTextMost)
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	return d.change(func(_ *document, now time.Time) (bool, error) {
		return false, d.store.Update(id, func(it *factory.Item) error {
			if it.Talk == "" && it.State != factory.StateQueued && it.State != factory.StateRunning {
				return fmt.Errorf("the job has no manager conversation")
			}
			pending := 0
			for _, m := range it.Thread {
				if m.State == "pending" {
					pending++
				}
			}
			if pending >= factory.ThreadMost {
				return fmt.Errorf("job has too many pending replies")
			}
			it.Thread = append(it.Thread, factory.Message{ID: "reply_" + hex.EncodeToString(token[:]), At: now, From: from, Text: words, State: "pending"})
			return factory.MergeThread(it, nil, nil)
		})
	})
}
