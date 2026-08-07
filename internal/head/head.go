// Package head turns durable thread messages into immediate conversational
// replies and asynchronous graph commands. It never plans or executes work;
// the thread remains responsive while the rest of Aforge changes the graph.
package head

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/resident"
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

The snapshot IS your workforce, seen live. Every line is one worker's assignment: "running" is a worker doing that thing at this moment, "pending" is work waiting its turn, "done" and "failed" are how assignments ended, and the result field is what came back. Whatever words the user reaches for — workers, agents, employees, tasks, jobs, threads, "what's everyone up to" — they mean these lines, because there is nothing else they could mean: you have no other staff, no hidden status system, no information channel besides this snapshot and the thread. So a question about activity, progress, or who is doing what is never outside your knowledge — it is a read of the snapshot, translated into plain speech.

Alongside the snapshot you carry a notebook: durable lessons, quirks, preferences, and facts distilled from past jobs and conversations. The notebook is your accumulated experience the way the snapshot is your present awareness. Questions about what you know, remember, or have learned are answered from the notebook exactly as status questions are answered from the snapshot; and when a notebook entry changes what you would say — a known quirk of a tool the user is asking about, a preference they stated before — let it shape the reply naturally.

When a competence map appears, it is the measured view of your own current strengths, weak spots, and learning frontier. Treat questions about what you are good at, where you struggle, or what you should practice as status questions: answer directly from that evidence, in first person, and emit no work command. Never claim strength or weakness absent from the map.

Return exactly one JSON object with this shape and no text outside it:
{"reply":"<what to say right now>","command":null,"remember":null,"retract":null}
where command may instead be {"kind":"reflex|splice|amend|cancel|pause|resume|reprioritize|restart","target":"<node id or empty>","instruction":"<the user's instruction, preserving their words verbatim>"}
and remember may instead be {"scope":"<scope>","kind":"preference|fact","body":"<one sharp sentence>"}
and retract may instead be {"seq":123}, naming exactly one numbered notebook line.

Routing law:
- Questions about the state of existing work — what is running, what was found, what happened, what anyone or anything is doing — you answer directly from the graph snapshot, with no command. Before deciding a question is unanswerable, re-read it as a question about the snapshot in different words; it usually is one. "I'm sorry, but" and "I don't have information about" are not sentences you produce — the reply is the state read off the snapshot, a numbered question, or a receipt for spliced work, always.
- Pure conversation — greetings, thanks, acknowledgements — just a reply, no command.
- EVERYTHING else is work for the workforce. Choose reflex only when the request is one obvious action, unambiguous, reversible, and honestly seconds-scale. A reflex still journals and runs one worker; it only skips compilation, planning, and delivery review. Emit the user's own words verbatim in instruction — do not improve, summarize, or reinterpret them.
- Reversibility, not apparent size, is the license for reflex. Anything that spends or transfers money, sends or publishes on the user's behalf, deletes beyond the workspace, or is otherwise hard to reverse is ALWAYS a normal splice, even if it is one tiny action. When scope or consequence is unclear, use splice.
- Use the measured reflex history when it appears below as a prior, never as a hard rule: a high promotion rate argues for splice on similar asks; a high clean-success rate at low cost argues for reflex. The current request and its consequences still decide.
- A reflex is not a synonym for lookup. A quick lookup may be a reflex when it is one reversible retrieval; research, multi-part work, uncertain action sequences, and anything likely to need several independent steps use splice.
- Never refuse and never say you cannot or lack access: you always can, by routing work. A normal splice receives the same verbatim instruction.
- For a redirect of existing work, emit amend and name the affected node id from the snapshot. For stopping work, emit cancel with its target. Never invent a node id; if there is no unambiguous target, explain that briefly and emit no command.
- When the message refers back to earlier work ("it", "the report", "the podcast") and MORE THAN ONE thing in the snapshot plausibly matches, never pick for the user. Reply with one short question listing the candidates as numbered options (1. ..., 2. ...), each identified by what the user would recognise — their own words from that job — and emit no command. Their next message chooses. A single plausible match is not ambiguity; proceed.
- When the user states something durable — a preference about how they like things done, a correction to how something was done for them, a lasting fact about themselves or their environment — capture it in remember as one sharp sentence, alongside whatever reply and command the message otherwise earns. Judge durability by one test: will this still matter after the current conversation is forgotten? Scope it to the narrowest thing it is about: user for personal preferences, tool:<name>, repo:<path>, file:<path>, or domain:<topic> for the rest. Task parameters and one-off details fail the test; remember stays null on almost every message.
- When the user rejects a notebook belief ("forget that", "that's wrong"), set retract to the exact #seq shown beside that belief and leave remember null. Retract only a clearly identified notebook line; if more than one line could be meant, ask one numbered question and leave retract null. Never invent a sequence number. Retraction is reversible, so confirm it plainly without turning it into new work.
- Never hand back a dead end. When something failed, is blocked, or cannot be done as literally asked, the reply pairs that fact with the nearest thing that CAN be done — a retry by another route, a narrower version, an adjacent source — offered as the default you will proceed with, or as numbered choices when the routes genuinely differ. A bare "that failed" or "that is not possible" hands the user a problem; your job is to hand them a decision already made or one crisp choice.

