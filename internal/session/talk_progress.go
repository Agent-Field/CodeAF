package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// ── THE ITEM'S MANAGER CONVERSATION, AS THE FACTORY RUNNER READS AND WRITES IT
//
// An item on the factory floor has one conversation of its own, and that
// conversation is the MANAGER of the item's run (internal/factory/run's
// manager.go): the runner reports each stage into it, and what the person types
// into it is the brief before a run and the steer during one. This file is the
// three doors the runner reaches it through, all of them over the conversation's
// own journal, so the file stays one valid conversation whoever wrote a line:
//
//   - [AppendProgress] writes one runner line as an assistant message, marked
//     `{"audience":"human","kind":"factory-progress"}`, with the step the line
//     reports on beside it when it reports on one (`"step":{"step":"review",
//     "round":1,"event":"done"}`), and tells [OnProgress]'s reader it did;
//   - [PersonLines] reads what the person typed, oldest first, after a moment;
//   - [LastSaid] reads the last sentence a conversation's model said, which is
//     how a stage's one-line summary is found when it left no notes.
//
// A LINE IS NEVER WRITTEN INTO THE MIDDLE OF A TURN. A conversation open in this
// process takes the line through its own agent, under the agent's lock, and only
// while no turn is running: an assistant line between a tool call and its result
// would make the transcript illegal to send. A turn in flight answers
// [ErrConversationBusy], and a conversation another process holds answers its
// lock error; the runner keeps the line and says it again later, in order.

// FactoryProgressKind is the presentation kind of a factory runner's line.
const FactoryProgressKind = "factory-progress"

// ErrConversationBusy is a progress line refused because the conversation is in
// the middle of a turn. The line is owed, not lost: say it again later.
var ErrConversationBusy = errors.New("the conversation is in the middle of a turn")

// liveJournals is every conversation open in this process, by its journal's
// real path, so a progress line reaches the agent holding the journal's lock
// instead of failing on it.
var liveJournals sync.Map

// journalKey is the one spelling of a journal's path the registry is keyed by.
func journalKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}

// rememberLiveJournal registers a conversation that opened its journal.
func rememberLiveJournal(path string, a *Agent) {
	if key := journalKey(path); key != "" && a != nil {
		liveJournals.Store(key, a)
	}
}

// LiveAgentFor answers the conversation open in this process on the journal
// at path, when one is. A factory runner's shaping turn goes THROUGH it rather
// than opening the journal a second time, which its lock would refuse.
func LiveAgentFor(path string) (*Agent, bool) {
	key := journalKey(path)
	if key == "" {
		return nil, false
	}
	v, ok := liveJournals.Load(key)
	if !ok {
		return nil, false
	}
	a, _ := v.(*Agent)
	if a == nil {
		return nil, false
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return nil, false
	}
	return a, true
}

// Closed says the conversation has been closed: a view attached to it (the
// factory item page hosting a running step's chat) lets go and opens the
// journal again rather than offering a box that can no longer take a word.
func (a *Agent) Closed() bool {
	if a == nil {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closed
}

// TurnRunning says whether a turn is in flight on the conversation: the
// person's, a wake, or a factory runner's shaping turn. A closed conversation
// runs nothing.
func (a *Agent) TurnRunning() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.closed && a.running
}

// LiveTurnRunning says whether the conversation on the journal at path is
// open in this process with a turn in flight: how the factory's item page
// knows its manager is thinking, whoever started the turn. It resolves the
// path's links, so it touches the disk and is asked off a surface's loop.
func LiveTurnRunning(path string) bool {
	a, ok := LiveAgentFor(path)
	return ok && a.TurnRunning()
}

// forgetLiveJournal takes a closing conversation off the registry.
func forgetLiveJournal(a *Agent) {
	turnRunDoors.Delete(a)
	liveJournals.Range(func(key, value any) bool {
		if value == a {
			liveJournals.CompareAndDelete(key, a)
		}
		return true
	})
}

// progressMessage is the line as the journal and the model keep it, with the
// step it reports on, nil for a line about the job.
func progressMessage(line string, step *factory.StepProgress) progressLine {
	text := strings.Join(strings.Fields(line), " ")
	return progressLine{text: text, mark: &messagePresentation{Audience: "human", Kind: FactoryProgressKind, FactoryStep: step}}
}

