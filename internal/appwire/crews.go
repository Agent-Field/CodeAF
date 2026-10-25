package appwire

import (
	"strings"
	"time"
)

// Wave 5: crews (PROTOCOL.md). A crew IS a codeaf team: its fields are the
// team's, read from the same store, renamed only where the app's words differ.
// The few the design needs and teams do not keep are the app layer's, and are
// commented so here.

// The crews' events.
const (
	EventCrewLog         = "crew.log"
	EventDecisionOpened  = "decision.opened"
	EventDecisionChanged = "decision.changed"
	EventMemberChanged   = "member.changed"
)

// A crew's states.
const (
	CrewWorking  = "working"
	CrewNeedsYou = "needs_you"
	CrewIdle     = "idle"
	CrewClosed   = "closed"
)

// SubkindCrewDecision is a decision packet waiting on the person, as an ask;
// SubkindCrewProposal is an agent's crew proposal (app layer).
const (
	SubkindCrewDecision = "crew_decision"
	SubkindCrewProposal = "crew_proposal"
	// SubkindOrderRun is a standing order's run asking before it goes.
	SubkindOrderRun = "order_run"
)

// The answers a crew proposal offers.
const (
	ProposalCreate = "create"
	ProposalEdit   = "edit"
	ProposalKeep   = "keep"
)

// CrewSettings are a crew's own overrides, each nil when it inherits.
type CrewSettings struct {
	QuestionsUp *bool    `json:"questions_up,omitempty"`
	CapUSDDay   *float64 `json:"cap_usd_day,omitempty"`
	DepthLimit  *int     `json:"depth_limit,omitempty"`
	SubShare    *float64 `json:"sub_share,omitempty"`
	Wake        *bool    `json:"wake,omitempty"`
}

// CrewOrigin is where an effective setting comes from.
type CrewOrigin struct {
	Kind string `json:"kind"`
	Crew string `json:"crew,omitempty"`
	Name string `json:"name,omitempty"`
}

// CrewEffective is every setting in force, each with its origin.
type CrewEffective struct {
	QuestionsUp     bool       `json:"questions_up"`
	QuestionsUpFrom CrewOrigin `json:"questions_up_from"`
	CapUSDDay       float64    `json:"cap_usd_day"`
	CapFrom         CrewOrigin `json:"cap_from"`
	DepthLimit      int        `json:"depth_limit"`
	DepthFrom       CrewOrigin `json:"depth_from"`
	SubShare        float64    `json:"sub_share"`
	SubShareFrom    CrewOrigin `json:"sub_share_from"`
	Wake            bool       `json:"wake"`
	WakeFrom        CrewOrigin `json:"wake_from"`
}

// CrewDefaults are the five profile defaults every crew inherits from.
type CrewDefaults struct {
	QuestionsUp bool    `json:"questions_up"`
	CapUSDDay   float64 `json:"cap_usd_day"`
	DepthLimit  int     `json:"depth_limit"`
	SubShare    float64 `json:"sub_share"`
	Wake        bool    `json:"wake"`
}

// CrewSpend is a crew's spending today, every crew under it counted.
type CrewSpend struct {
	Day      string             `json:"day"`
	USD      float64            `json:"usd"`
	Calls    int                `json:"calls"`
	ByMember map[string]float64 `json:"by_member,omitempty"`
}

// CrewWrap is a wrap-up in progress.
type CrewWrap struct {
	Started time.Time `json:"started"`
	BoundS  int       `json:"bound_s"`
}

// CrewItem is one linked job, issue, pull request or file (app layer).
type CrewItem struct {
	ID    string    `json:"id"`
	Crew  string    `json:"crew"`
	Kind  string    `json:"kind"`
	Ref   string    `json:"ref"`
	Title string    `json:"title,omitempty"`
	At    time.Time `json:"at"`
}

