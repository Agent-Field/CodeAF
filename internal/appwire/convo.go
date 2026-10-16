package appwire

import (
	"strings"
	"time"
	"unicode/utf8"
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
	// Media is what the engine measured of a picture it sent with the message
	// (media_submission.go); absent on every attachment sent as words.
	Media *AttachmentMedia `json:"media,omitempty"`
}

// MessageRead is one read of a reference the app performed to write a
// person's message — a collection it expanded at send time — as it found it
// at that moment. The engine keeps it on the message's line and reads it back
// on the message's entry; it never reads the reference again, and the model
// is sent none of it. Count is exactly how many references the read returned,
// Items the first few of them (descriptors, never contents); Missing,
// Unavailable and Unread count what it could not return and More says it was
// cut. Complete is the app's own answer, and may not be true while anything
// was unavailable, unread or cut. The counts and flags are always present.
type MessageRead struct {
	Source      MessageReference   `json:"source"`
	Count       int                `json:"count"`
	Missing     int                `json:"missing"`
	Unavailable int                `json:"unavailable"`
	Unread      int                `json:"unread"`
	More        bool               `json:"more"`
	Complete    bool               `json:"complete"`
	Warnings    []string           `json:"warnings,omitempty"`
	Items       []MessageReference `json:"items,omitempty"`
}

