package session

// ── A TEAM'S DAILY CAP IS HELD, NEVER CUT MID-TURN ──────────────────────────
//
// A team may have a daily cap (teams' teamsettings.go, `cap_usd_day`, which
// inherits), and a cap is a POOL: its owner's spend counts every team under
// it, so an inherited cap is the ancestor's one pool and never a second
// allowance ([teams.Effective]'s CapFrom). This file is what enforces it, on
// the session's side, and what it enforces is deliberately small:
//
//   - AT OR OVER THE CAP, WHAT THE TEAM WOULD START IS HELD. A wake (a
//     directive to a member, replies to a manager, an answer to a question), a
//     new member's first turn on its brief, and `team_start` are refused while
//     the pool is at its cap. A turn already running is never cut: it finishes
//     and nothing new starts behind it. The lines that were held are not
//     lost; they are delivered at the conversation's next turn.
//   - THE PERSON'S OWN WORDS ARE NOT HELD. A person typing into a member's
//     conversation spends their own money in front of them, which is not what
//     a cap on unattended work is for.
//   - ONE CAP PACKET PER POOL AND CEILING goes to the person ([teams.Person]):
//     `harbor reached its $5 cap today`, with `Raise to $10` and `Stop for
//     today`, and a recommendation. Whoever meets the cap first raises it;
//     everyone after finds it waiting and raises nothing, including a second
//     process: [teams.Raise] keeps one packet per pool, day and ceiling under
//     the decisions file's lock. A raise lifts the
//     ceiling for the rest of that local day to the figure the option said
//     ([teams.CapFacts].RaiseTo); a stop, or an answer in the person's own
//     words, holds the pool until the day turns or the cap is changed.
//   - A MANAGER NEVER RAISES A CAP. The packet waits on the person, and
//     `team_decide` refuses a cap packet in words.
//
// IT IS NEVER A HOT-PATH LEDGER SCAN. The check runs only where the team would
// start something (a wake, a start), never per model request, and the spend
// it reads is kept against [teams.TeamSpendStamp] (a stat of the teams file
// and one of the ledger): a pool whose stamp has not moved is answered from
// memory, and one that moved is read by teams' own incremental fold.

