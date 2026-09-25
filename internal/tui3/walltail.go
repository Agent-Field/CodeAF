package tui3

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/fuzzy"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE WALL'S READING: WHAT EACH OPEN CONVERSATION IS SAYING ───────────────
//
// This file turns what this process holds into tiles (wallcontract.go names
// the shapes). It has two halves and the line between them is the frame law.
//
// THE READ IS OFF THE LOOP. [session.Agent.Transcript] takes the agent's mutex
// and shapes every message it holds, and over an engine handle it crosses a
// wire. So it is only ever called inside a command ([app.wallReadCmd]), and the
// entries come back as a message the loop folds into the cache
// ([app.wallTakeRead]). A turn holding the lock stalls a goroutine, never a
// keystroke.
//
// THE TILES ARE READ OFF THE CACHE. [app.wallTiles] is called by the frame, so
// it reads the strip's own list, the cache and [app.tabSignalFor], and nothing
// else: no file, no wire, no mutex (framedisk_law_test.go).
//
// WHAT WAKES A READING is what already wakes the surface. A held conversation's
// stir ([behindStirMsg]) asks for its tile, throttled so a streaming answer is
// read four times a second rather than once per token, and a half-second tick
// reads the front and every tile that says it is working, because the
// conversation in front has no watcher to stir for it.
//
// OVER A SHARED ENGINE HANDLE ONLY THE FRONT IS LIVE ([Options.SharedAgent]).
// That handle holds one conversation at a time and the keeper holds nothing,
// so every other tile is the last reading this window took of it: a snapshot,
// drawn as one, never refreshed by pretending to read a conversation the engine
// has already let go of.

// wallStirEvery is the fastest one conversation's tile is read on a stir.
const wallStirEvery = 250 * time.Millisecond

// wallTickEvery is the wall's own clock while it is up.
const wallTickEvery = 500 * time.Millisecond

// wallSparkBytes is how many bytes of growth make one step of the sparkline.
// Forty is about a line of prose, so a steady stream reads mid-height and a
// tool dumping output reads full.
const wallSparkBytes = 40

// wallReadMsg is one conversation's transcript, read off the loop. at is when
// the read began, so a slow read that lands after a newer one is dropped.
type wallReadMsg struct {
	key     string
	entries []session.DisplayEntry
	at      time.Time
	live    bool
}

// wallTickMsg is the wall's clock.
type wallTickMsg struct{ at time.Time }

// wallTailFrom flattens a transcript into the tail a tile draws: logical lines,
// oldest first, at most [wallTailCap] of them, and the total text length the
// next reading measures growth against.
//
// IT WALKS FROM THE END and stops shaping once the cap is full, because a
// long conversation has thousands of entries and a tile shows forty lines.
// Only the length sum walks the whole list, and that is one add per entry.
func wallTailFrom(entries []session.DisplayEntry) (lines []wallLine, textLen int) {
	for i := range entries {
		textLen += len(entries[i].Text)
	}
	var chunks [][]wallLine
	have := 0
	for i := len(entries) - 1; i >= 0 && have < wallTailCap; i-- {
		chunk := wallEntryLines(entries[i])
		if len(chunk) == 0 {
			continue
		}
		chunks = append(chunks, chunk)
		have += len(chunk)
	}
	lines = make([]wallLine, 0, have)
	for i := len(chunks) - 1; i >= 0; i-- {
		lines = append(lines, chunks[i]...)
	}
	if len(lines) > wallTailCap {
		lines = lines[len(lines)-wallTailCap:]
	}
	return lines, textLen
}

