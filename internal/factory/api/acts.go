package api

import (
	"bytes"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── PUBLIC ACTS ON THE WIRE ─────────────────────────────────────────────────
//
// Three job doors, each `POST /v1/jobs/{id}/<verb>`:
//
//   - act proposes public acts on the job's own pull request. Each is carried
//     out at once when its proposer may (the person themselves, or the job's
//     manager in Auto), and otherwise waits for the person, and the job reads
//     `wait: {who: you, action: approve_acts}`.
//   - acts_approve is the person's one yes to every act that waits.
//   - acts_decline is their one no, with words for the manager.
//
// The job carries its acts as `acts` (factory.PublicAct), so a reader shows
// exactly what will be posted or changed before the yes, and what became of
// each after.

// The three verbs.
const (
	ActVerb         = "act"
	ActsApproveVerb = "acts_approve"
	ActsDeclineVerb = "acts_decline"
)

// ActAsk is one public act as it is proposed. Kind is one of the
// factory.ActKind words; the other fields are that kind's own:
//
//	edit_pr           title and/or body
//	change_base       base
//	reply             thread (a feedback item's id, such as inline:<comment id>) and text
//	comment           text
//	resolve_thread    thread
//	rerequest_review  reviewers (empty asks everyone who has reviewed)
//	rerun_checks, mark_ready, push, open_pr, merge   nothing more
type ActAsk struct {
	Kind      string   `json:"kind"`
	Title     string   `json:"title,omitempty"`
	Body      string   `json:"body,omitempty"`
	Base      string   `json:"base,omitempty"`
	Thread    string   `json:"thread,omitempty"`
	Text      string   `json:"text,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
}

// ActsRequest is the body of act. By is who proposes ("main_agent" or
// "you"), set by the trusted caller; Why is their reason, kept with each act.
type ActsRequest struct {
	Acts   []ActAsk    `json:"acts"`
	By     string      `json:"by"`
	Why    string      `json:"why,omitempty"`
	Expect *ActsExpect `json:"expect,omitempty"`
}

// ActsApproveRequest is the body of acts_approve. IDs, when given, are the
// acts the person was shown: a set that has changed since is refused instead
// of approved unseen.
type ActsApproveRequest struct {
	IDs    []string    `json:"ids,omitempty"`
	Expect *ActsExpect `json:"expect,omitempty"`
}

// ActsDeclineRequest is the body of acts_decline. Words is the person's
// reason, kept on each declined act for the job's manager to read.
type ActsDeclineRequest struct {
	Words  string      `json:"words,omitempty"`
	Expect *ActsExpect `json:"expect,omitempty"`
}

// ActsExpect is the job the caller was looking at: a bare number is its
// version, with 0 for no expectation, and an object is an ordinary [Expect].
type ActsExpect struct{ Expect }

// UnmarshalJSON takes the version as a number or the whole expectation.
func (e *ActsExpect) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '{' {
		return json.Unmarshal(data, &e.Expect)
	}
	var version int
	if err := json.Unmarshal(data, &version); err != nil {
		return err
	}
	e.Expect = Expect{}
	if version > 0 {
		e.Version = &version
	}
	return nil
}

// Guard is the expectation as the doors read one, nil for none.
func (e *ActsExpect) Guard() *Expect {
	if e == nil || e.Expect == (Expect{}) {
		return nil
	}
	return &e.Expect
}

// How one proposed act was answered, as [ActResult.State]. The states of a
// stored act (factory.ActProposed and the rest) are answers too.
const (
	// ActWaitingOnYou is a push whose approval already waits on the person.
	ActWaitingOnYou = "waiting_on_you"
	// ActPressed is a push whose waiting approval was pressed.
	ActPressed = "pressed"
	// ActOffered is a merge offered to the person, who alone merges.
	ActOffered = "offered"
	// ActNotOffered is a merge the job cannot offer yet, with why.
	ActNotOffered = "not_offered"
)

// ActResult is the answer to one proposed act, in the order proposed.
type ActResult struct {
	// ID is the act's id on the job, for a stored act.
	ID    string `json:"id,omitempty"`
	Kind  string `json:"kind"`
	State string `json:"state"`
	// Words says it in a line.
	Words string `json:"words,omitempty"`
}

// ActsAnswer is what each of the three doors answers: what became of each
// act, the acts that now wait for the person in one line, and the job's acts
// and wait as they stand.
type ActsAnswer struct {
	Factory Factory     `json:"factory"`
	Job     int         `json:"job"`
	Results []ActResult `json:"results,omitempty"`
	// Waiting is the acts that wait for the person, said in one line, ""
	// when none does.
	Waiting string              `json:"waiting,omitempty"`
	Acts    []factory.PublicAct `json:"acts,omitempty"`
	Wait    *factory.Wait       `json:"wait,omitempty"`
}
