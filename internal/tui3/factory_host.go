package tui3

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE CENTER OF THE ITEM PAGE IS THE REAL CHAT ────────────────────────────
//
// The owner's layout (2026-10-09): selecting a step shows that step's chat
// in the center, and selecting the manager shows the manager's, THE SAME
// VIEW AS A NORMAL CODEAF CHAT, its tasks panel on the right included. Not a
// render of the transcript: the conversation itself, live, scrollable, with
// its own box to type into.
//
// SO THE PAGE DOES NOT DRAW A CHAT; IT LENDS ONE A RECTANGLE, the way the
// teams page once hosted its manager. While the center hosts a chat
// ([app.factoryHosting]) the conversation in front IS that chat, and its own
// geometry is the terminal less the left column: [app.size] answers the
// narrower width, and [app.topHeight] charges the page's head and top bar,
// so every layout, scroll and hit test the chat makes resolves against the
// cells it is really drawn in. The frame is the chat's own
// ([app.chatFrameLines]) with the places' head and the top bar across the
// whole width over it, and the left column joined on the left of every row
// under them.
//
// THE CHAT IS BROUGHT IN FRONT BY SELECTING ITS ROW ([app.factoryHostSync],
// after every message): the manager's chat is the item's own conversation
// ([factory.Item.Talk]), a step's is its phase's ([factory.Phase.Chat]).
// It is opened through the window's own door ([Options.Open]), the folder
// read and the conversation built OFF THE LOOP and off the door line, and only
// the attach done on it ([app.factoryHostBring]); the conversation that was in
// front goes on running behind, one tab away. A RUNNING STEP'S CHAT IS THE
// STEP'S OWN AGENT, LIVE: the door hands the conversation this process already
// holds on that journal (cmd/codeaf's factory_embed.go) rather than a second
// one its lock would refuse, so the center streams the step as it works. One
// attempt per selection: a refusal, or an open that does not answer, is said
// in the center, and choosing the row again asks again.
//
// THE KEYS WALK THE PAGE UNTIL THE BOX HAS THEM. `→`, `tab`, `enter` on the
// row, or a press on the chat puts them in the box ([factoryPage.box]);
// then every key is the chat's, the arrows editing the words, until `esc` or
// `tab` gives them back to the page. A press on the left column or the bar
// gives them back too. While the keys walk the page the chat's caret is not
// drawn, because a caret is a promise that a letter lands there.
//
// WALKING THE STEPS LEAVES NO TABS BEHIND. A chat the page opened only
// because the cursor passed over its row ([factoryHost.opened]) is let go of
// once the cursor leaves it, or the page closes: detached, the way a window
// leaving lets go of a conversation, so no work it holds is ended. A chat the
// person put the keys into ([factoryHost.kept]) is theirs and stays a tab, as
// does every chat this window already held before the page brought it.
//
// A TERMINAL TOO NARROW FOR THE COLUMN HOSTS NOTHING: under
// [factoryStageFloor] the center is the page's own, and `enter` on a step
// with a chat walks into it the way a room is walked into.

// factoryHost is the chat the center hosts and how it got there.
type factoryHost struct {
	// key is the hosted chat's identity while it is the conversation in
	// front, "" while the center hosts none.
	key string
	// want is the transcript the selected row wants in the center, and
	// wantKey its identity, read once per want (it is a disk question).
	want    string
	wantKey string
	// asked is the transcript an open was last asked for, so one selection
	// asks once; why is what that open said when it refused.
	asked string
	why   string
	// forwarding says a message is being handed to the hosted chat, or the
	// chat's frame is being drawn, with the page set aside for it.
	forwarding bool
	// focus says the box takes the keys as soon as the chat being brought
	// lands: `enter` asked for the manager's chat and is waiting on it.
	focus bool
	// talks is the manager's chat the Talk door answered, by item, for a
	// floor read that does not carry it yet.
	talks map[int]string
	// opened is every chat the page opened itself, by key, its transcript
	// the value; kept is those of them the person put the keys into, which
	// stay. The rest are let go of when the cursor leaves them
	// ([app.factoryHostLetGo]).
	opened map[string]string
	kept   map[string]bool
	// talkAsked is every item the Talk door was asked for its conversation by
	// the cursor resting on the manager row, once each while the page stands.
	talkAsked map[int]bool
	// talkOut is the item whose Talk door ask is out now, 0 for none, so a
	// press landing meanwhile waits on it rather than asking twice.
	talkOut int
	// after is what waits on the manager's chat being in front: a shaping
	// turn, or a run whose shaping turn should stream there
	// ([app.factoryAfterManager]).
	after *factoryAfter
}

