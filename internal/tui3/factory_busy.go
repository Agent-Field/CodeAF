package tui3

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE FLOOR'S WORK IN FLIGHT ──────────────────────────────────────────────
//
// EVERY ASYNCHRONOUS THING ON THE FLOOR SAYS IT IS HAPPENING, in the words
// the surface already has (owner ruling, 2026-10-08): the braille spinner a
// genuinely in-flight row wears in the transcript (formingblock.go), a dim
// word after it, and NOTHING AT ALL WHEN NOTHING IS IN FLIGHT (the emptiness
// law). WHILE SOMETHING IS IN FLIGHT THE FLOOR ASKS FOR FRAMES AT THE
// SPINNER'S OWN CADENCE, AND WHILE NOTHING IS IT ASKS FOR NONE
// ([app.factorySpinning], the paint clock's twentieth reason). The spinner
// counts painted frames, and the place's own beat paints once in three
// seconds, so a floor that left the turning to that beat drew a spinner that
// stood still: the owner looked at `⠸ asking gh…` and could not tell anything
// was running (2026-10-08). A still floor still costs nothing.
//
// Three kinds of flight are drawn, each from what the floor itself says rather
// than from a guess:
//
//   - AN ITEM BEING READ OR READ AGAIN, from [factory.Snapshot.Busy]: the row's
//     priority cell spins and its last fact is `reading…` or `refreshing…`;
//     the peek's read block says `reading…` while the read is still empty;
//     the item page's read line counts up the seconds.
//   - AN ITEM WAITING ITS TURN, which the floor says with any other busy word
//     ([factory.BusyWaiting]): ONLY THE ROW BEING READ SPINS. A row merely
//     queued behind it wears a still dim dot in the priority cell and `waiting
//     to read` as its last fact, because a whole floor's re-read that spun
//     every row it would reach made "queued" and "reading now" one mark, and
//     spent the accent once per row (factory_marks.go's [app.factoryRowSpins]).
//   - THE WHOLE FLOOR BEING READ AGAIN, from [factory.Snapshot.BusyAll], and a
//     source mid-poll, from [factory.SourceInfo.Polling]: the handover's facts
//     clause spins.
//   - A DOOR A KEY ASKED, while it is out (`T`, `b`, the picker's `enter`,
//     `R`): the place's note line spins with what the door is doing, until it
//     answers ([app.factoryDoingNote]).
//
// AND `u` AND `U` ARE THE PERSON'S OWN ASK. `u` reads the item under the cursor
// again through [factory.Seam.Refresh]; the note line says `re-reading #6…`
// until the floor stops saying the item is busy, and then what it cost. `U`
// reads every item again through [factory.Seam.RefreshAll], and ASKS FIRST:
// the door is asked under [factory.DryRun] for the count and the cost, and the
// question stands where the typing row stands until `y` or `n`.

// factoryRefreshWait is how long a re-read door is given to answer a key. The
// read itself runs on; this bounds only the door's reply.
const factoryRefreshWait = 30 * time.Second

// factoryRefreshAsk is `U`'s question: how many items the whole re-read would
// read, and what the door says it would cost.
type factoryRefreshAsk struct {
	items int
	usd   float64
}

// factoryBusy is what the floor says is being done to the item with id, as
// the one word it draws, and false when nothing is.
func (a *app) factoryBusy(id int) (string, bool) {
	word, ok := a.fp.snap.Busy[id]
	word = strings.TrimSpace(word)
	if !ok || word == "" {
		return "", false
	}
	return word, true
}

// factorySpinning says whether the floor is drawing a spinner that has to
// turn: the factory place is the one showing (the picker and the recipe page
// stand over it on the same place) and something on it is in flight. Each
// term is a spinner this file's header names, read from the fact that draws
// it: a door a key asked, out ([app.factoryDoingNote]); an item being read,
// whose row, peek and read line spin ([app.factoryRowSpins]; a row waiting
// its turn wears a still dot and turns nothing); the whole floor read again,
// or a source mid-poll ([app.factoryHeadFresh]); a manager reading an issue
// whose page just opened ([app.factoryShapeOnOpen]); and a running stage on
// the open item page ([app.factoryStageMark]). The linear tier draws one still
// mark ([app.formingMark]), so it turns nothing either.
func (a *app) factorySpinning() bool {
	if !a.at(pageFactory) || a.linear {
		return false
	}
	if a.fp.act.doing != "" || len(a.fp.shaping) > 0 || strings.TrimSpace(a.fp.snap.BusyAll) != "" || a.factoryFirstReading() {
		return true
	}
	for id := range a.fp.snap.Busy {
		if a.factoryRowSpins(id) {
			return true
		}
	}
	for _, src := range a.fp.snap.Sources {
		if src.Polling && src.Name != "" {
			return true
		}
	}
	return a.factoryPageRunning()
}

