package session

// ── QUESTIONS GO UP AS DECISION PACKETS ─────────────────────────────────────
//
// The ruling (c-5, c-7): the person talks to the top manager and steps away,
// so a member's clarifying question goes to the manager it reports to first
// (its HOME, teams' home.go), and only what no manager may or can decide
// reaches the person. Whatever travels up travels as one self-contained
// DECISION PACKET ([teams.Packet]): the question, the asker's context, the
// options each with what happens if it is chosen, and the asker's own pick
// with its reason, so whoever decides it never has to read a transcript.
//
// THE ROAD, end to end:
//
//   - A member calls `ask` with a clarifying question (a clarification, a
//     choice or a confirmation) while `questions_up` is on for the team it
//     reports to ([teams.Effective], inherited). Nothing is put on the
//     person's screen: [Agent.askUp] raises a question packet addressed to
//     the home manager's team and hands the model back a sentence saying
//     so. The call does not park: a manager may take minutes, and a tool call
//     parked across two conversations is a turn nobody can stop cleanly.
//   - The raise appends a [teams.KindPacket] line to the team's Traffic, and
//     the manager's traffic watch wakes it on that line
//     ([Agent.teamPacketWakes]); its delivery hands it the packet whole
//     ([teamPacketLine]).
//   - The manager answers with `team_decide`, or sends it up with
//     `team_escalate` to its own manager or to the person. The decision is one
//     more packet line, which wakes the member and is delivered to it marked
//     `◆ answered: …`, and the Traffic rail reads `answered @web: …`.
//
// PERMISSION PROMPTS NEVER TAKE THIS ROAD. They are the approval gate's, not
// `ask`'s, and a manager may not answer one (section 5 of the design); only the
// kinds in [askGoesUp] do, and a permission kind written into `ask` stays the
// person's.
//
// A MANAGER'S OWN QUESTION is a packet too: to its own home manager when it has
// one and questions go up there, and otherwise to the person (addressed
// [teams.Person], `you`), where the teams page's inbox shows it. The person's
// decision reaches the manager by the same delivery and wakes it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/teams"
)

const (
	teamDecideToolName   = "team_decide"
	teamEscalateToolName = "team_escalate"
)

const teamDecideDescription = "Decide a decision packet waiting on you: a member's question, or anything your members raised to you. " +
	"answer is one of the packet's option ids, or your own words when no option fits. The asker is handed your answer marked as yours and continues. " +
	"You cannot decide a cap or a closing report; those are the person's."

const teamDecideSchema = `{"type":"object","properties":{"packet":{"type":"string","description":"The packet id, like p1a2b3c4d5e6f."},` +
	`"answer":{"type":"string","description":"An option id, or your own answer."},` +
	`"reason":{"type":"string","description":"One line: why."},` +
	teamArgSchema + `},"required":["packet","answer"],"additionalProperties":false}`

const teamEscalateDescription = "Send a decision packet waiting on you up the tree when it is not yours to decide: to your own manager, or to the person. " +
	"to is up (your manager, or the person when you have none) or you (the person). Never sideways."

const teamEscalateSchema = `{"type":"object","properties":{"packet":{"type":"string","description":"The packet id."},` +
	`"to":{"type":"string","enum":["up","you"],"description":"Default up."},` +
	`"reason":{"type":"string","description":"Why it is not yours to decide."},` +
	teamArgSchema + `},"required":["packet","reason"],"additionalProperties":false}`

// askGoesUp reports whether an `ask` of kind goes up as a packet: the
// clarifying kinds for a member, and a judgement as well for a manager, whose
// judgement calls are the person's to make (c-5). A permission, a landing, an
// assumption and a ratify never go up: the first is the person's safety gate,
// and the others are about this conversation's own work in front of the person.
func askGoesUp(kind AskKind, manager bool) bool {
	switch kind {
	case AskClarification, AskChoice, AskConfirmation:
		return true
	case AskJudgement:
		return manager
	}
	return false
}

// teamSnapshot is the teams file, this conversation's keys and roles, and the
// profile's defaults, from one hold of the seat's lock: a stat each of the
// teams file and config.json, and a read only when one moved.
func (a *Agent) teamSnapshot(profile string) (*teams.File, []string, []teamRole, teams.Defaults) {
	a.team.mu.Lock()
	defer a.team.mu.Unlock()
	roles := a.teamRolesLocked(profile)
	return a.team.file, append([]string(nil), a.teamKeysLocked()...), roles, a.team.defaults
}

