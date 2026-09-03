package shaped

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// scripted is one model with its answers written down, and a record of what
// each call actually went out with — the ceiling it was given and the messages
// it carried. Those two are the whole of what this package does, so they are the
// whole of what these tests read.
type scripted struct {
	replies  []*ai.Response
	ceilings []int
	sent     [][]ai.Message
	formats  []string
}

func (s *scripted) CompleteWithMessages(_ context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	request := &ai.Request{}
	for _, option := range options {
		if err := option(request); err != nil {
			return nil, err
		}
	}
	ceiling := 0
	if request.MaxTokens != nil {
		ceiling = *request.MaxTokens
	}
	s.ceilings = append(s.ceilings, ceiling)
	s.sent = append(s.sent, messages)
	format := ""
	if request.ResponseFormat != nil {
		format = request.ResponseFormat.Type
	}
	s.formats = append(s.formats, format)
	index := len(s.ceilings) - 1
	if index >= len(s.replies) {
		index = len(s.replies) - 1
	}
	return s.replies[index], nil
}

func cut(text string, spent int) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: text}}}, FinishReason: "length"}},
		Usage: &ai.Usage{CompletionTokens: spent},
	}
}

func whole(text string) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: text}}}, FinishReason: "stop"}},
		Usage: &ai.Usage{CompletionTokens: 40},
	}
}

type parts struct {
	Parts []struct {
		Title string `json:"title"`
	} `json:"parts"`
}

const partsSchema = `{"type":"object","properties":{"parts":{"type":"array"}}}`

// THE FIRST OF THE TWO SHAPES THAT KILLED A RUN. The fan-out's answer was cut at
// the output ceiling with the object half written; the harness re-bought the
// same answer, hit the same wall, and the run exited with zero nodes. The
// expensive half of that reply was already in hand, so what is bought now is the
// REST of it, and the two halves are joined into one object.
func TestACutAnswerIsContinuedFromWhereItStopped(t *testing.T) {
	client := &scripted{replies: []*ai.Response{
		cut(`{"parts":[{"title":"read the failing test"},{"title":"fix the fol`, 8192),
		whole(`low state"}]}`),
	}}
	var decoded parts
	if _, err := Answer(context.Background(), client, Ask{
		Lane: "plan", Schema: json.RawMessage(partsSchema), Routed: true, Answers: 5,
	}, &decoded); err != nil {
		t.Fatalf("a cut answer must be continued, not surfaced as a failure: %v", err)
	}
	if len(decoded.Parts) != 2 || decoded.Parts[1].Title != "fix the follow state" {
		t.Fatalf("the halves were not joined: %+v", decoded)
	}
	if len(client.ceilings) != 2 {
		t.Fatalf("calls = %d, want the attempt and one continuation", len(client.ceilings))
	}
	// The continuation carries what was already said, as the assistant turn it
	// was, and asks for the rest. Without both, the model has no way to know
	// where to resume from.
	second := client.sent[1]
	if last := second[len(second)-1]; !strings.Contains(text(&ai.Response{Choices: []ai.Choice{{Message: last}}}), "cut off") {
		t.Fatalf("the continuation did not ask for the rest: %+v", last)
	}
	if prior := second[len(second)-2]; prior.Role != "assistant" ||
		!strings.HasSuffix(prior.Content[0].Text, `"fix the fol`) {
		t.Fatalf("the continuation did not carry the fragment it continues: %+v", prior)
	}
}

// A model that ignores the instruction and re-sends the whole object has still
// answered. Joining that onto the fragment would produce nonsense, so it is
// recognised and taken on its own.
func TestAContinuationThatRestartsTheObjectIsTakenWhole(t *testing.T) {
	client := &scripted{replies: []*ai.Response{
		cut(`{"parts":[{"title":"read the fail`, 8192),
		whole(`{"parts":[{"title":"read the failing test"}]}`),
	}}
	var decoded parts
	if _, err := Answer(context.Background(), client, Ask{Lane: "plan",
		Schema: json.RawMessage(partsSchema), Routed: true}, &decoded); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(decoded.Parts) != 1 || decoded.Parts[0].Title != "read the failing test" {
		t.Fatalf("the restarted object was mangled: %+v", decoded)
	}
}

