package session

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/approval"
)

// ── THE CONVERSATION'S OWN POSTURE ON THE TOOL GATE ─────────────────────────
//
// The gate had two scopes and neither was the one a person standing in a
// conversation reaches for. The settings rows (`tools.approvalMode`, its two
// exception rows and the guardian) are the INSTALL'S answer, persisted, and a
// change there lands wherever the rows are next read. `--yolo` is the LAUNCH'S
// answer: it opens the gate for the whole process and writes nothing down. What
// was missing is the scope the thinking rung already has (effort.go) — THIS
// CONVERSATION, moved from inside it, kept in its own meta.json so that a
// person who opened the gate here, closed the terminal and came back finds it
// where they left it.
//
// THE POSTURES ARE A LADDER OF INCREASING AUTONOMY AND NOT THE ROW'S ENUM. The
// row says prompt, allow or deny; a wheel that walked those three would put
// "refuse every call" one press past "run every call", which is a wheel that
// breaks a session by accident. So the ladder the surface walks is
//
//	ask       every call the rules say to ask about is asked about
//	guardian  a small model answers the plainly-safe ones, you get the rest
//	allow     the gate is open — the same posture --yolo opens
//
// with `deny` reachable by NAME and never by a press, and `auto` the way back
// to whatever the settings rows say. A posture is a WORD FOR A PAIR — the
// blanket mode and whether the guardian stands in — and the guardian's row is
// folded into the ladder because the settings panel already positions it as a
// filter in front of the mode rather than as a fourth mode.
//
// THE ENGINE HOLDS THE WORD AND SOMEBODY ELSE BUILDS THE GATE. A policy is built
// from three settings rows resolved through the project layer, which is
// cmd/codeaf's knowledge and not this package's (approvalgate.go says why the
// gate is pushed rather than re-read). So the door onto the rows is handed in
// as [Config.ApprovalGate], and [Agent.SetApprovalPosture] asks it for the
// finished policy the way a consent card's banked rule already does — the same
// v3Policy, the same rows, the same floors. A session with no such door has no
// dial: the surface asks [Agent.ApprovalDial] and draws no control, which is
// the design law that a capability which cannot work is absent rather than
// broken.

// The postures, spelled once. They are the words `/approvals` takes and the
// words meta.json keeps; what a PERSON reads for each is the surface's
// (internal/tui3's approvalchip.go).
const (
	// PostureAsk asks about every call the rules say to ask about, and stands
	// the guardian down even where the settings row has it up: a person who
	// chose "ask" chose to be asked.
	PostureAsk = "ask"
	// PostureGuardian asks, with the small model answering the plainly-safe
	// calls first (guardian.go).
	PostureGuardian = "guardian"
	// PostureAllow is the open gate: the blanket answer becomes allow, exactly
	// as `--yolo` makes it. The floors hold under it as they hold under the flag.
	PostureAllow = "allow"
	// PostureDeny refuses every call the rules do not name. It is a posture a
	// person chooses on purpose and it is never a stop on the wheel.
	PostureDeny = "deny"
	// PostureAuto hands the conversation back to the settings rows as they
	// stand. It is a stored value and not absence, because absence means "never
	// touched" and a launch flag still speaks for an untouched conversation.
	PostureAuto = "auto"
)

// ApprovalWheel is the wheel's stops in walking order. `deny` and `auto` are
// deliberately not on it — see the header.
var ApprovalWheel = []string{PostureAsk, PostureGuardian, PostureAllow}

// ApprovalPostures is every word the door takes, the wheel first.
var ApprovalPostures = []string{PostureAsk, PostureGuardian, PostureAllow, PostureDeny, PostureAuto}

// ApprovalGate is the door onto the settings rows, handed in by whoever built
// the session (cmd/codeaf's chatv3_approval.go).
type ApprovalGate interface {
	// Build is the gate for one posture: the finished policy and whether the
	// guardian stands in. An empty posture is the rows exactly as they stand.
	Build(posture string) (*approval.Policy, bool, error)
	// Standing is the posture the rows as they stand amount to, in the ladder's
	// own words — what an untouched conversation on this install is running at.
	Standing() string
}

// errNoApprovalDial is what a session with no door onto its gate answers.
var errNoApprovalDial = errors.New("this session has no dial onto what runs without asking")

// ApprovalDial reports whether this session can move its own gate at all.
func (a *Agent) ApprovalDial() bool { return a.config.ApprovalGate != nil }

