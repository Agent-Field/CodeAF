package config

import (
	"fmt"
	"strconv"
)

// ── THE TEAMS GROUP: WHAT EVERY TEAM INHERITS WHEN IT SAYS NOTHING ─────────
//
// A team may override five things about how work is delegated to it
// (internal/teams' teamsettings.go): whether team traffic wakes its idle
// conversations, whether its members' questions go up to its manager first, what it may spend in a day, how deep teams may nest under
// it, and what share of its own cap a new sub-team is handed. A team that sets
// none of them inherits each from its parent, and the top of every chain
// inherits from these five rows. So these are DEFAULTS and nothing else: no
// session reads them to decide anything except through internal/teams'
// resolver, which reports beside every value where it came from, so a card can
// say `$5/day · from Settings` without guessing.
//
// THEY ARE FLAT KEYS UNDER `teams.`, because config.json is a flat dotted map
// ([readProfileConfig]); a nested `teams` object would read as unset with no
// error and every team would fall to the built-in default.

// The five rows' keys.
const (
	KeyTeamsQuestionsUp = "teams.questions_up"
	KeyTeamsCapUSDDay   = "teams.cap_usd_day"
	KeyTeamsDepthLimit  = "teams.depth_limit"
	KeyTeamsSubSharePct = "teams.sub_share_pct"
	KeyTeamsWake        = "teams.wake"
)

// The built-in defaults.
//
// QUESTIONS GO UP by default, because that is the ruling's goal: a person who
// talks to the top manager and steps away should be asked only what no manager
// could answer. A day's cap is OFF by default, for the reason every rail in
// this build ships large or off ([DefaultSpendRailUSD]): a cap nobody chose
// would stop work nobody asked to stop. Depth is three levels, the ruling's
// "about three". A new sub-team gets half its parent's cap.
const (
	DefaultTeamsQuestionsUp = true
	DefaultTeamsCapUSDDay   = 0.0
	DefaultTeamsDepthLimit  = 3
	DefaultTeamsSubSharePct = 50
	// DefaultTeamsWake is on: a directive that waited for the person to type
	// in the member's tab would make a manager a mailbox.
	DefaultTeamsWake = true
)

// The bands the two counts are kept inside. A depth of 1 is teams with no
// sub-teams at all; ten levels is past anything a person could follow. A
// share is a whole percentage of the parent's cap, and 0 would be a sub-team
// that can spend nothing, which is a stopped team rather than a share.
const (
	teamsDepthMin = 1
	teamsDepthMax = 10
	teamsShareMin = 1
	teamsShareMax = 100
)

// TeamDefaults is the five rows resolved: the persisted value where there is
// one inside its band, and the built-in default everywhere else.
type TeamDefaults struct {
	QuestionsUp bool
	// CapUSDDay is dollars per local day for a team and everything under it;
	// 0 is no cap.
	CapUSDDay float64
	// DepthLimit is how many levels a chain of teams may have, the top
	// counting as one.
	DepthLimit int
	// SubSharePct is the whole percentage of its parent's cap a new sub-team
	// is given.
	SubSharePct int
	// Wake is whether team traffic wakes an idle conversation: a directive its
	// member, a member's reply its manager.
	Wake bool
}

// TeamDefaultsAt resolves the five rows in one read of the profile, for
// internal/teams' [teams.DefaultsAt]. A value outside its band reads as the
// default rather than refusing a team over a hand-edited file.
func TeamDefaultsAt(profileDir string) TeamDefaults {
	out := TeamDefaults{
		QuestionsUp: DefaultTeamsQuestionsUp,
		CapUSDDay:   DefaultTeamsCapUSDDay,
		DepthLimit:  DefaultTeamsDepthLimit,
		SubSharePct: DefaultTeamsSubSharePct,
		Wake:        DefaultTeamsWake,
	}
	if value, ok := persistedBool(profileDir, KeyTeamsQuestionsUp); ok {
		out.QuestionsUp = value
	}
	if value, ok := persistedFloat(profileDir, KeyTeamsCapUSDDay); ok && value >= 0 {
		out.CapUSDDay = value
	}
	if value, ok := persistedInt(profileDir, KeyTeamsDepthLimit); ok && value >= teamsDepthMin && value <= teamsDepthMax {
		out.DepthLimit = value
	}
	if value, ok := persistedInt(profileDir, KeyTeamsSubSharePct); ok && value >= teamsShareMin && value <= teamsShareMax {
		out.SubSharePct = value
	}
	if value, ok := persistedBool(profileDir, KeyTeamsWake); ok {
		out.Wake = value
	}
	return out
}

