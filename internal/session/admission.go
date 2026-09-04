package session

// The record a task is admitted with, and how a worker reads it.
//
// A node's opening document already carries the person's own sentence and the
// contract a model groomed out of it (task_brief.go). What it never carried was
// the middle: a constraint typed two turns before the one that started the work,
// the assistant's own reading of a file it had just opened, the exact arguments
// of the call that failed. This is a BOUNDED, ATTRIBUTED SELECTION of that
// middle — quotations with a place to read them in full, not a summary and not
// a constraint list.
//
// THREE INVARIANTS, and everything here exists for one of them:
//
//   - A quote is evidence of what was SAID. Nothing in this file classifies,
//     concludes or summarises; the only judgement made is which lines to carry,
//     by recency and role. [admissionQuotesRule] says as much to the worker.
//   - THE SELECTION IS INCOMPLETE BY CONSTRUCTION, so every entry carries where
//     its full text lives ([AdmissionQuote.Source]) and the document says the
//     record is partial. A worker that needs certainty reads the journal.
//   - Nothing missing is reported as something good. An outcome this process did
//     not record renders as unknown, never as success ([AdmissionHandle.line]).
//
// Compilation lives in admission_compile.go; this file is the record, its
// persistence and its rendering.

import "strings"

// AdmissionContextVersion is the shape of the record below. A checkpoint from
// another build carries another number and is read through
// [AdmissionContext.restored] rather than trusted field by field.
const AdmissionContextVersion = 1

// The two speakers. There is no third: a wake note, a task's landing and a
// job's exit are the session talking to itself, and they are kept out of the
// person's lane where the person's words are recorded
// ([Agent.rememberAskLocked]).
const (
	admissionPerson    = "person"
	admissionAssistant = "assistant"
)

// AdmissionQuote is one thing somebody said.
//
// Speaker and Source are what keep it from reading as a finding: whose sentence
// it is, and where the whole of it can be read. Text is bounded and may be
// elided in the middle ([elide]), so Source is not decoration — it is what makes
// a clipped quote safe to carry.
type AdmissionQuote struct {
	// ID is the DEDUPLICATION KEY and never an address. What it identifies
	// differs by speaker, and the difference is honest rather than tidy:
	//
	//   - The person's turns are numbered as they are heard, so `p3` is an EVENT.
	//     The same sentence typed again after a correction is a new instruction
	//     and gets a new number.
	//   - An assistant line is keyed by its CONTENT (`a<fingerprint>`), because
	//     nothing durable numbers assistant messages: a transcript position moves
	//     under compaction, so a position-derived id would be a different id for
	//     the same line after a fold. Two identical assistant lines therefore
	//     share an id and are carried once, which is the wanted answer anyway.
	ID      string `json:"id"`
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
	// Source is the record this was quoted from: the session journal, which a
	// worker can open with `read` and `grep`. There is deliberately no line
	// number — placing one would mean scanning the whole journal at every task
	// admission, and an exact line for a message whose words appear twice cannot
	// be told apart from the other one anyway. The words themselves are what the
	// worker greps for. An empty Source draws no pointer at all rather than an id
	// nothing can resolve.
	Source string `json:"source,omitempty"`
	// Calls names the tools this text was said alongside. Text and calls are not
	// alternatives: an assistant message that reads a result and starts the next
	// call in the same breath is the ordinary shape of work, and a walker that
	// treated them as exclusive would drop exactly those.
	Calls []string `json:"calls,omitempty"`
	// From is whose transcript this came out of — empty for the conversation,
	// "task 7" for a node.
	From string `json:"from,omitempty"`
	// Depth counts the admissions this entry has travelled through. 0 is local;
	// past [admissionDepthLimit] it is not carried.
	Depth int `json:"depth,omitempty"`
}

// AdmissionHandle is one call that already ran: what was asked of which tool,
// what became of it as far as this record knows, and where the full result is.
//
// A successful result's body is deliberately absent. The point of a handle is
// that the bytes stay where they are and the worker fetches what it needs.
type AdmissionHandle struct {
	Call  string `json:"call"`
	Tool  string `json:"tool"`
	Input string `json:"input,omitempty"`
	// Outcome is what is KNOWN about how the call ended, and the unknown state is
	// a real one: the flag a tool returned does not survive into the transcript,
	// so a context compiled after a restart can say a call was answered without
	// being able to say whether it succeeded.
	Outcome AdmissionOutcome `json:"outcome,omitempty"`
	// Detail is a failure's own first line, and only a failure's: knowing that a
	// call failed without knowing how is what makes a worker run it again.
	Detail string `json:"detail,omitempty"`
	// Source is the record the whole result can be read out of. It is the session
	// journal rather than the conversation store, because a store id is not
	// something `read` or `grep` can open — the same affordance rule the fold
	// marker states (loop.go). The call id is the token to grep for: the journal
	// writes it on the result's own line.
	Source string `json:"source,omitempty"`
	From   string `json:"from,omitempty"`
	Depth  int    `json:"depth,omitempty"`
}

// AdmissionOutcome is the three answers there are about a finished call.
type AdmissionOutcome string

const (
	// AdmissionUnknown is the honest default: a result reached the transcript and
	// nothing in this process recorded whether it was a failure.
	AdmissionUnknown AdmissionOutcome = ""
	AdmissionOK      AdmissionOutcome = "ok"
	AdmissionFailed  AdmissionOutcome = "failed"
	// AdmissionUnanswered is a call with no result in the transcript at all — an
	// interrupted batch, a turn that died. It is never rendered as either
	// success or failure.
	AdmissionUnanswered AdmissionOutcome = "unanswered"
)

