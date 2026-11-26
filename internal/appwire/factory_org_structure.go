package appwire

import "net/http"

// ── A WORKSTREAM'S STRUCTURE (Workstreams v5, SPEC §2.1) ───────────────────
//
// Nesting, the steward, people and rules ride factory.org_workstream_create
// and factory.org_workstream_update. Two more methods carry what is not a
// field:
//
//	factory.org_workstream_archive_keep {connection, id} -> OrgArchiveKeep
//	factory.org_workstream_fold         {connection, id} -> OrgWorkstreamFolded
//
// An engine that carries them says "org_workstream_nesting" (parent, the
// two methods above) and "org_workstream_rules" (steward, people, rules) in
// hello. Whether one organization's server keeps them is its own answer: an
// older server answers a workstream without people or rules, and 404 for the
// two methods, passed on as not_found.

// The hello words of a workstream's structure.
const (
	FeatureOrgWorkstreamNesting = "org_workstream_nesting"
	FeatureOrgWorkstreamRules   = "org_workstream_rules"
)

// OrgRulesOwner is who owns incoming work: a person, or "everyone".
type OrgRulesOwner struct {
	Kind string `json:"kind" enum:"person,everyone"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// OrgRulesPerson is one person a rule names.
type OrgRulesPerson struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// OrgKeepsRunning is what runs without anyone asking: the Factory, and the
// standing orders switched on, by id.
type OrgKeepsRunning struct {
	Factory bool     `json:"factory"`
	Orders  []string `json:"orders"`
	// FactorySince is when the Factory switch was last turned on; the
	// server stamps it and ignores what a writer sends.
	FactorySince string `json:"factory_since,omitempty"`
}

// OrgWorkstreamRules are how a workstream runs. StartWithoutAskingUSD 0 is
// never start without asking.
type OrgWorkstreamRules struct {
	DefaultOwner          OrgRulesOwner   `json:"default_owner"`
	Fallback              *OrgRulesPerson `json:"fallback,omitempty"`
	StartWithoutAskingUSD float64         `json:"start_without_asking_usd"`
	KeepsRunning          OrgKeepsRunning `json:"keeps_running"`
	// PublishesWithoutAsking turns the ask rule off. AsksBefore is how much
	// it stops while on: "" or "outward" (published, sent or paid for, the
	// default), "shared_file", or "every_task". RULES ARE WRITTEN WHOLE, so a
	// field this struct lacked was wiped by every save from the app.
	PublishesWithoutAsking bool   `json:"publishes_without_asking,omitempty"`
	AsksBefore             string `json:"asks_before,omitempty" enum:",outward,shared_file,every_task"`
	// FallbackAfterHours is how long an Ask waits on its decider before
	// Fallback answers it too: 0 or 24 is a day; 12, 48 and 72 are offered.
	FallbackAfterHours int `json:"fallback_after_hours,omitempty"`
	// Exceptions are the kinds of question Afi decides without asking here;
	// NotNow the suggested kinds the steward turned down. Absent, the server
	// keeps what it has; an empty list clears it.
	Exceptions []OrgAskException `json:"exceptions,omitempty"`
	NotNow     []OrgAskNotNow    `json:"not_now,omitempty"`
	// Said are the rules in plain words, as people wrote them. Absent (nil),
	// the server keeps what it has; an empty list clears them, so it is a
	// pointer: omitempty would drop an empty slice and the last rule taken
	// out would stay.
	Said *[]string `json:"said,omitempty"`
}

// OrgAskException is one kind of question Afi decides without asking in a
// workstream; the server stamps who added it and when.
type OrgAskException struct {
	Kind    string          `json:"kind"`
	AddedBy *OrgRulesPerson `json:"added_by,omitempty"`
	AddedAt string          `json:"added_at,omitempty"`
}

// OrgAskNotNow is a suggested exception the steward turned down, and when
// (the server stamps a missing time).
type OrgAskNotNow struct {
	Kind string `json:"kind"`
	At   string `json:"at,omitempty"`
}

// OrgRuleSuggestion is Afi's suggestion to loosen the ask rule, shown to the
// steward only: Title is the question, Why its evidence, Yes the button that
// adds Kind to the rules' exceptions.
type OrgRuleSuggestion struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Why     string `json:"why"`
	Yes     string `json:"yes"`
	Waiting int    `json:"waiting"`
	OfKind  int    `json:"of_kind"`
}

// OrgMember is one person or team in a workstream, with the role access
// reads. A team carries its members now; Steward marks who looks after it.
type OrgMember struct {
	Kind    string   `json:"kind" enum:"person,team"`
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Role    string   `json:"role"`
	Members []string `json:"members,omitempty"`
	Steward bool     `json:"steward,omitempty"`
	AddedAt string   `json:"added_at,omitempty"`
	// AddedBy is the handle of whoever added them, when the server kept it.
	AddedBy string `json:"added_by,omitempty"`
	// Invited marks someone invited who has not joined yet.
	Invited bool `json:"invited,omitempty"`
}

// OrgKeepCandidate is one page or source a child could leave in its parent.
type OrgKeepCandidate struct {
	Kind  string `json:"kind" enum:"page,source"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

// OrgArchiveKeep is what a child could leave in its parent when archived.
type OrgArchiveKeep struct {
	Parent string             `json:"parent,omitempty"`
	Keep   []OrgKeepCandidate `json:"keep"`
}

// OrgWorkstreamFolded is the parent after a child folded back into it, and
// the heading the child became.
type OrgWorkstreamFolded struct {
	Parent  OrgWorkstream `json:"parent"`
	Heading string        `json:"heading"`
}

func init() {
	methods["factory.org_workstream_archive_keep"] = filled(orgMethod[OrgWorkstreamParams, OrgArchiveKeep]("factory.org_workstream_archive_keep", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodGet, Path: path + "/archive/keep"}, err
	}), func(k *OrgArchiveKeep) { k.Keep = nonNil(k.Keep) })
	methods["factory.org_workstream_fold"] = filled(orgMethod[OrgWorkstreamParams, OrgWorkstreamFolded]("factory.org_workstream_fold", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodPost, Path: path + "/fold"}, err
	}), func(f *OrgWorkstreamFolded) { f.Parent.fill() })
}