// ApprovalPosture is the posture THIS conversation was set to, "" when nobody
// has set one. It is the stored word and not the resolved one, for
// [Agent.ConversationEffort]'s reason.
func (a *Agent) ApprovalPosture() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.approvalPosture
}

// ResolvedApprovalPosture is the posture the gate is actually standing at,
// whichever scope decided it: the conversation's own word, else the launch's
// (`--yolo`), else the settings rows as they stand. It is what a surface draws.
//
// `auto` RESOLVES THROUGH TO THE ROWS and never reads as a word of its own: a
// conversation handed back to the rows is running at whatever they say, and
// that is the fact a person standing at the chip needs.
func (a *Agent) ResolvedApprovalPosture() string {
	a.mu.Lock()
	stored := a.approvalPosture
	a.mu.Unlock()
	switch {
	case stored != "" && stored != PostureAuto:
		return stored
	case stored == "" && a.config.ApprovalPosture != "":
		return a.config.ApprovalPosture
	case a.config.ApprovalGate != nil:
		return a.config.ApprovalGate.Standing()
	}
	return ""
}

// SetApprovalPosture moves this conversation's gate to one posture and writes
// the word down. It is sticky the way the rung is: the word lands in the
// session folder's meta.json (placemeta.go) and cmd/codeaf sets it again on
// resume, so the gate a person opened here is open when they come back.
//
// A CALL IN FLIGHT KEEPS THE GATE IT WAS DECIDED UNDER, which is approvalgate.go's
// bargain: the policy pointer is swapped, never edited, so a decision already
// taken was taken about a rule set that really was in force.
//
// A REBUILD THAT FAILS MOVES NOTHING. The word is not stored and the gate
// already standing stays standing, for the reason [Agent.SetApprovalPolicy]
// refuses a nil: the answer to "I could not read the rules" is never a session
// whose posture and gate disagree.
func (a *Agent) SetApprovalPosture(posture string) error {
	word := strings.ToLower(strings.TrimSpace(posture))
	if !knownPosture(word) {
		return fmt.Errorf("%q is not a posture. Use one of: %s", posture, strings.Join(ApprovalPostures, ", "))
	}
	gate := a.config.ApprovalGate
	if gate == nil {
		return errNoApprovalDial
	}
	build := word
	if build == PostureAuto {
		build = ""
	}
	policy, guardian, err := gate.Build(build)
	if err != nil {
		return err
	}
	if policy == nil {
		return errors.New("the rules could not be rebuilt")
	}
	a.mu.Lock()
	a.approvalPolicy = policy
	a.approvalPosture = word
	a.guardianOverride = &guardian
	a.mu.Unlock()
	// Written through the same door every other fact about this conversation is
	// written through, so the file keeps one shape and one writer.
	a.stampApproval()
	return nil
}

// RebuildApprovalGate builds the gate again for the posture in force and pushes
// it — what a consent card's banked rule and a dropped one call after they have
// changed the rows underneath (cmd/codeaf's chatv3_approval.go).
//
// THE POSTURE IN FORCE IS THE RESOLVED ONE, so a rule banked inside a
// conversation that was walked to `allow` lands on an `allow` gate and not on
// the rows' own, and a rule banked under `--yolo` keeps the flag's open gate
// exactly as it did before this scope existed.
func (a *Agent) RebuildApprovalGate() error {
	gate := a.config.ApprovalGate
	if gate == nil {
		return errNoApprovalDial
	}
	a.mu.Lock()
	stored := a.approvalPosture
	a.mu.Unlock()
	build := stored
	switch {
	case stored == "":
		build = a.config.ApprovalPosture
	case stored == PostureAuto:
		build = ""
	}
	policy, guardian, err := gate.Build(build)
	if err != nil {
		return err
	}
	if policy == nil {
		return errors.New("the rules could not be rebuilt")
	}
	a.mu.Lock()
	a.approvalPolicy = policy
	if stored != "" {
		a.guardianOverride = &guardian
	}
	a.mu.Unlock()
	return nil
}

// guardianOn is whether the small model stands in on this session right now:
// the conversation's own posture where one has been set, and the launch's
// config where none has.
func (a *Agent) guardianOn() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.guardianOverride != nil {
		return *a.guardianOverride
	}
	return a.config.Guardian
}

// stampApproval writes the posture down the moment it changes, for
// [Agent.stampEffort]'s reason: a person who opens the gate and closes the
// terminal before saying anything else must find it open when they come back.
func (a *Agent) stampApproval() { a.stampMeta() }

func knownPosture(word string) bool {
	for _, known := range ApprovalPostures {
		if word == known {
			return true
		}
	}
	return false
}