// Crew is one team. IdleSince is the instant since which no member has run,
// on an idle or needs_you crew: the latest instant any member stopped (the end
// of its last turn, or when the question holding its open turn was put). It
// is absent while any member runs, on a closed crew, and when any member's
// instant is unknown.
type Crew struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
	Root   bool   `json:"root,omitempty"`
	// FactoryManaged is true only for a team the factory itself made or used
	// (the `factory` team and each item's team); absent otherwise, and on every
	// team stored before it was kept. It is never a guess from the name.
	FactoryManaged bool       `json:"factory_managed,omitempty"`
	Manager        string     `json:"manager,omitempty"`
	Hue            float64    `json:"hue"`
	Tier           int        `json:"tier"`
	Tint           string     `json:"tint"`
	MadeAt         *time.Time `json:"made_at,omitempty"`
	State          string     `json:"state"`
	IdleSince      *time.Time `json:"idle_since,omitempty"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
	// ArchivedAt is when a closed crew was put away; absent otherwise. The
	// crew's state stays closed: archived is closed and listed only when asked.
	ArchivedAt    *time.Time    `json:"archived_at,omitempty"`
	ClosedWith    string        `json:"closed_with,omitempty"`
	Report        string        `json:"report,omitempty"`
	Wrap          *CrewWrap     `json:"wrap,omitempty"`
	Settings      CrewSettings  `json:"settings"`
	Effective     CrewEffective `json:"effective"`
	Spend         CrewSpend     `json:"spend"`
	Members       []CrewMember  `json:"members"`
	FormerMembers []CrewMember  `json:"former_members,omitempty"`
	Brief         string        `json:"brief,omitempty"`
	// The app layer's.
	Goal    string     `json:"goal,omitempty"`
	EndsAt  *time.Time `json:"ends_at,omitempty"`
	History []string   `json:"history,omitempty"`
	// HistoryTasks is History with its owner: the tasks the founding manager
	// had when the crew was made, each with the agent that owned it then.
	HistoryTasks []CrewHistoryTask `json:"history_tasks,omitempty"`
	Items        []CrewItem        `json:"items,omitempty"`
}

// CrewHistoryTask is one task from before a crew was made. Task is its id in
// Owner's own task list; Owner is the agent that held it then and keeps it.
type CrewHistoryTask struct {
	Task  string `json:"task"`
	Owner string `json:"owner"`
}

// CrewReportsTo is the manager a member answers to.
type CrewReportsTo struct {
	Crew     string `json:"crew"`
	Manager  string `json:"manager"`
	Distance int    `json:"distance"`
}

// MemberWas is what a member was before its role or crew changed (app layer).
type MemberWas struct {
	Role string    `json:"role,omitempty"`
	Crew string    `json:"crew,omitempty"`
	At   time.Time `json:"at"`
}

// CrewMember is one conversation in a crew.
type CrewMember struct {
	Agent       string         `json:"agent"`
	Crew        string         `json:"crew,omitempty"`
	Handle      string         `json:"handle,omitempty"`
	HandleBy    string         `json:"handle_by,omitempty"`
	Title       string         `json:"title,omitempty"`
	Workspace   string         `json:"workspace,omitempty"`
	JoinedAt    *time.Time     `json:"joined_at,omitempty"`
	Started     bool           `json:"started,omitempty"`
	Home        bool           `json:"home,omitempty"`
	Independent bool           `json:"independent,omitempty"`
	ReportsTo   *CrewReportsTo `json:"reports_to,omitempty"`
	AlsoIn      []string       `json:"also_in,omitempty"`
	Leads       string         `json:"leads,omitempty"` // the child crew this member manages
	State       string         `json:"state,omitempty"`
	Line        string         `json:"line,omitempty"`
	SpentUSD    float64        `json:"spent_usd"`
	Model       string         `json:"model,omitempty"`
	// The app layer's.
	Role   string     `json:"role,omitempty"`
	CapUSD *float64   `json:"cap_usd,omitempty"`
	Paused bool       `json:"paused,omitempty"`
	Was    *MemberWas `json:"was,omitempty"`
}

// LogLine is one line of a crew's Traffic, as stored.
type LogLine struct {
	ID      string    `json:"id"`
	Crew    string    `json:"crew"`
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Text    string    `json:"text"`
	Handles []string  `json:"handles,omitempty"`
	Answers string    `json:"answers,omitempty"`
	Files   []string  `json:"files,omitempty"`
	Member  string    `json:"member,omitempty"`
	Agent   string    `json:"agent,omitempty"`
	State   string    `json:"state,omitempty"`
	Packet  string    `json:"packet,omitempty"`
	Reply   string    `json:"reply,omitempty"`
	Team    string    `json:"team,omitempty"`
	// Turn is the person's message that OPENED the turn which caused this line:
	// the same string as Entry.message_id on that message's chat entry, and
	// nothing else. Run is the producing run, the same string as Entry.run_id on
	// that run's answers, and RunAgent is the app agent that ran it. They are
	// three separate facts, each recorded by the engine at the moment of the
	// call that wrote the line. Every one is absent on a line written before
	// they existed and on a line no turn wrote: absent is unknown, the line is
	// still listed, and no reader may fill it in from time, order or wording.
	Turn     string `json:"turn,omitempty"`
	Run      string `json:"run,omitempty"`
	RunAgent string `json:"run_agent,omitempty"`
}

// DecisionParty is one side of a decision.
type DecisionParty struct {
	Agent   string `json:"agent,omitempty"`
	Handle  string `json:"handle,omitempty"`
	Crew    string `json:"crew,omitempty"`
	Context string `json:"context,omitempty"`
}

// DecisionOption is one answer and what follows from it.
type DecisionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Consequence string `json:"consequence"`
}

// DecisionPick is the option the raiser would choose, and why.
type DecisionPick struct {
	Option string `json:"option"`
	Reason string `json:"reason"`
}

// DecisionHop is one climb of a decision.
type DecisionHop struct {
	From   string    `json:"from"`
	To     string    `json:"to"`
	By     string    `json:"by"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

