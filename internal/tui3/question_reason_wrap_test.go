package tui3

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/charmbracelet/x/ansi"
)

// ── A CARD'S SENTENCE IS WRAPPED, NEVER CUT ─────────────────────────────────
//
// A hand test of chat moves (2026-11-19) showed two cards that stopped in the
// middle of the one sentence a person had to read: the refusal card ended at
// `switch it back on`, and the continue card for a big folder at `copies that
// folder too: 174`. The card kept two rows of the reason and dropped the rest
// without a mark.

// squashed is rows with their colour, their frame and every space taken out. The
// card's wrapper may break a word at its hyphen (`host-` over `b;`), and the
// frame stands between the halves, so the words are read with all of that gone.
func squashed(rows []string) string {
	return strings.NewReplacer(" ", "", "│", "", "╭", "", "╮", "", "╰", "", "╯", "", "─", "").Replace(ansi.Strip(strings.Join(rows, " ")))
}

// readsInOrder reports whether every word of sentence stands in rows, in order,
// whatever stands between them.
func readsInOrder(rows []string, sentence string) (missing string, ok bool) {
	have := squashed(rows)
	for _, word := range strings.Fields(sentence) {
		at := strings.Index(have, strings.ReplaceAll(word, " ", ""))
		if at < 0 {
			return word, false
		}
		have = have[at+len(word):]
	}
	return "", true
}

// TestACardShowsItsWholeSentenceAtEveryWidth is contracts C2.1 and C2.2. The two
// cards are the two hand-test ones: the continue card before a big folder, and
// the retry card that carries the refusal for blind pass-through switched off.
func TestACardShowsItsWholeSentenceAtEveryWidth(t *testing.T) {
	big := chatlist.Row{Cell: "c-big", Title: "Index", Device: "host-b", Status: chatlist.Off, DurableAgo: 6 * time.Minute, Folder: "", Size: 174 << 20}
	a := continueHome(t, &fakeTaker{}, continueRows, 120)
	refused := a.continueFailure(homeTakenMsg{row: big, err: fmt.Errorf("cellsync: fetch object x: %w", syncwords.ErrLiveTransferOff)})
	cards := map[string]questionShown{
		"the continue card": a.continueShown(big, continueYesAt),
		"the refusal card":  a.continueRetryShown(homeTakenMsg{row: big}, refused),
	}
	for name, card := range cards {
		for _, width := range []int{44, 50, 60, 80, 100, 120} {
			t.Run(fmt.Sprintf("%s at %d columns", name, width), func(t *testing.T) {
				reason := strings.TrimSpace(card.question.Reason)
				if len(strings.Fields(reason)) < 12 {
					t.Fatalf("the card's sentence is too short to prove anything: %q", reason)
				}
				rows := a.questionPanelRows(card, width)
				if word, ok := readsInOrder(rows, reason); !ok {
					t.Errorf("the card is cut: the word %q of %q is not on it:\n%s", word, reason, ansi.Strip(strings.Join(rows, "\n")))
				}
				for _, row := range rows {
					if got := ansi.StringWidth(row); got > width {
						t.Errorf("a card row is %d columns wide on a %d-column card: %q", got, width, ansi.Strip(row))
					}
				}
			})
		}
	}
}

// TestARunawaySentenceIsStillBoundedAndSaysItWasCut is contract C2.3: the rows
// a sentence may take stay bounded so the answers are never pushed off the
// screen, and a sentence that does not fit even then ends in a mark, because
// half a sentence with no sign of it is what the hand test found.
func TestARunawaySentenceIsStillBoundedAndSaysItWasCut(t *testing.T) {
	a := continueHome(t, &fakeTaker{}, continueRows, 120)
	row := continueRows[0]
	card := a.continueShown(row, continueYesAt)
	card.question.Reason = strings.Repeat("this sentence never ends and ", 200)
	rows := a.questionPanelRows(card, 50)
	text := ansi.Strip(strings.Join(rows, "\n"))
	// The sentence's own rows are counted, not the card's: the card also holds a
	// head, the answers and the keys.
	sentenceRows := 0
	for _, row := range strings.Split(text, "\n") {
		if strings.Contains(row, "sentence") || strings.Contains(row, "never ends") {
			sentenceRows++
		}
	}
	if sentenceRows > questionPanelReasonRows {
		t.Errorf("a runaway sentence took %d rows; the bound is %d", sentenceRows, questionPanelReasonRows)
	}
	if !regexp.MustCompile(`(this|sentence|never|ends|and)` + glyphMore).MatchString(text) {
		t.Errorf("a sentence cut at the bound carries no sign of it on its last row:\n%s", text)
	}
	if !strings.Contains(text, moveStay) {
		t.Errorf("the answers were pushed off the card by the sentence:\n%s", text)
	}
}