// Prose with the object somewhere inside it is not a failure at all — that
// tolerance belongs to the provider boundary and every structured call in the
// system already has it. What must not happen is a second call being bought for
// an answer that was already there.
func TestProseAroundACompleteObjectCostsNothing(t *testing.T) {
	client := &scripted{replies: []*ai.Response{
		whole("Here is my answer:\n```json\n{\"parts\":[{\"title\":\"one\"}]}\n```\nHope that helps."),
	}}
	var decoded parts
	if _, err := Answer(context.Background(), client, Ask{Lane: "plan",
		Schema: json.RawMessage(partsSchema), Routed: true}, &decoded); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(client.ceilings) != 1 {
		t.Fatalf("calls = %d, want one — the answer was already in hand", len(client.ceilings))
	}
	if len(decoded.Parts) != 1 {
		t.Fatalf("decoded %+v", decoded)
	}
}

// Prose with NO object in it is the other repair: there is nothing to continue,
// so the model is asked again once, with its own words quoted back and the
// format contract stated. One re-ask, and the answer it then gives stands.
func TestProseWithNoObjectIsAskedAgainOnceWithItsOwnWordsQuoted(t *testing.T) {
	client := &scripted{replies: []*ai.Response{
		whole("I think the best approach here is to read the failing test first."),
		whole(`{"parts":[{"title":"read the failing test"}]}`),
	}}
	var decoded parts
	if _, err := Answer(context.Background(), client, Ask{Lane: "plan",
		Schema: json.RawMessage(partsSchema), Routed: true}, &decoded); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(client.ceilings) != 2 {
		t.Fatalf("calls = %d, want the attempt and one re-ask", len(client.ceilings))
	}
	asked := client.sent[1][len(client.sent[1])-1].Content[0].Text
	if !strings.Contains(asked, "I think the best approach") {
		t.Fatalf("the re-ask did not quote what was wrong:\n%s", asked)
	}
	if !strings.Contains(asked, "exactly one JSON object") {
		t.Fatalf("the re-ask did not state the contract:\n%s", asked)
	}
	if !strings.Contains(asked, partsSchema) {
		t.Fatalf("the re-ask did not carry the shape:\n%s", asked)
	}
	// A re-ask is the one place the doubling law still applies: the first
	// attempt may have run out of room as well as out of shape.
	if client.ceilings[1] <= client.ceilings[0] {
		t.Fatalf("the re-ask got no more room: %v", client.ceilings)
	}
}

// THE SECOND OF THE TWO SHAPES THAT KILLED A RUN, and the reason ErrUnreadable
// is a type. A model that will not answer in shape is a FAULT the caller has to
// deal with — the delivery gate used to read it as "no opinion" and ship the
// work as done.
func TestAModelThatNeverAnswersInShapeIsATypedFault(t *testing.T) {
	client := &scripted{replies: []*ai.Response{whole("no thanks"), whole("still no")}}
	var decoded parts
	_, err := Answer(context.Background(), client, Ask{Lane: "gate",
		Schema: json.RawMessage(partsSchema), Routed: true}, &decoded)
	if err == nil {
		t.Fatal("an unreadable answer must not be returned as a success")
	}
	if !Unreadable(err) {
		t.Fatalf("the fault is not typed: %v", err)
	}
	if !strings.Contains(err.Error(), "gate request") {
		t.Fatalf("the fault does not name the lane that asked: %v", err)
	}
	if len(client.ceilings) != 2 {
		t.Fatalf("calls = %d, want the attempt and exactly one re-ask", len(client.ceilings))
	}
}

