package appwire

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/session"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/effort"
)

// writerQueue is how many frames may wait for stdout before a sender waits.
// It bounds the memory a stalled reader can make this process hold, and it is
// never what the engine's own read loops wait on: those hand their events to
// an [Outbox], whose sender is the one that waits here.
const writerQueue = 1024

// MaxEntries is the most transcript entries one agents.entries answers, and
// what a request that names no limit gets.
const MaxEntries = 200

// handlerGrace is how long a leaving process waits for calls still in flight
// to answer after the engine has been closed under them. A call that has not
// answered by then is abandoned; the process is exiting either way.
const handlerGrace = 2 * time.Second

// Server is one stdio connection to the app: the one writer, the sequence
// counter, and the dispatch table.
type Server struct {
	frames chan frame

	// mu guards closed against a late Emit. A sender holds the read lock while
	// it queues, and the leaving process takes the write lock before it closes
	// the queue, so no frame is ever sent on a closed channel.
	mu     sync.RWMutex
	closed bool

	wrote chan struct{}
	err   error
}

// NewServer starts the writer over out. Every line this process writes to the
// app goes through it, so the sequence numbers are assigned in exactly the
// order the lines leave.
func NewServer(out io.Writer) *Server {
	s := &Server{frames: make(chan frame, writerQueue), wrote: make(chan struct{})}
	go s.write(out)
	return s
}

// write is the one writer. It encodes each frame once, writes it to a
// buffered stdout, and flushes when nothing else is waiting: a burst of frames
// is one system call, and a lone frame leaves at once.
func (s *Server) write(out io.Writer) {
	defer close(s.wrote)
	buffered := bufio.NewWriterSize(out, 64<<10)
	var seq uint64
	put := func(f frame) {
		if s.err != nil {
			return
		}
		number := uint64(0)
		if f.event != "" {
			seq++
			number = seq
		}
		line, err := f.encode(number)
		if err != nil {
			// A value that cannot be encoded is a bug in this binary, and the
			// app is still owed an answer to the request that produced it.
			if f.event != "" {
				return
			}
			line, _ = frame{id: f.id, err: Errorf(CodeEngine, "the answer could not be encoded: %v", err)}.encode(0)
		}
		line = append(line, '\n')
		if _, err := buffered.Write(line); err != nil {
			s.err = err
		}
	}
	for f := range s.frames {
		put(f)
	drain:
		for {
			select {
			case next, ok := <-s.frames:
				if !ok {
					break drain
				}
				put(next)
			default:
				break drain
			}
		}
		if s.err == nil {
			if err := buffered.Flush(); err != nil {
				s.err = err
			}
		}
	}
	if s.err == nil {
		_ = buffered.Flush()
	}
}

// Emit queues one event. It is safe from any goroutine, and after the server
// has closed it drops the event: the app has stopped reading.
func (s *Server) Emit(event string, data any) {
	s.send(frame{event: event, data: data})
}

func (s *Server) send(f frame) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return
	}
	s.frames <- f
}

