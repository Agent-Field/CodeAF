package session

// THE CHAT'S DOOR ONTO A REPOSITORY'S RECIPE — BEHIND A CARD.
//
// A repository's recipe (`.codeaf/factory.md`, internal/factory's recipefile.go)
// is the stages its factory work runs through, the policy it is held to and the
// habits the person has banked. A conversation is often where a rule is first
// said out loud: "in this repo always run a security review when auth is
// touched", "never post without green tests", "for PRs, two review rounds".
// `factory_recipe` is the verb that carries one such sentence into the file,
// and it carries it as a QUESTION, never as a write — `factory_add`'s shape
// (tools_factory.go) with a different payload and a different door.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - NOTHING IS WRITTEN UNTIL THE PERSON SAYS YES. The tool raises a card; only
//     `bank it` writes the line. There is no argument and no phrasing that
//     writes without that answer.
//
//   - ONE YES IS ONE LINE. A stage line, a policy sentence or a habit, and never
//     two: the card shows exactly what will be in the file, and the file gets
//     exactly that.
//
//   - A LINE THE FILE CANNOT READ IS NEVER OFFERED. A stage line is parsed with
//     the file's own reader before any card is raised, and a line it names a
//     problem in is refused with the reader's words, so a yes can never bank a
//     line the floor would then report as broken.
//
//   - THERE IS NO CLOCK THAT BANKS. The window bounds the tool call, as
//     [factoryCardWindow] does, and its ending is nothing written.
//
//   - WORDS ARE A CHANGE, NOT A YES, as on the factory card.

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// The sentences the model reads back, and the card's own words. The manual
// quotes them (internal/manual/chat/factory.md) and a surface drawing the card
// spells the same head.
const (
	// RecipeCardLead opens every recipe card's head; [RecipeHead] finishes it.
	RecipeCardLead = "wants to add to "
	// RecipeBankLabel and RecipeNotNowLabel are the card's two answers.
	RecipeBankLabel   = "bank it"
	RecipeNotNowLabel = "not now"
	// RecipeChangePrompt is the line over the card's text box.
	RecipeChangePrompt = FactoryChangePrompt

	recipeChangedLead = "the person changed it: "
	recipeChangedTail = "\nNothing is banked. Propose it again with that"
	recipeDeclined    = "nothing was banked: the person said no."
	recipeUnanswered  = "nothing was banked: the card was never answered"
	// recipeRefusedLead opens what a door that would not write returns; the
	// door's own sentence follows it.
	recipeRefusedLead = "nothing was banked: "
)

// The card's two keys, the factory card's: `1` is the yes and `2` the answer
// that changes nothing.
const (
	RecipeBankKey   = "1"
	RecipeNotNowKey = "2"
)

// recipeKinds are the sections a stage line may go in. Chore is left out on
// purpose: it is a section of the file, but nothing in a conversation names it,
// and a model offered it would file stages nobody asked for under it.
var recipeKinds = []string{string(factory.KindIssue), string(factory.KindPR), string(factory.KindCI)}

const factoryRecipeDescription = "Offer one line for a repository's factory recipe, its policy or its habits, when the person says something that belongs there: " +
	"\"in this repo always run a security review when auth is touched\" is a stage, \"never post without green tests\" is policy, \"PRs from my own issues ship when proof is green\" is a habit. " +
	"Give exactly one of stage, policy or habit. " +
	"A stage is one line in the recipe grammar, `name · kind · ask · knobs`, for example `security · chat · read it for auth holes · when touches auth`; a line the recipe cannot read is refused with the reason, so fix it and call again. " +
	"NOTHING IS WRITTEN BY CALLING THIS: the person is shown a card with the line, and only their `bank it` writes it. " +
	"If they type a change instead, nothing is written and you are told their words: propose again with them."

func factoryRecipeSchemaJSON() string {
	quoted := make([]string, len(recipeKinds))
	for i, kind := range recipeKinds {
		quoted[i] = strconv.Quote(kind)
	}
	return `{"type":"object","properties":{` +
		`"repo":{"type":"string","description":"The repository's short name as the person says it, or this workspace's name when they do not name one."},` +
		`"kind":{"type":"string","enum":[` + strings.Join(quoted, ",") + `],"description":"Which work the stage is for. Required with stage."},` +
		`"stage":{"type":"string","description":"One stage line: name · kind · ask · knobs."},` +
		`"policy":{"type":"string","description":"One policy sentence."},` +
		`"habit":{"type":"string","description":"One habit sentence."}` +
		`},"required":["repo"],"additionalProperties":false}`
}

// The gloss a person reads beside the call is the stage line.
func init() { glossField["factory_recipe"] = "stage" }

// recipeOffer is one recipe card still waiting on somebody, [factoryOffer]'s
// twin.
type recipeOffer struct {
	answers chan RecipeAnswer
	notice  RecipeNotice
	asked   time.Time
}

// reStageNumber is a leading `N.` the model may have copied off the file.
var reStageNumber = regexp.MustCompile(`^\d+\.\s*`)

