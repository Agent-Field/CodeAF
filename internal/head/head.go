// Package head turns durable thread messages into immediate conversational
// replies and asynchronous graph commands. It never plans or executes work;
// the thread remains responsive while the rest of Aforge changes the graph.
package head

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	pollInterval          = 400 * time.Millisecond
	messagePageSize       = 200
	recentMessageLimit    = 10
	maxGraphContextBytes  = 4 << 10
	maxThreadContextBytes = 4 << 10
	providerErrorReply    = "hit a provider error answering that — try again"
	commandErrorReply     = "I couldn't queue that change — try again"
)

// headSystemPrompt is deliberately a router prompt, not a planning prompt. Its
// job ends when the user has an answer and, when needed, a durable command
// receipt; the reconciler owns every graph mutation after that boundary.
const headSystemPrompt = `You are the front desk of a task-graph agent. Behind you is a workforce that can search the web, run code, read and write files, and work on anything for minutes at a time. You yourself do no work and know nothing about the world beyond the graph snapshot — you only route, and you answer instantly.

Return exactly one JSON object with this shape and no text outside it:
{"reply":"<what to say right now>","command":null}
or
{"reply":"<what to say right now>","command":{"kind":"splice|amend|cancel","target":"<node id or empty>","instruction":"<the user's instruction, preserving their words verbatim>"}}

Routing law:
- Questions about the state of existing work — what is running, what was found, what happened — you answer directly from the graph snapshot, with no command.
- Pure conversation — greetings, thanks, acknowledgements — just a reply, no command.
- EVERYTHING else is work for the workforce: a fact you do not have (weather, prices, news, anything about the world), research, code, files, any task at all. Emit a splice command with the user's own words verbatim in instruction — do not improve, summarize, or reinterpret them. Never refuse and never say you cannot or lack access: you always can, by splicing. A quick lookup is still a splice.
- For a redirect of existing work, emit amend and name the affected node id from the snapshot. For stopping work, emit cancel with its target. Never invent a node id; if there is no unambiguous target, explain that briefly and emit no command.
- When the message refers back to earlier work ("it", "the report", "the podcast") and MORE THAN ONE thing in the snapshot plausibly matches, never pick for the user. Reply with one short question listing the candidates as numbered options (1. ..., 2. ...), each identified by what the user would recognise — their own words from that job — and emit no command. Their next message chooses. A single plausible match is not ambiguity; proceed.

The reply is what the user sees immediately. When splicing, make it a receipt: say you are on it and will report back when it lands. Never imply the work already finished or promise synchronous completion. Be concise and warm. Reply text is plain prose with no markdown headers.`

// Client is the one provider operation the conversational components need.
// Keeping the boundary this small makes both routing and compiling testable
// without a network.
type Client interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// Head tails the durable thread and turns each new user message into one fast
// routing call, one reply, and at most one asynchronous command.
type Head struct {
	client Client
	store  *store.Store
}

// New returns a conversational head backed by graphStore.
func New(client Client, graphStore *store.Store) *Head {
	return &Head{client: client, store: graphStore}
}

