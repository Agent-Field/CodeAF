package tui3

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── BRINGING THE MANAGER INTO THE PANE ──────────────────────────────────────
//
// The pane hosts the selected team's manager only while that conversation is
// the one in front ([app.teamsHosting]); everything here is how it gets there,
// and what the pane says while it does not.
//
// THE PANE USED TO SAY `opening ◆ prism's manager…` WHENEVER THE MANAGER WAS NOT
// IN FRONT, whether or not anything was opening it. The one attempt was made by
// [app.teamsBringManager] on the page's own doors (opening the page, choosing a
// team, a close and a reopen), and every other road to the same state made
// none: the teams arriving from the disk after the page opened, a manager set
// on the file by a session, a message handed to the hosted conversation that
// moved the front (a press on a Traffic row), a refusal that went to a line the
// pane does not draw. Each of them left the word on the pane with nothing
// behind it, and the owner watched it for as long as he looked (2026-09-24).
// And the attempt that did run went through the switcher's own door, which
// ends by stepping off whatever place is standing ([app.hopLand]): a manager
// this window was not holding was opened and the person was taken off the page
// to it.
//
// SO AN ATTEMPT IS NOW A THING THE PAGE HOLDS ([teamsOpen]), and the word on the
// pane is read off it rather than off the front. [app.teamsSync], which runs
// after every message, starts one whenever the selected team's manager is not
// in front and no attempt has been made for it, so no road can leave the pane
// waiting on nothing. The open is asked off the loop and lands the manager
// behind, then brings it forward only if the page still wants it, so the
// person never leaves the page for it. And the pane never waits silently: a
// refusal is said with its reason, an attempt the engine has not answered
// within [teamsOpenBound] is said too, and both offer `Retry` and
// `Open in chats` (the chat surface's own door, [app.trafficGo]). A manager
// whose transcript is gone offers `+ Manager`, which makes a new one.
//
// ONE OPEN PER MANAGER IS OUT AT A TIME. A team chosen twice (a double press, or
// away and back while the first open was on the wire) used to ask the door
// twice for the same transcript. The first answer came back as a stale attempt
// and was put behind, the second met the first one's lock, and the pane said
// the conversation was open in another window, about a conversation this window
// was holding, until the person pressed Retry. Now a choice while an open is
// out takes that open up as its own ([teamsOpenOut]), an answer for the manager
// the page is asking about is the page's answer whichever attempt it was, and a
// refusal about a conversation this window already holds is no refusal. `Retry`
// alone asks again while an open is out, because it is pressed when that open
// has not answered.
//
// AND THE DOOR IS NEVER ASKED FROM UPDATE. Over a connection that holds one
// conversation at a time the open is the engine's swap; it was made on the
// loop, and the window froze for the round trip. It goes through the ordered
// door line now ([app.offLoop]), because a swap is a gesture whose order
// matters, and the swap is taken when it answers.

// teamsOpenBound is how long the pane says `opening` before it says the engine
// has not answered. Long enough for an ordinary open over the socket, short
// enough that a person is not left looking at a word with nothing behind it.
const teamsOpenBound = 4 * time.Second

// teamsManagerGoneWord is the reason for a manager whose transcript is not on
// the disk any more.
const teamsManagerGoneWord = "its conversation is gone"

// teamsOpenInChatsWord is the button that opens the manager where every
// conversation is opened, off the page.
const teamsOpenInChatsWord = "Open in chats"

// teamsOpen is the page's one attempt to bring a manager in front: which
// manager, when it was asked, and how it ended.
type teamsOpen struct {
	// key is the manager this attempt is for; gen tells a stale answer from
	// the current one after a Retry.
	key string
	gen int
	// at is when the open was asked, and out says it has not answered yet.
	at  time.Time
	out bool
	// why is the refusal, said on the pane; missing says the transcript is
	// gone, which offers `+ Manager` rather than a door that cannot open it.
	why     string
	missing bool
	// done says the manager came in front on this attempt.
	done bool
}

// teamsOpenOut is one open a door has not answered yet: its attempt and when
// it was asked.
type teamsOpenOut struct {
	gen int
	at  time.Time
}