// factoryAfter is one thing the page does once the manager's chat of item id
// is in front, or once it is clear it will not be (the open refused, or the
// Talk door made no conversation), so it is never left waiting.
type factoryAfter struct {
	id     int
	do     func() tea.Cmd
	failed bool
}

// factoryCanHost says whether the page can host a chat at all on this
// window: a door to open conversations, and the width for the column.
func (a *app) factoryCanHost() bool {
	return a.canOpen() && a.width >= factoryStageFloor
}

// factoryChatOf is the transcript the row wants in the center: the item's
// own conversation on the manager row, a step's chat on a step row that ran
// as one, and "" on every other row.
func (a *app) factoryChatOf(it factory.Item, r factoryPageRow) string {
	switch r.kind {
	case factoryPageManager:
		if talk := strings.TrimSpace(it.Talk); talk != "" {
			return talk
		}
		return a.fp.host.talks[it.ID]
	case factoryPageStage:
		return factoryRoomOf(r)
	}
	return ""
}

// factoryTalkHere is `enter` on the issue or the manager row, and `T` on the
// item page: the manager's chat, IN THE CENTER. The cursor goes to the
// manager row and the box takes the keys once the chat is in front; an item
// with no conversation yet asks the Talk door for one first
// ([app.factoryTalkFetch]).
func (a *app) factoryTalkHere(it factory.Item) tea.Cmd {
	a.factoryManagerRow(it)
	h := &a.fp.host
	h.focus = true
	if a.factoryChatOf(it, factoryPageRow{kind: factoryPageManager}) != "" || h.talkOut == it.ID {
		return nil
	}
	return a.factoryTalkFetch(it, true)
}

// factoryManagerRow puts the left column's cursor on the manager row.
func (a *app) factoryManagerRow(it factory.Item) {
	for i, row := range a.factoryItemRows(it) {
		if row.kind == factoryPageManager {
			if i != a.fp.stage {
				a.factoryStageSelect(i)
			}
			return
		}
	}
}

