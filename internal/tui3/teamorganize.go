package tui3

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── ORGANIZE: TEAMS SUGGESTED FOR THE CONVERSATIONS, NEVER APPLIED ALONE ────
//
// On the wall's All view the Teams row ends in ` ✦ Organize `. A press (or o)
// puts up a card of suggested teams, each ticked, and nothing changes until
// Apply; Undo, on the Teams row for a few seconds after, puts the list back
// exactly as it was.
//
// THE SUGGESTIONS COME FROM TWO PASSES. The folder pass is free and exact:
// conversations that share a project folder, two or more, are a team named
// after it, or additions to a team that already has its name. The model pass
// is one call on the naming role ([session.Agent.ProposeTeams]) for what a
// folder cannot see. The two are merged with the folder pass winning, and when
// the model is absent or fails the folder pass is shown alone and says so.
//
// IT ONLY EVER ADDS. A suggestion is a new team or conversations added to one
// that exists; nothing here removes a member or renames a team, so pressing it
// again is safe and nothing runs until a person presses it.
//
// The painter half is pure, as the rest of the wall's is: the card and the
// button are drawn from [wallView.org] and never read the app.

// orgProp is one suggestion: a new team when team is "", else conversations
// added to the team with that id.
type orgProp struct {
	team string
	// name is the new team's name, or the existing team's, for drawing; hue is
	// the colour the new team will get, or the existing team's own.
	name string
	hue  teamHueSpec
	// keys are the conversations it adds, and names their titles in order.
	keys  []string
	names []string
	// reason is the model's few words on why, "" for the folder pass; folder
	// says the folder pass made it.
	reason string
	folder bool
	take   bool
	// closes is the teams a `Close N quiet teams` suggestion closes (ruling
	// c-9): open teams with no activity for [teamstore.QuietAfter] and nothing
	// waiting. It is the one suggestion that is not about membership, and its
	// team is [orgCloseRow].
	closes []string
}

// orgCloseRow is the team field of the close-quiet suggestion: no team's id,
// so Apply's membership pass passes over it.
const orgCloseRow = "\x00close"

// wallOrganize is Organize's whole state, and the painter's input once the
// frame has filled in loose and clean.
type wallOrganize struct {
	// on says the card is up and thinking that the model is still being asked.
	on, thinking bool
	// gen counts the asks, so an answer to one the card has moved past is
	// dropped.
	gen int
	// props are the suggestions in the card's order, new teams first; cursor
	// is the row the keyboard is on, and top the first line the card showed
	// when it had to scroll.
	props  []orgProp
	cursor int
	top    int
	// folderOnly says the model pass did not answer and only the folder pass
	// is shown; cost is what the ask cost, spelled, "" when none was made.
	folderOnly bool
	cost       string
	// undo is the team list as it was before the last Apply, and doneAt, made
	// and added what the Teams row says while Undo is offered.
	undo   []team
	doneAt time.Time
	// undoMade and undoJoins are what the last Apply did, the teams it made by
	// id and the members it added, so Undo can take exactly those back.
	undoMade   []string
	undoJoins  []orgJoin
	undoClosed []string
	made       int
	added      int
	closed     int
	// quiet is the open teams the last off-loop read found quiet, and quietGen
	// the ask it answered.
	quiet    []string
	quietGen int
	// cleanSig is the state of the conversations and teams when the last run
	// found nothing to suggest, and cleanSet says there is one.
	cleanSig uint64
	cleanSet bool
	// loose and clean are the frame's: how many shown conversations are in no
	// team, and whether nothing has changed since a run found nothing.
	loose int
	clean bool
}

// wallOrganizedFor is how long the Teams row offers Undo after an Apply.
const wallOrganizedFor = 6 * time.Second

// wallOrganizeLoose is how many conversations in no team the button counts
// from: under it the count is noise.
const wallOrganizeLoose = 5

// organizeWait bounds the model pass. Past it the folder pass is shown alone.
const organizeWait = 10 * time.Second

// organizeAnswerTokens is what a proposal's answer is taken to cost in
// tokens, for the estimate: a few short JSON rows.
const organizeAnswerTokens = 250

// teamProposer is the door the model pass comes through, asserted as
// [teamNamer] is, so an agent without it is simply not asked.
type teamProposer interface {
	ProposeTeams(ctx context.Context, in session.TeamProposalInput) (session.TeamProposal, error)
}

// orgConv is one conversation as Organize reads it.
type orgConv struct {
	key, title, where string
}

// orgConvOf is tab as an orgConv, and false for what is no conversation to
// organize: the start and work pages, a tab with no key, and one with no name
// yet, which a person could not recognise in a suggestion.
func orgConvOf(tab chatTab) (orgConv, bool) {
	if tab.start || tab.work || tab.key == "" {
		return orgConv{}, false
	}
	title := strings.TrimSpace(tab.full)
	if title == "" {
		title = strings.TrimSpace(tab.word)
	}
	if title == "" {
		return orgConv{}, false
	}
	return orgConv{key: tab.key, title: title, where: tab.where}, true
}

