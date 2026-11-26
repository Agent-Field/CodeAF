package runs

import (
	"errors"
	"strings"
)

// StopAgent makes the stop durable before cancelling the worker. A late
// response cannot resurrect the checkpoint or start another effect.
func (e *Engine) StopAgent(app, id string) error {
	e.mu.Lock()
	c, err := e.agentControlTarget(app, id)
	if err == nil {
		c.State, c.Error, c.Approval, c.EndedAt, c.Stopped = Incomplete, "Stopped", nil, e.cfg.Now(), true
		err = e.saveLocked(&c, "", nil)
		if err == nil && e.workCancels[id] != nil {
			e.workCancels[id]()
		}
	}
	e.mu.Unlock()
	if err == nil {
		e.changed(c.Run)
	}
	return err
}

// SteerAgent adds a person's follow-up to the next undispatched model step.
func (e *Engine) SteerAgent(app, id, text string) error {
	if strings.TrimSpace(text) == "" || len(text) > 16000 {
		return errors.New("enter a follow-up under 16,000 bytes")
	}
	e.mu.Lock()
	c, err := e.agentControlTarget(app, id)
	if err == nil && (c.State != Running || c.Action != nil || e.busy[id] || len(c.Steering) >= 5) {
		err = errors.New("this run already started its current step")
	}
	if err == nil {
		c.Steering = append(c.Steering, text)
		err = e.saveLocked(&c, "", nil)
	}
	e.mu.Unlock()
	if err == nil {
		e.changed(c.Run)
	}
	return err
}

func (e *Engine) agentControlTarget(app, id string) (checkpoint, error) {
	raw := e.checkpoints[runPrefix+id]
	if raw == nil {
		return checkpoint{}, ErrNotFound
	}
	c, err := decode[checkpoint](raw)
	if err == nil && (c.App != app || c.Harness == "" || c.State != Running && c.State != Finishing && c.State != YourCall) {
		err = ErrNotFound
	}
	return c, err
}