// finish closes the queue and waits for every queued line to be written.
func (s *Server) finish() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.frames)
	}
	s.mu.Unlock()
	<-s.wrote
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Serve is the whole lifetime of the connection: the hello line first, then
// requests until stdin ends, the context is cancelled, or the app asks for
// shutdown. Every way out closes the engine and writes every queued line
// before it returns.
func (s *Server) Serve(ctx context.Context, in io.Reader, hello Hello, engine Engine) error {
	hello.Contract = Contract
	hello = withFeature(hello, FeatureAgentEffortRungs)
	hello = withCollectionsFeature(hello, engine)
	hello = withMessageReadsFeature(hello, engine)
	hello = withWorkGraphFeature(hello, engine)
	hello = withSlashCatalogFeature(hello, engine)
	hello = withSyncFeatures(hello, engine)
	hello = withSkillsFeatures(hello, engine)
	hello = withMachinePolicyFeatures(hello, engine)
	hello = withSessionActionsFeature(hello, engine)
	hello = withRunHarnessFeature(hello, engine)
	hello = withConversationFeatures(hello, engine)
	hello = withCrewsOverviewFeature(hello, engine)
	hello = withCrewsOrganizeFeature(hello, engine)
	hello = withReplySuggestionFeature(hello, engine)
	hello = withProcessFeatures(hello, engine)
	hello = withProcessActionFeature(hello, engine)
	hello = withMemoryAppliesFeature(hello, engine)
	hello = withFactoryFeature(hello, engine)
	hello = withFactoryActorFeature(hello, engine)
	hello = withFactoryJobStepEditFeature(hello, engine)
	hello = withFactoryJobRejectPlanFeature(hello, engine)
	hello = withFactoryJobChatsFeature(hello, engine)
	hello = withFactoryRenameFeature(hello, engine)
	hello = withFactoryRefreshFeature(hello, engine)
	hello = withFactoryLocalFeature(hello, engine)
	hello = withOrdersMadeFeature(hello, engine)
	hello = withFactoryAdminFeature(hello, engine)
	hello = withFactoryLinksFeature(hello, engine)
	hello = withFactoryReposManageFeature(hello, engine)
	hello = withFactoryRunnerManageFeature(hello, engine)
	hello = withFactoryPipelinePRFeature(hello, engine)
	hello = withFactoryPipelineEditFeature(hello, engine)
	s.Emit(EventHello, hello)
	if warmer, ok := engine.(Warmer); ok {
		go warmer.Warm()
	}

	lines := make(chan []byte)
	readErr := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		reader := bufio.NewReaderSize(in, 64<<10)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				select {
				case lines <- line:
				case <-stop:
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					err = nil
				}
				readErr <- err
				return
			}
		}
	}()

	calls, cancel := context.WithCancel(ctx)
	var inflight sync.WaitGroup
	var result error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case result = <-readErr:
			break loop
		case line := <-lines:
			if s.handle(calls, line, engine, &inflight) {
				break loop
			}
		}
	}
	close(stop)
	cancel()
	_ = engine.Close()
	waited := make(chan struct{})
	go func() { inflight.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(handlerGrace):
	}
	s.finish()
	if result == nil {
		result = s.err
	}
	return result
}

// handle answers one line, and reports whether it was the request to leave.
func (s *Server) handle(ctx context.Context, line []byte, engine Engine, inflight *sync.WaitGroup) bool {
	text := strings.TrimSpace(string(line))
	if text == "" {
		return false
	}
	var req request
	if err := json.Unmarshal([]byte(text), &req); err != nil {
		s.Emit(EventLog, Log{Level: "warn", Message: "a line that is not a JSON object was ignored"})
		return false
	}
	var id uint64
	if len(req.ID) == 0 || json.Unmarshal(req.ID, &id) != nil {
		s.Emit(EventLog, Log{Level: "warn", Message: "a request without a numeric id was ignored"})
		return false
	}
	if req.Method == "shutdown" {
		s.send(frame{id: id, result: struct{}{}})
		return true
	}
	method, ok := methods[req.Method]
	if !ok {
		s.send(frame{id: id, err: Errorf(CodeUnknownMethod, "there is no method named %q", req.Method)})
		return false
	}
	params := req.Params
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage("{}")
	}
	if strings.TrimSpace(string(params))[0] != '{' {
		s.send(frame{id: id, err: Errorf(CodeBadParams, "params must be an object")})
		return false
	}
	inflight.Add(1)
	go func() {
		defer inflight.Done()
		result, err := method.call(engine, params)
		if ctx.Err() != nil && err != nil {
			err = Errorf(CodeClosed, "codeaf is shutting down")
		}
		if err != nil {
			var wire *Error
			if !errors.As(err, &wire) {
				wire = &Error{Code: CodeEngine, Message: err.Error()}
			}
			s.send(frame{id: id, err: wire})
			return
		}
		s.send(frame{id: id, result: result})
	}()
	return false
}