// CrewReport is a closing report.
type CrewReport struct {
	Done       string   `json:"done"`
	Left       string   `json:"left,omitempty"`
	Files      []string `json:"files,omitempty"`
	SpendUSD   float64  `json:"spend_usd,omitempty"`
	Incomplete bool     `json:"incomplete,omitempty"`
}

// CrewCap is a cap decision's facts.
type CrewCap struct {
	Crew     string  `json:"crew"`
	Day      string  `json:"day"`
	CapUSD   float64 `json:"cap_usd"`
	SpentUSD float64 `json:"spent_usd"`
	RaiseTo  float64 `json:"raise_to"`
}

// Decision is one decision packet, as stored.
type Decision struct {
	Irreversible   bool             `json:"irreversible,omitempty"`
	ID             string           `json:"id"`
	Crew           string           `json:"crew"`
	WaitsOn        string           `json:"waits_on"`
	Kind           string           `json:"kind"`
	RaisedBy       string           `json:"raised_by"`
	Parties        []DecisionParty  `json:"parties,omitempty"`
	Question       string           `json:"question"`
	Options        []DecisionOption `json:"options,omitempty"`
	Recommendation *DecisionPick    `json:"recommendation,omitempty"`
	Report         *CrewReport      `json:"report,omitempty"`
	Cap            *CrewCap         `json:"cap,omitempty"`
	State          string           `json:"state"`
	DecidedBy      string           `json:"decided_by,omitempty"`
	Decision       string           `json:"decision,omitempty"`
	Reason         string           `json:"reason,omitempty"`
	Trail          []DecisionHop    `json:"trail,omitempty"`
	Path           []string         `json:"path"`
	Raised         time.Time        `json:"raised"`
	At             time.Time        `json:"at"`
}

// RoleCopy is one member a role is given to.
type RoleCopy struct {
	Crew  string `json:"crew"`
	Agent string `json:"agent"`
}

// Role is a named set of instructions and a model (app layer).
type Role struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Instructions string     `json:"instructions"`
	Model        string     `json:"model,omitempty"`
	MadeAt       time.Time  `json:"made_at"`
	Copies       []RoleCopy `json:"copies"`
}

// ProposalHelper is one helper a proposal would start.
type ProposalHelper struct {
	Handle string `json:"handle"`
	Role   string `json:"role,omitempty"`
	Brief  string `json:"brief"`
	Model  string `json:"model,omitempty"`
}

// Proposal is an agent's proposal to become a crew (app layer).
type Proposal struct {
	ID        string           `json:"id"`
	Agent     string           `json:"agent"`
	Name      string           `json:"name"`
	Hue       *float64         `json:"hue,omitempty"`
	Helpers   []ProposalHelper `json:"helpers"`
	Existing  []string         `json:"existing,omitempty"`
	CapUSDDay *float64         `json:"cap_usd_day,omitempty"`
	Goal      string           `json:"goal,omitempty"`
	EndsAt    *time.Time       `json:"ends_at,omitempty"`
	At        time.Time        `json:"at"`
}

// The data of the crews' events.
type (
	CrewLogEvent struct {
		Line LogLine `json:"line"`
	}
	DecisionEvent struct {
		Decision Decision `json:"decision"`
	}
	MemberChanged struct {
		Member CrewMember `json:"member"`
	}
)

