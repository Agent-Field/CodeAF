package tui3

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/tui2/prose"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// SOURCE INSIDE A TOOL EXPANSION.
//
// Two blocks on this surface are a file rather than a result: the content a
// `write` lays down, and the text a `read` handed back. Both were drawn by
// [app.plainRows] — every row one flat dim — which is right for a build log and
// wrong for eighty lines of Go, where the shape a reader is looking for is
// carried by exactly the things a lexer knows about.
//
// THE HIGHLIGHTER IS NOT HERE, for the reason internal/tui3's markdown.go
// already gives about fences: internal/tui2/prose owns chroma and the one chroma
// style built from the tokens ramp, and a second copy of that two-hundred-row
// table is a table that drifts. This file is the two things prose leaves to a
// caller — which lexer, and how loud.
//
// HOW LOUD IS THE WHOLE DESIGN. [prose.HighlightLine] paints a run the lexer
// named at its slot on the ramp and everything else at the tier the caller
// states, so a block lights up only where there is meaning and keeps the
// expansion's own quiet voice everywhere else. The tier here is
// [tokens.TextTertiary] — the ramp's dim end, the tier this surface's own
// telemetry sits at — so an opened write reads as evidence somebody may skim,
// not as an editor window somebody opened. No new colour table is invented and
// none could be: the palette is the token layer's.

// codeTier is the grey a highlighted block's unlexed text sits at. It is the
// dim end of the tokens ramp, chosen to sit where [palette.dim] sits, so a
// highlighted expansion and a plain one read as the same kind of block.
const codeTier = tokens.TextTertiary

// codeRows is [app.plainRows] for text that is SOURCE: one row per line,
// truncated to width and never wrapped, lexed as the language of path and
// painted at [codeTier].
//
// It degrades to [app.plainRows] in three cases, and every one of them is the
// same judgement — that nothing here would be told apart by colour:
//
//   - the linear tier, where the surface is being read aloud rather than looked
//     at, and a keyword announces itself no differently from an identifier
//   - a path chroma's registry claims no lexer for, where the alternative is
//     chroma's fallback lexer painting a log file as though it were code
//   - a path that arrived empty, which is a call whose payload never carried one
//
// Lines are lexed ONE AT A TIME, which is what lets each be fitted to the width
// before it is painted — measuring a string through escape sequences measures it
// wrong. The cost is a construct that spans rows: a block comment or a raw
// string literal is coloured by what each of its lines looks like alone. That is
// the same bargain internal/tui2's record rows make, and it is the only one
// available to a preview whose first line has not arrived yet.
func (a *app) codeRows(text, path string, width int) []string {
	return a.codeRowsWith(a.styler(), text, path, width)
}

// codeRowsWith is [app.codeRows] against a stated Styler, split off for
// markdown.go's reason: the profile is detected from the environment exactly
// once per process, so a test that wants to see what a truecolor terminal gets
// cannot ask for one afterwards.
func (a *app) codeRowsWith(st *tokens.Styler, text, path string, width int) []string {
	lang := codeLang(a.pal, path)
	if lang == "" {
		return a.plainRows(text, width)
	}
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	// THE ONE CACHE ON THIS PATH, and it is not optional. Lexing costs about
	// sixty microseconds a row, which is nothing for the twenty rows of a write
	// and forty milliseconds for the eight hundred a person gets after clicking
	// "… N more lines" on a large read. Forty milliseconds is past the whole
	// frame budget.
	//
	// The inline block a tool row hangs has a memo in front of this one now
	// (toolview.go's [toolBlock]), so the ordinary transcript asks for a given
	// block once rather than thirty times a second. This still runs per frame for
	// everything that reaches the renderer another way — the phone's full-frame
	// sheet redraws from [app.detailBody] on every tick (expand.go), a forming
	// write is re-lexed as its own body arrives — and it is what keeps the memo's
	// misses cheap.
	//
	// THE KEY IS EVERYTHING THAT DECIDES A ROW. The text, the width and the
	// language are the obvious three; the Styler and the palette's own dim hue are
	// the two that would otherwise go stale, because a person switching theme
	// changes both and neither changes a byte of the text.
	key := fmt.Sprintf("%p|%v|%d|%s\x00%s", st, a.pal.ramp.dim, width, lang, text)
	if rows, known := a.codeCache.get(key); known {
		return rows
	}

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		fitted := fit(expandTabs(line), width)
		if strings.TrimSpace(fitted) == "" {
			// A blank line is still a row — an expansion that dropped them would
			// close the gaps a person reads the file's structure by — and it is a
			// row nothing can be highlighted in.
			out = append(out, a.pal.dim(fitted))
			continue
		}
		out = append(out, prose.HighlightLine(st, fitted, lang, codeTier))
	}
	a.codeCache.put(key, out)
	return out
}