// methods is the dispatch table. shutdown is not in it: it is answered by the
// loop itself, because it ends the loop.
var methods = map[string]method{
	"ping": typed(func(Engine, struct{}) (*Empty, error) { return &Empty{}, nil }),
	"agents.list": typed(func(e Engine, p AgentsListParams) (*AgentsResult, error) {
		// A STATE AND A QUERY are answered by an engine that keeps them.
		if t, ok := e.(Tidier); ok {
			switch p.State {
			case "", ListOpen, ListClosed, ListArchived, ListAll:
			default:
				return nil, Errorf(CodeBadParams, "state is open, closed, archived or all")
			}
			agents, err := t.ListAgentsIn(p.Workspace, p.State, p.Query)
			if err != nil {
				return nil, err
			}
			return &AgentsResult{nonNil(agents)}, nil
		}
		agents, err := e.ListAgents(p.Workspace)
		if err != nil {
			return nil, err
		}
		return &AgentsResult{nonNil(agents)}, nil
	}),
	"agents.create": typed(func(e Engine, p CreateAsk) (*agentResult, error) {
		if p.BudgetUSD != nil && *p.BudgetUSD < 0 {
			return nil, Errorf(CodeBadParams, "budget_usd cannot be negative")
		}
		if err := CheckAttachSources(p.Attachments); err != nil {
			return nil, err
		}
		agent, err := e.CreateAgent(p)
		if err != nil {
			return nil, err
		}
		return &agentResult{agent}, nil
	}),
	"agents.entries": typed(func(e Engine, p EntriesParams) (*EntriesResult, error) {
		if strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "agents.entries needs an id")
		}
		if p.Limit < 0 {
			return nil, Errorf(CodeBadParams, "limit cannot be negative")
		}
		if p.Limit == 0 || p.Limit > MaxEntries {
			p.Limit = MaxEntries
		}
		entries, more, err := e.Entries(p.ID, p.Before, p.Limit)
		if err != nil {
			return nil, err
		}
		return &EntriesResult{nonNil(entries), more}, nil
	}),
	"agents.set_models": typed(func(e Engine, p struct {
		ID       string `json:"id"`
		Main     string `json:"main"`
		Fallback string `json:"fallback"`
	}) (*agentResult, error) {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Main) == "" {
			return nil, Errorf(CodeBadParams, "choose a conversation and main model")
		}
		door, ok := e.(interface {
			SetConversationModels(string, session.ConversationModels) (Agent, error)
		})
		if !ok {
			return nil, Errorf(CodeBadParams, "main/fallback selection requires a newer engine")
		}
		agent, err := door.SetConversationModels(p.ID, session.ConversationModels{Main: p.Main, Fallback: p.Fallback})
		if err != nil {
			return nil, err
		}
		return &agentResult{agent}, nil
	}),
	"agents.set_model": typed(func(e Engine, p struct {
		Automatic *bool  `json:"automatic,omitempty"`
		ID        string `json:"id"`
		Model     string `json:"model"`
	}) (*agentResult, error) {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Model) == "" {
			return nil, Errorf(CodeBadParams, "agents.set_model needs an id and a model")
		}
		if p.Automatic != nil {
			door, ok := e.(interface{ SetAutomaticModels(string, bool) error })
			if !ok {
				return nil, Errorf(CodeBadParams, "automatic model selection is unsupported")
			}
			if err := door.SetAutomaticModels(p.ID, *p.Automatic); err != nil {
				return nil, err
			}
		}
		agent, err := e.SetModel(p.ID, strings.TrimSpace(p.Model))
		if err != nil {
			return nil, err
		}
		return &agentResult{agent}, nil
	}),
	"agents.set_effort": typed(func(e Engine, p struct {
		ID     string `json:"id"`
		Effort string `json:"effort" enum:"auto,low,medium,high,xhigh,max"`
	}) (*agentResult, error) {
		rung, ok := effort.Parse(p.Effort)
		if strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "agents.set_effort needs an id")
		}
		if !ok {
			return nil, Errorf(CodeBadParams, "effort is low, medium or high")
		}
		agent, err := e.SetEffort(p.ID, rung.String())
		if err != nil {
			return nil, err
		}
		return &agentResult{agent}, nil
	}),
	"agents.retry": typed(func(e Engine, p IDParams) (*TurnResult, error) {
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		turn, err := e.Retry(id)
		if err != nil {
			return nil, err
		}
		return &TurnResult{Turn: turn}, nil
	}),
	"models.list": typed(func(e Engine, _ struct{}) (*ModelsResult, error) {
		models, err := e.ListModels()
		if err != nil {
			return nil, err
		}
		return &ModelsResult{nonNil(models)}, nil
	}),
	"settings.get": typed(func(e Engine, _ struct{}) (*SettingsResult, error) {
		settings, err := e.Settings()
		if err != nil {
			return nil, err
		}
		return &SettingsResult{settings}, nil
	}),
	"spend.today": typed(func(e Engine, _ struct{}) (*SpendResult, error) {
		spend, err := e.SpendToday()
		if err != nil {
			return nil, err
		}
		return &SpendResult{spend}, nil
	}),
	"agents.open": typed(func(e Engine, p IDParams) (*agentResult, error) {
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		agent, err := e.OpenAgent(id)
		if err != nil {
			return nil, err
		}
		return &agentResult{agent}, nil
	}),
	"agents.send": typed(func(e Engine, p struct {
		ID          string        `json:"id"`
		Text        string        `json:"text"`
		Said        *string       `json:"said"`
		ClientID    string        `json:"client_id"`
		Attachments []string      `json:"attachments"`
		Reads       []MessageRead `json:"reads"`
		// Submission names which attachments go as pictures and what was marked
		// on them (media_submission.go).
		Submission *MessageSubmission `json:"submission,omitempty"`
	}) (*TurnResult, error) {
		if strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "agents.send needs an id")
		}
		// Attachments alone are a message: their words are the turn's.
		if strings.TrimSpace(p.Text) == "" && len(p.Attachments) == 0 {
			return nil, Errorf(CodeBadParams, "agents.send needs some text")
		}
		var turn uint64
		var err error
		if p.Submission != nil {
			// THE PICTURES ARE SENT WITH THE MESSAGE OR THE MESSAGE IS NOT SENT: an
			// engine with no door for them refuses before it reads anything, and
			// the staged attachments stay staged.
			c, ok := e.(ComposedSender)
			if !ok {
				return nil, Errorf(CodeUnknownMethod, "this engine does not serve agents.send with pictures")
			}
			if err := CheckAttachIDs(p.Attachments); err != nil {
				return nil, err
			}
			if err := p.Submission.Check(p.Attachments); err != nil {
				return nil, err
			}
			receipt, err := c.SendComposed(p.ID, AgentMessage{Text: p.Text, Said: p.Said, ClientID: p.ClientID, Attachments: p.Attachments, Reads: p.Reads, Submission: p.Submission})
			if err != nil {
				return nil, err
			}
			return &TurnResult{Turn: receipt.Turn, MessageID: receipt.MessageID, Delivery: receipt.Delivery, SeenBy: receipt.SeenBy, Queued: receipt.Queued}, nil
		}
		if p.Said != nil || len(p.Reads) > 0 {
			// WHAT THE MESSAGE CARRIED IS SENT WITH IT OR NOT AT ALL: an engine
			// with no door for it refuses, rather than running the words and
			// keeping a message the person did not send.
			s, ok := e.(MessageSender)
			if !ok {
				return nil, Errorf(CodeUnknownMethod, "this engine does not serve agents.send with said or reads")
			}
			if err := CheckAttachIDs(p.Attachments); err != nil {
				return nil, err
			}
			turn, err = s.SendMessage(p.ID, AgentMessage{Text: p.Text, Said: p.Said, ClientID: p.ClientID, Attachments: p.Attachments, Reads: p.Reads})
		} else if len(p.Attachments) > 0 {
			t, terr := talker(e, "agents.send with attachments")
			if terr != nil {
				return nil, terr
			}
			if err := CheckAttachIDs(p.Attachments); err != nil {
				return nil, err
			}
			turn, err = t.SendWith(p.ID, p.Text, p.ClientID, p.Attachments)
		} else {
			turn, err = e.Send(p.ID, p.Text, p.ClientID)
		}
		if err != nil {
			return nil, err
		}
		return &TurnResult{Turn: turn}, nil
	}),
	"agents.stop": typed(func(e Engine, p IDParams) (*Empty, error) {
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		return done(e.Stop(id))
	}),
	"agents.close": typed(func(e Engine, p IDParams) (*Empty, error) {
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		return done(e.CloseAgent(id))
	}),
	"asks.list": typed(func(e Engine, p AgentParams) (*AsksResult, error) {
		id, err := required("agent", p.Agent)
		if err != nil {
			return nil, err
		}
		asks, err := e.ListAsks(id)
		if err != nil {
			return nil, err
		}
		return &AsksResult{nonNil(asks)}, nil
	}),
	"asks.answer": typed(func(e Engine, p struct {
		Scope     string `json:"scope"`
		Agent     string `json:"agent"`
		ID        string `json:"id"`
		Option    string `json:"option"`
		Text      string `json:"text"`
		Why       string `json:"why"`
		BatchItem string `json:"batch_item"`
		Later     bool   `json:"later"`
		YouDecide bool   `json:"you_decide"`
	}) (*Empty, error) {
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "asks.answer needs an agent and an id")
		}
		if strings.TrimSpace(p.Option) == "" && strings.TrimSpace(p.Text) == "" && !p.Later && !p.YouDecide {
			return nil, Errorf(CodeBadParams, "asks.answer needs an option, some text, later or you_decide")
		}
		if t, ok := e.(Talker); ok {
			return done(t.AnswerAskWith(p.Agent, p.ID, AskAnswer{
				Scope: p.Scope, Option: strings.TrimSpace(p.Option), Text: p.Text, Why: p.Why, BatchItem: p.BatchItem, Later: p.Later, YouDecide: p.YouDecide,
			}))
		}
		if p.Scope != "" || p.Later || p.YouDecide || p.BatchItem != "" || p.Why != "" {
			return nil, Errorf(CodeBadParams, "this engine answers with an option and words only")
		}
		return done(e.AnswerAsk(p.Agent, p.ID, strings.TrimSpace(p.Option), p.Text))
	}),
	"tasks.list": typed(func(e Engine, p AgentParams) (*TasksResult, error) {
		id, err := required("agent", p.Agent)
		if err != nil {
			return nil, err
		}
		tasks, err := e.ListTasks(id)
		if err != nil {
			return nil, err
		}
		return &TasksResult{nonNil(tasks)}, nil
	}),
	"crews.list": typed(func(e Engine, p struct {
		Closed bool `json:"closed"`
	}) (*CrewsResult, error) {
		var crews []Crew
		var err error
		if c, ok := e.(Crewer); ok {
			crews, err = c.CrewList(p.Closed)
		} else {
			crews, err = e.ListCrews()
		}
		if err != nil {
			return nil, err
		}
		for i := range crews {
			crews[i].Members = nonNil(crews[i].Members)
		}
		return &CrewsResult{nonNil(crews)}, nil
	}),
}