// factoryTalkFetch asks the Talk door for item it's conversation OFF THE
// LOOP; for a gesture the floor is read in the same ask so the row learns it
// has one.
// THE DOOR MAKES NO MODEL CALL: the conversation is written with the issue
// already in it ([factory.Seam.Talk]), idle until somebody speaks, so the
// manager row opens as a normal chat at once. focus says the box takes the
// keys when it lands (`enter`, `T`); the cursor merely resting on the row
// does not.
func (a *app) factoryTalkFetch(it factory.Item, focus bool) tea.Cmd {
	seam, id := a.factory, it.ID
	if seam.Talk == nil {
		return nil
	}
	h := &a.fp.host
	if h.talkAsked == nil {
		h.talkAsked = map[int]bool{}
	}
	h.talkAsked[id] = true
	h.talkOut = id
	a.fp.act.doing = "opening " + it.Ref() + "'s conversation…"
	// THE CURSOR RESTING ON THE ROW IS NOT A GESTURE, so its ask stands
	// beside the ordered line rather than in front of the next key; `enter`
	// and `T` are, and keep their place in it.
	line := a.besideLine
	if focus {
		line = a.offLoop
	}
	return line(func() func(bool) tea.Cmd {
		chat, err := seam.Talk(context.Background(), id)
		// THE FLOOR IS READ AGAIN ONLY FOR A GESTURE: the chat is kept by the
		// page itself ([factoryHost.talks]) until the floor's own next read
		// carries it, so the cursor resting on a row costs no read.
		var snap factory.Snapshot
		var lerr error
		read := focus && seam.Load != nil
		if read {
			snap, lerr = seam.Load()
		}
		return func(bool) tea.Cmd {
			a.fp.act.doing = ""
			if a.fp.host.talkOut == id {
				a.fp.host.talkOut = 0
			}
			focus = focus || a.fp.host.focus
			switch {
			case lerr != nil:
				a.fp.err = lerr
			case read:
				a.factoryFold(snap)
			}
			chat = strings.TrimSpace(chat)
			if err != nil || chat == "" {
				a.fp.host.focus = false
				if after := a.fp.host.after; after != nil && after.id == id {
					after.failed = true
				}
				if err != nil {
					a.pageMsg = strings.TrimSpace(err.Error())
				} else if focus {
					// NO CHAT CAME BACK TO HOST: the manager's own box takes
					// the keys instead, and the Say door makes the chat on the
					// first words.
					if it, ok := a.factoryItemByID(id); ok {
						a.factoryTLOpenBox(it)
					}
				}
				a.touch()
				return nil
			}
			if a.fp.host.talks == nil {
				a.fp.host.talks = map[int]string{}
			}
			a.fp.host.talks[id] = chat
			a.touch()
			return nil
		}
	})
}

// factoryAfterManager does do once the manager's chat of item it is in front
// of the person: the cursor goes to the manager row now, the chat is made and
// brought by the ordinary sync ([app.factoryHostSync]), and do is called on
// the first sync that finds it in front ([app.factoryAfterFire]). A page that
// cannot host a chat (no door to open one, no Talk door, too narrow) does it
// at once, and so does one whose chat refused to open: the turn then runs
// headless and lands in the conversation all the same.
func (a *app) factoryAfterManager(it factory.Item, do func() tea.Cmd) tea.Cmd {
	if !a.fp.open || !a.factory.Has("talk") || !a.factoryCanHost() {
		return do()
	}
	a.factoryManagerRow(it)
	a.fp.host.after = &factoryAfter{id: it.ID, do: do}
	return nil
}

// factoryAfterFire calls what waits on the manager's chat when it is time:
// the chat is in front, its open refused, its conversation could not be made,
// or the person walked the cursor off the manager row meanwhile (the turn is
// theirs all the same). It is asked at the end of every sync.
func (a *app) factoryAfterFire() tea.Cmd {
	h := &a.fp.host
	after := h.after
	if after == nil {
		return nil
	}
	it, ok := a.factoryCursorItem()
	if !ok || it.ID != after.id || !a.fp.open {
		h.after = nil
		return nil
	}
	ready := after.failed
	if r, ok := a.factoryPageRowAt(it); !ok || r.kind != factoryPageManager {
		ready = true
	}
	switch chat := a.factoryChatOf(it, factoryPageRow{kind: factoryPageManager}); {
	case chat == "" && h.talkAsked[it.ID] && h.talkOut != it.ID:
		// THE TALK DOOR WAS ASKED AND MADE NOTHING: there is no chat to wait on.
		ready = true
	case chat != "" && h.want == chat && (h.why != "" || (h.wantKey != "" && h.key == h.wantKey)):
		ready = true
	}
	if !ready {
		return nil
	}
	h.after = nil
	return after.do()
}

// factoryHosting reports whether the center hosts its chat now: the item
// page is standing (or a message is being handed to the chat), the selected
// row's chat is the conversation in front, and nothing that takes the whole
// frame is over it. It is asked by [app.size] on every layout, so it reads a
// handful of fields and allocates nothing.
func (a *app) factoryHosting() bool {
	h := &a.fp.host
	if h.key == "" || !a.fp.open || !(a.at(pageFactory) || h.forwarding) {
		return false
	}
	if a.wall.on || a.setup.open || a.pasteEdit.open || a.fp.keys || a.fp.pick != nil || a.fp.recipe != nil {
		return false
	}
	return a.width >= factoryStageFloor && h.key == a.frontTabKey()
}

