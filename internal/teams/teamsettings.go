package teams

import (
	"errors"
	"math"

	"github.com/Agent-Field/codeaf/internal/config"
)

// ── A TEAM'S DELEGATION SETTINGS, AND WHERE EACH ONE CAME FROM ──────────────
//
// Five things about how work is delegated can differ from team to team:
// whether team traffic wakes an idle conversation (wake: a directive its
// member, a member's reply its manager), whether a member's clarifying
// questions go up to the manager first (questions_up), what the team and everything under it may spend in a local
// day (cap_usd_day, 0 for no cap), how many levels of teams may stand under it
// counting the top as one (depth_limit), and what share of its own cap a new
// sub-team is handed (sub_share, a fraction in (0, 1]).
//
// EACH IS OPTIONAL, AND UNSET IS INHERIT. A team that says nothing takes its
// parent's value, the parent its own parent's, and the top of the chain takes
// the profile's `teams.` defaults (internal/config's teamdefaults.go). The
// resolver ([File.Effective]) answers every value together with where it came
// from ([Origin]), so a settings card can draw an inherited value dim with
// `· from Settings` or `· from harbor` and an overridden one in ink with a
// `reset` beside it, and never has to walk the tree itself.
//
// A CLOSED TEAM IS NOT IN ANY WALK (lifecycle.go). Its overrides are kept, so
// a reopened team has them back, but a team under it (which a cascade closed
// too, so this is only a hand-edited file) skips it on the way up, and its own
// effective cap is none: a closed team spends nothing, so it has nothing to
// cap.

// Settings are a team's own overrides. A nil field is unset.
type Settings struct {
	QuestionsUp *bool    `json:"questions_up,omitempty"`
	CapUSDDay   *float64 `json:"cap_usd_day,omitempty"`
	DepthLimit  *int     `json:"depth_limit,omitempty"`
	SubShare    *float64 `json:"sub_share,omitempty"`
	// Wake is stored as "wake". A file from before wake was inheritable wrote
	// only "wake": false (on was written as nothing), and that spelling reads
	// here unchanged as an override to off.
	Wake *bool `json:"wake,omitempty"`
}

// The bands an override is kept inside; a value outside them is dropped by
// tidy (it reads as inherit) and refused by [File.SetSettings].
const (
	depthLimitMax = 10
)

// ErrSetting is an override outside its band.
var ErrSetting = errors.New("teams: a cap is 0 or more, a depth 1 to 10, a share above 0 and at most 1")

// Empty reports whether the team overrides nothing.
func (s Settings) Empty() bool {
	return s.QuestionsUp == nil && s.CapUSDDay == nil && s.DepthLimit == nil && s.SubShare == nil && s.Wake == nil
}

// valid reports whether every set field is inside its band.
func (s Settings) valid() bool {
	return (s.CapUSDDay == nil || validCap(*s.CapUSDDay)) &&
		(s.DepthLimit == nil || *s.DepthLimit >= 1 && *s.DepthLimit <= depthLimitMax) &&
		(s.SubShare == nil || validShare(*s.SubShare))
}

func validCap(v float64) bool   { return v >= 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }
func validShare(v float64) bool { return v > 0 && v <= 1 }

// tidy drops every set field outside its band, and reports whether it did.
func (s *Settings) tidy() bool {
	changed := false
	if s.CapUSDDay != nil && !validCap(*s.CapUSDDay) {
		s.CapUSDDay, changed = nil, true
	}
	if s.DepthLimit != nil && (*s.DepthLimit < 1 || *s.DepthLimit > depthLimitMax) {
		s.DepthLimit, changed = nil, true
	}
	if s.SubShare != nil && !validShare(*s.SubShare) {
		s.SubShare, changed = nil, true
	}
	return changed
}

// clone is s with its own copies of every set value.
func (s Settings) clone() Settings {
	if s.QuestionsUp != nil {
		v := *s.QuestionsUp
		s.QuestionsUp = &v
	}
	if s.CapUSDDay != nil {
		v := *s.CapUSDDay
		s.CapUSDDay = &v
	}
	if s.DepthLimit != nil {
		v := *s.DepthLimit
		s.DepthLimit = &v
	}
	if s.SubShare != nil {
		v := *s.SubShare
		s.SubShare = &v
	}
	if s.Wake != nil {
		v := *s.Wake
		s.Wake = &v
	}
	return s
}

// SetSettings changes team id's overrides with change, which sets a field to
// override it and nils it to reset it to inherit. A result outside a band is
// [ErrSetting] and nothing is changed.
func (f *File) SetSettings(id string, change func(*Settings)) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	next := f.Teams[i].Settings.clone()
	change(&next)
	if !next.valid() {
		return ErrSetting
	}
	f.Teams[i].Settings = next
	return nil
}

// Defaults are the profile's `teams.` rows as the resolver takes them.
type Defaults struct {
	QuestionsUp bool    `json:"questions_up"`
	CapUSDDay   float64 `json:"cap_usd_day"`
	DepthLimit  int     `json:"depth_limit"`
	// SubShare is a fraction, the row's whole percentage over 100.
	SubShare float64 `json:"sub_share"`
	// Wake is whether team traffic wakes idle conversations.
	Wake bool `json:"wake"`
}

// DefaultsAt reads the five rows from profileDir's config.json in one read
// (config's TeamDefaultsAt). An empty profileDir is the ordinary launch.
func DefaultsAt(profileDir string) Defaults {
	d := config.TeamDefaultsAt(profileDir)
	return Defaults{
		QuestionsUp: d.QuestionsUp,
		CapUSDDay:   d.CapUSDDay,
		DepthLimit:  d.DepthLimit,
		SubShare:    float64(d.SubSharePct) / 100,
		Wake:        d.Wake,
	}
}

