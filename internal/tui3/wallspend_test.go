package tui3

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// heldKeyOf is the key this window holds agent behind the front under.
func heldKeyOf(t *testing.T, a *app, agent Agent) string {
	t.Helper()
	for key, held := range a.behind {
		if held != nil && held.conv.Agent == agent {
			return key
		}
	}
	t.Fatal("the agent is not held")
	return ""
}

// wallTileRowsFor is tile key's rows in the wall's frame, plain.
func wallTileRowsFor(t *testing.T, a *app, key string) string {
	t.Helper()
	frame := a.wallFrame(a.width, a.height)
	for i, tile := range a.wallShown(a.now()) {
		if tile.tab.key != key {
			continue
		}
		// A tile is answered by several targets (its border, its body, its
		// controls); its rectangle is all of them together.
		x0, y0, x1, y1 := -1, -1, -1, -1
		for _, hit := range a.wall.hits {
			if hit.arg != i || (hit.kind != wallHitTile && hit.kind != wallHitOpen) {
				continue
			}
			if x0 < 0 || hit.x0 < x0 {
				x0 = hit.x0
			}
			if y0 < 0 || hit.y0 < y0 {
				y0 = hit.y0
			}
			x1, y1 = max(x1, hit.x1), max(y1, hit.y1)
		}
		if y0 >= 0 {
			var rows []string
			for y := y0; y < y1 && y < len(frame); y++ {
				rows = append(rows, ansi.Strip(ansi.Cut(frame[y], x0, x1)))
			}
			return strings.Join(rows, "\n")
		}
	}
	t.Fatalf("no tile for %q on the wall", key)
	return ""
}

// Contract 1.1 and 1.2: the tile in front says what the conversation spent,
// the status line's very figure and spelling.
func TestTheFrontTileSaysTheStatusLinesSpend(t *testing.T) {
	a, _, _ := tabApp(t)
	a.take(session.Usage{CostUSD: 0.0042})
	_ = a.openWall()
	a.wall.revealAt = time.Time{} // the opening's motion, which any key cuts short
	want := dollars(a.spendShown())
	if got := wallTileRowsFor(t, a, a.frontTabKey()); !strings.Contains(got, want) {
		t.Fatalf("the front tile does not say %s:\n%s", want, got)
	}
}

// Contract 1.3: a held conversation's tile says what its own books say, read
// off the loop with its transcript.
func TestAHeldTileSaysWhatItsConversationSpent(t *testing.T) {
	a, older, _ := tabApp(t)
	older.usage = session.Usage{CostUSD: 1.2}
	key := heldKeyOf(t, a, older)
	_ = a.openWall()
	drive(t, a, runCmd(a.wallReadCmd(key))...)
	if got := wallTileRowsFor(t, a, key); !strings.Contains(got, "$1.20") {
		t.Fatalf("the held tile does not say $1.20:\n%s", got)
	}
}

// Contract 1.4: a conversation that spent nothing draws no money at all.
func TestATileThatSpentNothingDrawsNoMoney(t *testing.T) {
	a, older, _ := tabApp(t)
	key := heldKeyOf(t, a, older)
	_ = a.openWall()
	drive(t, a, runCmd(a.wallReadCmd(key))...)
	for _, k := range []string{key, a.frontTabKey()} {
		if got := wallTileRowsFor(t, a, k); strings.Contains(got, "$") {
			t.Fatalf("a tile that spent nothing drew money:\n%s", got)
		}
	}
}

// Contract 1.5 and 1.6: the state words win. The figure sits at the right end
// of the state line with room before it, is dropped whole when it does not fit,
// and stands alone on an idle tile.
func TestTheSpendGivesWayToTheStateWords(t *testing.T) {
	a, _, _ := tabApp(t)
	now := time.Now()
	v := wallView{now: now, spin: 0}
	g := wallGlyphsFor(a.pal.ascii)
	working := wallTile{signal: tabWorking, live: true, doing: "running bash", moved: now.Add(-2 * time.Minute), spent: 0.0042}
	wide := ansi.Strip(wallMetaLine(a.pal, g, v, working, 40))
	if !strings.HasSuffix(wide, "$0.0042") || !strings.Contains(wide, "running bash") || !strings.Contains(wide, "  $0.0042") {
		t.Fatalf("a wide state line: %q", wide)
	}
	words := ansi.StringWidth(ansi.Strip(wallMetaLine(a.pal, g, v, wallTile{signal: tabWorking, live: true, doing: "running bash", moved: now.Add(-2 * time.Minute)}, 40)))
	narrow := ansi.Strip(wallMetaLine(a.pal, g, v, working, words+3))
	if strings.Contains(narrow, "$") || !strings.Contains(narrow, "running bash") {
		t.Fatalf("a narrow state line cut the words for the money: %q", narrow)
	}
	idle := ansi.Strip(wallMetaLine(a.pal, g, v, wallTile{live: true, spent: 0.5}, 30))
	if strings.TrimSpace(idle) != "$0.50" || ansi.StringWidth(idle) > 30 {
		t.Fatalf("an idle tile's state line: %q", idle)
	}
}

// usageCounter is an agent whose books count how often they are asked.
type usageCounter struct {
	*fakeAgent
	asked *atomic.Int32
}

func (u usageCounter) Usage() session.Usage { u.asked.Add(1); return u.fakeAgent.Usage() }

// Contract 1.3: a frame never asks a conversation what it spent; only the read
// the wall makes off the loop does.
func TestAWallFrameNeverAsksWhatAConversationSpent(t *testing.T) {
	a, older, _ := tabApp(t)
	key := heldKeyOf(t, a, older)
	var asked atomic.Int32
	a.behind[key].conv.Agent = usageCounter{fakeAgent: older, asked: &asked}
	_ = a.openWall()
	drive(t, a, runCmd(a.wallReadCmd(key))...)
	before := asked.Load()
	for i := 0; i < 5; i++ {
		_ = a.wallFrame(a.width, a.height)
		_ = a.wallShown(a.now())
	}
	if asked.Load() != before {
		t.Fatalf("a frame asked the books %d times", asked.Load()-before)
	}
}

// Contract 1.3: a held conversation whose work has spent more than its books
// know shows the ledger's figure, as its status line would, read off the loop.
func TestAHeldTileSaysWhatItsWorkSpentOnTheLedger(t *testing.T) {
	a, older, _ := tabApp(t)
	older.usage = session.Usage{CostUSD: 0.5}
	key := heldKeyOf(t, a, older)
	const id = "aaaa1111bbbb2222"
	a.behind[key].conv.SessionFile = "/tmp/lab/" + id + "/transcript.jsonl"
	asked := 0
	a.ledger = func(time.Time) ([]session.UsageLine, bool, bool) {
		asked++
		return []session.UsageLine{
			{At: time.Now(), Session: id, Calls: 1, USD: 0.5},
			{At: time.Now(), Session: "task-node", Root: id, Calls: 1, USD: 1.75},
		}, true, true
	}
	// The opening asks for the transcripts and, once, for the ledger.
	open := a.openWall()
	for i := 0; i < 3; i++ {
		_ = a.wallFrame(a.width, a.height)
	}
	if asked != 0 {
		t.Fatalf("a frame read the ledger %d times", asked)
	}
	drive(t, a, runCmd(open)...)
	if asked != 1 {
		t.Fatalf("the opening read the ledger %d times", asked)
	}
	if got := wallTileRowsFor(t, a, key); !strings.Contains(got, "$2.25") {
		t.Fatalf("the held tile does not say its tree's $2.25:\n%s", got)
	}
}
