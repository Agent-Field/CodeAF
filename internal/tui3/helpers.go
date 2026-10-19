package tui3

// /helpers — THIS CONVERSATION BECOMES A TEAM'S MANAGER, AND IS ASKED FOR HELPERS.
//
// Every other door onto a team starts from the team: the Teams page's `+ New AI
// team`, the conversation grid's `+ New team`, Organize, and a manager's own
// sub-team start. Each makes a team first and a manager after, if at all, and
// the manager `+ AI manager` makes is a NEW conversation that knows nothing yet.
// This door starts from the other end. The person is already deep in a piece of
// work, it has grown, and they want it to become a team effort with the
// conversation that knows the work running it.
//
// SO IT IS TWO THINGS, IN THIS ORDER, AND NOTHING MORE.
//
//  1. A new top-level team is made with the conversation in front as its
//     manager and only member, in ONE store edit ([app.teamCreateLed]): the
//     same edit the Teams page's dialog makes, with the manager line added, so
//     every rule a team made there meets is met here — a fresh name, a colour,
//     one conversation managing one team and its descendants, no deleted
//     conversation readmitted. The strip narrows to it, so the person sees
//     `◆ Manager` and the column turns to the team's Traffic.
//  2. Once the store has TAKEN that edit, and only then, the conversation is
//     sent one turn, shown in the transcript as `/helpers` and carrying the
//     words below ([helpersAskOf]): propose two or three helpers, ask the
//     person, start nobody until they agree. Waiting for the store is what
//     makes the turn's first step find the conversation already a manager, with
//     its verbs on the belt ([teamsWrote], [app.helpersAsked]).
//
// THE PERSON ANSWERS IN THEIR OWN WORDS. There is no card. Yes is a reply the
// manager acts on with `team_start`, which runs under the approval posture every
// other start does; a different list is a reply it proposes again from; and no
// is a reply it acts on with `team_dissolve`, which takes back a team that has
// never held anyone but its manager and is on the belt only while that is true
// (internal/session's team_dissolve.go). After the first helper joins, the team
// ends the way every other team does: disbanded by the person, on Teams.
//
// IT IS ABSENT WHERE IT CANNOT WORK, as the column's door says by not being
// drawn ([app.helpersOffered]): over --host to an engine without the teams
// doors, on a connection that holds one conversation at a time (a helper opens
// beside the manager), on the welcome screen before a conversation exists, and
// for a conversation that already manages a team.

