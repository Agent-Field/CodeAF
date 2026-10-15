package appwire

import (
	"encoding/json"
	"strings"
	"time"
)

// Wave 3b (PROTOCOL.md, V65–V70): the conversation's own entries and events,
// the decision dock's ask facts, steering and the queue, files and links in
// the composer, and a task as a conversation.

// The events of this wave.
const (
	EventTurnThinking     = "turn.thinking"
	EventTurnThinkingDone = "turn.thinking_done"
	EventTurnRetrying     = "turn.retrying"
	EventQueueChanged     = "queue.changed"
	EventNoteChanged      = "note.changed"
)

// A steer's states, and a note's.
const (
	SteerPending     = "pending"
	SteerConsumed    = "consumed"
	SteerFellThrough = "fell_through"

	NoteSending = "sending"
	NoteWaiting = "waiting"
	NoteRead    = "read"
)

// The kinds of footer.
const (
	FooterStoppedByYou    = "stopped_by_you"
	FooterRetrying        = "retrying"
	FooterProviderFailed  = "provider_failed"
	FooterFallback        = "fallback"
	FooterProcessFinished = "process_finished"
	FooterProcessLimit    = "process_limit"
	// FooterResumedAfterQuit is in front of a reply that carries on after the
	// app was quit in the middle of it.
	FooterResumedAfterQuit = "resumed_after_quit"
)

// CodeSent refuses an edit to a queued message that has already been sent.
const CodeSent = "sent"

// AskSource is where an ask comes from: this reply, or one of its tasks.
type AskSource struct {
	Kind string `json:"kind"`
	Task string `json:"task,omitempty"`
}

// Evidence is the first lines of what an ask shows, and how many more there are.
type Evidence struct {
	Lines []string `json:"lines"`
	More  int      `json:"more"`
}

// AskChoice is one answer of a choice, with the asker's suggestion marked.
type AskChoice struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Detail    string `json:"detail,omitempty"`
	Suggested bool   `json:"suggested,omitempty"`
}

// BatchItem is one call of several asked about together.
type BatchItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	State string `json:"state,omitempty"`
}

// Attachment is a file, a link or a long paste that rides on a message.
type Attachment struct {
	ID    string `json:"id,omitempty"`
	Kind  string `json:"kind"`
	Path  string `json:"path,omitempty"`
	URL   string `json:"url,omitempty"`
	Title string `json:"title,omitempty"`
	Lines *int   `json:"lines,omitempty"`
	Add   *int   `json:"add,omitempty"`
	Del   *int   `json:"del,omitempty"`
	State string `json:"state,omitempty"`
}

// The attachment kinds and a file's states.
const (
	AttachFile  = "file"
	AttachLink  = "link"
	AttachPaste = "paste"

	FileExists  = "exists"
	FileChanged = "changed"
	FileMissing = "missing"
	FileOutside = "outside"
)

// Queued is one message waiting for the turn to end.
type Queued struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// FileGroup is one group of files.find's answer.
type FileGroup struct {
	Label string      `json:"label"`
	Files []FoundFile `json:"files"`
}

// FoundFile is one file or folder a reference may name.
type FoundFile struct {
	Path string `json:"path"`
	Add  *int   `json:"add,omitempty"`
	Del  *int   `json:"del,omitempty"`
	Dir  bool   `json:"dir,omitempty"`
}

// FileStat is one path as files.stat finds it.
type FileStat struct {
	Path   string `json:"path"`
	State  string `json:"state"`
	Add    *int   `json:"add,omitempty"`
	Del    *int   `json:"del,omitempty"`
	Size   *int64 `json:"size,omitempty"`
	Binary bool   `json:"binary,omitempty"`
}

// AskAnswer is the whole of an asks.answer: the option, the words, a reason,
// which call of a batch, or putting it aside or handing it back.
type AskAnswer struct {
	Option    string
	Text      string
	Why       string
	BatchItem string
	Later     bool
	YouDecide bool
}

// The data of this wave's events.
type (
	TurnThinking struct {
		Agent string `json:"agent"`
		Turn  uint64 `json:"turn"`
		Text  string `json:"text"`
	}
	TurnThinkingDone struct {
		Agent   string `json:"agent"`
		Turn    uint64 `json:"turn"`
		Seconds int    `json:"seconds"`
	}
	TurnRetrying struct {
		Agent   string `json:"agent"`
		Turn    uint64 `json:"turn"`
		InS     *int   `json:"in_s,omitempty"`
		Attempt int    `json:"attempt"`
		// Reason is empty for a failed request being asked again, paced for a
		// rate limit, and offline while the network is being waited for.
		Reason string `json:"reason,omitempty"`
	}
	QueueChanged struct {
		Agent  string   `json:"agent"`
		Queued []Queued `json:"queued"`
	}
	NoteChanged struct {
		Task       string `json:"task"`
		ID         string `json:"id"`
		State      string `json:"state"`
		ReadAtStep *int   `json:"read_at_step,omitempty"`
	}
)

