package session

import (
	"strconv"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Note kinds name WHAT WROTE a session-authored line, so a surface can draw a
// finished task, a job's exit, a watch firing and a resume's account of an
// interrupt as the different things they are without reading their words.
//
// They are persisted bytes: a journal written today is read by builds that
// do not exist yet, so a kind is never respelled once it has shipped.
const (
	// NoteKindTask is a task or run reporting that it finished.
	NoteKindTask = "task"
	// NoteKindJob is a background job's exit.
	NoteKindJob = "job"
	// NoteKindWatch is a watch's news, tick or firing.
	NoteKindWatch = "watch"
	// NoteKindResume is the account a resumed session gives of the work the
	// last process was interrupted in the middle of.
	NoteKindResume = "resume"
)

// noteFacts is what the session knows about one line it wrote, said by the code
// that wrote it and never recovered from the line's words.
//
// IT IS ONE CANONICAL SIGNAL, JOURNALED ONCE. The live queue and the replay read
// the same three fields, so a reopened page draws what the live one drew; a
// journal written before the fields existed replays the zero value, which every
// surface reads as "say nothing" by the emptiness law.
type noteFacts struct {
	// Kind is one of the NoteKind constants, or empty for a note with no single
	// author worth naming (a batch that mixed several, a standing fold).
	Kind string `json:"kind,omitempty"`
	// Title is a short name the author gave its subject. Only a job sets it, from
	// its own label or command.
	Title string `json:"title,omitempty"`
	// Tasks are the finished tasks the note reports, as the decimal strings a
	// surface keys its task rows by.
	Tasks []string `json:"tasks,omitempty"`
	// UndoReceipts is exact context mutation provenance, never recovered from text.
	// Mixed batched notes deliberately drop this authority.
	UndoReceipts []string `json:"undoReceipts,omitempty"`
}

func (f noteFacts) empty() bool {
	return f.Kind == "" && f.Title == "" && len(f.Tasks) == 0 && len(f.UndoReceipts) == 0
}

// taskFacts is the facts of a note reporting the given tasks as finished.
func taskFacts(ids ...uint64) noteFacts {
	facts := noteFacts{Kind: NoteKindTask}
	for _, id := range ids {
		if id != 0 {
			facts.Tasks = append(facts.Tasks, strconv.FormatUint(id, 10))
		}
	}
	return facts
}

// mergeNoteFacts is the facts of ONE note standing for several, as the
// boundary's "while you worked" batch is.
//
// A KIND IS KEPT ONLY WHEN EVERY NOTE AGREES, because a batch of a task and a job
// has no honest single author and naming one would draw the other as something
// it is not. Tasks are the union in note order, and a title survives only when
// exactly one distinct title was given.
func mergeNoteFacts(all []noteFacts) noteFacts {
	if len(all) == 1 {
		return all[0]
	}
	var merged noteFacts
	titles := map[string]bool{}
	for index, facts := range all {
		if index == 0 {
			merged.Kind = facts.Kind
		} else if merged.Kind != facts.Kind {
			merged.Kind = ""
		}
		if facts.Title != "" {
			titles[facts.Title] = true
			merged.Title = facts.Title
		}
		merged.Tasks = append(merged.Tasks, facts.Tasks...)
	}
	if len(titles) != 1 || merged.Kind == "" {
		merged.Title = ""
	}
	return merged
}

// rememberNoteFacts indexes one note's facts under the note's fingerprint, in a
// map the caller owns.
func rememberNoteFacts(index map[string]noteFacts, message ai.Message, facts noteFacts) {
	if facts.empty() {
		return
	}
	if key := noteKey(message); key != "" {
		index[key] = facts
	}
}

// noteFactsOf is what the journal holds about one note, or the zero value.
func (s *sessionFile) noteFactsOf(message ai.Message) noteFacts {
	if s == nil {
		return noteFacts{}
	}
	key := noteKey(message)
	if key == "" {
		return noteFacts{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.facts[key]
}
