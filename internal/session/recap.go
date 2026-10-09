package session

// The account of a conversation that History lists.
//
// A person coming back to old conversations is not asking "which one had nine
// messages". They are asking "which one decided the strict-mode question", and
// a title cannot say that: it names the topic, never what came of it. So a
// conversation writes a RECAP of itself — one sentence for a list row, a short
// account of what was discussed, what was decided and by whom, and what came of
// it — and keeps it beside its title in meta.json, where a list of every
// conversation on the machine can read it without opening a single transcript.
//
// The design is the title errand's (title.go), copied on purpose because it is
// the same job with the same constraints:
//
//   - IT RUNS WHEN A TURN SETTLES AND NEVER BEFORE. The errand starts from the
//     door every turn shape already ends through ([Agent.maybeTitle]), on a
//     goroutine of the session's lifetime, so nothing in a turn waits on it and
//     a failure says nothing. There is no event kind for "a small thing did not
//     work", and borrowing EventError would report a fault about work nobody
//     asked for.
//
//   - IT IS PAID FOR ONLY WHEN THE CONVERSATION CHANGED. The recap carries a
//     FINGERPRINT of what it summarised — how many messages, and a hash of the
//     last one. A turn that settles on the same conversation (a wake that found
//     nothing to say, a resume of a conversation that was already summarised)
//     compares fingerprints and buys nothing. ONE SHORT CALL PER CHANGED
//     CONVERSATION is the whole bill.
//
//   - THE INPUT IS BOUNDED. Every message is clipped and only the first and the
//     last [recapTail] are sent, so the cost of a recap stays the same however
//     long the conversation runs; the first message is kept because it says
//     what the conversation was for.
//
//   - THE CHEAP MODEL, THROUGH THE ROLE LADDER. [roles.RoleRecap] is on the low
//     tier, owned on the desktop by the "Titles and summaries" role, and its
//     spend is billed to the session's auxiliary ledger the moment it lands and
//     never inside a turn's own sealed figure ([Agent.addDetachedUsageAs]).
//
//   - ONLY A DOOR THAT SHOWS RECAPS ASKS FOR THEM ([Config.Recaps]). The terminal
//     lists conversations by title and opening words; paying for a summary
//     nobody there would read is a tax on every turn. The desktop sets it.
//
//   - IT IS ONLY EVER WRITTEN THROUGH [Agent.updateMeta], so the patch owns the
//     recap and nothing else: a title or a spend stamp landing between the read
//     and the rename is not clobbered, and the recap is not clobbered by them.
//
//   - THE CHANGED FILES ARE FACTS AND NOT THE MODEL'S. Paths and line counts come
//     from the edit and write calls the transcript holds (the arithmetic
//     [editDiffstat] already does for `/why`), so a recap never invents a number
//     or a path. A write adds its line count only for a file this session
//     created, because for a file that already existed the lines it replaced
//     are unknowable from the call; every other write is a path with zero.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// The recap writer is a ROLE registered from the file that makes the call. LOW
// is deliberate: a wrong recap costs a glance, the conversation is one click
// away, and no decision downstream is made from these words.
func init() { roles.Register(roles.RoleRecap, roles.TierLow) }

// auxRoleRecap is the journal tag for this errand, for the reason the title's
// is: a recap is something a person reads, and a bad one is unattributable
// without the model that wrote it.
const auxRoleRecap = "recap"

const (
	// recapClip bounds each message handed to the writer. A recap is derived
	// from what was said, and the first paragraphs say it; a 300KB tool-assisted
	// reply would pay for a whole context to produce one sentence.
	recapClip = 1200
	// recapTail is how many of the newest messages are sent beside the first.
	recapTail = 40
	// recapAskWindow bounds the one ask. The role tier bounds a call; this
	// bounds the errand, so a slow rung cannot hold the session's goroutine.
	recapAskWindow = 60 * time.Second

	recapLineLimit      = 220
	recapDiscussedLimit = 700
	recapDecisionLimit  = 240
	recapOutcomeLimit   = 300
	recapHowLimit       = 24
	recapDecisions      = 8
	// recapFilesKept bounds the changed files stored with a recap. The list a
	// person sees shows three; the count and the search want the rest.
	recapFilesKept = 60
)

// RecapDecision is one thing a conversation settled, and who settled it.
type RecapDecision struct {
	Text string `json:"text"`
	// By is "you" when the person chose it, "codeaf" when codeaf did, and empty
	// when the writer did not say rather than a guess dressed as a fact.
	By  string `json:"by,omitempty"`
	How string `json:"how,omitempty"`
}