// teamRows are the Teams group, in the order a person reaches for them: who
// answers a question and whether messages wake, then money, then the shape of
// the tree.
func teamRows(dir string) []Setting {
	return []Setting{
		{
			Key: KeyTeamsQuestionsUp, Category: CategoryTeams, Kind: SettingBool,
			Label: "questions go to the manager",
			Hint: "when a team has a manager, a member's clarifying question goes to that " +
				"manager first and reaches you only if the manager cannot answer it. " +
				"Permission prompts always come to you. A team can override this.",
			read:  func() string { return formatBool(TeamDefaultsAt(dir).QuestionsUp) },
			write: func(raw string) error { return writeBool(dir, KeyTeamsQuestionsUp, raw) },
		},
		{
			Key: KeyTeamsWake, Category: CategoryTeams, Kind: SettingBool,
			Label: "team messages wake",
			Hint: "a manager's directive starts an idle member's turn, and a member's " +
				"reply starts the manager's. Off, everything still arrives, at the next " +
				"turn each conversation takes. A team can override this.",
			read:  func() string { return formatBool(TeamDefaultsAt(dir).Wake) },
			write: func(raw string) error { return writeBool(dir, KeyTeamsWake, raw) },
		},
		{
			Key: KeyTeamsCapUSDDay, Category: CategoryTeams, Kind: SettingDollars,
			Label: "team daily cap", EmptyLabel: "no cap",
			Hint: "what a team and every team under it may spend in a day before its " +
				"manager asks you whether to go on. 0 is no cap. A team can set its own.",
			read:  func() string { return moneyValue(TeamDefaultsAt(dir).CapUSDDay) },
			write: func(raw string) error { return writeDollars(dir, KeyTeamsCapUSDDay, raw) },
		},
		{
			Key: KeyTeamsSubSharePct, Category: CategoryTeams, Kind: SettingCount,
			Label: "sub-team share", Unit: "%",
			Hint: "the share of its parent's daily cap a new sub-team starts with. It is " +
				"written on the sub-team when it is made, so changing this later moves " +
				"no team that already exists.",
			read: func() string { return strconv.Itoa(TeamDefaultsAt(dir).SubSharePct) },
			write: func(raw string) error {
				return writeTeamsBand(dir, KeyTeamsSubSharePct, raw, teamsShareMin, teamsShareMax)
			},
		},
		{
			Key: KeyTeamsDepthLimit, Category: CategoryTeams, Kind: SettingCount,
			Label: "team depth", Unit: "levels", UnitOne: "level",
			Hint: "how many levels of teams a manager may build by starting sub-teams, " +
				"the top team counting as one. 1 means no sub-teams.",
			read: func() string { return strconv.Itoa(TeamDefaultsAt(dir).DepthLimit) },
			write: func(raw string) error {
				return writeTeamsBand(dir, KeyTeamsDepthLimit, raw, teamsDepthMin, teamsDepthMax)
			},
		},
	}
}

// writeTeamsBand persists a whole number inside [low, high], refusing in the
// row's own words anything outside it.
func writeTeamsBand(profileDir, key, raw string, low, high int) error {
	value, err := parseCount(raw)
	if err != nil {
		return err
	}
	if value < low || value > high {
		return fmt.Errorf("that's not between %d and %d", low, high)
	}
	return writeProfileValue(profileDir, key, value)
}