// factoryHostLeft is the columns the left column and its rule take from the
// chat while the center hosts it, and 0 when it does not.
func (a *app) factoryHostLeft() int {
	if !a.factoryHosting() {
		return 0
	}
	return factoryItemColW + factoryRuleW
}

// factoryHostTopHeight is the top bar's rows over the hosted chat, charged
// in [app.topHeight]. Zero when the center hosts nothing.
func (a *app) factoryHostTopHeight() int {
	if !a.factoryHosting() {
		return 0
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return 0
	}
	return a.factoryBarCount(it)
}

// factoryHostSync settles which chat the center hosts, after every message:
// the selected row's chat when it is in front, and an open asked for it when
// it is not and none was asked for this selection. The manager row of an item
// with no conversation yet asks the Talk door for one, once. Then what waited
// on the manager's chat is done when it is time ([app.factoryAfterFire]). It
// reads memory only on every other place.
func (a *app) factoryHostSync() tea.Cmd {
	settled := a.factoryHostSettle()
	talk := a.factoryTalkAuto()
	if a.fp.host.after == nil && talk == nil {
		return settled
	}
	return tea.Batch(settled, talk, a.factoryAfterFire())
}

// factoryTalkAuto is THE MANAGER ROW OPENING AS A CHAT AT ONCE: an item with
// no conversation yet has one made for it, with the issue in it and no model
// asked anything, the first time the cursor rests on its manager row. nil on
// every other row, and once asked.
func (a *app) factoryTalkAuto() tea.Cmd {
	if !a.at(pageFactory) || !a.fp.open || !a.factory.Has("talk") || !a.factoryCanHost() || a.fp.act.doing != "" {
		return nil
	}
	it, ok := a.factoryCursorItem()
	if !ok || a.fp.host.talkAsked[it.ID] {
		return nil
	}
	r, ok := a.factoryPageRowAt(it)
	if !ok || r.kind != factoryPageManager || a.factoryChatOf(it, r) != "" {
		return nil
	}
	return a.factoryTalkFetch(it, false)
}

// factoryHostSettle is the sync's settling of the center ([app.factoryHostSync]).
func (a *app) factoryHostSettle() tea.Cmd {
	h := &a.fp.host
	if !a.at(pageFactory) || !a.fp.open || h.forwarding {
		if !a.at(pageFactory) || !a.fp.open {
			var cmd tea.Cmd
			if len(h.opened) > 0 {
				// Back on the floor, the chat in front is stepped back from
				// too; gone to another place, the one in front is where the
				// person went, and stays.
				cmd = a.factoryHostLetGo("", a.at(pageFactory))
			}
			if h.key != "" || h.want != "" || len(h.opened) > 0 {
				*h = factoryHost{}
			}
			a.fp.box = false
			return cmd
		}
		return nil
	}
	if a.fp.box && h.key != "" && h.opened[h.key] != "" {
		if h.kept == nil {
			h.kept = map[string]bool{}
		}
		h.kept[h.key] = true
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil
	}
	want := ""
	if r, ok := a.factoryPageRowAt(it); ok && a.factoryCanHost() {
		want = a.factoryChatOf(it, r)
	}
	if want != h.want {
		if h.want != "" {
			h.focus = false
		}
		h.want, h.wantKey, h.why = want, "", ""
		if want != "" {
			h.wantKey = a.convKey(want)
		}
	}
	let := a.factoryHostLetGo(h.wantKey, false)
	if want == "" {
		if h.key != "" {
			h.key = ""
			a.touch()
		}
		a.fp.box = false
		return let
	}
	if h.wantKey != "" && h.wantKey == a.frontTabKey() {
		if a.factoryFrontEnded() {
			// THE CHAT IN FRONT ENDED UNDER THE PAGE: the live view of a
			// running step whose round has closed. It is let go of and the
			// transcript asked for again, which opens it from the journal,
			// box and all.
			if back, ok := a.leaveFront(factoryLetGoOff, false); ok {
				delete(h.opened, h.wantKey)
				h.key, h.why, h.asked = "", "", want
				a.fp.box = false
				a.touch()
				return tea.Batch(let, back, a.factoryHostBring(want))
			}
		}
		if h.key != h.wantKey {
			h.key, h.why = h.wantKey, ""
			a.touch()
		}
		if h.focus {
			h.focus, a.fp.box = false, true
		}
		return let
	}
	if h.key != "" {
		h.key = ""
		a.touch()
	}
	a.fp.box = false
	if h.asked == want {
		return let
	}
	h.asked = want
	return tea.Batch(let, a.factoryHostBring(want))
}

