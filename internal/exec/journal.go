package exec

import (
	"encoding/json"
	"strings"
	"time"
)

// THE JOURNAL: every host call, written down before its answer goes back.
//
// PRD §5 makes this a law rather than a feature, and the reason is that one
// record has to serve four readers who would otherwise each grow their own:
// the progress feed a person watches while a run is going, the resume point a
// stopped run continues from, the evidence a later revision is argued from, and
// the "how it went" line the list draws afterwards. The old system saved a whole
// trace per run and had NO non-test reader of it (internal/subharness's
// Store.Runs), which is how learning quietly became impossible — so the two
// things this contract insists on are that the journal is written INCREMENTALLY
// and that it is actually read.
//
// A HOST CALL IS JOURNALED BEFORE IT RETURNS. Not after the run, not on the way
// out: the entry is the fact that the call happened, and a run killed halfway
// has to leave behind exactly the calls it made. The writer is the runtime
// lane's; this file is the shape it writes and the door it writes through.

// JournalEntry is one host call, whole.
type JournalEntry struct {
	// Seq counts from one within a run, so a reader can order entries without a
	// clock. Two entries written in the same millisecond are still in order.
	Seq int       `json:"seq"`
	At  time.Time `json:"at"`
	// Call is which door was used: "ai", "tool", "ask", "remember", "recall" or
	// "log". The constants are below, and they are spelled the way the program
	// spells them so a person reading a journal and a person reading the bundle
	// are reading one vocabulary.
	Call string `json:"call"`
	// Ref is what the call was about, in one string: the prompt asset an ai()
	// named, the tool a tool() asked for, the question an ask() put, the note a
	// remember() kept. It is what a compact progress row is drawn from.
	Ref string `json:"ref,omitempty"`
	// Input and Output are the call's two halves as JSON. THEY ARE NOT CLIPPED
	// HERE: clipping is a decision about a surface, and a journal that had
	// already thrown the material away could not be the resume point it is also
	// meant to be. A writer with a size law of its own applies it on the way to
	// disk and says so.
	Input  json.RawMessage `json:"input,omitempty"`
	Output json.RawMessage `json:"output,omitempty"`
	// Spend is what this one call cost. Summed across a run's entries it is the
	// run's whole ledger, which is the only reason the ledger and the journal
	// can never disagree.
	Spend Spend `json:"spend,omitzero"`
	// Note is the one line a person reads for this row, in a person's words. It
	// is where a log() status lands, and it is what a surface draws when it has
	// room for one line and not for a call.
	Note string `json:"note,omitempty"`
	// Err is what went wrong, when something did. A call that failed is still a
	// call that happened and still costs what it spent before it failed.
	Err     string        `json:"err,omitempty"`
	Elapsed time.Duration `json:"elapsed,omitempty"`
}

// The six doors, in the spelling a bundle uses. They are constants so that a
// surface filtering a journal for model calls and the host writing them are
// reading one string, and a rename is one edit.
const (
	CallAI       = "ai"
	CallTool     = "tool"
	CallAsk      = "ask"
	CallRemember = "remember"
	CallRecall   = "recall"
	CallLog      = "log"
)

// Journal is where a run's entries go. It is one method because it has one job,
// and the incremental writer behind it — the file, the flush discipline, the
// progress events it raises on the way past — is the runtime lane's to build.
//
// A NIL JOURNAL IS A RUN NOBODY IS WATCHING, which is a real case: a guard check
// before the run, a headless invocation whose caller wants only the answer. Use
// [Record] rather than calling Write on a possibly-nil interface, so that no
// host implementation has to carry the nil check itself.
type Journal interface {
	Write(entry JournalEntry) error
}

// Record writes one entry through a journal that may not be there. It is the
// door every host call uses, so "journaled before it returns" is one line at
// each call site and cannot be forgotten differently in six places.
func Record(journal Journal, entry JournalEntry) error {
	if journal == nil {
		return nil
	}
	return journal.Write(entry)
}

