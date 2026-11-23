package appwire

import (
	"strings"
	"unicode"
)

// ── crews.start: a team with its own manager conversation, and the global one ─
//
// crews.create grows an opt-in (StartManager) that makes ONE new manager
// conversation for the team and, when a goal is given, sends it as that
// manager's first message exactly once. crews.root.start is the person's
// explicit act that makes the root ("All teams") and its manager. Both are
// optional engine halves: an engine that is not a [CrewStarter] answers
// unknown_method before anything is made, so a goal is never dropped on the
// floor and a team is never made without the manager it was asked to carry.
// The engine's policy lives in cmd/codeaf/appteamstart.go; this file is shape,
// bounds and routing, and says nothing about where a surface puts any of it.

// FeatureCrewStart is hello's word for an engine that serves crews.create's
// start_manager and crews.root.start.
const FeatureCrewStart = "crews.start"

// The bounds of a started team. They are checked here, before the engine is
// asked, so no engine can be handed more than they allow.
const (
	CrewNameMax         = 120
	CrewGoalMax         = 32 << 10
	CrewStartMembersMax = 32
	CrewRequestIDMax    = 128
)

// The states of a started team's goal message. They say what the engine KNOWS
// and nothing more: a goal that may have reached the manager is never called
// unsent, and one that was not handed over is never called sent.
const (
	// GoalNone: no goal was given, so nothing was sent.
	GoalNone = "none"
	// GoalStarting: the manager's first turn is being started behind the answer;
	// its acceptance or failure arrives on the ordinary turn events.
	GoalStarting = "starting"
	// GoalNotSent: the team and its manager exist and the goal was provably
	// never handed to the manager (Error says why). It is not sent later by
	// itself; a person sends it to the manager as an ordinary message.
	GoalNotSent = "not_sent"
	// GoalUnknown: a repeat of a request whose goal may have been handed to
	// the manager. It is NEVER sent again; whether it was accepted is read from
	// the manager's entries, by the client id when one was given.
	GoalUnknown = "unknown"
)

// CrewGoal is the state of a started team's goal message.
type CrewGoal struct {
	State    string `json:"state"`
	ClientID string `json:"client_id,omitempty"`
	Error    string `json:"error,omitempty"`
}

// CrewStarted is what a started team adds to crews.create's reply: the request
// it answers, the one manager conversation (an agent id), and its goal.
// Replayed is a repeat of a request already claimed: nothing was made or sent.
type CrewStarted struct {
	RequestID string   `json:"request_id"`
	Replayed  bool     `json:"replayed,omitempty"`
	Manager   string   `json:"manager"`
	Goal      CrewGoal `json:"goal"`
}

// CrewStartResult is a started team and what was started with it.
type CrewStartResult struct {
	Crew  Crew
	Start CrewStarted
}

// CrewStarter is the optional engine half of crews.create's start_manager.
type CrewStarter interface {
	CrewStart(ask CrewCreate) (CrewStartResult, error)
}

// RootStartAsk is crews.root.start's params.
type RootStartAsk struct {
	Workspace string `json:"workspace,omitempty"`
	Model     string `json:"model,omitempty"`
}

// RootStartResult is crews.root.start's reply. Neither flag set means the root
// and its manager were already there and nothing was made.
type RootStartResult struct {
	Crew           Crew   `json:"crew"`
	Manager        string `json:"manager"`
	RootCreated    bool   `json:"root_created,omitempty"`
	ManagerCreated bool   `json:"manager_created,omitempty"`
}

// RootStarter is the optional engine half of crews.root.start.
type RootStarter interface {
	RootStart(ask RootStartAsk) (RootStartResult, error)
}

// startFieldsSet says a start-only field rode on a create without the opt-in.
func (p CrewCreate) startFieldsSet() bool {
	return p.ManagerStartsMembers || p.RequestID != "" || p.Said != nil || p.ClientID != "" ||
		len(p.Attachments) > 0 || p.Workspace != "" || p.Model != ""
}