// orgConvs is every open conversation Organize can suggest a team for, in the
// strip's order. A filter does not narrow it: organizing is about all of them.
func (a *app) orgConvs() []orgConv {
	var out []orgConv
	for _, tab := range a.tabList() {
		if c, ok := orgConvOf(tab); ok {
			out = append(out, c)
		}
	}
	return out
}

// folderTeamName is the team name a folder gives: its last element,
// lowercased and cut to [teamNameCells], "" for a root.
func folderTeamName(where string) string {
	base := filepath.Base(filepath.Clean(where))
	if base == "/" || base == "." || base == "" {
		return ""
	}
	return ansi.Truncate(strings.ToLower(base), teamNameCells, "")
}

// teamNamed is the index of the team called name, compared without case, -1
// for none.
func teamNamed(teams []team, name string) int {
	for i, t := range teams {
		if strings.EqualFold(strings.TrimSpace(t.Name), strings.TrimSpace(name)) {
			return i
		}
	}
	return -1
}

// organizeFolders is the folder pass: every folder two or more conversations
// share becomes a new team named after it, unless one team already holds them
// all, and unless a team already has the folder's name, when the ones it is
// missing are suggested for it instead. Two folders of one name are one team.
// It is pure and exact, so the same conversations always give the same rows.
func organizeFolders(convs []orgConv, teams []team) []orgProp {
	groups := map[string][]string{}
	var order []string
	for _, c := range convs {
		if strings.TrimSpace(c.where) == "" {
			continue
		}
		w := filepath.Clean(c.where)
		if _, ok := groups[w]; !ok {
			order = append(order, w)
		}
		if !orgHas(groups[w], c.key) {
			groups[w] = append(groups[w], c.key)
		}
	}
	var out []orgProp
	for _, w := range order {
		keys := groups[w]
		name := folderTeamName(w)
		if len(keys) < 2 || name == "" || teamsHoldAll(teams, keys) {
			continue
		}
		if i := teamNamed(teams, name); i >= 0 {
			var missing []string
			for _, k := range keys {
				if !teamHolds(teams[i], k) {
					missing = append(missing, k)
				}
			}
			out = orgMergeInto(out, orgProp{team: teams[i].ID, name: teams[i].Name, hue: teams[i].HueSpec(), keys: missing, folder: true})
			continue
		}
		out = orgMergeInto(out, orgProp{name: name, keys: keys, folder: true})
	}
	return out
}

