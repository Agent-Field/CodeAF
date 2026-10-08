package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE FACTORY OFFER, AS A PERSON MEETS IT ─────────────────────────────────
//
// Every test below asserts what is on the screen and what the floor is asked,
// rather than the shape of the code under it (factorycard.go).

// factoryCardBody is a reason long enough to need the three-row cut at the
// width the lab draws at.
const factoryCardBody = "Pasting three lines into the message box keeps only two of them. " +
	"The third is lost whenever the paste ends without a newline, which is most pastes from a browser. " +
	"The fix belongs in the paste path, and it is done when a three-line paste sends three lines. " +
	"Somebody should also check the bracketed paste mode on terminals that do not send it at all."

// factoryNotice is one offer as the engine raises it.
func factoryNotice() session.FactoryNotice {
	return session.FactoryNotice{
		ID:    "f1",
		Title: "paste drops the last line",
		Body:  factoryCardBody,
		Repo:  "codeaf",
		Kind:  "bug",
		Size:  "M",
	}
}

// factoryOfferQuestion is the question the engine raises beside the card, built
// the way session's factoryQuestion builds it.
func factoryOfferQuestion(n session.FactoryNotice) session.Question {
	return session.Question{
		Ref:      n.ID,
		Kind:     session.QuestionFactory,
		Ask:      session.AskPermission,
		Form:     session.FormCard,
		Asker:    session.Asker{Kind: session.AskerModel},
		Head:     session.FactoryCardLead + n.Title,
		Reason:   n.Body,
		Subject:  session.SubjectRef{Ref: n.ID, Name: strings.Join([]string{n.Repo, n.Kind, n.Size}, session.DecisionSep)},
		Options:  session.AnswerOptions(session.QuestionFactory),
		Input:    session.InputShape{Kind: session.InputText, Prompt: session.FactoryChangePrompt},
		Stakes:   session.StakesReversible,
		Blocking: session.Blocking{Turn: true},
	}
}

// factoryOffer is one EventFactoryProposal.
func factoryOffer(n session.FactoryNotice) session.Event {
	return session.Event{Kind: session.EventFactoryProposal, Tool: "factory_add", Text: n.Title, Factory: &n}
}

// factoryCardLines is the card's rows, plain, at width.
func factoryCardLines(a *app, width int) []string {
	for i := range a.entries {
		if a.entries[i].kind == entryFactory {
			return questionPlainRows(FactoryCardRows(a, a.entries[i].fac, width, false))
		}
	}
	return nil
}

// THE CARD SAYS THE ENGINE'S HEAD, THE FACTS WITH NOTHING INVENTED, THE BODY
// CUT TO THREE ROWS, AND THE STAGES THE FLOOR WOULD RUN.
func TestFactoryCardDrawsTheOffer(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	a.factoryProposal(factoryOffer(factoryNotice()))
	rows := factoryCardLines(a, 100)
	if len(rows) == 0 {
		t.Fatal("the offer drew no card in the transcript")
	}
	text := strings.Join(rows, "\n")
	if !strings.Contains(rows[0], "wants to put this on the factory floor: paste drops the last line") {
		t.Fatalf("the head is not the engine's: %q", rows[0])
	}
	if !strings.Contains(rows[1], "codeaf · bug · M") {
		t.Fatalf("the facts row is %q", rows[1])
	}
	// THE EMPTINESS LAW: no estimate is no estimate, never `~$0`.
	if strings.Contains(text, "$") {
		t.Fatalf("a card with no estimate drew money:\n%s", text)
	}
	body := 0
	for _, row := range rows {
		if strings.Contains(row, "paste") && !strings.Contains(row, "factory floor") {
			body++
		}
	}
	if body == 0 {
		t.Fatalf("the body is not on the card:\n%s", text)
	}
	// Three rows of body, the last ending on the ellipsis.
	if len(rows) != 7 {
		t.Fatalf("the card is head, facts, three body rows, stages and foot; it drew %d rows:\n%s", len(rows), text)
	}
	if !strings.HasSuffix(strings.TrimRight(strings.TrimPrefix(rows[4], "│"), " "), a.icon(tokens.GEllipsis)) {
		t.Fatalf("the cut body does not end on the ellipsis: %q", rows[4])
	}
	if !strings.Contains(rows[5], "plan · write · test · review · security · proof") {
		t.Fatalf("the stages row is %q", rows[5])
	}

	// AND AN ESTIMATE IS SAID WITH ITS TILDE.
	priced := factoryNotice()
	priced.ID, priced.Estimate = "f2", 1.5
	if meta := factoryCardMeta(priced); meta != "codeaf · bug · M · ~$1.50" {
		t.Fatalf("the priced facts row is %q", meta)
	}
	bare := session.FactoryNotice{Repo: "codeaf", Kind: "chore"}
	if meta := factoryCardMeta(bare); meta != "codeaf · chore" {
		t.Fatalf("a card with no size and no estimate reads %q", meta)
	}
}

