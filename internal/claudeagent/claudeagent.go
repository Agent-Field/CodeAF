// Package claudeagent holds a live conversation with the person's own Claude
// Code, the way an editor that embeds Claude Code does: one long-lived `claude`
// process speaking stream-json on both pipes, with the Agent SDK's control
// protocol on top of it.
//
// THIS IS THE ARRANGEMENT THE AGENT SDK ITSELF MAKES. The SDK does not call
// Anthropic; it starts the CLI with `--input-format stream-json
// --output-format stream-json --permission-prompt-tool stdio`, sends it an
// `initialize` control request, writes each person's message as a `user` line,
// answers the CLI's `can_use_tool` requests, and sends `interrupt` to stop a
// turn. T3 Code runs Claude Code exactly so (it sets CLAUDE_CODE_ENTRYPOINT to
// name itself). This package is that client in Go, so codeaf can seat Claude
// Code anywhere it seats a worker without a Node runtime and without holding a
// Claude credential: the CLI signs in on its own account.
//
// A SESSION IS MANY TURNS. A message sent while a turn is running is written at
// once and Claude Code takes it in; a turn ends with one `result` event, and
// the process stays up for the next message until [Session.Close].
package claudeagent

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Entrypoint is the name codeaf gives itself to Claude Code, which records it
// beside the session the way it records `t3-code` or `sdk-ts`.
const Entrypoint = "codeaf"

// Options is how a session is started.
type Options struct {
	// Binary is the `claude` program to start.
	Binary string
	// Dir is the folder Claude Code works in.
	Dir string
	// Model is a model word or name (`opus`, `claude-sonnet-4-5`); empty is
	// Claude Code's own default.
	Model string
	// PermissionMode is Claude Code's mode: default, acceptEdits, plan or
	// bypassPermissions. Empty is default, where every tool use that is not
	// already allowed is asked of [Options.CanUseTool].
	PermissionMode string
	// Resume continues an earlier Claude Code session by its id.
	Resume string
	// AppendSystemPrompt is added after Claude Code's own system prompt.
	AppendSystemPrompt string
	// Disallowed is Claude Code deny rules (`Bash(git push:*)`), which hold
	// even over a hook that allows.
	Disallowed []string
	// MaxBudgetUSD stops the session at that much list-price spend.
	MaxBudgetUSD float64
	// CanUseTool answers a permission request. Nil allows everything, which
	// only a caller that set PermissionMode to bypassPermissions should want.
	CanUseTool func(ctx context.Context, req PermissionRequest) Decision
	// OnEvent is handed every event the CLI prints, in order, on the reading
	// goroutine. It must not block.
	OnEvent func(Event)
	// Stderr receives the CLI's own diagnostics; nil discards them.
	Stderr io.Writer
	// Env is added to the inherited environment.
	Env []string
	// Tools, when set, are tools this client serves to Claude Code inside the
	// session, beside its own (see [ToolServer]).
	Tools *ToolServer
	// PluginDirs are Claude Code plugin folders loaded for this session only,
	// beside the plugins the person installed: how a skill kept for codeaf
	// reaches Claude Code without being copied into its own folders.
	PluginDirs []string
}

// ToolServer is a set of tools codeaf serves to Claude Code in-process, over
// the same control channel as everything else: Claude Code's SDK MCP servers.
//
// IT IS HOW CLAUDE CODE REACHES CODEAF'S OWN VERBS. The CLI is told of a
// server of type `sdk` and, instead of starting a process, sends each MCP
// message to this client as an `mcp_message` control request; the reply goes
// back as that request's answer. So propose_task, the team tools and the
// manual are tools Claude Code can call by name (`mcp__<Name>__<tool>`)
// while it keeps its own Bash, Read and Edit.
type ToolServer struct {
	// Name is the server's name; Claude Code sees its tools as mcp__Name__tool.
	Name string
	// List answers which tools there are.
	List func() []Tool
	// Call runs one call and may block for as long as the tool takes; Claude
	// Code waits on it.
	Call func(ctx context.Context, call ToolCall) ToolResult
}

// Tool is one tool a [ToolServer] serves.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolCall is Claude Code calling a served tool.
type ToolCall struct {
	Name      string
	Arguments json.RawMessage
	// ToolUseID is Claude Code's own id for the call.
	ToolUseID string
}

// ToolResult is what a served tool answered.
type ToolResult struct {
	Text    string
	IsError bool
}