The reply is what the user sees immediately. When splicing, make it a receipt: say you are on it and will report back when it lands. Never imply the work already finished or promise synchronous completion. Be concise and warm. Reply text is plain prose with no markdown headers. Speak entirely in the user's terms — what each piece of work is about and how it is going. Your internals stay backstage: the permanent spine or root is plumbing rather than an assignment and is never worth mentioning, and words like node, splice, snapshot, or raw ids belong to the machinery, not the conversation.`

// Client is the one provider operation the conversational components need.
// Keeping the boundary this small makes both routing and compiling testable
// without a network.
type Client interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// Head tails the durable thread and turns each new user message into one fast
// routing call, one reply, and at most one asynchronous command.
type Head struct {
	client         Client
	store          *store.Store
	knowledge      func() string
	competence     func() string
	dailyBudgetUSD float64
	modalities     interface {
		Supports(string, string, string) bool
	}
	defaultModel string
	dailyRailSet bool
}

// WithImageInput lets the routing head receive durable chat attachments as
// OpenAI-style image parts when its current talk model advertises vision.
func (h *Head) WithImageInput(modalities interface {
	Supports(string, string, string) bool
}, defaultModel string) *Head {
	h.modalities = modalities
	h.defaultModel = defaultModel
	return h
}

// New returns a conversational head backed by graphStore.
func New(client Client, graphStore *store.Store) *Head {
	return &Head{client: client, store: graphStore}
}

// WithSelfKnowledge supplies measured execution history to the routing call.
// Nil and empty values preserve the original prompt exactly.
func (h *Head) WithSelfKnowledge(knowledge func() string) *Head {
	h.knowledge = knowledge
	return h
}

// WithCompetenceMap registers the derived capability view with the head's
// grounding path. It is read only for competence-shaped questions, and its
// structured data is voiced by the head's existing single routing call.
func (h *Head) WithCompetenceMap(competence func() string) *Head {
	h.competence = competence
	return h
}

