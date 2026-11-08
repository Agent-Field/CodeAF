package claudemodel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/claudeagent"
)

// ── A TURN WITH TOOLS IS A TURN OF CLAUDE CODE ITSELF ──────────────────────
//
// A conversation or a crew member seated on Claude Code sends codeaf's turn
// loop through this transport like any model. Its request carries codeaf's
// tool belt, which Claude Code cannot be handed as tools it would hand back,
// so a request WITH tools is answered by a live Claude Code session held for
// that conversation, in that conversation's folder: the person's newest words
// go in, Claude Code works with its own tools, and its final message comes
// back as the assistant's answer with no tool calls, which ends codeaf's turn.
//
// What it does on the way is streamed as reasoning — each tool call as it
// starts, and a line while a long one runs — so the person watches it work
// and codeaf's stall guard, which ends a stream silent for 45 seconds, sees a
// stream that is alive.

// heartbeat is how often a quiet turn says it is still working.
var heartbeat = 20 * time.Second

// handoff is how long a turn whose request was cut keeps running, waiting for
// the request that carries the person's correction.
//
// A CORRECTION IS NOT A STOP. When a person types into a running turn, codeaf
// cuts the model request in flight and sends the next one at once with their
// words as the newest message. Claude Code works inside ONE request, so a cut
// read as a stop would interrupt its edit or its build halfway. T3 Code instead
// writes the words into the running session, and Claude Code takes them in at
// its next step. So a cut request leaves the turn running, and the request that
// follows within this window adopts it and writes the words in. Only a cut that
// no request follows (the person pressed stop) interrupts Claude Code.
var handoff = 3 * time.Second

// settle is how long a turn that took a correction waits after a result for the
// turn the correction may still start: Claude Code answers a message folded
// into a running turn with that turn's one result, and a message that arrived
// too late for it with a turn of its own.
var settle = 2 * time.Second

// outwardRules are refused even when the person's own hooks would allow them:
// pushing, opening or merging a pull request, a release, a publish.
var outwardRules = []string{
	"Bash(git push:*)", "Bash(rtk git push:*)", "Bash(gh pr create:*)", "Bash(gh pr merge:*)",
	"Bash(gh release:*)", "Bash(npm publish:*)", "Bash(pnpm publish:*)", "Bash(cargo publish:*)",
}

// seated is the prompt added to Claude Code's own when it is a conversation.
const seated = "You are seated in codeaf as this conversation's agent. Work in the folder with your own tools. " +
	"codeaf's own tools are yours too, named mcp__codeaf__*: propose_task for work that should run as its own task, " +
	"the team tools to work with your crew, manual for anything about codeaf itself. " +
	"Pushing, publishing and opening pull requests are the person's to do: say what to run instead."

// native are codeaf tools Claude Code already has its own of, so they are not
// served to it twice: it reads, edits, searches and runs commands its own way.
var native = map[string]bool{
	"bash": true, "read": true, "write": true, "edit": true, "grep": true, "find": true, "ls": true,
	"glob": true, "web_fetch": true, "web_search": true, "view_image": true,
}

// pendingCall is one call Claude Code made to a codeaf tool, waiting for
// codeaf's turn loop to run it and send the result back in its next request.
type pendingCall struct {
	id     string
	name   string
	args   json.RawMessage
	result chan claudeagent.ToolResult
}

// live is one conversation's Claude Code session.
type live struct {
	session *claudeagent.Session
	model   string
	turn    sync.Mutex // one turn at a time
	mu      sync.Mutex
	sink    chan claudeagent.Event
	turns   int
	// seen is how many of the conversation's messages this session has been
	// given, its own answer included, so a turn after another model had the
	// conversation hands Claude Code what was said while it was away.
	seen int
	// kept is the file the session's id and seen are written to after each
	// turn; "" keeps nothing.
	kept string
	// adoptable is a turn whose request was cut and that is still running,
	// waiting for the next request to take it over; letGo interrupts it when
	// none does.
	adoptable bool
	letGo     *time.Timer
	// belt is codeaf's tools as this conversation's latest request carried
	// them, less the ones Claude Code has natively.
	belt []claudeagent.Tool
	// calls are Claude Code's calls to codeaf tools as they arrive; awaiting
	// are the ones handed to codeaf and not yet answered.
	calls    chan *pendingCall
	awaiting map[string]*pendingCall
	// started are Claude Code's own calls waiting for their results, by its
	// id for them. Only the turn's stream reads or writes it, and one stream
	// runs at a time, a cut one's calls carrying over to the stream that
	// adopts its turn.
	started map[string]startedStep
}