// wallEntryLines is one entry as tail lines. A tool call is ONE line whatever
// it printed, because a tile is a glance and a glance wants what was done, not
// its output. Blank lines are dropped everywhere: a tile has a handful of rows
// and a blank one is a row spent saying nothing.
func wallEntryLines(e session.DisplayEntry) []wallLine {
	switch e.Role {
	case "assistant":
		var out []wallLine
		for _, raw := range strings.Split(e.Text, "\n") {
			if text := wallProseLine(raw); text != "" {
				out = append(out, wallLine{kind: wallProse, text: text})
			}
		}
		return out
	case "tool":
		what := strings.TrimSpace(e.Hint)
		if what == "" {
			what = wallFirstLine(e.Text)
		}
		name := strings.TrimSpace(e.Tool)
		if name == "" {
			name = "tool"
		}
		text := "▸ " + name
		if what != "" {
			text += " " + what
		}
		return []wallLine{{kind: wallTool, text: text}}
	case "user":
		var out []wallLine
		for _, raw := range strings.Split(e.Text, "\n") {
			text := strings.TrimSpace(raw)
			if text == "" {
				continue
			}
			lead := "  "
			if len(out) == 0 {
				lead = "› "
			}
			out = append(out, wallLine{kind: wallUser, text: lead + text})
		}
		return out
	case "note", "aside":
		// A team delivery is its lines and a team wake with nothing in it is
		// nothing, as the conversation draws them (teamcard.go's
		// [asideShapeOf]).
		if e.Role == "aside" {
			switch asideShapeOf(e) {
			case asideHidden:
				return nil
			case asideTeam:
				var out []wallLine
				for _, c := range teamCardsOf(e, teamManagerGlyph) {
					out = append(out, wallLine{kind: wallNote, text: c.from + " → " + c.to + "  " + wallFirstLine(c.text)})
				}
				return out
			}
		}
		// A compaction summary is pages long and a task's note a paragraph; the
		// tile says that one happened, in its first line.
		if text := wallFirstLine(e.Text); text != "" {
			return []wallLine{{kind: wallNote, text: text}}
		}
	}
	return nil
}

// wallProseLine takes the markdown's scaffolding off one line, lightly: a
// fence is dropped, a heading loses its hashes, emphasis and code ticks go.
// The painter has no markdown renderer and a tile drawn with raw `**` in it
// reads as noise.
func wallProseLine(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" || strings.HasPrefix(text, "```") || strings.HasPrefix(text, "~~~") {
		return ""
	}
	if strings.HasPrefix(text, "#") {
		text = strings.TrimSpace(strings.TrimLeft(text, "#"))
	}
	text = strings.NewReplacer("**", "", "__", "", "`", "").Replace(text)
	if strings.HasPrefix(text, "- ") || strings.HasPrefix(text, "* ") {
		text = "• " + text[2:]
	}
	return strings.TrimSpace(text)
}

// wallFirstLine is the first non-blank line of s, trimmed.
func wallFirstLine(s string) string {
	for _, raw := range strings.Split(s, "\n") {
		if text := strings.TrimSpace(raw); text != "" {
			return text
		}
	}
	return ""
}

// take folds one reading into the cache: the new tail, how many of its last
// lines are fresh, and one activity sample for the second it was read in.
//
// THE FIRST READING IS NOT ACTIVITY. Opening the wall on a conversation with a
// long history reads all of it at once, and a tile that lit every line and
// spiked its sparkline for that would be saying the history just happened.
//
// A SHRINKING TRANSCRIPT IS A NEW ONE (a rewind, a compaction), so it is taken
// as a first reading too: nothing fresh, nothing sampled.
func (t *wallTail) take(entries []session.DisplayEntry, now time.Time, live bool) {
	lines, textLen := wallTailFrom(entries)
	first := t.seen.IsZero() || len(entries) < t.count
	fresh, grew := 0, 0
	if !first && textLen > t.textLen {
		grew = textLen - t.textLen
	}
	if !first && (len(entries) > t.count || grew > 0) {
		prefix := 0
		for i := 0; i < t.count && i < len(entries); i++ {
			prefix += len(entries[i].Text)
		}
		if prefix > t.textLen {
			// The entry that was last grew: one fresh line, however it wrapped.
			fresh++
		}
		if t.count < len(entries) {
			for i := t.count; i < len(entries); i++ {
				fresh += len(wallEntryLines(entries[i]))
			}
		}
		if fresh > len(lines) {
			fresh = len(lines)
		}
	}
	if first || len(entries) != t.count || textLen != t.textLen {
		t.ver++
		from := max(len(entries)-wallRecentCap, 0)
		t.recent = append([]session.DisplayEntry(nil), entries[from:]...)
	}
	t.lines, t.count, t.textLen, t.seen, t.live = lines, len(entries), textLen, now, live
	if first {
		t.fresh = 0
		return
	}
	if fresh > 0 {
		t.fresh, t.freshAt = fresh, now
	}
	if grew > 0 || fresh > 0 {
		sample := (grew + wallSparkBytes - 1) / wallSparkBytes
		if sample < 1 {
			sample = 1
		}
		t.sample(now, sample)
	}
}

