package session

// Bitmap frames: the middle rung of the compaction ladder.
//
// The ladder has three rungs and each one trades a different thing away.
// Rung 1 is the STUB (stub.go): one heavy tool result becomes a pointer to its
// own bytes, nothing is summarized, and the model pays a read when the bytes
// turn out to matter. Rung 3 is the SUMMARY (loop.go): the whole discarded
// prefix becomes six sections of prose written by the model itself — cheap to
// carry, lossy by construction, and the loss is unrecoverable because the
// transcript it was written from is gone.
//
// This is rung 2, and it is what a model that can SEE makes possible. The
// discarded prefix is not described, it is PHOTOGRAPHED: rendered to monospaced
// page images, deterministically, in this process, with no LLM call at all. The
// pages carry the transcript verbatim — every path, every error string, every
// argument the summarizer would have paraphrased — and a vision model reads them
// back the way a person reads a printout.
//
// ── WHY THIS IS NOT JUST A CHEAPER SUMMARY ──
//
// It is a different kind of preservation. A summary is a claim ABOUT the
// conversation and can be wrong; a page is the conversation. The pass also costs
// nothing but CPU: no round trip, no auxiliary spend, no model deciding what
// mattered, and no failure mode where the summarizer returns prose about a
// conversation it misread. What it costs instead is IMAGE TOKENS — flat and
// known ([imagePartTokens] × pages) — and that ceiling is what [framesMaxPages]
// is: eight pages is about what one summary weighs, so the rung is a swap of
// equal budget rather than an expansion of it.
//
// ── THE DECISION IS PER PASS, NEVER STICKY ──
//
// Frames are chosen when the session's model can read images and nobody asked
// for a focused compaction; anything else summarizes. A person who says
// `/compact keep the API decisions` is telling the summarizer what to be careful
// about, and a renderer cannot be careful about anything — that instruction has
// no meaning on this rung, so the request itself sends the pass back to rung 3.
// A later pass on a model with no vision summarizes normally: nothing here is
// remembered between passes, because the only thing that decides is what the
// context is about to be sent to.
//
// ── THE JOURNAL HOLDS REFERENCES, NOT PICTURES ──
//
// Every page is written to the workspace and journaled as a [journalPart] —
// path, digest, media type — which is the same contract a person's attached
// screenshot travels under (image.go). A resume re-reads the file only while the
// digest matches and puts a placeholder in its place otherwise, so a session
// whose frames were deleted says so instead of resuming as if it still had them.
// A session with no workspace to write to has nowhere to put a durable page, and
// falls back to the summary rather than putting bytes in the transcript that no
// resume could ever recover.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	// framesCols is the page width in characters. 120 is the width the code in
	// this repository is written to, so a rendered diff, a stack trace and a
	// table all land inside it and wrapping stays the exception.
	framesCols = 120

	// framesRows is the page height in lines. 64 with the 7×13 face puts a page
	// at 856×~900 pixels — inside the tile budget every vision endpoint bills in,
	// and about a screenful, which is the unit a reader of a printout expects.
	framesRows = 64

	// framesMaxPages is the ceiling, and it is a TOKEN ceiling wearing a page's
	// clothes: 8 × [imagePartTokens] is ~8k, which is what a compaction summary
	// of a long session weighs. A prefix that needs more than eight pages has its
	// overflow summarized (see [Agent.framesPass]) rather than buying itself a
	// bigger budget than the rung it replaced.
	//
	// It is the ceiling and not the answer: what a given session can actually
	// afford is [Agent.framePageBudget], which is this number or less.
	framesMaxPages = 8

	// framesDirName is where the pages live, under the workspace for the reason
	// the stubs are (stub.go): a person can find them after the session is over,
	// and a resume reaches them by the path the journal wrote.
	framesDirName = ".aforge-v3/frames"

	// framesNote is what the model reads in front of the pages. It says the same
	// three things [compactionNote] says — this is compacted context, neither of
	// us said it, questions inside it are still open — and one more that only
	// this rung can say: nothing was paraphrased.
	framesNote = "[context compacted] Everything before this point was rendered verbatim to page " +
		"images rather than summarized — the transcript is kept as images below, in order, and " +
		"nothing in it was shortened or rephrased. It is not something either of us said, and any " +
		"question inside it is still open."
)