// tools is the served list.
func (l *live) tools() []claudeagent.Tool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]claudeagent.Tool(nil), l.belt...)
}

// call hands one call to the running turn and waits for codeaf's answer.
func (l *live) call(ctx context.Context, call claudeagent.ToolCall) claudeagent.ToolResult {
	id := call.ToolUseID
	if id == "" {
		id = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	pc := &pendingCall{id: stepPrefix + id, name: call.Name, args: call.Arguments, result: make(chan claudeagent.ToolResult, 1)}
	select {
	case l.calls <- pc:
	case <-l.session.Done():
		return claudeagent.ToolResult{Text: "the session ended", IsError: true}
	}
	select {
	case answer := <-pc.result:
		return answer
	case <-l.session.Done():
		return claudeagent.ToolResult{Text: "the session ended", IsError: true}
	}
}

// beltOf reads the request's tools into the served list.
func beltOf(raw json.RawMessage) []claudeagent.Tool {
	var tools []struct {
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	_ = json.Unmarshal(raw, &tools)
	out := make([]claudeagent.Tool, 0, len(tools))
	for _, t := range tools {
		if t.Function.Name == "" || native[t.Function.Name] {
			continue
		}
		schema := t.Function.Parameters
		if len(schema) == 0 || string(schema) == "null" {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, claudeagent.Tool{Name: t.Function.Name, Description: t.Function.Description, InputSchema: schema})
	}
	return out
}

var (
	livesMu sync.Mutex
	lives   = map[string]*live{}
)

func liveKey(dir, conversation string) string { return dir + "\x00" + conversation }

// liveFor answers the session for this conversation, starting one when there
// is none or the last one has ended.
func liveFor(ctx context.Context, dir, conversation, model, kept string, env []string, belt []claudeagent.Tool) (*live, error) {
	livesMu.Lock()
	defer livesMu.Unlock()
	key := liveKey(dir, conversation)
	if l := lives[key]; l != nil {
		select {
		case <-l.session.Done():
		default:
			if model != "" && model != l.model {
				if err := l.session.SetModel(ctx, model); err == nil {
					l.model = model
				}
			}
			return l, nil
		}
	}
	binary, err := claudeagent.Binary()
	if err != nil {
		return nil, err
	}
	l := &live{model: model, kept: kept, belt: belt, calls: make(chan *pendingCall, 64), awaiting: map[string]*pendingCall{}, started: map[string]startedStep{}}
	options := claudeagent.Options{
		Binary: binary, Dir: dir, Model: model, PermissionMode: "acceptEdits",
		AppendSystemPrompt: seated, Disallowed: outwardRules, Stderr: os.Stderr,
		Tools:      &claudeagent.ToolServer{Name: "codeaf", List: l.tools, Call: l.call},
		PluginDirs: pluginDirs(dir),
		// A codeaf tool may wait on the person (a proposed task, a question),
		// so Claude Code is told to wait as long as an hour for one.
		Env: append([]string{"MCP_TOOL_TIMEOUT=3600000"}, env...),
		OnEvent: func(ev claudeagent.Event) {
			l.mu.Lock()
			sink := l.sink
			l.mu.Unlock()
			if sink != nil {
				sink <- ev
			}
		},
	}
	// A CONVERSATION THAT HAD A SESSION RESUMES IT, as T3 Code does, so
	// Claude Code keeps what it read and did and is not handed the history
	// as text. One that cannot be resumed (Claude Code forgot it) starts over.
	if saved := readKept(kept); saved.Session != "" {
		resumed := options
		resumed.Resume = saved.Session
		if session, err := claudeagent.Start(context.Background(), resumed); err == nil {
			l.session, l.seen, l.turns = session, saved.Seen, 1
			lives[key] = l
			return l, nil
		}
	}
	session, err := claudeagent.Start(context.Background(), options)
	if err != nil {
		return nil, err
	}
	l.session = session
	lives[key] = l
	return l, nil
}

// pluginDirs is the plugin of codeaf's skills for dir, when there is one.
func pluginDirs(dir string) []string {
	if root := claudeagent.SkillPlugin(dir); root != "" {
		return []string{root}
	}
	return nil
}

// CloseAll ends every live session, for a process that is shutting down.
func CloseAll() {
	livesMu.Lock()
	defer livesMu.Unlock()
	for key, l := range lives {
		_ = l.session.Close(5 * time.Second)
		delete(lives, key)
	}
}

// agentTurn answers one request with tools by one turn of the conversation's
// Claude Code session.
func (t transport) agentTurn(request *http.Request, chat chatRequest, conversation string) (*http.Response, error) {
	dir := t.dir
	if dir == "" {
		return jsonResponse(request, http.StatusServiceUnavailable, errorBody("this conversation has no folder for Claude Code to work in"))
	}
	model := model(chat.Model)
	belt := beltOf(chat.Tools)
	l, err := liveFor(request.Context(), dir, conversation, model, t.kept, envFrom(request.Context()), belt)
	if err != nil {
		return jsonResponse(request, http.StatusServiceUnavailable, errorBody(err.Error()))
	}
	l.turn.Lock()
	l.mu.Lock()
	l.belt = belt
	if len(l.awaiting) > 0 {
		return l.resume(request, chat, model)
	}
	events, adopted := l.sink, l.adoptable
	// A TURN THAT IS ADOPTED HAS SEEN EVERYTHING BUT THE CORRECTION, so only
	// the newest words go in; a fresh session is given the whole history.
	from := l.seen
	switch {
	case adopted:
		// The cut request's messages reached the running turn; no answer
		// followed them, so the correction starts one short of seen.
		from = l.seen - 1
	case l.turns == 0:
		from = 0
	}
	prompt := turnPrompt(chat, from)
	if prompt == "" {
		l.mu.Unlock()
		l.turn.Unlock()
		return jsonResponse(request, http.StatusBadRequest, errorBody("there is nothing new to say to Claude Code"))
	}
	l.seen = len(spoken(chat.Messages)) + 1
	if adopted {
		l.letGo.Stop()
		l.adoptable = false
	} else {
		events = make(chan claudeagent.Event, 256)
		l.sink = events
	}
	l.mu.Unlock()
	if err := l.session.SendWith(prompt, newestImages(chat)); err != nil {
		l.release()
		return jsonResponse(request, http.StatusServiceUnavailable, errorBody("Claude Code would not take the message: "+err.Error()))
	}
	l.turns++
	reader, writer := io.Pipe()
	go l.stream(request.Context(), writer, events, model, adopted)
	return &http.Response{
		StatusCode: http.StatusOK, Status: "200 OK", Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader, ContentLength: -1, Request: request,
	}, nil
}

// resume continues a turn that handed codeaf tool calls out: the results in
// this request answer them, and Claude Code carries on where it waited. Words
// the person added after the results go in as a correction. Runs with l.turn
// and l.mu held.
func (l *live) resume(request *http.Request, chat chatRequest, model string) (*http.Response, error) {
	lastAssistant := -1
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		if chat.Messages[i].Role == "assistant" {
			lastAssistant = i
			break
		}
	}
	var words []string
	for _, m := range chat.Messages[lastAssistant+1:] {
		switch m.Role {
		case "tool":
			if pc := l.awaiting[m.ToolCallID]; pc != nil {
				pc.result <- claudeagent.ToolResult{Text: contentText(m.Content)}
				delete(l.awaiting, m.ToolCallID)
			}
		case "user":
			if text := strings.TrimSpace(contentText(m.Content)); text != "" {
				words = append(words, text)
			}
		}
	}
	// A CALL CODEAF NEVER ANSWERED IS ANSWERED HERE, so Claude Code is not
	// left waiting on a tool the person stopped.
	for id, pc := range l.awaiting {
		pc.result <- claudeagent.ToolResult{Text: "codeaf did not run this call", IsError: true}
		delete(l.awaiting, id)
	}
	events := l.sink
	l.seen = len(spoken(chat.Messages)) + 1
	l.mu.Unlock()
	if len(words) > 0 {
		if err := l.session.Send(strings.Join(words, "\n\n")); err != nil {
			l.release()
			return jsonResponse(request, http.StatusServiceUnavailable, errorBody("Claude Code would not take the message: "+err.Error()))
		}
	}
	reader, writer := io.Pipe()
	go l.stream(request.Context(), writer, events, model, len(words) > 0)
	return &http.Response{
		StatusCode: http.StatusOK, Status: "200 OK", Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader, ContentLength: -1, Request: request,
	}, nil
}

func (l *live) release() {
	l.mu.Lock()
	l.sink = nil
	l.mu.Unlock()
	l.turn.Unlock()
}

// park leaves a cut turn running for [handoff], for the request that carries
// the correction to adopt, and interrupts it when none comes.
func (l *live) park() {
	l.mu.Lock()
	l.adoptable = true
	l.letGo = time.AfterFunc(handoff, func() {
		l.mu.Lock()
		if !l.adoptable {
			l.mu.Unlock()
			return
		}
		l.adoptable, l.sink = false, nil
		l.mu.Unlock()
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = l.session.Interrupt(stop)
	})
	l.mu.Unlock()
	l.turn.Unlock()
}

// stream writes the turn as server-sent chunks until Claude Code's result.
func (l *live) stream(ctx context.Context, w *io.PipeWriter, events <-chan claudeagent.Event, model string, steered bool) {
	parked := false
	defer func() {
		if !parked {
			l.release()
		}
	}()
	id := fmt.Sprintf("claude-code-%d", time.Now().UnixNano())
	write := func(delta map[string]any, finish string, usage map[string]any) bool {
		choice := map[string]any{"index": 0, "delta": delta}
		if finish != "" {
			choice["finish_reason"] = finish
		}
		chunk := map[string]any{"id": id, "object": "chat.completion.chunk", "model": model, "choices": []any{choice}}
		if usage != nil {
			chunk["usage"] = usage
		}
		line, _ := json.Marshal(chunk)
		_, err := fmt.Fprintf(w, "data: %s\n\n", line)
		return err == nil
	}
	think := func(text string) bool { return write(map[string]any{"reasoning": text}, "", nil) }
	finish := func(content string, usage map[string]any) {
		if content != "" {
			write(map[string]any{"role": "assistant", "content": content}, "", nil)
		}
		write(map[string]any{}, "stop", usage)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		_ = w.Close()
	}
	beat := time.NewTicker(heartbeat)
	defer beat.Stop()
	started := time.Now()
	running, held := "", ""
	// weight is how much the newest call of the turn read: its input,
	// cached or not.
	weight := 0
	// settled fires a while after a result in a turn that took a correction;
	// until then that result waits in last, because the correction may yet
	// start a turn of its own whose result is the real answer.
	var settled <-chan time.Time
	var last json.RawMessage
	answer := func(raw json.RawMessage) {
		var r struct {
			Result string `json:"result"`
			Usage  struct {
				InputTokens         int `json:"input_tokens"`
				OutputTokens        int `json:"output_tokens"`
				CacheReadTokens     int `json:"cache_read_input_tokens"`
				CacheCreationTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(raw, &r)
		in := r.Usage.InputTokens + r.Usage.CacheReadTokens + r.Usage.CacheCreationTokens
		// THE RESULT'S INPUT IS EVERY CALL OF THE TURN ADDED UP: a turn of four
		// steps reads its context four times. codeaf reads a request's input as
		// how full the conversation is, and a sum four times the context made it
		// compact a three-line conversation as though it filled the window. So
		// the input reported is what the turn's LAST call read, which is the
		// conversation's actual weight.
		if weight > 0 {
			in = weight
		}
		finish(strings.TrimSpace(r.Result), map[string]any{"prompt_tokens": in, "completion_tokens": r.Usage.OutputTokens, "total_tokens": in + r.Usage.OutputTokens})
	}
	for {
		select {
		case <-ctx.Done():
			if last == nil {
				parked = true
				l.park()
			}
			_ = w.CloseWithError(ctx.Err())
			return
		case <-settled:
			answer(last)
			return
		case pc := <-l.calls:
			// CLAUDE CODE CALLED A CODEAF TOOL: the call goes back to codeaf's
			// turn loop as the model's tool call, which runs it as it runs any
			// model's, and the turn waits in Claude Code for the result.
			// Calls made together are handed out together.
			batch := []*pendingCall{pc}
			for gathering := time.After(300 * time.Millisecond); gathering != nil; {
				select {
				case more := <-l.calls:
					batch = append(batch, more)
				case <-gathering:
					gathering = nil
				}
			}
			calls := make([]any, 0, len(batch))
			l.mu.Lock()
			for i, call := range batch {
				l.awaiting[call.id] = call
				calls = append(calls, map[string]any{"index": i, "id": call.id, "type": "function",
					"function": map[string]any{"name": call.name, "arguments": string(call.args)}})
			}
			l.mu.Unlock()
			write(map[string]any{"role": "assistant", "tool_calls": calls}, "", nil)
			write(map[string]any{}, "tool_calls", nil)
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			_ = w.Close()
			parked = true
			l.turn.Unlock()
			return
		case <-l.session.Done():
			finish("Claude Code ended before it answered.", nil)
			return
		case <-beat.C:
			if running != "" {
				if !think(fmt.Sprintf("still %s (%s)\n", running, time.Since(started).Round(time.Second))) {
					return
				}
			}
		case ev := <-events:
			if ev.Type == "assistant" {
				settled = nil
			}
			switch ev.Type {
			case "assistant":
				if read := callContext(ev.Raw); read > 0 {
					weight = read
				}
				for _, b := range contentBlocks(ev.Raw) {
					switch b.Type {
					case "text":
						// TEXT IS HELD UNTIL WHAT FOLLOWS IT IS KNOWN: words
						// before a tool call are Claude Code thinking aloud,
						// and words that end the turn are its answer, which
						// arrives once, as the reply.
						held = strings.TrimSpace(b.Text)
					case "tool_use":
						if strings.HasPrefix(b.Name, "mcp__codeaf__") {
							// codeaf draws its own tool's row when the call reaches it.
							continue
						}
						if held != "" {
							if !think(held + "\n") {
								return
							}
							held = ""
						}
						running = b.Name + " " + describe(b.Input)
						started = time.Now()
						if b.ID != "" {
							l.started[b.ID] = startedStep{name: b.Name, input: b.Input, at: started}
						}
						if !think("▸ " + running + "\n") {
							return
						}
					}
				}
			case "user":
				running = ""
				// EACH FINISHED STEP GOES UP AS CODEAF'S OWN CALL, so it is
				// drawn and written down where every tool's is (steps.go).
				var ran []ranStep
				for _, result := range toolResults(ev.Raw) {
					call, ok := l.started[result.id]
					if !ok {
						continue
					}
					delete(l.started, result.id)
					if step, ok := stepFor(result.id, call, result.text, result.isError); ok {
						ran = append(ran, step)
					}
				}
				if len(ran) > 0 && !write(map[string]any{"codeaf_steps": ran}, "", nil) {
					return
				}
			case "result":
				l.keep()
				if steered {
					last, settled = ev.Raw, time.After(settle)
					continue
				}
				answer(ev.Raw)
				return
			}
		}
	}
}

// newestImages are the pictures attached to the person's newest message, as
// codeaf sends them: data URLs in image_url parts. A picture held anywhere
// else (a URL Claude Code would have to fetch) is left out.
func newestImages(chat chatRequest) []claudeagent.Image {
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		if chat.Messages[i].Role != "user" {
			continue
		}
		var parts []struct {
			Type     string `json:"type"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		_ = json.Unmarshal(chat.Messages[i].Content, &parts)
		var images []claudeagent.Image
		for _, part := range parts {
			head, data, ok := strings.Cut(part.ImageURL.URL, ";base64,")
			if part.Type != "image_url" || !ok || !strings.HasPrefix(head, "data:image/") {
				continue
			}
			images = append(images, claudeagent.Image{MediaType: strings.TrimPrefix(head, "data:"), Data: data})
		}
		return images
	}
	return nil
}

// keptSession is what a conversation keeps of its Claude Code session.
type keptSession struct {
	Session string `json:"session"`
	Seen    int    `json:"seen"`
}

func readKept(path string) keptSession {
	var k keptSession
	if path == "" {
		return k
	}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &k)
	}
	return k
}

// keep writes the session's id and how much of the conversation it has seen.
func (l *live) keep() {
	if l.kept == "" {
		return
	}
	l.mu.Lock()
	k := keptSession{Session: l.session.SessionID(), Seen: l.seen}
	l.mu.Unlock()
	if k.Session == "" {
		return
	}
	raw, _ := json.Marshal(k)
	_ = os.WriteFile(l.kept, raw, 0o600)
}

// turnPrompt is what Claude Code is told this turn: the person's newest words,
// after whatever it has not yet been given of the conversation from message
// from on — the whole history on a session's first turn of a conversation
// that started elsewhere, or what another model said and was told while the
// person had moved the conversation off Claude Code and back.
func turnPrompt(chat chatRequest, from int) string {
	// FROM COUNTS WHAT WAS SAID, not Claude Code's own steps codeaf wrote
	// down: those are its session's own history (steps.go).
	messages := spoken(chat.Messages)
	last, lastAnswer := -1, -1
	for i, m := range messages {
		switch m.Role {
		case "user":
			last = i
		case "assistant":
			lastAnswer = i
		}
	}
	if last < 0 {
		return ""
	}
	if from < 0 {
		from = 0
	}
	// WHAT IS NEW IS EVERYTHING IN THE USER ROLE SINCE THE LAST ANSWER, not
	// the last such message alone: codeaf lands its own notes in that role
	// between an answer and the next request (where the work stands, what the
	// crew is doing), and each opens by saying it is a note, not the person.
	// Taking only the last one handed Claude Code a note as "the person now
	// says" and dropped what the person actually typed.
	begin := max(lastAnswer+1, from)
	begin = min(begin, last)
	var fresh []string
	for _, m := range messages[begin:] {
		if m.Role != "user" {
			continue
		}
		if text := strings.TrimSpace(contentText(m.Content)); text != "" {
			fresh = append(fresh, text)
		}
	}
	newest := strings.Join(fresh, "\n\n")
	var earlier []string
	for i := from; i < begin; i++ {
		m := messages[i]
		text := strings.TrimSpace(contentText(m.Content))
		if text == "" || m.Role == "system" || m.Role == "developer" {
			continue
		}
		who := "User"
		if m.Role == "assistant" {
			who = "Assistant"
		} else if m.Role == "tool" {
			who = "Tool result"
		}
		earlier = append(earlier, who+": "+text)
	}
	if len(earlier) == 0 {
		return newest
	}
	return "What was said in this conversation that you have not seen:\n\n" + strings.Join(earlier, "\n\n") + "\n\nNow:\n\n" + newest
}

type contentBlock struct {
	ID    string          `json:"id"`
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// callContext is how many tokens one of Claude Code's calls read, from the
// usage its assistant message carries, or 0 when it says none.
func callContext(raw json.RawMessage) int {
	var line struct {
		Message struct {
			Usage struct {
				InputTokens         int `json:"input_tokens"`
				CacheReadTokens     int `json:"cache_read_input_tokens"`
				CacheCreationTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	_ = json.Unmarshal(raw, &line)
	u := line.Message.Usage
	return u.InputTokens + u.CacheReadTokens + u.CacheCreationTokens
}

func contentBlocks(raw json.RawMessage) []contentBlock {
	var line struct {
		Message struct {
			Content []contentBlock `json:"content"`
		} `json:"message"`
	}
	_ = json.Unmarshal(raw, &line)
	return line.Message.Content
}

func describe(input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	for _, key := range []string{"command", "file_path", "pattern", "path", "url"} {
		if value, ok := in[key].(string); ok && value != "" {
			if len(value) > 120 {
				value = value[:120] + "…"
			}
			return value
		}
	}
	return ""
}
