package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/prose"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── WHAT THE FORGE SAYS ABOUT AN ITEM ───────────────────────────────────────
//
// An item's words arrive as the forge holds them, which is Markdown: an issue
// body with a heading, a list of claims, a path in backticks, a fenced test
// output. THEY ARE RENDERED, NEVER SHOWN RAW, through the one renderer the
// transcript uses (internal/tui2/prose), wrapped at the peek's sixty cells and
// the item page's seventy-two. A link is drawn as its words, because the
// item's own page is offered once, as `github ↗` and `g`, rather than in every
// sentence.
//
// AFTER THE BODY come the forge's blocks, in this order and EACH ABSENT WHEN
// IT HAS NOTHING TO SAY (the emptiness law):
//
//	comments   the last three: `author · 2h` dim, then the words, rendered
//	checks     one row per check run: its mark, its name, its state
//	files      a pull request's `+N −M`, then up to six paths with theirs, dim
//	activity   one dim line per event, newest last, at most five
//
// Each block is headed by its own word, dim, so a block a person did not
// expect still says what it is. The peek and the item page's issue pane draw
// the same blocks; the page shows each comment whole.

// factoryMarkdown is text rendered as Markdown at w cells, painted, with a run
// of blank rows kept as one and none at either end: A GAP ASKED FOR TWICE IS
// STILL ONE GAP.
//
// IT IS RENDERED ONCE PER TEXT, WIDTH AND PAINT, not once per frame (owner's
// floor, 2026-10-09): the peek and the issue pane drew the body and every
// comment through the Markdown parser on every pointer step, a quarter of the
// item page's frame. The rendered rows are kept by what they were rendered
// from ([factoryMDCache]) and handed out as a copy, because a caller may cut
// a row of them in place.
func (a *app) factoryMarkdown(text string, w int) []string {
	if strings.TrimSpace(text) == "" || w <= 0 {
		return nil
	}
	// THE PLAIN FLOOR IS UNPAINTED HERE TOO: a palette with no colour hands
	// prose no styler, so the body says no more than the rows around it.
	st := a.styler()
	if a.pal.profile == tokens.NoColor {
		st = nil
	}
	key := factoryMDKey{text: text, w: w, styler: st}
	if rows, ok := a.fp.md[key]; ok {
		return append([]string(nil), rows...)
	}
	out := factoryMarkdownRows(text, w, st)
	if a.fp.md == nil || len(a.fp.md) >= factoryMDCache {
		a.fp.md = make(map[factoryMDKey][]string, factoryMDCache)
	}
	a.fp.md[key] = out
	return append([]string(nil), out...)
}

// factoryMDCache is how many rendered texts the page keeps: a floor's worth
// of peeks a pointer sweeps through, each with its comments. Past it the
// whole cache is dropped and refilled, which costs one frame's renders.
const factoryMDCache = 256

// factoryMDKey is what a rendered text was rendered from.
type factoryMDKey struct {
	text   string
	w      int
	styler *tokens.Styler
}