// CrewCreate is crews.create's params: from agents, or from a proposal.
type CrewCreate struct {
	From      string        `json:"from"`
	Name      string        `json:"name"`
	Agents    []string      `json:"agents"`
	Manager   string        `json:"manager"`
	Parent    string        `json:"parent"`
	Hue       *float64      `json:"hue"`
	Tint      string        `json:"tint"`
	Settings  *CrewSettings `json:"settings"`
	Goal      string        `json:"goal"`
	EndsAt    *time.Time    `json:"ends_at"`
	Proposal  string        `json:"proposal"`
	Edits     *Proposal     `json:"edits"`
	CapUSDDay *float64      `json:"cap_usd_day"`

	// The fields below are crews.start's: they ride on crews.create only with
	// StartManager, and any of them without it is bad_params (crewstart.go).
	StartManager         bool           `json:"start_manager,omitempty"`
	ManagerStartsMembers bool           `json:"manager_starts_members,omitempty"`
	RequestID            string         `json:"request_id,omitempty"`
	Said                 *string        `json:"said,omitempty"`
	ClientID             string         `json:"client_id,omitempty"`
	Attachments          []AttachSource `json:"attachments,omitempty"`
	Workspace            string         `json:"workspace,omitempty"`
	Model                string         `json:"model,omitempty"`

	// BeforeMint and BeforeStart let a scoped executor keep and lease the new
	// manager before its first turn, just as agents.create does.
	BeforeMint  func(string) error `json:"-"`
	BeforeStart func(Agent) error  `json:"-"`
}

// CrewUpdate is crews.update's params; a nil field is left alone.
type CrewUpdate struct {
	ID       string        `json:"id"`
	Name     *string       `json:"name"`
	Hue      *float64      `json:"hue"`
	Tint     *string       `json:"tint"`
	Settings *CrewSettings `json:"settings"`
	// Clear names settings to set back to inherit. The word "hue" clears the
	// crew's colour instead; it stays uncoloured until a hue or tint is set, and
	// it cannot be sent with one.
	Clear  []string   `json:"clear"`
	Goal   *string    `json:"goal"`
	EndsAt *time.Time `json:"ends_at"`
}

// MemberAsk is the params of the member verbs.
type MemberAsk struct {
	Crew   string   `json:"crew"`
	Agent  string   `json:"agent"`
	Handle string   `json:"handle"`
	Brief  string   `json:"brief"`
	Role   string   `json:"role"`
	Model  string   `json:"model"`
	Reason string   `json:"reason"`
	CapUSD *float64 `json:"cap_usd"`
	To     string   `json:"to"`
}

// CrewMessage is crews.message's params. To is empty (or "manager") for the
// crew's manager, or a member's agent id. Attachments are ids staged with
// attachments.add on that same target agent; with them the text may be empty.
// A message with attachments is never queued behind a busy target: it is
// refused busy and its attachments stay staged for the person to send again.
// Said and Reads are agents.send's ([AgentMessage]), and a message carrying
// either is likewise never queued: it is refused busy.
type CrewMessage struct {
	Crew        string        `json:"crew"`
	To          string        `json:"to"`
	Text        string        `json:"text"`
	Said        *string       `json:"said,omitempty"`
	ClientID    string        `json:"client_id"`
	Attachments []string      `json:"attachments,omitempty"`
	Reads       []MessageRead `json:"reads,omitempty"`
	// Submission is refused: a crew message carries no pictures.
	Submission *MessageSubmission `json:"submission,omitempty"`
}

// Crewer is the engine's half of crews.
type Crewer interface {
	CrewList(closed bool) ([]Crew, error)
	CrewGet(id string) (Crew, error)
	CrewCreate(ask CrewCreate) (Crew, error)
	CrewUpdate(ask CrewUpdate) (Crew, error)
	CrewMessage(ask CrewMessage) (Entry, error)
	CrewLog(id, after string, limit int) ([]LogLine, string, error)
	CrewLogAt(ask LogAnchorAsk) (LogAnchor, error)
	CrewDecisions(id string, all bool) ([]Decision, error)
	CrewDecide(packet, option, text, reason string) (Decision, error)
	CrewManager(id, agent string) (Crew, error)
	CrewMove(ids []string, parent string) error
	CrewWrapUp(id, text string) error
	CrewClose(id string) error
	CrewReopen(id string) (Crew, error)
	// CrewArchive puts a closed crew away (its own mark; no crew under it is
	// archived). CrewListArchived is the crews put away, newest first.
	CrewArchive(id string) (Crew, error)
	CrewListArchived() ([]Crew, error)
	CrewDelete(id string) ([]string, error)
	CrewDefaults() (CrewDefaults, error)
	CrewSetDefault(key, value string) (CrewDefaults, error)
	CrewItems(id string) ([]CrewItem, error)
	CrewLink(id, kind, ref, title string) (CrewItem, error)
	CrewUnlink(id, item string) error
	CrewPropose(agent string) error
	Member(verb string, ask MemberAsk) (CrewMember, error)
	MemberBrief(crew, agent string) (MemberBrief, error)
	MemberBriefSet(crew, agent, text string, base *string) (MemberBrief, error)
	Roles() ([]Role, error)
	RoleUpdate(id string, name, instructions, model *string) (Role, error)
}

