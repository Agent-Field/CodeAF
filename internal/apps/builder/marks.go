package builder

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/cell"
)

type Mark struct {
	ID         string `json:"id"`
	Checkpoint string `json:"checkpoint,omitempty"`
	Target     Target `json:"target"`
	Text       string `json:"text"`
}

// Presence is held by a running turn. Its ID and revision come from the job
// lease, and the agent command only supplies the element it is editing.
type Presence struct {
	Actor    string    `json:"actor"`
	Target   Target    `json:"target"`
	Revision uint64    `json:"revision"`
	At       time.Time `json:"at"`
}

func (j *job) mark(text string, target Target) error {
	id, err := cell.NewID(time.Now())
	if err != nil {
		return err
	}
	checkpoint := target.Checkpoint
	if checkpoint == "" && len(j.Checkpoints) > 0 {
		checkpoint = j.Checkpoints[len(j.Checkpoints)-1].ID
	}
	if checkpoint != "" {
		if _, found := j.Saved[checkpoint]; !found {
			return store.ErrNotFound
		}
	}
	j.Marks = append(j.Marks, Mark{ID: id, Checkpoint: checkpoint, Target: target, Text: text})
	return nil
}

func (s *Jobs) claim(ctx context.Context, draft, turnID string, target Target) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.load(draft)
	if err != nil {
		return err
	}
	if ctx.Err() != nil || len(j.Queue) == 0 || j.Queue[0].ID != turnID {
		return store.ErrConflict
	}
	j.Presence = &Presence{Actor: turnID, Target: target, Revision: j.Revision + 1, At: time.Now().UTC()}
	return s.save(j, Event{})
}

func (s *Jobs) Mark(draft, text string, target Target) error {
	return s.Say(draft, text, &target)
}
