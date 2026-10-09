package tui3

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE LIVE ITEM CARD: ONE FLOOR ITEM, DRAWN IN A CONVERSATION, KEPT TRUE.
//
// Wherever a conversation is about an item on the factory floor, the item is
// drawn in it as one block, boxed like a task element, in the transcript's
// width:
//
//	╭─ ▤ #1 Total double-counts an entry added twice     factory-demo · bug · S
//	│    new · gate ship · cap $5               ○ plan  ○ write  ○ test  ○ proof
//	╰──────────────────────────────────────────────────────────────────────────
//
// The second row says where the item stands: `running 24m · $1.42 / $5` with
// the running stage's cell in accent, `? plan is ready` in the asking colour,
// `landed · 3✓ 1✕`, `shipped · $1.90`.
//
// IT IS DRAWN IN THREE PLACES, and in no others:
//
//   - after `factory_add` settles with the floor's number, as that card's body
//     (factorycard.go's [app.factoryAdded]);
//   - at the top of an item's own conversation, where the brief's first line,
//     `[factory item #1]`, is drawn as the card instead of the brief the model
//     reads (cmd/codeaf's factory_talk.go writes it; [app.replayBlocks] draws
//     it);
//   - under a reply that names an item the floor knows — `#12` of the current
//     repository — ONCE PER TURN per item ([app.factoryRefCards]).
//
// IT STAYS LIVE. The card holds no copy of the item it trusts: every frame
// reads the floor's last snapshot, the one the page draws from. The snapshot
// is read again when a card settles (EventFactoryAdded, EventFactoryProposal,
// EventItemChanged) and on a three-second clock while a conversation holding a
// live card is in front ([factoryCardPollEvery]) — ONE Load per beat, shared
// with the floor through [app.factoryRead]'s own guard, so a screen full of
// cards costs what one does. Over --host there is no floor here to read, and
// the card draws from the item the engine's news carried instead
// ([session.FactoryNotice.Now], [session.ItemNotice.Now]), which is the same
// events a local window draws from.
//
// `enter` ON THE CARD, OR A PRESS ON IT, OPENS THE ITEM PAGE, by the road the
// floor's own `enter` takes ([app.factoryOpenItem]). A window with no floor
// behind it draws the card and opens nothing (a capability that cannot work is
// absent, not broken).

// factoryCardPollEvery is how often the floor is read again while a
// conversation holding a live item card is in front.
const factoryCardPollEvery = 3 * time.Second

// factoryCardMargin is the cells a card keeps back from the transcript's
// right edge: the margin task.go's head leaves (width - head - title - 3, and
// the one space before the rule).
const factoryCardMargin = 2

// factoryCardPollMsg is one beat of that clock.
type factoryCardPollMsg struct{}

// factoryItemCards is the live cards' one piece of state on the app: whether
// the clock is running, so two roads that arm it start one clock.
type factoryItemCards struct {
	polling bool
}

// factoryItemLive is which item a live card shows, and what it knows about
// the item before the floor has said: the fallback the card draws when there
// is no snapshot to read and no news has carried the item.
type factoryItemLive struct {
	// id is the floor's own id, and ref how the floor names the item (`#12`).
	// A card made from a reply's words may know only the ref until it finds
	// the row.
	id  int
	ref string
	// repo, title, kind and size are what the card that made this one knew.
	repo, title, kind, size string
	// last is the newest copy of the item an event carried, nil until one did.
	last *factory.Item
	// drawn is what the card's cached rows were drawn from ([app.factoryLiveStale]).
	drawn string
}

// factoryLiveItem is the item a live card draws: the floor's snapshot when it
// holds the row, unless an event has since carried a newer copy; the event's
// copy where the floor is not here; and the card's own fallback otherwise.
func (a *app) factoryLiveItem(l *factoryItemLive) factory.Item {
	if l == nil {
		return factory.Item{}
	}
	if it, ok := a.factoryLiveFind(l); ok {
		if l.last == nil || !l.last.Changed.After(it.Changed) {
			return it
		}
	}
	if l.last != nil {
		return *l.last
	}
	it := factory.Item{ID: l.id, Title: l.title, Repo: l.repo,
		Triage: factory.Triage{Type: l.kind, Size: l.size}}
	if l.kind != "" {
		it.Kind = factoryCardKind(l.kind)
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(l.ref, "#")); err == nil && n != l.id {
		it.Num = n
	}
	return it
}