import (
	"fmt"
	"math/rand/v2"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The words this door says. Each is quoted in the manual exactly as spelled.
const (
	// helpersDoorWord is the side column's door in a chat in no team, and
	// helpersDoorHint what the hint line says over it.
	helpersDoorWord = "+ Make team"
	helpersDoorHint = "Make this conversation the manager of a new team, and ask it to propose helpers"
	// helpersPlaceWord is /helpers typed where no conversation is in front.
	helpersPlaceWord = "/helpers makes a team of the conversation you are in · open one first"
	// helpersFreshWord is /helpers on the welcome screen, before there is a
	// conversation to make a team of.
	helpersFreshWord = "/helpers makes a team of a conversation you have started · say what you are working on first"
	// helpersManagesWord is /helpers in a conversation that already manages a
	// team; the strip is narrowed to that team.
	helpersManagesWord = "this conversation already manages %s · ask it for helpers"
	// helpersPendingWord is a second /helpers while the first is being saved.
	helpersPendingWord = "the team is still being saved"
	// helpersMadeWord is the receipt, once the store has the team.
	helpersMadeWord = "made the team %s · this conversation is its manager"
	// helpersAwayWord is the receipt when the person left the conversation
	// before the store answered: the team is made and the ask is not sent.
	helpersAwayWord = "made the team %s · its manager is the conversation you left; ask it for helpers there"
)

// helpersShared is the refusal on a connection that holds one conversation at
// a time: a helper is a conversation that opens beside its manager.
var helpersShared = "helpers open beside this conversation, and " + oneConversationWord

// helpersAskOf is the turn the conversation is sent, for team name, with the
// person's own words after /helpers, if any, at the end. It is written as the
// person speaking, because it is their message: the transcript shows what they
// typed and the model reads this behind it, the way /manual works
// (manualcmd.go). It names both verbs, so the yes and the no each have one.
func helpersAskOf(name, rest string) string {
	ask := fmt.Sprintf("I have just turned this conversation into a team called %q, and you are its manager: you keep this conversation and everything in it, and nobody else is in the team yet. "+
		"Propose two or three helpers for the work we are doing here. For each, give a handle (2 to 12 lowercase letters or digits), the part of the work it would take, and the brief you would give it. "+
		"Then ask me to accept them, change the list, or cancel. Start nobody until I agree. "+
		"When I agree, start each one with team_start. If I cancel, take the team back with team_dissolve.", name)
	if rest = strings.TrimSpace(rest); rest != "" {
		ask += " What I want help with: " + rest
	}
	return ask
}

// helpersPending is one /helpers between its edit and the store's answer: the
// edit's notice tie, the conversation it made a manager, the team's name, and
// the turn to send once the store has taken it. due says the answer is in and
// [app.helpersAsked] has not acted on it yet.
type helpersPending struct {
	said  teamWriteSaid
	key   string
	name  string
	ask   string
	shown string
	due   bool
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

// helpersOffered reports whether the side column draws its door: the same
// refusals [app.runHelpersCommand] would give, asked of memory only, because a
// frame asks it.
func (a *app) helpersOffered() bool {
	if a.teamsOff() || a.shared || !a.wall.loaded || a.helpers.said.pending {
		return false
	}
	if a.agent == nil || a.startingChat() || a.pageShowing() {
		return false
	}
	key := a.frontTabKey()
	if key == "" {
		return false
	}
	_, manages := a.teamManaged(key)
	return !manages
}

// runHelpersCommand is /helpers, and the column's door: the team made, and the
// ask queued behind the store's answer.
func (a *app) runHelpersCommand(rest string) tea.Cmd {
	if a.pageShowing() {
		a.note(helpersPlaceWord)
		return nil
	}
	if a.teamsOff() {
		a.note(teamHostedWord)
		return nil
	}
	if a.shared {
		a.note(helpersShared)
		return nil
	}
	tab, ok := a.helpersTab()
	if !ok {
		a.note(helpersFreshWord)
		return nil
	}
	if a.helpers.said.pending {
		a.note(helpersPendingWord)
		return nil
	}
	a.teamsEnsure()
	if t, manages := a.teamManaged(tab.key); manages {
		a.note(fmt.Sprintf(helpersManagesWord, t.Name))
		return a.teamActivate(t.ID)
	}
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
	shown := "/helpers"
	if rest = strings.TrimSpace(rest); rest != "" {
		shown += " " + rest
	}
	a.helpers = helpersPending{said: a.teamWriteWatch(nil), key: tab.key, name: name, ask: helpersAskOf(name, rest), shown: shown}
	return a.teamActivate(id)
}

// helpersAsked is a /helpers whose write has come back: the receipt and the
// turn when the store took the team, and nothing when it refused it, because
// [app.teamsTake] has already said why.
func (a *app) helpersAsked() tea.Cmd {
	p := a.helpers
	if !p.due {
		return nil
	}
	a.helpers = helpersPending{}
	if p.said.why != "" {
		return nil
	}
	// THE PERSON MAY HAVE LEFT. The turn goes to the conversation in front,
	// and a manager's first words sent to another conversation would be the
	// wrong conversation's; so the team stands and the ask is left to them.
	if a.frontTabKey() != p.key {
		a.note(fmt.Sprintf(helpersAwayWord, p.name))
		return nil
	}
	a.note(fmt.Sprintf(helpersMadeWord, p.name))
	return a.submitShown(p.ask, p.shown, nil)
}
