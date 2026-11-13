// Package claudemodel lets the person's own Claude Code answer codeaf's small,
// toolless calls — titles, summaries, the light seat — the way a model service
// does, so a codeaf with no OpenRouter key and only a Claude plan still has
// something to name a conversation with.
//
// IT IS A TRANSPORT, LIKE CODEX'S. The provider client speaks OpenAI chat
// completions to whatever service a seat is on; for the Claude Code service the
// http.Client it is handed answers `/chat/completions` by running one
// `claude -p` (internal/claudeagent's Once) and `/models` from [Models]. No
// request leaves the machine except the one Claude Code itself makes on its
// own sign-in.
//
// A CALL WITH TOOLS IS NEVER SENT AS ONE. Claude Code will not hand back a
// call to a tool it does not hold (a schema-shaped answer was tried and it
// called the tool itself), so a request that carries codeaf's tool belt is a
// turn of a conversation or a crew member, and it is answered by that
// conversation's own live Claude Code session working with its own tools
// (agent.go). A request with no tools is one `claude -p`.
package claudemodel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/claudeagent"
)

// ID is the service's id and the word its persisted row is written with.
const ID = "claude-code"

// Name is how every surface spells the service.
const Name = "Claude Code"

// Address is the service's address. Nothing is ever dialled there: the
// transport answers every request itself.
const Address = "http://claude-code.local/v1"

// Sentinel is the key the service's row carries. The sign-in is Claude Code's
// own and codeaf never holds it.
const Sentinel = "claude-code"

// window is the context Claude Code's models take.
const window = 200_000

// families are the models Claude Code answers to. A family's word (`haiku`)
// runs whichever version the installed Claude Code maps it to, so it follows
// that program's updates and, on an older install, is an older model. Its
// versions are the two newest, listed so a person can hold a seat to one of
// them whatever the word means on this computer.
var families = []struct {
	word, name string
	versions   []string
}{
	{"opus", "Claude Opus", []string{"5.5", "5"}},
	{"sonnet", "Claude Sonnet", []string{"5.5", "5"}},
	{"haiku", "Claude Haiku", []string{"5.5", "4.5"}},
}

// Models is the service's catalog: the three words Claude Code answers to,
// then each family's versions under Claude Code's own name for them
// (`claude-haiku-5-5`), every row with tools among its parameters.
func Models() []catalog.Model {
	row := func(id, name string) catalog.Model {
		return catalog.Model{
			ID: id, Name: name, ContextLength: window, PriceUnknown: true,
			Parameters: []string{"max_tokens", "temperature", "stop", "tools", "tool_choice"},
		}
	}
	rows := make([]catalog.Model, 0, 3*len(families))
	for _, family := range families {
		rows = append(rows, row(family.word, family.name))
	}
	for _, family := range families {
		for _, version := range family.versions {
			rows = append(rows, row("claude-"+family.word+"-"+strings.ReplaceAll(version, ".", "-"), family.name+" "+version))
		}
	}
	return rows
}

// Version answers the Claude Code model name a seat's id spells in full
// (`claude-code/claude-haiku-4-5`, or the name alone), and "" for a family
// word or another service's spelling of a Claude model.
//
// A NAME IN CLAUDE CODE'S OWN SPELLING IS SENT WHOLE. Reading it down to its
// family word would run whichever version the installed Claude Code maps the
// word to, which is not the version the person picked.
func Version(id string) string {
	name := strings.TrimPrefix(strings.TrimSpace(id), ID+"/")
	if !strings.HasPrefix(name, "claude-") || strings.ContainsAny(name, "/. ") {
		return ""
	}
	return name
}

// Client is the http.Client the provider is handed for the Claude Code service
// where no conversation folder is known: titles, summaries, catalog reads.
func Client() *http.Client { return ClientIn("") }

// ClientIn is the client for a conversation working in dir, whose turns with
// tools are turns of a Claude Code session in that folder.
func ClientIn(dir string) *http.Client { return ClientAt(Seat{Dir: dir}) }

// Seat is where a conversation's Claude Code session works and what it carries.
type Seat struct {
	// Dir is the conversation's folder.
	Dir string
	// Kept is the file the session's id is kept in, so the conversation picks
	// up the same session after codeaf restarts rather than starting Claude
	// Code over with the history written out; "" keeps nothing.
	Kept string
}

type envKey struct{}

// WithEnv carries variables a request's turn wants in Claude Code's
// environment, and so in every command it runs: the conversation's agent
// identity, which a tool such as `canvas` records as who made a change,
// exactly as a command codeaf runs sees it.
//
// IT RIDES THE REQUEST, NOT THE CLIENT, because a conversation's first client
// is built before its identity is settled; the turn that sends the request
// always knows it. It is read when the conversation's session starts.
func WithEnv(ctx context.Context, env []string) context.Context {
	if len(env) == 0 {
		return ctx
	}
	return context.WithValue(ctx, envKey{}, env)
}

func envFrom(ctx context.Context) []string {
	env, _ := ctx.Value(envKey{}).([]string)
	return env
}

// ClientAt is the client for a conversation seated as seat says.
func ClientAt(seat Seat) *http.Client {
	return &http.Client{Transport: transport{ask: claudeagent.Once, dir: seat.Dir, kept: seat.Kept}}
}

type transport struct {
	ask  func(context.Context, claudeagent.Ask) (claudeagent.Answer, error)
	dir  string
	kept string
}

