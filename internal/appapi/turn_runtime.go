package appapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	turnAttachmentBytes = 8 << 20
	turnToolBytes       = 1 << 20
	turnContextBytes    = 64 << 10
	turnTailEntries     = 100
	turnPageEntries     = 500
	turnLongDelay       = 3 * time.Minute
)

func turnText(ask TurnRequest) string { return contextMessage(ask.Text, ask.Context) }
func drainTurn(events <-chan session.Event) {
	for range events {
	}
}
func copyTurnQueue(queue []QueueItem) []QueueItem { return append([]QueueItem{}, queue...) }
func (a *AgentRegistry) publishTurn(kind EventType, payload any) {
	if a.events != nil {
		_, _ = a.events.Publish(kind, payload)
	}
}

func (a *AgentRegistry) beginTurn(record *agentRecord, live *agentLive, ask TurnRequest, id string) (<-chan session.Event, error) {
	text := turnText(ask)
	var events <-chan session.Event
	var err error
	if len(ask.Files) > 0 {
		door, ok := live.conn.Engine.(interface {
			SubmitFiles(context.Context, string, []remote.WireFile, []session.Image) (<-chan session.Event, error)
		})
		if !ok {
			return nil, agentError(409, "not_allowed", "this engine cannot send files")
		}
		files := []remote.WireFile{}
		total := 0
		for _, file := range ask.Files {
			info, e := os.Stat(file.Path)
			if e != nil || !info.Mode().IsRegular() {
				return nil, agentError(422, "bad_input", "attachments must be readable regular files")
			}
			f, e := os.Open(file.Path)
			if e != nil {
				return nil, agentError(422, "bad_input", "the attached file could not be read")
			}
			info, e = f.Stat()
			if e != nil || !info.Mode().IsRegular() {
				f.Close()
				return nil, agentError(422, "bad_input", "attachments must be regular files")
			}
			raw, e := io.ReadAll(io.LimitReader(f, int64(turnAttachmentBytes-total+1)))
			f.Close()
			total += len(raw)
			if e != nil || total > turnAttachmentBytes {
				return nil, agentError(422, "bad_input", "attachments exceed 8 MiB")
			}
			name := file.Name
			if name == "" {
				name = filepath.Base(file.Path)
			}
			files = append(files, remote.WireFile{Name: name, MIME: file.MIME, Bytes: raw})
		}
		events, err = door.SubmitFiles(context.Background(), text, files, nil)
	} else {
		events, err = live.conn.Engine.Submit(context.Background(), text)
	}
	if err != nil {
		return nil, agentError(409, "conflict", err.Error())
	}
	if events == nil {
		return nil, agentError(502, "conflict", "the engine returned no reply stream")
	}
	if record.Agent.Name == "" && AgentName(ask.Text) != "" {
		record.Agent.Name = AgentName(ask.Text)
		_ = live.conn.Engine.Rename(record.Agent.Name)
	}
	record.TurnSeq++
	if id == "" {
		id = fmt.Sprintf("%s_%d", record.Agent.ID, record.TurnSeq)
	}
	live.generation++
	live.running = true
	live.turnID = id
	live.primary = events
	ask.Expect = nil
	ask.Mode = "send"
	record.LastTurn = &ask
	record.Agent.State = "running"
	record.Agent.Receipt = ""
	return events, nil
}

func (a *AgentRegistry) invalidateTurn(live *agentLive) {
	live.generation++
	live.running = false
	if live.longTimer != nil {
		live.longTimer.Stop()
		live.longTimer = nil
	}
}

// A primary stream is published once. Steer subscriptions are drained separately
// because they repeat the same underlying turn rather than owning another reply.
func (a *AgentRegistry) startTurnPump(id string, live *agentLive, events <-chan session.Event) {
	live.primary = events
	live.longShown = false
	if live.turnID == "" {
		live.turnID = fmt.Sprintf("%s_%d", id, live.generation)
	}
	generation, turn := live.generation, live.turnID
	a.publishTurn(EventTurnStarted, TurnLifecycleEvent{Agent: id, Turn: turn})
	live.longTimer = a.after(turnLongDelay, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if !a.stopped && a.live[id] == live && live.generation == generation && live.running && !live.longShown {
			live.longShown = true
			a.publishTurn(EventTurnLong, TurnLongEvent{Agent: id, Turn: turn, AfterS: int(turnLongDelay / time.Second)})
		}
	})
	go a.pump(id, live, generation, events)
}

