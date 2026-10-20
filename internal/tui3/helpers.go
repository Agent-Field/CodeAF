package tui3

// + New from this session, AND /helpers — THIS CONVERSATION BECOMES A TEAM'S
// MANAGER, AND THE TEAM IS ASKED FOR HELPERS.
//
// Every other door onto a team starts from the team: the Teams page's `+ New AI
// team`, the conversation grid's `+ New team`, Organize, and a manager's own
// sub-team start. Each makes a team first and a manager after, if at all, and
// the manager `+ AI manager` makes is a NEW conversation that knows nothing yet.
// This door starts from the other end. The person is already deep in a piece of
// work, it has grown, and they want it to become a team effort with the
// conversation that knows the work running it.
//
// IT IS TWO STEPS, AND EACH ONE ANSWERS AT ONCE.
//
//  1. The Teams dropdown's last row, `+ New from this session` (teammenu.go),
//     MAKES THE TEAM WHEN IT IS CHOSEN ([app.helpersNewTeam]): a new top-level
//     team with the conversation in front as its manager and only member, in
//     ONE store edit ([app.teamCreateLed]) — the same edit the Teams page's
//     dialog makes, with the manager line added, so every rule a team made
//     there meets is met here. The strip narrows to it on the same frame, so
//     the person sees `◆ Manager` and the column turns to the team's Traffic:
//     the choice has an answer nobody can miss. The box is given `/helpers`
//     and [helpersPrompt], in place of what was there, for the second step.
//  2. `/helpers` ASKS THE TEAM THIS CONVERSATION MANAGES FOR HELPERS
//     ([app.runHelpersCommand]): the words after it, or [helpersPrompt], sent
//     as the person's message, and nothing else. It makes no team. In a
//     conversation that manages none it changes nothing and says where the
//     team is made.
//
// IT USED TO BE ONE STEP, and that was the defect. `/helpers` made the team and
// sent the ask, and the row only filled the box; a person who chose the row saw
// nothing happen above, and the words that appeared in the box below were
// missed. A team made on the click is the reaction; helpers make helpers.
//
// NOTHING IS SENT THAT THE PERSON DID NOT SEE. The words the model reads are
// the words in the transcript after `/helpers`, exactly. What the model knows
// beyond those words is what every manager knows: the role note internal/session
// lands at its first step, and its verbs' own descriptions. A /helpers pressed
// before the store has the team is held until it does ([app.helpersSettled]),
// so the turn's first step finds the conversation already a manager.
//
// THE ROW IS FOR A CONVERSATION SEEN WITH NO TEAM SELECTED. With a team's
// overlay on the strip the row is absent (teammenu.go's [app.teamMenuHelpers]),
// because the team it makes is a top-level team and making it from inside
// another team would read as nesting; a team inside a team is `+ Add subteam`
// on Teams, or a manager's own sub-team start.
//
// THE PERSON ANSWERS IN THEIR OWN WORDS. There is no card. Yes is a reply the
// manager acts on with `team_start`, which runs under the approval posture every
// other start does, and each helper it starts opens as a tab in this team's
// overlay (teamtraffic.go's [app.trafficStarted]); a different list is a reply
// it proposes again from; and cancel is a reply it acts on with
// `team_dissolve`, which takes back a team that has never held anyone but its
// manager and is on the belt only while that is true (internal/session's
// team_dissolve.go). Disband on Teams ends it too, and keeps it as history.

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// helpersPrompt is what a bare /helpers asks, and what the dropdown's row puts
// in the box after `/helpers `. It is the person speaking, in one sentence of
// words they would use, because it is their message and it is on their screen:
// it names no tool and nothing the person could not have typed. It asks for a
// proposal and says what happens next — the person answers yes, a change, or
// cancel — which is what keeps the manager from starting anyone first.
//
// IT DESCRIBES THIS EXCHANGE AND STATES NO STANDING RULE. An earlier wording
// ended "Don't start anyone until I agree, and if I say cancel, undo the team.",
// and the memory that notices rules (internal/session's memory) kept that as a
// standing order for the whole project, read by every manager after. Said as
// the next step of this one conversation, there is nothing in it to keep.
const helpersPrompt = "Suggest two or three helpers for this work, and I'll say yes, change them, or cancel."