func init() {
	for name, m := range crewMethods {
		methods[name] = m
	}
}

func crewer(e Engine, name string) (Crewer, error) {
	c, ok := e.(Crewer)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return c, nil
}

// crewCall decodes a method's params into P and answers what call answers.
func crewCall[P, R any](name string, call func(Crewer, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		c, err := crewer(e, name)
		if err != nil {
			return nil, err
		}
		return call(c, p)
	})
}

type crewID struct {
	ID string `json:"id"`
}

func needID(name, id string) error {
	if strings.TrimSpace(id) == "" {
		return Errorf(CodeBadParams, "%s needs an id", name)
	}
	return nil
}

type crewResult struct {
	Crew  Crew         `json:"crew"`
	Start *CrewStarted `json:"start,omitempty"`
}

func crewOf(c Crew, err error) (*crewResult, error) {
	if err != nil {
		return nil, err
	}
	c.Members = nonNil(c.Members)
	return &crewResult{Crew: c}, nil
}

// LogAnchorAsk names one crew log line from a conversation's own record: the
// agent whose chat holds it (the receipt's sender, the delivery's addressee),
// the line's id (a handoff's or a delivery's post) and the crew name the record
// gave. Around is how many lines on each side to read with it (at most
// [LogAnchorAroundMax]).
type LogAnchorAsk struct {
	Agent    string `json:"agent"`
	Post     string `json:"post"`
	CrewName string `json:"crew_name,omitempty"`
	// Crew is the crew id the record names (a post up to the crew above):
	// then that crew's log is read, if the agent is in it or in a crew under
	// it, and CrewName is not matched (a rename keeps the line).
	Crew   string `json:"crew,omitempty"`
	Around int    `json:"around,omitempty"`
}

// LogAnchorAroundMax bounds a log anchor's neighbours on each side.
const LogAnchorAroundMax = 10

// LogAnchor is the crew whose log holds an anchored line and that line with its
// neighbours, oldest first, read by id (never by words or time). Closed says
// the crew is closed: its log is read-only history.
type LogAnchor struct {
	Crew   string    `json:"crew"`
	Closed bool      `json:"closed,omitempty"`
	Lines  []LogLine `json:"lines"`
}

// MemberBrief is a member's brief: the words its manager started it with, or
// the newest change made to them since (members.brief.set). Text is empty when
// the record holds neither: a member a person added, a crew's first manager,
// or a start older than the log. From is who started it (a handle); At when
// the brief now in force was written. Revisions are the changes, oldest first
// (at most 16); none means the brief was never changed — nothing is inferred
// for a brief from before changes were kept.
type MemberBrief struct {
	Text      string          `json:"text,omitempty"`
	From      string          `json:"from,omitempty"`
	At        *time.Time      `json:"at,omitempty"`
	Revisions []BriefRevision `json:"revisions,omitempty"`
	// FactoryStage marks the brief of a factory stage's conversation: Text is
	// the brief the factory actually started it on, with no From and no
	// Revisions, and members.brief.set refuses it (the job owns it). Absent for
	// every other member and for stages recorded before it was kept.
	FactoryStage bool `json:"factory_stage,omitempty"`
}

