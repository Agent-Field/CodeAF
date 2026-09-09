package session

// The working context a task is admitted with: a bounded, attributed selection
// of the conversation the work came out of, and the calls that already ran.
//
// A node's opening document already carries the person's own sentence and the
// contract groomed out of it (task_brief.go). This is the middle it never
// carried — a constraint typed two turns earlier, the assistant's reading of a
// file it had just opened, the arguments of the call that failed.
//
// Three invariants hold everything here together:
//
//   - A quote is evidence of what was SAID, never of what is true. Nothing in
//     this package classifies, concludes or summarises; the only judgement is
//     which lines to carry, made by recency and role.
//   - The selection is incomplete by construction, so every entry names a record
//     a worker can open and [admissionQuotesRule] says the selection is partial.
//   - Nothing missing is reported as something good: an outcome this process did
//     not record renders as unknown rather than as success.
//
// Compilation is in admission_compile.go; this file is the record, its
// persistence and its rendering.

import "strings"

// AdmissionContextVersion is the shape of the record below. Another build's
// checkpoint carries another number and is read through
// [AdmissionContext.restored] rather than trusted field by field.
const AdmissionContextVersion = 1

// The two speakers. A wake note, a task's landing and a job's exit are the
// session talking to itself and are kept out of the person's lane where the
// person's words are recorded ([Agent.rememberAskLocked]).
const (
	admissionPerson    = "person"
	admissionAssistant = "assistant"
)

// AdmissionQuote is one thing somebody said. Speaker and Source are what keep it
// from reading as a finding: whose sentence it is, and where the whole of it can
// be read.
type AdmissionQuote struct {
	// ID is a deduplication key, never an address. What it identifies differs by
	// speaker: the person's turns are numbered as they are heard, so the same
	// sentence typed again after a correction is a separate instruction, while an
	// assistant line is keyed by its content, because nothing durable numbers
	// assistant messages and a position moves under compaction. Two identical
	// assistant lines therefore share an id and are carried once.
	ID      string `json:"id"`
	Speaker string `json:"speaker"`
	// Text is bounded and may be elided in the middle ([elide]), which is what
	// makes Source load-bearing rather than decorative.
	Text string `json:"text"`
	// Source is the record this was quoted from: the session journal, which a
	// worker can open with read and grep. There is no line number — resolving one
	// would mean parsing the whole journal at every admission, and a line for a
	// sentence that appears twice cannot be told from the other one. The words
	// are the grep token.
	Source string `json:"source,omitempty"`
	// Calls names the tools this text was said alongside. Text and calls are not
	// alternatives: a message that reads a result and starts the next call in the
	// same breath is the ordinary shape of work.
	Calls []string `json:"calls,omitempty"`
	// From is whose transcript this came out of: empty for the conversation,
	// "task 7" for a node.
	From string `json:"from,omitempty"`
	// Depth counts the admissions this entry has travelled through. 0 is local;
	// past [admissionDepthLimit] it is not carried.
	Depth int `json:"depth,omitempty"`
}

// AdmissionHandle is one call that already ran. A successful result's body is
// deliberately absent: the bytes stay where they are and the worker fetches what
// it needs.
type AdmissionHandle struct {
	Call string `json:"call"`
	Tool string `json:"tool"`
	// Input is the OPENING OF the call's arguments, cut at
	// [admissionInputLimit] and marked where it was cut. It is enough to
	// recognise which call this was; the whole of it is in the record.
	Input string `json:"input,omitempty"`
	// Outcome is what is known about how the call ended. Unknown is a real state:
	// the flag a tool returned does not survive into the transcript, so a context
	// compiled after a restart can say a call was answered without being able to
	// say whether it succeeded.
	Outcome AdmissionOutcome `json:"outcome,omitempty"`
	// Detail is a failure's own first line, and only a failure's: knowing that a
	// call failed without knowing how is what makes a worker run it again.
	Detail string `json:"detail,omitempty"`
	// Source is the record the whole result can be read out of — the session
	// journal rather than the conversation store, because a store id is not
	// something read or grep can open (loop.go states the same rule for a fold
	// marker). The call id is the token to grep for.
	Source string `json:"source,omitempty"`
	From   string `json:"from,omitempty"`
	Depth  int    `json:"depth,omitempty"`
}

// AdmissionOutcome is the four answers there are about a call.
type AdmissionOutcome string

const (
	// AdmissionUnknown is the honest default: a result reached the transcript and
	// nothing in this process recorded whether it was a failure.
	AdmissionUnknown AdmissionOutcome = ""
	AdmissionOK      AdmissionOutcome = "ok"
	AdmissionFailed  AdmissionOutcome = "failed"
	// AdmissionUnanswered is a call with no result in the transcript at all — an
	// interrupted batch, a turn that died — and is rendered as neither success
	// nor failure.
	AdmissionUnanswered AdmissionOutcome = "unanswered"
)

// AdmissionContext is the working context one task is admitted with.
//
// Every field is exported and tagged because this is embedded in the checkpoint
// record (task_store.go): unexported fields serialise as `{}`, and a resumed
// task would read an empty document under a full heading.
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
// names survive across versions and meanings need not, and the result is the
// document every older checkpoint already gives — the contract alone.
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
// in the document; what they may claim is decided here.
//
// The quotes rule carries the safety of the feature: the lines were said rather
// than established, the selection is partial, and the work above is still the
// work. It deliberately does not rank quotes against the contract — a quoted
// line that contradicts the brief is news for the report, not a precedence rule
// for a worker to apply alone.
const (
	admissionQuotesHeading   = "SOME OF WHAT WAS SAID AROUND THIS WORK"
	admissionQuotesRule      = "A FEW LINES FROM THE CONVERSATION THIS CAME OUT OF, OLDEST FIRST — a bounded selection, not the whole record and not a list of your requirements. They are what was SAID, not what is true: check anything you are about to depend on, and grep the record named on the line for the whole of it. A later line may have replaced an earlier one. Where one of them plainly contradicts the work above, say so in your report rather than quietly choosing."
	admissionEvidenceHeading = "CALLS THAT HAVE ALREADY RUN"
	admissionEvidenceRule    = "The opening of what each one was called with — cut where you see a `…`, never the whole arguments — and what is known about how it ended. \"outcome unknown\" means nobody recorded the outcome, not that it went well. Nothing here says what a result MEANT: grep the call id in the record named on the line to read the whole of it."
)

// admissionQuotesSection and admissionEvidenceSection are the two lists as the
// worker reads them, and nothing at all when there is nothing to draw.
func admissionQuotesSection(context AdmissionContext) string {
	return admissionLines(len(context.Quotes), func(index int) string {
		return context.Quotes[index].line()
	})
}

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

// where points in the idiom task_brief.go's [originPointer] already uses: the
// tools that open the file, then the path. No source draws no pointer, because
// an identifier no tool can resolve is worse than none — a worker will try.
func (q AdmissionQuote) where() string {
	if strings.TrimSpace(q.Source) == "" {
		return ""
	}
	return "grep or read " + q.Source
}

// line is one handle: the tool, the opening of its input, then what is known
// about how it ended and where the whole result is.
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
		// Nothing to fetch, so no pointer: the call did not finish, or the record
		// ends before it did.
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