// factoryHostBring brings transcript chat in front without leaving the page:
// at once when this window holds it behind, and otherwise the whole open asked
// OFF THE LOOP AND OFF THE DOOR LINE, landing only if the page still wants it.
//
// THE OPEN IS NOT A GESTURE'S DOOR, so it does not stand in the ordered line
// ([app.besideLine]): it is the cursor resting on a row, and nothing a person
// does next depends on it having been asked first. In the line it waited
// behind whatever stood there, and the center said `opening the chat…` for as
// long as that took, which on the owner's run (2026-10-09) was for good. And
// the open itself is a real conversation being built (a launch for the step's
// folder, its journal replayed), so it is asked on the ask's goroutine and
// only the attach is done on the loop: the page never stops drawing for it.
//
// AN OPEN THAT DOES NOT ANSWER IS SAID TO HAVE FAILED after
// [factoryHostOpenWait], never left spinning: one output, one meaning. What
// it opens after that is let go of where it lands.
func (a *app) factoryHostBring(chat string) tea.Cmd {
	if a.holding(chat) {
		cmd, _ := a.bringForward(chat)
		a.touch()
		return cmd
	}
	if a.open == nil || a.shared {
		// THE OLDER DOOR (a whole-window swap, or the resume shape) keeps the
		// open on the loop, where its swap of the conversation in front
		// belongs.
		return a.besideLine(func() func(bool) tea.Cmd {
			where := factoryChatFolder(chat)
			return func(bool) tea.Cmd {
				h := &a.fp.host
				if h.want != chat || !a.at(pageFactory) || !a.fp.open {
					if h.asked == chat {
						h.asked = ""
					}
					return nil
				}
				cmd, refusal := a.openBeside(where, chat)
				if refusal != "" {
					h.why = refusal
				} else {
					a.factoryHostOpened(chat)
				}
				a.touch()
				return cmd
			}
		})
	}
	open, wait := a.open, factoryHostOpenWait
	return a.besideLine(func() func(bool) tea.Cmd {
		where := factoryChatFolder(chat)
		conv, err := factoryOpenWithin(open, where, chat, wait)
		return func(bool) tea.Cmd { return a.factoryHostLanded(chat, conv, err) }
	})
}

// factoryHostOpenWait is how long the center waits on an open before it says
// the chat did not open. A var so a test can shorten it.
var factoryHostOpenWait = 30 * time.Second

// errFactoryHostSlow is an open that did not answer within the wait.
var errFactoryHostSlow = errors.New("nothing answered within " + strconv.Itoa(int(factoryHostOpenWait/time.Second)) + "s" + rowSep + "choose the row again to try again")