// homeOf is the manager keys report to.
func homeOf(file *teams.File, keys []string) (teams.Report, bool) {
	if file == nil {
		return teams.Report{}, false
	}
	for _, key := range keys {
		if report, ok := file.Home(key); ok {
			return report, true
		}
	}
	return teams.Report{}, false
}

// askUp is `ask`'s road up: the answer to hand the model and true when q went
// up as a packet, and false when it is the person's as it always was. A packet
// that cannot be written falls back to the person, because a question that
// reaches nobody is worse than one that reaches the person.
func (a *Agent) askUp(q Question) (string, bool) {
	profile := a.config.teamProfile()
	if profile == "" || q.Policy.Kind == PolicyDecide {
		return "", false
	}
	file, keys, roles, d := a.teamSnapshot(profile)
	if file == nil || len(roles) == 0 {
		return "", false
	}
	var manages *teamRole
	var member *teamRole
	for i := range roles {
		switch {
		case roles[i].manager && manages == nil:
			manages = &roles[i]
		case !roles[i].manager && roles[i].managed && !roles[i].shared && member == nil:
			member = &roles[i]
		}
	}
	home, hasHome := homeOf(file, keys)
	upThere := hasHome && file.Effective(home.Team, d).QuestionsUp
	packet := askPacket(q)
	switch {
	case member != nil && askGoesUp(q.Ask, false) && upThere && home.Team == member.boss:
		if member.handle == "" {
			return "", false
		}
		packet.Team, packet.Origin, packet.RaisedBy = home.Team, member.id, member.handle
		packet.Parties = []teams.Party{{Key: member.key, Handle: member.handle, Team: member.id, Context: strings.TrimSpace(q.Reason)}}
		raised, err := teams.Raise(profile, packet)
		if err != nil {
			return "", false
		}
		who := "your manager"
		if team, ok := file.Team(home.Team); ok {
			if m, ok := team.Member(team.Manager); ok {
				if m.Handle != "" {
					who = fmt.Sprintf("your manager (@%s, manager of %q)", m.Handle, team.Name)
				}
				// A MANAGER NOBODY HOLDS IS OPENED to answer it, as a reply
				// to it would open it (team_wakewatch.go).
				a.teamRouse(profile, team, member.wakes, []teams.Member{m}, "")
			}
		}
		return fmt.Sprintf("Your question went to %s as packet %s, not to the person: in this team questions go to the manager first. "+
			"Its answer comes back to you as a message marked \"◆ answered\" and starts your turn if you are idle. "+
			"Carry on with what does not depend on it, or end your turn and wait.", who, raised.ID), true
	case manages != nil && askGoesUp(q.Ask, true):
		if q.Ask == AskJudgement {
			packet.Kind = teams.PacketJudgement
		}
		packet.RaisedBy = teams.FromManager
		packet.Parties = []teams.Party{{Key: manages.key, Handle: manages.handle, Team: manages.id, Context: strings.TrimSpace(q.Reason)}}
		// UP TO ITS OWN MANAGER when it has one and questions go up there;
		// otherwise the person. Its own team is the origin either way, which
		// is under the manager it reports to (a manager's home is one level
		// up, home.go).
		packet.Origin, packet.Team = manages.id, teams.Person
		to := "the person's inbox (a decision card on the teams page)"
		if hasHome && upThere && packet.Kind == teams.PacketQuestion {
			packet.Team = home.Team
			if team, ok := file.Team(home.Team); ok {
				to = fmt.Sprintf("your own manager, the manager of %q", team.Name)
			}
		}
		if len(packet.Options) == 0 && packet.Kind != teams.PacketQuestion {
			// A judgement needs its options; one written with none is asked of
			// the person as it always was.
			return "", false
		}
		raised, err := teams.Raise(profile, packet)
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("Your question went to %s as packet %s. "+
			"The answer comes back to you as a message marked \"◆ answered\" and starts your turn if you are idle; do not wait or poll for it.", to, raised.ID), true
	}
	return "", false
}

// askPacket is q as a question packet: its head, its answers each with what
// happens, and the asker's pick with its reason. The caller fills in who
// decides, where it is from and who raised it.
func askPacket(q Question) teams.Packet {
	p := teams.Packet{Kind: teams.PacketQuestion, Question: strings.TrimSpace(q.Head)}
	byKey := map[string]string{}
	for _, option := range q.Options {
		label := strings.TrimSpace(option.Label)
		if label == "" {
			continue
		}
		consequence := strings.TrimSpace(option.Consequence)
		if consequence == "" {
			consequence = strings.TrimSpace(option.Body)
		}
		if consequence == "" {
			consequence = "the asker goes on with " + label
		}
		id := strconv.Itoa(len(p.Options) + 1)
		byKey[option.Key] = id
		p.Options = append(p.Options, teams.Option{ID: id, Label: label, Consequence: consequence})
	}
	if q.Pick != nil {
		if id, ok := byKey[q.Pick.Key]; ok {
			reason := strings.TrimSpace(q.Pick.Reason)
			if reason == "" {
				reason = "the asker's own pick"
			}
			p.Recommendation = &teams.Recommendation{Option: id, Reason: reason}
		}
	}
	return p
}

