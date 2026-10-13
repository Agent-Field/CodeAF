package appapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Ask records retain answers and withdrawals so a second window can read the
// receipt after the engine has released the original question channel.
// One question subscription feeds both the Ask record and the derived Inbox.
type askObserver struct {
	mu    sync.Mutex
	inbox func(session.Event)
}

func (o *askObserver) deliver(ev session.Event) {
	o.mu.Lock()
	callback := o.inbox
	o.mu.Unlock()
	if callback != nil {
		callback(ev)
	}
}

func (s *Server) observeInboxAsks(id string, o *askObserver) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.inbox = func(event session.Event) {
		if event.Question == nil {
			return
		}
		how, by := "no_longer_needed", ""
		switch event.Kind {
		case session.EventQuestionAnswered:
			how, by = "in_place", "you"
			if event.Answer != nil && event.Answer.From == "inbox" {
				how = "here"
			}
		case session.EventQuestionWithdrawn:
		default:
			return
		}
		s.ResolveInbox(inboxQuestionID(id, *event.Question), how, by)
	}
}

type savedAsk struct {
	Ask        Ask              `json:"ask"`
	Question   session.Question `json:"question"`
	Permission *Permission      `json:"permission,omitempty"`
	Args       json.RawMessage  `json:"args,omitempty"`
	Tool       string           `json:"tool,omitempty"`
	Held       bool             `json:"held,omitempty"`
	Version    uint64           `json:"version"`
}
type askDisk struct {
	Asks map[string]savedAsk `json:"asks"`
}

func askID(agent string, q session.Question) string {
	return "ask_" + base64.RawURLEncoding.EncodeToString([]byte(agent+"\x00"+string(q.Kind)+"\x00"+q.Token()+"\x00"+fmt.Sprint(q.Asked.UnixNano())))
}
func askAgent(id string) string {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "ask_"))
	if err != nil || !strings.HasPrefix(id, "ask_") {
		return ""
	}
	fields := strings.Split(string(raw), "\x00")
	if len(fields) != 4 {
		return ""
	}
	return fields[0]
}

func (a *AgentRegistry) asks(save bool, fn func(*askDisk) error) error {
	dir := filepath.Join(a.home, "v3", "app")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "asks.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer func() { _ = filelock.Unlock(lock) }()
	path := filepath.Join(dir, "asks.json")
	d := askDisk{Asks: map[string]savedAsk{}}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
	}
	if d.Asks == nil {
		d.Asks = map[string]savedAsk{}
	}
	if err := fn(&d); err != nil {
		return err
	}
	if !save {
		return nil
	}
	raw, err = json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".asks-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (a *AgentRegistry) questionAsk(agent string, q session.Question, entries []DisplayEntry) savedAsk {
	ask := turnAsk(agent, q)
	// Retrieval keeps exactly two context slots, even at the start of a conversation.
	ask.Before = make([]DisplayEntry, 2)
	tail := entries[max(0, len(entries)-2):]
	copy(ask.Before[2-len(tail):], tail)
	record := savedAsk{Ask: ask, Question: q, Version: 1}
	if q.Kind == session.QuestionConsent {
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if e.CallID == q.Subject.CallID {
				record.Tool = e.Tool
				record.Args = json.RawMessage(e.Args)
				break
			}
		}
		if record.Tool == "" {
			record.Tool = q.Subject.Name
		}
		if q.PermissionCommand != "" {
			record.Args, _ = json.Marshal(map[string]string{"command": q.PermissionCommand})
		}
		command := config.AppPermissionCommand(record.Tool, record.Args)
		if len(record.Args) == 0 {
			command = q.Head
		}
		record.Permission = &Permission{ID: ask.ID, AgentID: agent, Command: command, Class: config.AppPermissionClass(record.Tool, record.Args), Irreversible: ask.Irreversible, Why: q.Reason, Before: ask.Before}
		if len(q.Blocking.Tasks) > 0 {
			record.Permission.TaskID = q.Blocking.Tasks[0]
			record.Ask.Source = SourceRef{Kind: "task", ID: record.Permission.TaskID}
			record.Ask.Path = append(record.Ask.Path, record.Ask.Source)
		}
		record.Ask.Permission = record.Permission
	}
	return record
}