// The page geometry. basicfont.Face7x13 is a fixed 7×13 bitmap face: every glyph
// advances exactly 7 pixels and sits on a 13-pixel line, which is what makes a
// page's layout arithmetic rather than measurement — and what makes two renders
// of the same text the same bytes.
const (
	framesAdvance    = 7
	framesLineHeight = 13
	framesAscent     = 11
	framesMargin     = 8
	// framesFooterGap is the space between the last line of text and the rule
	// above the footer.
	framesFooterGap = 8
)

// framePage is one rendered page on its way into the context: the bytes the
// model reads and the reference the journal writes in their place. The two are
// carried together for the reason [userMessage] carries its refs — a data URL
// has no provenance, and by the time the message reaches the journal there is
// nothing left to recover the path from.
type framePage struct {
	png []byte
	ref journalPart
}

// compactionPass is what one compaction produced, whichever rung ran it.
//
// Both fields can be set at once and that is the overflow case, not a
// contradiction: the pages hold as much of the discarded prefix as the page cap
// allows, and the summary holds whatever ran past it. They are appended to the
// rebuilt transcript in that order, which is the order the conversation happened
// in — pictures of the older part, prose about the newer part, then the verbatim
// tail nobody touched.
type compactionPass struct {
	summary string
	frames  []framePage
}

// empty reports whether the pass produced nothing to put in the context. A pass
// that is empty is a pass that failed, and the transcript is left alone.
func (p compactionPass) empty() bool {
	return len(p.frames) == 0 && strings.TrimSpace(p.summary) == ""
}

// refs are the journal's references for the pages, in page order.
func (p compactionPass) refs() []journalPart {
	if len(p.frames) == 0 {
		return nil
	}
	refs := make([]journalPart, 0, len(p.frames))
	for _, page := range p.frames {
		refs = append(refs, page.ref)
	}
	return refs
}

// message is the pages as one user-role message: the note, then a picture per
// page. User role because it is context handed TO the model, and because image
// parts ride a user message on every backend this surface routes through.
//
// The note comes first and the pictures follow it in order, which is the shape
// [imageUserMessage] builds and therefore the shape [replayedMessage] and
// [rememberParts] both assume: the references are the message's LAST parts.
func (p compactionPass) message() ai.Message {
	parts := make([]ai.ContentPart, 0, len(p.frames)+1)
	parts = append(parts, ai.ContentPart{Type: "text", Text: framesNote})
	for _, page := range p.frames {
		parts = append(parts, ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{
			URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(page.png),
		}})
	}
	return ai.Message{Role: "user", Content: parts}
}

// framesChosen is the rung selection, and it asks three questions in the order
// that makes the cheapest one first.
//
// A FOCUS SENDS THE PASS TO RUNG 3, unconditionally. The focus is an instruction
// to a summarizer about what must survive being paraphrased; a renderer
// paraphrases nothing and can honour no part of it. Running frames under a focus
// would be obeying the letter of the request — the words did survive — while
// ignoring the reason somebody typed it, which was to steer a lossy pass.
//
// A WORKSPACE IS REQUIRED because a page nobody can write to disk cannot be
// journaled by reference, and a transcript holding pictures no resume can
// recover is worse than a summary that resumes.
//
// VISION IS THE LAST QUESTION because it is the one that costs a call into the
// surface's catalog. Nil SupportsImages is "nobody can say", which is a no: this
// rung sends image parts to a model, and a build with no opinion about whether
// that model can read them must not find out from a 400.
func (a *Agent) framesChosen(ctx context.Context, model string) bool {
	if focus, ok := ctx.Value(compactFocusKey{}).(string); ok && strings.TrimSpace(focus) != "" {
		return false
	}
	if strings.TrimSpace(a.config.Workspace) == "" {
		return false
	}
	if a.framePageBudget() == 0 {
		return false
	}
	return a.config.SupportsImages != nil && a.config.SupportsImages(model)
}