// The words this door says. Each is quoted in the manual exactly as spelled.
const (
	// helpersRowWord is the Teams dropdown's last row, which makes the team
	// and puts /helpers and [helpersPrompt] in the box.
	helpersRowWord = "+ New from this session"
	// helpersPlaceWord is /helpers, or the row, where no conversation is in
	// front.
	helpersPlaceWord = "/helpers asks the team a conversation manages for helpers · open that conversation first"
	// helpersFreshWord is the row on the welcome screen, before there is a
	// conversation to make a team of.
	helpersFreshWord = "+ New from this session makes a team of a conversation you have started · say what you are working on first"
	// helpersNotManagerWord is /helpers in a conversation that manages no
	// team: it says where the team is made, and changes nothing.
	helpersNotManagerWord = "/helpers asks this conversation's team for helpers, and it has none yet · make one with + New from this session in the Teams menu"
	// helpersManagesWord is the row in a conversation that already manages a
	// team; the strip is narrowed to that team.
	helpersManagesWord = "this conversation already manages %s · ask it with /helpers"
	// helpersPendingWord is the row chosen again while its team is being saved.
	helpersPendingWord = "the team is still being saved"
	// helpersMadeWord is the receipt, once the store has the team.
	helpersMadeWord = "made the team %s · this conversation is its manager"
	// helpersAwayWord is the receipt when the person left the conversation
	// before the store answered, with a /helpers waiting: the team is made and
	// the message is not sent.
	helpersAwayWord = "made the team %s · its manager is the conversation you left; ask it with /helpers there"
)

// helpersShared is the refusal on a connection that holds one conversation at
// a time: a helper is a conversation that opens beside its manager.
var helpersShared = "helpers open beside this conversation, and " + oneConversationWord

// helpersPending is one team the row made, between its edit and the store's
// answer: the edit's notice tie, the conversation it made a manager, the
// team's id and the name it was made with, the line it put in the box, and a
// /helpers pressed meanwhile (ask and shown), held until the store has the
// team. due says the answer is in and [app.helpersSettled] has not acted on
// it yet.
type helpersPending struct {
	said  teamWriteSaid
	key   string
	id    string
	name  string
	box   string
	ask   string
	shown string
	due   bool
}

// ── THE TEAM'S NAME, FROM THE CONVERSATION ──────────────────────────────────
//
// THE ROW NAMES THE TEAM TWICE, and the first name is only a placeholder. A
// click has to answer on its frame, and no model answers on a frame, so the
// team is made at once under its folder's name ([teamFreshName], the same word
// the conversation grid offers). Beside the loop, the naming role is asked —
// once, bounded by [teamNameWait], on the cheap model conversations name
// themselves with — for one or two words naming the work in this
// conversation, read off its title or its opening words. When that answer
// comes, the team takes it.
//
// THE ANSWER NEVER OVERWRITES A CHOICE. It is applied inside the store's own
// edit, and only while the team still has the name it was made with: a team
// the person has renamed meanwhile, a team that is gone, and a name another
// team already has all leave the team exactly as it is, and so does a failure,
// a timeout or an answer that is not a name. Nothing is said for any of them;
// the folder's name was a real name all along.

// helpersNameWords is the most words a team made from one conversation is
// named with: the work's name, not a description of it.
const helpersNameWords = 2

// helpersNamedWord is said in the manager's conversation when the team takes
// its name from the conversation.
const helpersNamedWord = "named the team %s"

// helpersNamer is the naming ask with a word limit (internal/session's
// teamname.go, and over the wire internal/remote's teamask.go). An agent that
// only has the group's ask ([teamNamer]) is asked that, and its answer is cut
// to [helpersNameWords].
type helpersNamer interface {
	NameTeamWords(ctx context.Context, titles []string, words int) (string, error)
}

// helpersNaming is the one name being asked for: which ask it is (gen), and
// the team, the name it was made with, and its manager.
type helpersNaming struct {
	gen  int
	id   string
	from string
	key  string
}

// helpersAskName asks for team id's name, beside the loop, from title: the
// conversation in front, which the row has just made the team's manager.
func (a *app) helpersAskName(id, from, key, title string) tea.Cmd {
	a.helpersName.gen++
	a.helpersName = helpersNaming{gen: a.helpersName.gen, id: id, from: from, key: key}
	title = strings.TrimSpace(title)
	if title == "" || a.agent == nil {
		return nil
	}
	worded, hasWords := a.agent.(helpersNamer)
	grouped, hasGroup := a.agent.(teamNamer)
	if !hasWords && !hasGroup {
		return nil
	}
	gen := a.helpersName.gen
	return a.besideLine(func() func(bool) tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), teamNameWait)
		defer cancel()
		var name string
		var err error
		if hasWords {
			name, err = worded.NameTeamWords(ctx, []string{title}, helpersNameWords)
		} else {
			name, err = grouped.NameTeam(ctx, []string{title})
		}
		return func(bool) tea.Cmd {
			a.helpersNamed(gen, name, err)
			return a.wake()
		}
	})
}

