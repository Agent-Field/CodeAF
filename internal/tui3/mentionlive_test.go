package tui3

// MENTIONS CARRY LIVE STATE. A @chat token in prose wears the mentioned
// conversation's signal the way the strip wears it: the FIRST CELL of the
// token is the mark, in tabSignalInk, and the rest keeps the link ink — two
// claims, two inks. An idle or unknown conversation changes nothing, the
// hover highlight still wins over the whole token, and the hint names the
// state in words. These tests pin each of those.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

func mentionLivePalette() palette { return newPalette(tokens.TrueColor, false) }

func mentionLiveChat(signal tabSignal) mentionChat {
	return mentionChat{key: "k-price", slug: "price", title: "Price scrape", signal: signal}
}

// A working mention paints the first cell of the token with the accent — the
// strip's working mark — and the rest with the link ink.
func TestMentionLiveWorkingPaintsFirstCell(t *testing.T) {
	pal := mentionLivePalette()
	painted, links := linkifyMentions("ask @price about it", pal, nil, []mentionChat{mentionLiveChat(tabWorking)}, -1)
	if len(links) != 1 {
		t.Fatalf("the mention did not link: %q", painted)
	}
	want := pal.accent("@") + teamLinkInk(pal, "price")
	if !strings.Contains(painted, want) {
		t.Fatalf("a working mention is not first-cell accent + link ink\nwant: %q\ngot:  %q", want, painted)
	}
	if strings.Contains(painted, pal.accent("@price")) {
		t.Fatalf("the mark swallowed the whole token — two claims, two inks: %q", painted)
	}
}

// A needs-you mention paints the first cell with the warning ink, the strip's
// question mark.
func TestMentionLiveNeedsYouPaintsWarn(t *testing.T) {
	pal := mentionLivePalette()
	painted, _ := linkifyMentions("ask @price about it", pal, nil, []mentionChat{mentionLiveChat(tabNeedsPerson)}, -1)
	want := pal.warn("@") + teamLinkInk(pal, "price")
	if !strings.Contains(painted, want) {
		t.Fatalf("a needs-you mention is not first-cell warn + link ink\nwant: %q\ngot:  %q", want, painted)
	}
}

// An idle mention — at rest, or not known to this window, which are the one
// honest answer — paints byte-identically to the link ink it has always worn.
func TestMentionLiveIdlePaintsAsBefore(t *testing.T) {
	pal := mentionLivePalette()
	idle, _ := linkifyMentions("ask @price about it", pal, nil, []mentionChat{mentionLiveChat(tabIdle)}, -1)
	if !strings.Contains(idle, teamLinkInk(pal, "@price")) {
		t.Fatalf("an idle mention is not exactly the link ink: %q", idle)
	}
	if strings.Contains(idle, pal.accent("@")) || strings.Contains(idle, pal.warn("@")) {
		t.Fatalf("an idle mention wears a mark the window cannot back: %q", idle)
	}
}

// Needs-you OUTRANKS working — the strip's own precedence, read through
// tabSignalFor rather than re-implemented here.
func TestMentionLiveNeedsYouOutranksWorking(t *testing.T) {
	a := &app{}
	watch := &behindWatch{}
	watch.turning.Store(true)
	watch.waits.Store(true)
	a.behind = map[string]*kept{"k-price": {watch: watch}}
	if sig := a.tabSignalFor("k-price", false); sig != tabNeedsPerson {
		t.Fatalf("a held conversation both working and waiting reads %v, not tabNeedsPerson", sig)
	}
	watch.waits.Store(false)
	if sig := a.tabSignalFor("k-price", false); sig != tabWorking {
		t.Fatalf("a held conversation with a turn in flight reads %v, not tabWorking", sig)
	}
	if sig := a.tabSignalFor("k-stranger", false); sig != tabIdle {
		t.Fatalf("a conversation this window does not hold reads %v, not tabIdle", sig)
	}
}

// The hover hint names the state in the strip's own words, before the click
// segment.
func TestMentionLiveHintCarriesSignalWord(t *testing.T) {
	a := &app{}
	watch := &behindWatch{}
	watch.turning.Store(true)
	a.behind = map[string]*kept{"k-price": {watch: watch}}
	a.comp.recents = []mentionChat{{key: "k-price", slug: "price", title: "Price scrape"}}
	hint := a.mentionChatHint("k-price")
	word := tabSignalWord(tabWorking)
	if !strings.Contains(hint, hintSegment+word) {
		t.Fatalf("the hint does not carry %q: %q", word, hint)
	}
	if !strings.HasSuffix(hint, hintSegment+"click") {
		t.Fatalf("the state word did not land before the click segment: %q", hint)
	}
}

// Hover wins: paintLinksWith replaces ref.paint with the hot ink on the hot
// ref, so a hovered working mention is the hot ink over the WHOLE token, and
// the mark returns when the hover leaves.
func TestMentionLiveHoverWinsOverSignal(t *testing.T) {
	pal := mentionLivePalette()
	chats := []mentionChat{mentionLiveChat(tabWorking)}
	hot, _ := linkifyMentions("ask @price about it", pal, nil, chats, 0)
	if !strings.Contains(hot, teamLinkHotInk(pal, "@price")) {
		t.Fatalf("a hovered working mention is not the hot ink whole: %q", hot)
	}
	if strings.Contains(hot, pal.accent("@")) {
		t.Fatalf("the signal mark bled into the hover: %q", hot)
	}
	cool, _ := linkifyMentions("ask @price about it", pal, nil, chats, -1)
	if !strings.Contains(cool, pal.accent(ansi.Cut("@price", 0, 1))) {
		t.Fatalf("the mark did not return when the hover left: %q", cool)
	}
}