// AdmissionContext is the working context one task is admitted with.
//
// Every field is exported and tagged because this is embedded in the checkpoint
// record (task_store.go): unexported fields would serialise as `{}` and a
// resumed task would read an empty document under a full heading.
type AdmissionContext struct {
	Version  int               `json:"version"`
	Quotes   []AdmissionQuote  `json:"quotes,omitempty"`
	Evidence []AdmissionHandle `json:"evidence,omitempty"`
}

func (c AdmissionContext) empty() bool {
	return len(c.Quotes) == 0 && len(c.Evidence) == 0
}

// restored is what a context read off a checkpoint is worth. A record from a
// version this build does not know is dropped rather than guessed at: field
// names survive across versions, meanings need not, and a worker opened on a
// misread document is worse off than one opened on the contract alone — which
// is what every checkpoint written before this existed already gives.
func (c AdmissionContext) restored() AdmissionContext {
	if c.Version <= 0 || c.Version > AdmissionContextVersion {
		return AdmissionContext{}
	}
	return c
}

// recordedAdmission is the checkpoint's copy, and nothing at all for a node that
// carries no context.
func recordedAdmission(context AdmissionContext) *AdmissionContext {
	if context.empty() {
		return nil
	}
	return &context
}

// restoredAdmission is the one reader of the checkpoint's field.
func restoredAdmission(record *AdmissionContext) AdmissionContext {
	if record == nil {
		return AdmissionContext{}
	}
	return record.restored()
}

// ── what the worker reads ───────────────────────────────────────────────────

// The two sections and the rules over them. task_brief.go decides where they sit
// in the document; what they may CLAIM is decided here.
//
// The quotes rule is the safety of the whole feature. It says three things a
// worker cannot get from the lines themselves: that they were said rather than
// established, that the selection is partial, and that the assignment above is
// still the assignment. It deliberately does not rank the quotes against the
// contract — where a quoted line and the brief plainly disagree that is news for
// the report, not a precedence rule for a worker to apply on its own.
const (
	admissionQuotesHeading   = "SOME OF WHAT WAS SAID AROUND THIS WORK"
	admissionQuotesRule      = "A FEW LINES FROM THE CONVERSATION THIS CAME OUT OF, OLDEST FIRST — a bounded selection, not the whole record and not a list of your requirements. They are what was SAID, not what is true: check anything you are about to depend on, and grep the record named on the line for the whole of it. A later line may have replaced an earlier one. Where one of them plainly contradicts the work above, say so in your report rather than quietly choosing."
	admissionEvidenceHeading = "CALLS THAT HAVE ALREADY RUN"
	admissionEvidenceRule    = "The exact input each one ran on, and what is known about how it ended. \"outcome unknown\" means nobody recorded the outcome — not that it went well. Nothing here says what a result MEANT: grep the call id in the record named on the line to read the whole of it."
)

// admissionQuotesSection is the quotes as the worker reads them, and nothing at
// all when there are none — the emptiness law, applied to a document.
func admissionQuotesSection(context AdmissionContext) string {
	return admissionLines(len(context.Quotes), func(index int) string {
		return context.Quotes[index].line()
	})
}

// admissionEvidenceSection is the handles as the worker reads them.
func admissionEvidenceSection(context AdmissionContext) string {
	return admissionLines(len(context.Evidence), func(index int) string {
		return context.Evidence[index].line()
	})
}

func admissionLines(count int, at func(int) string) string {
	if count == 0 {
		return ""
	}
	lines := make([]string, 0, count)
	for index := 0; index < count; index++ {
		lines = append(lines, "· "+at(index))
	}
	return strings.Join(lines, "\n")
}

// line is one quote: who said it, where the whole of it is, then the words.
func (q AdmissionQuote) line() string {
	line := q.who()
	if place := q.where(); place != "" {
		line += " (" + place + ")"
	}
	return line + ": “" + strings.TrimSpace(q.Text) + "”"
}

func (q AdmissionQuote) who() string {
	if q.Speaker == admissionAssistant {
		who := "the assistant"
		if q.From != "" {
			who = q.From
		}
		if len(q.Calls) > 0 {
			who += ", alongside " + strings.Join(q.Calls, ", ")
		}
		return who
	}
	if q.From != "" {
		return "the person, to " + q.From
	}
	return "the person"
}

// where is the address in the idiom this package already points with
// (task_brief.go's [originPointer]): the tools that open the file, then the
// path. NO SOURCE DRAWS NOTHING — an identifier no tool can resolve is worse
// than no pointer, because a worker will try to resolve it.
func (q AdmissionQuote) where() string {
	if strings.TrimSpace(q.Source) == "" {
		return ""
	}
	return "grep or read " + q.Source
}

// line is one handle: the tool, its input, then either where the result is or
// what is known about how it ended.
func (h AdmissionHandle) line() string {
	line := h.Tool
	if h.Input != "" {
		line += " " + h.Input
	}
	if h.From != "" {
		line += " — run by " + h.From
	}
	switch h.Outcome {
	case AdmissionFailed:
		if h.Detail != "" {
			line += " — FAILED: " + h.Detail
		} else {
			line += " — FAILED"
		}
	case AdmissionUnanswered:
		// No result to fetch, so no pointer either: this call did not finish, or
		// the record ends before it did.
		return line + " — no result on this record"
	case AdmissionOK:
		line += " — came back"
	default:
		line += " — outcome unknown"
	}
	if h.Source != "" {
		line += "; grep " + h.Call + " in " + h.Source
	}
	return line
}
