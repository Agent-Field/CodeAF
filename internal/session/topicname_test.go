package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── A DESKTOP CONVERSATION IS RENAMED ONCE PER CLEAR CHANGE OF SUBJECT ──────
//
// Each test drives a real [Agent] with a journal through the desktop door
// ([Agent.SubmitMessage]) and answers the title role from a local fixture
// (intentname_test.go's [intentCompleter]). What is asserted is how many times
// the title role was asked, what it was shown, the name the conversation ends
// with, and what its journal says on a reopen.

const (
	topicOpening = "make product images for my twitter saas launch, a hero shot and a banner"
	topicSwitch  = "help me calculate fire plan please for inaidn living in canada as nri wanting to live in chennai"
)

// topicNames answers the opening naming with the first name and every later
// ask with the second.
func topicNames(turns int, opening, later string) *intentCompleter {
	return intentFixture(turns, func(n int) intentAnswer {
		if n == 0 {
			return intentAnswer{title: opening}
		}
		return intentAnswer{title: later}
	})
}

// moreTurns gives the fixture another n ordinary answers.
func moreTurns(c *intentCompleter, n int) {
	for i := 0; i < n; i++ {
		c.steps = append(c.steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the assistant's own answer"), nil
		})
	}
}

// THE OWNER'S CONVERSATION: a new subject in the same chat earns a new name, the
// title role is shown the current name and the new message, the surfaces hear
// it, and a reopen keeps it.
func TestANewSubjectInTheSameChatEarnsANewName(t *testing.T) {
	c := topicNames(2, "Twitter SaaS image gen", "NRI fire plan Chennai")
	a, path := intentAgent(t, c, nil)
	lane, leave := a.WatchTitle()
	defer leave()

	submitMessage(t, a, Submission{Text: topicOpening})
	settleNaming(a)
	if a.Title() != "Twitter SaaS image gen" {
		t.Fatalf("opening Title() = %q", a.Title())
	}
	submitMessage(t, a, Submission{Text: topicSwitch})
	settleNaming(a)

	if c.asks() != 2 {
		t.Fatalf("title asks = %d, want the opening's and one for the new subject", c.asks())
	}
	asked := c.ask(1).userWords()
	for _, want := range []string{"Current name: Twitter SaaS image gen", topicSwitch, "twitter saas launch", "KEEP_NAME"} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the title role was not shown %q:\n%s", want, asked)
		}
	}
	if c.ask(1).maxTokens != topicTokens {
		t.Fatalf("max tokens = %d, want %d", c.ask(1).maxTokens, topicTokens)
	}
	if a.Title() != "NRI fire plan Chennai" {
		t.Fatalf("Title() = %q, want the new subject's name", a.Title())
	}
	names := drainTitles(lane)
	if len(names) == 0 || names[len(names)-1] != "NRI fire plan Chennai" {
		t.Fatalf("the surfaces last heard %q", names)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newAgent(Config{Workspace: a.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: path, IntentNames: true}, c)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Title() != "NRI fire plan Chennai" {
		t.Fatalf("reopened Title() = %q", reopened.Title())
	}
}

// A FOLLOW-UP ON THE SAME SUBJECT COSTS NOTHING: no title call at all, whether
// it refers back, is short, or shares the conversation's words.
func TestAFollowUpOnTheSameSubjectAsksNothing(t *testing.T) {
	followUps := []string{
		"make the banner darker",
		"fix that slide",
		"the hero shot needs our logo in the corner and a darker twitter banner",
		"add a launch tweet to go with the product images",
	}
	c := topicNames(1+len(followUps), "Twitter SaaS image gen", "Something else")
	a, _ := intentAgent(t, c, nil)
	submitMessage(t, a, Submission{Text: topicOpening})
	settleNaming(a)
	for _, followUp := range followUps {
		submitMessage(t, a, Submission{Text: followUp})
		settleNaming(a)
	}
	if c.asks() != 1 {
		t.Fatalf("title asks = %d, want only the opening's", c.asks())
	}
	if a.Title() != "Twitter SaaS image gen" {
		t.Fatalf("Title() = %q", a.Title())
	}
}

// A SUSPICION THE MODEL CALLS A CONTINUATION LEAVES THE NAME, and is one call.
func TestASuspicionAnsweredKeepNameKeepsTheName(t *testing.T) {
	c := topicNames(2, "Twitter SaaS image gen", "KEEP_NAME")
	a, path := intentAgent(t, c, nil)
	submitMessage(t, a, Submission{Text: topicOpening})
	settleNaming(a)
	submitMessage(t, a, Submission{Text: topicSwitch})
	settleNaming(a)
	if c.asks() != 2 || a.Title() != "Twitter SaaS image gen" {
		t.Fatalf("asks = %d, Title() = %q", c.asks(), a.Title())
	}
	for _, line := range titleLines(t, path) {
		if strings.Contains(line, "KEEP_NAME") {
			t.Fatalf("a continuation reached the journal: %s", line)
		}
	}
}