// RecipeHead is the card's head for one notice: what it adds and where.
func RecipeHead(n RecipeNotice) string {
	repo := strings.TrimSpace(n.Repo)
	switch {
	case n.Policy != "":
		return RecipeCardLead + repo + "'s policy: " + n.Policy
	case n.Habit != "":
		return RecipeCardLead + repo + "'s habits: " + n.Habit
	}
	return RecipeCardLead + repo + "'s recipe for " + n.Kind + ": " + n.Line
}

// recipeBanked is the sentence a yes returns.
func recipeBanked(n RecipeNotice) string {
	switch {
	case n.Policy != "":
		return n.Policy + " is in " + n.Repo + "'s policy"
	case n.Habit != "":
		return n.Habit + " is in " + n.Repo + "'s habits"
	}
	return n.Line + " is in " + n.Repo + "'s recipe for " + n.Kind
}

// recipeStageLine reads one stage line the way the file would, as the only
// stage of a one-section document, and answers it in the file's own spelling
// (number dropped) or the reader's first problem with it.
func recipeStageLine(kind factory.Kind, line string) (string, error) {
	line = strings.Join(strings.Fields(reStageNumber.ReplaceAllString(strings.TrimSpace(line), "")), " ")
	if line == "" {
		return "", errors.New("the stage line is empty")
	}
	recipe, problems := factory.Parse("## " + string(kind) + "\n1. " + line)
	if len(problems) > 0 {
		return "", errors.New(problems[0].Why)
	}
	stages := recipe.For(kind)
	if len(stages) != 1 {
		return "", errors.New("a stage is one line: N. name · kind · ask · knobs")
	}
	return reStageNumber.ReplaceAllString(factory.StageLines(stages)[0], ""), nil
}

// factoryRecipeTool raises one recipe card and waits for its answer.
func (a *Agent) factoryRecipeTool() bare.Tool {
	return bare.Tool{
		Name:        "factory_recipe",
		Description: factoryRecipeDescription,
		Schema:      json.RawMessage(factoryRecipeSchemaJSON()),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Repo   string `json:"repo"`
				Kind   string `json:"kind"`
				Stage  string `json:"stage"`
				Policy string `json:"policy"`
				Habit  string `json:"habit"`
			}
			if len(args) > 0 {
				if err := decodeToolArguments(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			notice := RecipeNotice{
				Repo:   strings.TrimSpace(parsed.Repo),
				Kind:   strings.ToLower(strings.TrimSpace(parsed.Kind)),
				Policy: strings.Join(strings.Fields(parsed.Policy), " "),
				Habit:  strings.Join(strings.Fields(parsed.Habit), " "),
			}
			stage := strings.TrimSpace(parsed.Stage)
			given := 0
			for _, part := range []string{stage, notice.Policy, notice.Habit} {
				if part != "" {
					given++
				}
			}
			if notice.Repo == "" {
				return "Invalid arguments: factory_recipe needs the repo it belongs to.", true, nil
			}
			if given != 1 {
				return "Invalid arguments: factory_recipe takes exactly one of stage, policy or habit.", true, nil
			}
			if stage != "" {
				if !oneOf(notice.Kind, recipeKinds) {
					return "Invalid arguments: a stage needs kind, one of " + strings.Join(recipeKinds, ", ") + ".", true, nil
				}
				line, err := recipeStageLine(factory.Kind(notice.Kind), stage)
				if err != nil {
					return "Invalid arguments: the recipe cannot read that stage line: " + err.Error() + ".", true, nil
				}
				notice.Line = line
			} else {
				// A sentence belongs to the whole repository, not to a kind.
				notice.Kind = ""
			}
			answer, err := a.askRecipe(ctx, &notice)
			if err != nil {
				return recipeUnanswered, false, nil
			}
			if change := strings.TrimSpace(answer.Change); change != "" {
				return recipeChangedLead + change + recipeChangedTail, false, nil
			}
			if !answer.Approved {
				return recipeDeclined, false, nil
			}
			door := a.config.Recipe
			switch {
			case notice.Policy != "":
				err = door.BankPolicy(ctx, notice.Repo, notice.Policy)
			case notice.Habit != "":
				err = door.BankHabit(ctx, notice.Repo, notice.Habit)
			default:
				err = door.BankStage(ctx, notice.Repo, factory.Kind(notice.Kind), notice.Line)
			}
			if err != nil {
				return recipeRefusedLead + oneLine(err.Error()), true, nil
			}
			banked := notice
			a.emitFactory(Event{Kind: EventRecipeBanked, Tool: "factory_recipe", Text: RecipeHead(notice), Recipe: &banked})
			return recipeBanked(notice), false, nil
		},
	}
}