// factoryLiveStale says whether an entry's cached rows were drawn from an
// item that has moved since: a live card is cached like any settled block, and
// its key is everything its two rows say — the item's state and changes, the
// floor's read of it, and the minute for a running item, whose clock is on the
// row.
func (a *app) factoryLiveStale(e *entry) bool {
	if e.kind != entryFactory || e.fac == nil || e.fac.live == nil {
		return false
	}
	it := a.factoryLiveItem(e.fac.live)
	key := strconv.Itoa(it.ID) + "|" + string(it.State) + "|" + it.Changed.String() + "|" + string(it.Gate) +
		"|" + strconv.FormatFloat(it.Cap, 'f', 2, 64) + "|" + it.Question + "|" + strconv.FormatBool(a.fp.loaded)
	if s := it.Stream; s != nil {
		key += "|" + strconv.FormatFloat(s.Spent, 'f', 2, 64) + "|" + strconv.Itoa(s.Cur) + "|" + strconv.Itoa(len(s.Phases))
		for _, ph := range s.Phases {
			key += string(ph.State)
		}
		if it.State == factory.StateRunning {
			key += "|" + a.now().Truncate(time.Minute).String()
		}
	}
	if key == e.fac.live.drawn {
		return false
	}
	e.fac.live.drawn = key
	return true
}

// factoryLiveFind is the card's row on the floor's last snapshot: by its id
// when the card knows it, and by its ref in its repository otherwise.
func (a *app) factoryLiveFind(l *factoryItemLive) (factory.Item, bool) {
	if !a.fp.loaded {
		return factory.Item{}, false
	}
	for _, it := range a.fp.snap.Items {
		if l.id > 0 {
			if it.ID == l.id {
				return it, true
			}
			continue
		}
		if l.ref != "" && it.Ref() == l.ref && (l.repo == "" || it.Repo == l.repo || it.Num == 0) {
			return it, true
		}
	}
	return factory.Item{}, false
}

// factoryLiveTake hands every live card of this item the copy an event
// carried, which is newer than any read the window has made.
func (a *app) factoryLiveTake(it factory.Item) {
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryFactory || e.fac == nil || e.fac.live == nil || e.fac.live.id != it.ID {
			continue
		}
		held := it
		e.fac.live.last = &held
		e.stale = true
	}
	a.touch()
}

// ── the card, drawn ─────────────────────────────────────────────────────────

// factoryItemCardRows is a live-only card: the item's two rows inside a frame
// of their own, the head corner leading the first and the foot closing it.
func (a *app) factoryItemCardRows(l *factoryItemLive, width int, sel bool) []string {
	// THE BOX STOPS WHERE A TASK ELEMENT'S HEAD STOPS: [app.taskHead] keeps
	// factoryCardMargin cells back from the transcript's edge, so a card never
	// runs into the side divider.
	width = max(width-factoryCardMargin, 4)
	corner, foot, rule := taskHeadCorner, taskFootCorner, a.blockRule()
	if a.pal.ascii {
		corner, foot = taskCornerASCII, taskCornerASCII
	}
	lead := corner + " "
	leadW := ansi.StringWidth(lead)
	lines := a.factoryItemLines(a.factoryLiveItem(l), max(width-leadW, 1), sel)
	if len(lines) == 0 {
		return nil
	}
	out := []string{a.pal.dim(lead) + lines[0]}
	stem := a.blockStem()
	pad := strings.Repeat(" ", max(leadW-ansi.StringWidth(stem), 0))
	for _, line := range lines[1:] {
		out = append(out, a.pal.dim(stem)+pad+line)
	}
	if fill := width - ansi.StringWidth(foot); fill > 0 {
		return append(out, a.pal.dim(foot+strings.Repeat(rule, fill)))
	}
	return append(out, a.pal.dim(foot))
}