// THE ANSWERS ARE THE BLOCK'S: `1 add it`, `2 not now` and the box's prompt are
// drawn above the box, once, and the body the card already says is not said
// again there.
func TestFactoryCardQuestionIsDrawnOnTheBlock(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	n := factoryNotice()
	a.factoryProposal(factoryOffer(n))
	lab.fromLane(factoryOfferQuestion(n))
	if len(a.questions) != 1 {
		t.Fatalf("the factory question did not reach the block: %d open", len(a.questions))
	}
	block := lab.plain()
	for _, want := range []string{"1", session.FactoryAddLabel, "2", session.FactoryNotNowLabel, session.FactoryChangePrompt} {
		if !strings.Contains(block, want) {
			t.Fatalf("the block is missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "Pasting three lines") {
		t.Fatalf("the block says the body the card already says:\n%s", block)
	}
}

// THE FOOT SAYS WHAT IT CAME TO: the floor's number, not now, a change, or
// nothing added when it came down unanswered.
func TestFactoryCardSettledWords(t *testing.T) {
	for _, c := range []struct {
		name   string
		settle func(a *app, n session.FactoryNotice)
		want   string
	}{
		{"added", func(a *app, n session.FactoryNotice) {
			yes := n
			yes.Decided = &session.FactoryAnswer{Approved: true}
			a.factoryProposal(factoryOffer(yes))
			added := n
			added.Item = 12
			a.factoryAdded(session.Event{Kind: session.EventFactoryAdded, Text: "#12", Factory: &added})
		}, "added · #12"},
		{"not now", func(a *app, n session.FactoryNotice) {
			no := n
			no.Decided = &session.FactoryAnswer{}
			a.factoryProposal(factoryOffer(no))
		}, "not now"},
		{"changed", func(a *app, n session.FactoryNotice) {
			words := n
			words.Decided = &session.FactoryAnswer{Approved: true, Change: "make it a chore"}
			a.factoryProposal(factoryOffer(words))
		}, "changed in words"},
		{"expired", func(a *app, n session.FactoryNotice) {
			gone := n
			gone.Withdrawn = "nothing was added to the factory floor"
			a.factoryProposal(factoryOffer(gone))
		}, "expired · nothing added"},
	} {
		t.Run(c.name, func(t *testing.T) {
			lab := newQuestionLab(t)
			a := lab.a
			n := factoryNotice()
			a.factoryProposal(factoryOffer(n))
			c.settle(a, n)
			rows := factoryCardLines(a, 100)
			if len(rows) != 2 {
				t.Fatalf("a settled card is its head and its foot; it drew:\n%s", strings.Join(rows, "\n"))
			}
			if !strings.Contains(rows[1], c.want) {
				t.Fatalf("the foot is %q, want %q", rows[1], c.want)
			}
		})
	}
}

// AN ADDED EVENT WITH NO PROPOSAL ID IS PAIRED BY TITLE, to the card that was
// answered yes and has no number yet.
func TestFactoryCardAddedWithoutAnIDPairsByTitle(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	n := factoryNotice()
	a.factoryProposal(factoryOffer(n))
	yes := n
	yes.Decided = &session.FactoryAnswer{Approved: true}
	a.factoryProposal(factoryOffer(yes))
	added := session.FactoryNotice{Title: n.Title, Item: 7}
	a.factoryAdded(session.Event{Kind: session.EventFactoryAdded, Factory: &added})
	if rows := factoryCardLines(a, 100); len(rows) != 2 || !strings.Contains(rows[1], "added · #7") {
		t.Fatalf("the card did not take its number by title:\n%s", strings.Join(rows, "\n"))
	}
}

// THE FLOOR IS READ ONCE WHEN AN ITEM IS ADDED, and the bar's count follows
// within that one read.
func TestFactoryCardAddedReadsTheFloorOnce(t *testing.T) {
	a := placeApp(t)
	a.width = 160
	loads := 0
	a.factory = factory.Seam{Load: func() (factory.Snapshot, error) {
		loads++
		return factory.Fixture(factoryTestNow), nil
	}}
	added := factoryNotice()
	added.Item = 12
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: session.Event{Kind: session.EventFactoryAdded, Text: "#12", Factory: &added}})
	if loads != 1 {
		t.Fatalf("an added item read the floor %d times", loads)
	}
	if line := plain(a.navLine(160, a.pal)); !strings.Contains(line, navLabel(pageFactory)+" ? 1") {
		t.Fatalf("the bar does not count the floor after the read: %q", line)
	}
}

