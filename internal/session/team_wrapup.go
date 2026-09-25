package session

// ── WRAP UP FIRST: THE MANAGER'S CLOSING REPORT ─────────────────────────────
//
// The ruling (c-9): a person closing a team with work running picks `Wrap up
// first` by default. The interface appends the one request teams' wrapup.go
// defines ([teams.WrapUpRequest]: a directive from `you` to `manager` whose
// State is [teams.StateWrapUp]) to the team's Traffic, and nothing else: the
// two sides meet only there.
//
// THE MANAGER'S SIDE, here:
//
//   - Its delivery hands it the request as an instruction ([wrapUpLine]):
//     tell every member to finish and commit (team_send, directive), answer
//     what it can of what they ask, start nothing new, then write the closing
//     report with `team_close_report` (done, left, where the files are). The
//     request wakes an idle manager like a member's reply does.
//   - The report is a [teams.PacketClosing] packet to the person with the
//     team's spend today, options `Close` and `Keep going`, and a
//     recommendation. The team closes only when the person picks `Close`
//     ([teams.AcceptClosing], which the interface calls on the click and this
//     side calls again on reading the decision; it is idempotent).
//   - IT IS BOUNDED, by [wrapUpFor] and by [wrapUpSpendUSD] of the team's
//     spend since the request. Past either, with no report, codeaf raises the
//     packet itself, marked `wrap-up incomplete`, with `Close now` and `Keep
//     going` ([Agent.teamWrapUpDue]). The clock is looked at by the traffic
//     watch every tick, running or idle, and costs nothing while no wrap-up is
//     in progress.
//
// THE CLOCK IS ON THE TEAM, in teams.json ([teams.Wrap]): the start and the
// bound it was given. A manager that restarts in the middle of one reads it
// back ([Agent.teamWrapUpResume]) and keeps the time that is left. One
// already past its bound raises the incomplete report on that start, once,
// the same way a live clock would have. The person's card still offers
// `Close now` either way (the interface's own road).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// The wrap-up's two bounds. Fifteen minutes is long enough for members to
// finish the piece in hand and commit, and short enough that a person who
// asked to close is not left wondering; two dollars is a few turns of each
// member, which is what finishing costs, and not a second day's work. Vars so
// a test can run a wrap-up out in milliseconds; nothing in the product writes
// them.
var (
	wrapUpFor              = 15 * time.Minute
	wrapUpSpendUSD float64 = 2
)

const teamCloseReportToolName = "team_close_report"

const teamCloseReportDescription = "Bring the person your team's closing report when you have wrapped up: what was done, what is left, and where the files are. " +
	"It goes to the person as a decision with Close and Keep going; the team closes only if they pick Close. Use it when the person asked you to wrap up."

const teamCloseReportSchema = `{"type":"object","properties":{"done":{"type":"string","description":"What the team finished."},` +
	`"left":{"type":"string","description":"What is left, and who had it."},` +
	`"files":{"type":"array","items":{"type":"string"},"description":"Where the work is: paths, branches, commits."},` +
	teamArgSchema + `},"required":["done"],"additionalProperties":false}`

// wrapUp is one managed team's wrap-up in progress.
type wrapUp struct {
	team    string
	name    string
	started time.Time
	// bound is how long this wrap-up was given. It is the value of [wrapUpFor]
	// when the clock started, kept on the team so a restart does not start
	// the limit again. Zero is a clock from before it was stored, and reads
	// as [wrapUpFor].
	bound time.Duration
	// noted says the start and the bound have been written to the team.
	noted bool
	// spentAt is the team's spend today when the wrap-up's clock first
	// looked, which the spend bound is measured from; measured says it has.
	spentAt  float64
	measured bool
}

// wrapUpTools is the manager's report verb.
func (a *Agent) wrapUpTools() []bare.Tool {
	return []bare.Tool{
		{Name: teamCloseReportToolName, Description: teamCloseReportDescription, Schema: json.RawMessage(teamCloseReportSchema), Execute: a.teamCloseReportTool},
	}
}

