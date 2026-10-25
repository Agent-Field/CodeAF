package main

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

type appRewindConversation interface {
	RewindPoints() []session.RewindPoint
	Transcript() []session.DisplayEntry
	RewindKeep(int) (string, error)
	UndoRewind(string) error
}

func (b *appBackend) ConversationPoints(id string) ([]appwire.ConversationPoint, error) {
	_, live, endLive, err := b.borrowLive(id)
	if err != nil {
		return nil, err
	}
	defer endLive()
	c, ok := live.conn.Conv.(appRewindConversation)
	if !ok {
		return nil, appwire.Errorf(appwire.CodeUnknownMethod, "this engine cannot rewind conversations")
	}
	points, entries := c.RewindPoints(), c.Transcript()
	rows := make([]appwire.ConversationPoint, 0)
	after := 0
	for i := len(points) - 1; i >= 0; i-- {
		p := points[i]
		if !p.Turn {
			continue
		}
		var at time.Time
		if p.Entry < len(entries) {
			at = entries[p.Entry].At
		}
		words := strings.Fields(p.Said)
		if len(words) > 12 {
			words = words[:12]
		}
		rows = append(rows, appwire.ConversationPoint{Point: p.Index, At: at, Words: appClipUTF8(strings.Join(words, " "), 512), TurnsAfter: after})
		after++
		if len(rows) == 1000 {
			break
		}
	}
	return rows, nil
}

func (b *appBackend) RewindConversation(id string, point int) (string, error) {
	a, live, endLive, err := b.borrowLive(id)
	if err != nil {
		return "", err
	}
	defer endLive()
	a.gate.Lock()
	defer a.gate.Unlock()
	c, ok := live.conn.Conv.(appRewindConversation)
	if !ok {
		return "", appwire.Errorf(appwire.CodeUnknownMethod, "this engine cannot keep a rewind for undo")
	}
	token, err := c.RewindKeep(point)
	if err == nil {
		b.resetConversationCount(a, c.Transcript())
	}
	return token, err
}

func (b *appBackend) UndoConversationRewind(id, token string) error {
	a, live, endLive, err := b.borrowLive(id)
	if err != nil {
		return err
	}
	defer endLive()
	a.gate.Lock()
	defer a.gate.Unlock()
	c, ok := live.conn.Conv.(appRewindConversation)
	if !ok {
		return appwire.Errorf(appwire.CodeUnknownMethod, "this engine cannot undo a rewind")
	}
	err = c.UndoRewind(token)
	if err == nil {
		b.resetConversationCount(a, c.Transcript())
	}
	return err
}

// Entry counts are read again after an edit instead of numbering from the old tail.
func (b *appBackend) resetConversationCount(a *appAgent, entries []session.DisplayEntry) {
	users := 0
	for _, e := range entries {
		if e.Role == "user" {
			users++
		}
	}
	b.locked(func() { a.users, a.counted = users, true; a.marks = nil; a.failed = ""; a.failedShown = nil })
}

func (b *appBackend) DebugAgent(id string) (appwire.ConversationDebug, error) {
	_, live, endLive, err := b.borrowLive(id)
	if err != nil {
		return appwire.ConversationDebug{}, err
	}
	defer endLive()
	c, ok := live.conn.Conv.(interface {
		DebugConversation() (string, string, error)
	})
	if !ok {
		return appwire.ConversationDebug{}, appwire.Errorf(appwire.CodeUnknownMethod, "this engine cannot keep a debug record")
	}
	path, message, err := c.DebugConversation()
	return appwire.ConversationDebug{Path: path, Message: message}, err
}

func (b *appBackend) ManualAgent(id, question, clientID string) (uint64, error) {
	text, said := session.ManualCommand(question)
	return b.SendMessage(id, appwire.AgentMessage{Text: text, Said: &said, ClientID: clientID})
}

func (b *appBackend) DeleteAgent(id string) error {
	a, err := b.find(id)
	if err != nil {
		return err
	}
	b.lockRelease(a)
	defer a.gate.Unlock()
	_, live, err := b.heldLiveLocked(a)
	if err != nil {
		return err
	}
	c, ok := live.conn.Conv.(interface{ DeleteOwnConversation() error })
	if !ok {
		return appwire.Errorf(appwire.CodeUnknownMethod, "this engine does not offer permanent deletion")
	}
	// Deletion holds the same service fence as Close until the record and
	// its live handle are gone. A failed stop leaves the record available.
	defer b.fenceAgentServices(id)()
	if err := b.stopAgentServices(id); err != nil {
		return err
	}
	if err := c.DeleteOwnConversation(); err != nil {
		return err
	}
	b.release(a)
	b.locked(func() { delete(b.agents, id) })
	b.restMemo.forget(id)
	appRestDisk.forget(id)
	return nil
}
