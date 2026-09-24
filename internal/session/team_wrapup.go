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
// THE WRAP-UP IS THE MANAGER PROCESS'S MEMORY, not a file: a manager that
// restarts in the middle of one forgets its clock, and the person's card
// still offers `Close now` (the interface's own road).

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
	a.team.wraps[role.id] = &wrapUp{team: role.id, name: role.name, started: at}
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
		case now.Sub(w.started) >= wrapUpFor:
			why = fmt.Sprintf("the wrap-up ran out of time (%s) before the manager brought its report", wrapUpFor.Round(time.Minute))
		case spent-w.spentAt >= wrapUpSpendUSD:
			why = fmt.Sprintf("the wrap-up spent %s, its limit, before the manager brought its report", teamMoney(spent-w.spentAt))
		default:
			continue
		}
		a.team.mu.Lock()
		delete(a.team.wraps, w.team)
		a.team.mu.Unlock()
		if openClosing(profile, w.team) {
			continue
		}
		_, _ = teams.Raise(profile, closingPacket(w.name, w.team, teams.ClosingReport{
			Done: "not reported: " + why, Left: "unknown: see the team's traffic and each member's conversation",
			SpendUSD: roundCents(spent), Incomplete: true,
		}))
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
