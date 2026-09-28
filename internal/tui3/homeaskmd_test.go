package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// askMarkdown is the canned answer every test below asks about: the four shapes
// a person notices immediately when they are NOT rendered — a heading, bold, an
// inline code span and a list.
const askMarkdown = "## Two reminders\n\n" +
	"You have **two** of them. Run `codeaf status` to see them:\n\n" +
	"- one at 6\n" +
	"- one at 9\n"

// askSigils are those four shapes as their source spells them. A pane holding
// any of these is a pane showing a person the model's typing rather than the
// model's answer.
var askSigils = []string{"**", "## ", "`"}

// errAskFailed is a fault to end a turn with, for the one ending that is one.
var errAskFailed = errors.New("the model hung up")

// paneProse is the pane drawn and stripped of its colour, which is the form
// every assertion here is about: what the eye reads.
func paneProse(a *app, ex *homeExchange) string {
	return ansi.Strip(strings.Join(a.exchangePane(ex, 50, 40, a.pal), "\n"))
}

// askMarkdownLab is an errand whose one scripted turn answers in markdown and
// ends, driven the whole way through the real door — the submit, the deltas,
// the turn ending and the stream closing behind it.
func askMarkdownLab(t *testing.T) *app {
	t.Helper()
	lab := newErrandLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	a := lab.app(mine, []session.Event{
		text(session.EventTextDelta, askMarkdown),
		{Kind: session.EventTurnDone},
	})
	besideTheList(a)
	a.openHome()
	typeHome(a, "what is standing")
	drive(t, a, key("up"), key("enter"))
	return a
}

// TestAnAnswerAskedFromHomeIsRenderedMarkdownAndNotItsSource is issue #213: the
// pane drew a settled reply as wrapped plain text, so the same words that read
// as prose in a conversation read as `**bold**` and `##` on home.
//
// It asserts BOTH halves, because either one alone passes for the wrong reason:
// that none of the source's own punctuation survives, and that the words did.
func TestAnAnswerAskedFromHomeIsRenderedMarkdownAndNotItsSource(t *testing.T) {
	a := askMarkdownLab(t)
	ex := theExchange(a)
	if ex == nil {
		t.Fatal("asking from home made no exchange")
	}
	prose := paneProse(a, ex)
	for _, sigil := range askSigils {
		if strings.Contains(prose, sigil) {
			t.Fatalf("the answer still carries %q — it was drawn as source, not as prose:\n%s",
				sigil, prose)
		}
	}
	for _, word := range []string{"Two reminders", "two", "codeaf status", "one at 6", "one at 9"} {
		if !strings.Contains(prose, word) {
			t.Fatalf("rendering the answer lost %q:\n%s", word, prose)
		}
	}
	// AND IT IS THE SURFACE'S ONE RENDERER that produced them, not a second one
	// that happens to agree today. The transcript's door, asked the same
	// question at the same width, gives the same rows.
	want := ansi.Strip(strings.Join(trimBlanks(a.renderMarkdown(askMarkdown, 50)), "\n"))
	if !strings.Contains(prose, want) {
		t.Fatalf("the pane's answer is not what [app.renderMarkdown] draws.\nwant inside:\n%s\ngot:\n%s",
			want, prose)
	}
}

// TestAHomeAnswerIsRawWhileItFormsAndStyledOnceItSettles is the streaming half,
// and it is the TRANSCRIPT'S behaviour rather than a rule of its own: render.go
// wraps the growing edge plain and formats the block when it settles, so a pane
// that formatted mid-sentence would be the one surface re-flowing under the
// reader's eye.
func TestAHomeAnswerIsRawWhileItFormsAndStyledOnceItSettles(t *testing.T) {
	a := askMarkdownLab(t)
	ex := theExchange(a)
	if ex == nil {
		t.Fatal("asking from home made no exchange")
	}
	// A follow-up, streamed by hand so the reply can be looked at half-arrived.
	ex.startTurn(a.now())
	a.errandEvent(ex, text(session.EventTextDelta, askMarkdown))
	// The lump is paced; the assertion is about SOURCE vs markdown, not
	// about how far the edge has walked. Catch up, then look.
	catchUpReveal(a)
	if forming := paneProse(a, ex); !strings.Contains(forming, "**two**") {
		t.Fatalf("a reply still arriving was formatted; the growing edge is plain in the transcript too:\n%s",
			forming)
	}
	a.errandEvent(ex, session.Event{Kind: session.EventTurnDone})
	settled := paneProse(a, ex)
	for _, sigil := range askSigils {
		if strings.Contains(settled, sigil) {
			t.Fatalf("the turn ended and the answer still carries %q:\n%s", sigil, settled)
		}
	}
}

// TestEveryEndingOfAHomeTurnSettlesTheReply is the law [homeExchange.closeReply]
// exists for, and the shape of the defect #178 closed in the transcript: a block
// that stopped growing down a lane nobody taught stays unsettled forever, and
// unsettled is what draws raw.
//
// The four endings are checked one per exchange, because settling is idempotent
// and a single exchange would let one lane pass on another lane's work.
func TestEveryEndingOfAHomeTurnSettlesTheReply(t *testing.T) {
	endings := []struct {
		name string
		end  func(a *app, ex *homeExchange)
	}{
		{"the turn is done", func(a *app, ex *homeExchange) {
			a.errandEvent(ex, session.Event{Kind: session.EventTurnDone})
		}},
		{"the stream closes behind it", func(a *app, ex *homeExchange) {
			drive(t, a, errandClosedMsg{ex: ex})
		}},
		{"a call starts under it", func(a *app, ex *homeExchange) {
			a.errandEvent(ex, session.Event{Kind: session.EventToolBegin, Tool: "bash"})
		}},
		{"an error lands", func(a *app, ex *homeExchange) {
			a.errandEvent(ex, session.Event{Kind: session.EventError, Err: errAskFailed})
		}},
		{"the person asks something else", func(a *app, ex *homeExchange) {
			ex.startTurn(a.now())
		}},
	}
	for _, ending := range endings {
		t.Run(ending.name, func(t *testing.T) {
			a := askMarkdownLab(t)
			ex := theExchange(a)
			if ex == nil {
				t.Fatal("asking from home made no exchange")
			}
			ex.startTurn(a.now())
			a.errandEvent(ex, text(session.EventTextDelta, askMarkdown))
			at := ex.live
			if at < 0 {
				t.Fatal("a text delta opened no reply row")
			}
			ending.end(a, ex)
			if !ex.rows[at].settled {
				t.Fatalf("%s left the reply unsettled, so it draws as source forever", ending.name)
			}
		})
	}
}