// teamsHoldAll reports whether one team holds every one of keys.
func teamsHoldAll(teams []team, keys []string) bool {
	for _, t := range teams {
		all := true
		for _, k := range keys {
			if !teamHolds(t, k) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// orgMergeInto adds p to props, folding it into a row that is the same
// suggestion already: the same existing team, or a new team of the same name.
// The row already there keeps its reason and its place.
func orgMergeInto(props []orgProp, p orgProp) []orgProp {
	if len(p.keys) == 0 {
		return props
	}
	for i, q := range props {
		same := p.team != "" && q.team == p.team ||
			p.team == "" && q.team == "" && strings.EqualFold(q.name, p.name)
		if !same {
			continue
		}
		for _, k := range p.keys {
			if !orgHas(props[i].keys, k) {
				props[i].keys = append(props[i].keys, k)
			}
		}
		return props
	}
	return append(props, p)
}

func orgHas(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// organizeFromModel is the model's answer as rows. The answer was validated
// by the session; what is checked again here is what may have moved while it
// was asked: a team that is gone, and a member a team now holds.
func organizeFromModel(res session.TeamProposal, teams []team) []orgProp {
	var out []orgProp
	for _, n := range res.New {
		if i := teamNamed(teams, n.Name); i >= 0 {
			out = append(out, orgProp{team: teams[i].ID, name: teams[i].Name, hue: teams[i].HueSpec(), keys: orgMissing(teams[i], n.Members)})
			continue
		}
		out = append(out, orgProp{name: n.Name, keys: n.Members, reason: n.Reason})
	}
	for _, add := range res.Additions {
		i := teamIndex(teams, add.TeamID)
		if i < 0 {
			continue
		}
		out = append(out, orgProp{team: teams[i].ID, name: teams[i].Name, hue: teams[i].HueSpec(), keys: orgMissing(teams[i], add.Members)})
	}
	return out
}

// orgMissing is keys less the ones t already holds.
func orgMissing(t team, keys []string) []string {
	var out []string
	for _, k := range keys {
		if !teamHolds(t, k) && !orgHas(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// organizeMerge is the two passes as one list, new teams first. The folder
// pass wins: a model team with a folder team's name, or with exactly its
// members, is dropped, and a model addition to a team the folder pass already
// adds to only brings the members that row lacks.
func organizeMerge(folder, model []orgProp) []orgProp {
	props := append([]orgProp(nil), folder...)
	for _, p := range model {
		if p.team == "" {
			dup := false
			for _, q := range folder {
				if q.team == "" && (strings.EqualFold(q.name, p.name) || orgSameKeys(q.keys, p.keys)) {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
		}
		props = orgMergeInto(props, p)
	}
	var out []orgProp
	for _, p := range props {
		if p.team == "" && len(p.keys) >= 2 {
			out = append(out, p)
		}
	}
	for _, p := range props {
		if p.team != "" && len(p.keys) > 0 {
			out = append(out, p)
		}
	}
	return out
}

func orgSameKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, k := range a {
		if !orgHas(b, k) {
			return false
		}
	}
	return true
}

// organizeSig is the state Organize reads, as one number that any change to
// it moves: which conversations are open and which team holds which. It is a
// sum of per-item hashes, so the order things are listed in does not count,
// and it allocates nothing, so a frame may take it.
func organizeSig(keys []string, teams []team) uint64 {
	var sum uint64
	for _, k := range keys {
		sum += orgHash(k, "", 1)
	}
	for _, t := range teams {
		sum += orgHash(t.ID, "", 2)
		for _, m := range t.Members {
			sum += orgHash(t.ID, m.Key, 3)
		}
	}
	return sum
}

// orgHash is FNV-1a over a, a separator of kind, and b.
func orgHash(a, b string, kind byte) uint64 {
	h := uint64(14695981039346656037)
	step := func(c byte) {
		h ^= uint64(c)
		h *= 1099511628211
	}
	for i := 0; i < len(a); i++ {
		step(a[i])
	}
	step(0)
	step(kind)
	for i := 0; i < len(b); i++ {
		step(b[i])
	}
	return h
}

// ── WIRED ───────────────────────────────────────────────────────────────────

// wallOrganizeOpen is the button and o: the card up, the folder pass drawn at
// once when there is no model to ask, and otherwise one ask off the loop with
// `thinking…` in the card until it answers. It does nothing while a team is
// shown, where the button is not drawn.
func (a *app) wallOrganizeOpen() tea.Cmd {
	if a.wall.activeID != "" {
		return nil
	}
	a.teamsEnsure()
	o := &a.wall.org
	o.gen++
	o.on, o.thinking, o.props, o.cursor, o.top, o.folderOnly, o.cost = true, false, nil, 0, 0, false, ""
	a.wall.pop, a.wall.help, a.wall.naming, a.wall.filterOn = wallPop{}, false, false, false
	a.wall.hover = wallHitRef{}
	a.wall.stirred = true
	a.touch()
	quietAsk := a.wallOrganizeQuietAsk(o.gen)
	convs := a.orgConvs()
	var proposer teamProposer
	if a.agent != nil {
		proposer, _ = a.agent.(teamProposer)
	}
	if proposer == nil || len(convs) < 2 {
		a.wallOrganizeShow(nil, proposer == nil && len(convs) >= 2, "")
		if quietAsk == nil {
			return nil
		}
		gen := o.gen
		return a.besideLine(func() func(here bool) tea.Cmd {
			ids := quietAsk()
			return func(bool) tea.Cmd {
				a.wallOrganizeQuietTake(gen, ids)
				return nil
			}
		})
	}
	in := session.TeamProposalInput{}
	for _, c := range convs {
		in.Conversations = append(in.Conversations, session.TeamProposalConversation{Key: c.key, Title: c.title, Folder: filepath.Base(filepath.Clean(c.where))})
	}
	for _, t := range a.wall.teams {
		pt := session.TeamProposalTeam{ID: t.ID, Name: t.Name}
		for _, m := range t.Members {
			pt.Members = append(pt.Members, m.Key)
		}
		in.Teams = append(in.Teams, pt)
	}
	gen := o.gen
	o.thinking = true
	return a.besideLine(func() func(here bool) tea.Cmd {
		var quiet []string
		if quietAsk != nil {
			quiet = quietAsk()
		}
		ctx, cancel := context.WithTimeout(context.Background(), organizeWait)
		defer cancel()
		res, err := proposer.ProposeTeams(ctx, in)
		return func(bool) tea.Cmd {
			a.wallOrganizeQuietTake(gen, quiet)
			a.wallOrganized(gen, res, err)
			return nil
		}
	})
}

// wallOrganizeQuietAsk is the question, to be asked off the loop in the same
// read as the model's, of which open teams have been quiet for
// [teamstore.QuietAfter] with nothing waiting, for the card's `Close N quiet
// teams`. It reads this machine's profile, so over --host it is nil: the
// engine has no door for it yet, and the card simply does not offer it.
func (a *app) wallOrganizeQuietAsk(gen int) func() []string {
	o := &a.wall.org
	o.quiet, o.quietGen = nil, gen
	if a.hosted() || a.teamsOff() {
		return nil
	}
	dir, now := a.profileDir, a.now()
	tree := &teamstore.File{Teams: teamsClone(a.wall.teams)}
	return func() []string {
		ids, err := teamstore.Quiet(dir, tree, now, teamstore.QuietAfter)
		if err != nil {
			return nil
		}
		return ids
	}
}

// wallOrganizeQuietTake folds the quiet teams in: kept for the card, and put
// on it at once when the card is already showing its suggestions.
func (a *app) wallOrganizeQuietTake(gen int, ids []string) {
	o := &a.wall.org
	if gen != o.quietGen || !o.on {
		return
	}
	o.quiet = ids
	if !o.thinking {
		o.props = a.orgWithQuiet(o.props)
		a.touch()
	}
}

// orgWithQuiet is props with the close-quiet suggestion last, when any team
// is quiet and the suggestion is not there yet.
func (a *app) orgWithQuiet(props []orgProp) []orgProp {
	o := &a.wall.org
	if len(o.quiet) == 0 {
		return props
	}
	for _, p := range props {
		if p.team == orgCloseRow {
			return props
		}
	}
	p := orgProp{team: orgCloseRow, take: true, reason: "Nothing has happened in these for a week; Undo takes it back"}
	for _, id := range o.quiet {
		if t, ok := a.teamByID(id); ok && !t.Closed() && !t.Root {
			p.closes = append(p.closes, id)
			p.names = append(p.names, t.Name)
		}
	}
	if len(p.closes) == 0 {
		return props
	}
	p.name = "Close " + strconv.Itoa(len(p.closes)) + " quiet team"
	if len(p.closes) != 1 {
		p.name += "s"
	}
	return append(props, p)
}

// wallOrganized takes answer gen into the card, if it is still the one being
// waited for and the card is still up. A failure shows the folder pass alone.
func (a *app) wallOrganized(gen int, res session.TeamProposal, err error) {
	o := &a.wall.org
	if gen != o.gen || !o.on || !o.thinking {
		return
	}
	a.touch()
	if err != nil {
		a.wallOrganizeShow(nil, true, "")
		return
	}
	cost := a.organizeCost(res.Model, res.PromptChars)
	a.wallOrganizeShow(organizeFromModel(res, a.wall.teams), false, cost)
}

// wallOrganizeShow puts the merged suggestions in the card: the folder pass,
// read now, with model's rows merged in. Each is ticked, each new team is
// given a colour distinct from every team's and each other's, and each row
// learns its members' titles. A run that suggests nothing remembers the state
// it looked at, so the button can say Organized until something changes.
func (a *app) wallOrganizeShow(model []orgProp, folderOnly bool, cost string) {
	o := &a.wall.org
	convs := a.orgConvs()
	props := organizeMerge(organizeFolders(convs, a.wall.teams), model)
	titles := make(map[string]string, len(convs))
	keys := make([]string, 0, len(convs))
	for _, c := range convs {
		titles[c.key] = c.title
		keys = append(keys, c.key)
	}
	used := a.teamHues("")
	reserved := teamReservedHues(a.pal)
	for i := range props {
		p := &props[i]
		p.take = true
		kept := p.keys[:0]
		for _, k := range p.keys {
			if t, ok := titles[k]; ok {
				kept = append(kept, k)
				p.names = append(p.names, t)
			}
		}
		p.keys = kept
		if p.team == "" {
			p.hue = nextTeamHue(used, reserved)
			used = append(used, p.hue)
		}
	}
	props = a.orgWithQuiet(props)
	o.thinking, o.props, o.folderOnly, o.cost = false, props, folderOnly, cost
	o.cursor, o.top = 0, 0
	o.cleanSet = len(props) == 0 && !folderOnly
	if o.cleanSet {
		o.cleanSig = organizeSig(keys, a.wall.teams)
	}
}

// organizeCost is what the ask cost, estimated from its size: its characters
// at four to a token and a short answer, priced at the model that answered
// when its price is known, and said in tokens when it is not, since a price
// nobody published is not a figure.
func (a *app) organizeCost(model string, chars int) string {
	if chars <= 0 {
		return ""
	}
	in := chars/4 + 1
	if m, ok := a.priceFor(model); ok {
		return "about " + dollars(float64(in)*m.PromptPrice+float64(organizeAnswerTokens)*m.CompletionPrice)
	}
	return "about " + tokenWord(in+organizeAnswerTokens) + " tokens"
}

// wallOrganizeClose puts the card away with no change; an answer still on its
// way is dropped.
func (a *app) wallOrganizeClose() {
	o := &a.wall.org
	o.on, o.thinking = false, false
	o.gen++
	a.wall.hover = wallHitRef{}
	a.wall.stirred = true
	a.touch()
}

// wallOrganizeToggle ticks suggestion i, or unticks it.
func (a *app) wallOrganizeToggle(i int) {
	o := &a.wall.org
	if i < 0 || i >= len(o.props) {
		return
	}
	o.cursor = i
	o.props[i].take = !o.props[i].take
	a.touch()
}

// wallOrganizeApply makes the ticked suggestions, in one save: each new team
// with the name and colour the card showed, which is the name it is made
// with, and each addition's members put into their team. The list as it was
// is kept for Undo. A new team whose name was taken while the card was up
// goes into the team that has it.
func (a *app) wallOrganizeApply() {
	o := &a.wall.org
	if !o.on || o.thinking {
		return
	}
	prior := teamsClone(a.wall.teams)
	now := a.now()
	// What Apply does is worked out against the teams this window holds, once,
	// as new teams (their ids minted here) and members added to teams that
	// exist, and then made through [app.teamEdit], which makes it again to the
	// file as it is on disk. The same two lists are what Undo takes back.
	var fresh []team
	var joins []orgJoin
	var closes []string
	for _, p := range o.props {
		if !p.take {
			continue
		}
		if p.team == orgCloseRow {
			closes = append(closes, p.closes...)
			continue
		}
		members := teamFromTabs(p.name, a.wallTabsFor(p.keys, nil), now).Members
		i := teamIndex(a.wall.teams, p.team)
		if p.team == "" {
			i = teamNamed(a.wall.teams, p.name)
			if i < 0 {
				for j := range fresh {
					if strings.EqualFold(strings.TrimSpace(fresh[j].Name), strings.TrimSpace(p.name)) {
						for _, m := range members {
							if !teamHolds(fresh[j], m.Key) {
								fresh[j].Members = append(fresh[j].Members, m)
								joins = append(joins, orgJoin{team: fresh[j].ID, member: m})
							}
						}
						members = nil
					}
				}
				if members == nil {
					continue
				}
			}
		}
		if i < 0 && p.team == "" && len(members) > 0 {
			made := team{ID: newTeamID(), Name: p.name, Members: members, Made: now}
			made.SetHue(p.hue)
			fresh = append(fresh, made)
			continue
		}
		if i < 0 {
			continue
		}
		t := a.wall.teams[i]
		for _, m := range members {
			if !teamHolds(t, m.Key) && !orgJoined(joins, t.ID, m.Key) {
				joins = append(joins, orgJoin{team: t.ID, member: m})
			}
		}
	}
	a.wallOrganizeClose()
	made, added := len(fresh), len(joins)
	if made == 0 && added == 0 && len(closes) == 0 {
		return
	}
	o.undo, o.doneAt, o.made, o.added, o.closed = prior, now, made, added, len(closes)
	o.undoMade, o.undoJoins, o.undoClosed = nil, joins, closes
	for _, t := range fresh {
		o.undoMade = append(o.undoMade, t.ID)
	}
	err := a.teamEdit(func(f *teamstore.File) error {
		for _, t := range fresh {
			if teamIndex(f.Teams, t.ID) < 0 {
				f.Teams = append(f.Teams, t.Clone())
			}
		}
		for _, j := range joins {
			if teamIndex(f.Teams, j.team) < 0 {
				continue
			}
			if err := f.AddMember(j.team, j.member); err != nil {
				return err
			}
		}
		// A quiet team closes with no report: nothing was running to wrap up.
		for _, id := range closes {
			if t, ok := f.Team(id); !ok || t.Closed() {
				continue
			}
			if err := f.Close(id, now, ""); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		a.note("the teams are kept for this window, but " + err.Error())
	}
	for _, id := range closes {
		if a.wall.activeID == id {
			a.wall.activeID = ""
		}
	}
}

// orgJoin is one conversation Apply put into a team.
type orgJoin struct {
	team   string
	member teamMember
}

// orgJoined reports whether joins already puts key into team id.
func orgJoined(joins []orgJoin, id, key string) bool {
	for _, j := range joins {
		if j.team == id && j.member.Key == key {
			return true
		}
	}
	return false
}

// wallOrganizeUndo takes back the last Apply, while the Teams row still offers
// it: the teams it made are dropped and the members it added are taken out
// again. It is made through [app.teamEdit] like every other edit, so a manager,
// a handle or a member another process wrote since the Apply is kept, and with
// nothing written in between the list is back exactly as it was.
func (a *app) wallOrganizeUndo() {
	o := &a.wall.org
	if o.undo == nil || o.doneAt.IsZero() || a.now().Sub(o.doneAt) >= wallOrganizedFor {
		return
	}
	made, joins, closed := o.undoMade, o.undoJoins, o.undoClosed
	o.undo, o.doneAt = nil, time.Time{}
	o.undoMade, o.undoJoins, o.undoClosed = nil, nil, nil
	a.wall.hover = wallHitRef{}
	a.wall.stirred = true
	a.touch()
	err := a.teamEdit(func(f *teamstore.File) error {
		for _, id := range made {
			teamDrop(f, id)
		}
		for _, j := range joins {
			if teamIndex(f.Teams, j.team) < 0 {
				continue
			}
			if err := f.RemoveMember(j.team, j.member.Key); err != nil {
				return err
			}
		}
		for _, id := range closed {
			if t, ok := f.Team(id); !ok || !t.Closed() {
				continue
			}
			if err := f.Reopen(id); err != nil {
				return err
			}
		}
		return nil
	})
	if teamIndex(a.wall.teams, a.wall.activeID) < 0 {
		a.wall.activeID = ""
	}
	if err != nil {
		a.note("the teams are back for this window, but " + err.Error())
	}
}

// teamsClone is a copy of teams that shares nothing with it, so a change to
// one is never a change to the other.
func teamsClone(teams []team) []team {
	if teams == nil {
		return []team{}
	}
	out := make([]team, len(teams))
	for i, t := range teams {
		out[i] = t.Clone()
	}
	return out
}

// wallOrganizeKey is a key while the card is up; the card has the keyboard.
// ↑ ↓ walk the rows, space ticks one, enter applies (or closes a card with
// nothing in it), esc or q puts it away with no change.
func (a *app) wallOrganizeKey(key string) tea.Cmd {
	o := &a.wall.org
	switch key {
	case "esc", "q":
		a.wallOrganizeClose()
	case "up", "k":
		o.cursor = max(o.cursor-1, 0)
	case "down", "j":
		o.cursor = min(o.cursor+1, max(len(o.props)-1, 0))
	case "space":
		a.wallOrganizeToggle(o.cursor)
	case "enter":
		switch {
		case o.thinking:
		case len(o.props) == 0:
			a.wallOrganizeClose()
		default:
			a.wallOrganizeApply()
		}
	}
	return nil
}

// wallOrganizePress is a press while the card is up: its rows and its
// buttons answer, and nothing else does.
func (a *app) wallOrganizePress(hit wallHit) tea.Cmd {
	switch {
	case hit.kind == wallHitOrgRow:
		a.wallOrganizeToggle(hit.arg)
	case hit.kind == wallHitAction && wallAct(hit.arg) == wallActOrgApply:
		a.wallOrganizeApply()
	case hit.kind == wallHitAction && wallAct(hit.arg) == wallActOrgCancel:
		a.wallOrganizeClose()
	}
	return nil
}

// wallOrganizeFrame fills in what the painter reads of Organize on this
// frame: how many shown conversations are in no team, and whether nothing has
// changed since a run found nothing. It reads memory only.
func (a *app) wallOrganizeFrame(tiles []wallTile, tabs []chatTab) wallOrganize {
	o := a.wall.org
	o.undo = nil
	o.loose = 0
	for _, t := range tiles {
		if len(t.teams) == 0 {
			o.loose++
		}
	}
	o.clean = false
	if o.cleanSet {
		keys := make([]string, 0, len(tabs))
		for _, tab := range tabs {
			if c, ok := orgConvOf(tab); ok {
				keys = append(keys, c.key)
			}
		}
		o.clean = organizeSig(keys, a.wall.teams) == o.cleanSig
	}
	return o
}

// ── PAINTED ─────────────────────────────────────────────────────────────────

// wallOrgPiece is Organize's part of the Teams row: its paint, its width, and
// its targets from its own first cell.
type wallOrgPiece struct {
	s    string
	w    int
	hits []wallHit
}

// wallOrgMarks is the button's two glyphs in the palette's tier.
func wallOrgMarks(ascii bool) (spark, check string) {
	if ascii {
		return "*", ""
	}
	return "✦", " ✓"
}

// wallOrganizeButton is Organize's part of the Teams row on row y: for a few
// seconds after an Apply what it did and an Undo; otherwise, on All only, the
// button, `✦ Organize` with the count of conversations in no team once there
// are five, or a dim `Organized ✓` when the last run found nothing and nothing
// has changed. It is drawn nowhere else.
func wallOrganizeButton(pal palette, g wallGlyphs, v wallView, y int) wallOrgPiece {
	o := v.org
	if !o.doneAt.IsZero() && v.now.Sub(o.doneAt) < wallOrganizedFor && v.now.Sub(o.doneAt) >= 0 {
		var said []string
		if o.made > 0 {
			said = append(said, strconv.Itoa(o.made)+" new "+wallPlural(o.made, "team"))
		}
		if o.added > 0 {
			said = append(said, strconv.Itoa(o.added)+" added")
		}
		if o.closed > 0 {
			said = append(said, strconv.Itoa(o.closed)+" closed")
		}
		word := "Organized " + g.sep + " " + strings.Join(said, ", ") + "  "
		undo := wallButton{act: wallActOrgUndo, label: "Undo"}
		hot := v.hover == wallHitRef{kind: wallHitAction, arg: int(wallActOrgUndo)}
		ww, bw := ansi.StringWidth(word), wallButtonW(undo)
		return wallOrgPiece{s: pal.dim(word) + wallButtonPaint(pal, undo, hot), w: ww + bw,
			hits: []wallHit{{x0: ww, y0: y, x1: ww + bw, y1: y + 1, kind: wallHitAction, arg: int(wallActOrgUndo)}}}
	}
	if v.team != "" {
		return wallOrgPiece{}
	}
	spark, check := wallOrgMarks(pal.ascii)
	hot := v.hover == wallHitRef{kind: wallHitAction, arg: int(wallActOrganize)}
	var s string
	switch {
	case o.clean:
		s = " " + pal.dim("Organized"+check) + " "
	case o.loose >= wallOrganizeLoose:
		s = " " + pal.ink(spark+" Organize") + " " + pal.dim(strconv.Itoa(o.loose)) + " "
	default:
		s = " " + pal.ink(spark+" Organize") + " "
	}
	w := ansi.StringWidth(s)
	if hot {
		s = pal.cursor(s, 0)
	}
	return wallOrgPiece{s: s, w: w, hits: []wallHit{{x0: 0, y0: y, x1: w, y1: y + 1, kind: wallHitAction, arg: int(wallActOrganize)}}}
}

// wallOrgCardMaxW is the widest the card is drawn.
const wallOrgCardMaxW = 72

// wallOrgNameCap is the widest a member's title is drawn in a row's list.
const wallOrgNameCap = 16

// wallOrgCard is the card of suggestions, centred over the grid:
//
//	╭─ Organize ──────────────────────────────────────────────╮
//	│                                                          │
//	│  New teams                                               │
//	│  ☑ ● codeaf          5  from the folder                  │
//	│  ☑ ● nvda research   3  cpu profiling, nvda deep…, 10-K  │
//	│  Add to existing                                         │
//	│  ☑ ● harbor        + 2  relay audit, footprint table     │
//	│                                                          │
//	│  about $0.0020                      Cancel esc  Apply ↵  │
//	│                                                          │
//	╰──────────────────────────────────────────────────────────╯
//
// While the model is asked it says `thinking…`; with nothing to suggest it
// says so beside a Close. When its rows do not fit between the head and the
// foot it drops its padding, then shows a window of them that keeps the
// cursor's row in view, and its bottom border says which way the rest lies.
// top is the first row line it drew, for the wiring to keep.
func wallOrgCard(pal palette, g wallGlyphs, v wallView, width, height int) wallCard {
	o := v.org
	w := min(wallOrgCardMaxW, width-2*wallMargin-2)
	inner := w - 2 - 2*wallCardPadX
	if inner < 30 {
		return wallCard{}
	}
	k := wallKeysFor(pal.ascii)
	var list []wallCardLine
	cursorLine := -1
	var tail []wallCardLine
	button := func(lead string, bs ...wallButton) wallCardLine {
		bw := wallBarWidth(bs, 1)
		s, _, hits := wallLay(pal, bs, v.hover, inner+2-bw, 0, 1)
		lw := ansi.StringWidth(lead)
		if lead != "" && lw+2 > inner+2-bw {
			lead = ""
		}
		pad := strings.Repeat(" ", max(inner+2-bw-lw-1, 0))
		if lead == "" {
			pad = strings.Repeat(" ", max(inner+2-bw, 0))
			return wallCardLine{s: pad + s, hits: hits, bleed: true}
		}
		return wallCardLine{s: " " + pal.dim(lead) + pad + s, hits: hits, bleed: true}
	}
	cancel := wallButton{act: wallActOrgCancel, label: "Cancel", key: "esc"}
	switch {
	case o.thinking:
		word := "thinking…"
		if pal.ascii {
			word = "thinking..."
		}
		list = append(list, wallCardLine{s: pal.dim(word)})
		tail = append(tail, wallCardLine{}, button("", cancel))
	case len(o.props) == 0:
		list = append(list, wallCardLine{s: pal.ink("Everything is organized")})
		tail = append(tail, wallCardLine{}, button("", wallButton{act: wallActOrgCancel, label: "Close", key: "esc"}))
	default:
		nameW, countW := 6, 1
		for _, p := range o.props {
			nameW = max(nameW, min(ansi.StringWidth(p.name), teamNameCells))
			countW = max(countW, len(wallOrgCount(p)))
		}
		heading := func(s string) {
			list = append(list, wallCardLine{s: pal.bold(pal.muted(s))})
		}
		for i, p := range o.props {
			if i == 0 && p.team == "" {
				heading("New teams")
			}
			if p.team != "" && p.team != orgCloseRow && (i == 0 || o.props[i-1].team == "") {
				heading("Add to existing")
			}
			if p.team == orgCloseRow {
				heading("Quiet for a week")
			}
			if i == o.cursor {
				cursorLine = len(list)
			}
			list = append(list, wallOrgRow(pal, g, k, v, p, i, inner, nameW, countW))
		}
		if o.folderOnly {
			tail = append(tail, wallCardLine{}, wallCardLine{s: pal.dim("suggestions from folders only")})
		}
		apply := wallButton{act: wallActOrgApply, label: "Apply", key: k.enter}
		tail = append(tail, wallCardLine{}, button(o.cost, cancel, apply))
	}

	top := wallGridTop
	floor := height - wallFootRows + 1 // the first row a card may not cover
	room := floor - top
	padY := wallCardPadY
	if len(list)+len(tail)+2+2*padY > room {
		padY = 0
	}
	vis := room - 2 - 2*padY - len(tail)
	if vis < 1 {
		return wallCard{}
	}
	over := max(len(list)-vis, 0)
	from := min(max(o.top, 0), over)
	if cursorLine >= 0 {
		// The cursor's row is kept in view, moving the window as little as it
		// can; the heading over the first row comes with it.
		if cursorLine < from {
			from = cursorLine
		}
		if cursorLine >= from+vis {
			from = cursorLine - vis + 1
		}
		if cursorLine == 1 && vis >= 2 {
			from = 0
		}
	}
	from = min(max(from, 0), over)
	lines := append(append([]wallCardLine(nil), list[from:min(from+vis, len(list))]...), tail...)
	h := len(lines) + 2 + 2*padY
	x := (width - w) / 2
	y := top + max((room-h)/2, 0)
	card := wallCardBuild(pal, "Organize", lines, x, y, w, wallCardPadX, padY)
	card.top = from
	if over > 0 {
		card.over = over
		card.rows[len(card.rows)-1] = wallHelpFoot(pal, w, from < over)
	}
	return card
}

// wallOrgCount is a row's count: how many a new team holds, or `+ 2` for how
// many an existing team gains.
func wallOrgCount(p orgProp) string {
	if p.team == orgCloseRow {
		return strconv.Itoa(len(p.closes))
	}
	if p.team != "" {
		return "+ " + strconv.Itoa(len(p.keys))
	}
	return strconv.Itoa(len(p.keys))
}

// wallOrgRow is one suggestion: its box, its colour, its name, its count and
// what it holds, the whole row a target that ticks it.
func wallOrgRow(pal palette, g wallGlyphs, k wallKeys, v wallView, p orgProp, i, inner, nameW, countW int) wallCardLine {
	box := pal.muted(k.boxOff)
	if p.take {
		box = pal.ink(k.boxOn)
	}
	mark := pal.dim(g.cell)
	if ink := pal.teamInk(p.hue); ink != nil {
		mark = ink("●")
	} else if r := []rune(p.name); len(r) > 0 && ansi.StringWidth(string(r[0])) == 1 {
		mark = pal.dim(strings.ToLower(string(r[0])))
	}
	name := p.name
	if ansi.StringWidth(name) > nameW {
		name = ansi.Truncate(name, nameW, g.more)
	}
	count := wallOrgCount(p)
	left := box + " " + mark + " " + pal.ink(name) + strings.Repeat(" ", nameW-ansi.StringWidth(name)+1) +
		strings.Repeat(" ", countW-len(count)) + pal.dim(count) + "  "
	detail := "from the folder"
	if !p.folder || p.team != "" {
		cut := make([]string, 0, len(p.names))
		for _, n := range p.names {
			if ansi.StringWidth(n) > wallOrgNameCap {
				// Cut at a word's end where it can be, so no blank sits
				// before the ellipsis.
				n = strings.TrimRight(ansi.Truncate(n, wallOrgNameCap-ansi.StringWidth(g.more), ""), " ") + g.more
			}
			cut = append(cut, n)
		}
		detail = strings.Join(cut, ", ")
	}
	if room := inner - ansi.StringWidth(left); ansi.StringWidth(detail) > room {
		detail = ansi.Truncate(detail, max(room, 0), g.more)
	}
	lit := v.org.cursor == i || v.hover == wallHitRef{kind: wallHitOrgRow, arg: i}
	return wallCardLine{
		s:    wallPopRowPaint(pal, left+pal.muted(detail), inner, lit),
		hits: []wallHit{{x0: 0, y0: 0, x1: inner, y1: 1, kind: wallHitOrgRow, arg: i}},
	}
}