// factoryOpenWithin asks open, and answers [errFactoryHostSlow] when it has
// not answered within wait. A conversation that opens after that is let go of
// as it lands, so a late open holds no journal nobody is showing.
func factoryOpenWithin(open func(where, chat string) (Conversation, error), where, chat string, wait time.Duration) (Conversation, error) {
	type answer struct {
		conv Conversation
		err  error
	}
	got := make(chan answer, 1)
	var mu sync.Mutex
	gone := false
	guard.Go("tui3/factory-host-open", func() {
		conv, err := open(where, chat)
		if late := factoryOpenLand(&mu, &gone, got, answer{conv, err}); late && err == nil && conv.Agent != nil {
			leaveAgent(conv.Agent)
		}
	})
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case r := <-got:
		return r.conv, r.err
	case <-timer.C:
	}
	mu.Lock()
	defer mu.Unlock()
	select {
	case r := <-got:
		return r.conv, r.err
	default:
	}
	gone = true
	return Conversation{}, errFactoryHostSlow
}

// factoryOpenLand hands an open's answer to the asker, and reports true when
// the asker has stopped waiting, so the answer is the opener's to let go of.
func factoryOpenLand[T any](mu *sync.Mutex, gone *bool, got chan T, v T) bool {
	mu.Lock()
	defer mu.Unlock()
	if *gone {
		return true
	}
	got <- v
	return false
}

// factoryHostLanded folds an open asked by [app.factoryHostBring] in, on the
// loop: the conversation goes in front when the page still wants it, its
// refusal is said in the center when it refused, and a conversation nobody
// wants any more is let go of rather than left holding its journal.
func (a *app) factoryHostLanded(chat string, conv Conversation, err error) tea.Cmd {
	h := &a.fp.host
	wanted := h.want == chat && a.at(pageFactory) && a.fp.open
	if err != nil {
		if wanted {
			h.why = factoryHostRefusal(err)
		} else if h.asked == chat {
			h.asked = ""
		}
		a.touch()
		return nil
	}
	if !wanted || a.holding(chat) {
		if h.asked == chat && !wanted {
			h.asked = ""
		}
		if conv.Agent != nil && !a.holdsAgent(conv.Agent) {
			factoryLetGoOff(conv.Agent)
		}
		if !wanted {
			return nil
		}
		// BROUGHT IN ANOTHER WAY while this open was out: that one stays.
		cmd, _ := a.bringForward(chat)
		a.touch()
		return cmd
	}
	cmd := a.takeBeside(conv)
	a.factoryHostOpened(chat)
	a.touch()
	return cmd
}

// factoryHostOpened records a chat the page opened itself, so it is let go of
// when the cursor leaves it.
func (a *app) factoryHostOpened(chat string) {
	key := a.convKey(chat)
	if key == "" {
		return
	}
	h := &a.fp.host
	if h.opened == nil {
		h.opened = map[string]string{}
	}
	h.opened[key] = chat
}

// holdsAgent says whether agent is the conversation in front or one this
// window keeps behind.
func (a *app) holdsAgent(agent Agent) bool {
	if a.agent == agent {
		return true
	}
	for _, held := range a.behind {
		if held != nil && held.conv.Agent == agent {
			return true
		}
	}
	return false
}

// factoryHostRefusal is an open's refusal as the center says it.
func factoryHostRefusal(err error) string {
	if errors.Is(err, session.ErrSessionLocked) {
		return sessionBusyWord
	}
	if words := strings.TrimSpace(err.Error()); words != "" {
		return words
	}
	return "it gave no reason"
}

// factoryLetGoOff lets go of a conversation away from the loop: a detach where
// the work outlives the view (a step's live chat), the ordinary close where it
// does not. Closing can wait on a turn ending; a keystroke is not charged for
// it.
func factoryLetGoOff(agent Agent) {
	if agent == nil {
		return
	}
	guard.Go("tui3/factory-host-let-go", func() { leaveAgent(agent) })
}

// endedAgent is a conversation that can say it has closed under its view: an
// in-process one whose owner (a step's round) ended it.
type endedAgent interface {
	Closed() bool
}