// A NAME THE PERSON GAVE IS NEVER REPLACED, in this process or after a reopen.
func TestANameThePersonGaveSurvivesANewSubject(t *testing.T) {
	c := topicNames(3, "Twitter SaaS image gen", "NRI fire plan Chennai")
	a, path := intentAgent(t, c, nil)
	submitMessage(t, a, Submission{Text: topicOpening})
	settleNaming(a)
	if err := a.RenameByPerson("Launch stuff"); err != nil {
		t.Fatal(err)
	}
	submitMessage(t, a, Submission{Text: topicSwitch})
	settleNaming(a)
	if c.asks() != 1 || a.Title() != "Launch stuff" {
		t.Fatalf("asks = %d, Title() = %q", c.asks(), a.Title())
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newAgent(Config{Workspace: a.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: path, IntentNames: true}, c)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	submitMessage(t, reopened, Submission{Text: "write a lesson plan for week three of the chemistry course"})
	settleNaming(reopened)
	if c.asks() != 1 || reopened.Title() != "Launch stuff" {
		t.Fatalf("after reopen: asks = %d, Title() = %q", c.asks(), reopened.Title())
	}
}

// ONE BREAK, ONE NEW NAME. The follow-ups that continue the new subject share
// its words and ask nothing, so the name does not flicker.
func TestANewSubjectIsRenamedOnceNotOnEachFollowUp(t *testing.T) {
	c := topicNames(4, "Twitter SaaS image gen", "NRI fire plan Chennai")
	a, _ := intentAgent(t, c, nil)
	submitMessage(t, a, Submission{Text: topicOpening})
	settleNaming(a)
	submitMessage(t, a, Submission{Text: topicSwitch})
	settleNaming(a)
	submitMessage(t, a, Submission{Text: "what if i retire at 45 instead"})
	settleNaming(a)
	submitMessage(t, a, Submission{Text: "plan assumes chennai living costs and canada savings growing 7%"})
	settleNaming(a)
	if c.asks() != 2 || a.Title() != "NRI fire plan Chennai" {
		t.Fatalf("asks = %d, Title() = %q", c.asks(), a.Title())
	}
}

// A LITERAL NAME FROM A JOURNAL THAT PREDATES THE OWNER MARK MAY BE A RENAME,
// so it is left alone; the same name marked as the engine's is not.
func TestAnOldLiteralNameIsReadAsThePersons(t *testing.T) {
	a := &Agent{config: Config{IntentNames: true}, title: "tidy the release notes", titleLiteral: true, titleCtx: context.Background(), file: &sessionFile{}}
	a.intentName = &intentNameState{Literal: true}
	if a.topicRenameableLocked() {
		t.Fatal("an old literal name was treated as the engine's")
	}
	a.intentName.Engine = true
	if !a.topicRenameableLocked() {
		t.Fatal("the engine's own fallback name could not be replaced")
	}
	a.intentName.Person = true
	if a.topicRenameableLocked() {
		t.Fatal("a person's name was treated as replaceable")
	}
}

// THE FREE SUSPICION, on the canvas quality runs' real follow-ups and on the
// message that found the defect.
func TestTopicBreakSuspectsOnlyAClearNewSubject(t *testing.T) {
	recent := func(said, reply string) []topicTurn { return []topicTurn{{said: said, reply: reply}} }
	same := []struct {
		name, said, reply, next string
	}{
		{"Spring menu ad", "put spring menu on the cafe ad in our brand font, and our logo in the corner", "Done: the ad now carries the spring menu in your brand font.", "make the headline bigger and add from $12 under it"},
		{"Open day flyer", "need a flyer for the open day, a4, we're printing 200", "The A4 flyer is on the canvas with the date, time and map.", "date moved to sat 14 march, fix the flyer"},
		{"Shift rota", "shift rota for next week", "Rota for next week, built from the staff sheet: two shifts a day, Dana is off Friday as asked, three baristas and one kitchen hand, and 27 slots are still unfilled.", "dana can do friday after all, and we hired a new barista kofi, 30h, any shift"},
		{"Sales dashboard", "make the sales dashboard nicer", "The dashboard has gross and net by month and by region.", "the chart is wrong, use net"},
		{"Vector db brief", "deep research brief on vector dbs w/ a comparison table and charts", "The brief compares Pinecone, Weaviate and Qdrant on price.", "pinecone came back with a quote, 3100/mo. update it"},
		{"Board deck", "5 slide deck for the board meeting", "Five slides: revenue, app rating, hiring, roadmap, asks.", "app rating is back up to 4.4, fix that slide. and add one on the hiring plan"},
		{"Hiring tracker", "track my hiring pipeline", "A sheet of the candidates by stage: Sofia and Leah are at offer, Omar is at interview, and two dropped out at screening. The chart shows the pipeline by stage.", "sofia took the offer and leah dropped out"},
	}
	for _, c := range same {
		if topicBreak(c.name, recent(c.said, c.reply), c.next) {
			t.Errorf("%q after %q was suspected as a new subject", c.next, c.said)
		}
	}
	if !topicBreak("Twitter SaaS image gen", recent(topicOpening, "Made a hero shot of the dashboard and a 1500x500 banner."), topicSwitch) {
		t.Error("the message that found the defect was not suspected")
	}
}