import (
	"fmt"
	"sync"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// teamSpendOf and teamSpendStamp are the pool's spend and its stamp; vars so a
// test can count how often the spend is really read.
var (
	teamSpendOf    = teams.TeamSpend
	teamSpendStamp = teams.TeamSpendStamp
	teamToday      = teams.Today
)

// capRaising serializes this process's cap raises, so two conversations in
// one pool do not both call [teams.Raise]. A second process is covered by the
// decisions file lock, not by this mutex.
var capRaising sync.Mutex

// spendMemo is one pool's spend as last read, against its stamp.
type spendMemo struct {
	stamp string
	usd   float64
}

// teamPoolSpend is the pool owner's spend today, read again only when its
// stamp moved.
func (a *Agent) teamPoolSpend(profile, owner, day string) float64 {
	stamp := teamSpendStamp(profile, owner, day)
	a.team.mu.Lock()
	if held, ok := a.team.spends[owner]; ok && held.stamp == stamp {
		a.team.mu.Unlock()
		return held.usd
	}
	a.team.mu.Unlock()
	spend, err := teamSpendOf(profile, owner, day)
	if err != nil {
		return 0
	}
	a.team.mu.Lock()
	if a.team.spends == nil {
		a.team.spends = map[string]spendMemo{}
	}
	a.team.spends[owner] = spendMemo{stamp: stamp, usd: spend.USD}
	a.team.mu.Unlock()
	return spend.USD
}

// capPool is the pool team id draws on: its owner and the cap, 0 for none.
// The owner is the team whose override the cap is, or, for the profile's
// default, the top of the chain (the root when there is one).
func capPool(file *teams.File, id string, d teams.Defaults) (teams.Team, float64) {
	if file == nil {
		return teams.Team{}, 0
	}
	e := file.Effective(id, d)
	if e.CapUSDDay <= 0 {
		return teams.Team{}, 0
	}
	owner := e.CapFrom.Team
	if e.CapFrom.Kind == teams.OriginSettings {
		owner = id
		for _, up := range file.Ancestors(id) {
			if !up.Closed() {
				owner = up.ID
			}
		}
	}
	team, ok := file.Team(owner)
	if !ok {
		return teams.Team{}, 0
	}
	return team, e.CapUSDDay
}

// teamCapHold is why new team work may not start in any of roles' pools right
// now, "" when it may. Meeting a cap for the first time raises its packet.
func (a *Agent) teamCapHold(profile string, roles []teamRole) string {
	if profile == "" || len(roles) == 0 {
		return ""
	}
	a.team.mu.Lock()
	file, d := a.team.file, a.team.defaults
	a.team.mu.Unlock()
	seen := map[string]bool{}
	for _, role := range roles {
		if !role.managed {
			continue
		}
		owner, cap := capPool(file, role.id, d)
		if cap <= 0 || seen[owner.ID] {
			continue
		}
		seen[owner.ID] = true
		if reason := a.poolHold(profile, owner, cap); reason != "" {
			return reason
		}
	}
	return ""
}

// poolHold is why owner's pool is held, "" when it is not.
func (a *Agent) poolHold(profile string, owner teams.Team, cap float64) string {
	day := teamToday()
	spent := a.teamPoolSpend(profile, owner.ID, day)
	latest, found := latestCapPacket(profile, owner.ID, day)
	ceiling := cap
	if found && latest.State == teams.PacketDecided && latest.Decision == teams.OptionRaiseCap && latest.Cap.RaiseTo > ceiling {
		ceiling = latest.Cap.RaiseTo
	}
	if spent < ceiling {
		return ""
	}
	held := fmt.Sprintf("%s reached its %s cap today (spent %s); the person has been asked whether to raise it, and nothing new starts until they answer",
		owner.Name, teamMoney(ceiling), teamSpendMoney(spent))
	if found && (latest.Waiting() || latest.Cap.CapUSD >= ceiling) {
		if !latest.Waiting() {
			held = fmt.Sprintf("%s reached its %s cap today and the person chose to stop it for today", owner.Name, teamMoney(ceiling))
		}
		return held
	}
	capRaising.Lock()
	defer capRaising.Unlock()
	if again, ok := latestCapPacket(profile, owner.ID, day); ok && (again.Waiting() || again.Cap.CapUSD >= ceiling) {
		return held
	}
	_, _ = teams.Raise(profile, capPacket(owner, day, ceiling, spent))
	return held
}

// latestCapPacket is the last cap packet raised for owner's pool on day.
func latestCapPacket(profile, owner, day string) (teams.Packet, bool) {
	list, err := teams.Packets(profile, owner)
	if err != nil {
		return teams.Packet{}, false
	}
	for i := len(list) - 1; i >= 0; i-- {
		p := list[i]
		if p.Kind == teams.PacketCap && p.Cap != nil && p.Cap.Team == owner && p.Cap.Day == day {
			return p, true
		}
	}
	return teams.Packet{}, false
}

// capPacket is the packet a pool at its ceiling raises to the person.
func capPacket(owner teams.Team, day string, ceiling, spent float64) teams.Packet {
	raiseTo := teams.RaiseTo(ceiling)
	return teams.Packet{
		Team: teams.Person, Origin: owner.ID, Kind: teams.PacketCap, RaisedBy: teams.FromSystem,
		Question: fmt.Sprintf("%s reached its %s cap today", owner.Name, teamMoney(ceiling)),
		Options: []teams.Option{
			{ID: teams.OptionRaiseCap, Label: "Raise to " + teamMoney(raiseTo),
				Consequence: fmt.Sprintf("%s and its sub-teams go on until %s today", owner.Name, teamMoney(raiseTo))},
			{ID: teams.OptionStopToday, Label: "Stop for today",
				Consequence: "members finish their current turn and start no new one until tomorrow"},
		},
		Recommendation: &teams.Recommendation{Option: teams.OptionStopToday,
			Reason: "the cap is the limit you set; raise it only if today's work is worth more to you"},
		Cap: &teams.CapFacts{Team: owner.ID, Day: day, CapUSD: ceiling, SpentUSD: teams.RoundMoney(spent), RaiseTo: raiseTo},
	}
}

// teamMoney is dollars as the person reads them: $5, $5.50, $0.001. It is
// teams' one spelling of a cap, so the refusal here, the packet and every
// screen that draws the same cap say the same figure ([teams.Money]).
func teamMoney(usd float64) string { return teams.Money(usd) }

// teamSpendMoney is a MEASURED spend as the person reads it: kept to the cent,
// or finer under a cent ([teams.RoundMoney]), then spelled as a cap is.
func teamSpendMoney(usd float64) string { return teams.Money(teams.RoundMoney(usd)) }
