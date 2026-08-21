package session

// answers.go is how an answer reaches a session that is stopped on a question
// in ANOTHER window.
//
// taskpresence.go carries the question outward: a live session blocked on a
// card writes what it is asking, with the answers it will take, into its
// presence file, and any other window may read it without opening a journal or
// taking a lock. This file is the return path, and it is deliberately the
// simplest thing that works — internal/standing's inbox.go in the other
// direction: ONE JSONL FILE in the session's own folder, appended by whoever
// answered, drained whole by the session itself on the heartbeat it already
// runs.
//
// ── THE THREE LAWS ──
//
//   - AN ANSWER IS APPLIED THROUGH THE SAME RESOLVER A SURFACE USES. There is
//     no second door into the approval gate, the task proposal or the standing
//     card: [Agent.applyAnswer] calls [Agent.ResolveConsentRemember],
//     [Agent.ResolveTask] and [Agent.ResolveStanding], which is exactly what the
//     card in the window calls. A lane of its own would be a second place that
//     decides what "yes" does, and the two would drift on the day one of them
//     learned something.
//
//   - A LATE ANSWER IS IGNORED, AND NOTHING SAYS SO. The resolvers already drop
//     an id nobody is waiting on — a question the clock approved, a card the
//     person answered in its own window a second earlier, a turn that was
//     interrupted — and an answer arriving through a file is late in exactly
//     those ways and no new ones. So this file adds no staleness rule of its
//     own; it hands the id over and lets the one rule that exists apply.
//
//   - THE KEY IS THE ANSWER'S NAME, AND THE MAPPING IS WRITTEN ONCE.
//     [AnswerOptions] says which keys a kind of question takes and what each of
//     them is called; [AnswerFromKey] says what one of them MEANS. Both live
//     here, in the engine, because the surface that draws the chips and the
//     session that applies the answer must agree about them completely — a
//     surface offering a key the session does not take is a chip that does
//     nothing, and a surface whose "2" means something else than the session's
//     is worse than either.
//
// ── THE FILE ──
//
//	<session dir>/answers.jsonl
//
// One JSON object per line: when it was given, which question it answers, and
// the key. JSONL and not JSON because two windows could answer two questions in
// the same instant and an append never loses one — the opposite of presence.json
// next to it, which has exactly one writer and one fact and is replaced whole.
//
// THE DRAIN RENAMES BEFORE IT READS, for standing's own reason: an answer
// delivered while the file is being read would otherwise be read and then
// deleted unapplied. Moving it aside first means a racing write starts a fresh
// file that the next beat finds.

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The two refusals this file makes, and both are about the CALLER rather than
// about the disk: a key the question does not take, and a session with no
// folder to leave anything in. Everything that can go wrong with the file
// system is dropped in silence instead (see [Agent.drainAnswers]).
var (
	errUnknownAnswer   = errors.New("session: that question does not take that answer")
	errNoSessionFolder = errors.New("session: that conversation has no folder to answer into")
)

// answersName is the file, inside one session's folder. Like presenceName it is
// spelled here and nowhere else: an answers file is not part of what a session
// KEEPS, it is a doorstep other windows leave things on.
const answersName = "answers.jsonl"

// AnswersPath is that doorstep for one session folder.
func AnswersPath(sessionDir string) string {
	return filepath.Join(strings.TrimSpace(sessionDir), answersName)
}

// QuestionKind is which of the three lanes a question came from. The words are
// the ones the code already uses for them.
type QuestionKind string

const (
	// QuestionConsent is the approval gate: may this call run (consent.go).
	QuestionConsent QuestionKind = "consent"
	// QuestionTask is a task proposal: should this work go (task.go).
	QuestionTask QuestionKind = "task"
	// QuestionStanding is a standing card: should this be kept an eye on
	// (tools_standing.go).
	QuestionStanding QuestionKind = "standing"
)

// AnswerOption is one answer a question will take: the key that gives it and
// the word for it.
type AnswerOption struct {
	// Key is what a person presses. It is a digit on every kind, because the
	// surface that offers these is home, where the letters are already typing.
	Key string `json:"key"`
	// Label is the answer in the words the card uses for it.
	Label string `json:"label"`
}