// WithDailyBudgetUSD lets the head render policy state and consume a pending
// rail question deterministically. Zero is unlimited.
func (h *Head) WithDailyBudgetUSD(amount float64) *Head {
	h.dailyBudgetUSD = amount
	h.dailyRailSet = true
	return h
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
	if handled, err := h.answerAgentQuestion(user); err != nil {
		return fmt.Errorf("serve head: answer agent question: %w", err)
	} else if handled {
		return nil
	}
	if handled, err := h.answerPendingQuestion(user); err != nil {
		return fmt.Errorf("serve head: answer selectable question: %w", err)
	} else if handled {
		return nil
	}
	if raised, err := h.raiseRailFromReply(user); err != nil {
		return fmt.Errorf("serve head: raise daily rail: %w", err)
	} else if raised {
		return nil
	}
	if handled, err := h.manageCharter(user); err != nil {
		return fmt.Errorf("serve head: manage charter: %w", err)
	} else if handled {
		return nil
	}
	if handled, err := h.manageSurgery(user); err != nil {
		return fmt.Errorf("serve head: manage node surgery: %w", err)
	} else if handled {
		return nil
	}

	decision, err := h.route(ctx, user)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return h.postAgent(user.SessionID, providerErrorReply, 0)
	}

	if memory := decision.Remember; memory != nil {
		if body := strings.TrimSpace(memory.Body); body != "" {
			scope := strings.TrimSpace(strings.ToLower(memory.Scope))
			if scope == "" {
				scope = "user"
			}
			kind := store.FactKind(strings.ToLower(strings.TrimSpace(memory.Kind)))
			switch kind {
			case store.FactPreference, store.FactQuirk, store.FactLesson, store.FactPlain:
			default:
				kind = store.FactPreference
			}
			_, _ = h.store.RecordFact(store.RootID, scope, kind, body)
		}
	}

	if retraction := decision.Retract; retraction != nil {
		fact, found, readErr := h.store.FactBySeq(retraction.Seq)
		if readErr != nil || !found || fact.Status != store.FactActive {
			decision.Reply = fmt.Sprintf("I couldn't find an active notebook belief #%d to forget.", retraction.Seq)
		} else if err := h.store.QuarantineFact(retraction.Seq, user.Seq,
			store.FactOriginUser); err != nil {
			decision.Reply = fmt.Sprintf("I couldn't retract notebook belief #%d.", retraction.Seq)
		} else {
			decision.Reply = fmt.Sprintf("Forgot #%d: %s", retraction.Seq, firstLine(fact.Body))
		}
	}

	var commandSeq int64
	if decision.Command != nil {
		kind, reflex, ok := commandKind(decision.Command.Kind)
		if !ok {
			// Validation normally catches this. Keeping the guard at the store
			// membrane prevents a future decoder change from emitting bad work.
			return h.postAgent(user.SessionID, commandErrorReply, 0)
		}
		if !reflex && isSurgeryCommand(kind) {
			return h.resolveSurgery(user, kind, decision.Command.Target, decision.Command.Instruction, false)
		}
		command, requestErr := h.store.RequestCommand(store.Command{
			SessionID:   user.SessionID,
			Kind:        kind,
			Reflex:      reflex,
			Target:      decision.Command.Target,
			Instruction: decision.Command.Instruction,
			Attachments: append([]string(nil), user.Attachments...),
		})
		if requestErr != nil {
			return h.postAgent(user.SessionID, commandErrorReply, 0)
		}
		commandSeq = command.Seq
	}
	return h.postAgent(user.SessionID, decision.Reply, commandSeq)
}

func (h *Head) raiseRailFromReply(user store.Message) (bool, error) {
	if h == nil || h.store == nil || h.dailyBudgetUSD <= 0 || !affirmativeRailReply(user.Body) {
		return false, nil
	}
	rail, pending, err := h.store.PendingDailyRailApproval(h.dailyBudgetUSD, user.SessionID)
	if err != nil || !pending {
		return false, err
	}
	amount := rail.RaiseAmount()
	if err := h.store.RaiseDailyRail(amount, "head:"+user.SessionID); err != nil {
		return false, err
	}
	updated, err := h.store.DailyRailToday(h.dailyBudgetUSD)
	if err != nil {
		return false, err
	}
	reply := fmt.Sprintf("Daily rail raised by $%.2f to $%.2f -- continuing.", amount, updated.Ceiling)
	return true, h.postAgent(user.SessionID, reply, 0)
}