// factoryFrontEnded says the conversation in front has closed under the page:
// the step's round that owned it ended, and what the center shows can no
// longer take a word. It asks the agent alone, under its own lock.
func (a *app) factoryFrontEnded() bool {
	ended, ok := a.agent.(endedAgent)
	return ok && ended.Closed()
}

// factoryHostLetGo lets go of every chat the page opened that the person did
// not keep and the page does not want now (want, a key, or "" for none):
// one held behind is detached where it stands; the one in front is stepped
// back from, bringing the conversation before it forward, only when all is
// set, the page closing.
func (a *app) factoryHostLetGo(want string, all bool) tea.Cmd {
	h := &a.fp.host
	var cmds []tea.Cmd
	for key := range h.opened {
		if key == want || h.kept[key] {
			continue
		}
		if held := a.behind[key]; held != nil {
			a.letGoKept(key, held, factoryLetGoOff)
			delete(h.opened, key)
			continue
		}
		if key != a.frontTabKey() {
			// Gone already (closed by hand, or taken by another window).
			delete(h.opened, key)
			continue
		}
		if all {
			if cmd, ok := a.stepBackFront(); ok {
				cmds = append(cmds, cmd)
			}
			delete(h.opened, key)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	a.touch()
	return tea.Batch(cmds...)
}

// factoryHostWaitWords is what the center says while the selected row's chat
// is not in front yet: that it is opening, or why it did not open. "" when
// the row wants no chat, or it is in front.
func (a *app) factoryHostWaitWords() string {
	h := &a.fp.host
	if h.want == "" || h.key != "" {
		return ""
	}
	if h.why != "" {
		return "the chat did not open" + rowSep + h.why
	}
	return "opening the chat…"
}

// ── THE FRAME ───────────────────────────────────────────────────────────────

// factoryHostFrame is the item page's whole frame while the center hosts a
// chat, and false when it hosts none (the shared place frame draws the page
// then).
func (a *app) factoryHostFrame() ([]string, int, int, bool) {
	if !a.factoryHosting() {
		return nil, 0, 0, false
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil, 0, 0, false
	}
	width, height := max(a.width, 8), max(a.height, 1)
	left := factoryItemColW + factoryRuleW
	paneW, _ := a.size()
	// THE CHAT'S OWN FRAME, laid out in its own cells: the page is set aside
	// for the draw, so every surface inside it answers as it does in the
	// conversation.
	h := &a.fp.host
	h.forwarding, a.page = true, pageNone
	chat, caretX, caretY := a.chatFrameLines(paneW, height)
	headN := a.tabsHeight(paneW) + a.headSealHeight(paneW)
	h.forwarding, a.page = false, pageFactory
	// THE HEAD IS THE PLACES' HEAD AND THE BAR IS THE PAGE'S, AT THE FULL
	// WIDTH, drawn in the places' palette as the page draws them.
	was := a.pal
	a.pal = was.onPlaces()
	a.pal.placeRows = true
	defer func() { a.pal = was }()
	a.tabRow = -1
	var head []string
	if headN > 0 {
		head = a.headRows(width, "", a.pal)
		for len(head) < headN {
			head = append(head, "")
		}
		head = head[:headN]
	}
	rows := a.factoryItemRows(it)
	a.fp.stage = moveCursor(a.fp.stage, 0, len(rows))
	g := &a.fp.geo
	*g = factoryItemGeo{barY: headN, ctlX0: -1, ctlX1: -1, boxY: -1, hosted: true}
	bar := a.factoryBarRows(it, width)
	g.leftY, g.leftW = headN+len(bar), factoryItemColW
	g.centerX, g.centerY = left, g.leftY
	col := a.factoryItemColumn(it, rows, max(height-g.leftY, 0))
	sep := a.pal.dim(a.linearMark("│", "|"))
	lines := make([]string, 0, height)
	lines = append(lines, head...)
	lines = append(lines, bar...)
	for i := len(lines); i < height; i++ {
		row := ""
		if i < len(chat) {
			row = chat[i]
		}
		c := factorySpaces(factoryItemColW)
		if r := i - g.leftY; r >= 0 && r < len(col) {
			c = col[r]
		}
		lines = append(lines, c+sep+row)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	// A reply is read once its transcript is on screen, as in the chat.
	delete(a.unreadChats, a.frontTabKey())
	// THE CARET IS THE BOX'S ONLY WHILE THE BOX HAS THE KEYS.
	if !a.fp.box {
		a.caret = false
	}
	return lines, caretX + left, caretY, true
}

// ── THE KEYS AND THE POINTER ────────────────────────────────────────────────

// factoryRoute takes what the page keeps of a message while the center
// hosts a chat and hands the rest to the chat. It is read at the top of
// [app.route], and answers false at once on every other place, with an
// overlay of the page up, and while a message is already being handed on.
func (a *app) factoryRoute(msg tea.Msg) (tea.Cmd, bool) {
	if a.fp.host.forwarding || !a.factoryHosting() || a.mapShowing || a.bar.on {
		return nil, false
	}
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		return a.factoryRouteKey(m)
	case tea.PasteMsg:
		if a.fp.box {
			return a.factoryForward(m), true
		}
	case tea.MouseClickMsg:
		return a.factoryRouteMouse(m, m.Mouse())
	case tea.MouseReleaseMsg:
		return a.factoryRouteMouse(m, m.Mouse())
	case tea.MouseMotionMsg:
		return a.factoryRouteMouse(m, m.Mouse())
	case tea.MouseWheelMsg:
		return a.factoryRouteMouse(m, m.Mouse())
	}
	return nil, false
}

// factoryRouteKey is a key while the center hosts a chat: with the box
// focused every key is the chat's but `esc` and `tab`, which give the keys
// back to the page (a running turn is paused from the bar, not from the
// box); with the keys walking the page, the page has them.
func (a *app) factoryRouteKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.fp.box {
		return nil, false
	}
	switch msg.String() {
	case "ctrl+c":
		return nil, false
	case "esc", "tab", "shift+tab":
		a.fp.box = false
		a.touch()
		return nil, true
	}
	return a.factoryForward(msg), true
}

// factoryRouteMouse is a pointer event while the center hosts a chat: over
// the chat it is the chat's, its column moved into the chat's own cells, and
// a press there puts the keys in the box; everywhere else it is the page's,
// and a press there gives the keys back to the page.
func (a *app) factoryRouteMouse(msg tea.Msg, m tea.Mouse) (tea.Cmd, bool) {
	g := &a.fp.geo
	if m.Y < g.centerY || m.X < g.centerX {
		if _, click := msg.(tea.MouseClickMsg); click && a.fp.box {
			a.fp.box = false
			a.touch()
		}
		return nil, false
	}
	if a.fp.hot != (factoryItemHot{}) {
		a.fp.hot = factoryItemHot{}
		a.fp.crumbHover = factoryCrumbNone
		a.touch()
	}
	m.X -= g.centerX
	switch msg.(type) {
	case tea.MouseClickMsg:
		a.fp.box = true
		a.touch()
		return a.factoryForward(tea.MouseClickMsg(m)), true
	case tea.MouseReleaseMsg:
		return a.factoryForward(tea.MouseReleaseMsg(m)), true
	case tea.MouseMotionMsg:
		return a.factoryForward(tea.MouseMotionMsg(m)), true
	case tea.MouseWheelMsg:
		return a.factoryForward(tea.MouseWheelMsg(m)), true
	}
	return nil, false
}

// factoryForward hands one message to the hosted chat as if no place were
// standing, and puts the page back after it unless the message itself went
// somewhere else (another place, or a page of its own).
func (a *app) factoryForward(msg tea.Msg) tea.Cmd {
	h := &a.fp.host
	h.forwarding, a.page = true, pageNone
	_, cmd := a.route(msg)
	h.forwarding = false
	if !a.pageShowing() {
		a.page = pageFactory
	}
	return cmd
}