// sample adds activity to the ring slot for now's second, clearing every slot
// the ring skipped over since the last sample: a second nobody sampled was a
// second nothing happened, and it must read 0, not whatever was in that slot
// twenty-four seconds ago.
func (t *wallTail) sample(now time.Time, n int) {
	sec := now.Unix()
	if t.sparkAt.IsZero() {
		t.spark = [wallSparkLen]uint8{}
	} else {
		last := t.sparkAt.Unix()
		switch {
		case sec < last:
			// A reading from before the newest sample; the ring has moved on.
			return
		case sec-last >= wallSparkLen:
			t.spark = [wallSparkLen]uint8{}
		default:
			for s := last + 1; s <= sec; s++ {
				t.spark[s%wallSparkLen] = 0
			}
		}
	}
	t.sparkAt = time.Unix(sec, 0)
	slot := &t.spark[sec%wallSparkLen]
	if v := int(*slot) + n; v > 7 {
		*slot = 7
	} else {
		*slot = uint8(v)
	}
}

// sparkline is the ring as the painter draws it: oldest first, one sample per
// second ending at now's second, len [wallSparkLen]. Seconds after the newest
// sample, and seconds older than the ring, read 0.
func (t *wallTail) sparkline(now time.Time) []uint8 {
	out := make([]uint8, wallSparkLen)
	if t.sparkAt.IsZero() {
		return out
	}
	newest, end := t.sparkAt.Unix(), now.Unix()
	for i := range out {
		sec := end - int64(wallSparkLen-1-i)
		if sec > newest || sec <= newest-wallSparkLen || sec < 0 {
			continue
		}
		out[i] = t.spark[sec%wallSparkLen]
	}
	return out
}

// wallAge spells a duration the way a tile has room for: one number, one unit.
func wallAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

// wallAgentFor is the agent behind one tab's key, and whether a reading of it
// is live, or nil when this window holds nothing it can read for that key.
//
// THE FRONT IS a.agent AND A HELD ONE IS THE KEEPER'S. Over a shared handle
// the keeper is empty by construction ([app.stow]), so every other key answers
// nil here and its tile stays the snapshot it last was.
func (a *app) wallAgentFor(key string) (Agent, bool) {
	if key == "" {
		return nil, false
	}
	if key == a.frontTabKey() {
		if a.agent == nil {
			return nil, false
		}
		return a.agent, true
	}
	if a.shared {
		return nil, false
	}
	if held := a.behind[key]; held != nil && held.conv.Agent != nil {
		return held.conv.Agent, true
	}
	return nil, false
}