// Where a value came from.
const (
	// OriginTeam is the team's own override.
	OriginTeam = "team"
	// OriginAncestor is an override on a team up the chain, named by Team.
	OriginAncestor = "ancestor"
	// OriginSettings is the profile's `teams.` default.
	OriginSettings = "settings"
	// OriginClosed is a closed team's cap, which is none.
	OriginClosed = "closed"
)

// Origin is where one resolved value came from: Kind is one of the Origin
// constants, and Team and Name name the team that set it for OriginTeam and
// OriginAncestor.
type Origin struct {
	Kind string `json:"kind"`
	Team string `json:"team,omitempty"`
	Name string `json:"name,omitempty"`
}

// Inherited reports whether the value is not the team's own.
func (o Origin) Inherited() bool { return o.Kind != OriginTeam }

// Words is the dim words an inherited value is drawn with, "" for the team's
// own: `from Settings`, `from harbor`, `closed`.
func (o Origin) Words() string {
	switch o.Kind {
	case OriginSettings:
		return "from Settings"
	case OriginAncestor:
		return "from " + o.Name
	case OriginClosed:
		return "closed"
	}
	return ""
}

// Effective is a team's five settings resolved, each with its origin.
type Effective struct {
	QuestionsUp     bool    `json:"questions_up"`
	QuestionsUpFrom Origin  `json:"questions_up_from"`
	CapUSDDay       float64 `json:"cap_usd_day"`
	// CapFrom names the team whose cap this is. A cap is a POOL: a team's
	// spend counts every team under it ([TeamSpend]), so an inherited cap is
	// the ancestor's one pool, shared, and not a second allowance of the same
	// size; the spend to set beside it is CapFrom.Team's.
	CapFrom      Origin  `json:"cap_from"`
	DepthLimit   int     `json:"depth_limit"`
	DepthFrom    Origin  `json:"depth_from"`
	SubShare     float64 `json:"sub_share"`
	SubShareFrom Origin  `json:"sub_share_from"`
	// Wake is whether team traffic wakes the team's idle conversations: a
	// directive its member, a member's reply or event its manager.
	Wake     bool   `json:"wake"`
	WakeFrom Origin `json:"wake_from"`
}

// Effective resolves team id's settings: each from the team's own override,
// else the nearest open ancestor's, else d. An id not in the file is d
// throughout.
func (f *File) Effective(id string, d Defaults) Effective {
	settings := Origin{Kind: OriginSettings}
	out := Effective{
		QuestionsUp: d.QuestionsUp, QuestionsUpFrom: settings,
		CapUSDDay: d.CapUSDDay, CapFrom: settings,
		DepthLimit: d.DepthLimit, DepthFrom: settings,
		SubShare: d.SubShare, SubShareFrom: settings,
		Wake: d.Wake, WakeFrom: settings,
	}
	self, ok := f.Team(id)
	if !ok {
		return out
	}
	var got struct{ questions, cap, depth, share, wake bool }
	chain := append([]Team{self}, f.Ancestors(id)...)
	for i, t := range chain {
		if t.Closed() && i > 0 {
			continue
		}
		origin := Origin{Kind: OriginAncestor, Team: t.ID, Name: t.Name}
		if i == 0 {
			origin.Kind = OriginTeam
		}
		s := t.Settings
		if !got.questions && s.QuestionsUp != nil {
			out.QuestionsUp, out.QuestionsUpFrom, got.questions = *s.QuestionsUp, origin, true
		}
		if !got.cap && s.CapUSDDay != nil {
			out.CapUSDDay, out.CapFrom, got.cap = *s.CapUSDDay, origin, true
		}
		if !got.depth && s.DepthLimit != nil {
			out.DepthLimit, out.DepthFrom, got.depth = *s.DepthLimit, origin, true
		}
		if !got.share && s.SubShare != nil {
			out.SubShare, out.SubShareFrom, got.share = *s.SubShare, origin, true
		}
		if !got.wake && s.Wake != nil {
			out.Wake, out.WakeFrom, got.wake = *s.Wake, origin, true
		}
	}
	if self.Closed() {
		out.CapUSDDay, out.CapFrom = 0, Origin{Kind: OriginClosed, Team: self.ID, Name: self.Name}
	}
	return out
}

// Depth is how many levels team id stands at, the top level being 1, and 0
// for an id not in the file. The root team (root.go) is not a level: it is 0,
// and a team directly under it is 1.
func (f *File) Depth(id string) int {
	t, ok := f.Team(id)
	if !ok || t.Root {
		return 0
	}
	depth := 1
	for _, a := range f.Ancestors(id) {
		if !a.Root {
			depth++
		}
	}
	return depth
}

// CanNest reports whether a new sub-team may be made under parent: the parent
// is open and one more level stays inside the parent's effective depth limit.
func (f *File) CanNest(parent string, d Defaults) bool {
	t, ok := f.Team(parent)
	if !ok || t.Closed() {
		return false
	}
	return f.Depth(parent)+1 <= f.Effective(parent, d).DepthLimit
}

// SubTeamCap is the cap a new sub-team under parent is made with: the
// parent's effective cap times its effective share, rounded by [RoundMoney]
// (to the cent, or finer under a cent, so a sub-cent share is not zero). A
// parent with no cap gives none (0), and the sub-team then shares whatever
// pool is above it. The caller writes the answer on the new team
// ([File.SetSettings]), so a later change to the share moves no team that
// exists.
func (f *File) SubTeamCap(parent string, d Defaults) float64 {
	e := f.Effective(parent, d)
	if e.CapUSDDay <= 0 {
		return 0
	}
	return RoundMoney(e.CapUSDDay * e.SubShare)
}