// helpersNamed takes answer gen as the team's name, when it is a name and the
// team will still take it (see above). The edit itself checks again under the
// store's lock, so a rename made in another window is not undone either.
func (a *app) helpersNamed(gen int, name string, err error) {
	n := a.helpersName
	if gen != n.gen || err != nil {
		return
	}
	words := strings.Fields(strings.ToLower(strings.TrimSpace(name)))
	if len(words) > helpersNameWords {
		words = words[:helpersNameWords]
	}
	name = ansi.Truncate(strings.Join(words, " "), teamNameCells, "")
	if name == "" || strings.EqualFold(name, n.from) {
		return
	}
	t, ok := a.teamByID(n.id)
	if !ok || t.Closed() || t.Name != n.from {
		return
	}
	for _, other := range a.wall.teams {
		if other.ID != n.id && strings.EqualFold(other.Name, name) {
			return
		}
	}
	id, from := n.id, n.from
	if a.teamEdit(func(f *teamstore.File) error {
		i := teamIndex(f.Teams, id)
		if i < 0 || f.Teams[i].Closed() || f.Teams[i].Name != from {
			return nil
		}
		for j, other := range f.Teams {
			if j != i && strings.EqualFold(other.Name, name) {
				return nil
			}
		}
		f.Teams[i].Name = name
		return nil
	}) != nil {
		return
	}
	// THE RECEIPT SAYS THE NAME ONCE. A name in before the store has taken
	// the team is in the receipt that follows ([app.helpersSettled] reads the
	// team's name as it is then); one after it is said on its own line.
	waiting := a.helpers.id == n.id && a.helpers.said.pending
	if a.frontTabKey() == n.key && !waiting {
		a.note(fmt.Sprintf(helpersNamedWord, name))
	}
	a.touch()
}

// helpersTab is the conversation in front as a strip tab, and false when there
// is none to make a team of.
func (a *app) helpersTab() (chatTab, bool) {
	if a.agent == nil || a.startingChat() {
		return chatTab{}, false
	}
	key := a.frontTabKey()
	if key == "" {
		return chatTab{}, false
	}
	return chatTab{key: key, file: a.file, where: a.workspace, word: a.conversationName()}, true
}

// helpersTeam is the open team the conversation with key manages, the global
// manager's `All teams` aside: it holds managers rather than helpers. Memory
// only.
func (a *app) helpersTeam(key string) (team, bool) {
	if !a.wall.loaded || key == "" {
		return team{}, false
	}
	if t, ok := a.teamActive(); ok && t.Manager == key && !t.Closed() && !t.Root {
		return t, true
	}
	for _, t := range a.wall.teams {
		if t.Manager == key && !t.Closed() && !t.Root {
			return t, true
		}
	}
	return team{}, false
}

// helpersRowRefusal is why the row cannot make a team of the conversation in
// front, "" when it can, and the team it already manages when that is why.
func (a *app) helpersRowRefusal() (string, team) {
	switch {
	case a.pageShowing():
		return helpersPlaceWord, team{}
	case a.teamsOff():
		return teamHostedWord, team{}
	case a.shared:
		return helpersShared, team{}
	}
	tab, ok := a.helpersTab()
	if !ok {
		return helpersFreshWord, team{}
	}
	if a.helpers.said.pending {
		return helpersPendingWord, team{}
	}
	a.teamsEnsure()
	if t, manages := a.helpersTeam(tab.key); manages {
		return fmt.Sprintf(helpersManagesWord, t.Name), t
	}
	return "", team{}
}

// helpersTipArmed is the 💡 tip's rule (notice.go): a conversation that has
// been spoken to, in front, with no team selected, not already a manager, and
// on a window that can keep teams and open helpers beside it — which is where
// the row would make its team. It reads memory only, because the table asks it
// at every event.
func helpersTipArmed(a *app) bool {
	if a.turn < 1 || a.pageShowing() || a.teamsOff() || a.shared || !a.wall.loaded {
		return false
	}
	if _, selected := a.teamActive(); selected {
		return false
	}
	_, manages := a.helpersTeam(a.frontTabKey())
	return !manages
}