// Every repair is announced, because a run that repaired itself twice must not
// look like one that never had to (FAILSAFE clauses 3 and 4). The words are the
// ones a person reads on the stream.
func TestEveryRepairIsAnnouncedToTheJournal(t *testing.T) {
	client := &scripted{replies: []*ai.Response{
		cut(`{"parts":[{"title":"a`, 8192),
		whole("prose, sorry"),
		whole("still prose"),
	}}
	var seen []Repair
	ctx := WithJournal(context.Background(), JournalFunc(func(r Repair) { seen = append(seen, r) }))
	var decoded parts
	if _, err := Answer(ctx, client, Ask{Lane: "plan",
		Schema: json.RawMessage(partsSchema), Routed: true}, &decoded); !Unreadable(err) {
		t.Fatalf("want the typed fault, got %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("repairs announced = %d, want the continuation, the re-ask and the fault: %+v", len(seen), seen)
	}
	if got, want := seen[0].Line(), "plan: answer cut at the ceiling — continued"; got != want {
		t.Fatalf("the stream line = %q, want %q", got, want)
	}
	if seen[1].Kind != RepairReasked || seen[2].Kind != RepairFailed {
		t.Fatalf("the repairs are not in the order they happened: %+v", seen)
	}
}

// A call with nobody listening is the ordinary case — every path without a store
// is one — and it must be exactly as correct and no slower.
func TestARepairWithNoJournalIsStillARepair(t *testing.T) {
	client := &scripted{replies: []*ai.Response{cut(`{"parts":[`, 8192), whole(`]}`)}}
	var decoded parts
	if _, err := Answer(context.Background(), client, Ask{Lane: "plan",
		Schema: json.RawMessage(partsSchema), Routed: true}, &decoded); err != nil {
		t.Fatalf("Answer: %v", err)
	}
}

// ── the ceiling ──────────────────────────────────────────────────────────────

// THE DERIVATION, AND THE PROPERTY THAT MAKES IT SAFE TO ADOPT EVERYWHERE: a
// one-object ask comes out of it with exactly the ceiling every one-object call
// in the system already had. Nothing anybody measured moves; only the asks that
// ask for MORE than one object get more room, which is the whole finding.
func TestOneObjectGetsTheShareTheGateMeasuredAndAListGetsMore(t *testing.T) {
	reserve := ctxbudget.CompletionReserve()
	one := Room(Ask{Lane: "gate"}, "")
	if want := reserve / objectShare; one != want {
		t.Fatalf("one object gets %d, want the reserve's share %d", one, want)
	}
	five := Room(Ask{Lane: "plan", Answers: 5}, "")
	if five != 5*one && five != reserve {
		t.Fatalf("five objects get %d, want five objects' worth (%d) or the reserve (%d)", five, 5*one, reserve)
	}
	if five <= one {
		t.Fatal("an ask for five parts was given the room for one — the fan-out's exact failure")
	}
	// The reserve is the one figure an operator states about how large a
	// completion may be, and nothing derived here may outrun it.
	if Room(Ask{Lane: "plan", Answers: 1000}, "") > reserve {
		t.Fatal("the derivation outran the reserve")
	}
}

// The echo term, which is the intent compiler's discovery: an answer that has to
// carry the request back verbatim cannot be smaller than the request, however
// small its schema is.
func TestAnAnswerThatEchoesTheAskIsSizedForTheAsk(t *testing.T) {
	short := Room(Ask{Lane: "compile", Echo: "fix the test"}, "")
	long := Room(Ask{Lane: "compile", Echo: strings.Repeat("a long instruction with many words in it ", 200)}, "")
	if long <= short {
		t.Fatalf("a long ask got no more room than a short one: %d vs %d", long, short)
	}
}

// A reserve an operator lowered on purpose is still their word, and the floor
// under one object never becomes a licence to overrun it.
func TestTheOperatorsReserveIsNeverOverrun(t *testing.T) {
	t.Setenv("AFORGE_COMPLETION_RESERVE", "8000")
	if got := Room(Ask{Lane: "gate"}, ""); got != objectFloor {
		t.Fatalf("one object under a small reserve = %d, want the floor %d", got, objectFloor)
	}
	t.Setenv("AFORGE_COMPLETION_RESERVE", "1000")
	if got := Room(Ask{Lane: "gate", Answers: 4}, ""); got != 1000 {
		t.Fatalf("room = %d, want the stated reserve of 1000", got)
	}
}

// A MODEL THAT NEVER CLOSES ITS OBJECT MUST STILL STOP.
//
// The round law is derived from what the answer spends, and a provider that
// reports no usage at all is the case where a naive reading of that makes the
// bound stop bounding. A reply that was cut spent the room it was given, whether
// or not anybody said so, so the continuations end and the caller gets the
// typed fault rather than a run that never comes back.
func TestAnAnswerThatNeverClosesStillEndsInAFault(t *testing.T) {
	forever := &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: `{"parts":[{"title":"a`}}}, FinishReason: "length"}},
	}
	client := &scripted{replies: []*ai.Response{forever}}
	var decoded parts
	_, err := Answer(context.Background(), client, Ask{Lane: "plan",
		Schema: json.RawMessage(partsSchema), Routed: true}, &decoded)
	if !Unreadable(err) {
		t.Fatalf("want the typed fault, got %v", err)
	}
	// reserve÷ceiling continuations, then the one re-ask. What matters is that
	// it is bounded and small, not the exact figure.
	if rounds := len(client.ceilings); rounds > 1+ctxbudget.CompletionReserve()/objectFloor {
		t.Fatalf("the continuation loop ran %d times — the round law is not bounding", rounds)
	}
}