// wallReadCmd reads each key's transcript off the loop, one message per key.
// It is nil when no key resolves to an agent, so a caller can batch it blind.
func (a *app) wallReadCmd(keys ...string) tea.Cmd {
	var cmds []tea.Cmd
	for _, key := range keys {
		agent, live := a.wallAgentFor(key)
		if agent == nil {
			continue
		}
		key := key
		cmds = append(cmds, func() tea.Msg {
			at := time.Now()
			return wallReadMsg{key: key, entries: agent.Transcript(), at: at, live: live}
		})
	}
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

// wallTakeRead folds one reading into the cache, on the loop.
//
// A READING OLDER THAN THE CACHE IS DROPPED: two reads of one conversation can
// cross, and the tail must never step backwards. Over a shared handle a read
// that began before a swap names a key that is no longer in front, and the
// handle it read now holds a different conversation, so that one is dropped
// too rather than filed under the wrong tile.
func (a *app) wallTakeRead(msg wallReadMsg) {
	if msg.key == "" {
		return
	}
	if a.shared && msg.key != a.frontTabKey() {
		return
	}
	if a.wall.tails == nil {
		a.wall.tails = map[string]*wallTail{}
	}
	tail := a.wall.tails[msg.key]
	if tail == nil {
		tail = &wallTail{}
		a.wall.tails[msg.key] = tail
	}
	if !tail.seen.IsZero() && !msg.at.After(tail.seen) {
		return
	}
	tail.take(msg.entries, msg.at, msg.live)
}

// wallStir is a held conversation's stir, as the wall hears it: a read of that
// one tile, at most once per [wallStirEvery]. A stir inside the window is not
// lost work: the tick reads every working tile anyway.
func (a *app) wallStir(key string) tea.Cmd {
	if !a.wall.on {
		return nil
	}
	if tail := a.wall.tails[key]; tail != nil && time.Since(tail.seen) < wallStirEvery {
		return nil
	}
	return a.wallReadCmd(key)
}

// wallTick is the wall's clock: read the front and every tile whose signal
// says it is working or waiting, and come round again. It is nil when the wall
// is down, which is what stops the clock.
func (a *app) wallTick() tea.Cmd {
	if !a.wall.on {
		a.wall.ticking = false
		return nil
	}
	a.wall.ticking = true
	// ONLY WHAT MOVED IS READ. The front conversation's transcript is asked for
	// when the surface's own entries say it changed ([app.wallFrontMoved]); a
	// held one is read on its stir ([app.wallStir]), and here only when its tile
	// has never been read. A whole transcript every half second, per working
	// tile, was the wall's cost with nothing on it moving.
	front := a.frontTabKey()
	var keys []string
	if a.wall.tails[front] == nil || a.wallFrontMoved() {
		keys = append(keys, front)
	}
	for _, tab := range a.tabList() {
		if tab.start || tab.work || tab.key == front || a.wall.tails[tab.key] != nil {
			continue
		}
		if a.tabSignalFor(tab.key, tab.here) != tabIdle {
			keys = append(keys, tab.key)
		}
	}
	next := tea.Tick(wallTickEvery, func(at time.Time) tea.Msg { return wallTickMsg{at: at} })
	// A tile that started working since the paint clock last stopped wants it
	// turning again for its spinner ([app.wallSpinning]); nil when it is.
	if a.wallSpinning() {
		next = tea.Batch(next, a.wake())
	}
	if read := a.wallReadCmd(keys...); read != nil {
		return tea.Batch(read, next)
	}
	return next
}

// wallFrontVer is a cheap reading of the conversation in front: how many
// entries the surface holds, how long the newest one is, and whether a turn is
// running. Two equal readings are a transcript nobody wrote to.
type wallFrontVer struct {
	file        string
	n, last     int
	working, ok bool
}

// wallFrontMoved reports whether the front has moved since the wall last read
// it, and takes the new reading. Memory only.
func (a *app) wallFrontMoved() bool {
	ver := wallFrontVer{file: a.file, n: len(a.entries), working: a.state == stateWorking, ok: true}
	if ver.n > 0 {
		ver.last = len(a.entries[ver.n-1].text)
	}
	if ver == a.wall.frontVer {
		return false
	}
	a.wall.frontVer = ver
	return true
}

// wallTiles is the wall as the painter draws it, in the strip's order.
//
// IT IS CALLED BY THE FRAME and reads only memory: the strip's list, the
// cache, the signal and the wall's own state. A tab never read yet is a tile
// with no lines, which the painter draws as an empty tile rather than a lie.
func (a *app) wallTiles(now time.Time) []wallTile {
	tabs := a.tabList()
	var terms []fuzzy.Term
	if strings.TrimSpace(a.wall.filter) != "" {
		terms = fuzzy.Terms(a.wall.filter)
	}
	tiles := make([]wallTile, 0, len(tabs))
	for _, tab := range tabs {
		if tab.start || tab.work {
			continue
		}
		if len(terms) > 0 {
			if _, ok := fuzzy.ScoreFields([]string{tab.word, tab.full}, terms); !ok {
				continue
			}
		}
		_, live := a.wallAgentFor(tab.key)
		tile := wallTile{
			tab:    tab,
			name:   tab.word,
			here:   tab.here,
			signal: a.tabSignalFor(tab.key, tab.here),
			live:   live,
			marked: a.wall.marked[tab.key],
		}
		tail := a.wall.tails[tab.key]
		if tail != nil {
			tile.seen = tail.seen
			tile.lines = tail.lines
			tile.fresh = tail.fresh
			tile.freshAt = tail.freshAt
			if !tail.freshAt.IsZero() {
				tile.age = wallAge(now.Sub(tail.freshAt))
			}
			if live {
				tile.spark = tail.sparkline(now)
			}
		}
		if tile.signal == tabNeedsPerson {
			tile.question = a.wallQuestion(tab, tail)
		}
		tiles = append(tiles, tile)
	}
	return tiles
}

// wallQuestion is the one-line ask on a tile that needs its person, from what
// is already in memory. The front has its open questions on the surface; a
// held one has only its tail, and a last line that asks something is the best
// honest guess. Anything less than that says the word the strip says.
func (a *app) wallQuestion(tab chatTab, tail *wallTail) string {
	if tab.here {
		for i := range a.questions {
			if head := strings.TrimSpace(a.questions[i].question.Head); head != "" {
				return head
			}
		}
		if a.task != nil && strings.TrimSpace(a.task.title) != "" {
			return strings.TrimSpace(a.task.title)
		}
	}
	if tail != nil {
		for i := len(tail.lines) - 1; i >= 0; i-- {
			line := tail.lines[i]
			if line.kind != wallProse {
				continue
			}
			if strings.HasSuffix(line.text, "?") {
				return line.text
			}
			break
		}
	}
	return tabNeedsPersonWord
}