// factoryItemLines is the item in the item card's shape, in room cells: the
// glyph, the ref and the title with the facts at the right edge, then where it
// stands with its stages at the right edge. It is the ONE place that shape is
// drawn — the offer's body, the settled offer's body and the live card all
// come here — so the row asked about is the row found.
//
// EVERY PART IS DROPPED WHEN NOBODY SAID IT (the emptiness law): an item with
// no size draws no size, a new item with no cap no cap, and a card that knows
// only a title draws one row.
func (a *app) factoryItemLines(it factory.Item, room int, sel bool) []string {
	pal := a.pal
	glyph := pal.muted(a.icon(tokens.GFileDocument))
	if sel {
		glyph = pal.bold(glyph)
	}
	name := strings.Join(strings.Fields(it.Title), " ")
	if it.ID > 0 || it.Num > 0 {
		name = strings.TrimSpace(it.Ref() + " " + name)
	}
	first := factorySpread(glyph+" "+pal.ink(name), pal.dim(factoryItemFacts(it)), room)
	state := a.factoryItemState(it)
	strip := a.factoryItemStrip(it)
	if state == "" && strip == "" {
		return []string{first}
	}
	return []string{first, factorySpread("  "+state, strip, room)}
}

// factoryItemFacts is the right of the first row: repo · kind · size, the
// kind in the item's own word (`bug`, `feat`) where the read gave one.
func factoryItemFacts(it factory.Item) string {
	kind := strings.TrimSpace(it.Triage.Type)
	if kind == "" {
		kind = string(it.Kind)
	}
	return strings.Join(nonEmpty([]string{it.Repo, kind, it.Triage.Size}), rowSep)
}

// factoryItemState is where the item stands, painted: the left of the second
// row.
func (a *app) factoryItemState(it factory.Item) string {
	pal := a.pal
	switch it.State {
	case factory.StateRunning:
		word := "running"
		if s := it.Stream; s != nil {
			if s.Paused {
				word = "paused"
			}
			if took := factoryElapsed(s.Started, factoryEnd(s, a.now())); took != "" {
				word += " " + took
			}
		}
		parts := []string{word}
		if spend := factorySpend(it.Stream, it.Cap); spend != "" {
			parts = append(parts, spend)
		}
		return pal.accent(parts[0]) + pal.dim(strings.Join(append([]string{""}, parts[1:]...), rowSep))
	case factory.StateNeedsYou:
		q := strings.Join(strings.Fields(it.Question), " ")
		if q == "" {
			q = string(factory.StateNeedsYou)
		}
		return pal.ask(a.icon(tokens.GNeedsHuman) + " " + q)
	case factory.StateLanded:
		shown, not := 0, 0
		for _, c := range append(append([]factory.Claim(nil), it.Proof...), it.Policy...) {
			if c.OK {
				shown++
			} else {
				not++
			}
		}
		var marks []string
		if shown > 0 {
			marks = append(marks, itoa(shown)+a.icon(tokens.GSettled))
		}
		if not > 0 {
			marks = append(marks, itoa(not)+a.icon(tokens.GFailed))
		}
		out := pal.muted("landed")
		if len(marks) > 0 {
			out += pal.dim(rowSep + strings.Join(marks, " "))
		}
		return out
	case factory.StateShipped:
		out := pal.muted("shipped")
		if s := it.Stream; s != nil {
			if spent := factoryMoney(s.Spent); spent != "" {
				out += pal.dim(rowSep + spent)
			}
		}
		return out
	case factory.StateDismissed:
		return pal.dim(wordDismissed)
	case "":
		return ""
	}
	// new and queued: the state, then the chips that are set, and the read's
	// estimate when no cap says what it may spend.
	parts := []string{string(it.State)}
	if it.Gate != "" {
		parts = append(parts, factoryAskAtWords(it.Gate))
	}
	if c := factoryMoney(it.Cap); c != "" {
		parts = append(parts, wordBudget+" "+c)
	} else if est := factoryMoney(it.Triage.Est); est != "" {
		parts = append(parts, "~"+est)
	}
	return pal.muted(parts[0]) + pal.dim(strings.Join(append([]string{""}, parts[1:]...), rowSep))
}