// RecapFile is one file the conversation changed, with the lines it added and
// removed where the transcript can say so.
type RecapFile struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

// ConversationRecap is what meta.json keeps of a conversation's recap.
type ConversationRecap struct {
	// Line is the one sentence a list row shows.
	Line      string          `json:"line"`
	Discussed string          `json:"discussed,omitempty"`
	Decided   []RecapDecision `json:"decided,omitempty"`
	Outcome   string          `json:"outcome,omitempty"`
	Files     []RecapFile     `json:"files,omitempty"`
	// UpdatedAt is when the recap was written and Messages how many messages it
	// summarised, so a reader can say the conversation has moved on since.
	UpdatedAt time.Time `json:"updatedAt"`
	Messages  int       `json:"messages"`
	// Fingerprint is the cheap identity of what was summarised: the message count
	// and a hash of the last message. Equal fingerprints mean nothing new to say.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// recapTurn is one message of the conversation as the writer reads it.
type recapTurn struct {
	Role string
	Text string
}

// recapJob is everything the errand needs, copied out from under the agent's
// lock so the goroutine reads no live state.
type recapJob struct {
	turns       []recapTurn
	files       []RecapFile
	fingerprint string
	model       string
	dir         string
}

// startRecapLocked starts the session writing a recap of itself, if there is
// anything new to say. It runs from [Agent.maybeTitle] with a.mu held.
//
// The attempt is remembered by fingerprint BEFORE the errand starts, so two
// doors ending one turn, or a failed call and the next settle on an unchanged
// conversation, never buy a second call for the same words.
func (a *Agent) startRecapLocked() {
	if !a.config.Recaps || a.file == nil || a.closed || a.titleCtx == nil || a.recapBusy {
		return
	}
	// A TASK NODE SUMMARISES NOTHING, for the namer's reason: History lists
	// conversations, and a node is a step of one whose journal is a record.
	if a.config.InTask || strings.TrimSpace(a.config.Place.Dir) == "" {
		return
	}
	turns, files := a.recapTurnsLocked()
	if !hasAnswer(turns) {
		return
	}
	fingerprint := recapFingerprint(turns)
	if fingerprint == a.recapSeen {
		return
	}
	a.recapSeen, a.recapBusy = fingerprint, true
	job := recapJob{turns: turns, files: files, fingerprint: fingerprint, model: a.model, dir: a.config.Place.Dir}
	ctx := a.titleCtx
	a.titleJobs.Add(1)
	go func() {
		defer a.titleJobs.Done()
		a.writeRecap(ctx, job)
		a.mu.Lock()
		a.recapBusy = false
		// THE CONVERSATION MAY HAVE MOVED WHILE THE CALL WAS OUT, and the settle
		// that moved it was refused by the busy mark above. Asking again here is
		// bounded by the fingerprint: it fires only for words not yet summarised.
		a.startRecapLocked()
		a.mu.Unlock()
	}()
}

// hasAnswer reports that the assistant has said something, because a
// conversation with no answer yet has nothing to recap.
func hasAnswer(turns []recapTurn) bool {
	for _, turn := range turns {
		if turn.Role == "assistant" {
			return true
		}
	}
	return false
}

// recapFingerprint is "count:hash-of-the-last-message". Cheap to compare and
// stable across a resume, which is what lets a reopened conversation skip the
// call.
func recapFingerprint(turns []recapTurn) string {
	if len(turns) == 0 {
		return ""
	}
	last := turns[len(turns)-1]
	sum := sha256.Sum256([]byte(last.Role + "\x00" + last.Text))
	return fmt.Sprintf("%d:%s", len(turns), hex.EncodeToString(sum[:6]))
}

// recapTurnsLocked reads the live transcript as the person would read it back:
// their words and the assistant's, without tool traffic or the session's own
// notes. It also returns the files the edit and write calls changed.
func (a *Agent) recapTurnsLocked() ([]recapTurn, []RecapFile) {
	results := map[string]string{}
	for _, message := range a.messages {
		if message.Role == "tool" && message.ToolCallID != "" {
			results[message.ToolCallID] = messageContentText(message)
		}
	}
	var turns []recapTurn
	changes := newRecapFiles()
	for _, message := range a.messages {
		switch message.Role {
		case "user":
			if a.sessionNoteLocked(message) {
				continue
			}
			if text := strings.TrimSpace(a.presentation.personWords(message)); text != "" {
				turns = append(turns, recapTurn{Role: "user", Text: text})
			}
		case "assistant":
			if humanShellReply(message) {
				continue
			}
			for _, call := range message.ToolCalls {
				changes.note(call, results[call.ID], a.createdLocked, a.config.Workspace)
			}
			if text := strings.TrimSpace(messageContentText(message)); text != "" {
				turns = append(turns, recapTurn{Role: "assistant", Text: text})
			}
		}
	}
	return turns, changes.list()
}

// createdLocked reports whether this session made the file, which is the only
// case where a write's whole content is the file's added lines.
func (a *Agent) createdLocked(path string) bool {
	for _, known := range a.createdFiles {
		if known.path == path {
			return true
		}
	}
	return false
}

// recapFiles accumulates the changed files in the order they first changed.
type recapFiles struct {
	order []string
	by    map[string]*RecapFile
}

func newRecapFiles() *recapFiles { return &recapFiles{by: map[string]*RecapFile{}} }

// note folds one assistant tool call in. A call whose result is missing or is
// not the tool's own success sentence changed nothing and counts for nothing —
// the same reading `/why` makes ([whyPhrase]).
func (f *recapFiles) note(call ai.ToolCall, result string, created func(string) bool, workspace string) {
	name := call.Function.Name
	if name != "edit" && name != "write" {
		return
	}
	path := strings.TrimSpace(whyArgumentsOf(call).String("path"))
	if path == "" {
		return
	}
	if name == "edit" && !strings.HasPrefix(result, "Successfully replaced") {
		return
	}
	if name == "write" && !strings.HasPrefix(result, "Successfully wrote") &&
		!strings.HasPrefix(result, "Appended") && !strings.HasPrefix(result, "Saved what arrived") {
		return
	}
	file := f.by[path]
	if file == nil {
		file = &RecapFile{Path: path}
		f.by[path] = file
		f.order = append(f.order, path)
	}
	if name == "edit" {
		added, removed, _ := editDiffstat(call)
		file.Added += added
		file.Removed += removed
		return
	}
	absolute := path
	if !filepath.IsAbs(absolute) && workspace != "" {
		absolute = filepath.Join(workspace, path)
	}
	if created(absolute) || created(path) {
		var parsed struct {
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(call.Function.Arguments), &parsed) == nil {
			file.Added += lineCount(parsed.Content)
		}
	}
}

func (f *recapFiles) list() []RecapFile {
	out := make([]RecapFile, 0, len(f.order))
	for _, path := range f.order {
		out = append(out, *f.by[path])
		if len(out) == recapFilesKept {
			break
		}
	}
	return out
}

// recapSystem is character only. A cheap model reads the end of the user message
// as the thing to do, so [recapPrompt] goes there and goes last.
const recapSystem = "You write short recaps of conversations."

// recapPrompt asks for one JSON object. The examples are of the SHAPE of a good
// line — a thing settled or found, or what was discussed and left open — and
// the instruction names the two failures a cheap model reaches for, a count and
// a duration, because those are what a title-like summary degrades into.
const recapPrompt = "Write a recap of this conversation for a history list. Answer with one JSON object and nothing else, with these keys:\n" +
	"\"line\": one sentence of substance for a list row, at most 25 words. Say what was decided or found, or what was discussed and left open. " +
	"Examples: \"Decided to keep strict mode as the default and fix it in the lexer\" · \"Ruled out JSON5, because it also allows comments\" · \"Discussed Load vs Open. No decision yet\". " +
	"Never a count of messages and never how long anything took.\n" +
	"\"discussed\": two or three sentences on what the conversation covered.\n" +
	"\"decided\": a list of {\"text\": what was decided, \"by\": \"you\" when the person chose it or \"codeaf\" when codeaf chose it, \"how\": one optional word such as \"accepted\"}. An empty list when nothing was decided.\n" +
	"\"outcome\": what came of it, in one sentence."

// recapAsk is the user message the writer reads: the conversation window, then
// the instruction.
func recapAsk(turns []recapTurn, files []RecapFile) string {
	var ask strings.Builder
	ask.WriteString("Conversation:\n")
	write := func(turn recapTurn) {
		who := "person"
		if turn.Role == "assistant" {
			who = "codeaf"
		}
		ask.WriteString(who + ": " + clip(turn.Text, recapClip) + "\n\n")
	}
	if len(turns) > recapTail+1 {
		write(turns[0])
		fmt.Fprintf(&ask, "[%d earlier messages left out]\n\n", len(turns)-recapTail-1)
		turns = turns[len(turns)-recapTail:]
	}
	for _, turn := range turns {
		write(turn)
	}
	if len(files) > 0 {
		ask.WriteString("Files changed:")
		for i, file := range files {
			if i == 20 {
				break
			}
			ask.WriteString(" " + file.Path)
		}
		ask.WriteString("\n\n")
	}
	return ask.String() + recapPrompt
}

// writeRecap is the errand: skip if the stored recap already covers this
// conversation, ask once through the ladder, and keep the answer.
func (a *Agent) writeRecap(ctx context.Context, job recapJob) {
	if stored, err := LoadMeta(job.dir); err == nil && stored.Recap != nil && stored.Recap.Fingerprint == job.fingerprint {
		return
	}
	ctx, done := context.WithTimeout(ctx, recapAskWindow)
	defer done()
	response, named, err := a.callRoleChecked(withDetachedUsage(ctx), roles.RoleRecap, job.model,
		[]ai.Message{
			textMessage("system", recapSystem),
			textMessage("user", recapAsk(job.turns, job.files)),
		}, func(response *ai.Response, named string) bool {
			if _, ok := parseRecap(response.Text()); ok {
				return true
			}
			a.addDetachedUsageAs(response, named, 1, auxRoleRecap)
			return false
		})
	if err != nil || response == nil || ctx.Err() != nil {
		return
	}
	a.addDetachedUsageAs(response, named, 1, auxRoleRecap)
	recap, ok := parseRecap(response.Text())
	if !ok {
		return
	}
	recap.Files = job.files
	recap.Messages = len(job.turns)
	recap.Fingerprint = job.fingerprint
	recap.UpdatedAt = time.Now()
	dir, snapshot := a.metaSnapshotAt()
	if strings.TrimSpace(dir) == "" {
		return
	}
	a.updateMeta(dir, snapshot, func(meta *Meta) { meta.Recap = &recap })
}

// notAnAccount is the shape a cheap model's line degrades into when it
// summarises the conversation's size instead of its content.
var notAnAccount = regexp.MustCompile(`(?i)^(worked\b|\d+\s+(messages?|turns?|steps?)\b)`)

// parseRecap reads the writer's answer tolerantly: a code fence, a sentence of
// throat-clearing, a decision given as a bare string, and fields of the wrong
// length are all survivable. An answer with no usable line is no answer — the
// line is what the list shows, and a recap without one would be a blank row.
func parseRecap(raw string) (ConversationRecap, bool) {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return ConversationRecap{}, false
	}
	var parsed struct {
		Line      string            `json:"line"`
		Discussed string            `json:"discussed"`
		Decided   []json.RawMessage `json:"decided"`
		Outcome   string            `json:"outcome"`
	}
	if json.Unmarshal([]byte(raw[start:end+1]), &parsed) != nil {
		return ConversationRecap{}, false
	}
	recap := ConversationRecap{
		Line:      clip(oneLine(parsed.Line), recapLineLimit),
		Discussed: clip(oneLine(parsed.Discussed), recapDiscussedLimit),
		Outcome:   clip(oneLine(parsed.Outcome), recapOutcomeLimit),
	}
	if recap.Line == "" || notAnAccount.MatchString(recap.Line) || titleHasControlTokens(raw) {
		return ConversationRecap{}, false
	}
	for _, item := range parsed.Decided {
		if decision, ok := parseDecision(item); ok {
			recap.Decided = append(recap.Decided, decision)
		}
		if len(recap.Decided) == recapDecisions {
			break
		}
	}
	return recap, true
}

// parseDecision accepts a decision as an object or as a bare sentence.
func parseDecision(raw json.RawMessage) (RecapDecision, bool) {
	var bare string
	if json.Unmarshal(raw, &bare) == nil {
		text := clip(oneLine(bare), recapDecisionLimit)
		return RecapDecision{Text: text}, text != ""
	}
	var object struct {
		Text string `json:"text"`
		By   string `json:"by"`
		How  string `json:"how"`
	}
	if json.Unmarshal(raw, &object) != nil {
		return RecapDecision{}, false
	}
	decision := RecapDecision{
		Text: clip(oneLine(object.Text), recapDecisionLimit),
		By:   decisionBy(object.By),
		How:  clip(oneLine(object.How), recapHowLimit),
	}
	return decision, decision.Text != ""
}

// decisionBy narrows who decided to the two words the wire carries. Anything
// else is left empty: an unattributed decision is true, a misattributed one is
// not.
func decisionBy(said string) string {
	switch strings.ToLower(strings.TrimSpace(said)) {
	case "you", "user", "person", "human":
		return "you"
	case "codeaf", "assistant", "ai", "model":
		return "codeaf"
	}
	return ""
}