// CheckStart is the shape of a start_manager create. It never reaches the
// engine malformed.
func (p CrewCreate) CheckStart() error {
	if !p.StartManager {
		if p.startFieldsSet() {
			return Errorf(CodeBadParams, "crews.create: those fields belong to start_manager, which was not set")
		}
		return nil
	}
	if p.From != "" {
		return Errorf(CodeBadParams, "start_manager makes a team from agents, not from a proposal")
	}
	name := strings.TrimSpace(p.Name)
	if name == "" || len(name) > CrewNameMax {
		return Errorf(CodeBadParams, "give the crew a name of at most %d bytes", CrewNameMax)
	}
	if p.Manager != "" {
		return Errorf(CodeBadParams, "start_manager makes the manager; do not name one")
	}
	if len(p.Agents) > CrewStartMembersMax {
		return Errorf(CodeBadParams, "a started crew picks at most %d agents, not %d", CrewStartMembersMax, len(p.Agents))
	}
	seen := map[string]bool{}
	for _, a := range p.Agents {
		if strings.TrimSpace(a) == "" || len(a) > 128 || seen[a] {
			return Errorf(CodeBadParams, "the picked agents must be distinct, non-empty ids")
		}
		seen[a] = true
	}
	if p.ManagerStartsMembers && len(p.Agents) > 0 {
		return Errorf(CodeBadParams, "when its manager starts the members, no agents are picked")
	}
	if !p.ManagerStartsMembers && len(p.Agents) == 0 {
		return Errorf(CodeBadParams, "pick at least one agent, or let the manager start its members")
	}
	if !plainText(p.RequestID, CrewRequestIDMax) {
		return Errorf(CodeBadParams, "start_manager needs a request_id of at most %d bytes without control characters", CrewRequestIDMax)
	}
	if len(p.Goal) > CrewGoalMax || (p.Said != nil && len(*p.Said) > CrewGoalMax) {
		return Errorf(CodeBadParams, "a goal is at most %d bytes", CrewGoalMax)
	}
	if strings.IndexFunc(p.ClientID, unicode.IsControl) >= 0 || len(p.ClientID) > 256 {
		return Errorf(CodeBadParams, "client_id is at most 256 bytes without control characters")
	}
	if len(p.Workspace) > 4096 || len(p.Model) > 256 || strings.ContainsRune(p.Workspace, 0) {
		return Errorf(CodeBadParams, "workspace or model is too long")
	}
	if err := CheckAttachSources(p.Attachments); err != nil {
		return err
	}
	if strings.TrimSpace(p.Goal) == "" && len(p.Attachments) > 0 {
		return nil
	}
	if strings.TrimSpace(p.Goal) == "" && (p.Said != nil && strings.TrimSpace(*p.Said) != "" || p.ClientID != "") {
		return Errorf(CodeBadParams, "said and client_id belong to a goal, and no goal was given")
	}
	return nil
}

func startCrew(c Crewer, p CrewCreate) (*crewResult, error) {
	if err := p.CheckStart(); err != nil {
		return nil, err
	}
	s, ok := c.(CrewStarter)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not start a crew's manager")
	}
	got, err := s.CrewStart(p)
	if err != nil {
		return nil, err
	}
	out, err := crewOf(got.Crew, nil)
	if err != nil {
		return nil, err
	}
	started := got.Start
	out.Start = &started
	return out, nil
}

func startRoot(c Crewer, p RootStartAsk) (*RootStartResult, error) {
	s, ok := c.(RootStarter)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve crews.root.start")
	}
	if len(p.Workspace) > 4096 || len(p.Model) > 256 || strings.ContainsRune(p.Workspace, 0) {
		return nil, Errorf(CodeBadParams, "workspace or model is too long")
	}
	got, err := s.RootStart(p)
	if err != nil {
		return nil, err
	}
	got.Crew.Members = nonNil(got.Crew.Members)
	return &got, nil
}