// teamsBringManager brings the selected team's manager in front when it is not
// there yet. It is the one move of the front this page makes, and only ever
// for the team the person chose: the conversation that was in front stays
// open behind, one `tab` away. A new attempt every time it is called, so the
// page's own doors (open, select, reopen) always try again.
func (a *app) teamsBringManager() tea.Cmd {
	t, ok := a.teamsSelected()
	if !ok || t.Closed() || t.Manager == "" || a.teamsOff() {
		a.tp.open = teamsOpen{gen: a.tp.open.gen}
		return nil
	}
	if t.Manager == a.frontTabKey() {
		a.tp.open = teamsOpen{key: t.Manager, gen: a.tp.open.gen, done: true}
		return nil
	}
	return a.teamsOpenManager(t, false)
}

// teamsKeepManager is [app.teamsSync]'s half: an attempt for the selected
// team's manager when none has been made for it, whatever road left it out of
// front. It makes one attempt per manager: a refusal stays said until the
// person presses Retry or chooses again, rather than being asked every beat.
func (a *app) teamsKeepManager(t team) tea.Cmd {
	if a.tp.open.key == t.Manager {
		return nil
	}
	return a.teamsOpenManager(t, false)
}

// teamsOpenManager asks for team t's manager: brought forward at once when this
// window holds it behind, and otherwise opened off the loop, landed behind, and
// brought forward when the answer comes back if the page still wants it. An
// open for the same manager that is still out is taken up rather than asked
// again, unless again says the person pressed Retry.
func (a *app) teamsOpenManager(t team, again bool) tea.Cmd {
	key := t.Manager
	a.tp.top = teamsTopCache{}
	a.touch()
	if out, ok := a.tp.opens[key]; ok && !again {
		a.tp.open = teamsOpen{key: key, gen: out.gen, at: out.at, out: true}
		return nil
	}
	a.tp.openSeq++
	gen := a.tp.openSeq
	a.tp.open = teamsOpen{key: key, gen: gen, at: a.now(), out: true}
	if held := a.behind[key]; held != nil {
		cmd, _ := a.bringForward(held.conv.SessionFile)
		a.tp.open.out, a.tp.open.done = false, true
		return cmd
	}
	m, ok := t.Member(key)
	if !ok || strings.TrimSpace(m.File) == "" {
		a.teamsOpenFailed(gen, teamsManagerGoneWord, true)
		return nil
	}
	file, where := m.File, m.Where
	switch {
	case a.shared:
		// ONE CONVERSATION PER CONNECTION: the engine swaps in place and there
		// is nothing to hold behind ([Options.SharedAgent]), so the swap is the
		// open. The identity is asked here, from memory ([app.openBeside]'s
		// rule); the swap itself is a door, asked on the line.
		if cmd, ours := a.bringForward(file); ours {
			a.tp.open.out, a.tp.open.done = false, true
			return cmd
		}
		if !a.canOpen() {
			a.teamsOpenFailed(gen, resumeUnavailableWord, false)
			return nil
		}
	case !a.canOpen():
		a.teamsOpenFailed(gen, resumeUnavailableWord, false)
		return nil
	}
	if a.tp.opens == nil {
		a.tp.opens = map[string]teamsOpenOut{}
	}
	a.tp.opens[key] = teamsOpenOut{gen: gen, at: a.tp.open.at}
	open, resume, local, shared := a.open, a.resume, !a.hosted(), a.shared
	ask := func() func(bool) tea.Cmd {
		var conv Conversation
		var err error
		missing := false
		switch {
		case local && !shared && transcriptGone(file):
			// A local path is this machine's, so its absence is a fact; over
			// --host the path is the far machine's and only its engine can say.
			missing, err = true, errors.New(teamsManagerGoneWord)
		case local && !shared && !homeFolderThere(where):
			err = errors.New(WorkspaceGoneWord + " · " + where)
		case open != nil:
			conv, err = open(where, file)
		default:
			var agent Agent
			agent, err = resume(file)
			conv = Conversation{Agent: agent, Workspace: where, SessionFile: file}
		}
		if shared {
			return func(bool) tea.Cmd { return a.teamsSwapped(gen, key, conv, err) }
		}
		return func(bool) tea.Cmd { return a.teamsOpened(gen, key, file, conv, err, missing) }
	}
	if shared {
		return a.offLoop(ask)
	}
	return a.besideLine(ask)
}