// BriefRevision is one change to a member's brief: who made it ("you" or a
// member's handle), when, the words before and after, and the crew log entry
// that delivered it (a LogLine id), which the member read at its next step.
type BriefRevision struct {
	Text   string    `json:"text"`
	Before string    `json:"before,omitempty"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Post   string    `json:"post,omitempty"`
}

// MemberResult is a member verb's answer.
type MemberResult struct {
	Member CrewMember `json:"member"`
}

func memberOf(m CrewMember, err error) (*MemberResult, error) {
	if err != nil {
		return nil, err
	}
	return &MemberResult{m}, nil
}

// memberVerb is one member verb.
func memberVerb(verb string) method {
	return crewCall("members."+verb, func(c Crewer, p MemberAsk) (*MemberResult, error) {
		if strings.TrimSpace(p.Crew) == "" {
			return nil, Errorf(CodeBadParams, "members.%s needs a crew", verb)
		}
		return memberOf(c.Member(verb, p))
	})
}

var crewMethods = map[string]method{
	"crews.get": crewCall("crews.get", func(c Crewer, p crewID) (*crewResult, error) {
		if err := needID("crews.get", p.ID); err != nil {
			return nil, err
		}
		return crewOf(c.CrewGet(p.ID))
	}),
	"crews.create": crewCall("crews.create", func(c Crewer, p CrewCreate) (*crewResult, error) {
		if p.StartManager || p.startFieldsSet() {
			return startCrew(c, p)
		}
		return crewOf(c.CrewCreate(p))
	}),
	"crews.root.start": crewCall("crews.root.start", func(c Crewer, p RootStartAsk) (*RootStartResult, error) {
		return startRoot(c, p)
	}),
	"crews.update": crewCall("crews.update", func(c Crewer, p CrewUpdate) (*crewResult, error) {
		if err := needID("crews.update", p.ID); err != nil {
			return nil, err
		}
		return crewOf(c.CrewUpdate(p))
	}),
	"crews.message": crewCall("crews.message", func(c Crewer, p CrewMessage) (*EntryResult, error) {
		if strings.TrimSpace(p.Crew) == "" || (strings.TrimSpace(p.Text) == "" && len(p.Attachments) == 0) {
			return nil, Errorf(CodeBadParams, "crews.message needs a crew and some text")
		}
		if err := CheckAttachIDs(p.Attachments); err != nil {
			return nil, err
		}
		if err := refuseSubmission(p.Submission, "crews.message"); err != nil {
			return nil, err
		}
		entry, err := c.CrewMessage(p)
		if err != nil {
			return nil, err
		}
		return &EntryResult{entry}, nil
	}),
	"crews.log": crewCall("crews.log", func(c Crewer, p struct {
		ID    string `json:"id"`
		After string `json:"after"`
		Limit int    `json:"limit"`
	}) (*LinesCursorResult, error) {
		if err := needID("crews.log", p.ID); err != nil {
			return nil, err
		}
		lines, cursor, err := c.CrewLog(p.ID, p.After, p.Limit)
		if err != nil {
			return nil, err
		}
		return &LinesCursorResult{nonNil(lines), cursor}, nil
	}),
	"crews.log.at": crewCall("crews.log.at", func(c Crewer, p LogAnchorAsk) (*struct {
		Anchor LogAnchor `json:"anchor"`
	}, error) {
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Post) == "" {
			return nil, Errorf(CodeBadParams, "crews.log.at needs an agent and a post")
		}
		if p.Around < 0 || p.Around > LogAnchorAroundMax {
			return nil, Errorf(CodeBadParams, "crews.log.at reads at most %d lines on each side", LogAnchorAroundMax)
		}
		anchor, err := c.CrewLogAt(p)
		if err != nil {
			return nil, err
		}
		anchor.Lines = nonNil(anchor.Lines)
		return &struct {
			Anchor LogAnchor `json:"anchor"`
		}{anchor}, nil
	}),
	"crews.decisions": crewCall("crews.decisions", func(c Crewer, p struct {
		ID  string `json:"id"`
		All bool   `json:"all"`
	}) (*DecisionsResult, error) {
		decisions, err := c.CrewDecisions(p.ID, p.All)
		if err != nil {
			return nil, err
		}
		return &DecisionsResult{nonNil(decisions)}, nil
	}),
	"crews.decide": crewCall("crews.decide", func(c Crewer, p struct {
		Packet string `json:"packet"`
		Option string `json:"option"`
		Text   string `json:"text"`
		Reason string `json:"reason"`
	}) (*DecisionResult, error) {
		if strings.TrimSpace(p.Packet) == "" || (strings.TrimSpace(p.Option) == "" && strings.TrimSpace(p.Text) == "") {
			return nil, Errorf(CodeBadParams, "crews.decide needs a packet and an option or some words")
		}
		d, err := c.CrewDecide(p.Packet, p.Option, p.Text, p.Reason)
		if err != nil {
			return nil, err
		}
		return &DecisionResult{d}, nil
	}),
	"crews.manager": crewCall("crews.manager", func(c Crewer, p struct {
		ID    string `json:"id"`
		Agent string `json:"agent"`
	}) (*crewResult, error) {
		if err := needID("crews.manager", p.ID); err != nil {
			return nil, err
		}
		return crewOf(c.CrewManager(p.ID, p.Agent))
	}),
	"crews.move": crewCall("crews.move", func(c Crewer, p struct {
		IDs    []string `json:"ids"`
		Parent string   `json:"parent"`
	}) (*Empty, error) {
		if len(p.IDs) == 0 {
			return nil, Errorf(CodeBadParams, "crews.move needs ids")
		}
		return done(c.CrewMove(p.IDs, p.Parent))
	}),
	"crews.wrap_up": crewCall("crews.wrap_up", func(c Crewer, p struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}) (*Empty, error) {
		if err := needID("crews.wrap_up", p.ID); err != nil {
			return nil, err
		}
		return done(c.CrewWrapUp(p.ID, p.Text))
	}),
	"crews.close": crewCall("crews.close", func(c Crewer, p crewID) (*Empty, error) {
		if err := needID("crews.close", p.ID); err != nil {
			return nil, err
		}
		return done(c.CrewClose(p.ID))
	}),
	"crews.reopen": crewCall("crews.reopen", func(c Crewer, p crewID) (*crewResult, error) {
		if err := needID("crews.reopen", p.ID); err != nil {
			return nil, err
		}
		return crewOf(c.CrewReopen(p.ID))
	}),
	// The archived crews have a method of their own, never a flag on crews.list:
	// a host without it answers unknown_method, where a flag would be ignored and
	// answer the open crews.
	"crews.archived": crewCall("crews.archived", func(c Crewer, _ struct{}) (*CrewsResult, error) {
		crews, err := c.CrewListArchived()
		if err != nil {
			return nil, err
		}
		for i := range crews {
			crews[i].Members = nonNil(crews[i].Members)
		}
		return &CrewsResult{nonNil(crews)}, nil
	}),
	"crews.archive": crewCall("crews.archive", func(c Crewer, p crewID) (*crewResult, error) {
		if err := needID("crews.archive", p.ID); err != nil {
			return nil, err
		}
		return crewOf(c.CrewArchive(p.ID))
	}),
	"crews.delete": crewCall("crews.delete", func(c Crewer, p crewID) (*IDsResult, error) {
		if err := needID("crews.delete", p.ID); err != nil {
			return nil, err
		}
		ids, err := c.CrewDelete(p.ID)
		if err != nil {
			return nil, err
		}
		return &IDsResult{nonNil(ids)}, nil
	}),
	"crews.defaults": crewCall("crews.defaults", func(c Crewer, _ struct{}) (*DefaultsResult, error) {
		d, err := c.CrewDefaults()
		if err != nil {
			return nil, err
		}
		return &DefaultsResult{d}, nil
	}),
	"crews.set_default": crewCall("crews.set_default", func(c Crewer, p struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}) (*DefaultsResult, error) {
		if strings.TrimSpace(p.Key) == "" {
			return nil, Errorf(CodeBadParams, "crews.set_default needs a key")
		}
		d, err := c.CrewSetDefault(p.Key, p.Value)
		if err != nil {
			return nil, err
		}
		return &DefaultsResult{d}, nil
	}),
	"crews.items": crewCall("crews.items", func(c Crewer, p crewID) (*ItemsResult, error) {
		if err := needID("crews.items", p.ID); err != nil {
			return nil, err
		}
		items, err := c.CrewItems(p.ID)
		if err != nil {
			return nil, err
		}
		return &ItemsResult{nonNil(items)}, nil
	}),
	"crews.link": crewCall("crews.link", func(c Crewer, p struct {
		ID    string `json:"id"`
		Kind  string `json:"kind"`
		Ref   string `json:"ref"`
		Title string `json:"title"`
	}) (*ItemResult, error) {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Ref) == "" {
			return nil, Errorf(CodeBadParams, "crews.link needs an id and a ref")
		}
		switch p.Kind {
		case "job", "issue", "pr", "file":
		default:
			return nil, Errorf(CodeBadParams, "kind is job, issue, pr or file")
		}
		item, err := c.CrewLink(p.ID, p.Kind, p.Ref, p.Title)
		if err != nil {
			return nil, err
		}
		return &ItemResult{item}, nil
	}),
	"crews.unlink": crewCall("crews.unlink", func(c Crewer, p struct {
		ID   string `json:"id"`
		Item string `json:"item"`
	}) (*Empty, error) {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Item) == "" {
			return nil, Errorf(CodeBadParams, "crews.unlink needs an id and an item")
		}
		return done(c.CrewUnlink(p.ID, p.Item))
	}),
	"crews.propose": crewCall("crews.propose", func(c Crewer, p struct {
		Agent string `json:"agent"`
	}) (*Empty, error) {
		if strings.TrimSpace(p.Agent) == "" {
			return nil, Errorf(CodeBadParams, "crews.propose needs an agent")
		}
		return done(c.CrewPropose(p.Agent))
	}),
	"members.brief": crewCall("members.brief", func(c Crewer, p struct {
		Crew  string `json:"crew"`
		Agent string `json:"agent"`
	}) (*struct {
		Brief MemberBrief `json:"brief"`
	}, error) {
		if strings.TrimSpace(p.Crew) == "" || strings.TrimSpace(p.Agent) == "" {
			return nil, Errorf(CodeBadParams, "members.brief needs a crew and an agent")
		}
		brief, err := c.MemberBrief(p.Crew, p.Agent)
		if err != nil {
			return nil, err
		}
		return &struct {
			Brief MemberBrief `json:"brief"`
		}{brief}, nil
	}),
	"members.brief.set": crewCall("members.brief.set", func(c Crewer, p struct {
		Crew  string  `json:"crew"`
		Agent string  `json:"agent"`
		Text  string  `json:"text"`
		Base  *string `json:"base"`
	}) (*struct {
		Brief MemberBrief `json:"brief"`
	}, error) {
		if strings.TrimSpace(p.Crew) == "" || strings.TrimSpace(p.Agent) == "" {
			return nil, Errorf(CodeBadParams, "members.brief.set needs a crew and an agent")
		}
		if strings.TrimSpace(p.Text) == "" {
			return nil, Errorf(CodeBadParams, "a brief needs words")
		}
		brief, err := c.MemberBriefSet(p.Crew, p.Agent, p.Text, p.Base)
		if err != nil {
			return nil, err
		}
		return &struct {
			Brief MemberBrief `json:"brief"`
		}{brief}, nil
	}),
	"members.add":    memberVerb("add"),
	"members.start":  memberVerb("start"),
	"members.stop":   memberVerb("stop"),
	"members.remove": memberVerb("remove"),
	"members.handle": memberVerb("handle"),
	"members.home":   memberVerb("home"),
	"members.pause":  memberVerb("pause"),
	"members.resume": memberVerb("resume"),
	"members.cap":    memberVerb("cap"),
	"members.role":   memberVerb("role"),
	"members.move":   memberVerb("move"),
	"roles.list": crewCall("roles.list", func(c Crewer, _ struct{}) (*RolesResult, error) {
		roles, err := c.Roles()
		if err != nil {
			return nil, err
		}
		return &RolesResult{nonNil(roles)}, nil
	}),
	"roles.update": crewCall("roles.update", func(c Crewer, p struct {
		ID           string  `json:"id"`
		Name         *string `json:"name"`
		Instructions *string `json:"instructions"`
		Model        *string `json:"model"`
	}) (*RoleResult, error) {
		if err := needID("roles.update", p.ID); err != nil {
			return nil, err
		}
		role, err := c.RoleUpdate(p.ID, p.Name, p.Instructions, p.Model)
		if err != nil {
			return nil, err
		}
		return &RoleResult{role}, nil
	}),
}

// The answers of this file's methods.
type (
	EntryResult struct {
		Entry Entry `json:"entry"`
	}
	LinesCursorResult struct {
		Lines  []LogLine `json:"lines"`
		Cursor string    `json:"cursor"`
	}
	DecisionsResult struct {
		Decisions []Decision `json:"decisions"`
	}
	DecisionResult struct {
		Decision Decision `json:"decision"`
	}
	IDsResult struct {
		IDs []string `json:"ids"`
	}
	DefaultsResult struct {
		Defaults CrewDefaults `json:"defaults"`
	}
	ItemsResult struct {
		Items []CrewItem `json:"items"`
	}
	ItemResult struct {
		Item CrewItem `json:"item"`
	}
	RolesResult struct {
		Roles []Role `json:"roles"`
	}
	RoleResult struct {
		Role Role `json:"role"`
	}
)
