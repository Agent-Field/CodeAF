package session

// THE SEAT: WHICH TIER'S MODEL ANSWERED, IN ONE WORD.
//
// A model id is an address (usage_ledger.go). The SEAT is the chair the call
// ran in — which class of model the person's settings put there. A spend page
// can already say what a day cost and which model took the money; without a
// seat it cannot say whether the money went on the seat that does the work or
// the seat that thinks. The vocabulary is internal/roles' five tiers — the
// classes the person actually configures — plus `judge`, the seat a run's
// judge is billed to, and `talk`, the one kind of call no tier governs: a
// conversation's own turns.
//
// THE WORDS ARE DERIVED, NEVER TYPED AT A CALL SITE. The registry already
// knows which tier a role sits on ([roles.TierOf]), so a call that went
// through it is seated by one hop, and the only hand-written table here is
// the five tiers to the five words ([SeatOfTier]). A seat is a fact about
// which tier's model answered, not a judgement about the call, and the seven
// words are the whole vocabulary: no free text ever reaches the field
// ([TagUsage] drops anything else).

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/roles"
)

// Seat is one word of the closed vocabulary a usage row's seat field carries.
type Seat string

const (
	// SeatReflex is the cheapest of all: the two calls made every single turn.
	SeatReflex Seat = "reflex"
	// SeatLow is the cheap, fast one: a title, a caption, the guardian.
	SeatLow Seat = "low"
	// SeatWorker is the tier that DOES THE WORK: a task node's own turns.
	SeatWorker Seat = "worker"
	// SeatHigh is the capable, expensive one: the auditor, a repair round.
	SeatHigh Seat = "high"
	// SeatMastermind is the tier whose one answer shapes all the others: the
	// planner, the designer, a division review.
	SeatMastermind Seat = "mastermind"
	// SeatJudge is the seat a run's judge is billed to, mapped from no tier
	// and no role.
	SeatJudge Seat = "judge"
	// SeatTalk is a conversation's own turns — the one kind of call no tier
	// governs, because the person is sitting in it and its model is theirs to
	// change mid-sentence.
	SeatTalk Seat = "talk"
)

// Seats is the whole vocabulary, cheapest first, talk last. Talk closes the
// list rather than opening it because it is not in the tier economy at all: it
// is the word for the turns a conversation makes for itself, and a settings
// surface that drew it as a tier would be drawing a dial that answers nothing.
var Seats = []Seat{SeatReflex, SeatLow, SeatWorker, SeatHigh, SeatMastermind, SeatJudge, SeatTalk}

// SeatValid reports whether a word is one of the seven. It compares EXACTLY: no
// trimming, no case folding. A word from outside the set is not a seat and is
// never written to a row — [TagUsage] drops it rather than writing a guess.
func SeatValid(word string) bool {
	for _, seat := range Seats {
		if string(seat) == word {
			return true
		}
	}
	return false
}

// SeatOfTier maps each of [roles.Tiers] to its own word, and reports false for
// anything else. No tier maps to talk, because talk is what a conversation's
// own turns answer under and no tier governs those — and none maps to judge
// either, because judge is the seat a run's judge is billed to and no tier
// holds that.
func SeatOfTier(tier roles.Tier) (Seat, bool) {
	switch tier {
	case roles.TierReflex:
		return SeatReflex, true
	case roles.TierLow:
		return SeatLow, true
	case roles.TierWorker:
		return SeatWorker, true
	case roles.TierHigh:
		return SeatHigh, true
	case roles.TierMastermind:
		return SeatMastermind, true
	}
	return "", false
}

// SeatOfRole is the seat a registered role's calls bill to: [roles.TierOf]
// followed by [SeatOfTier], one hop through the registry rather than a second
// hand-written table. A role the registry never had is nobody's seat — false,
// and the row carries no word rather than a guess.
func SeatOfRole(role roles.Role) (Seat, bool) {
	tier, ok := roles.TierOf(role)
	if !ok {
		return "", false
	}
	return SeatOfTier(tier)
}

// AgentKind is what an agent IS, in the one question a seat needs answered:
// whose turns are these?
type AgentKind string

const (
	// AgentChat is a conversation somebody sits in — and every agent that is
	// neither a node, a checker nor a repair round, because that is what they
	// all are: a session answering for itself.
	AgentChat AgentKind = "chat"
	// AgentTask is one task node's runner (task_run.go).
	AgentTask AgentKind = "task"
	// AgentAudit is the read-only judge a node's landing waits on
	// (task_audit.go's [Agent.newAuditAgent]).
	AgentAudit AgentKind = "audit"
	// AgentRepair is one repair round's fresh worker
	// (task_audit.go's [Agent.repairNode]).
	AgentRepair AgentKind = "repair"
)

// SeatOfAgent is the seat an agent's OWN turns bill to: chat is talk, task is
// worker, audit and repair are high. An unknown kind is false, and the row it
// would have carried stays wordless rather than guessed.
func SeatOfAgent(kind AgentKind) (Seat, bool) {
	switch kind {
	case AgentChat:
		return SeatTalk, true
	case AgentTask:
		return SeatWorker, true
	case AgentAudit, AgentRepair:
		return SeatHigh, true
	}
	return "", false
}

// agentKind is what THIS agent is, from the three facts its config carries:
// the crew role it answers for (the checker, [Config.crewRole]), the repair
// marker its builder set ([Config.repairRound]), the node flag — and, the
// default, none of those: a conversation, or an agent standing in for one.
func (a *Agent) agentKind() AgentKind {
	switch {
	case a.config.crewRole != "":
		return AgentAudit
	case a.config.repairRound:
		return AgentRepair
	case a.config.InTask:
		return AgentTask
	}
	return AgentChat
}

// TagUsage writes a row's two name fields, and is the ONE door they go
// through. Nothing else on the line is touched.
//
// A non-empty role is written to UsageLine.Role; an empty role leaves whatever
// was there. The seat argument wins when it is one of the seven words; a word
// outside the set is dropped rather than written, and the seat then falls back
// to the role's own tier ([SeatOfRole]); if that fails too, whatever seat the
// line already carried stays — which is what keeps a caller that knows nothing
// about a row from overwriting a word somebody else wrote onto it.
func TagUsage(line UsageLine, role roles.Role, seat Seat) UsageLine {
	if name := strings.TrimSpace(string(role)); name != "" {
		line.Role = name
	}
	if SeatValid(string(seat)) {
		line.Seat = seat
	} else if derived, ok := SeatOfRole(role); ok {
		line.Seat = derived
	}
	return line
}