// transcriptGone reports whether a local transcript is not on the disk. Only a
// not-exist answer counts: a permission error is a door's to say.
func transcriptGone(file string) bool {
	_, err := os.Stat(file)
	return errors.Is(err, fs.ErrNotExist)
}

// teamsOpened folds one open's answer in, on the loop. A conversation that
// opened is held behind, and brought forward only when the page still wants
// that manager: an answer that arrives after the person chose another team, or
// left the page, stays a tab of its own rather than moving them. An answer
// about the manager the page is asking for is the page's answer whichever
// attempt carried it, and a refusal is said only when it is the page's current
// attempt and the conversation is not in this window's hands after all.
func (a *app) teamsOpened(gen int, key, file string, conv Conversation, err error, missing bool) tea.Cmd {
	if out, ok := a.tp.opens[key]; ok && out.gen == gen {
		delete(a.tp.opens, key)
	}
	mine := a.tp.open.key == key
	current := mine && a.tp.open.gen == gen
	if err != nil || conv.Agent == nil {
		switch {
		case mine && a.holding(file):
			// Another answer for this manager landed it while this one was
			// out, and this one met its lock: the manager is here.
			return a.teamsLanded(key, file)
		case current:
			why := "the conversation did not open"
			switch {
			case errors.Is(err, session.ErrSessionLocked):
				why = sessionBusyWord
			case err != nil:
				why = err.Error()
			}
			a.teamsOpenFailed(gen, why, missing)
		}
		return nil
	}
	var cmd tea.Cmd
	if a.holding(conv.SessionFile) {
		// Another road opened it while this ask was out (a Retry, or the
		// person's own door); the window holds one handle per conversation.
		leaveOffFrame(conv.Agent)
	} else {
		cmd = a.stow(conv, nil)
		a.chatTabBar = tabBar{}
	}
	if mine {
		cmd = tea.Batch(cmd, a.teamsLanded(key, conv.SessionFile))
	}
	a.tp.top = teamsTopCache{}
	a.touch()
	return cmd
}

// teamsLanded is the page's attempt for key answered with the conversation
// held: the pane stops waiting, and the manager comes in front if the page
// still wants it.
func (a *app) teamsLanded(key, file string) tea.Cmd {
	a.tp.open.out, a.tp.open.why, a.tp.open.missing = false, "", false
	a.tp.top = teamsTopCache{}
	a.touch()
	if !a.teamsWantsManager(key) {
		return nil
	}
	cmd, _ := a.bringForward(file)
	a.tp.open.done = true
	return cmd
}

// teamsSwapped folds a swap over a connection that holds one conversation at a
// time. The engine has already moved when it answers, so a conversation that
// opened is taken in front whatever the page wants now ([app.takeBeside]): the
// window must show the conversation its one handle names. A refusal is said
// when it is the page's current attempt.
func (a *app) teamsSwapped(gen int, key string, conv Conversation, err error) tea.Cmd {
	if out, ok := a.tp.opens[key]; ok && out.gen == gen {
		delete(a.tp.opens, key)
	}
	current := a.tp.open.key == key && a.tp.open.gen == gen
	if err != nil || conv.Agent == nil {
		if current {
			why := "the conversation did not open"
			switch {
			case errors.Is(err, session.ErrSessionLocked):
				why = sessionBusyWord
			case err != nil:
				why = err.Error()
			}
			a.teamsOpenFailed(gen, why, false)
		}
		return nil
	}
	cmd := a.takeBeside(conv)
	if a.tp.open.key == key {
		a.tp.open.out, a.tp.open.why, a.tp.open.done = false, "", true
	}
	a.tp.top = teamsTopCache{}
	a.touch()
	return cmd
}

// teamsOpenFailed says attempt gen's refusal on the pane.
func (a *app) teamsOpenFailed(gen int, why string, missing bool) {
	if a.tp.open.gen != gen {
		return
	}
	a.tp.open.out, a.tp.open.done = false, false
	a.tp.open.why, a.tp.open.missing = why, missing
	a.tp.top = teamsTopCache{}
	a.touch()
}