func affirmativeRailReply(body string) bool {
	normalized := strings.ToLower(strings.TrimSpace(body))
	normalized = strings.Trim(normalized, " .,!?:;\t\n\r")
	switch normalized {
	case "y", "yes", "yes please", "continue", "go ahead", "go on", "proceed", "do it", "sure", "ok", "okay":
		return true
	default:
		return false
	}
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

	graphContext := renderGraph(snapshot)
	if h.dailyRailSet {
		if rail, railErr := h.store.DailyRailToday(h.dailyBudgetUSD); railErr == nil {
			line := fmt.Sprintf("today's spend: $%.2f of $%.2f daily rail", rail.Spend, rail.Ceiling)
			if rail.Unlimited {
				line = fmt.Sprintf("today's spend: $%.2f; daily rail unlimited", rail.Spend)
			}
			graphContext = line + "\n" + graphContext
		}
	}
	prompt := "Live graph snapshot:\n" + graphContext +
		"\n\nNotebook (durable memory across jobs and conversations):\n" + renderNotebook(h.store, user.Body) +
		"\n\nRecent thread before this message:\n" + renderThread(recent) +
		"\n\nCurrent user message (verbatim):\n" + user.Body
	if h.knowledge != nil {
		if measured := strings.TrimSpace(h.knowledge()); measured != "" {
			prompt = "Measured execution history (evidence for routing priors):\n" + measured + "\n\n" + prompt
		}
	}
	if h.competence != nil && asksForCompetence(user.Body) {
		if competence := strings.TrimSpace(h.competence()); competence != "" {
			prompt = "Competence map (ground truth for this question):\n" + competence + "\n\n" + prompt
		}
	}
	messages := []ai.Message{
		textMessage("system", resident.VoicePrompt(h.store, headSystemPrompt, user.Body)),
		textMessage("user", prompt),
	}
	if h.supportsImages() && len(user.Attachments) > 0 {
		parts := messages[1].Content
		for _, path := range user.Attachments {
			if part, ok := imageContentPart(path); ok {
				parts = append(parts, part)
			}
		}
		messages[1].Content = parts
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
	if err := decodeJSONObject(raw, &decision); err != nil {
		if raw == "" {
			return routeDecision{}, errors.New("provider returned an empty response")
		}
		return routeDecision{Reply: raw}, nil
	}
	if decision.Command != nil && decision.Command.Kind == routeReflexKind {
		decision.Command.Instruction = user.Body
		decision.enforceConsequences()
	}
	if decision.validate() != nil {
		if raw == "" {
			return routeDecision{}, errors.New("provider returned an empty response")
		}
		return routeDecision{Reply: raw}, nil
	}
	decision.Reply = strings.TrimSpace(decision.Reply)
	return decision, nil
}

func (h *Head) supportsImages() bool {
	if h == nil || h.modalities == nil {
		return false
	}
	model := h.defaultModel
	if current, ok := h.client.(interface{ Model() string }); ok {
		model = current.Model()
	}
	return h.modalities.Supports(model, "input", "image")
}

func imageContentPart(path string) (ai.ContentPart, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	mediaType := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif"}[ext]
	if mediaType == "" {
		return ai.ContentPart{}, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 10<<20 {
		return ai.ContentPart{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ai.ContentPart{}, false
	}
	if len(data) > 10<<20 {
		return ai.ContentPart{}, false
	}
	return ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{
		URL: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data),
	}}, true
}

func asksForCompetence(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	for _, phrase := range []string{
		"what are you good at", "what are you bad at", "where are you strong",
		"where are you weak", "where do you struggle", "your strengths",
		"your weaknesses", "your competence", "competence map",
		"learning frontier", "what should you practice", "what do you struggle",
	} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
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

// notebookContextBytes bounds the memory shown to the router: enough for the
// beliefs that matter to this message, never the whole archive.
const notebookContextBytes = 2000

// renderNotebook blends the two free retrieval layers — BM25 relevance to
// this message, then recency — into a bounded, age-annotated view. The age
// on every line is deliberate: a claim's freshness is part of its evidence.
func renderNotebook(graphStore *store.Store, message string) string {
	if graphStore == nil {
		return "(no notebook)"
	}
	now := time.Now()
	seen := make(map[int64]bool)
	total := 0
	var lines []string
	add := func(facts []store.Fact) {
		for _, fact := range facts {
			if seen[fact.Seq] {
				continue
			}
			seen[fact.Seq] = true
			line := fmt.Sprintf("- #%d [%s · %s · %s] %s", fact.Seq, fact.Scope,
				fact.Kind, store.AgeLabel(fact.Time, now), fact.Body)
			if total+len(line) > notebookContextBytes {
				return
			}
			total += len(line)
			lines = append(lines, line)
		}
	}
	if found, err := graphStore.SearchFacts(store.FactQuery{
		Cues: resident.ExtractCues(message), Terms: message, Limit: 8,
	}); err == nil {
		add(found)
	}
	if recent, err := graphStore.RecentFacts(10); err == nil {
		add(recent)
	}
	if len(lines) == 0 {
		return "(nothing learned yet)"
	}
	return strings.Join(lines, "\n")
}

type routeDecision struct {
	Reply    string           `json:"reply"`
	Command  *routeCommand    `json:"command"`
	Remember *routeMemory     `json:"remember"`
	Retract  *routeRetraction `json:"retract"`
}

// routeMemory is a durable fact the user just stated, captured into the
// notebook at conversation speed rather than waiting for a job to distill it.
type routeMemory struct {
	Scope string `json:"scope"`
	Kind  string `json:"kind"`
	Body  string `json:"body"`
}

type routeRetraction struct {
	Seq int64 `json:"seq"`
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
	if decision.Retract != nil {
		if decision.Retract.Seq <= 0 || decision.Command != nil || decision.Remember != nil {
			return errors.New("invalid or conflicting retraction")
		}
		return nil
	}
	if decision.Command == nil {
		return nil
	}
	_, reflex, ok := commandKind(decision.Command.Kind)
	if !ok {
		return fmt.Errorf("unknown command kind %q", decision.Command.Kind)
	}
	if strings.TrimSpace(decision.Command.Instruction) == "" {
		return errors.New("empty command instruction")
	}
	if reflex && consequenceGated(decision.Command.Instruction) {
		return errors.New("consequential instruction cannot use reflex")
	}
	if decision.Command.Kind != string(store.CommandSplice) && !reflex && strings.TrimSpace(decision.Command.Target) == "" {
		return fmt.Errorf("%s command has no target", decision.Command.Kind)
	}
	if reflex && strings.TrimSpace(decision.Command.Target) != "" {
		return errors.New("reflex command cannot target existing work")
	}
	return nil
}

const routeReflexKind = "reflex"

func commandKind(kind string) (store.CommandKind, bool, bool) {
	switch kind {
	case routeReflexKind:
		return store.CommandSplice, true, true
	case string(store.CommandSplice):
		return store.CommandSplice, false, true
	case string(store.CommandAmend):
		return store.CommandAmend, false, true
	case string(store.CommandCancel):
		return store.CommandCancel, false, true
	case string(store.CommandPause):
		return store.CommandPause, false, true
	case string(store.CommandResume):
		return store.CommandResume, false, true
	case string(store.CommandReprioritize):
		return store.CommandReprioritize, false, true
	case string(store.CommandRestart):
		return store.CommandRestart, false, true
	default:
		return "", false, false
	}
}

func (decision *routeDecision) enforceConsequences() {
	if decision.Command != nil && decision.Command.Kind == routeReflexKind &&
		consequenceGated(decision.Command.Instruction) {
		decision.Command.Kind = string(store.CommandSplice)
	}
}

// consequenceGated is a safety membrane, not a triviality classifier. It names
// only irreversible effect families; everything about how small or obvious an
// action is remains a learned model judgment.
func consequenceGated(instruction string) bool {
	lower := strings.ToLower(instruction)
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	contains := func(candidates ...string) bool {
		for _, word := range words {
			for _, candidate := range candidates {
				if word == candidate {
					return true
				}
			}
		}
		return false
	}
	if contains("buy", "purchase", "pay", "spend", "transfer", "donate", "subscribe", "order", "refund") {
		return true
	}
	if contains("publish", "post", "tweet", "email", "send", "deploy", "release", "push", "merge") {
		return true
	}
	if !contains("delete", "remove", "erase", "wipe", "destroy", "drop") {
		return false
	}
	if strings.Contains(lower, "outside the workspace") ||
		contains("account", "database", "production", "remote", "cloud", "system") {
		return true
	}
	for _, field := range strings.Fields(lower) {
		field = strings.Trim(field, `"'(),;:`)
		if strings.HasPrefix(field, "/") || strings.HasPrefix(field, "~/") {
			return true
		}
	}
	return false
}

func renderGraph(snapshot store.Snapshot) string {
	if len(snapshot.Nodes) == 0 {
		return "(no active nodes)"
	}
	// Live work first, then the freshest history. The snapshot budget is
	// finite and the store returns creation order, so a long-lived graph fed
	// the head months of settled nodes before the job running right now —
	// which is how the head answered "I don't see anything labeled webgpu"
	// about a subtree the rail was rendering 49 minutes into its run. The
	// one thing the snapshot must never truncate away is what is happening.
	nodes := append([]store.Node(nil), snapshot.Nodes...)
	rank := func(node store.Node) int {
		switch {
		case node.Status == store.Running || node.Status == store.Claimed:
			return 0
		case node.Status == store.Pending:
			return 1
		case node.FoldRoot:
			// Packed history: reachable, but last in line for the budget.
			return 3
		default:
			return 2
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		ri, rj := rank(nodes[i]), rank(nodes[j])
		if ri != rj {
			return ri < rj
		}
		if ri >= 2 {
			return nodes[i].FinishedAt.After(nodes[j].FinishedAt)
		}
		return nodes[i].CreatedSeq > nodes[j].CreatedSeq
	})
	var rendered strings.Builder
	for _, node := range nodes {
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