// THE MEMO HAS THE LAST WORD, AND IT IS SOURCED FROM WHAT HAPPENED.
//
// A derivation is an argument; a cut is a measurement. A model this profile has
// watched overrun a lane's ceiling is never sent that ceiling again, and what it
// is sent instead is what it actually spent, doubled — the same arithmetic every
// retry in this tree has always used. Nothing here shrinks a ceiling: a model
// that answered inside its room teaches nothing, and forgetting a cut buys the
// same failure twice.
func TestAModelWatchedOverrunningALaneIsNeverSentThatCeilingAgain(t *testing.T) {
	const model = "vendor/model-that-cuts"
	before := Room(Ask{Lane: "fan-out-memo", Answers: 1}, model)
	provider.NoteAnswerCut(model, "fan-out-memo", before)
	after := Room(Ask{Lane: "fan-out-memo", Answers: 1}, model)
	if after != 2*before && after != reserve() {
		t.Fatalf("after a cut at %d the ceiling is %d, want it doubled or the reserve", before, after)
	}
	// The memo is per model AND per lane. A model that cuts on a five-part
	// fan-out says nothing about the same model answering a one-object verdict,
	// and pooling them would raise every ceiling in the system because one ask
	// is wide.
	if other := Room(Ask{Lane: "some-other-lane"}, model); other != before {
		t.Fatalf("a cut on one lane moved another lane's ceiling: %d, want %d", other, before)
	}
	if unknown := Room(Ask{Lane: "fan-out-memo"}, "vendor/some-other-model"); unknown != before {
		t.Fatalf("a cut by one model moved another model's ceiling: %d, want %d", unknown, before)
	}
}

