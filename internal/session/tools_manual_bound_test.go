package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/manual"
)

// NO PAGE CAN FLOOD A CONVERSATION.
//
// The manual's biggest page is four times what the read tool will hand over for
// a file, and one lookup used to put all of it in the conversation. These tests
// hold the bargain the read tool makes: bounded, said out loud, and the rest
// reachable by name rather than lost.

// askManual calls the tool the way a model does.
func askManual(t *testing.T, arguments map[string]string) (string, bool) {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	result, isError, err := (&Agent{}).manualTool().Execute(context.Background(), encoded)
	if err != nil {
		t.Fatal(err)
	}
	return result, isError
}

// oversizedChatPages is every page this bound has to hold, not just the biggest
// one: a page grows into the cap without anybody noticing, and a recipe that
// checked one page would go on passing while a second page lost its tail.
func oversizedChatPages(t *testing.T) []string {
	t.Helper()
	over := make([]string, 0, 8)
	for _, name := range manual.Chat().Pages() {
		page, found := manual.Chat().Page(name)
		if !found {
			t.Fatalf("the corpus lists page %q and then does not have it", name)
		}
		if len(page) > bare.ResultByteCap {
			over = append(over, name)
		}
	}
	if len(over) == 0 {
		t.Skip("no chat page is over the cap any more, so there is nothing to cut")
	}
	return over
}

func TestEveryLongManualPageComesBackCutAndSaysSo(t *testing.T) {
	for _, name := range oversizedChatPages(t) {
		text, _ := manual.Chat().Page(name)
		result, isError := askManual(t, map[string]string{"page": name})
		if isError {
			t.Fatalf("reading %s was refused: %s", name, result)
		}
		if len(result) > bare.ResultByteCap {
			t.Errorf("page %s came back at %d bytes, over the %d a read is bounded by", name, len(result), bare.ResultByteCap)
		}
		cut := strings.Index(result, "\n\n[Cut:")
		if cut < 0 {
			t.Errorf("page %s is %d bytes and did not say it was cut", name, len(text))
			continue
		}
		if !strings.HasPrefix(text, result[:cut]) {
			t.Errorf("what came back is not the start of %s", name)
		}
		if !strings.Contains(result, "section set to one of:") {
			t.Errorf("the cut of %s does not say how to get the rest:\n%s", name, result[cut:])
		}
		// AND THE NOTICE HAS TO READ AS A NOTICE. A cut that lands inside a
		// block of sample output puts the message about the read, and the
		// headings it offers, inside the example.
		if fences := strings.Count(result[:cut], "\n```"); fences%2 != 0 {
			t.Errorf("the cut of %s ends inside a code fence, so its notice reads as sample output", name)
		}
	}
}

// The section read is bounded too, and the bound is meant to stay a guard: the
// manual's own rule is that a section is short and self-contained, and one that
// outgrew a whole read would be unreadable long before it was unbounded.
func TestNoManualSectionIsLongerThanOneRead(t *testing.T) {
	for _, section := range manual.Chat().Sections() {
		if len(section.Body) > bare.ResultByteCap {
			t.Errorf("section %q of %s is %d bytes, over the %d one read hands over — split it",
				section.Title, section.Page, len(section.Body), bare.ResultByteCap)
		}
	}
}

// THE LIST IS THE POINT. Every heading the cut names has to be askable and has
// to come back whole, or the bound is a dead end for the part of the page it
// kept back.
func TestEverySectionTheCutNamesComesBackWhole(t *testing.T) {
	for _, name := range oversizedChatPages(t) {
		result, _ := askManual(t, map[string]string{"page": name})
		notice := result[strings.Index(result, "\n\n[Cut:"):]
		sections := manual.Chat().PageSections(name)
		if len(sections) < 2 {
			t.Fatalf("page %s has %d sections, so the cut has nothing to offer", name, len(sections))
		}
		for _, section := range sections {
			if !strings.Contains(notice, "\n- "+section.Title) {
				t.Errorf("the cut of %s does not name its section %q", name, section.Title)
				continue
			}
			body, isError := askManual(t, map[string]string{"page": name, "section": section.Title})
			if isError {
				t.Errorf("section %q of %s was named by the cut and then refused: %s", section.Title, name, body)
				continue
			}
			if body != section.Body {
				t.Errorf("section %q of %s did not come back whole: got %d bytes, want %d", section.Title, name, len(body), len(section.Body))
			}
		}
	}
}