// framePageBudget is how many pages THIS session can afford, and it is the
// answer to a question [framesMaxPages] alone cannot: pages are not free, and a
// pass that put its own images back over the threshold it just fired at would be
// compacted again on the next step — throwing away the pages it just rendered,
// because a picture cannot be re-rendered from a picture (see
// [flattenTranscript]). One wasteful pass, and the fidelity this rung exists for
// gone with it.
//
// The budget is three quarters of the room between the threshold and the
// verbatim tail. The threshold minus the tail is what a rebuilt context has to
// live inside; three quarters of it leaves the state block, the note and the
// estimator's own slack somewhere to sit. A window with no room at all answers
// zero and the pass summarizes, which is the honest reading of a small window:
// one page costs [imagePartTokens], and a model whose whole context is a few
// thousand tokens cannot spend a thousand of them on a photograph of six.
func (a *Agent) framePageBudget() int {
	headroom := a.compactThreshold() - a.keepRecentTokens()
	if headroom <= 0 {
		return 0
	}
	pages := headroom * 3 / 4 / imagePartTokens
	if pages > framesMaxPages {
		return framesMaxPages
	}
	return pages
}

// framesPass renders the discarded prefix to pages and summarizes whatever ran
// past the page cap. It reports ok=false when the rung could not run at all, and
// the caller then takes rung 3 for the whole prefix.
//
// The overflow summary is the pass's ONE model call and it is made only when
// there is overflow, which is the arithmetic this rung is for: a prefix that
// fits in eight pages costs nothing but CPU. When that call fails the whole pass
// fails — the alternative is a context that silently drops the part of the
// conversation the pages could not hold, which is the one outcome neither rung
// is allowed to produce.
func (a *Agent) framesPass(ctx context.Context, discarded []ai.Message, title string) (compactionPass, bool, error) {
	budget := a.framePageBudget()
	if budget == 0 {
		return compactionPass{}, false, nil
	}
	lines := frameLines(flattenTranscript(discarded))
	if len(lines) == 0 {
		return compactionPass{}, false, nil
	}

	kept, overflow := splitFrameLines(lines, budget)
	pages, err := renderFrames(title, kept, budget)
	if err != nil || len(pages) == 0 {
		return compactionPass{}, false, nil
	}

	workspace := strings.TrimSpace(a.config.Workspace)
	frames := make([]framePage, 0, len(pages))
	for _, data := range pages {
		ref, err := writeFrame(workspace, data)
		if err != nil {
			// A page that did not reach disk is a page no resume can read back,
			// and half a transcript is not a rung. Fall back whole.
			return compactionPass{}, false, nil
		}
		frames = append(frames, framePage{png: data, ref: ref})
	}

	pass := compactionPass{frames: frames}
	if overflow == "" {
		return pass, true, nil
	}
	summary, err := a.summarizeText(ctx, overflow)
	if err == nil && strings.TrimSpace(summary) == "" {
		err = errors.New("session: summarizer returned nothing for the frames overflow")
	}
	if err != nil {
		// The pages are on disk and cost nothing to leave there — they are named
		// by their own digest, so the next pass over the same prefix finds them
		// rather than writing them again. What must not happen is the transcript
		// keeping the pages while the overflow they could not hold disappears.
		return compactionPass{}, true, err
	}
	pass.summary = summary
	return pass, true, nil
}

// splitFrameLines cuts the rendered lines at the page cap: what the pages will
// hold, and the rest as text for the summarizer.
//
// The PAGES TAKE THE HEAD and the summary takes the tail, which reads backwards
// until the ordering is drawn out: the head of the discarded prefix is the OLDER
// half of it, and the newer half is the one that ends closest to the verbatim
// tail the compaction kept. The result is a context that degrades once, in the
// middle — pictures of the beginning, prose about the run-up, verbatim for the
// present — instead of twice.
func splitFrameLines(lines []string, pages int) ([]string, string) {
	capacity := pages * framesRows
	if len(lines) <= capacity {
		return lines, ""
	}
	return lines[:capacity], strings.Join(lines[capacity:], "\n")
}