// THE BAR COUNTS THE FLOOR FROM THE LAUNCH, before anybody has opened it.
func TestFactoryCountOnTheBarFromTheLaunch(t *testing.T) {
	a := placeApp(t)
	a.width = 160
	a.factory = factory.FixtureSeam(factoryTestNow)
	if a.fp.loaded {
		t.Fatal("the lab read the floor before the launch")
	}
	if line := plain(a.navLine(160, a.pal)); strings.Contains(line, "? 1") {
		t.Fatalf("the bar counted a floor nobody read: %q", line)
	}
	drive(t, a, runCmd(a.factoryLaunchRead())...)
	if line := plain(a.navLine(160, a.pal)); !strings.Contains(line, navLabel(pageFactory)+" ? 1") {
		t.Fatalf("the launch read did not put the count on the bar: %q", line)
	}
	// AND THE LAUNCH ASKS FOR IT: the read is one of the commands Init batches.
	b := placeApp(t)
	b.factory = factory.FixtureSeam(factoryTestNow)
	drive(t, b, runCmd(b.Init())...)
	if !b.fp.loaded {
		t.Fatal("Init did not read the floor")
	}
	// AND A WINDOW WITH NO FLOOR READS NOTHING.
	c := placeApp(t)
	if cmd := c.factoryLaunchRead(); cmd != nil {
		t.Fatal("a window with no floor asked to read one")
	}
}

// A CHAT ITEM IS NAMED BY THE FLOOR'S OWN ID, never `#0`; a forge item keeps
// its forge number.
func TestFactoryCardChatItemRefIsTheStoreID(t *testing.T) {
	chat := factory.Item{ID: 99, Origin: factory.OriginChat, Kind: factory.KindIssue}
	if got := chat.Ref(); got != "#99" {
		t.Fatalf("a chat item's ref is %q", got)
	}
	forge := factory.Item{ID: 3, Num: 1538, Origin: factory.OriginForge, Kind: factory.KindIssue}
	if got := forge.Ref(); got != "#1538" {
		t.Fatalf("a forge item's ref is %q", got)
	}
	snap := factory.Fixture(factoryTestNow)
	snap.Items = append(snap.Items, factory.Item{
		ID: 99, Repo: snap.Items[0].Repo, Kind: factory.KindIssue, Title: "paste drops the last line",
		Tier: factory.TierOwner, Origin: factory.OriginChat, Created: factoryTestNow, Changed: factoryTestNow,
		State: factory.StateNeedsYou, Question: "go?",
	})
	a := placeApp(t)
	a.factory = factory.Seam{Load: func() (factory.Snapshot, error) { return snap, nil }}
	a.width, a.height = 120, 40
	if cmd := a.showPage(pageFactory); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	text := factoryFrameText(a)
	if !strings.Contains(text, "#99") {
		t.Fatalf("the chat item's row does not carry #99:\n%s", text)
	}
	if strings.Contains(text, "#0 ") {
		t.Fatalf("a row still says #0:\n%s", text)
	}
}

// ── THE RECIPE OFFER, AS A PERSON MEETS IT ──────────────────────────────────

// recipeNotice is one recipe offer as the engine raises it.
func recipeNotice() session.RecipeNotice {
	return session.RecipeNotice{ID: "r1", Repo: "web", Kind: "issue", Line: "security · chat · read it for auth holes · when touches auth"}
}

func recipeOffer(n session.RecipeNotice) session.Event {
	return session.Event{Kind: session.EventRecipeProposal, Tool: "factory_recipe", Text: session.RecipeHead(n), Recipe: &n}
}

// recipeOfferQuestion is the question the engine raises beside the card.
func recipeOfferQuestion(n session.RecipeNotice) session.Question {
	return session.Question{
		Ref:      n.ID,
		Kind:     session.QuestionRecipe,
		Ask:      session.AskPermission,
		Form:     session.FormCard,
		Asker:    session.Asker{Kind: session.AskerModel},
		Head:     session.RecipeHead(n),
		Reason:   session.RecipeWords(n),
		Subject:  session.SubjectRef{Ref: n.ID, Name: session.RecipeSubject(n)},
		Options:  session.AnswerOptions(session.QuestionRecipe),
		Input:    session.InputShape{Kind: session.InputText, Prompt: session.RecipeChangePrompt},
		Stakes:   session.StakesReversible,
		Blocking: session.Blocking{Turn: true},
	}
}