// The answers of the methods above that are not answered by a type of their
// own elsewhere.
type (
	AgentsListParams struct {
		Workspace string `json:"workspace"`
		State     string `json:"state" enum:"open,closed,archived,all"`
		Query     string `json:"query"`
	}
	EntriesParams struct {
		ID     string `json:"id"`
		Before string `json:"before"`
		Limit  int    `json:"limit"`
	}
	AgentsResult struct {
		Agents []Agent `json:"agents"`
	}
	EntriesResult struct {
		Entries []Entry `json:"entries"`
		More    bool    `json:"more"`
	}
	TurnResult struct {
		Turn uint64 `json:"turn"`
		// MessageID, Delivery, SeenBy and Queued answer an agents.send that
		// carried a submission (media_submission.go) and are otherwise absent.
		MessageID string `json:"message_id,omitempty"`
		Delivery  string `json:"delivery,omitempty" enum:"text,image,description"`
		SeenBy    string `json:"seen_by,omitempty"`
		Queued    bool   `json:"queued,omitempty"`
	}
	ModelsResult struct {
		Models []Model `json:"models"`
	}
	SettingsResult struct {
		Settings Settings `json:"settings"`
	}
	SpendResult struct {
		Spend Spend `json:"spend"`
	}
	AsksResult struct {
		Asks []Ask `json:"asks"`
	}
	TasksResult struct {
		Tasks []Task `json:"tasks"`
	}
	CrewsResult struct {
		Crews []Crew `json:"crews"`
	}
)

type agentResult struct {
	Agent Agent `json:"agent"`
}

// decode reads params into v, answering bad_params when their shape is wrong.
func decode(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return Errorf(CodeBadParams, "the params could not be read: %v", err)
	}
	return nil
}

// nonNil keeps an empty list a list on the wire, never null.
func nonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