// ── the manager's two verbs ─────────────────────────────────────────────────

// packetTools are the manager's verbs for packets waiting on it.
func (a *Agent) packetTools() []bare.Tool {
	return []bare.Tool{
		{Name: teamDecideToolName, Description: teamDecideDescription, Schema: json.RawMessage(teamDecideSchema), Execute: a.teamDecideTool},
		{Name: teamEscalateToolName, Description: teamEscalateDescription, Schema: json.RawMessage(teamEscalateSchema), Execute: a.teamEscalateTool},
	}
}

// managedPacket is packet id when it waits on a team this conversation
// manages, with that team, and the refusal to hand the model otherwise.
func (a *Agent) managedPacket(id, want string) (teams.Packet, teams.Team, string) {
	id = strings.TrimSpace(id)
	team, _, refusal := a.teamTarget(want, true)
	if refusal != "" {
		// One manager in several teams names none: the packet says which.
		if want != "" || !strings.Contains(refusal, "more than one") {
			return teams.Packet{}, teams.Team{}, refusal
		}
	}
	profile := a.config.teamProfile()
	p, err := teams.PacketByID(profile, id)
	if err != nil {
		return teams.Packet{}, teams.Team{}, fmt.Sprintf("There is no packet %q.", id)
	}
	if !p.Waiting() {
		return teams.Packet{}, teams.Team{}, fmt.Sprintf("Packet %s was already decided (%s). Nothing was done.", p.ID, p.Decision)
	}
	if p.Team == teams.Person {
		return teams.Packet{}, teams.Team{}, fmt.Sprintf("Packet %s waits on the person, not on you: a %s is theirs to decide.", p.ID, p.Kind)
	}
	file, keys, _, _ := a.teamSnapshot(profile)
	if file == nil {
		return teams.Packet{}, teams.Team{}, "The teams file could not be read."
	}
	at, ok := file.Team(p.Team)
	if !ok || !teamHoldsKey(keys, at.Manager) {
		return teams.Packet{}, teams.Team{}, fmt.Sprintf("Packet %s waits on another team's manager, not on you. Nothing was done.", p.ID)
	}
	if team.ID != "" && team.ID != at.ID {
		return teams.Packet{}, teams.Team{}, fmt.Sprintf("Packet %s waits on %q, not on %q.", p.ID, at.Name, team.Name)
	}
	return p, at, ""
}

func (a *Agent) teamDecideTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed struct {
		Packet string `json:"packet"`
		Answer string `json:"answer"`
		Reason string `json:"reason"`
		Team   string `json:"team"`
	}
	if err := decodeToolArguments(args, &parsed); err != nil {
		return invalidArgumentsPrefix + err.Error(), true, nil
	}
	answer := strings.TrimSpace(parsed.Answer)
	if answer == "" {
		return invalidArgumentsPrefix + "answer is empty", true, nil
	}
	p, team, refusal := a.managedPacket(parsed.Packet, parsed.Team)
	if refusal != "" {
		return refusal, true, nil
	}
	switch p.Kind {
	case teams.PacketCap, teams.PacketClosing:
		return fmt.Sprintf("A %s is the person's to decide, never a manager's. Nothing was done.", p.Kind), true, nil
	}
	// AN OPTION MAY BE NAMED BY ITS LABEL: a model that writes "JSON" for
	// option 1 means option 1.
	for _, option := range p.Options {
		if strings.EqualFold(option.Label, answer) {
			answer = option.ID
		}
	}
	decided, err := teams.Decide(a.config.teamProfile(), p.ID, teams.FromManager, answer, strings.TrimSpace(parsed.Reason))
	if err != nil {
		return "The decision could not be recorded: " + teamErrorWords(err), true, nil
	}
	return fmt.Sprintf("Decided %s in %q: %s. %s is handed your answer marked \"◆ answered\" and continues; the traffic shows it.",
		decided.ID, team.Name, packetWord(decided), raiserWord(decided.RaisedBy)), false, nil
}