// Talker is the engine's half of this wave. An [Engine] that is also a
// Talker serves these methods; one that is not refuses them as unknown.
type Talker interface {
	Steer(agent, text, clientID string) error
	Queue(agent, text, clientID string) ([]Queued, error)
	EditQueued(agent, id, text string) ([]Queued, error)
	RemoveQueued(agent, id string) ([]Queued, error)
	MoveQueued(agent, id string, to int) ([]Queued, error)
	AnswerAskWith(agent, id string, answer AskAnswer) error
	HoldAsk(agent, id string) error
	FindFiles(agent, query string, limit int) ([]FileGroup, error)
	StatFiles(agent string, paths []string) ([]FileStat, error)
	AddAttachment(agent string, attachment Attachment, text string) (Attachment, error)
	SendWith(agent, text, clientID string, attachments []string) (uint64, error)
	RetryTask(id string) error
	NoteTask(id, text, clientID string) (Entry, error)
	TaskEntries(id, before string, limit int) ([]Entry, bool, error)
}

func init() {
	for name, m := range talkMethods {
		methods[name] = m
	}
}

// talker is the engine as a Talker, or a refusal.
func talker(e Engine, name string) (Talker, error) {
	t, ok := e.(Talker)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return t, nil
}

// queueResult is every queue method's answer.
type queueResult struct {
	Queued []Queued `json:"queued"`
}

// sayTo is agents.steer and agents.queue's params.
type sayTo struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	ClientID string `json:"client_id"`
}

func decodeSay(raw json.RawMessage, name string) (sayTo, error) {
	var p sayTo
	if err := decode(raw, &p); err != nil {
		return p, err
	}
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Text) == "" {
		return p, Errorf(CodeBadParams, "%s needs an id and some text", name)
	}
	return p, nil
}

// queueAt is the queue methods' params.
type queueAt struct {
	Agent string `json:"agent"`
	ID    string `json:"id"`
	Text  string `json:"text"`
	To    *int   `json:"to"`
}

func decodeQueueAt(raw json.RawMessage, name string) (queueAt, error) {
	var p queueAt
	if err := decode(raw, &p); err != nil {
		return p, err
	}
	if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
		return p, Errorf(CodeBadParams, "%s needs an agent and an id", name)
	}
	return p, nil
}

// talkTask is one task verb that takes only an id, served by the Worker.
func talkTaskVerb(action string) method {
	return func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "tasks."+action)
		if err != nil {
			return nil, err
		}
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		return struct{}{}, w.ActOnTask(id, action, "", "")
	}
}