// codeCacheBlocks is how many highlighted blocks are remembered. Two is the
// realistic screen — an open call and the one above it — and four leaves room
// for a person walking down a turn opening rows without the cache thrashing,
// while bounding what is held to a handful of expansions' worth of painted rows.
const codeCacheBlocks = 4

// codeCache remembers the painted rows of a block that has already been lexed,
// keyed by everything that decides them: the language, the width and the text.
//
// It is keyed by the WHOLE text rather than by a call's identity because that is
// the only key that cannot go stale — a block whose text changed is a different
// key and simply misses, so there is no invalidation to get wrong. Hashing forty
// kilobytes costs a couple of microseconds against the forty milliseconds it
// saves.
//
// It is mutex-guarded for internal/tui2/prose's reason about its own tables: the
// surface renders from one goroutine today and has no business being unable to
// render from two. It hangs off the surface rather than off the package so that
// two surfaces in one process — which the tests are — cannot read each other's
// paint.
type codeBlockCache struct {
	mu   sync.Mutex
	rows map[string][]string
	// order is the keys in the order they were added, oldest first. The eviction
	// is first-in-first-out rather than least-recently-used: the thing being
	// evicted is a block that has scrolled off, and telling the two apart would
	// mean writing to the map on every read.
	order []string
}

func (c *codeBlockCache) get(key string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, known := c.rows[key]
	return rows, known
}

func (c *codeBlockCache) put(key string, rows []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rows == nil {
		c.rows = make(map[string][]string, codeCacheBlocks)
	}
	if _, known := c.rows[key]; !known {
		c.order = append(c.order, key)
	}
	c.rows[key] = rows
	for len(c.order) > codeCacheBlocks {
		delete(c.rows, c.order[0])
		c.order = c.order[1:]
	}
}

// drop forgets every block this cache is holding.
//
// The key is the language, the width and the text, which is everything that
// decides the ROWS — and, until this wave, everything that could change. A
// palette that changed under a cached block is the one thing the key cannot see:
// the rows are finished strings with escape sequences already inside them, so a
// hit after a re-coloured ladder would hand back yesterday's paint. The measured
// background is the only thing that does this and it does it once, so the answer
// is to drop the lot rather than to widen a key that is hashed on every read
// (adaptive.go's [app.repaintPalette]).
func (c *codeBlockCache) drop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rows, c.order = nil, nil
}

// codeLang is the lexer a path's contents should be read as, or "" for a block
// this surface will draw flat.
func codeLang(pal palette, path string) string {
	// THE LINEAR TIER STAYS PLAIN. Syntax colour is the one paint on this surface
	// whose whole content is "this word is a keyword" — a claim carried by hue
	// alone, which a screen reader does not read and a person listening does not
	// receive. Everything else the palette does survives linear mode because it
	// costs nothing to keep; this costs a lexing pass per row to say nothing.
	if pal.linear {
		return ""
	}
	path = strings.TrimSpace(firstLine(path))
	if path == "" {
		return ""
	}
	return prose.LexerName(filepath.Base(path))
}