// askRecipe raises one recipe card and waits for the person, for as long as the
// window allows. It is [Agent.askFactory] with the payload changed, and every
// ending but an answer is an error the tool reads as nothing banked.
func (a *Agent) askRecipe(ctx context.Context, notice *RecipeNotice) (RecipeAnswer, error) {
	answers := make(chan RecipeAnswer, 1)
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return RecipeAnswer{}, errAgentClosed
	}
	a.recipeSeq++
	notice.ID = "r" + strconv.FormatUint(a.recipeSeq, 10)
	if a.recipeOffers == nil {
		a.recipeOffers = make(map[string]*recipeOffer, 1)
	}
	offer := &recipeOffer{answers: answers, notice: *notice, asked: time.Now()}
	a.recipeOffers[notice.ID] = offer
	a.mu.Unlock()
	card := *notice
	// THE CARD COMES DOWN WITH THE TOOL CALL, as the factory card does.
	defer a.forgetRecipe(card)
	defer a.raiseQuestion(a.recipeQuestion(card.ID, card, offer.asked), func() {
		a.emitFactory(Event{Kind: EventRecipeProposal, Tool: "factory_recipe", Text: RecipeHead(card), Recipe: &card})
	})()

	window := a.config.factoryWindow
	if window <= 0 {
		window = factoryCardWindow
	}
	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case answer := <-answers:
		decided := card
		settled := answer
		decided.Decided = &settled
		a.emitFactory(Event{Kind: EventRecipeProposal, Tool: "factory_recipe", Text: RecipeHead(card), Recipe: &decided})
		return answer, nil
	case <-timer.C:
		// NOTHING WAS BANKED, and that is the whole of what the window decides.
		return RecipeAnswer{}, errRecipeUnanswered
	case <-ctx.Done():
		return RecipeAnswer{}, ctx.Err()
	}
}

// errRecipeUnanswered is the window ending with the card still up.
var errRecipeUnanswered = errors.New("session: the recipe card went unanswered")

// recipeGoneReason is the sentence a card that came down unanswered retires
// with, on the card and on the question alike.
const recipeGoneReason = "nothing was banked in the recipe"

// forgetRecipe drops one card and, where it was still standing, says on the
// task lane that it came down unanswered ([Agent.forgetFactory]'s reading).
func (a *Agent) forgetRecipe(card RecipeNotice) {
	a.mu.Lock()
	offer := a.recipeOffers[card.ID]
	delete(a.recipeOffers, card.ID)
	a.mu.Unlock()
	if offer == nil {
		return
	}
	gone := card
	gone.Withdrawn = recipeGoneReason
	a.emitFactory(Event{Kind: EventRecipeProposal, Tool: "factory_recipe", Text: RecipeHead(card), Recipe: &gone})
}

// standingRecipeCardsLocked is every recipe card still waiting on somebody, as
// the events that raised them, oldest first. a.mu is held. It is what a window
// that arrives late is handed, for [Agent.standingFactoryCardsLocked]'s reason.
func (a *Agent) standingRecipeCardsLocked() []Event {
	if len(a.recipeOffers) == 0 {
		return nil
	}
	offers := make([]*recipeOffer, 0, len(a.recipeOffers))
	for _, offer := range a.recipeOffers {
		offers = append(offers, offer)
	}
	sort.Slice(offers, func(i, j int) bool {
		if !offers[i].asked.Equal(offers[j].asked) {
			return offers[i].asked.Before(offers[j].asked)
		}
		return offers[i].notice.ID < offers[j].notice.ID
	})
	cards := make([]Event, 0, len(offers))
	for _, offer := range offers {
		card := offer.notice
		cards = append(cards, Event{Kind: EventRecipeProposal, Tool: "factory_recipe", Text: RecipeHead(card), Recipe: &card})
	}
	return cards
}

// recipeQuestion is a recipe card as a question. The subject line is
// `recipe · <repo> · <kind>` (or policy, or habits), and the reason is the line
// itself, which is what the person is deciding about.
func (a *Agent) recipeQuestion(id string, notice RecipeNotice, asked time.Time) Question {
	return a.said(QuestionRecipe, id, Question{
		Ref:      id,
		Kind:     QuestionRecipe,
		Ask:      AskPermission,
		Form:     FormCard,
		Asker:    Asker{Kind: AskerModel},
		Head:     RecipeHead(notice),
		Reason:   RecipeWords(notice),
		Subject:  SubjectRef{Ref: id, Name: RecipeSubject(notice)},
		Options:  AnswerOptions(QuestionRecipe),
		Input:    InputShape{Kind: InputText, Prompt: RecipeChangePrompt},
		Stakes:   StakesReversible,
		Blocking: Blocking{Turn: true},
		Asked:    asked,
	})
}

// RecipeWords is the one line the card proposes: the stage line, the policy or
// the habit.
func RecipeWords(n RecipeNotice) string {
	switch {
	case n.Policy != "":
		return n.Policy
	case n.Habit != "":
		return n.Habit
	}
	return n.Line
}

// RecipeSubject is the card's dim facts row: `recipe · <repo> · <kind>`, with
// `policy` or `habits` where a stage would name its kind.
func RecipeSubject(n RecipeNotice) string {
	where := n.Kind
	switch {
	case n.Policy != "":
		where = "policy"
	case n.Habit != "":
		where = "habits"
	}
	parts := []string{"recipe"}
	for _, part := range []string{n.Repo, where} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, DecisionSep)
}