// AnswerOptions is what one kind of question may be answered with, in the order
// the chips are drawn.
//
// THE KEYS ARE THE CARD'S OWN DIGITS WHERE THE CARD HAS DIGITS. The standing
// card is answered 1 yes, 2 change when, 3 once in its own window (tui3's
// standing.go), and 1 and 3 mean the same here — the hand that learned them
// there is right here. `2 change when` is deliberately NOT on this list: it is a
// request for a text box, and there is no box on the row this is drawn beside.
//
// THE CONSENT KEYS ARE NEW AND THE ANSWERS ARE NOT. In its own window the gate
// is answered y / a / n; those letters cannot be borrowed here, because a letter
// on home is a character being typed. So the three answers keep their meaning
// and take digits, and the words beside them are the card's own.
func AnswerOptions(kind QuestionKind) []AnswerOption {
	switch kind {
	case QuestionConsent:
		return []AnswerOption{
			{Key: "1", Label: "allow once"},
			{Key: "2", Label: "always"},
			{Key: "3", Label: "deny"},
		}
	case QuestionTask:
		return []AnswerOption{
			{Key: "1", Label: "yes"},
			{Key: "2", Label: "no"},
		}
	case QuestionStanding:
		return []AnswerOption{
			{Key: "1", Label: "yes"},
			{Key: "3", Label: "once, not standing"},
		}
	}
	return nil
}

// AnswerLabel is the word for one key, and "" for a key that kind does not
// take. A surface says what it just did with it.
func AnswerLabel(kind QuestionKind, key string) string {
	for _, option := range AnswerOptions(kind) {
		if option.Key == key {
			return option.Label
		}
	}
	return ""
}

// AnswerAction is what one key MEANS: the answer, in the shape the lane's own
// resolver takes.
//
// It is one struct with three lanes' answers on it rather than three functions,
// because a caller with a key and a kind in its hand wants ONE call — and
// because the zero value of each field is already that lane's "no", so a
// mis-typed key can never come out as a yes.
type AnswerAction struct {
	// Kind is the lane this action belongs to, and the field a caller switches
	// on to know which of the three below to read.
	Kind QuestionKind
	// Allow and Scope are the consent lane's answer, for
	// [Agent.ResolveConsentRemember].
	Allow bool
	Scope ConsentScope
	// Task is the proposal lane's answer, for [Agent.ResolveTask].
	Task TaskAnswer
	// Standing is the standing lane's answer, for [Agent.ResolveStanding].
	Standing StandingAnswer
}

// AnswerFromKey is the whole mapping, and it is the one place it is written.
//
// A key the kind does not take answers false and is applied to nothing. That is
// the same conservative reading [Agent.ResolveConsentRemember] takes of an
// unknown scope, and for the same reason: a typo must never widen an approval,
// and a key that fell off a chip row must never be read as the answer next to
// it.
//
// WHAT "ALWAYS" IS, EXACTLY. It is [ConsentToolSession] — the same answer the
// window's `a` key sends the engine, which stops that session asking about that
// TOOL for the rest of its life. It is not [ConsentRule]: a banked rule is
// written from the command line the call actually carried, and a window
// answering somebody else's question has only the one line the session is
// stopped on. Writing a rule from that gloss would bank a standing approval for
// a command that was never run (tui3's [app.askCommand] states the same law).
// The floor holds either way — the shapes internal/approval always asks about
// are asked again whatever memo is standing.
func AnswerFromKey(kind QuestionKind, key string) (AnswerAction, bool) {
	key = strings.TrimSpace(key)
	if AnswerLabel(kind, key) == "" {
		return AnswerAction{}, false
	}
	action := AnswerAction{Kind: kind}
	switch kind {
	case QuestionConsent:
		switch key {
		case "1":
			action.Allow, action.Scope = true, ConsentOnce
		case "2":
			action.Allow, action.Scope = true, ConsentToolSession
		case "3":
			action.Allow, action.Scope = false, ConsentOnce
		}
	case QuestionTask:
		switch key {
		case "1":
			action.Task = TaskAnswer{Approved: true}
		case "2":
			action.Task = TaskAnswer{Approved: false}
		}
	case QuestionStanding:
		switch key {
		case "1":
			action.Standing = StandingAnswer{Approved: true}
		case "3":
			action.Standing = StandingAnswer{Once: true}
		}
	}
	return action, true
}

// Answer is one line of the file: an answer somebody gave, from somewhere else.
type Answer struct {
	// At is when it was given. It orders a drain and is the only thing here a
	// reader could use to notice an answer that sat on the doorstep for a week
	// — nothing does, because a question that old is one nobody is waiting on
	// and the resolvers already drop it.
	At time.Time `json:"at"`
	// Kind and ID name the question, exactly as the presence file's
	// [PresenceQuestion] spelled them.
	Kind QuestionKind `json:"kind"`
	ID   uint64       `json:"id"`
	// Key is the answer, as [AnswerOptions] names it.
	Key string `json:"key"`
	// From is where it came from — "home" is the only writer today. It is here
	// so a later reader can tell an answer somebody gave on another screen from
	// one a machine gave, without guessing from a timestamp.
	From string `json:"from,omitempty"`
}