// teamWrapUpBeginLocked records the wrap-up of role's team, once: a second
// request while one is in progress keeps the first clock. The caller holds
// a.team.mu, so it reads nothing: the spend the bound is measured from is
// taken at the clock's first look ([Agent.teamWrapUpDue]).
func (a *Agent) teamWrapUpBeginLocked(role teamRole, at time.Time) {
	if a.team.wraps == nil {
		a.team.wraps = map[string]*wrapUp{}
	}
	if _, going := a.team.wraps[role.id]; going {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	a.team.wraps[role.id] = &wrapUp{team: role.id, name: role.name, started: at, bound: wrapUpFor}
}

// teamWrapUpNote writes every wrap-up this process started and has not yet
// recorded. The caller does not hold a.team.mu: the write takes the file's
// own lock, and holding the seat across it would stall every reader. A second
// start keeps the first clock ([teams.File.SetWrap]).
func (a *Agent) teamWrapUpNote(profile string) {
	if profile == "" {
		return
	}
	a.team.mu.Lock()
	var pending []wrapUp
	for _, w := range a.team.wraps {
		if !w.noted {
			pending = append(pending, *w)
		}
	}
	a.team.mu.Unlock()
	for _, w := range pending {
		bound := w.bound
		if bound <= 0 {
			bound = wrapUpFor
		}
		if _, _, err := teams.Change(profile, func(f *teams.File) error {
			return f.SetWrap(w.team, w.started, bound)
		}); err != nil {
			continue
		}
		a.team.mu.Lock()
		if held, ok := a.team.wraps[w.team]; ok {
			held.noted = true
		}
		a.team.mu.Unlock()
	}
}

// teamWrapUpResume arms the clock of every team this conversation manages
// that has a wrap-up on disk, then looks at it. A wrap-up still inside its
// bound keeps the start it was given, so the time left is what was left. One
// already past it raises the incomplete report here, on the start, and not
// again: the record is cleared when the report goes out. It is called when
// the session opens ([Agent.watchTeamTraffic]).
func (a *Agent) teamWrapUpResume(profile string, now time.Time) {
	if profile == "" {
		return
	}
	a.team.mu.Lock()
	roles := a.teamRolesLocked(profile)
	file := a.team.file
	if a.team.wraps == nil {
		a.team.wraps = map[string]*wrapUp{}
	}
	for _, role := range roles {
		if !role.manager || file == nil {
			continue
		}
		t, ok := file.Team(role.id)
		if !ok || t.Wrap == nil || t.Closed() {
			continue
		}
		if _, going := a.team.wraps[role.id]; going {
			continue
		}
		a.team.wraps[role.id] = &wrapUp{
			team: role.id, name: role.name, started: t.Wrap.Started, bound: t.Wrap.Bound, noted: true,
		}
	}
	a.team.mu.Unlock()
	a.teamWrapUpDue(profile, now)
}

// clearTeamWrap forgets a wrap-up the clock has finished with. A missing team
// is not an error worth the report's road.
func clearTeamWrap(profile, id string) {
	_, _, _ = teams.Change(profile, func(f *teams.File) error {
		return f.ClearWrap(id)
	})
}

// wrapBound is how long w was given.
func wrapBound(w wrapUp) time.Duration {
	if w.bound > 0 {
		return w.bound
	}
	return wrapUpFor
}

// wrapUpLine is the request as the manager is handed it: the person's words,
// and what to do.
func wrapUpLine(role teamRole, entry teams.Entry) string {
	return fmt.Sprintf("◆ from the person: %s\n    Wrap up %q now: team_send every member a directive to finish the piece in hand, commit its work and report; "+
		"answer what you can of what they ask; start nothing new. Then call team_close_report with what was done, what is left and where the files are. "+
		"You have %s and %s of team spend for this; past either, codeaf sends the person the report as incomplete.",
		oneLineTeam(entry.Text), role.name, wrapUpFor.Round(time.Minute), teamMoney(wrapUpSpendUSD))
}

// teamWrapUpDue raises the incomplete report for every wrap-up past a bound.
// It is the watch's, every tick, and reads nothing while none is in progress.
func (a *Agent) teamWrapUpDue(profile string, now time.Time) {
	a.team.mu.Lock()
	if len(a.team.wraps) == 0 {
		a.team.mu.Unlock()
		return
	}
	going := make([]wrapUp, 0, len(a.team.wraps))
	for _, w := range a.team.wraps {
		going = append(going, *w)
	}
	a.team.mu.Unlock()
	for _, w := range going {
		why := ""
		spent := a.teamPoolSpend(profile, w.team, teamToday())
		if !w.measured {
			a.team.mu.Lock()
			if held, ok := a.team.wraps[w.team]; ok {
				held.spentAt, held.measured = spent, true
			}
			a.team.mu.Unlock()
			w.spentAt = spent
		}
		switch {
		case now.Sub(w.started) >= wrapBound(w):
			why = fmt.Sprintf("the wrap-up ran out of time (%s) before the manager brought its report", wrapBound(w).Round(time.Minute))
		case spent-w.spentAt >= wrapUpSpendUSD:
			why = fmt.Sprintf("the wrap-up spent %s, its limit, before the manager brought its report", teamSpendMoney(spent-w.spentAt))
		default:
			continue
		}
		a.team.mu.Lock()
		held := a.team.wraps[w.team]
		delete(a.team.wraps, w.team)
		a.team.mu.Unlock()
		if openClosing(profile, w.team) {
			// The report is already waiting, so the clock is over. Clearing
			// the record is what stops the next start from raising it again.
			clearTeamWrap(profile, w.team)
			continue
		}
		if _, err := teams.Raise(profile, closingPacket(w.name, w.team, teams.ClosingReport{
			Done: "not reported: " + why, Left: "unknown: see the team's traffic and each member's conversation",
			SpendUSD: roundCents(spent), Incomplete: true,
		})); err != nil {
			// THE RAISE DID NOT LAND. The clock was taken out before the
			// write so a second look during it cannot raise a second report.
			// A busy decisions file (ErrBusy after its wait) writes nothing,
			// and leaving the clock out would mean this process never tries
			// again: only a restart, which reads the disk, would send it.
			// Put the same clock back and let the next ordinary look retry.
			// A clock begun since the delete is newer and stays.
			a.team.mu.Lock()
			if held != nil {
				if a.team.wraps == nil {
					a.team.wraps = map[string]*wrapUp{}
				}
				if _, ok := a.team.wraps[w.team]; !ok {
					a.team.wraps[w.team] = held
				}
			}
			a.team.mu.Unlock()
			continue
		}
		clearTeamWrap(profile, w.team)
	}
}

// openClosing reports whether team already has a closing report waiting.
func openClosing(profile, team string) bool {
	waiting, _, err := teams.OpenPackets(profile, teams.Person)
	if err != nil {
		return false
	}
	for _, p := range waiting {
		if p.Kind == teams.PacketClosing && p.Origin == team {
			return true
		}
	}
	return false
}

// closingPacket is a closing report as the person is asked it. An incomplete
// one offers `Close now` in place of `Close`, and recommends keeping going,
// because nobody has said the work is safe to leave.
func closingPacket(name, team string, report teams.ClosingReport) teams.Packet {
	p := teams.Packet{
		Team: teams.Person, Origin: team, Kind: teams.PacketClosing, RaisedBy: teams.FromManager,
		Question: fmt.Sprintf("close %s?", name), Report: &report,
	}
	if report.Incomplete {
		p.RaisedBy = teams.FromSystem
		p.Question = fmt.Sprintf("close %s? (wrap-up incomplete)", name)
		p.Options = []teams.Option{
			{ID: teams.OptionCloseNow, Label: "Close now", Consequence: "every member's turn is stopped and " + name + " closes as it stands"},
			{ID: teams.OptionKeepGoing, Label: "Keep going", Consequence: name + " stays open and its manager carries on"},
		}
		p.Recommendation = &teams.Recommendation{Option: teams.OptionKeepGoing, Reason: "the manager has not said the work is safe to leave"}
		return p
	}
	p.Options = []teams.Option{
		{ID: teams.OptionClose, Label: "Close", Consequence: name + " closes; its members, traffic and this report are kept under Closed"},
		{ID: teams.OptionKeepGoing, Label: "Keep going", Consequence: name + " stays open and its manager carries on"},
	}
	p.Recommendation = &teams.Recommendation{Option: teams.OptionClose, Reason: "the manager reports the work wrapped up"}
	return p
}

func roundCents(usd float64) float64 { return float64(int64(usd*100+0.5)) / 100 }

func (a *Agent) teamCloseReportTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed struct {
		Done  string   `json:"done"`
		Left  string   `json:"left"`
		Files []string `json:"files"`
		Team  string   `json:"team"`
	}
	if err := decodeToolArguments(args, &parsed); err != nil {
		return invalidArgumentsPrefix + err.Error(), true, nil
	}
	done := strings.TrimSpace(parsed.Done)
	if done == "" {
		return invalidArgumentsPrefix + "done is empty", true, nil
	}
	team, _, refusal := a.teamTarget(parsed.Team, true)
	if refusal != "" {
		return refusal, true, nil
	}
	profile := a.config.teamProfile()
	if openClosing(profile, team.ID) {
		return fmt.Sprintf("A closing report for %q is already waiting on the person. Nothing was sent.", team.Name), true, nil
	}
	var files []string
	for _, file := range parsed.Files {
		if file = strings.TrimSpace(file); file != "" {
			files = append(files, file)
		}
	}
	spent := a.teamPoolSpend(profile, team.ID, teamToday())
	raised, err := teams.Raise(profile, closingPacket(team.Name, team.ID, teams.ClosingReport{
		Done: done, Left: strings.TrimSpace(parsed.Left), Files: files, SpendUSD: roundCents(spent),
	}))
	if err != nil {
		return "The report could not be sent: " + err.Error(), true, nil
	}
	a.team.mu.Lock()
	delete(a.team.wraps, team.ID)
	a.team.mu.Unlock()
	clearTeamWrap(profile, team.ID)
	return fmt.Sprintf("Your closing report went to the person as packet %s. %q closes only if they pick Close; if they pick Keep going you carry on, and you are told either way.", raised.ID, team.Name), false, nil
}

// closingDecidedLine is a decided closing report as its manager is told it,
// having closed the team when the person accepted it.
func closingDecidedLine(profile string, p teams.Packet) string {
	closed, _ := teams.AcceptClosing(profile, p)
	switch p.Decision {
	case teams.OptionClose, teams.OptionCloseNow:
		if closed {
			return "◆ the person accepted the closing report: the team is closed. Tell them it is done, and start nothing more in it."
		}
		return "◆ the person accepted the closing report; the team is closed."
	case teams.OptionKeepGoing:
		return "◆ the person read the closing report and chose to keep going: the team stays open. Carry on with what is left."
	}
	return "◆ the person answered the closing report: " + oneLineTeam(p.Decision)
}