// THE RECIPE CARD IS THE FACTORY CARD'S SHAPE: the engine's head, one dim row
// saying where the line goes, the line, and the foot.
func TestFactoryRecipeCardDrawsTheLine(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	a.recipeProposal(recipeOffer(recipeNotice()))
	rows := factoryCardLines(a, 140)
	if len(rows) != 4 {
		t.Fatalf("the recipe card is head, where, line and foot; it drew %d rows:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[0], "wants to add to web's recipe for issue: security") {
		t.Fatalf("the head is not the engine's: %q", rows[0])
	}
	if !strings.Contains(rows[1], "recipe · web · issue") {
		t.Fatalf("the where row is %q", rows[1])
	}
	if !strings.Contains(rows[2], "security · chat · read it for auth holes · when touches auth") {
		t.Fatalf("the line row is %q", rows[2])
	}

	policy := session.RecipeNotice{ID: "r2", Repo: "web", Policy: "never post without green tests"}
	a.recipeProposal(recipeOffer(policy))
	if card := a.factoryCardFor("r2", ""); card == nil {
		t.Fatal("the policy card was not drawn")
	} else if rows := questionPlainRows(FactoryCardRows(a, card, 140, false)); !strings.Contains(rows[0], "wants to add to web's policy: never post without green tests") ||
		!strings.Contains(rows[1], "recipe · web · policy") {
		t.Fatalf("the policy card drew:\n%s", strings.Join(rows, "\n"))
	}
}

// THE ANSWERS ARE THE BLOCK'S: `1 bank it`, `2 not now` and the box's prompt.
func TestFactoryRecipeCardQuestionIsDrawnOnTheBlock(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	n := recipeNotice()
	a.recipeProposal(recipeOffer(n))
	lab.fromLane(recipeOfferQuestion(n))
	if len(a.questions) != 1 {
		t.Fatalf("the recipe question did not reach the block: %d open", len(a.questions))
	}
	block := lab.plain()
	for _, want := range []string{session.RecipeBankLabel, session.RecipeNotNowLabel, session.RecipeChangePrompt} {
		if !strings.Contains(block, want) {
			t.Fatalf("the block is missing %q:\n%s", want, block)
		}
	}
}

// THE FOOT SAYS WHAT IT CAME TO.
func TestFactoryRecipeCardSettledWords(t *testing.T) {
	for _, c := range []struct {
		name   string
		settle func(a *app, n session.RecipeNotice)
		want   string
	}{
		{"banked", func(a *app, n session.RecipeNotice) {
			yes := n
			yes.Decided = &session.RecipeAnswer{Approved: true}
			a.recipeProposal(recipeOffer(yes))
			a.recipeBanked(session.Event{Kind: session.EventRecipeBanked, Recipe: &n})
		}, "banked"},
		{"not now", func(a *app, n session.RecipeNotice) {
			no := n
			no.Decided = &session.RecipeAnswer{}
			a.recipeProposal(recipeOffer(no))
		}, "not now"},
		{"changed", func(a *app, n session.RecipeNotice) {
			words := n
			words.Decided = &session.RecipeAnswer{Change: "only for pull requests"}
			a.recipeProposal(recipeOffer(words))
		}, "changed in words"},
		{"expired", func(a *app, n session.RecipeNotice) {
			gone := n
			gone.Withdrawn = "nothing was banked in the recipe"
			a.recipeProposal(recipeOffer(gone))
		}, "expired · nothing banked"},
	} {
		t.Run(c.name, func(t *testing.T) {
			lab := newQuestionLab(t)
			a := lab.a
			n := recipeNotice()
			a.recipeProposal(recipeOffer(n))
			c.settle(a, n)
			rows := factoryCardLines(a, 140)
			if len(rows) != 2 {
				t.Fatalf("a settled card is its head and its foot; it drew:\n%s", strings.Join(rows, "\n"))
			}
			if !strings.Contains(rows[1], c.want) {
				t.Fatalf("the foot is %q, want %q", rows[1], c.want)
			}
		})
	}
}

// THE FLOOR IS READ AGAIN WHEN A LINE IS BANKED, through the task lane.
func TestFactoryRecipeBankedReadsTheFloor(t *testing.T) {
	a := placeApp(t)
	a.width = 160
	loads := 0
	a.factory = factory.Seam{Load: func() (factory.Snapshot, error) {
		loads++
		return factory.Fixture(factoryTestNow), nil
	}}
	n := recipeNotice()
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: session.Event{Kind: session.EventRecipeBanked, Recipe: &n}})
	if loads != 1 {
		t.Fatalf("a banked line read the floor %d times", loads)
	}
}