var talkMethods = map[string]method{
	"agents.steer": func(e Engine, raw json.RawMessage) (any, error) {
		t, err := talker(e, "agents.steer")
		if err != nil {
			return nil, err
		}
		p, err := decodeSay(raw, "agents.steer")
		if err != nil {
			return nil, err
		}
		return struct{}{}, t.Steer(p.ID, p.Text, p.ClientID)
	},
	"agents.queue": func(e Engine, raw json.RawMessage) (any, error) {
		t, err := talker(e, "agents.queue")
		if err != nil {
			return nil, err
		}
		p, err := decodeSay(raw, "agents.queue")
		if err != nil {
			return nil, err
		}
		queued, err := t.Queue(p.ID, p.Text, p.ClientID)
		return queueResult{nonNil(queued)}, err
	},
	"queue.edit": func(e Engine, raw json.RawMessage) (any, error) {
		t, err := talker(e, "queue.edit")
		if err != nil {
			return nil, err
		}
		p, err := decodeQueueAt(raw, "queue.edit")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Text) == "" {
			return nil, Errorf(CodeBadParams, "queue.edit needs the new text; queue.remove takes a message out")
		}
		queued, err := t.EditQueued(p.Agent, p.ID, p.Text)
		return queueResult{nonNil(queued)}, err
	},
	"queue.remove": func(e Engine, raw json.RawMessage) (any, error) {
		t, err := talker(e, "queue.remove")
		if err != nil {
			return nil, err
		}
		p, err := decodeQueueAt(raw, "queue.remove")
		if err != nil {
			return nil, err
		}
		queued, err := t.RemoveQueued(p.Agent, p.ID)
		return queueResult{nonNil(queued)}, err
	},
	"queue.move": func(e Engine, raw json.RawMessage) (any, error) {
		t, err := talker(e, "queue.move")
		if err != nil {
			return nil, err
		}
		p, err := decodeQueueAt(raw, "queue.move")
		if err != nil {
			return nil, err
		}
		if p.To == nil || *p.To < 0 {
			return nil, Errorf(CodeBadParams, "queue.move needs to, a place counted from zero")
		}
		queued, err := t.MoveQueued(p.Agent, p.ID, *p.To)
		return queueResult{nonNil(queued)}, err
	},
	"asks.hold": func(e Engine, raw json.RawMessage) (any, error) {
		t, err := talker(e, "asks.hold")
		if err != nil {
			return nil, err
		}
		var p struct {
			Agent string `json:"agent"`
			ID    string `json:"id"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "asks.hold needs an agent and an id")
		}
		return struct{}{}, t.HoldAsk(p.Agent, p.ID)
	},
	"files.find": func(e Engine, raw json.RawMessage) (any, error) {
		t, err := talker(e, "files.find")
		if err != nil {
			return nil, err
		}
		var p struct {
			Agent string `json:"agent"`
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || p.Limit < 0 {
			return nil, Errorf(CodeBadParams, "files.find needs an agent and a limit of zero or more")
		}
		groups, err := t.FindFiles(p.Agent, p.Query, p.Limit)
		if err != nil {
			return nil, err
		}
		for i := range groups {
			groups[i].Files = nonNil(groups[i].Files)
		}
		return struct {
			Groups []FileGroup `json:"groups"`
		}{nonNil(groups)}, nil
	},
	"files.stat": func(e Engine, raw json.RawMessage) (any, error) {
		t, err := talker(e, "files.stat")
		if err != nil {
			return nil, err
		}
		var p struct {
			Agent string   `json:"agent"`
			Paths []string `json:"paths"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || len(p.Paths) == 0 {
			return nil, Errorf(CodeBadParams, "files.stat needs an agent and some paths")
		}
		files, err := t.StatFiles(p.Agent, p.Paths)
		return struct {
			Files []FileStat `json:"files"`
		}{nonNil(files)}, err
	},
	"attachments.add": func(e Engine, raw json.RawMessage) (any, error) {
		t, err := talker(e, "attachments.add")
		if err != nil {
			return nil, err
		}
		var p struct {
			Agent string `json:"agent"`
			Kind  string `json:"kind"`
			Path  string `json:"path"`
			Text  string `json:"text"`
			URL   string `json:"url"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		switch {
		case strings.TrimSpace(p.Agent) == "":
			return nil, Errorf(CodeBadParams, "attachments.add needs an agent")
		case p.Kind == AttachFile && strings.TrimSpace(p.Path) == "",
			p.Kind == AttachLink && strings.TrimSpace(p.URL) == "",
			p.Kind == AttachPaste && p.Text == "":
			return nil, Errorf(CodeBadParams, "a file needs a path, a link a url, and a paste its text")
		case p.Kind != AttachFile && p.Kind != AttachLink && p.Kind != AttachPaste:
			return nil, Errorf(CodeBadParams, "kind is file, link or paste")
		}
		attachment, err := t.AddAttachment(p.Agent, Attachment{Kind: p.Kind, Path: p.Path, URL: p.URL}, p.Text)
		if err != nil {
			return nil, err
		}
		return struct {
			Attachment Attachment `json:"attachment"`
		}{attachment}, nil
	},
	"tasks.pause":  talkTaskVerb("pause"),
	"tasks.resume": talkTaskVerb("resume"),
	"tasks.retry": func(e Engine, raw json.RawMessage) (any, error) {
		t, err := talker(e, "tasks.retry")
		if err != nil {
			return nil, err
		}
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		return struct{}{}, t.RetryTask(id)
	},
	"tasks.brief": func(e Engine, raw json.RawMessage) (any, error) {
		w, err := worker(e, "tasks.brief")
		if err != nil {
			return nil, err
		}
		var p struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Text) == "" {
			return nil, Errorf(CodeBadParams, "tasks.brief needs an id and the new brief")
		}
		return struct{}{}, w.ActOnTask(p.ID, "brief", "", p.Text)
	},
	"tasks.entries": func(e Engine, raw json.RawMessage) (any, error) {
		t, err := talker(e, "tasks.entries")
		if err != nil {
			return nil, err
		}
		var p struct {
			ID     string `json:"id"`
			Before string `json:"before"`
			Limit  int    `json:"limit"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" || p.Limit < 0 {
			return nil, Errorf(CodeBadParams, "tasks.entries needs an id")
		}
		if p.Limit == 0 {
			p.Limit = 200
		}
		entries, more, err := t.TaskEntries(p.ID, p.Before, p.Limit)
		if err != nil {
			return nil, err
		}
		return struct {
			Entries []Entry `json:"entries"`
			More    bool    `json:"more"`
		}{nonNil(entries), more}, nil
	},
}

// ThinkingSeconds is how long a thought lasted, in whole seconds, at least one.
func ThinkingSeconds(d time.Duration) int {
	return max(1, int(d.Round(time.Second)/time.Second))
}
