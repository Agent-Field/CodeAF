package session

// GROOMING IS INFER-THEN-CONFIRM, NEVER INTERROGATE.
//
// PRD §4 states the law and the intake card is where it becomes visible: what
// the conversation already answers is IN the fields when the card comes up, and
// the only thing anybody is asked about is what is left. The failure this exists
// to prevent is the one every form-filling assistant falls into — a program that
// has just been told everything it needs asking for it again, field by field,
// while the person watches their own sentence being read back at them as
// questions.
//
// ONE MODEL CALL, AT MOST, AND ONLY WHEN THERE IS SOMETHING TO FILL. A program
// whose every field carries a schema default is answered without asking anybody,
// and a conversation that has said nothing yet is not worth a call to be told so.
// The call that does happen is on the cheap tier for the archetypal cheap-call
// reason: a wrong guess costs one correction on a card the person is already
// looking at.
//
// NOTHING HERE COMMITS ANYTHING. The card is the consent — the person reads what
// was inferred, changes what is wrong, fills what is missing, and confirms. A
// field this file filled and a field they typed are drawn differently
// ([SubharnessField.Filled]) precisely so that an inference can never be mistaken
// for something they said.

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// RoleIntake is registered here because this file owns the call, which is the
// arrangement task_shape.go's RoleShaper already keeps.
func init() { roles.Register(roles.RoleIntake, roles.TierLow) }

const (
	// intakeWindow bounds the one call. A card is a thing somebody is waiting to
	// see, so a namer that hangs is worse than a card with blanks in it: the
	// window expires, every field stays empty, and the card is the honest one
	// `/subharness <name>` typed cold already shows.
	intakeWindow = 20 * time.Second

	// intakeClip bounds each message handed to the filler, and intakeMessages
	// how many of them. The material that answers a form is what was said
	// RECENTLY — the brief, the file name, the deadline somebody just typed —
	// and sending a whole session's transcript would pay for a context to fill
	// four fields.
	intakeClip     = 2000
	intakeMessages = 12
)

// intakeSystem says who is being asked, and nothing else. The instruction goes
// last, in the user message, for the reason title.go states at length: the small
// models this lands on read the system message as CHARACTER and the end of the
// user message as THE THING TO DO.
const intakeSystem = "You fill in a form from what has already been said."

// intakePrompt is the whole instruction and it is one paragraph, because the
// only thing that matters about the answer is the rule for leaving a field out.
//
// THE OMISSION RULE IS THE FEATURE. A model that guesses fills the card with
// plausible wrong answers a person then has to notice and correct, which is
// worse than a blank they were going to fill anyway — a blank asks, and a wrong
// answer does not.
const intakePrompt = "Fill in the fields above from the conversation. Answer with a JSON object and nothing else. " +
	"Include a field ONLY when the conversation actually says what it is; leave out anything you would be guessing at, " +
	"however likely the guess. An empty object is the right answer when nothing here says any of it."

// SubharnessIntake is the card's data for one subharness: every field of its
// input schema, what is filled, and which required ones are still blank.
//
// A NAME NOTHING HAS IS AN ERROR AND NOT AN EMPTY CARD, which is the difference
// between this door and the list beside it. Somebody typed a name; getting a
// blank card for a subharness that does not exist would send them looking for
// the fields rather than for the typo.
//
// THE ORDER IS THE SCHEMA'S OWN ([exec.Schema.Fields] orders by `x-order` where
// the schema states one and alphabetically otherwise), so a card drawn twice is
// the same card.
func (a *Agent) SubharnessIntake(name string) (SubharnessCard, error) {
	registry := a.config.Subharnesses
	if registry == nil {
		return SubharnessCard{}, errSubharnessUnwired
	}
	runner, err := registry.Subharness(strings.TrimSpace(name))
	if err != nil {
		return SubharnessCard{}, err
	}
	card := blankSubharnessCard(runner.Manifest())
	a.fillSubharnessCard(&card)
	return card, nil
}

// blankSubharnessCard is the card before anybody has read anything into it: the
// schema's fields in the schema's order, with the DEFAULTS already in place.
//
// A DEFAULT IS AN ANSWER AND IS NOT A FILLING. The field is answered — it is not
// in Missing, and confirming the card would launch with it — but nobody put it
// there, so [SubharnessField.Filled] stays false and the card draws it dim. The
// two are different facts and the separation is the whole reason that field
// exists beside Value.
func blankSubharnessCard(manifest exec.Manifest) SubharnessCard {
	card := SubharnessCard{Manifest: manifest}
	for _, field := range manifest.Input.Fields() {
		line := SubharnessField{Field: field}
		if len(field.Default) > 0 && string(field.Default) != "null" {
			line.Value = field.Default
		}
		card.Fields = append(card.Fields, line)
	}
	card.Missing = missingSubharnessFields(card.Fields)
	return card
}

// missingSubharnessFields is the required blanks, in the card's own field order.
// An empty answer is a card that could be confirmed as it stands.
func missingSubharnessFields(fields []SubharnessField) []string {
	var missing []string
	for _, field := range fields {
		if field.Field.Required && len(field.Value) == 0 {
			missing = append(missing, field.Field.Name)
		}
	}
	return missing
}