// Serve tails every session until ctx is cancelled.
//
// On startup it resumes at the last non-user message. This intentionally
// replays only a trailing run of user messages: history ending in an agent or
// system message is treated as answered, while a crash after the user wrote but
// before the head replied remains recoverable. It is a deliberately simple
// journal rule; the cursor advances only after each message has been handled.
func (h *Head) Serve(ctx context.Context) error {
	if h == nil || h.client == nil {
		return errors.New("serve head: nil client")
	}
	if h.store == nil {
		return errors.New("serve head: nil store")
	}

	cursor, err := h.initialCursor()
	if err != nil {
		return fmt.Errorf("serve head: initialize cursor: %w", err)
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		cursor, err = h.poll(ctx, cursor)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (h *Head) initialCursor() (int64, error) {
	var scanned, lastNonUser int64
	for {
		messages, err := h.store.Messages("", scanned, messagePageSize)
		if err != nil {
			return 0, err
		}
		if len(messages) == 0 {
			return lastNonUser, nil
		}
		for _, message := range messages {
			scanned = message.Seq
			if message.Role != store.RoleUser {
				lastNonUser = message.Seq
			}
		}
	}
}

func (h *Head) poll(ctx context.Context, cursor int64) (int64, error) {
	for {
		if err := ctx.Err(); err != nil {
			return cursor, err
		}
		messages, err := h.store.Messages("", cursor, messagePageSize)
		if err != nil {
			return cursor, fmt.Errorf("serve head: tail messages: %w", err)
		}
		if len(messages) == 0 {
			return cursor, nil
		}
		for _, message := range messages {
			// A user message anchored to a node is mid-flight steering for
			// that worker, not a new ask — the executor consumes it between
			// turns and the head stays out of the way.
			if message.Role == store.RoleUser && message.NodeID == "" {
				if err := h.answer(ctx, message); err != nil {
					return cursor, err
				}
			}
			cursor = message.Seq
		}
	}
}

func (h *Head) answer(ctx context.Context, user store.Message) error {
	decision, err := h.route(ctx, user)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return h.postAgent(user.SessionID, providerErrorReply, 0)
	}

	var commandSeq int64
	if decision.Command != nil {
		kind, ok := commandKind(decision.Command.Kind)
		if !ok {
			// Validation normally catches this. Keeping the guard at the store
			// membrane prevents a future decoder change from emitting bad work.
			return h.postAgent(user.SessionID, commandErrorReply, 0)
		}
		command, requestErr := h.store.RequestCommand(store.Command{
			SessionID:   user.SessionID,
			Kind:        kind,
			Target:      decision.Command.Target,
			Instruction: decision.Command.Instruction,
		})
		if requestErr != nil {
			return h.postAgent(user.SessionID, commandErrorReply, 0)
		}
		commandSeq = command.Seq
	}
	return h.postAgent(user.SessionID, decision.Reply, commandSeq)
}

func (h *Head) route(ctx context.Context, user store.Message) (routeDecision, error) {
	snapshot, err := h.store.ActiveSnapshot()
	if err != nil {
		return routeDecision{}, fmt.Errorf("read active graph: %w", err)
	}
	recent, err := h.recentThread(user.SessionID, user.Seq)
	if err != nil {
		return routeDecision{}, fmt.Errorf("read recent thread: %w", err)
	}

	prompt := "Live graph snapshot:\n" + renderGraph(snapshot) +
		"\n\nRecent thread before this message:\n" + renderThread(recent) +
		"\n\nCurrent user message (verbatim):\n" + user.Body
	messages := []ai.Message{
		textMessage("system", headSystemPrompt),
		textMessage("user", prompt),
	}
	// No response-format schema here: measured against the shipped default
	// model, schema-constrained calls came back empty two times in three and
	// took 4-6s, while prompt-shaped JSON parsed three of three at under a
	// second. The defensive decoder below covers the difference.
	raw := ""
	for attempt := 0; attempt < 2; attempt++ {
		response, err := h.client.CompleteWithMessages(ctx, messages, ai.WithMaxTokens(600))
		if err != nil {
			// One transient failure should not surface as "try again" — the
			// user already tried. Retry once; only a repeat offense escapes.
			if attempt == 0 && ctx.Err() == nil {
				select {
				case <-ctx.Done():
					return routeDecision{}, ctx.Err()
				case <-time.After(400 * time.Millisecond):
				}
				continue
			}
			return routeDecision{}, err
		}
		if response == nil {
			return routeDecision{}, errors.New("provider returned a nil response")
		}
		raw = strings.TrimSpace(response.Text())
		if raw != "" {
			break
		}
	}
	var decision routeDecision
	if err := decodeJSONObject(raw, &decision); err != nil || decision.validate() != nil {
		if raw == "" {
			return routeDecision{}, errors.New("provider returned an empty response")
		}
		return routeDecision{Reply: raw}, nil
	}
	decision.Reply = strings.TrimSpace(decision.Reply)
	return decision, nil
}

func (h *Head) recentThread(sessionID string, beforeSeq int64) ([]store.Message, error) {
	recent := make([]store.Message, 0, recentMessageLimit)
	var cursor int64
	for {
		messages, err := h.store.Messages(sessionID, cursor, messagePageSize)
		if err != nil {
			return nil, err
		}
		if len(messages) == 0 {
			return recent, nil
		}
		for _, message := range messages {
			cursor = message.Seq
			if message.Seq >= beforeSeq {
				return recent, nil
			}
			if len(recent) == recentMessageLimit {
				copy(recent, recent[1:])
				recent[len(recent)-1] = message
			} else {
				recent = append(recent, message)
			}
		}
	}
}

func (h *Head) postAgent(sessionID, body string, commandSeq int64) error {
	_, err := h.store.PostMessage(store.Message{
		SessionID:  sessionID,
		Role:       store.RoleAgent,
		Body:       body,
		CommandSeq: commandSeq,
	})
	if err != nil {
		return fmt.Errorf("serve head: post reply: %w", err)
	}
	return nil
}

type routeDecision struct {
	Reply   string        `json:"reply"`
	Command *routeCommand `json:"command"`
}

type routeCommand struct {
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Instruction string `json:"instruction"`
}

func (decision routeDecision) validate() error {
	if strings.TrimSpace(decision.Reply) == "" {
		return errors.New("empty reply")
	}
	if decision.Command == nil {
		return nil
	}
	if _, ok := commandKind(decision.Command.Kind); !ok {
		return fmt.Errorf("unknown command kind %q", decision.Command.Kind)
	}
	if strings.TrimSpace(decision.Command.Instruction) == "" {
		return errors.New("empty command instruction")
	}
	if decision.Command.Kind != string(store.CommandSplice) && strings.TrimSpace(decision.Command.Target) == "" {
		return fmt.Errorf("%s command has no target", decision.Command.Kind)
	}
	return nil
}

func commandKind(kind string) (store.CommandKind, bool) {
	switch kind {
	case string(store.CommandSplice):
		return store.CommandSplice, true
	case string(store.CommandAmend):
		return store.CommandAmend, true
	case string(store.CommandCancel):
		return store.CommandCancel, true
	default:
		return "", false
	}
}

func renderGraph(snapshot store.Snapshot) string {
	if len(snapshot.Nodes) == 0 {
		return "(no active nodes)"
	}
	var rendered strings.Builder
	for _, node := range snapshot.Nodes {
		brief := firstLine(node.Brief)
		if brief == "" {
			brief = "(no brief)"
		}
		// A settled node's first result line is what "what did you find?"
		// gets answered from; without it the head can only recite statuses.
		result := firstLine(node.Summary)
		if result == "" {
			result = firstLine(node.FoldDigest)
		}
		line := fmt.Sprintf("- %s | %s | %s", node.ID, node.Status, brief)
		if result != "" {
			line += " | result: " + result
		}
		line += "\n"
		if rendered.Len()+len(line) > maxGraphContextBytes {
			rendered.WriteString("(snapshot truncated)\n")
			break
		}
		rendered.WriteString(line)
	}
	return strings.TrimSpace(rendered.String())
}

func renderThread(messages []store.Message) string {
	if len(messages) == 0 {
		return "(no earlier messages in this session)"
	}
	var rendered strings.Builder
	for _, message := range messages {
		body := truncateBytes(strings.TrimSpace(message.Body), 600)
		body = strings.ReplaceAll(body, "\n", "\n  ")
		line := fmt.Sprintf("%s: %s\n", message.Role, body)
		if rendered.Len()+len(line) > maxThreadContextBytes {
			rendered.WriteString("(thread context truncated)\n")
			break
		}
		rendered.WriteString(line)
	}
	return strings.TrimSpace(rendered.String())
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexAny(value, "\r\n"); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

func truncateBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit - len("…")
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut] + "…"
}

func textMessage(role, body string) ai.Message {
	return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: body}}}
}
