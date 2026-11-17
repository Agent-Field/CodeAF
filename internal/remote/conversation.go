package remote

import (
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// RewindKeep and UndoRewind forward to the conversation that owns the journal.
func (a *Agent) RewindKeep(point int) (string, error) {
	raw, err := a.c.call(nil, "RewindKeep", point)
	if err != nil {
		return "", err
	}
	var token string
	err = json.Unmarshal(raw, &token)
	return token, err
}
func (a *Agent) UndoRewind(token string) error {
	_, err := a.c.call(nil, "UndoRewind", token)
	return err
}
func (a *Agent) DebugConversation() (string, string, error) {
	raw, err := a.c.call(nil, "DebugConversation", nil)
	if err != nil {
		return "", "", err
	}
	var out DebugRecord
	err = json.Unmarshal(raw, &out)
	return out.Path, out.Message, err
}

// DebugRecord carries the exact recording receipt from the owning machine.
type DebugRecord struct{ Path, Message string }

// DeleteOwnConversation uses the saved transcript named by the engine, never a client path.
func (a *Agent) DeleteOwnConversation() error {
	_, err := a.c.call(nil, "DeleteOwnConversation", nil)
	return err
}

func invokeConversation(engine *Engine, agent WrappedAgent, method string, payload json.RawMessage) ([]byte, bool, error) {
	var raw []byte
	var err error
	switch method {
	case "RewindKeep":
		raw, err = keepConversationRewind(agent, payload)
	case "UndoRewind":
		err = undoConversationRewind(agent, payload)
	case "DebugConversation":
		raw, err = debugConversation(agent)
	case "DeleteOwnConversation":
		err = deleteOwnConversation(engine)
	default:
		return nil, false, nil
	}
	return raw, true, err
}

func keepConversationRewind(agent WrappedAgent, payload json.RawMessage) ([]byte, error) {
	c, ok := agent.(interface{ RewindKeep(int) (string, error) })
	if !ok {
		return nil, errors.New("this engine cannot keep a rewind for undo")
	}
	var point int
	if err := json.Unmarshal(payload, &point); err != nil {
		return nil, err
	}
	token, err := c.RewindKeep(point)
	if err != nil {
		return nil, err
	}
	return json.Marshal(token)
}

func undoConversationRewind(agent WrappedAgent, payload json.RawMessage) error {
	c, ok := agent.(interface{ UndoRewind(string) error })
	if !ok {
		return errors.New("this engine cannot undo a rewind")
	}
	var token string
	if err := json.Unmarshal(payload, &token); err != nil {
		return err
	}
	return c.UndoRewind(token)
}

func debugConversation(agent WrappedAgent) ([]byte, error) {
	c, ok := agent.(interface {
		DebugConversation() (string, string, error)
	})
	if !ok {
		return nil, errors.New("this engine cannot keep a debug record")
	}
	path, message, err := c.DebugConversation()
	if err != nil {
		return nil, err
	}
	return json.Marshal(DebugRecord{path, message})
}

func deleteOwnConversation(engine *Engine) error {
	if engine.DeleteConversation == nil {
		return errors.New("this engine does not offer permanent deletion")
	}
	f, err := teams.Load(engine.ProfileDir)
	if err != nil {
		return err
	}
	choices, err := session.ConversationDeleteChoices(f, engine.SessionFile)
	if err != nil {
		return err
	}
	return engine.DeleteConversation(engine.SessionFile, choices, nil)
}