func (a *Agent) teamEscalateTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed struct {
		Packet string `json:"packet"`
		To     string `json:"to"`
		Reason string `json:"reason"`
		Team   string `json:"team"`
	}
	if err := decodeToolArguments(args, &parsed); err != nil {
		return invalidArgumentsPrefix + err.Error(), true, nil
	}
	reason := strings.TrimSpace(parsed.Reason)
	if reason == "" {
		return invalidArgumentsPrefix + "reason is empty", true, nil
	}
	p, team, refusal := a.managedPacket(parsed.Packet, parsed.Team)
	if refusal != "" {
		return refusal, true, nil
	}
	profile := a.config.teamProfile()
	to, where := teams.Person, "the person"
	if strings.TrimSpace(parsed.To) != teams.Person {
		file, _, _, _ := a.teamSnapshot(profile)
		if home, ok := homeOf(file, []string{team.Manager}); ok {
			to = home.Team
			if at, ok := file.Team(home.Team); ok {
				where = fmt.Sprintf("the manager of %q", at.Name)
			}
		}
	}
	if _, err := teams.Escalate(profile, p.ID, teams.FromManager, to, reason); err != nil {
		return "The packet could not be sent up: " + teamErrorWords(err), true, nil
	}
	return fmt.Sprintf("Sent %s up to %s. Its asker is told it went up and is handed the answer when it is decided.", p.ID, where), false, nil
}

// teamErrorWords is a store error as a sentence for the model.
func teamErrorWords(err error) string {
	switch {
	case errors.Is(err, teams.ErrDecided):
		return "it was already decided."
	case errors.Is(err, teams.ErrNotDecider):
		return "it does not wait on you."
	case errors.Is(err, teams.ErrSideways):
		return "a packet goes up the tree or to the person, never sideways or down."
	case errors.Is(err, teams.ErrNoPacket):
		return "there is no such packet."
	}
	return err.Error()
}

// raiserWord names a packet's raiser in a sentence.
func raiserWord(by string) string {
	switch by {
	case teams.FromManager:
		return "The manager"
	case teams.FromSystem:
		return "codeaf"
	case teams.Person:
		return "The person"
	}
	return "@" + strings.TrimPrefix(by, "@")
}

// packetWord is a decided packet's decision in words: the option's label, or
// the decider's own words.
func packetWord(p teams.Packet) string {
	if option, ok := p.Option(p.Decision); ok {
		return option.Label
	}
	return p.Decision
}

// ── delivery and the wake ───────────────────────────────────────────────────

// teamPacketText bounds one packet as delivered.
const teamPacketText = 3000

// teamPacketLine is a [teams.KindPacket] line as this conversation is told it,
// "" for one that is not its business. Its packet is read by id (the packet
// files' fold, stat-first, teams' decision.go), which only a packet line
// costs.
//
//   - A MANAGER is handed a packet newly waiting on its team whole, and told
//     when a packet it raised (its own question) was decided or sent up, and
//     when the person decided its team's closing report, which closes the
//     team on a Close ([closingDecidedLine]).
//   - A MEMBER is told when its own question was answered (`◆ answered: …`)
//     or sent up.
func teamPacketLine(profile string, role teamRole, entry teams.Entry) string {
	p, ok := packetOf(profile, entry)
	if !ok {
		return ""
	}
	if closingDecided(role, entry, p) {
		return closingDecidedLine(profile, p)
	}
	return packetLineFor(role, entry, p)
}

// packetOf is the packet a packet line is about, as it stands.
func packetOf(profile string, entry teams.Entry) (teams.Packet, bool) {
	if entry.Kind != teams.KindPacket || entry.Packet == "" {
		return teams.Packet{}, false
	}
	p, err := teams.PacketByID(profile, entry.Packet)
	return p, err == nil
}

// closingDecided reports whether entry is the decision on role's own team's
// closing report, for its manager.
func closingDecided(role teamRole, entry teams.Entry, p teams.Packet) bool {
	return role.manager && p.Kind == teams.PacketClosing && p.Origin == role.id && entry.State == teams.PacketDecided
}