func (a *AgentRegistry) pump(id string, live *agentLive, generation uint64, events <-chan session.Event) {
	failed := false
	failureWords := "Stopped · the model provider could not finish this reply"
	endedNormally := false
	for {
		var event session.Event
		var open bool
		select {
		case <-a.done:
			return
		case event, open = <-events:
		}
		if !open {
			break
		}
		a.mu.Lock()
		if a.stopped || a.live[id] != live || live.generation != generation {
			a.mu.Unlock()
			continue
		}
		switch event.Kind {
		case session.EventTextDelta, session.EventReasoning:
			kind := "text"
			if event.Kind == session.EventReasoning {
				kind = "thinking"
			}
			a.publishTurn(EventTurnDelta, TurnDeltaEvent{Agent: id, Turn: live.turnID, Kind: kind, Text: event.Text})
		case session.EventToolBegin, session.EventToolEnd, session.EventToolFailed:
			entry := DisplayEntry{Kind: "tool", Role: "tool", CallID: event.CallID, Tool: event.Tool, Text: event.Hint, Args: event.Args, Output: event.Output, Answered: event.Kind != session.EventToolBegin}
			a.publishTurn(EventTurnEntry, TurnEntryEvent{Agent: id, Entry: entry})
		case session.EventConsentRequest, session.EventQuestion:
			a.mu.Unlock()
			_ = a.transitionState(id, "needs_you", live, generation)
			continue
		case session.EventQuestionAnswered, session.EventQuestionWithdrawn:
			state := "running"
			if live.conn.Engine.NeedsPerson() {
				state = "needs_you"
			}
			a.mu.Unlock()
			_ = a.transitionState(id, state, live, generation)
			continue
		case session.EventAssistantDone:
			a.publishTurn(EventTurnEntry, TurnEntryEvent{Agent: id, Entry: DisplayEntry{Kind: "message", Role: "assistant", Text: event.Text}})
		case session.EventRetrying:
			a.publishTurn(EventTurnEntry, TurnEntryEvent{Agent: id, Entry: DisplayEntry{Kind: "retry", Role: "note", Text: event.Text}})
		case session.EventNotice, session.EventCaption:
			a.publishTurn(EventTurnEntry, TurnEntryEvent{Agent: id, Entry: DisplayEntry{Kind: "notice", Role: "note", Text: event.Text}})
		case session.EventError:
			failed = !errors.Is(event.Err, session.ErrSpendRail)
			if event.Err != nil {
				failureWords = "Stopped · " + redact.Secrets(event.Err.Error())
			}
		case session.EventTurnDone:
			endedNormally = true
		case session.EventTitleChanged:
			a.publishTurn(EventTitleChanged, TitleChangedEvent{Agent: id, Name: event.Text})
		}
		a.mu.Unlock()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.live[id] != live || live.generation != generation || a.stopped {
		return
	}
	if live.longTimer != nil {
		live.longTimer.Stop()
		live.longTimer = nil
	}
	live.running = false
	state, how := "idle", "done"
	if failed {
		state, how = "failed", "failed"
	} else if !endedNormally {
		how = "stopped"
	}
	if live.conn.Engine.NeedsPerson() && !failed {
		state = "needs_you"
	}
	var changed Agent
	var next <-chan session.Event
	var queueChanged bool
	var receipt *DisplayEntry
	var turnEnded TurnLifecycleEvent
	err := a.disk(func(d *agentDisk) error {
		record, ok := d.Records[id]
		if !ok {
			return errAgentMissing
		}
		if record.Agent.State == "closed" || record.Agent.State == "archived" {
			return nil
		}
		record.Agent.State = state
		record.Agent.Version++
		if failed {
			record.Agent.Receipt = failureWords
			r := a.turnReceipt(&record, failureWords, nil)
			r.By = "codeaf"
			record.Receipts[len(record.Receipts)-1].Entry.By = r.By
			receipt = &r
		}
		if how == "stopped" {
			r := a.turnReceipt(&record, "Stopped", nil)
			receipt = &r
		}
		turnEnded = TurnLifecycleEvent{Agent: id, Turn: live.turnID, How: how, Receipt: receipt}
		// A failed or interrupted turn cannot start queued work. The messages stay
		// editable until the person retries, sends another message, or stops them.
		if how == "done" && state == "idle" && len(record.Queue) > 0 {
			item := record.Queue[0]
			stream, e := a.beginTurn(&record, live, TurnRequest{Text: item.Text, Mode: "send", Context: item.Context, Files: item.Files}, item.ID)
			if e != nil {
				record.Agent.Receipt = "Not sent · " + redact.Secrets(e.Error())
			} else {
				next = stream
				record.Queue = record.Queue[1:]
				queueChanged = true
			}
		}
		d.Records[id] = record
		changed = record.Agent
		return nil
	}, true)
	if err != nil {
		return
	}
	if changed.ID != "" {
		a.publishTurn(EventTurnEnded, turnEnded)
		if turnEnded.Receipt != nil {
			a.publishTurn(EventTurnEntry, TurnEntryEvent{Agent: id, Entry: *turnEnded.Receipt})
		}
		a.publish(EventAgentChanged, changed, "state", "glyph")
	}
	if queueChanged {
		_ = a.disk(func(d *agentDisk) error {
			a.publishTurn(EventQueueChanged, QueueChangedEvent{id, copyTurnQueue(d.Records[id].Queue)})
			return nil
		}, false)
	}
	if next != nil {
		a.startTurnPump(id, live, next)
	}
}