// The question lane is independent of turn and task subscriptions. It hears
// answers from the Inbox, another window and automatic rules through one door.
func (a *AgentRegistry) watchAsks(agent string, conn *AgentConnection) {
	door, ok := conn.Engine.(interface {
		WatchQuestions() (<-chan session.Event, func())
	})
	if !ok {
		return
	}
	events, stop := door.WatchQuestions()
	observer := &askObserver{}
	conn.asks = observer
	var once sync.Once
	done := make(chan struct{})
	oldClose := conn.Close
	conn.Close = func() {
		once.Do(func() {
			close(done)
			stop()
			if oldClose != nil {
				oldClose()
			}
		})
	}
	engine := conn.Engine
	go func() {
		timers := map[string]*time.Timer{}
		defer func() {
			for _, timer := range timers {
				if timer != nil {
					timer.Stop()
				}
			}
		}()
		for {
			select {
			case <-done:
				return
			case <-a.done:
				return
			case ev, open := <-events:
				if !open {
					return
				}
				if ev.Question == nil {
					continue
				}
				path, _ := a.CrewQuestionPath(agent)
				a.mu.Lock()
				id := askID(agent, *ev.Question)
				var saved savedAsk
				err := a.observeAskLocked(agent, ev, &saved, path)
				if err == nil && saved.Ask.State == "waiting" && !saved.Held && saved.Ask.Clock != nil {
					if _, known := timers[id]; !known {
						delay := saved.Ask.Clock.PausesAt.Sub(a.now())
						timers[id] = a.after(max(0, delay), func() { a.pauseAsk(agent, id, engine) })
					}
				} else {
					if timer := timers[id]; timer != nil {
						timer.Stop()
					}
					delete(timers, id)
				}
				a.mu.Unlock()
				observer.deliver(ev)
			}
		}
	}()
}

func (a *AgentRegistry) observeAskLocked(agent string, ev session.Event, out *savedAsk, paths ...[]SourceRef) error {
	q := *ev.Question
	var entries []DisplayEntry
	if err := a.disk(func(d *agentDisk) error {
		r, ok := d.Records[agent]
		if !ok {
			return errAgentMissing
		}
		entries = a.turnBaseEntries(r)
		return nil
	}, false); err != nil {
		return err
	}
	var event EventType
	var how string
	var receipt *DisplayEntry
	err := a.asks(true, func(d *askDisk) error {
		id := askID(agent, q)
		old, known := d.Asks[id]
		if known && old.Ask.State != "waiting" {
			*out = old
			return nil
		}
		row := a.questionAsk(agent, q, entries)
		if len(paths) > 0 && len(paths[0]) > 1 {
			row.Ask.Path = append([]SourceRef{}, paths[0]...)
			if row.Ask.Source.Kind == "task" {
				row.Ask.Path = append([]SourceRef{row.Ask.Source}, row.Ask.Path...)
			} else {
				row.Ask.Source = paths[0][0]
			}
		}
		if known {
			row.Ask.Before = old.Ask.Before
			if row.Permission != nil {
				row.Permission.Before = old.Ask.Before
				row.Ask.Permission = row.Permission
			}
			row.Held = old.Held
			row.Version = old.Version
			row.Ask.Clock = old.Ask.Clock
		}
		if ev.Kind == session.EventQuestion {
			if !known {
				event = EventAskCreated
			}
			if !known && (q.Kind == session.QuestionConsent || q.Kind == session.QuestionAsk) && (q.Blocking.Turn || len(q.Blocking.Tasks) > 0) {
				profile := a.askProfile
				if profile == "" {
					profile = a.home
				}
				duration, err := config.AppQuestionClock(profile)
				if err != nil {
					return err
				}
				start := q.Asked
				if start.IsZero() {
					start = a.now().UTC()
				}
				pause := start.Add(duration)
				row.Ask.Clock = &AskClock{StartsAt: &start, PausesAt: &pause}
			}
		} else {
			text, by, resolvedHow := askResolution(q, ev.Answer)
			how = resolvedHow
			if ev.Kind == session.EventQuestionWithdrawn {
				row.Ask.State = "withdrawn"
				text = "No longer needed — the turn moved on"
				by = "codeaf"
				how = "withdrawn"
				event = EventAskWithdrawn
			} else {
				row.Ask.State = "answered"
				event = EventAskResolved
			}
			row.Ask.Clock = nil
			row.Version++
			at := a.now().UTC()
			if ev.Answer != nil && !ev.Answer.At.IsZero() {
				at = ev.Answer.At
			}
			receipt = &DisplayEntry{ID: "receipt_" + id, Kind: "receipt", Role: "note", Text: text, By: by, At: &at, Link: &SourceRef{Kind: "ask", ID: id}}
			if ev.Answer != nil && strings.HasPrefix(ev.Answer.From, "rule:") {
				receipt.Link = &SourceRef{Kind: "rule", ID: strings.TrimPrefix(ev.Answer.From, "rule:")}
			}
			row.Ask.Receipt = receipt
		}
		d.Asks[id] = row
		*out = row
		return nil
	})
	if err != nil {
		return err
	}
	if receipt != nil {
		if err := a.saveAskReceipt(agent, *receipt); err != nil {
			return err
		}
	}
	if event != "" {
		a.publishTurn(event, AskEvent{Ask: out.Ask.ID, Agent: agent, Kind: out.Ask.Kind, How: how, By: askBy(out.Ask)})
	}
	return nil
}
func askBy(ask Ask) string {
	if ask.Receipt != nil {
		return ask.Receipt.By
	}
	return ""
}
func askResolution(q session.Question, answer *session.Answer) (string, string, string) {
	by, how, text := "you", "answered", "Answered"
	if answer != nil {
		if answer.From != "" && answer.From != "app" {
			by = answer.From
		}
		if answer.DecidedBy != session.DecidedByPerson && answer.DecidedBy != session.DecidedByWindow {
			by = "codeaf"
		}
		if strings.HasPrefix(answer.Why, "rule:") {
			text = "Allowed · saved rule"
			how = "saved_rule"
		} else if strings.HasPrefix(answer.From, "rule:") {
			text = "Allowed automatically"
			how = "automatic"
		} else if q.Kind == session.QuestionConsent {
			if answer.Key == "3" {
				text = "Denied"
			} else if answer.Scope == session.ScopeAlways {
				text = "Allowed · saved rule"
			} else {
				text = "Allowed once"
			}
		} else if answer.Change != "" {
			text = "Answered · " + answer.Change
		} else if opt, ok := q.Option(answer.Key); ok {
			text = "Answered · " + opt.Label
		}
	}
	return text + " · " + q.Head, by, how
}
func (a *AgentRegistry) saveAskReceipt(agent string, entry DisplayEntry) error {
	err := a.disk(func(d *agentDisk) error {
		r, ok := d.Records[agent]
		if !ok {
			return errAgentMissing
		}
		for _, old := range r.Receipts {
			if old.Entry.ID == entry.ID {
				return nil
			}
		}
		r.Receipts = append(r.Receipts, turnSavedReceipt{Entry: entry, AtEntry: len(a.turnBaseEntries(r)) - len(r.Receipts)})
		r.Agent.Version++
		d.Records[agent] = r
		return nil
	}, true)
	if err == nil {
		a.publishTurn(EventTurnEntry, TurnEntryEvent{Agent: agent, Entry: entry})
	}
	return err
}