// frameLines turns a flattened transcript into the exact lines a page draws:
// ANSI stripped, tabs expanded, everything the 7×13 face cannot draw
// transliterated, and hard-wrapped at [framesCols].
//
// It is the whole sanitization, in one pass, because the pages ARE the record on
// this rung: a line that silently lost its last thirty characters to the page
// edge would be a transcript that lost them.
func frameLines(transcript string) []string {
	transcript = strings.TrimRight(transcript, "\n")
	if strings.TrimSpace(transcript) == "" {
		return nil
	}
	var lines []string
	for _, raw := range strings.Split(transcript, "\n") {
		lines = append(lines, wrapFrameLine(frameText(raw))...)
	}
	return lines
}

// wrapFrameLine hard-wraps one sanitized line at the page width. Hard, not at
// word boundaries: the content is code, paths and error text as often as prose,
// and a wrap that moved a token to keep a sentence pretty would be a wrap that
// changed what a path looks like.
func wrapFrameLine(line string) []string {
	if line == "" {
		return []string{""}
	}
	var wrapped []string
	for len(line) > framesCols {
		wrapped = append(wrapped, line[:framesCols])
		line = line[framesCols:]
	}
	return append(wrapped, line)
}

// frameTransliterations are the characters this codebase actually writes that
// the 7×13 face has no glyph for. Each one is replaced by what it MEANS in
// ASCII rather than dropped: an em dash that became "?" reads as damage, and a
// vision model reading damage reports damage.
var frameTransliterations = map[rune]string{
	'—': "--", '–': "-", '―': "--", '·': "*", '•': "*", '…': "...",
	'“': `"`, '”': `"`, '„': `"`, '‘': "'", '’': "'", '«': `"`, '»': `"`,
	'→': "->", '←': "<-", '⇒': "=>", '↔': "<->", '×': "x", '÷': "/",
	'≥': ">=", '≤': "<=", '≠': "!=", '≈': "~=", '±': "+/-",
	'─': "-", '━': "-", '│': "|", '┃': "|", '├': "+", '┤': "+", '┬': "+",
	'┴': "+", '┌': "+", '┐': "+", '└': "+", '┘': "+", '┼': "+", '▸': ">",
	'✓': "[ok]", '✔': "[ok]", '✗': "[x]", '✘': "[x]", '⚠': "[!]",
	'°': "deg", '§': "S", '¶': "P", '©': "(c)", '™': "(tm)", '€': "EUR",
	'£': "GBP", '¢': "c", ' ': " ",
}

// frameText makes one line drawable: no escape sequences, no tabs, no runes the
// face cannot render.
//
// An unknown rune becomes "?" — one character, not the four a codepoint escape
// would cost — because the page is read by something looking for meaning, and a
// paragraph of ☃ is noise where a paragraph of ? is a paragraph with
// something illegible in it.
func frameText(line string) string {
	line = ansi.Strip(line)
	line = strings.ReplaceAll(line, "\t", "    ")
	line = strings.ReplaceAll(line, "\r", "")
	var out strings.Builder
	out.Grow(len(line))
	for _, r := range line {
		switch {
		case r >= 0x20 && r < 0x7f:
			out.WriteRune(r)
		default:
			if replacement, known := frameTransliterations[r]; known {
				out.WriteString(replacement)
				continue
			}
			out.WriteString("?")
		}
	}
	return strings.TrimRight(out.String(), " ")
}