// fillSubharnessCard reads what this conversation already answers into the card.
//
// IT NEVER FAILS THE CARD. A model that could not be reached, a window that ran
// out, an answer that is not an object: every one of them leaves the card exactly
// as it was, which is the honest card for a conversation nothing could be read
// out of. There is no event kind for "a small thing did not work" and inventing
// one here would report a fault about work the person never asked for
// (title.go makes the same argument about the namer).
func (a *Agent) fillSubharnessCard(card *SubharnessCard) {
	if len(card.Missing) == 0 {
		// EVERY REQUIRED FIELD IS ALREADY ANSWERED, so there is nothing this call
		// could improve enough to be worth making. The optional blanks stay blank
		// — a program's optional field is optional because the program has an
		// answer for its absence, and paying a model call to guess at one is
		// paying to make a decision the program already made.
		return
	}
	if a.client == nil {
		return
	}
	material := a.intakeMaterial()
	if material == "" {
		return
	}
	model, err := roles.Resolve(roles.Source(a.config.RolesSource), roles.RoleIntake, a.Model())
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), intakeWindow)
	defer cancel()
	response, err := a.client.CompleteWithMessages(
		provider.WithoutStream(ctx),
		[]ai.Message{
			textMessage("system", intakeSystem),
			textMessage("user", "The form:\n"+intakeFields(*card)+
				"\n\nWhat has been said:\n"+material+
				"\n\n"+intakePrompt),
		},
		ai.WithModel(model), ai.WithJSONMode())
	if err != nil || response == nil {
		return
	}
	a.addAuxiliaryUsageAs(response, model, 1, auxRoleIntake)
	applyIntakeAnswer(card, response.Text())
}

// intakeFields is the form as the filler is shown it: one line per field with
// its type, what it is for, and whether it has to be answered.
//
// THE FIELDS ALREADY ANSWERED ARE SHOWN TOO, and shown as answered. A filler
// that could not see them would re-derive them from the same material and
// sometimes disagree with the default the program declared — and the card would
// then show a value nobody chose over one the program did.
func intakeFields(card SubharnessCard) string {
	var out strings.Builder
	for _, field := range card.Fields {
		out.WriteString("- " + field.Field.Name)
		if kind := strings.TrimSpace(field.Field.Type); kind != "" {
			out.WriteString(" (" + kind + ")")
		}
		if field.Field.Required {
			out.WriteString(" — required")
		}
		for _, line := range []string{field.Field.Title, field.Field.Description} {
			if line = strings.TrimSpace(line); line != "" {
				out.WriteString(" — " + line)
			}
		}
		if len(field.Field.Enum) > 0 {
			out.WriteString(" — one of: " + strings.Join(field.Field.Enum, ", "))
		}
		if len(field.Value) > 0 {
			out.WriteString("\n  already answered: " + clip(string(field.Value), 200))
		}
		out.WriteString("\n")
	}
	return out.String()
}

// intakeMaterial is the recent conversation as plain text, newest last.
//
// IT READS THE TAIL AND NOT THE HEAD, which is where this differs from the
// namer beside it (title.go's firstExchangeLocked): a session's NAME is about
// what the conversation has been about all along, and a form is filled from what
// was just said. Tool results are left out — they are the machine's own output
// and a filler reading one would put a file listing into a field somebody meant
// to name one file.
func (a *Agent) intakeMaterial() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	lines := make([]string, 0, intakeMessages)
	for index := len(a.messages) - 1; index >= 0 && len(lines) < intakeMessages; index-- {
		message := a.messages[index]
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		text := strings.TrimSpace(messageContentText(message))
		if text == "" {
			continue
		}
		lines = append(lines, message.Role+": "+clip(text, intakeClip))
	}
	// Reversed, because the material reads forwards even though it was gathered
	// backwards, and a model handed a conversation in reverse answers about the
	// wrong end of it.
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
	return strings.Join(lines, "\n\n")
}

// applyIntakeAnswer puts what came back into the card, and refuses everything it
// cannot place.
//
// A NAME THE FORM DOES NOT HAVE IS DROPPED IN SILENCE. The models this lands on
// are the cheapest ones configured and inventing a field is an ordinary way for
// one of them to go wrong; a card carrying a field its program never declared
// would be a launch that fails at the runner with a message about a schema the
// person never saw.
func applyIntakeAnswer(card *SubharnessCard, answer string) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return
	}
	var filled map[string]json.RawMessage
	if err := json.Unmarshal([]byte(answer), &filled); err != nil {
		return
	}
	for index := range card.Fields {
		value, said := filled[card.Fields[index].Field.Name]
		if !said || len(value) == 0 || string(value) == "null" {
			continue
		}
		// A FILLED FIELD OVERRIDES A DEFAULT AND SAYS SO. The default was the
		// program's answer for a question nobody answered; the conversation just
		// answered it, and the card draws the two differently.
		card.Fields[index].Value = value
		card.Fields[index].Filled = true
	}
	card.Missing = missingSubharnessFields(card.Fields)
}

// SubharnessInput is the card as the runner takes it: one JSON object of the
// fields that have a value.
//
// IT IS THE ONE PLACE A CARD BECOMES AN INPUT, so a surface that lets somebody
// edit a field inline and a surface that just confirms what was inferred are
// launching the same object. A field with no value is LEFT OUT rather than sent
// as null — a program's optional field being absent and being explicitly nothing
// are two different instructions, and only one of them was given.
func SubharnessInput(card SubharnessCard) json.RawMessage {
	fields := make(map[string]json.RawMessage, len(card.Fields))
	for _, field := range card.Fields {
		if len(field.Value) == 0 {
			continue
		}
		fields[field.Field.Name] = field.Value
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return json.RawMessage("{}")
	}
	return encoded
}
