package appwire

import (
	"reflect"
	"time"
)

// COMPACTION ON THE WIRE (PROTOCOL.md): what a conversation weighs against its
// window, and each compaction pass as facts. The app words all of it; nothing
// here is a sentence for a person.

// AgentContext is what a conversation weighs against the window it runs in.
// Window and Threshold are absent when nothing has said what the window is,
// which the app shows as unknown rather than as room. The whole object is
// absent for a conversation nothing holds.
type AgentContext struct {
	Tokens    int `json:"tokens"`
	Window    int `json:"window,omitempty"`
	Threshold int `json:"threshold,omitempty"`
	// Summarizing says a summary request is in flight right now.
	Summarizing bool `json:"summarizing,omitempty"`
}

// The states of a compaction entry.
const (
	CompactionSummarizing = "summarizing"
	CompactionSettled     = "settled"
	CompactionFailed      = "failed"
)

// KeptLimit bounds the summary text one entry carries. A longer one is cut
// there and KeptClipped is set, so a copy of it can say it is not whole.
const KeptLimit = 64 << 10

// Compaction is one compaction pass, on its summary entry. Seq numbers the
// conversation's passes from 1 and is the same live and read back, so the
// entry's id is the same too. State is summarizing while a summary is being
// written, failed when one was asked for and did not land, and settled
// otherwise. Stubbed, Folded and Summarized are what the pass did; a pass with
// Summarized zero wrote no summary. Kept is the summary the model now reads
// in place of the messages it replaced, Model the model that wrote it, Turns
// how many of the person's messages it stands for, between CoversFrom and
// CoversUntil. Failed is why a summary did not land (interrupted, declined,
// empty, changed, not_shorter, error) and Full that the conversation is still
// at or over its window.
type Compaction struct {
	Seq          int        `json:"seq"`
	State        string     `json:"state" enum:"summarizing,settled,failed"`
	TokensBefore int        `json:"tokens_before,omitempty"`
	TokensAfter  int        `json:"tokens_after,omitempty"`
	Stubbed      int        `json:"stubbed,omitempty"`
	Folded       int        `json:"folded,omitempty"`
	Summarized   int        `json:"summarized,omitempty"`
	Kept         string     `json:"kept,omitempty"`
	KeptClipped  bool       `json:"kept_clipped,omitempty"`
	Model        string     `json:"model,omitempty"`
	Turns        int        `json:"turns,omitempty"`
	CoversFrom   *time.Time `json:"covers_from,omitempty"`
	CoversUntil  *time.Time `json:"covers_until,omitempty"`
	Failed       string     `json:"failed,omitempty" enum:"interrupted,declined,empty,changed,not_shorter,error"`
	Full         bool       `json:"full,omitempty"`
}

// EventCompactionEnded ends every agents.compact the app asked for, once,
// after whatever entries the pass said: compacted (the pass ran; its entries
// say what it did, and an engine too old to say them sends none), busy
// (another pass was already running), nothing (nothing older to compact) or
// error (the pass could not be asked for or did not answer; Message says how).
const EventCompactionEnded = "compaction.ended"

// The outcomes of an agents.compact.
const (
	CompactionCompacted = "compacted"
	CompactionBusy      = "busy"
	CompactionNothing   = "nothing"
	CompactionError     = "error"
)

// CompactionEnded is that event's data.
type CompactionEnded struct {
	Agent   string `json:"agent"`
	Outcome string `json:"outcome" enum:"compacted,busy,nothing,error"`
	Message string `json:"message,omitempty"`
}

// Compactor is an engine that compacts a conversation when asked. It is
// asserted, as Tidier is, so an engine without it answers unknown_method.
type Compactor interface {
	// CompactAgent starts one compaction pass and answers at once, or
	// refuses busy while one it started is still running. The pass says
	// itself on entry.added and always ends on compaction.ended.
	CompactAgent(id string) error
}

func init() {
	eventShapes[EventCompactionEnded] = reflect.TypeFor[CompactionEnded]()
	methods["agents.compact"] = typed(func(e Engine, p crewID) (*Empty, error) {
		c, ok := e.(Compactor)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve agents.compact")
		}
		if err := needID("agents.compact", p.ID); err != nil {
			return nil, err
		}
		return done(c.CompactAgent(p.ID))
	})
}