// Expiry stops work without supplying Allow, Deny or a suggested answer. A
// cancelled pending call is withdrawn by the engine's ordinary question lane.
func (a *AgentRegistry) pauseAsk(agent, id string, engine AgentEngine) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped || a.live[agent] == nil || a.live[agent].conn.Engine != engine {
		return
	}
	var receipt DisplayEntry
	expired := false
	_ = a.asks(true, func(d *askDisk) error {
		row, ok := d.Asks[id]
		if !ok || row.Held || row.Ask.State != "waiting" || row.Ask.Clock == nil || a.now().Before(*row.Ask.Clock.PausesAt) {
			return nil
		}
		row.Held = true
		row.Version++
		d.Asks[id] = row
		expired = true
		at := a.now().UTC()
		receipt = DisplayEntry{ID: "clock_" + id, Kind: "receipt", Role: "note", Text: "Paused · no answer before the question clock ran out", By: "codeaf", At: &at, Link: &SourceRef{Kind: "ask", ID: id}}
		return nil
	})
	if !expired {
		return
	}
	if err := engine.StopWork(); err != nil {
		return
	}
	_ = a.saveAskReceipt(agent, receipt)
}

// Refresh through the engine interface supports embedders without a standing
// subscription, and freezes the two context entries at the first observation.
func (a *AgentRegistry) findAskLocked(id string) (savedAsk, error) {
	var row savedAsk
	known := false
	if err := a.asks(false, func(d *askDisk) error { row, known = d.Asks[id]; return nil }); err != nil {
		return row, err
	}
	if known {
		return row, nil
	}
	agent := askAgent(id)
	live := a.live[agent]
	if live == nil {
		return row, agentError(404, "not_found", "no such ask")
	}
	door, ok := live.conn.Engine.(interface{ OpenQuestions() []session.Question })
	if !ok {
		return row, agentError(409, "not_allowed", "this engine cannot read asks")
	}
	for _, q := range door.OpenQuestions() {
		if askID(agent, q) == id {
			if err := a.observeAskLocked(agent, session.Event{Kind: session.EventQuestion, Question: &q}, &row); err != nil {
				return row, err
			}
			return row, nil
		}
	}
	return row, agentError(404, "not_found", fmt.Sprintf("no such ask %s", id))
}

// Task permissions retain the needs-you glyph without occupying the reply.
// The native reading also protects a reply started from a joined TUI window.
func taskPermissionsLeaveReplyFree(live *agentLive) bool {
	if live == nil || live.running {
		return false
	}
	questions, ok := live.conn.Engine.(interface{ OpenQuestions() []session.Question })
	if !ok {
		return false
	}
	hasTask := false
	for _, q := range questions.OpenQuestions() {
		if q.Blocking.Turn {
			return false
		}
		hasTask = hasTask || q.Kind == session.QuestionConsent && len(q.Blocking.Tasks) > 0
	}
	if !hasTask {
		return false
	}
	reply, ok := live.conn.Engine.(interface{ ReplyRunning() bool })
	return ok && !reply.ReplyRunning()
}