// And a heading that is not there is an exact refusal that names the ones that
// are, never a neighbouring section returned as though it were asked for.
func TestAnUnknownSectionNamesTheSectionsThatExist(t *testing.T) {
	name := oversizedChatPages(t)[0]
	sections := manual.Chat().PageSections(name)

	result, isError := askManual(t, map[string]string{"page": name, "section": "how do I make it rain"})
	if !isError {
		t.Error("a heading that does not exist was answered rather than refused")
	}
	if !strings.Contains(result, sections[0].Title) || !strings.Contains(result, sections[len(sections)-1].Title) {
		t.Errorf("the refusal does not name the sections that do exist:\n%s", result)
	}
	if len(result) > bare.ResultByteCap {
		t.Errorf("the refusal is %d bytes, over the %d a result is bounded by", len(result), bare.ResultByteCap)
	}
}

func TestAnUnknownManualPageNamesThePages(t *testing.T) {
	result, isError := askManual(t, map[string]string{"page": "no-such-page"})
	if !isError {
		t.Error("an unknown page was answered rather than refused")
	}
	named := 0
	for _, name := range manual.Chat().Pages() {
		if strings.Contains(result, name) {
			named++
		}
	}
	if named < 5 {
		t.Errorf("the refusal names %d pages of %d:\n%s", named, len(manual.Chat().Pages()), result)
	}
	// Helpful, and still bounded: an invitation is a signpost, not a document.
	if len(result) > bare.ResultByteCap {
		t.Errorf("the refusal is %d bytes, over the %d a result is bounded by", len(result), bare.ResultByteCap)
	}
}

func TestAShortManualPageIsUnchanged(t *testing.T) {
	name, text := "", ""
	for _, page := range manual.Chat().Pages() {
		body, _ := manual.Chat().Page(page)
		if len(body) <= bare.ResultByteCap && len(body) > len(text) {
			name, text = page, body
		}
	}
	if name == "" {
		t.Fatal("every chat page is over the cap, which cannot be right")
	}

	result, isError := askManual(t, map[string]string{"page": name})
	if isError {
		t.Fatalf("reading %s was refused: %s", name, result)
	}
	if result != text {
		t.Errorf("page %s (%d bytes, under the cap) came back changed at %d bytes", name, len(text), len(result))
	}
}

// THE BUDGET IS A NUMBER SOMEBODY HAS TO SEE COMING. When a page's headings
// outgrow [manualListCap] the list quietly stops naming the last ones, and the
// only thing that fails is [TestEverySectionTheCutNamesComesBackWhole] — on a
// section whoever broke it never touched. That is exactly how a heading added
// to the tasks page on 2026-09-09 came back as a regression in a section about
// returning to main from a nested task, which cost two lanes an afternoon
// between them. So the budget is asserted here in its own words, and the
// failure names the page that is closest to it and by how much.
func TestNoPagesHeadingsOutgrowTheListThatOffersThem(t *testing.T) {
	fullest, used := "", 0
	for _, page := range manual.Chat().Pages() {
		total := 0
		for _, section := range manual.Chat().PageSections(page) {
			total += len(listItemPrefix) + len(section.Title)
		}
		if total > used {
			fullest, used = page, total
		}
		if total > manualListCap {
			t.Errorf("the headings of %s come to %d bytes, over the %d the list that offers them holds — "+
				"the cut will stop naming its last sections, so shorten a heading or raise manualListCap",
				page, total, manualListCap)
		}
	}
	t.Logf("the fullest page is %s at %d bytes of %d, with %d to spare", fullest, used, manualListCap, manualListCap-used)
}
