package builder

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/cell"
)

// NewDraft opens a job for an ordinary agent to compose through app edit.
func (s *Jobs) NewDraft(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return "", s.ctx.Err()
	}
	j, err := s.create("")
	if err != nil {
		return "", err
	}
	if name = strings.TrimSpace(name); name != "" {
		j.Name = name
	}
	j.Dirty = true
	return j.Draft, s.save(j, Event{})
}

// Check tests an explicitly edited snapshot. It returns the report without
// asking a model to change the agent's edits during this turn.
func (s *Jobs) Check(ctx context.Context, draft string) (State, error) {
	s.mu.Lock()
	j, err := s.load(draft)
	if err != nil {
		s.mu.Unlock()
		return State{}, err
	}
	if len(j.Queue) != 0 || j.Publishing || j.Package != nil || s.ctx.Err() != nil {
		s.mu.Unlock()
		return State{}, store.ErrConflict
	}
	id, err := cell.NewID(time.Now())
	if err == nil {
		j.Queue = []turn{{ID: id, Stage: Writing, Round: 1, CheckOnly: true}}
		j.State.State, j.Problem = Checking, ""
		j.Progress = []Event{}
		err = s.save(j, Event{})
		if err == nil {
			s.start(j)
		}
	}
	s.mu.Unlock()
	if err != nil {
		return State{}, err
	}
	return s.wait(ctx, draft, id)
}

// Reference returns the file formats and worked apps for the served kit.
func Reference() string { return schema.BuilderReference() }