// factoryMarkdownRows is [app.factoryMarkdown]'s render, uncached.
func factoryMarkdownRows(text string, w int, st *tokens.Styler) []string {
	rows := prose.Render(text, prose.Options{Width: w, Measure: w, Styler: st, LinksAsText: true})
	out := make([]string, 0, len(rows))
	blank := true
	for _, r := range rows {
		if strings.TrimSpace(ansi.Strip(r)) == "" {
			if !blank {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, r)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// factoryForgeBlocks is the forge's blocks for an item at w cells, in their
// order, each one block for [factoryStack]. commentRows is the most rows one
// comment's words may take, 0 for all of them.
func (a *app) factoryForgeBlocks(it factory.Item, w, commentRows int) [][]string {
	return [][]string{
		a.factoryCommentsBlock(it, w, commentRows),
		a.factoryChecksBlock(it, w),
		a.factoryFilesBlock(it, w),
		a.factoryActivityBlock(it, w),
	}
}

// factoryBlockHead is a block's word, dim: what the rows under it are.
func (a *app) factoryBlockHead(word string, w int) string { return a.pal.dim(fit(word, w)) }

// factoryCommentsBlock is the last [factoryCommentsShown] comments, oldest of
// them first: who and how long ago, dim, then the words rendered, cut to
// rows with an ellipsis when rows is not 0.
//
// A GITHUB ITEM WHOSE COMMENTS ARE NOT READ YET SAYS SO, dim, rather than
// draw as an item nobody said anything on: a busy repository's first read
// folds its items first and reads their comments over the next minutes.
func (a *app) factoryCommentsBlock(it factory.Item, w, rows int) []string {
	cs := it.Comments
	if cs == nil && it.Origin == factory.OriginForge && it.Num > 0 && w > 0 {
		return []string{a.pal.dim(fit("comments not read yet", w))}
	}
	if len(cs) == 0 || w <= 0 {
		return nil
	}
	if len(cs) > factoryCommentsShown {
		cs = cs[len(cs)-factoryCommentsShown:]
	}
	out := []string{a.factoryBlockHead("comments", w)}
	for i, c := range cs {
		words := a.factoryMarkdown(c.Body, w)
		if len(words) == 0 {
			continue
		}
		if i > 0 {
			out = append(out, "")
		}
		who := factoryOr(strings.TrimSpace(c.Author), "someone")
		if !c.At.IsZero() && !a.fp.snap.Now.IsZero() {
			who += rowSep + factoryAgo(a.fp.snap.Now.Sub(c.At))
		}
		out = append(out, a.pal.dim(fit(who, w)))
		if rows > 0 && len(words) > rows {
			words = words[:rows]
			last := fit(words[rows-1], max(w-factoryLeadW, 1))
			words[rows-1] = last + " " + a.pal.ink(a.icon(tokens.GEllipsis))
		}
		out = append(out, words...)
	}
	if len(out) == 1 {
		return nil
	}
	return out
}

// factoryChecksBlock is one row per check run: its mark by how it went, its
// name in ink, and its state dim at the right.
func (a *app) factoryChecksBlock(it factory.Item, w int) []string {
	if len(it.CheckRuns) == 0 || w <= 0 {
		return nil
	}
	pal := a.pal
	out := []string{a.factoryBlockHead("checks", w)}
	for _, c := range it.CheckRuns {
		mark, paint := a.factoryCheckMark(c.State)
		left := paint(mark) + " " + pal.ink(factoryOr(c.Name, "check"))
		if c.URL != "" && a.pathLinks {
			left = paint(mark) + " " + linkify(pal.ink(factoryOr(c.Name, "check")), c.URL)
		}
		out = append(out, factorySpread(left, pal.dim(strings.TrimSpace(c.State)), w))
	}
	return out
}

// factoryCheckMark is a check run's state as the vocabulary's mark and its
// paint: passed is the settled check, failed the cross, still going the
// working mark, and anything else (skipped, neutral) the pending dot, dim.
func (a *app) factoryCheckMark(state string) (string, func(string) string) {
	pal := a.pal
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "success", "passed", "pass", "ok":
		return a.icon(tokens.GSettled), pal.add
	case "failure", "failed", "fail", "error", "cancelled", "timed_out", "action_required":
		return a.icon(tokens.GFailed), pal.bad
	case "pending", "queued", "in_progress", "running", "waiting", "requested":
		return a.icon(tokens.GWorking), pal.accent
	}
	return a.icon(tokens.GStepPending), pal.dim
}

// factoryFilesBlock is a pull request's files: the whole change as `+N −M`,
// then up to [factoryFilesShown] paths with their own counts, dim, and how many
// more when there are.
func (a *app) factoryFilesBlock(it factory.Item, w int) []string {
	if len(it.Files) == 0 || w <= 0 {
		return nil
	}
	pal := a.pal
	added, removed := 0, 0
	for _, f := range it.Files {
		added += f.Added
		removed += f.Removed
	}
	out := []string{a.factoryBlockHead("files", w)}
	if total := a.factoryDiffWords(added, removed); total != "" {
		out = append(out, fit(pal.ink(total)+pal.dim(rowSep+itoa(len(it.Files))+" "+factoryPlural(len(it.Files), "file", "files")), w))
	}
	shown := it.Files
	if len(shown) > factoryFilesShown {
		shown = shown[:factoryFilesShown]
	}
	for _, f := range shown {
		out = append(out, factorySpread(pal.dim(f.Path), pal.dim(a.factoryDiffWords(f.Added, f.Removed)), w))
	}
	if more := len(it.Files) - len(shown); more > 0 {
		out = append(out, pal.dim(fit(itoa(more)+" more", w)))
	}
	return out
}

// factoryDiffWords is lines added and removed, `+12 −3`, each said only when it
// is not zero.
func (a *app) factoryDiffWords(added, removed int) string {
	var parts []string
	if added > 0 {
		parts = append(parts, a.icon(tokens.GDiffAdd)+itoa(added))
	}
	if removed > 0 {
		parts = append(parts, a.icon(tokens.GDiffDel)+itoa(removed))
	}
	return strings.Join(parts, " ")
}

// factoryActivityBlock is the last [factoryEventsShown] things that happened to
// the item, newest last, each one dim line led by how long ago.
func (a *app) factoryActivityBlock(it factory.Item, w int) []string {
	ev := it.Activity
	if len(ev) == 0 || w <= 0 {
		return nil
	}
	if len(ev) > factoryEventsShown {
		ev = ev[len(ev)-factoryEventsShown:]
	}
	out := []string{a.factoryBlockHead("activity", w)}
	for _, e := range ev {
		what := strings.TrimSpace(e.What)
		if what == "" {
			continue
		}
		if !e.At.IsZero() && !a.fp.snap.Now.IsZero() {
			ago := factoryAgo(a.fp.snap.Now.Sub(e.At))
			what = ago + factorySpaces(max(factoryAgeW-ansi.StringWidth(ago), 0)+1) + what
		}
		out = append(out, a.pal.dim(fit(what, w)))
	}
	if len(out) == 1 {
		return nil
	}
	return out
}