func (t transport) RoundTrip(request *http.Request) (*http.Response, error) {
	path := strings.TrimRight(request.URL.Path, "/")
	switch {
	case strings.HasSuffix(path, "/models"):
		return jsonResponse(request, http.StatusOK, modelList())
	case strings.HasSuffix(path, "/chat/completions") && request.Method == http.MethodPost:
		return t.complete(request)
	}
	return jsonResponse(request, http.StatusNotFound, errorBody("Claude Code answers chat completions and its model list only"))
}

type chatRequest struct {
	Model          string          `json:"model"`
	Stream         bool            `json:"stream"`
	Tools          json.RawMessage `json:"tools"`
	PromptCacheKey string          `json:"prompt_cache_key"`
	Messages       []chatMessage   `json:"messages"`
}

type chatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []struct {
		ID string `json:"id"`
	} `json:"tool_calls,omitempty"`
}

func (t transport) complete(request *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	var chat chatRequest
	if err := json.Unmarshal(raw, &chat); err != nil {
		return jsonResponse(request, http.StatusBadRequest, errorBody("the request is not chat completions JSON"))
	}
	if tools := strings.TrimSpace(string(chat.Tools)); tools != "" && tools != "null" && tools != "[]" {
		conversation := strings.TrimSpace(chat.PromptCacheKey)
		if conversation == "" {
			conversation = strings.TrimSpace(request.Header.Get("X-Session-Affinity"))
		}
		return t.agentTurn(request, chat, conversation)
	}
	ask := Compose(chat)
	answer, err := t.ask(request.Context(), ask)
	if err != nil {
		// A 503 READS AS "THIS SERVICE IS DOWN, TRY ANOTHER", which is the
		// truth of a Claude Code that is signed out or not installed.
		return jsonResponse(request, http.StatusServiceUnavailable, errorBody(err.Error()))
	}
	id := fmt.Sprintf("claude-code-%d", time.Now().UnixNano())
	usage := map[string]any{"prompt_tokens": answer.InputTokens, "completion_tokens": answer.OutputTokens, "total_tokens": answer.InputTokens + answer.OutputTokens}
	if chat.Stream {
		var b bytes.Buffer
		for _, chunk := range []map[string]any{
			{"id": id, "object": "chat.completion.chunk", "model": ask.Model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": answer.Text}}}},
			{"id": id, "object": "chat.completion.chunk", "model": ask.Model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}, "usage": usage},
		} {
			line, _ := json.Marshal(chunk)
			b.WriteString("data: ")
			b.Write(line)
			b.WriteString("\n\n")
		}
		b.WriteString("data: [DONE]\n\n")
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
			Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:   io.NopCloser(&b), ContentLength: int64(b.Len()), Request: request,
		}, nil
	}
	return jsonResponse(request, http.StatusOK, map[string]any{
		"id": id, "object": "chat.completion", "model": ask.Model, "usage": usage,
		"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": answer.Text}}},
	})
}

// Compose turns a chat request into one prompt for Claude Code: the system
// messages become its system prompt, and the rest is the conversation written
// out, ending on what the assistant is to answer.
func Compose(chat chatRequest) claudeagent.Ask {
	var system []string
	var turns []string
	for _, m := range chat.Messages {
		text := strings.TrimSpace(contentText(m.Content))
		if text == "" {
			continue
		}
		switch m.Role {
		case "system", "developer":
			system = append(system, text)
		case "assistant":
			turns = append(turns, "Assistant: "+text)
		case "tool":
			turns = append(turns, "Tool result: "+text)
		default:
			turns = append(turns, "User: "+text)
		}
	}
	prompt := ""
	switch {
	case len(turns) == 1 && strings.HasPrefix(turns[0], "User: "):
		prompt = strings.TrimPrefix(turns[0], "User: ")
	case len(turns) > 0:
		prompt = "The conversation so far:\n\n" + strings.Join(turns, "\n\n") + "\n\nWrite the assistant's next message, and only that."
	}
	return claudeagent.Ask{Model: model(chat.Model), System: strings.Join(system, "\n\n"), Prompt: prompt}
}

// model is what Claude Code is asked to run for a seat's id: a service prefix
// is dropped (`claude-code/sonnet`), a version in Claude Code's own spelling
// (`claude-code/claude-haiku-4-5`) is kept whole, and another catalog's
// spelling of a Claude model (`anthropic/claude-opus-4.6`) is read as its
// family.
func model(id string) string {
	id = strings.TrimSpace(id)
	if version := Version(id); version != "" {
		return version
	}
	if family := claudeagent.Family(id); family != "" {
		return family
	}
	if _, rest, ok := strings.Cut(id, "/"); ok {
		return rest
	}
	return id
}

func contentText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func modelList() map[string]any {
	models := Models()
	data := make([]any, 0, len(models))
	for _, m := range models {
		data = append(data, map[string]any{"id": m.ID, "name": m.Name, "context_length": m.ContextLength, "supported_parameters": m.Parameters})
	}
	return map[string]any{"object": "list", "data": data}
}

func errorBody(message string) map[string]any {
	return map[string]any{"error": map[string]any{"message": message, "type": "claude_code"}}
}

func jsonResponse(request *http.Request, status int, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(bytes.NewReader(raw)), ContentLength: int64(len(raw)), Request: request,
	}, nil
}