// factoryPageRunning says whether the item page is open, on the place
// showing, over an item with a stage running: the one moment the page is
// alive, its stage spinning ([app.factoryStageMark]) and its time and log
// read every second ([app.factoryWantsSecondBeat]).
func (a *app) factoryPageRunning() bool {
	if !a.at(pageFactory) || !a.fp.open {
		return false
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return false
	}
	// THE MANAGER THINKING IS THE PAGE ALIVE TOO: its spinner turns and its
	// conversation is read every second, so the reply streams in
	// (factory_timeline.go).
	if a.factoryTLThinking(it) {
		return true
	}
	if it.Stream == nil {
		return false
	}
	for i := range it.Stream.Phases {
		if factoryPhaseKind(it, i) == factoryMarkRunning {
			return true
		}
	}
	return false
}

// factorySpin is the spinner's frame now: the transcript's own braille, on
// the transcript's own clock, and one still mark under the linear tier, which
// a surface read aloud would otherwise hear thirty times a second
// ([app.formingMark] makes the same trade).
func (a *app) factorySpin() string { return a.formingMark() }

// factoryFoldBusy takes what a fresh snapshot says about work in flight: the
// moment each busy item was first seen busy, so its read line can count up,
// and the end of every read `u` asked for, which says on the note line what
// it cost.
func (a *app) factoryFoldBusy() {
	busy := a.fp.snap.Busy
	for id := range a.fp.busySince {
		if _, ok := busy[id]; !ok {
			delete(a.fp.busySince, id)
		}
	}
	for id := range busy {
		if _, ok := a.fp.busySince[id]; !ok {
			if a.fp.busySince == nil {
				a.fp.busySince = map[int]time.Time{}
			}
			a.fp.busySince[id] = a.now()
		}
	}
	for id, usd := range a.fp.rereads {
		if _, ok := busy[id]; ok {
			continue
		}
		delete(a.fp.rereads, id)
		ref := "#" + itoa(id)
		if it, ok := a.factoryItemByID(id); ok {
			ref = it.Ref()
		}
		words := ref + " refreshed"
		if cost := factoryCostWord(usd); cost != "" {
			words += rowSep + "~" + cost
		}
		a.pageMsg = words
	}
}

// factoryCostWord is what a read costs, to the first figure that is not zero:
// `$0.0004`, `$0.004`, `$0.12`. A read is cheap enough that cents would say
// nothing, and the emptiness law draws nothing for nothing.
func factoryCostWord(usd float64) string {
	if usd <= 0 {
		return ""
	}
	if usd >= 0.01 {
		return dollars(usd)
	}
	digits := 2
	for x := usd; x < 0.1 && digits < 8; x *= 10 {
		digits++
	}
	return "$" + strings.TrimRight(strings.TrimRight(strconv.FormatFloat(usd, 'f', digits-1, 64), "0"), ".")
}

// factoryReread is `u`: the item under the cursor is read again through the
// seam's Refresh door, off the loop, and the note line says so until the floor
// says the read is over ([app.factoryFoldBusy]).
func (a *app) factoryReread(it factory.Item) tea.Cmd {
	if a.factory.Refresh == nil {
		return nil
	}
	id := it.ID
	if a.fp.rereads == nil {
		a.fp.rereads = map[int]float64{}
	}
	a.fp.rereads[id] = 0
	a.pageMsg = ""
	// THE COST OF A READ IS THE FLOOR'S LAST PRICED READ: the door answers only
	// whether it worked, and the snapshot carries what a read costs.
	usd := a.fp.snap.LastReadCost
	return a.factoryDo(func(s factory.Seam) error {
		ctx, cancel := context.WithTimeout(context.Background(), factoryRefreshWait)
		defer cancel()
		return s.Refresh(ctx, id)
	}, func(err error) {
		if err != nil {
			delete(a.fp.rereads, id)
			return
		}
		if _, waiting := a.fp.rereads[id]; waiting {
			a.fp.rereads[id] = usd
			// A READ THE DOOR FINISHED BEFORE IT ANSWERED has no busy entry
			// left to clear, so it is said done now.
			if _, busy := a.factoryBusy(id); !busy {
				a.factoryFoldBusy()
			}
		}
	})
}

