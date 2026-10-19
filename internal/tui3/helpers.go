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
//  2. Once the store has TAKEN that edit, and only then, the person's words
//     after `/helpers` are sent to the conversation as their message. Waiting
//     for the store is what makes the turn's first step find the conversation
//     already a manager, with its role note and its verbs ([teamsWrote],
//     [app.helpersAsked]).
//
// NOTHING IS SENT THAT THE PERSON DID NOT SEE. The words the model reads are
// the words in the transcript after `/helpers`, exactly. A bare `/helpers` is
// [helpersPrompt], and the transcript shows it written out after the command,
// so the line on screen is the whole of what was asked. What the model knows
// beyond those words is what every manager knows: the role note internal/session
// lands at its first step, and its verbs' own descriptions.
//
// THE TEAMS DROPDOWN'S LAST ROW IS THE SAME DOOR, ONE STEP EARLIER. `+ New from
// this session` (teammenu.go) puts `/helpers ` and [helpersPrompt] in the box,
// in place of whatever was there, and sends nothing: the person reads the
// request, changes it into the team they want, and presses enter.
//
// BOTH ARE FOR A CONVERSATION SEEN WITH NO TEAM SELECTED. With a team's
// overlay on the strip the row is absent and the command refuses, because the
// team it makes is a top-level team and making it from inside another team
// would read as nesting; a team inside a team is `+ Add subteam` on Teams, or
// a manager's own sub-team start.
//
// THE PERSON ANSWERS IN THEIR OWN WORDS. There is no card. Yes is a reply the
// manager acts on with `team_start`, which runs under the approval posture every
// other start does, and each helper it starts opens as a tab in this team's
// overlay (teamtraffic.go's [app.trafficStarted]); a different list is a reply
// it proposes again from; and cancel is a reply it acts on with
// `team_dissolve`, which takes back a team that has never held anyone but its
// manager and is on the belt only while that is true (internal/session's
// team_dissolve.go). After the first helper joins, the team ends the way every
// other team does: disbanded by the person, on Teams.