// factoryItemStrip is the right of the second row: every stage the item runs,
// a mark and a name each, the running one in accent — the floor's own phase
// marks ([app.factoryPhaseMark]), so the card and the rail cannot disagree.
// An item whose stages nobody has copied yet shows its kind's stages from the
// repository's recipe, or the default recipe's where the floor holds none.
func (a *app) factoryItemStrip(it factory.Item) string {
	stages := it.Stages
	if len(stages) == 0 && a.fp.loaded {
		stages = factoryStages(a.fp.snap, it)
	}
	if len(stages) == 0 {
		kind := it.Kind
		if kind == "" {
			kind = factory.KindIssue
		}
		stages = factory.DefaultRecipe().For(kind)
	}
	phase := map[string]factory.PhaseState{}
	if s := it.Stream; s != nil {
		for _, ph := range s.Phases {
			phase[ph.Name] = ph.State
		}
	}
	var cells []string
	for _, st := range stages {
		if !st.On || !factory.Fits(st, it) {
			continue
		}
		mark, paint := a.factoryPhaseMark(phase[st.Name])
		name := a.pal.dim(st.Name)
		if phase[st.Name] == factory.PhaseRunning || phase[st.Name] == factory.PhaseWaiting {
			name = paint(st.Name)
		}
		cells = append(cells, paint(mark)+" "+name)
	}
	return strings.Join(cells, "  ")
}

// ── the brief's marker ──────────────────────────────────────────────────────

// reFactoryMarker is the brief's first line: `[factory item #12]`, or
// `[factory item #1540 · 7]` for an item the floor names by a forge number
// (cmd/codeaf's talkMarker).
var reFactoryMarker = regexp.MustCompile(`^\[factory item (#\d+|ci)(?: · (\d+))?\]`)