// factoryRereadAll is `U`: the whole floor's re-read is priced first, under
// [factory.DryRun], and the question is put where the typing row stands.
func (a *app) factoryRereadAll() tea.Cmd {
	all := a.factory.RefreshAll
	if all == nil {
		return nil
	}
	a.pageMsg = ""
	a.fp.act.doing = "pricing a re-read of the floor…"
	return a.offLoop(func() func(bool) tea.Cmd {
		ctx, cancel := context.WithTimeout(factory.DryRun(context.Background()), factoryRefreshWait)
		defer cancel()
		n, usd, err := all(ctx)
		return func(bool) tea.Cmd {
			a.fp.act.doing = ""
			switch {
			case err != nil:
				a.pageMsg = strings.TrimSpace(err.Error())
			case n <= 0:
				a.pageMsg = "nothing on the floor to " + wordRefresh
			default:
				a.fp.act.refresh = &factoryRefreshAsk{items: n, usd: usd}
			}
			a.touch()
			return nil
		}
	})
}

// factoryRefreshKey is a key while `U`'s question stands: `y` goes, `n` and
// `esc` do not, and every other key is the question's and does nothing.
func (a *app) factoryRefreshKey(k string) tea.Cmd {
	switch k {
	case "y":
		a.fp.act.refresh = nil
		a.touch()
		return a.factoryDo(func(s factory.Seam) error {
			ctx, cancel := context.WithTimeout(context.Background(), factoryRefreshWait)
			defer cancel()
			_, _, err := s.RefreshAll(ctx)
			return err
		}, nil)
	case "n", "esc":
		a.fp.act.refresh = nil
		a.touch()
	}
	return nil
}

// factoryRefreshRow is `U`'s question as the one row it is drawn on:
// `re-read 8 items · ~$0.004? [y] go · [n] not now`.
func (a *app) factoryRefreshRow(measure int) string {
	q := a.fp.act.refresh
	if q == nil || measure <= 0 {
		return ""
	}
	pal := a.pal
	words := "re-read " + itoa(q.items) + " " + factoryPlural(q.items, "item", "items")
	if cost := factoryCostWord(q.usd); cost != "" {
		words += rowSep + "~" + cost
	}
	return fit(pal.ink(words+"? ")+pal.accent("[y] go")+pal.dim(" · [n] not now"), measure)
}

// factoryDoingNote is the note line while a door a key asked is out: the
// spinner and what the door is doing, dim; then a read `u` asked for; and ""
// when nothing is in flight.
func (a *app) factoryDoingNote() string {
	if d := a.fp.act.doing; d != "" {
		return a.pal.accent(a.factorySpin()) + " " + a.pal.dim(d)
	}
	for id := range a.fp.rereads {
		ref := "#" + itoa(id)
		if it, ok := a.factoryItemByID(id); ok {
			ref = it.Ref()
		}
		return a.pal.dim("re-reading " + ref + "…")
	}
	return ""
}

// factoryReadLine is the item page's line about the cheap read: when it was
// made and the key that makes it again, `read 3m ago · u refresh`; while one is
// in flight, the spinner and the seconds it has taken, `⠋ reading · 4s`; while
// it waits its turn, `waiting to read`, still. A read never made says so, `not
// read yet · u refresh`, and a read whose time nobody kept says only `u
// refresh`. THE KEY IS NEVER A LINE ON ITS OWN: a bare `u refresh` under the
// body read as a placeholder. A seam with no door to read says no key, and an
// unread item on one is no line.
func (a *app) factoryReadLine(it factory.Item, measure int) string {
	pal := a.pal
	if a.factoryRowWaits(it.ID) {
		return pal.dim(fit(factoryWaitWords, measure))
	}
	if word, busy := a.factoryBusy(it.ID); busy {
		line := word
		if at, ok := a.fp.busySince[it.ID]; ok {
			if up := countUpWord(a.now().Sub(at)); up != "" {
				line += rowSep + up
			}
		}
		return fit(pal.accent(a.factorySpin())+" "+pal.dim(line), measure)
	}
	door := a.factory.Has("refresh")
	var parts []string
	switch at := it.Triage.TriagedAt; {
	case !at.IsZero() && !a.fp.snap.Now.IsZero():
		if ago := factoryAgo(a.fp.snap.Now.Sub(at)); ago == "now" {
			parts = append(parts, "read just now")
		} else {
			parts = append(parts, "read "+ago+" ago")
		}
		if door {
			parts = append(parts, factoryHintClause(keyRefresh, wordRefresh))
		}
	case !door:
	case strings.TrimSpace(it.Triage.Read) == "":
		parts = append(parts, "not read yet", factoryHintClause(keyRefresh, wordRefresh))
	default:
		parts = append(parts, factoryHintClause(keyRefresh, wordRefresh))
	}
	if len(parts) == 0 {
		return ""
	}
	return pal.dim(fit(strings.Join(parts, rowSep), measure))
}