// helpersNewTeam is the Teams dropdown's `+ New from this session`: the team
// made and shown at once, and `/helpers ` and [helpersPrompt] put in the box,
// in place of what was there, for the person to read, change and send. What
// was in the box goes onto the drafts ring first, as a draft restored over it
// does (draftpage.go), so replacing it loses nothing. Where the row cannot
// make a team it says why and leaves the box alone.
func (a *app) helpersNewTeam() tea.Cmd {
	a.noticeEvent(eventHelpersUsed)
	a.closeTeamMenu()
	if a.wall.on {
		a.closeWall()
	}
	if why, managed := a.helpersRowRefusal(); why != "" {
		a.note(why)
		if managed.ID != "" {
			return a.teamActivate(managed.ID)
		}
		return nil
	}
	tab, _ := a.helpersTab()
	name := teamFreshName([]chatTab{tab}, a.teamNames(), "", rand.IntN)
	hue := nextTeamHue(a.teamHues(""), teamReservedHues(a.pal))
	id, err := a.teamCreateLed(name, []chatTab{tab}, hue, tab.key)
	if err != nil {
		// THE EDIT REFUSED WHAT THIS WINDOW HOLDS, so nothing was queued and
		// nothing changed: the store's own rules, in the store's own words.
		a.note("could not make a team of this conversation: " + err.Error())
		return nil
	}
	// A REFUSAL FROM THE STORE PUTS THE WINDOW BACK the way a refused `+ AI
	// manager` does ([app.teamsTake]): the overlay closes and the store's
	// teams are taken, so a team the store never had is not drawn.
	a.teamsWatchManagerStart(tab.key)
	line := "/helpers " + helpersPrompt
	if live := a.input.String(); strings.TrimSpace(live) != "" && live != line {
		a.drafts.push([]rune(live))
	}
	a.input.setText(line)
	a.helpers = helpersPending{said: a.teamWriteWatch(nil), key: tab.key, id: id, name: name, box: line}
	a.touch()
	return tea.Batch(a.teamActivate(id), a.syncLists(), a.helpersAskName(id, name, tab.key, tab.word))
}

// runHelpersCommand is /helpers: the person's words, or [helpersPrompt], sent
// to the conversation in front as their message, when it manages a team. The
// transcript shows exactly what was sent. A team the row is still saving holds
// the words until the store has it.
func (a *app) runHelpersCommand(rest string) tea.Cmd {
	a.noticeEvent(eventHelpersUsed)
	switch {
	case a.pageShowing():
		a.note(helpersPlaceWord)
		return nil
	case a.teamsOff():
		a.note(teamHostedWord)
		return nil
	case a.shared:
		a.note(helpersShared)
		return nil
	}
	ask := strings.TrimSpace(rest)
	if ask == "" {
		ask = helpersPrompt
	}
	shown := "/helpers " + ask
	front := a.frontTabKey()
	if a.helpers.said.pending && a.helpers.key == front && front != "" {
		a.helpers.ask, a.helpers.shown = ask, shown
		return nil
	}
	a.teamsEnsure()
	t, manages := a.helpersTeam(front)
	if a.agent == nil || a.startingChat() || !manages {
		a.note(helpersNotManagerWord)
		return nil
	}
	// THE STRIP SHOWS THE TEAM BEING ASKED. The conversation in front is its
	// manager, so this moves no focus.
	var show tea.Cmd
	if active, ok := a.teamActive(); !ok || active.ID != t.ID {
		show = a.teamActivate(t.ID)
	}
	return tea.Batch(show, a.submitShown(ask, shown, nil))
}

// helpersSettled is a row's team whose write has come back: the receipt when
// the store took the team, and a /helpers held meanwhile sent; and, when it
// refused it, the box given back its own words, because [app.teamsTake] has
// already said why and the window has taken the store's teams back.
func (a *app) helpersSettled() tea.Cmd {
	p := a.helpers
	if !p.due {
		return nil
	}
	a.helpers = helpersPending{}
	if p.said.why != "" {
		if a.input.String() == p.box {
			a.input.reset()
			a.touch()
		}
		return nil
	}
	name := p.name
	if t, ok := a.teamByID(p.id); ok {
		name = t.Name
	}
	if a.frontTabKey() != p.key {
		if p.ask != "" {
			a.note(fmt.Sprintf(helpersAwayWord, name))
		}
		return nil
	}
	a.note(fmt.Sprintf(helpersMadeWord, name))
	if p.ask == "" {
		return nil
	}
	return a.submitShown(p.ask, p.shown, nil)
}