// answerFromHome is what home writes in [Answer.From].
const answerFromHome = "home"

// WriteAnswer leaves one answer on a session's doorstep.
//
// It is the seam a surface is handed (tui3's Options.Answer), and it takes the
// session's FOLDER rather than an agent, because the whole point is that the
// session being answered is in another process. A key the kind does not take is
// refused here rather than written and dropped later — the surface that offered
// the chip is the one that can still say something about it.
func WriteAnswer(sessionDir string, kind QuestionKind, id uint64, key string) error {
	if _, ok := AnswerFromKey(kind, key); !ok {
		return errUnknownAnswer
	}
	return deliverAnswer(sessionDir, Answer{
		At:   time.Now(),
		Kind: kind,
		ID:   id,
		Key:  strings.TrimSpace(key),
		From: answerFromHome,
	})
}

// deliverAnswer appends one line, making the folder if it is not there. It is
// [standing.Deliver] with a different payload and the same shape.
func deliverAnswer(sessionDir string, answer Answer) error {
	dir := strings.TrimSpace(sessionDir)
	if dir == "" {
		return errNoSessionFolder
	}
	line, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(AnswersPath(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(line); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// DrainAnswers reads and removes a session's answers, oldest first. A folder
// with nothing on its doorstep is an empty slice and no error, which is the
// ordinary case on every beat of every session that was never answered from
// anywhere.
func DrainAnswers(sessionDir string) ([]Answer, error) {
	path := AnswersPath(sessionDir)
	if strings.TrimSpace(sessionDir) == "" {
		return nil, nil
	}
	// THE RENAME IS THE READ'S OWN LOCK, and it is the whole of the concurrency
	// story here: whoever wins the rename owns those lines, and a write racing
	// it lands in a fresh file the next beat drains.
	staged := path + "." + NewSessionID() + ".draining"
	if err := os.Rename(path, staged); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer os.Remove(staged)
	file, err := os.Open(staged)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var answers []Answer
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var answer Answer
		if json.Unmarshal(raw, &answer) != nil {
			// A line nothing can read is a line nothing can apply. It is
			// dropped rather than reported: there is no question in it to be
			// answered and nobody left to tell.
			continue
		}
		answers = append(answers, answer)
	}
	if err := scanner.Err(); err != nil {
		return answers, err
	}
	sort.SliceStable(answers, func(a, b int) bool { return answers[a].At.Before(answers[b].At) })
	return answers, file.Close()
}

// ── the live session's side ─────────────────────────────────────────────────

// drainAnswers takes whatever is on this session's doorstep and applies it.
//
// It runs on the presence heartbeat (taskpresence.go's [presenceDesk.beat]),
// which is the natural place for it and not merely a convenient one: the same
// beat is what put the question on disk, the cadence a person waits after
// pressing a key is the cadence the question appeared at, and a session with no
// folder — a memory-only conversation, a task node — has no presence and
// therefore no doorstep either.
//
// EVERY FAILURE IS SILENCE, as every other write in that file is. A session
// must not stall or say anything because a directory would not answer; the
// answer is simply not applied, and the person's window still has the question
// on it.
func (a *Agent) drainAnswers() {
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" {
		return
	}
	answers, err := DrainAnswers(dir)
	if err != nil && len(answers) == 0 {
		return
	}
	for _, answer := range answers {
		a.applyAnswer(answer)
	}
}

// applyAnswer hands one answer to the lane it belongs to, THROUGH THE SAME
// RESOLVER A SURFACE USES (the first law in this file's header). An id nobody is
// waiting on falls through those resolvers untouched, which is what makes a
// stale answer a no-op rather than a special case here.
func (a *Agent) applyAnswer(answer Answer) {
	action, ok := AnswerFromKey(answer.Kind, answer.Key)
	if !ok {
		return
	}
	switch action.Kind {
	case QuestionConsent:
		a.ResolveConsentRemember(answer.ID, action.Allow, action.Scope)
	case QuestionTask:
		a.ResolveTask(answer.ID, action.Task)
	case QuestionStanding:
		a.ResolveStanding(answer.ID, action.Standing)
	}
}