// PermissionRequest is Claude Code asking whether a tool may run.
type PermissionRequest struct {
	ToolName  string          `json:"tool_name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Title     string          `json:"title"`
	Reason    string          `json:"decision_reason"`
}

// Decision is the answer to a [PermissionRequest].
type Decision struct {
	Allow bool
	// Message tells Claude Code why a tool was refused, so it can choose
	// another road instead of asking again.
	Message string
}

// Event is one line Claude Code printed. Raw is the whole line; the named
// fields are the ones a caller needs to route it.
type Event struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Raw       json.RawMessage
}

// Session is one live Claude Code process.
type Session struct {
	opts  Options
	cmd   *exec.Cmd
	stdin io.WriteCloser

	writeMu sync.Mutex
	mu      sync.Mutex
	waiting map[string]chan controlReply
	session string
	closed  bool

	done chan struct{}
	err  error
}

type controlReply struct {
	ok       bool
	response json.RawMessage
	err      string
}

// Start starts Claude Code and completes the SDK's initialize handshake.
func Start(ctx context.Context, opts Options) (*Session, error) {
	if opts.Binary == "" {
		return nil, errors.New("no Claude Code program was named")
	}
	cmd := exec.Command(opts.Binary, Args(opts)...)
	cmd.Dir = opts.Dir
	cmd.Env = append(append(os.Environ(), "CLAUDE_CODE_ENTRYPOINT="+Entrypoint), opts.Env...)
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("Claude Code did not start: %w", err)
	}
	s := &Session{opts: opts, cmd: cmd, stdin: stdin, waiting: map[string]chan controlReply{}, done: make(chan struct{})}
	go s.read(stdout)
	initCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	initialize := map[string]any{"subtype": "initialize"}
	if opts.Tools != nil {
		initialize["sdkMcpServers"] = []string{opts.Tools.Name}
	}
	if _, err := s.control(initCtx, initialize); err != nil {
		s.kill()
		return nil, fmt.Errorf("Claude Code did not answer its handshake: %w", err)
	}
	return s, nil
}

// Args is the command line a session starts Claude Code with. It is the line
// the Agent SDK builds, less the flags codeaf has no use for.
func Args(opts Options) []string {
	args := []string{
		"--output-format", "stream-json", "--verbose",
		"--input-format", "stream-json",
		"--permission-prompt-tool", "stdio",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.PermissionMode != "" {
		args = append(args, "--permission-mode", opts.PermissionMode)
		if opts.PermissionMode == "bypassPermissions" {
			args = append(args, "--allow-dangerously-skip-permissions")
		}
	}
	if opts.Resume != "" {
		args = append(args, "--resume", opts.Resume)
	}
	if opts.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.AppendSystemPrompt)
	}
	if len(opts.Disallowed) > 0 {
		args = append(args, "--disallowedTools", strings.Join(opts.Disallowed, ","))
	}
	if opts.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", opts.MaxBudgetUSD))
	}
	if opts.Tools != nil {
		config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{opts.Tools.Name: map[string]any{"type": "sdk", "name": opts.Tools.Name}}})
		args = append(args, "--mcp-config", string(config))
	}
	for _, dir := range opts.PluginDirs {
		args = append(args, "--plugin-dir", dir)
	}
	return args
}

// Send writes one person's message. It may be sent while a turn is running;
// Claude Code takes it in at its next chance.
func (s *Session) Send(text string) error { return s.SendWith(text, nil) }

// Image is a picture sent with a message, as Claude's own content block takes
// one: its media type and its bytes in base64.
type Image struct {
	MediaType string
	Data      string
}

// SendWith is [Session.Send] with pictures, which go before the words.
func (s *Session) SendWith(text string, images []Image) error {
	content := make([]map[string]any, 0, len(images)+1)
	for _, image := range images {
		content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": image.MediaType, "data": image.Data}})
	}
	content = append(content, map[string]any{"type": "text", "text": text})
	return s.write(map[string]any{
		"type":               "user",
		"session_id":         "",
		"parent_tool_use_id": nil,
		"message":            map[string]any{"role": "user", "content": content},
	})
}

// Interrupt stops the running turn. The session stays up for another message.
func (s *Session) Interrupt(ctx context.Context) error {
	_, err := s.control(ctx, map[string]any{"subtype": "interrupt"})
	return err
}

// SetModel switches the model for the turns after this one.
func (s *Session) SetModel(ctx context.Context, model string) error {
	_, err := s.control(ctx, map[string]any{"subtype": "set_model", "model": model})
	return err
}

// SetPermissionMode changes how tool use is asked about from now on.
func (s *Session) SetPermissionMode(ctx context.Context, mode string) error {
	_, err := s.control(ctx, map[string]any{"subtype": "set_permission_mode", "mode": mode})
	return err
}

// SessionID is Claude Code's id for this conversation, which [Options.Resume]
// takes to continue it later. It is empty until the CLI has said it.
func (s *Session) SessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session
}

// Done is closed when the process has ended.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err is why the process ended, once [Session.Done] is closed.
func (s *Session) Err() error { <-s.done; return s.err }

// Close ends the session the polite way: its input is closed, which Claude
// Code reads as the end of the conversation, and it is killed only if it has
// not gone within the grace.
func (s *Session) Close(grace time.Duration) error {
	s.mu.Lock()
	already := s.closed
	s.closed = true
	s.mu.Unlock()
	if !already {
		_ = s.stdin.Close()
	}
	select {
	case <-s.done:
	case <-time.After(grace):
		s.kill()
		<-s.done
	}
	return s.err
}

func (s *Session) kill() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

func (s *Session) control(ctx context.Context, request map[string]any) (json.RawMessage, error) {
	id := newID()
	reply := make(chan controlReply, 1)
	s.mu.Lock()
	s.waiting[id] = reply
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.waiting, id); s.mu.Unlock() }()
	if err := s.write(map[string]any{"type": "control_request", "request_id": id, "request": request}); err != nil {
		return nil, err
	}
	select {
	case r := <-reply:
		if !r.ok {
			return nil, errors.New(r.err)
		}
		return r.response, nil
	case <-s.done:
		return nil, errors.New("Claude Code ended")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Session) write(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.stdin.Write(append(line, '\n'))
	return err
}

// incoming is the shape of every line, read once to route it.
type incoming struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
	Response  struct {
		Subtype   string          `json:"subtype"`
		RequestID string          `json:"request_id"`
		Response  json.RawMessage `json:"response"`
		Error     string          `json:"error"`
	} `json:"response"`
}

func (s *Session) read(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 32<<20)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var in incoming
		if json.Unmarshal(line, &in) != nil {
			continue
		}
		switch in.Type {
		case "control_response":
			s.mu.Lock()
			reply, ok := s.waiting[in.Response.RequestID]
			s.mu.Unlock()
			if ok {
				reply <- controlReply{ok: in.Response.Subtype == "success", response: in.Response.Response, err: in.Response.Error}
			}
			continue
		case "control_request":
			go s.answer(in.RequestID, in.Request)
			continue
		case "control_cancel_request":
			continue
		}
		if in.SessionID != "" {
			s.mu.Lock()
			s.session = in.SessionID
			s.mu.Unlock()
		}
		if s.opts.OnEvent != nil {
			s.opts.OnEvent(Event{Type: in.Type, Subtype: in.Subtype, SessionID: in.SessionID, Raw: line})
		}
	}
	s.err = s.cmd.Wait()
	close(s.done)
}

// answer replies to a control request the CLI sent: a permission question, or
// anything else, which this client does not serve and says so.
func (s *Session) answer(id string, raw json.RawMessage) {
	var head struct {
		Subtype string `json:"subtype"`
	}
	_ = json.Unmarshal(raw, &head)
	if head.Subtype == "mcp_message" && s.opts.Tools != nil {
		s.serveTool(id, raw)
		return
	}
	if head.Subtype != "can_use_tool" {
		_ = s.write(map[string]any{"type": "control_response", "response": map[string]any{
			"subtype": "error", "request_id": id, "error": "codeaf does not serve " + head.Subtype,
		}})
		return
	}
	var req PermissionRequest
	_ = json.Unmarshal(raw, &req)
	decision := Decision{Allow: true}
	if s.opts.CanUseTool != nil {
		decision = s.opts.CanUseTool(context.Background(), req)
	}
	response := map[string]any{"behavior": "deny", "message": decision.Message, "toolUseID": req.ToolUseID}
	if decision.Allow {
		input := req.Input
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		response = map[string]any{"behavior": "allow", "updatedInput": input, "toolUseID": req.ToolUseID}
	}
	_ = s.write(map[string]any{"type": "control_response", "response": map[string]any{
		"subtype": "success", "request_id": id, "response": response,
	}})
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Text is the plain text of an assistant or result event, for a caller that
// wants words rather than blocks.
func Text(ev Event) string {
	var line struct {
		Result  string `json:"result"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	_ = json.Unmarshal(ev.Raw, &line)
	if ev.Type == "result" {
		return line.Result
	}
	var b strings.Builder
	for _, c := range line.Message.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// serveTool answers one MCP message for the [ToolServer]: the handshake, the
// list, a call. Notifications carry no id and are acknowledged empty.
func (s *Session) serveTool(id string, raw json.RawMessage) {
	var req struct {
		Message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		} `json:"message"`
	}
	_ = json.Unmarshal(raw, &req)
	msg := req.Message
	var result any = map[string]any{}
	switch msg.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		if params.ProtocolVersion == "" {
			params.ProtocolVersion = "2024-11-05"
		}
		result = map[string]any{"protocolVersion": params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{"name": s.opts.Tools.Name, "version": "1"}}
	case "tools/list":
		tools := []Tool{}
		if s.opts.Tools.List != nil {
			tools = append(tools, s.opts.Tools.List()...)
		}
		result = map[string]any{"tools": tools}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Meta      struct {
				ToolUseID string `json:"claudecode/toolUseId"`
			} `json:"_meta"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		if len(params.Arguments) == 0 {
			params.Arguments = json.RawMessage("{}")
		}
		answer := ToolResult{Text: "codeaf serves no tool calls here", IsError: true}
		if s.opts.Tools.Call != nil {
			answer = s.opts.Tools.Call(context.Background(), ToolCall{Name: params.Name, Arguments: params.Arguments, ToolUseID: params.Meta.ToolUseID})
		}
		result = map[string]any{"content": []any{map[string]any{"type": "text", "text": answer.Text}}, "isError": answer.IsError}
	}
	reply := map[string]any{"jsonrpc": "2.0", "result": result}
	if len(msg.ID) > 0 {
		reply["id"] = msg.ID
	}
	_ = s.write(map[string]any{"type": "control_response", "response": map[string]any{
		"subtype": "success", "request_id": id, "response": map[string]any{"mcp_response": reply},
	}})
}