// renderFrames draws every page of one transcript segment, up to the caller's
// page budget.
func renderFrames(title string, lines []string, budget int) ([][]byte, error) {
	if len(lines) == 0 || budget <= 0 {
		return nil, nil
	}
	total := (len(lines) + framesRows - 1) / framesRows
	if total > budget {
		total = budget
	}
	pages := make([][]byte, 0, total)
	for page := 0; page < total; page++ {
		start := page * framesRows
		end := start + framesRows
		if end > len(lines) {
			end = len(lines)
		}
		data, err := renderFramePage(title, lines[start:end], page+1, total)
		if err != nil {
			return nil, err
		}
		pages = append(pages, data)
	}
	return pages, nil
}

// renderFramePage draws one page and encodes it.
//
// It is DETERMINISTIC and that is a property, not an accident: the geometry is
// arithmetic on fixed constants, the face is a pinned bitmap, the image is
// 8-bit grey, and the PNG encoder writes no timestamp. The same lines render to
// the same bytes, which is what makes the page's digest a stable name on disk —
// a compaction that ran twice over the same prefix files one page, not two.
//
// Grey rather than colour because nothing here is coloured: the ANSI is stripped
// before a glyph is drawn, and a greyscale PNG of black text is a third of the
// bytes of the RGBA that would carry exactly the same picture.
func renderFramePage(title string, lines []string, page, pages int) ([]byte, error) {
	width := 2*framesMargin + framesCols*framesAdvance
	body := framesRows * framesLineHeight
	height := 2*framesMargin + body + framesFooterGap + 1 + framesFooterGap + framesLineHeight

	canvas := image.NewGray(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.Gray{Y: 0xff}), image.Point{}, draw.Src)

	drawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(color.Gray{Y: 0x00}),
		Face: basicfont.Face7x13,
	}
	for index, line := range lines {
		if index >= framesRows {
			break
		}
		drawer.Dot = fixed.P(framesMargin, framesMargin+framesAscent+index*framesLineHeight)
		drawer.DrawString(line)
	}

	// The rule and the footer. The footer is the page's own identity — which
	// conversation, which page, how many — and it is on every page because a
	// vision model reads pages as a set and has nothing else to order them by.
	ruleY := framesMargin + body + framesFooterGap
	draw.Draw(canvas,
		image.Rect(framesMargin, ruleY, width-framesMargin, ruleY+1),
		image.NewUniform(color.Gray{Y: 0x99}), image.Point{}, draw.Src)
	drawer.Src = image.NewUniform(color.Gray{Y: 0x55})
	drawer.Dot = fixed.P(framesMargin, ruleY+framesFooterGap+framesAscent)
	drawer.DrawString(frameFooter(title, page, pages))

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// frameFooter is the line under the rule: what this is, and where in it we are.
// An unnamed session says so rather than leaving the page anonymous — a
// conversation is often compacted before it has been titled (title.go).
func frameFooter(title string, page, pages int) string {
	title = frameText(strings.TrimSpace(title))
	if title == "" {
		title = "untitled session"
	}
	footer := fmt.Sprintf("%s | context page %d of %d", title, page, pages)
	return wrapFrameLine(footer)[0]
}

// writeFrame files one page and returns the reference the journal writes.
//
// The name is the page's own digest, so a re-render of the same prefix is one
// file rather than two and a file already on disk is left exactly as it is —
// the same idempotence [writeStub] has, for the same reason: these directories
// are written to for the length of a session and read from for longer.
func writeFrame(workspace string, data []byte) (journalPart, error) {
	digest := sha256.Sum256(data)
	full := hex.EncodeToString(digest[:])
	relative := filepath.Join(framesDirName, full[:16]+".png")
	path := filepath.Join(workspace, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return journalPart{}, err
	}
	if info, err := os.Stat(path); err != nil || info.Size() != int64(len(data)) {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return journalPart{}, err
		}
	}
	// The ABSOLUTE path, unlike a stub's relative one. A stub line is read by the
	// model with a workspace-rooted read tool; this path is read by a RESUME,
	// which opens the file itself from wherever the process happens to be
	// standing (see [journalPart.contentPart]).
	return journalPart{
		Type:   journalPartImage,
		Path:   filepath.ToSlash(path),
		SHA256: full,
		MIME:   "image/png",
	}, nil
}
