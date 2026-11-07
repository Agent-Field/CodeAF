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
	"Pushing, publishing and opening pull requests are the person's to do: say what to run instead."

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
}

var (
	livesMu sync.Mutex
	lives   = map[string]*live{}
)

func liveKey(dir, conversation string) string { return dir + "\x00" + conversation }

// liveFor answers the session for this conversation, starting one when there
// is none or the last one has ended.
func liveFor(ctx context.Context, dir, conversation, model, kept string) (*live, error) {
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
	l := &live{model: model, kept: kept}
	options := claudeagent.Options{
		Binary: binary, Dir: dir, Model: model, PermissionMode: "acceptEdits",
		AppendSystemPrompt: seated, Disallowed: outwardRules, Stderr: os.Stderr,
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
	l, err := liveFor(request.Context(), dir, conversation, model, t.kept)
	if err != nil {
		return jsonResponse(request, http.StatusServiceUnavailable, errorBody(err.Error()))
	}
	l.turn.Lock()
	l.mu.Lock()
	events, adopted := l.sink, l.adoptable
	// A TURN THAT IS ADOPTED HAS SEEN EVERYTHING BUT THE CORRECTION, so only
	// the newest words go in; a fresh session is given the whole history.
	from := l.seen
	switch {
	case adopted:
		from = len(chat.Messages)
	case l.turns == 0:
		from = 0
	}
	prompt := turnPrompt(chat, from)
	if prompt == "" {
		l.mu.Unlock()
		l.turn.Unlock()
		return jsonResponse(request, http.StatusBadRequest, errorBody("there is nothing new to say to Claude Code"))
	}
	l.seen = len(chat.Messages) + 1
	if adopted {
		l.letGo.Stop()
		l.adoptable = false
	} else {
		events = make(chan claudeagent.Event, 256)
		l.sink = events
	}
	l.mu.Unlock()
	if err := l.session.Send(prompt); err != nil {
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
				for _, b := range contentBlocks(ev.Raw) {
					switch b.Type {
					case "text":
						// TEXT IS HELD UNTIL WHAT FOLLOWS IT IS KNOWN: words
						// before a tool call are Claude Code thinking aloud,
						// and words that end the turn are its answer, which
						// arrives once, as the reply.
						held = strings.TrimSpace(b.Text)
					case "tool_use":
						if held != "" {
							if !think(held + "\n") {
								return
							}
							held = ""
						}
						running = b.Name + " " + describe(b.Input)
						started = time.Now()
						if !think("▸ " + running + "\n") {
							return
						}
					}
				}
			case "user":
				running = ""
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
	last := -1
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		if chat.Messages[i].Role == "user" {
			last = i
			break
		}
	}
	if last < 0 {
		return ""
	}
	newest := strings.TrimSpace(contentText(chat.Messages[last].Content))
	if from < 0 {
		from = 0
	}
	var earlier []string
	for i := from; i < last; i++ {
		m := chat.Messages[i]
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
	return "What was said in this conversation that you have not seen:\n\n" + strings.Join(earlier, "\n\n") + "\n\nThe person now says:\n\n" + newest
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
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