import (
	"fmt"
	"math/rand/v2"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// helpersPrompt is what a bare /helpers asks, and what the dropdown's row puts
// in the box after `/helpers `. It is the person speaking, in words they would
// use, because it is their message and it is on their screen: it names no tool
// and nothing the person could not have typed. It asks for a proposal to read
// and says what happens next — the person answers yes, a change, or cancel —
// which is what keeps the manager from starting anyone before they agree.
//
// IT DESCRIBES THIS EXCHANGE AND STATES NO STANDING RULE. It used to end
// "Don't start anyone until I agree, and if I say cancel, undo the team.", and
// the memory that notices rules (internal/session's memory) kept that as one
// for the whole project: a person's standing order never to start helpers
// without asking, read by every manager after. Said as the next step of this
// one conversation, there is nothing in it to keep.
const helpersPrompt = "Suggest two or three helpers for what we're working on here. For each, say what it would take on and the brief you'd give it. I'll answer yes, ask for changes, or cancel."

// The words this door says. Each is quoted in the manual exactly as spelled.
const (
	// helpersRowWord is the Teams dropdown's last row, which puts /helpers and
	// [helpersPrompt] in the box.
	helpersRowWord = "+ New from this session"
	// helpersPlaceWord is /helpers typed where no conversation is in front.
	helpersPlaceWord = "/helpers makes a team of the conversation you are in · open one first"
	// helpersFreshWord is /helpers on the welcome screen, before there is a
	// conversation to make a team of.
	helpersFreshWord = "/helpers makes a team of a conversation you have started · say what you are working on first"
	// helpersManagesWord is /helpers in a conversation that already manages a
	// team; the strip is narrowed to that team.
	helpersManagesWord = "this conversation already manages %s · ask it for helpers"
	// helpersSelectedWord is /helpers with a team selected in the strip: a
	// team made here is a top-level team, and subteams have their own door.
	helpersSelectedWord = "/helpers works with no team selected · clear %s with × first, or use + Add subteam on Teams for a team inside it"
	// helpersPendingWord is a second /helpers while the first is being saved.
	helpersPendingWord = "the team is still being saved"
	// helpersMadeWord is the receipt, once the store has the team.
	helpersMadeWord = "made the team %s · this conversation is its manager"
	// helpersAwayWord is the receipt when the person left the conversation
	// before the store answered: the team is made and the message is not sent.
	helpersAwayWord = "made the team %s · its manager is the conversation you left; ask it for helpers there"
)

// helpersShared is the refusal on a connection that holds one conversation at
// a time: a helper is a conversation that opens beside its manager.
var helpersShared = "helpers open beside this conversation, and " + oneConversationWord

// helpersPending is one /helpers between its edit and the store's answer: the
// edit's notice tie, the conversation it made a manager, the team's name, and
// the message to send once the store has taken it. due says the answer is in
// and [app.helpersAsked] has not acted on it yet.
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

// helpersRefusal is why /helpers cannot make a team of the conversation in
// front, "" when it can, and the team it already manages when that is why. It
// is asked by the command and by the dropdown's row, so the row never fills the
// box with a request the command would refuse.
func (a *app) helpersRefusal() (string, team) {
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
	if t, manages := a.teamManaged(tab.key); manages {
		return fmt.Sprintf(helpersManagesWord, t.Name), t
	}
	// ONLY WITH NO TEAM SELECTED, the dropdown's row and the command alike
	// ([app.teamMenuHelpers] says why): a team made here is a top-level team,
	// and making it from inside another team's overlay would read as nesting.
	if t, selected := a.teamActive(); selected {
		return fmt.Sprintf(helpersSelectedWord, t.Name), team{}
	}
	return "", team{}
}

// helpersTipArmed is the 💡 tip's rule (notice.go): a conversation that has
// been spoken to, in front, with no team selected, not already a manager, and
// on a window that can keep teams and open helpers beside it. It reads memory
// only, because the table asks it at every event.
func helpersTipArmed(a *app) bool {
	if a.turn < 1 || a.pageShowing() || a.teamsOff() || a.shared || !a.wall.loaded {
		return false
	}
	if _, selected := a.teamActive(); selected {
		return false
	}
	_, manages := a.teamManaged(a.frontTabKey())
	return !manages
}

// runHelpersCommand is /helpers: the team made, and the person's words queued
// behind the store's answer. With no words after the command they are
// [helpersPrompt], and the transcript shows them.
func (a *app) runHelpersCommand(rest string) tea.Cmd {
	a.noticeEvent(eventHelpersUsed)
	if why, managed := a.helpersRefusal(); why != "" {
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
	ask := strings.TrimSpace(rest)
	if ask == "" {
		ask = helpersPrompt
	}
	a.helpers = helpersPending{said: a.teamWriteWatch(nil), key: tab.key, name: name, ask: ask, shown: "/helpers " + ask}
	return a.teamActivate(id)
}

// helpersAsked is a /helpers whose write has come back: the receipt and the
// message when the store took the team, and nothing when it refused it,
// because [app.teamsTake] has already said why.
func (a *app) helpersAsked() tea.Cmd {
	p := a.helpers
	if !p.due {
		return nil
	}
	a.helpers = helpersPending{}
	if p.said.why != "" {
		return nil
	}
	// THE PERSON MAY HAVE LEFT. The message goes to the conversation in front,
	// and a manager's first words sent to another conversation would be the
	// wrong conversation's; so the team stands and the ask is left to them.
	if a.frontTabKey() != p.key {
		a.note(fmt.Sprintf(helpersAwayWord, p.name))
		return nil
	}
	a.note(fmt.Sprintf(helpersMadeWord, p.name))
	return a.submitShown(p.ask, p.shown, nil)
}

// helpersFill is the Teams dropdown's `+ New from this session`: `/helpers `
// and [helpersPrompt] put in the box, in place of what was there, for the
// person to read, change and send. Nothing is made until they press enter.
// What was in the box goes onto the drafts ring first, as a draft restored
// over it does (draftpage.go), so replacing it loses nothing. Where /helpers
// would refuse, the row says so instead and leaves the box alone.
func (a *app) helpersFill() tea.Cmd {
	a.noticeEvent(eventHelpersUsed)
	a.closeTeamMenu()
	if a.wall.on {
		a.closeWall()
	}
	if why, managed := a.helpersRefusal(); why != "" {
		a.note(why)
		if managed.ID != "" {
			return a.teamActivate(managed.ID)
		}
		return nil
	}
	line := "/helpers " + helpersPrompt
	if live := a.input.String(); strings.TrimSpace(live) != "" && live != line {
		a.drafts.push([]rune(live))
	}
	a.input.setText(line)
	a.touch()
	return a.syncLists()
}