// MessageReference names what a read was of, or one thing it returned, in the
// app's own words. Kind is required, with at least one of id, name, path or
// url.
type MessageReference struct {
	Kind  string `json:"kind"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Owner string `json:"owner,omitempty"`
	Path  string `json:"path,omitempty"`
	URL   string `json:"url,omitempty"`
}

// AgentMessage is a person's message with what the app knows about it beyond
// the words the model reads: Said, when set, is the person's own words (empty
// for a message of attachments or reads alone), never replaced by Text; Reads
// are the reads Text was written from. Attachments are staged ids, as
// agents.send takes them.
type AgentMessage struct {
	Text        string
	Said        *string
	ClientID    string
	Attachments []string
	Reads       []MessageRead
	// Submission names which staged attachments are pictures and what was
	// marked on them (media_submission.go); only agents.send carries one.
	Submission *MessageSubmission
}

// MessageSender is the optional engine half of agents.send and crews.message
// carrying said or reads. One that is not a MessageSender answers such a send
// unknown_method rather than running it without what it carried; a send with
// neither goes by Send or SendWith exactly as before.
type MessageSender interface {
	SendMessage(agent string, message AgentMessage) (uint64, error)
}

// FeatureMessageReads is hello's word for an engine that is a [MessageSender]:
// agents.send and crews.message keep said and reads on the person's message.
const FeatureMessageReads = "messages.reads"

// MessageSteerer is the optional engine half of agents.steer carrying said or
// reads. One that is not answers such a correction unknown_method, before the
// turn hears anything; a correction with neither goes by Steer as before.
// Attachments is always empty: a correction carries none.
type MessageSteerer interface {
	SteerMessage(agent string, message AgentMessage) error
}

// MessageQueuer is the optional engine half of agents.queue and queue.edit
// carrying said or reads. The queue keeps the whole message — the words the
// model will read, said and reads — and shows the person's own words; one
// that is not a MessageQueuer answers such a call unknown_method with the
// queue as it was. Attachments is always empty: a queued message carries none.
type MessageQueuer interface {
	QueueMessage(agent string, message AgentMessage) ([]Queued, error)
	// EditQueuedMessage replaces one queued message whole, checking the
	// replacement before anything changes; an id already sent is CodeSent.
	EditQueuedMessage(agent, id string, message AgentMessage) ([]Queued, error)
}

// FeatureSteerReads and FeatureQueueReads are hello's words for an engine
// that is a [MessageSteerer] or a [MessageQueuer]. Each says only its own: an
// engine that keeps a sent message's reads need not keep a queued one's.
const (
	FeatureSteerReads = "agents.steer.reads"
	FeatureQueueReads = "agents.queue.reads"
)

// withMessageReadsFeature is hello carrying [FeatureMessageReads],
// [FeatureSteerReads] and [FeatureQueueReads] for each door the engine has.
func withMessageReadsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(MessageSender); ok {
		hello = withFeature(hello, FeatureMessageReads)
	}
	if _, ok := e.(MessageSteerer); ok {
		hello = withFeature(hello, FeatureSteerReads)
	}
	if _, ok := e.(MessageQueuer); ok {
		hello = withFeature(hello, FeatureQueueReads)
	}
	if _, ok := e.(ComposedSender); ok {
		hello = withFeature(hello, FeatureMessageMedia)
	}
	if _, ok := e.(MediaAttacher); ok {
		hello = withFeature(hello, FeatureMediaAttach)
	}
	if _, ok := e.(MediaFrameAttacher); ok {
		hello = withFeature(hello, FeatureMediaAttachFrame)
	}
	if _, ok := e.(MediaRangeAttacher); ok {
		hello = withFeature(hello, FeatureMediaAttachRange)
	}
	if _, ok := e.(MediaReferenceResolver); ok {
		hello = withFeature(hello, FeatureMediaResolve)
	}
	if _, ok := e.(CrewStarter); ok {
		if _, ok := e.(RootStarter); ok {
			hello = withFeature(hello, FeatureCrewStart)
		}
	}
	return hello
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

// AttachSource is one attachment named by what it is rather than by a staged
// id: agents.create carries these, because a conversation that does not exist
// yet has nowhere to stage a chip. A file names a path (relative to the
// workspace, or absolute) that must exist when it is accepted; a link its url;
// a paste its text. Nothing else is read from it.
type AttachSource struct {
	Kind string `json:"kind" enum:"file,link,paste"`
	Path string `json:"path,omitempty"`
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
}

// The bounds of what one message may carry. ONE MESSAGE IS ONE TURN'S
// CONTEXT, so the bounds hold the same whether the attachments were staged
// first (agents.send, crews.message) or named on the create itself.
const (
	// MaxAttachments is how many attachments one message may carry.
	MaxAttachments = 10
	// MaxPasteChars is the most characters one paste may hold.
	MaxPasteChars = 200000
	// MaxAttachChars is the most characters of paste one message may carry.
	MaxAttachChars = 400000
)

// CheckAttachSources is the shape of a create's attachments, checked before
// anything is made: a known kind with its own field, within the bounds. It
// reads no disk; whether a file is there is the engine's to say.
func CheckAttachSources(sources []AttachSource) error {
	if len(sources) > MaxAttachments {
		return Errorf(CodeBadParams, "a message carries at most %d attachments, not %d", MaxAttachments, len(sources))
	}
	total := 0
	for i, s := range sources {
		switch s.Kind {
		case AttachFile:
			if strings.TrimSpace(s.Path) == "" {
				return Errorf(CodeBadParams, "attachment %d is a file and needs a path", i+1)
			}
		case AttachLink:
			if strings.TrimSpace(s.URL) == "" {
				return Errorf(CodeBadParams, "attachment %d is a link and needs a url", i+1)
			}
		case AttachPaste:
			if strings.TrimSpace(s.Text) == "" {
				return Errorf(CodeBadParams, "attachment %d is a paste and needs its text", i+1)
			}
			n := utf8.RuneCountInString(s.Text)
			if n > MaxPasteChars {
				return Errorf(CodeBadParams, "attachment %d is a paste of %d characters; one paste holds at most %d", i+1, n, MaxPasteChars)
			}
			total += n
		default:
			return Errorf(CodeBadParams, "attachment %d: kind is file, link or paste", i+1)
		}
	}
	if total > MaxAttachChars {
		return Errorf(CodeBadParams, "the pastes come to %d characters; one message carries at most %d", total, MaxAttachChars)
	}
	return nil
}

// CheckAttachIDs is the shape of a send's staged attachment ids: within the
// count, none empty, and none named twice. A repeated id is refused rather
// than folded, so the context the model reads is never doubled and the app
// learns its list was wrong.
func CheckAttachIDs(ids []string) error {
	if len(ids) > MaxAttachments {
		return Errorf(CodeBadParams, "a message carries at most %d attachments, not %d", MaxAttachments, len(ids))
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return Errorf(CodeBadParams, "an attachment id is empty")
		}
		if seen[id] {
			return Errorf(CodeBadParams, "attachment %s is named twice", id)
		}
		seen[id] = true
	}
	return nil
}

// Queued is one message waiting for the turn to end. Text is the person's own
// words — said, when the message carried it — and never the body the model
// will read; Reads are the reads that body was written from.
type Queued struct {
	ID    string        `json:"id"`
	Text  string        `json:"text"`
	Reads []MessageRead `json:"reads,omitempty"`
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

// AttachmentRemover is the optional half of attachments: an engine that keeps
// staged attachments lets the app drop one that will not be sent. It is a
// separate interface so a Talker without staging needs no stub; one that is
// not an AttachmentRemover refuses attachments.remove as unknown.
type AttachmentRemover interface {
	// RemoveAttachment drops one staged attachment. It is idempotent: an id
	// that is missing or already sent is not an error, so cleanup after a
	// failed send is harmless. An unknown agent is still an error.
	RemoveAttachment(agent, id string) error
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
	ID          string        `json:"id"`
	Text        string        `json:"text"`
	Said        *string       `json:"said"`
	ClientID    string        `json:"client_id"`
	Attachments []string      `json:"attachments"`
	Reads       []MessageRead `json:"reads"`
	// Submission is refused: a correction or a queued message carries no pictures.
	Submission *MessageSubmission `json:"submission,omitempty"`
}

func checkSay(p sayTo, name string) (sayTo, error) {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Text) == "" {
		return p, Errorf(CodeBadParams, "%s needs an id and some text", name)
	}
	if err := refuseSubmission(p.Submission, name); err != nil {
		return p, err
	}
	return p, refuseAttachments(p.Attachments, name)
}

// refuseAttachments is a correction or a queued message carrying
// attachments, refused rather than run without them: neither road keeps one.
func refuseAttachments(ids []string, name string) error {
	if len(ids) > 0 {
		return Errorf(CodeBadParams, "%s does not carry attachments; send them with agents.send", name)
	}
	return nil
}

// marked is whether a message carries anything beyond its words.
func marked(said *string, reads []MessageRead) bool {
	return said != nil || len(reads) > 0
}

// queueAt is the queue methods' params.
type queueAt struct {
	Agent       string        `json:"agent"`
	ID          string        `json:"id"`
	Text        string        `json:"text"`
	Said        *string       `json:"said"`
	Attachments []string      `json:"attachments"`
	Reads       []MessageRead `json:"reads"`
	To          *int          `json:"to"`
	// Submission is refused: a queued message carries no pictures.
	Submission *MessageSubmission `json:"submission,omitempty"`
}

// queuer is the engine's door for a queued message's said and reads, or a
// refusal that leaves the queue as it was.
func queuer(e Engine, name string) (MessageQueuer, error) {
	q, ok := e.(MessageQueuer)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s with said or reads", name)
	}
	return q, nil
}

func checkQueueAt(p queueAt, name string) (queueAt, error) {
	if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
		return p, Errorf(CodeBadParams, "%s needs an agent and an id", name)
	}
	return p, refuseSubmission(p.Submission, name)
}

// talkTask is one task verb that takes only an id, served by the Worker.
func talkTaskVerb(action string) method {
	return typed(func(e Engine, p IDParams) (*Empty, error) {
		w, err := worker(e, "tasks."+action)
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		return done(w.ActOnTask(id, action, "", ""))
	})
}

var talkMethods = map[string]method{
	"agents.steer": typed(func(e Engine, p sayTo) (*Empty, error) {
		t, err := talker(e, "agents.steer")
		if err != nil {
			return nil, err
		}
		p, err = checkSay(p, "agents.steer")
		if err != nil {
			return nil, err
		}
		if marked(p.Said, p.Reads) {
			s, ok := e.(MessageSteerer)
			if !ok {
				return nil, Errorf(CodeUnknownMethod, "this engine does not serve agents.steer with said or reads")
			}
			return done(s.SteerMessage(p.ID, AgentMessage{Text: p.Text, Said: p.Said, ClientID: p.ClientID, Reads: p.Reads}))
		}
		return done(t.Steer(p.ID, p.Text, p.ClientID))
	}),
	"agents.queue": typed(func(e Engine, p sayTo) (*queueResult, error) {
		t, err := talker(e, "agents.queue")
		if err != nil {
			return nil, err
		}
		p, err = checkSay(p, "agents.queue")
		if err != nil {
			return nil, err
		}
		var queued []Queued
		if marked(p.Said, p.Reads) {
			q, qerr := queuer(e, "agents.queue")
			if qerr != nil {
				return nil, qerr
			}
			queued, err = q.QueueMessage(p.ID, AgentMessage{Text: p.Text, Said: p.Said, ClientID: p.ClientID, Reads: p.Reads})
		} else {
			queued, err = t.Queue(p.ID, p.Text, p.ClientID)
		}
		return &queueResult{nonNil(queued)}, err
	}),
	"queue.edit": typed(func(e Engine, p queueAt) (*queueResult, error) {
		t, err := talker(e, "queue.edit")
		if err != nil {
			return nil, err
		}
		p, err = checkQueueAt(p, "queue.edit")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Text) == "" {
			return nil, Errorf(CodeBadParams, "queue.edit needs the new text; queue.remove takes a message out")
		}
		if err := refuseAttachments(p.Attachments, "queue.edit"); err != nil {
			return nil, err
		}
		// A PLAIN EDIT REPLACES THE WHOLE MESSAGE with plain words, the old
		// said and reads included; only an edit that carries them keeps any.
		var queued []Queued
		if marked(p.Said, p.Reads) {
			q, qerr := queuer(e, "queue.edit")
			if qerr != nil {
				return nil, qerr
			}
			queued, err = q.EditQueuedMessage(p.Agent, p.ID, AgentMessage{Text: p.Text, Said: p.Said, Reads: p.Reads})
		} else {
			queued, err = t.EditQueued(p.Agent, p.ID, p.Text)
		}
		return &queueResult{nonNil(queued)}, err
	}),
	"queue.remove": typed(func(e Engine, p queueAt) (*queueResult, error) {
		t, err := talker(e, "queue.remove")
		if err != nil {
			return nil, err
		}
		p, err = checkQueueAt(p, "queue.remove")
		if err != nil {
			return nil, err
		}
		queued, err := t.RemoveQueued(p.Agent, p.ID)
		return &queueResult{nonNil(queued)}, err
	}),
	"queue.move": typed(func(e Engine, p queueAt) (*queueResult, error) {
		t, err := talker(e, "queue.move")
		if err != nil {
			return nil, err
		}
		p, err = checkQueueAt(p, "queue.move")
		if err != nil {
			return nil, err
		}
		if p.To == nil || *p.To < 0 {
			return nil, Errorf(CodeBadParams, "queue.move needs to, a place counted from zero")
		}
		queued, err := t.MoveQueued(p.Agent, p.ID, *p.To)
		return &queueResult{nonNil(queued)}, err
	}),
	"asks.hold": typed(func(e Engine, p struct {
		Agent string `json:"agent"`
		ID    string `json:"id"`
	}) (*Empty, error) {
		t, err := talker(e, "asks.hold")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "asks.hold needs an agent and an id")
		}
		return done(t.HoldAsk(p.Agent, p.ID))
	}),
	"files.find": typed(func(e Engine, p struct {
		Agent string `json:"agent"`
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}) (*GroupsResult, error) {
		t, err := talker(e, "files.find")
		if err != nil {
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
		return &GroupsResult{nonNil(groups)}, nil
	}),
	"files.stat": typed(func(e Engine, p struct {
		Agent string   `json:"agent"`
		Paths []string `json:"paths"`
	}) (*FilesResult, error) {
		t, err := talker(e, "files.stat")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || len(p.Paths) == 0 {
			return nil, Errorf(CodeBadParams, "files.stat needs an agent and some paths")
		}
		files, err := t.StatFiles(p.Agent, p.Paths)
		return &FilesResult{nonNil(files)}, err
	}),
	"attachments.add": typed(func(e Engine, p struct {
		Agent string `json:"agent"`
		Kind  string `json:"kind"`
		Path  string `json:"path"`
		Text  string `json:"text"`
		URL   string `json:"url"`
	}) (*AttachmentResult, error) {
		t, err := talker(e, "attachments.add")
		if err != nil {
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
		case p.Kind == AttachPaste && utf8.RuneCountInString(p.Text) > MaxPasteChars:
			return nil, Errorf(CodeBadParams, "one paste holds at most %d characters", MaxPasteChars)
		}
		attachment, err := t.AddAttachment(p.Agent, Attachment{Kind: p.Kind, Path: p.Path, URL: p.URL}, p.Text)
		if err != nil {
			return nil, err
		}
		return &AttachmentResult{attachment}, nil
	}),
	"attachments.remove": typed(func(e Engine, p struct {
		Agent string `json:"agent"`
		ID    string `json:"id"`
	}) (*Empty, error) {
		r, ok := e.(AttachmentRemover)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve attachments.remove")
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "attachments.remove needs an agent and an id")
		}
		return done(r.RemoveAttachment(p.Agent, p.ID))
	}),
	"tasks.pause":  talkTaskVerb("pause"),
	"tasks.resume": talkTaskVerb("resume"),
	"tasks.retry": typed(func(e Engine, p IDParams) (*Empty, error) {
		t, err := talker(e, "tasks.retry")
		if err != nil {
			return nil, err
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		return done(t.RetryTask(id))
	}),
	"tasks.brief": typed(func(e Engine, p struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}) (*Empty, error) {
		w, err := worker(e, "tasks.brief")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Text) == "" {
			return nil, Errorf(CodeBadParams, "tasks.brief needs an id and the new brief")
		}
		return done(w.ActOnTask(p.ID, "brief", "", p.Text))
	}),
	"tasks.entries": typed(func(e Engine, p struct {
		ID     string `json:"id"`
		Before string `json:"before"`
		Limit  int    `json:"limit"`
	}) (*EntriesMoreResult, error) {
		t, err := talker(e, "tasks.entries")
		if err != nil {
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
		return &EntriesMoreResult{nonNil(entries), more}, nil
	}),
}

// ThinkingSeconds is how long a thought lasted, in whole seconds, at least one.
func ThinkingSeconds(d time.Duration) int {
	return max(1, int(d.Round(time.Second)/time.Second))
}

// The answers of this file's methods.
type (
	GroupsResult struct {
		Groups []FileGroup `json:"groups"`
	}
	AttachmentResult struct {
		Attachment Attachment `json:"attachment"`
	}
	EntriesMoreResult struct {
		Entries []Entry `json:"entries"`
		More    bool    `json:"more"`
	}
)

// The answers of this file's methods.
type (
	FilesResult struct {
		Files []FileStat `json:"files"`
	}
)