// progressLine is one runner line and its mark.
type progressLine struct {
	text string
	mark *messagePresentation
}

// AppendProgress writes line into the conversation at path as an assistant
// message marked [FactoryProgressKind], with step beside it when the line
// reports on one step's round. A conversation open in this process takes it
// through its agent, between turns; one nobody holds is opened, written and let
// go; one held elsewhere, or mid-turn, refuses and the caller says it again
// later. A line written is told to [OnProgress]'s reader, after it is on disk.
func AppendProgress(path, line string, step *factory.StepProgress) error {
	p := progressMessage(line, step)
	if p.text == "" {
		return nil
	}
	key := journalKey(path)
	if key == "" {
		return errors.New("a progress line needs a conversation")
	}
	if _, err := os.Stat(key); err != nil {
		return err
	}
	// A CONVERSATION DELETED FOR GOOD IS GONE, not busy: its lines are not
	// owed to anybody.
	if _, err := os.Stat(filepath.Join(filepath.Dir(key), conversationDeletedFile)); err == nil {
		return os.ErrNotExist
	}
	if v, ok := liveJournals.Load(key); ok {
		if a, _ := v.(*Agent); a != nil {
			done, err := a.recordIdleProgress(p)
			if err != nil {
				return err
			}
			if done {
				tellProgress(ProgressNews{Path: path, Text: p.text, Step: step})
				return nil
			}
		}
	}
	journal, _, err := openSessionFile(key, "", "", sessionIDOfFolder(key))
	if err != nil {
		return err
	}
	message := textMessage("assistant", p.text)
	journal.presentation.remember(message, p.mark)
	journal.appendReasonedMessage(message, provider.MessageReasoning{})
	// The journal is let go before the line is told, so a reader that reads
	// the conversation back finds it free and the line in it.
	journal.Close()
	tellProgress(ProgressNews{Path: path, Text: p.text, Step: step})
	return nil
}

// ── TOLD AS IT LANDS ────────────────────────────────────────────────────────
//
// A progress line lands in a conversation outside any turn, so no turn's
// stream carries it to a surface showing that conversation. [OnProgress] is
// how one hears it: the app reads it back and says it as the entry it is
// (internal/appwire's entry.added), so the job's page shows each step's line
// as it is written rather than at its next reading.

// ProgressNews is one line [AppendProgress] wrote: the conversation's journal
// as the runner named it, the words as the journal keeps them, and the step
// the line reports on, nil for a line about the job.
type ProgressNews struct {
	Path string
	Text string
	Step *factory.StepProgress
}

var (
	progressMu     sync.RWMutex
	progressReader func(ProgressNews)
)

// progressDesk is where a written line is left for the reader (sidecar.go's
// desk): THE RUNNER LEAVES THE NEWS AND WALKS AWAY. The reader reads the
// conversation back before it says anything, and the runner's next line, and
// the step after it, never wait on that.
var progressDesk desk

// OnProgress registers the reader every written progress line is told to and
// hands back the one that was there, which a reader that is not the last word
// calls on in turn ([OnPhaseNews] is the same shape). A nil function
// unregisters. The reader is told from a desk, never on the writer's
// goroutine, in the order the lines were written, each once it is on disk.
func OnProgress(fn func(ProgressNews)) (previous func(ProgressNews)) {
	progressMu.Lock()
	defer progressMu.Unlock()
	previous, progressReader = progressReader, fn
	return previous
}

// tellProgress leaves one written line on the desk for the registered reader,
// if there is one.
func tellProgress(news ProgressNews) {
	progressMu.RLock()
	reader := progressReader
	progressMu.RUnlock()
	if reader == nil {
		return
	}
	progressDesk.tell(func() { reader(news) })
}

