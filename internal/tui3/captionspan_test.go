package tui3

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestACaptionConsumesOnlyTheSourceWordsItDisplays(t *testing.T) {
	for _, tc := range []struct{ name, text, title, remainder string }{
		{"same-line sentence", "The tests pass. Fixing the loader", "The tests pass", "The tests pass. Fixing the loader"},
		{"word budget", "one two three four five six seven eight nine ten eleven twelve", "one two three four five six seven eight nine ten", "one two three four five six seven eight nine ten eleven twelve"},
		{"multiline", "checking where the fold is minted.\nThe details follow.", "checking where the fold is minted", "The details follow."},
		{"unicode spaces", "  Vérifier\u00a0les\u00a0contrats. Réparer le chargeur", "Vérifier les contrats", "  Vérifier\u00a0les\u00a0contrats. Réparer le chargeur"},
		{"selected later sentence", "OK. Checking the loader carefully.", "Checking the loader carefully", "OK. Checking the loader carefully."},
		{"one complete sentence", "Checking the loader carefully.", "Checking the loader carefully", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			title, cut := captionSpan(tc.text)
			if title != tc.title || cut < 0 || cut > len(tc.text) {
				t.Fatalf("caption span = %q, %d; want %q and a valid source offset", title, cut, tc.title)
			}
			if got := tc.text[cut:]; got != tc.remainder || !utf8.ValidString(got) {
				t.Fatalf("caption lost source text: got %q; want %q", got, tc.remainder)
			}
		})
	}
}

// The live tool's expanded body and a deliberately opened historical step use
// the same source span. The caption may shorten the heading, never the record.
func TestAShortenedCaptionKeepsItsRemainingNarrationOnThePage(t *testing.T) {
	for _, status := range []toolState{toolRunning, toolOK} {
		t.Run(itoa(int(status)), func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.room = a.newRoom(7, "Fix the nil-map crash")
			a.room.entries = []entry{
				{kind: entryUser, text: "Fix the nil-map crash", turn: 1},
				{kind: entryAssistant, text: "The tests pass. Fixing the loader", settled: true, turn: 1},
				{kind: entryTool, tool: "edit", text: "edit internal/config/load.go", status: status, turn: 1},
			}
			a.room.capOpen = map[int]bool{1: true}
			drawn, _ := a.deckRows(a.room.deck(), 90)
			var page []string
			captions := 0
			for _, r := range drawn {
				page = append(page, plain(r.text))
				if r.hit == hitCaption && strings.Contains(plain(r.text), "The tests pass") {
					captions++
				}
			}
			text := strings.Join(page, "\n")
			if captions != 1 || strings.Count(text, "Fixing the loader") != 1 || strings.Count(text, "The tests pass") != 2 {
				t.Fatalf("shortening the heading lost its complete narration:\n%s", text)
			}
			if !strings.Contains(text, "edit internal/config/load.go") {
				t.Fatalf("the caption's call disappeared:\n%s", text)
			}
		})
	}
}
