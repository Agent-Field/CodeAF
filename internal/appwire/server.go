package appwire

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

// writerQueue is how many frames may wait for stdout before a sender waits.
// It bounds the memory a stalled reader can make this process hold, and it is
// never what the engine's own read loops wait on: those hand their events to
// an [Outbox], whose sender is the one that waits here.
const writerQueue = 1024

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
	s.Emit(EventHello, hello)

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
		result, err := method(engine, params)
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

// method answers one request from its params, which are already known to be
// a JSON object.
type method func(Engine, json.RawMessage) (any, error)

// methods is the dispatch table. shutdown is not in it: it is answered by the
// loop itself, because it ends the loop.
var methods = map[string]method{
	"ping": func(Engine, json.RawMessage) (any, error) { return struct{}{}, nil },
	"agents.list": func(e Engine, raw json.RawMessage) (any, error) {
		var p struct {
			Workspace string `json:"workspace"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		agents, err := e.ListAgents(p.Workspace)
		if err != nil {
			return nil, err
		}
		return struct {
			Agents []Agent `json:"agents"`
		}{nonNil(agents)}, nil
	},
	"agents.create": func(e Engine, raw json.RawMessage) (any, error) {
		var p struct {
			Workspace string `json:"workspace"`
			Model     string `json:"model"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Workspace) == "" {
			return nil, Errorf(CodeBadParams, "agents.create needs a workspace")
		}
		agent, err := e.CreateAgent(p.Workspace, p.Model)
		if err != nil {
			return nil, err
		}
		return agentResult{agent}, nil
	},
	"agents.open": func(e Engine, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		agent, err := e.OpenAgent(id)
		if err != nil {
			return nil, err
		}
		return agentResult{agent}, nil
	},
	"agents.send": func(e Engine, raw json.RawMessage) (any, error) {
		var p struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "agents.send needs an id")
		}
		if strings.TrimSpace(p.Text) == "" {
			return nil, Errorf(CodeBadParams, "agents.send needs some text")
		}
		turn, err := e.Send(p.ID, p.Text)
		if err != nil {
			return nil, err
		}
		return struct {
			Turn uint64 `json:"turn"`
		}{turn}, nil
	},
	"agents.stop": func(e Engine, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		return struct{}{}, e.Stop(id)
	},
	"agents.close": func(e Engine, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		return struct{}{}, e.CloseAgent(id)
	},
	"tasks.list": func(e Engine, raw json.RawMessage) (any, error) {
		id, err := decodeID(raw, "agent")
		if err != nil {
			return nil, err
		}
		tasks, err := e.ListTasks(id)
		if err != nil {
			return nil, err
		}
		return struct {
			Tasks []Task `json:"tasks"`
		}{nonNil(tasks)}, nil
	},
	"crews.list": func(e Engine, raw json.RawMessage) (any, error) {
		crews, err := e.ListCrews()
		if err != nil {
			return nil, err
		}
		for i := range crews {
			crews[i].Members = nonNil(crews[i].Members)
		}
		return struct {
			Crews []Crew `json:"crews"`
		}{nonNil(crews)}, nil
	},
}

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

// decodeID reads the one required string a method takes.
func decodeID(raw json.RawMessage, field string) (string, error) {
	var p map[string]json.RawMessage
	if err := decode(raw, &p); err != nil {
		return "", err
	}
	var id string
	if value, ok := p[field]; ok {
		if err := json.Unmarshal(value, &id); err != nil {
			return "", Errorf(CodeBadParams, "%s must be a string", field)
		}
	}
	if strings.TrimSpace(id) == "" {
		return "", Errorf(CodeBadParams, "%s is required", field)
	}
	return id, nil
}

// nonNil keeps an empty list a list on the wire, never null.
func nonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