// Spend is what one call, or one whole run, cost.
//
// THE FIGURES ARE THE LEDGER'S, field for field: internal/subharness/usage.go
// carries exactly these — the model the provider named, the call count, the two
// token counts, the two cache counts, and the provider's own dollar figure — and
// the session folds them through [Agent.foldHarnessUsage] into the auxiliary
// door. Reading them here in the same spelling is what keeps one run from having
// two accounts of itself.
//
// It is a type in this package rather than that one for a reason worth stating,
// because it is the one place this contract does not simply reuse what exists:
// the ledger's fold methods are unexported, so nothing outside internal/
// subharness can add a call to one. The goja host and every Go runner live
// outside it. So the fields are the ledger's and the doors are exported, and
// the session's fold takes either.
//
// NOTHING HERE LOCKS, which is the same law the ledger states about itself and
// for the same reason: host calls are made from the one goroutine a run has. A
// runtime that ever ran them concurrently owns this type's safety along with
// everything else it changed about running a program.
type Spend struct {
	// Model is what answered, as the PROVIDER reported it, and it is empty when
	// two calls disagreed — a run whose steps pinned models of their own is not
	// a run one name describes, and choosing one of them would be this package
	// stating a fact nobody gave it.
	Model string `json:"model,omitempty"`
	// Calls is every request made, the intermediate rounds of a tool loop
	// included. A call the provider reported no usage for is still counted: a
	// missing count is not a call that did not happen.
	Calls int `json:"calls,omitempty"`
	// Input and Output are the provider's token counts, with cache reads beside
	// the prompt count rather than inside it — the session's own accounting
	// keeps them in exactly this shape.
	Input  int `json:"input,omitempty"`
	Output int `json:"output,omitempty"`
	// CacheRead and CacheWrite are the prompt-cache accounting.
	CacheRead  int `json:"cache_read,omitempty"`
	CacheWrite int `json:"cache_write,omitempty"`
	// CostUSD is the provider's OWN figure, summed. Zero is "the provider did not
	// say", which is not the same fact as free — and under the emptiness law a
	// surface that cannot tell them apart draws nothing rather than $0.00.
	CostUSD float64 `json:"cost_usd,omitempty"`

	// mixed remembers that two calls named different models, so a third call
	// agreeing with the first cannot quietly restore a name the run has already
	// outgrown.
	mixed bool
}

// Reported says whether the provider gave this spend any accounting at all. It
// is the question asked before billing anybody: ten calls to an endpoint that
// publishes no usage leave every field zero, and folding that into a session
// total would be this build claiming the work was free.
func (s Spend) Reported() bool {
	return s.Input != 0 || s.Output != 0 || s.CacheRead != 0 || s.CacheWrite != 0 || s.CostUSD != 0
}

// Add folds one model call in. A nil receiver is a caller that is not counting,
// which is every guard check and every runner that spends nothing.
func (s *Spend) Add(other Spend) {
	if s == nil {
		return
	}
	s.Calls += other.Calls
	s.note(other.Model)
	s.Input += other.Input
	s.Output += other.Output
	s.CacheRead += other.CacheRead
	s.CacheWrite += other.CacheWrite
	s.CostUSD += other.CostUSD
	if other.mixed {
		s.Model, s.mixed = "", true
	}
}

// note records which model answered and forgets the name the moment two calls
// disagree. A call that named no model says nothing either way — an endpoint
// that echoes no model back is not evidence that a run changed models.
func (s *Spend) note(model string) {
	if s.mixed {
		return
	}
	model = strings.TrimSpace(model)
	switch {
	case model == "":
		return
	case s.Model == "":
		s.Model = model
	case s.Model != model:
		s.Model, s.mixed = "", true
	}
}

// Mixed reports that this run's calls did not agree on a model, which is a
// different fact from having no model at all. A surface that wants to say "on
// several models" rather than nothing asks this.
func (s Spend) Mixed() bool { return s.mixed }