// "The answer was not readable" is two facts, and this seam journaled one word
// for both: a model reasoning out loud, and a caller's own contract refusing a
// well-formed answer. textual v4-flash s13 holds two of these on lane `gate`,
// and telling them apart meant reading token counts out of the usage table
// three events either side of each one.
func TestTheJournalSaysWhyAnAnswerCouldNotBeRead(t *testing.T) {
	for name, sent := range map[string]struct {
		reply string
		want  string
	}{
		"a model reasoning out loud": {
			"Looking at the deliverable, the module implements the state machine and the tests cover it.",
			"no JSON object"},
		"a contract the caller refused": {
			`{"pass":false,"gaps":"x","quote":"not a stated behaviour"}`,
			"not one of them"},
	} {
		var journaled []Repair
		ctx := WithJournal(context.Background(),
			JournalFunc(func(repair Repair) { journaled = append(journaled, repair) }))
		client := &scripted{replies: []*ai.Response{whole(sent.reply), whole(`{"ok":true}`)}}
		var into fussyDestination
		if _, err := Answer(ctx, client, Ask{Lane: "gate"}, &into); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(journaled) != 1 || journaled[0].Kind != RepairReasked {
			t.Fatalf("%s: journaled %+v", name, journaled)
		}
		if !strings.Contains(journaled[0].Note, sent.want) {
			t.Errorf("%s: note = %q, want it to name %q", name, journaled[0].Note, sent.want)
		}
	}
}

// fussyDestination is a caller whose contract refuses an answer that decoded
// perfectly well — the delivery gate's own shape, in miniature.
type fussyDestination struct{ OK bool }

