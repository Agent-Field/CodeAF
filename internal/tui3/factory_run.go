package tui3

import (
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE RUN SURFACE ─────────────────────────────────────────────────────────
//
// What the floor shows while an item runs, drawn from the runner's own record
// (internal/factory/run/loop.go writes every field read here): each phase's
// state and round, the stream's log, the question the item is parked on, and
// the proof sheet it lands with. And the doors a person walks through from it:
// a stage that ran as a conversation is a ROOM, and `enter` on its row walks
// into it.
//
// THE SENTENCES ARE THE RUNNER'S. A question is drawn exactly as loop.go wrote
// it (`review is not clean after 2 rounds: 3 findings · one more round, or go
// on as is?`, `cap of $5 reached · $5 more, or stop?`, `plan is ready · go, or
// change it?`), and a door that refuses says its own sentence on the note line.
// The surface never paraphrases the runner, because a person who reads one
// sentence on the floor and another in the item's log has two facts to
// reconcile where there was one.
//
// NOTHING HERE IS DRAWN FOR AN ITEM WITH NO STREAM: no log row, no elapsed, no
// sign-off line (the emptiness law). And every key is drawn only where its seam
// door exists (factory_keys.go): a capability that cannot work is absent.

// ── stage rooms ─────────────────────────────────────────────────────────────

// factoryRoomOf is the conversation behind a rail row: the phase's Chat when
// the row is a stage that ran as one, and "" for every other row.
func factoryRoomOf(r factoryPageRow) string {
	if r.kind != factoryPageStage || !r.view.ran {
		return ""
	}
	return strings.TrimSpace(r.view.phase.Chat)
}

// factoryNoRoomWords is what `enter` says, on the pane's action line, on a
// stage row with no conversation to walk into: why there is none, in a few
// words. A CHECK HAS NO ROOM AND NEVER WILL, so it points at the log, which is
// where a command's grain is.
func factoryNoRoomWords(v factoryStageView) string {
	name := v.stage.Name
	kind := v.stage.Kind
	if v.ran && v.phase.Kind != "" {
		kind = v.phase.Kind
	}
	switch {
	case v.off:
		return name + " is off on this item"
	case v.skipped:
		return name + " is skipped on this item"
	case kind == factory.StageCheck:
		return name + " is a check" + rowSep + "its log is below"
	case kind == factory.StageGate:
		return name + " is your answer" + rowSep + "it has no conversation"
	case kind == factory.StagePost:
		return name + " is a write to github" + rowSep + "it has no conversation"
	case !v.ran || v.state == factory.PhasePending:
		return name + " has not started"
	case v.state == factory.PhaseRunning || v.state == factory.PhaseWaiting:
		return name + " has no conversation yet" + rowSep + "it opens when a round ends"
	}
	return name + " kept no conversation"
}

// factoryRoomRow is the stage row the item page's cursor stands on when that
// stage has a room, and false otherwise: the row whose name the crumbs carry
// and whose `enter` walks in.
func (a *app) factoryRoomRow(it factory.Item) (factoryPageRow, bool) {
	if !a.fp.open {
		return factoryPageRow{}, false
	}
	rows := a.factoryItemRows(it)
	if a.fp.stage < 0 || a.fp.stage >= len(rows) || factoryRoomOf(rows[a.fp.stage]) == "" {
		return factoryPageRow{}, false
	}
	return rows[a.fp.stage], true
}

// factoryOpenRoom is `enter` on a stage with a room: its conversation opens
// THE WAY `T` OPENS THE ITEM'S OWN (factory_talk.go), and the first `esc` on
// its empty box comes back to the item page, because the page is kept open
// under it. The conversation's folder is read off the loop, from its meta.
func (a *app) factoryOpenRoom(it factory.Item, v factoryStageView) tea.Cmd {
	chat := strings.TrimSpace(v.phase.Chat)
	a.fp.act.doing = "opening " + it.Ref() + " › " + v.stage.Name + "…"
	return a.offLoop(func() func(bool) tea.Cmd {
		where := factoryChatFolder(chat)
		return func(bool) tea.Cmd {
			a.fp.act.doing = ""
			return a.factoryEnterChat(chat, where)
		}
	})
}

// factoryChatFolder is the folder a conversation works in, by its session
// folder's meta, and "" when that cannot be read. It touches the disk, so it
// is asked off the loop.
func factoryChatFolder(chat string) string {
	if meta, err := session.LoadMeta(filepath.Dir(chat)); err == nil {
		return strings.TrimSpace(meta.Workspace)
	}
	return ""
}

// factoryEnterChat opens a conversation from the floor, on the loop, and
// keeps the way back: `T`'s and a stage room's one shared door.
func (a *app) factoryEnterChat(chat, where string) tea.Cmd {
	a.fp.act.talk = a.convKey(chat)
	cmd := a.openConversationRow(session.SessionRow{Transcript: chat, ProjectDir: where})
	if a.pageShowing() {
		// The open refused, and said why on the floor's note line: there is
		// no conversation in front to come back from.
		a.fp.act.talk = ""
	}
	a.touch()
	return tea.Batch(cmd, a.factoryTeamsRead())
}

// ── a running phase's time ──────────────────────────────────────────────────

// factoryPhaseKey names one phase of one item for [factoryPage.phaseSince].
func factoryPhaseKey(id, at int, name string) string {
	return itoa(id) + "/" + itoa(at) + "/" + name
}

// factoryFoldPhases notes when each running phase was FIRST SEEN RUNNING, on
// the floor's own clock (the snapshot's Now), and forgets every phase that is not running any more. The runner
// keeps no start on a phase, so this is what the surface saw: a page opened
// mid-phase counts from its opening, which is the honest reading of a clock
// nobody wrote down.
func (a *app) factoryFoldPhases() {
	seen := map[string]bool{}
	for _, it := range a.fp.snap.Items {
		if it.Stream == nil {
			continue
		}
		// A HELD ITEM IS TIMED THE SAME WAY, from the first snapshot that said
		// it was paused, so its row can say `paused 13m` and not how long it
		// ran (factory_marks.go's [app.factoryPausedFor]).
		if factoryPaused(it) {
			k := factoryPausedKey(it.ID)
			seen[k] = true
			if _, ok := a.fp.phaseSince[k]; !ok {
				if a.fp.phaseSince == nil {
					a.fp.phaseSince = map[string]time.Time{}
				}
				a.fp.phaseSince[k] = a.fp.snap.Now
			}
		}
		for i, ph := range it.Stream.Phases {
			if ph.State != factory.PhaseRunning {
				continue
			}
			k := factoryPhaseKey(it.ID, i, ph.Name)
			seen[k] = true
			if _, ok := a.fp.phaseSince[k]; !ok {
				if a.fp.phaseSince == nil {
					a.fp.phaseSince = map[string]time.Time{}
				}
				a.fp.phaseSince[k] = a.fp.snap.Now
			}
		}
	}
	for k := range a.fp.phaseSince {
		if !seen[k] {
			delete(a.fp.phaseSince, k)
		}
	}
}

// factoryPhaseElapsed is how long the phase at index at of the item has been
// seen running, in one short word (`12s`, `4m`, `1h 5m`), and "" when it is
// not running or no time has passed.
func (a *app) factoryPhaseElapsed(id, at int, name string) string {
	since, ok := a.fp.phaseSince[factoryPhaseKey(id, at, name)]
	if !ok || since.IsZero() || a.fp.snap.Now.IsZero() {
		return ""
	}
	d := a.fp.snap.Now.Sub(since)
	switch {
	case d < time.Second:
		return ""
	case d < time.Minute:
		return itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	}
	return factoryElapsed(since, a.fp.snap.Now)
}

// ── the log ─────────────────────────────────────────────────────────────────

// factoryIsSteer says whether a log line is the person's own words handed into
// the stream, which the runner writes as `steer: …`.
func factoryIsSteer(l factory.LogLine) bool {
	return strings.HasPrefix(strings.TrimSpace(l.Text), "steer:")
}

// factoryLogRow is one line of the stream's log: its time at the margin as
// `12:04`, dim, a mark for its kind, and its words. THERE ARE THREE VOICES: a
// thought is dim, mark and all, because it is the stream thinking aloud; the
// person's own `steer:` line is ink, because it is the one line on the log a
// person wrote; and every other line is muted with its mark in its tone's
// colour (a failure's, a success's, a question's amber).
func (a *app) factoryLogRow(l factory.LogLine, measure int) string {
	pal := a.pal
	mark, markPaint, _ := a.factoryTone(l)
	text := pal.muted
	switch {
	case l.Tone == "thought":
		markPaint, text = pal.dim, pal.dim
	case factoryIsSteer(l):
		text = pal.ink
	}
	stamp := factorySpaces(factoryStampW)
	if !l.At.IsZero() {
		stamp = pal.dim(l.At.Format("15:04")) + factorySpaces(factoryStampW-len("15:04"))
	}
	return fit(stamp+markPaint(mark)+" "+text(l.Text), measure)
}

// factoryLogPane is the `log` row's pane: the stream's log, oldest at the top
// and THE NEWEST AT THE BOTTOM, the last lines that fit, then the item's keys.
func (a *app) factoryLogPane(it factory.Item, measure, room int) []string {
	action := a.factoryPageAction(it, measure)
	avail := room
	if action != "" {
		avail = room - 1
		if room >= 3 {
			avail = room - factoryActionRows
		}
	}
	var log []factory.LogLine
	if it.Stream != nil {
		log = it.Stream.Log
	}
	if len(log) > avail {
		log = log[len(log)-max(avail, 0):]
	}
	lines := make([]string, 0, len(log))
	for _, l := range log {
		lines = append(lines, a.factoryLogRow(l, measure))
	}
	return factoryPaneLadder([][]string{lines}, action, room)
}

// ── the proof sheet ─────────────────────────────────────────────────────────

// factoryClaims is the sheet's rows, the claims and then the policy rows.
func factoryClaims(it factory.Item) []factory.Claim {
	return append(append([]factory.Claim(nil), it.Proof...), it.Policy...)
}

// factorySheetCount is how many rows the sheet holds and how many of them
// nothing showed.
func factorySheetCount(it factory.Item) (all, bad int) {
	for _, c := range factoryClaims(it) {
		all++
		if !c.OK {
			bad++
		}
	}
	return all, bad
}

// factorySignOffLine is the sheet's last line: how much of it was shown and
// the keys that settle it, `all 4 shown · s sign off` or `1 of 4 not shown ·
// e sign off with changes · B send back`. THE COUNT IS THE WHOLE SHEET, policy
// rows included, because the runner refuses a plain sign-off on any row not
// shown. The keys are drawn only on a landed item, and each only where its
// door exists; a sheet with no rows is no line.
func (a *app) factorySignOffLine(it factory.Item, measure int) string {
	all, bad := factorySheetCount(it)
	if all == 0 {
		return ""
	}
	seam := a.factory
	landed := it.State == factory.StateLanded
	var parts []string
	if bad == 0 {
		parts = append(parts, "all "+itoa(all)+" shown")
		if landed && seam.Has("signoff") {
			parts = append(parts, "s sign off")
		}
	} else {
		parts = append(parts, itoa(bad)+" of "+itoa(all)+" not shown")
		if landed && seam.Has("signoff") {
			parts = append(parts, "e sign off with changes")
		}
		if landed && seam.Has("sendback") {
			parts = append(parts, "B send back")
		}
	}
	return a.pal.dim(fit(strings.Join(parts, rowSep), measure))
}

// factorySheetRows is the sheet's rows as the proof pane and the peek draw
// them.
func (a *app) factorySheetRows(it factory.Item, measure int) []string {
	var out []string
	for _, c := range it.Proof {
		out = append(out, a.factoryClaimRow(c, "", measure))
	}
	for _, c := range it.Policy {
		out = append(out, a.factoryClaimRow(c, "policy", measure))
	}
	return out
}

// factoryProofPane is the proof row's pane: the stage's knobs and ask when the
// row is a stage, the sheet, and on a landed item the sign-off line pinned
// where the keys stand, because on the sheet those ARE the keys. Before the
// item lands the count stands as a block of its own and the item's keys keep
// the last row.
func (a *app) factoryProofPane(it factory.Item, head []string, measure, room int) []string {
	blocks := [][]string{head, a.factorySheetRows(it, measure)}
	action := a.factoryPageAction(it, measure)
	if it.State == factory.StateLanded {
		action = a.factorySignOffLine(it, measure)
	} else if line := a.factorySignOffLine(it, measure); line != "" {
		blocks = append(blocks, []string{line})
	}
	return factoryPaneLadder(blocks, action, room)
}

// ── the question ────────────────────────────────────────────────────────────

// factoryItemQuestion is the item page's second row while the item waits on
// the person: the runner's question, its mark in amber and its words in ink
// (COLOUR IS STROKE, NEVER FILL), and the answers at the right when both fit.
// THE HEAD STAYS TWO ROWS (owner ruling, 2026-10-08): the question stands
// where the gate, cap and effort stand, because none of them turns while the
// item is parked, and
// the pane's waiting stage repeats the answers under it.
func (a *app) factoryItemQuestion(it factory.Item, measure int) string {
	pal := a.pal
	q := strings.TrimSpace(it.Question)
	if it.State != factory.StateNeedsYou || q == "" {
		return ""
	}
	left := pal.ask(a.icon(tokens.GNeedsHuman)) + " " + pal.ink(q)
	right := ""
	if a.factory.Has("answer") {
		right = pal.dim(factoryAnswerKeys)
	}
	if right != "" && ansi.StringWidth(left)+factoryGutter+ansi.StringWidth(right) <= measure {
		return factorySpread(left, right, measure)
	}
	return fit(left, measure)
}

// factoryPhaseNoteLine is the one dim line under the peek's strip about the
// phase that stopped: the waiting phase's note, else the latest failed one's,
// as `review · review is not clean after 2 rounds`. A note that is only the
// stage's own ask (what the runner compiles a phase with) says nothing new and
// is not drawn.
func factoryPhaseNoteLine(it factory.Item, stages []factory.Stage) string {
	s := it.Stream
	if s == nil {
		return ""
	}
	// A STOPPED RUN SAYS IT WAS STOPPED, never the note its last phase
	// carried as it was stopped: `test · 9/12 · TestRelay failed` under a stop
	// read as a failure nobody found (factory_marks.go).
	if at := factoryStoppedAt(it); at >= 0 {
		return s.Phases[at].Name + rowSep + factoryStoppedWords
	}
	pick := -1
	for i, ph := range s.Phases {
		if ph.State == factory.PhaseWaiting {
			pick = i
			break
		}
		if ph.State == factory.PhaseFailed {
			pick = i
		}
	}
	if pick < 0 {
		return ""
	}
	ph := s.Phases[pick]
	note := strings.TrimSpace(ph.Note)
	if note == "" {
		return ""
	}
	if at := factory.StageIndex(stages, ph.Name); at >= 0 && strings.TrimSpace(stages[at].Ask) == note {
		return ""
	}
	return ph.Name + rowSep + note
}

// factoryRoundWords is a phase's round over its most, `2/2`, and "" when the
// stage runs one round: A ROUND COUNT IS DRAWN ONLY WHERE THERE CAN BE A
// SECOND. A round past the stage's most, which a person's `one more round`
// makes, raises the most with it, so the pair never reads `3/2`.
func factoryRoundWords(round, most int) string {
	if round <= 0 {
		return ""
	}
	most = max(most, round)
	if most <= 1 {
		return ""
	}
	return strconv.Itoa(round) + "/" + strconv.Itoa(most)
}

// ── launching the marked ────────────────────────────────────────────────────

// factoryLaunchAsk is `L`'s question while marks stand: which items it would
// launch, in the floor's order, and what their reads estimate in all.
type factoryLaunchAsk struct {
	ids []int
	usd float64
}

// factoryEstWord is an estimate as `L` says it, in whole dollars (`$7`), and
// cents only under a dollar; nothing for nothing.
func factoryEstWord(usd float64) string {
	if usd <= 0 {
		return ""
	}
	if usd < 1 {
		return dollars(usd)
	}
	return "$" + strconv.Itoa(int(math.Round(usd)))
}

// factoryOpenLaunch puts `L`'s question up for the marked items.
func (a *app) factoryOpenLaunch(ids []int) {
	var usd float64
	for _, id := range ids {
		if it, ok := a.factoryItemByID(id); ok {
			usd += it.Triage.Est
		}
	}
	a.pageMsg = ""
	a.fp.act.launch = &factoryLaunchAsk{ids: ids, usd: usd}
	a.touch()
}

// factoryLaunchKey is a key while `L`'s question stands: `y` launches every
// marked item and spends the marks, `n` and `esc` put the question away, and
// every other key is the question's and does nothing.
func (a *app) factoryLaunchKey(k string) tea.Cmd {
	q := a.fp.act.launch
	switch k {
	case "y":
		a.fp.act.launch = nil
		a.touch()
		ids := q.ids
		// THE MARKS ARE SPENT BY THE LAUNCH: an item that is now a stream has
		// nothing left for a mark to mean.
		for _, m := range ids {
			delete(a.fp.marked, m)
		}
		return a.factoryDo(func(s factory.Seam) error {
			for _, m := range ids {
				if err := s.Launch(m); err != nil {
					return err
				}
			}
			return nil
		}, func(err error) {
			if err == nil {
				a.factorySay("launched " + itoa(len(ids)))
			}
		})
	case "n", "esc":
		a.fp.act.launch = nil
		a.touch()
	}
	return nil
}

// factoryLaunchRow is `L`'s question as the one row it is drawn on:
// `launch 3 marked · ~$7? [y] go · [n] not now`.
func (a *app) factoryLaunchRow(measure int) string {
	q := a.fp.act.launch
	if q == nil || measure <= 0 {
		return ""
	}
	pal := a.pal
	words := "launch " + itoa(len(q.ids)) + " marked"
	if est := factoryEstWord(q.usd); est != "" {
		words += rowSep + "~" + est
	}
	return fit(pal.ink(words+"? ")+pal.accent("[y] go")+pal.dim(" · [n] not now"), measure)
}