// teamsWantsManager reports whether the page still wants key in front: it is
// standing, and key is the selected open team's manager.
func (a *app) teamsWantsManager(key string) bool {
	if !a.at(pageTeams) {
		return false
	}
	t, ok := a.teamsSelected()
	return ok && !t.Closed() && t.Manager == key
}

// teamsRetryManager is `Retry`: a new attempt for the selected team's manager.
func (a *app) teamsRetryManager() tea.Cmd {
	t, ok := a.teamsSelected()
	if !ok || t.Closed() || t.Manager == "" {
		return nil
	}
	return a.teamsOpenManager(t, true)
}

// teamsOpenInChats is `Open in chats`: the manager opened through the chat
// surface's own door ([app.trafficGo]), off the page, where a refusal is said
// on the conversation's own line.
func (a *app) teamsOpenInChats() tea.Cmd {
	t, ok := a.teamsSelected()
	if !ok || t.Manager == "" {
		return nil
	}
	a.leavePlace()
	return a.trafficGo(t.Manager)
}

// teamsManagerMissing reports whether team t's manager is known to be gone, so
// `+ Manager` may replace it.
func (a *app) teamsManagerMissing(t team) bool {
	return t.Manager != "" && a.tp.open.key == t.Manager && a.tp.open.missing
}

// teamsOpeningRows is the pane under the header while the selected team's
// manager is not in front: `opening` for the beat it takes, and once it has
// taken longer than [teamsOpenBound] or been refused, the reason and a way
// forward.
func (a *app) teamsOpeningRows(d *teamsDraw, t team, width, y int) []string {
	pal := a.pal
	name := t.Name
	if t.Root {
		name = "all teams"
	}
	manager := a.teamManagerMark() + " " + name + "'s manager"
	o := a.tp.open
	mine := o.key == t.Manager
	why := ""
	switch {
	case t.Manager == a.frontTabKey():
		// In front, with a surface over it that keeps the pane from hosting it
		// for now; nothing is being opened.
		return nil
	case mine && o.why != "":
		why = o.why
	case mine && o.out && a.now().Sub(o.at) >= teamsOpenBound:
		why = "no answer in " + itoa(int(teamsOpenBound/time.Second)) + "s"
	case mine && o.done:
		// It came in front on this attempt and something has since moved the
		// front off it.
		out := []string{"", " " + pal.dim(fit(manager+" is not in front", width-2))}
		return append(out, a.teamsOpenButtons(d, t, y+len(out), "Bring it here")...)
	default:
		word := "opening " + manager + a.linearMark("…", "...")
		return []string{"", " " + pal.dim(fit(word, width-2))}
	}
	out := []string{""}
	for _, l := range wrap("couldn't open "+manager+": "+why, max(width-2, 8)) {
		out = append(out, " "+pal.muted(l))
	}
	out = append(out, "")
	return append(out, a.teamsOpenButtons(d, t, y+len(out), "Retry")...)
}

// teamsOpenButtons is the pane's way forward: `retry` (the word says what it
// does here) and `Open in chats`, or `+ Manager` in place of the second when
// the manager's transcript is gone.
func (a *app) teamsOpenButtons(d *teamsDraw, t team, y int, retry string) []string {
	pal := a.pal
	row := " "
	x := 1
	if a.teamsManagerMissing(t) {
		s, w := d.button(teamManagerSlotWord, teamsTarget{act: teamsActManager, id: t.ID, x0: x, y: y,
			hint: "Start a new manager for " + t.Name + hintSegment + "m"}, pal.ink)
		row += s + "  "
		x += w + 2
	}
	s, w := d.button(retry, teamsTarget{act: teamsActRetryManager, id: t.ID, x0: x, y: y,
		hint: "Open " + t.Name + "'s manager here again"}, pal.ink)
	row += s
	x += w
	if !a.teamsManagerMissing(t) {
		row += "  "
		x += 2
		s, _ := d.button(teamsOpenInChatsWord, teamsTarget{act: teamsActOpenInChats, id: t.ID, x0: x, y: y,
			hint: "Open " + t.Name + "'s manager as a conversation, off this page"}, pal.ink)
		row += s
	}
	return []string{row}
}