func (d *fussyDestination) UnmarshalJSON(data []byte) error {
	var raw struct {
		OK    bool   `json:"ok"`
		Quote string `json:"quote"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if !raw.OK {
		return fmt.Errorf("a fail must quote one of the behaviours this request states, "+
			"and %q is not one of them", raw.Quote)
	}
	d.OK = raw.OK
	return nil
}

// A FRAGMENT HAS NO SHAPE. The attempt and the re-ask ask for an object on the
// wire; the continuation of a cut object asks for the room alone, because a
// shape hint on "emit the characters that come next" makes the model restart a
// whole object instead — measured on the intent compile, where the restarted
// object carried every field but the one the fragment held (#335).
func TestAContinuationCarriesNoShapeHint(t *testing.T) {
	client := &scripted{replies: []*ai.Response{
		cut(`{"parts":[{"title":"read the failing test"},{"title":"fix the fol`, 8192),
		whole(`low state"}]}`),
	}}
	var decoded parts
	if _, err := Answer(context.Background(), client, Ask{Lane: "plan", JSON: true}, &decoded); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(client.formats) != 2 || client.formats[0] != "json_object" || client.formats[1] != "" {
		t.Fatalf("response formats = %v, want json_object on the attempt and none on the continuation", client.formats)
	}
	client = &scripted{replies: []*ai.Response{
		whole("I cannot answer that in JSON."),
		whole("Still prose."),
	}}
	_, _ = Answer(context.Background(), client, Ask{Lane: "plan", JSON: true}, &decoded)
	if len(client.formats) != 2 || client.formats[1] != "json_object" {
		t.Fatalf("response formats = %v, want the re-ask to keep asking for an object", client.formats)
	}
}

// The fault a caller is handed carries the head of the reply, because a
// streamed call's log row carries no body and the error is the only record
// of what the model said (#335).
func TestTheFaultQuotesTheReplyItCouldNotRead(t *testing.T) {
	client := &scripted{replies: []*ai.Response{
		whole("Here is my answer, in prose, as a paragraph."),
		whole("Here is my answer, in prose, as a paragraph."),
	}}
	var decoded parts
	_, err := Answer(context.Background(), client, Ask{Lane: "plan", JSON: true}, &decoded)
	if err == nil || !strings.Contains(err.Error(), `reply="Here is my answer, in prose`) {
		t.Fatalf("err = %v, want the reply quoted in the fault", err)
	}
}

// ── re-ask quotation repair ─────────────────────────────────────────────────

// A first answer that is valid JSON but fails only the quotation rule — the
// caller's contract demands a quote and the model left it empty — is not asked
// for the whole verdict again. The re-ask asks for the missing quotation alone
// over the answer already held, and the merged result is accepted.
func TestReaskQuotationOnlyRepair(t *testing.T) {
	first := whole(`{"pass":false,"gaps":"incomplete","quote":""}`)
	quoteReply := whole("the behaviour about retries")
	client := &scripted{replies: []*ai.Response{first, quoteReply}}
	var into quoteDestination
	_, err := Answer(context.Background(), client, Ask{Lane: "gate"}, &into)
	if err != nil {
		t.Fatalf("a quotation-only repair must succeed: %v", err)
	}
	if into.Quote != "the behaviour about retries" {
		t.Fatalf("quote = %q, want the merged reply", into.Quote)
	}
	if len(client.ceilings) != 2 {
		t.Fatalf("calls = %d, want the attempt and one targeted re-ask", len(client.ceilings))
	}
	// The re-ask must ask for the quotation alone, not the whole verdict.
	asked := client.sent[1][len(client.sent[1])-1].Content[0].Text
	if !strings.Contains(asked, "quotation") && !strings.Contains(asked, "quote") {
		t.Fatalf("the re-ask did not ask for the missing quotation:\n%s", asked)
	}
	// It must carry the answer already held so the model can fill in the gap.
	if !strings.Contains(asked, `"gaps":"incomplete"`) {
		t.Fatalf("the re-ask did not carry the answer already held:\n%s", asked)
	}
}

// quoteDestination accepts any valid JSON but refuses a verdict whose quote is
// empty — the delivery gate's rule in miniature, and the exact failure this
// repair exists for.
type quoteDestination struct {
	Pass  bool   `json:"pass"`
	Gaps  string `json:"gaps"`
	Quote string `json:"quote"`
}

func (d *quoteDestination) UnmarshalJSON(data []byte) error {
	type alias quoteDestination
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Quote == "" {
		return fmt.Errorf("a fail must quote the words of the request it is a failure of")
	}
	*d = quoteDestination(raw)
	return nil
}

// The quotation re-ask names the rule the first answer broke, carried in the
// caller's own refusal note — never duplicated prose that can drift from the
// one place the rule is stated.
func TestReaskPromptNamesBrokenRule(t *testing.T) {
	// The quotation rule: the re-ask prompt must name it from the parse note.
	client := &scripted{replies: []*ai.Response{
		whole(`{"pass":false,"gaps":"incomplete","quote":""}`),
		whole("the behaviour about retries"),
	}}
	var into quoteDestination
	if _, err := Answer(context.Background(), client, Ask{Lane: "gate"}, &into); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	asked := client.sent[1][len(client.sent[1])-1].Content[0].Text
	if !strings.Contains(asked, "must quote") {
		t.Fatalf("the re-ask did not name the broken rule:\n%s", asked)
	}
}

// ── findQuotationField ──────────────────────────────────────────────────────

// findQuotationField returns "quote" when the refusal note is about a quotation
// or a missing quote, and "" for every other kind of refusal — the empty string
// is what keeps non-quotation refusals on the ordinary re-ask road.
func TestFindQuotationFieldReturnsQuoteForQuotationNote(t *testing.T) {
	if got := findQuotationField("any", "a fail must quote the words of the request"); got != "quote" {
		t.Fatalf("findQuotationField = %q, want %q", got, "quote")
	}
	if got := findQuotationField("any", "the quotation is missing"); got != "quote" {
		t.Fatalf("findQuotationField = %q, want %q", got, "quote")
	}
}

func TestFindQuotationFieldReturnsEmptyForNonQuotationNote(t *testing.T) {
	if got := findQuotationField("any", ""); got != "" {
		t.Fatalf("findQuotationField(empty) = %q, want \"\"", got)
	}
	if got := findQuotationField("any", "no JSON object found"); got != "" {
		t.Fatalf("findQuotationField(non-quotation) = %q, want \"\"", got)
	}
	if got := findQuotationField("any", "invalid character '}' looking for beginning of value"); got != "" {
		t.Fatalf("findQuotationField(parse error) = %q, want \"\"", got)
	}
}

// ── mergeQuote ──────────────────────────────────────────────────────────────

// mergeQuote takes the field name as a parameter and sets that field in the
// original object. It does not hardcode "quote", so a caller whose quotation
// field has a different name gets its own field set.
func TestMergeQuoteUsesFieldParameter(t *testing.T) {
	original := `{"pass":false,"gaps":"incomplete"}`
	merged := mergeQuote(original, "the answer is correct", "custom")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(merged), &raw); err != nil {
		t.Fatalf("mergeQuote produced invalid JSON: %v", err)
	}
	if got := string(raw["custom"]); got != `"the answer is correct"` {
		t.Fatalf("raw[\"custom\"] = %s, want \"the answer is correct\"", got)
	}
	if _, ok := raw["quote"]; ok {
		t.Fatal("mergeQuote set raw[\"quote\"] — it must not hardcode the field name")
	}
}

// ── quotation repair journal ────────────────────────────────────────────────

// When the quotation repair succeeds, RepairReasked appears exactly once in the
// journal — at the success path inside the branch. A repair that was journaled
// twice (once before the attempt and again on success) would mislead an autopsy
// into counting one repair as two.
func TestQuotationRepairThatSucceedsJournalsOnce(t *testing.T) {
	first := whole(`{"pass":false,"gaps":"incomplete","quote":""}`)
	quoteReply := whole("the behaviour about retries")
	client := &scripted{replies: []*ai.Response{first, quoteReply}}
	var seen []Repair
	ctx := WithJournal(context.Background(), JournalFunc(func(r Repair) { seen = append(seen, r) }))
	var into quoteDestination
	if _, err := Answer(ctx, client, Ask{Lane: "gate"}, &into); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	reasked := 0
	for _, r := range seen {
		if r.Kind == RepairReasked {
			reasked++
		}
	}
	if reasked != 1 {
		t.Fatalf("RepairReasked count = %d, want exactly 1 — the journal must note the repair once, not before and after the attempt", reasked)
	}
}

// When the quotation repair is attempted but the merge does not produce a valid
// answer — the model sent back something that still fails the caller's contract —
// the branch falls through to the ordinary re-ask. RepairReasked must appear
// exactly once, from the fall-through note, and not from the branch's success
// path (which is never reached).
func TestQuotationRepairThatFallsThroughJournalsOnce(t *testing.T) {
	// The first answer has an empty quote, refused by quoteDestination.
	// The quotation repair reply is also empty, so the merged result still has
	// an empty quote, which quoteDestination refuses again — DecodeJSONObject
	// fails, the branch falls through.
	first := whole(`{"pass":false,"gaps":"incomplete","quote":""}`)
	emptyReply := whole(``) // empty reply → merge keeps quote empty → still fails
	normalReply := whole(`{"pass":true,"gaps":"","quote":"the behaviour about retries"}`)
	client := &scripted{replies: []*ai.Response{first, emptyReply, normalReply}}
	var seen []Repair
	ctx := WithJournal(context.Background(), JournalFunc(func(r Repair) { seen = append(seen, r) }))
	var into quoteDestination
	if _, err := Answer(ctx, client, Ask{Lane: "gate"}, &into); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	reasked := 0
	for _, r := range seen {
		if r.Kind == RepairReasked {
			reasked++
		}
	}
	if reasked != 1 {
		t.Fatalf("RepairReasked count = %d, want exactly 1 — a fall-through must journal the re-ask once, not twice", reasked)
	}
	// The quotation repair was attempted (call 2) and the ordinary re-ask
	// succeeded (call 3).
	if len(client.ceilings) != 3 {
		t.Fatalf("calls = %d, want attempt, quotation repair, and fall-through re-ask", len(client.ceilings))
	}
}