// recordIdleProgress records the line through a live agent. done is false
// when the agent has closed, and the journal is then free for the caller.
func (a *Agent) recordIdleProgress(p progressLine) (done bool, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.file == nil {
		return false, nil
	}
	if a.running {
		return true, ErrConversationBusy
	}
	if a.presentation == nil {
		a.presentation = &presentationIndex{}
	}
	message := textMessage("assistant", p.text)
	a.presentation.remember(message, p.mark)
	a.alignReasoningLocked()
	a.messages = append(a.messages, message)
	a.messageReasoning = append(a.messageReasoning, provider.MessageReasoning{})
	a.file.appendReasonedMessage(message, provider.MessageReasoning{})
	a.chatlog.post(message)
	return true, nil
}

// PersonLine is one thing the person typed into a conversation.
type PersonLine struct {
	At    time.Time
	Words string
}

// PersonLines answers what the person typed into the conversation at path
// after the moment after, oldest first: every user message that is not a note
// the session wrote (an opening brief, a landing's news, a team's wake).
//
// A COMPACTED JOURNAL IS READ ONCE. A pass writes the window it kept again
// behind its marker ([sessionFile.appendCompaction]), so the copies that follow
// a marker are skipped, and every line is read where it was first written, with
// the instant it was written.
func PersonLines(path string, after time.Time) ([]PersonLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var out []PersonLine
	copies := 0
	for {
		raw, err := r.ReadBytes('\n')
		if len(raw) > 0 {
			var e struct {
				Type      string `json:"type"`
				Role      string `json:"role"`
				Content   string `json:"content"`
				Note      bool   `json:"note"`
				Window    int    `json:"window"`
				Timestamp string `json:"timestamp"`
			}
			if json.Unmarshal(raw, &e) == nil {
				switch e.Type {
				case "compaction":
					copies = e.Window
				case "message":
					if copies > 0 {
						copies--
						break
					}
					if e.Role != "user" || e.Note {
						break
					}
					words := strings.TrimSpace(e.Content)
					// A CARRY-ON IS CODEAF'S OWN VOICE in the person's seat (the
					// checkpoint's nudge to finish, checkpoint.go), never the
					// person's words: a runner that took it as the steer would
					// steer every stage with it, as the 18:31 run did.
					if isCarryOn(words) {
						break
					}
					at, perr := time.Parse(time.RFC3339Nano, e.Timestamp)
					if words == "" || perr != nil || !at.After(after) {
						break
					}
					out = append(out, PersonLine{At: at, Words: words})
				}
			}
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}

// LastSaid is the last sentence the model said in the conversation at path,
// "" when it said nothing or the file cannot be read.
func LastSaid(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	replayed, err := replaySessionFile(path)
	if err != nil {
		return ""
	}
	for i := len(replayed.messages) - 1; i >= 0; i-- {
		m := replayed.messages[i]
		if m.Role != "assistant" {
			continue
		}
		if text := strings.TrimSpace(messageContentText(m)); text != "" {
			return lastSentence(text)
		}
	}
	return ""
}

// lastSentence is the last sentence of text, on one line.
func lastSentence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	cut := -1
	for _, end := range []string{". ", "! ", "? "} {
		if i := strings.LastIndex(strings.TrimRight(text, ".!? "), end); i > cut {
			cut = i
		}
	}
	if cut >= 0 {
		return strings.TrimSpace(text[cut+2:])
	}
	return text
}

// carryOnLead opens every user message the checkpoint writes in the person's
// seat to make the model carry on (agent.go, checkpoint.go); isCarryOn says
// whether a user message is one of those rather than something a person typed.
const carryOnLead = "[carry on] "

// CarryOn is text as a carry-on message: the person's seat, but not their
// words, so the conversation's own reading of what a person asked skips it.
// The factory's stage maker puts it on the note a resumed stage is given.
func CarryOn(text string) string { return carryOnLead + strings.TrimSpace(text) }

func isCarryOn(text string) bool { return strings.HasPrefix(text, carryOnLead) }

// ClosedSignal closes as soon as Close refuses further work. A view subscribes
// under the same lock as Close, so even a previously closed agent is observed.
func (a *Agent) ClosedSignal() <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closedSignal == nil {
		a.closedSignal = make(chan struct{})
		if a.closed {
			close(a.closedSignal)
		}
	}
	return a.closedSignal
}