// factoryMarkerCard is the live card a brief's marker stands for, or false
// when the text is not a brief. The title and the repository come from the
// brief's own next lines (`#12 · title`, `repo web · …`), so the card has
// something true to say before the floor is read and where it never is.
func factoryMarkerCard(text string) (*factoryCard, bool) {
	text = strings.TrimSpace(text)
	m := reFactoryMarker.FindStringSubmatch(text)
	if m == nil {
		return nil, false
	}
	live := &factoryItemLive{ref: m[1]}
	if m[2] != "" {
		live.id, _ = strconv.Atoi(m[2])
	} else if n, err := strconv.Atoi(strings.TrimPrefix(m[1], "#")); err == nil {
		live.id = n
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 1 {
		if title, ok := strings.CutPrefix(strings.TrimSpace(lines[1]), m[1]+rowSep); ok {
			live.title = title
		}
	}
	if len(lines) > 2 {
		for _, part := range strings.Split(strings.TrimSpace(lines[2]), rowSep) {
			if repo, ok := strings.CutPrefix(part, "repo "); ok {
				live.repo = strings.TrimSpace(repo)
			}
		}
	}
	return &factoryCard{live: live, liveOnly: true}, true
}

// ── a reply that names an item ──────────────────────────────────────────────

// reFactoryRef is an item ref in the model's words: `#12`, not inside a word,
// a path or a colour.
var reFactoryRef = regexp.MustCompile(`(?:^|[\s(\[*_` + "`" + `])(#\d+)\b`)

// factoryRefCards draws a live card under this turn's reply for every item it
// names that the floor knows, ONCE PER TURN per item: a reply that says `#12`
// three times draws one card, and an item already carded in this turn — by
// its own offer settling, or by the brief — draws none. It is asked as the
// turn ends, when the reply is whole.
func (a *app) factoryRefCards() tea.Cmd {
	if !a.fp.loaded || len(a.fp.snap.Items) == 0 {
		return nil
	}
	seen := map[int]bool{}
	var said []string
	for i := range a.entries {
		e := &a.entries[i]
		if e.turn != a.turn {
			continue
		}
		if e.kind == entryFactory && e.fac != nil && e.fac.live != nil {
			seen[a.factoryLiveItem(e.fac.live).ID] = true
		}
		if e.kind == entryAssistant {
			said = append(said, e.text)
		}
	}
	repo := a.factoryCurrentRepo()
	added := false
	for _, m := range reFactoryRef.FindAllStringSubmatch(strings.Join(said, "\n"), -1) {
		it, ok := a.factoryItemByRef(m[1], repo)
		if !ok || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		if !added {
			a.closeLive()
		}
		live := &factoryItemLive{id: it.ID, ref: it.Ref(), repo: it.Repo, title: it.Title}
		a.entries = append(a.entries, entry{kind: entryFactory, turn: a.turn, fac: &factoryCard{live: live, liveOnly: true}})
		added = true
	}
	if !added {
		return nil
	}
	a.follow()
	a.touch()
	return a.factoryCardPollArm()
}

// factoryCurrentRepo is the repository a bare `#12` in this conversation is
// read against: the item's own when this is an item's conversation, and the
// folder this window works in otherwise.
func (a *app) factoryCurrentRepo() string {
	for i := range a.entries {
		if e := &a.entries[i]; e.kind == entryFactory && e.fac != nil && e.fac.liveOnly && e.fac.live != nil && e.fac.live.repo != "" {
			return e.fac.live.repo
		}
	}
	if ws := strings.TrimSpace(a.workspace); ws != "" {
		return filepath.Base(ws)
	}
	return ""
}

// factoryItemByRef is the floor's row a ref names: one in the current
// repository, or one the floor numbers itself — a chat's or a terminal's
// item, whose `#<id>` is the floor's own and names one row on any repository.
func (a *app) factoryItemByRef(ref, repo string) (factory.Item, bool) {
	var own factory.Item
	found := false
	for _, it := range a.fp.snap.Items {
		if it.Ref() != ref || it.State == factory.StateDismissed {
			continue
		}
		if repo != "" && it.Repo == repo {
			return it, true
		}
		if it.Num == 0 && !found {
			own, found = it, true
		}
	}
	return own, found
}

// ── the clock ───────────────────────────────────────────────────────────────

// factoryLiveHere says whether the conversation in front holds a live card.
func (a *app) factoryLiveHere() bool {
	for i := range a.entries {
		if e := &a.entries[i]; e.kind == entryFactory && e.fac != nil && e.fac.live != nil {
			return true
		}
	}
	return false
}

// factoryCardPollArm starts the clock when a live card is in the conversation
// and a floor is behind this window, and does nothing when it is running
// already, when there is no card, or when there is no floor to read.
func (a *app) factoryCardPollArm() tea.Cmd {
	if a.fic.polling || a.factory.Load == nil || !a.factoryLiveHere() {
		return nil
	}
	a.fic.polling = true
	return surfaceTick(factoryCardPollEvery, func(time.Time) tea.Msg { return factoryCardPollMsg{} })
}

// factoryCardPoll is one beat: the floor is read when the conversation is in
// front, and the clock goes on while a live card is in it. A conversation
// without one stops the clock; the next card to arrive starts it again.
func (a *app) factoryCardPoll() tea.Cmd {
	a.fic.polling = false
	if !a.factoryLiveHere() {
		return nil
	}
	var read tea.Cmd
	if !a.pageShowing() {
		read = a.factoryRead()
	}
	return tea.Batch(read, a.factoryCardPollArm())
}

// ── opening the item ────────────────────────────────────────────────────────

// factoryCardOpens says whether this card is a door onto an item page: it
// shows an item, and a floor is behind this window to open it on.
func (a *app) factoryCardOpens(card *factoryCard) bool {
	return card != nil && card.live != nil && a.factoryConnected()
}

// openFactoryItemCard is `enter` on a selected live card, or a press on one:
// the floor comes up with its cursor on the item and the item page opens over
// it, as the floor's own `enter` opens it ([app.factoryOpenItem]). It answers
// false for an entry that is not such a card, and the key keeps its meaning.
func (a *app) openFactoryItemCard(i int) (tea.Cmd, bool) {
	if a.roomOpen() || i < 0 || i >= len(a.entries) {
		return nil, false
	}
	e := &a.entries[i]
	if e.kind != entryFactory || !a.factoryCardOpens(e.fac) {
		return nil, false
	}
	it := a.factoryLiveItem(e.fac.live)
	if it.ID <= 0 {
		return nil, false
	}
	cmd := a.showPage(pageFactory)
	a.factoryFocus(it.ID)
	var shape tea.Cmd
	if cur, ok := a.factoryCursorItem(); ok && cur.ID == it.ID {
		shape, _ = a.factoryOpenItem()
	}
	return tea.Batch(cmd, shape, a.factoryRead()), true
}