// packetLineFor is [teamPacketLine] for every packet but a closing report's
// decision, off the packet already read.
func packetLineFor(role teamRole, entry teams.Entry, p teams.Packet) string {
	mine := p.Origin == role.id && (role.manager && p.RaisedBy == teams.FromManager ||
		!role.manager && role.handle != "" && p.RaisedBy == role.handle)
	if p.Kind == teams.PacketClosing || p.Kind == teams.PacketCap {
		// The person's cards: a manager is told of its closing report's
		// decision above, and of nothing else about these two here.
		return ""
	}
	if p.Kind == teams.PacketConflict {
		// A CONFLICT'S DECISION REACHES THE PARTIES AS A RULING, a directive
		// the store writes to each (team_nest.go), so its packet line hands
		// them nothing more; a party that did not raise it is told it was
		// raised.
		if entry.State == teams.PacketDecided {
			return ""
		}
		if told := partyTold(role, entry, p); told != "" && !(role.manager && p.Team == role.id) {
			return told
		}
	}
	switch entry.State {
	case teams.PacketOpen, teams.PacketEscalated:
		if role.manager && p.Waiting() && p.Team == role.id && entry.State == p.State {
			return cutRunesTeam(packetBrief(p), teamPacketText)
		}
		if mine && entry.State == teams.PacketEscalated {
			return fmt.Sprintf("◆ your question %s was sent up, to %s: %s. Its answer comes to you when it is decided.", p.ID, packetDecider(p), oneLineTeam(p.Reason))
		}
	case teams.PacketDecided:
		if !mine {
			return ""
		}
		return answeredLine(p)
	}
	return ""
}

// answeredLine is a decided packet as its raiser is handed it.
func answeredLine(p teams.Packet) string {
	by := "by the manager"
	if p.DecidedBy == teams.Person {
		by = "by the person"
	} else if p.DecidedBy != "" && p.DecidedBy != teams.FromManager {
		by = "by ◆ @" + p.DecidedBy
	}
	line := "◆ answered: " + indentAfterFirst(cutRunesTeam(packetWord(p), teamEntryText)) + " (" + by
	if reason := strings.TrimSpace(p.Reason); reason != "" {
		line += ", because " + oneLineTeam(reason)
	}
	return line + "). Your question was: " + oneLineTeam(p.Question)
}

// packetDecider names who a packet waits on.
func packetDecider(p teams.Packet) string {
	if p.Team == teams.Person {
		return "the person"
	}
	return "a manager above"
}

// packetBrief is a packet whole, as the manager it waits on is handed it.
func packetBrief(p teams.Packet) string {
	var b strings.Builder
	fmt.Fprintf(&b, "◆ %s %s from %s, waiting on you: %s", p.Kind, p.ID, raiserName(p.RaisedBy), oneLineTeam(p.Question))
	if p.Kind == teams.PacketConflict {
		var sides []string
		for _, party := range p.Parties {
			sides = append(sides, "@"+party.Handle)
		}
		fmt.Fprintf(&b, "\n    parties: %s. Your ruling reaches each of them as a directive; you may team_read a party first (team/@handle for one in a team under yours).", strings.Join(sides, ", "))
	}
	for _, party := range p.Parties {
		if context := strings.TrimSpace(party.Context); context != "" {
			who := "@" + party.Handle
			if party.Handle == "" {
				who = "the asker"
			}
			fmt.Fprintf(&b, "\n    %s says: %s", who, oneLineTeam(context))
		}
	}
	for _, option := range p.Options {
		fmt.Fprintf(&b, "\n    [%s] %s: %s", option.ID, option.Label, oneLineTeam(option.Consequence))
	}
	if r := p.Recommendation; r != nil {
		fmt.Fprintf(&b, "\n    recommended: %s, because %s", r.Option, oneLineTeam(r.Reason))
	}
	for _, hop := range p.Trail {
		fmt.Fprintf(&b, "\n    sent up by %s: %s", hop.By, oneLineTeam(hop.Reason))
	}
	b.WriteString("\n    Decide it with team_decide (an option id, or your own words), or send it up with team_escalate if it is not yours to decide.")
	return b.String()
}

// raiserName names a raiser in a packet's first line.
func raiserName(by string) string {
	switch by {
	case teams.FromManager:
		return "a manager"
	case teams.FromSystem:
		return "codeaf"
	case teams.Person:
		return "the person"
	}
	return "@" + strings.TrimPrefix(by, "@")
}

// teamPacketWakes reports whether a packet line starts this conversation's
// turn when it is idle: for a manager, a packet newly waiting on its team, its
// own packet decided or sent up, or its team's closing report decided; for a
// member, its own question decided. It closes nothing: that is the delivery's.
func teamPacketWakes(profile string, role teamRole, entry teams.Entry) bool {
	p, ok := packetOf(profile, entry)
	if !ok {
		return false
	}
	if closingDecided(role, entry, p) {
		return true
	}
	if partyTold(role, entry, p) != "" && !(role.manager && p.Team == role.id) {
		// Told, not woken: the ruling is what it acts on.
		return false
	}
	return packetLineFor(role, entry, p) != "" && (role.manager || entry.State == teams.PacketDecided)
}
