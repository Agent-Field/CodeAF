package provider

import (
	"strings"
	"testing"
)

// THE ANSWER/WORKING SPLIT, PINNED (#225).
//
// One decision, taken with the wire in hand, spent twice: on the events a
// surface draws and on the text the transcript keeps (answer.go). Every want
// here is asked of BOTH — what the observer was handed, and what the response
// accumulated — because the defect the file closes is the two disagreeing.

// splitWire runs one response's channels through a split in wire order and hands
// back what a surface would have drawn and what a transcript would have kept.
// It is the loop in client.go with everything but the split taken out.
type wireDelta struct{ content, reasoning string }

func splitWire(deltas []wireDelta, tools bool, prose ...bool) (drawnAnswer, drawnWorking, kept string) {
	var split answerSplit
	var answer, working, content strings.Builder
	if tools {
		split.sawTools()
	}
	for _, delta := range deltas {
		gotAnswer, gotWorking := split.content(delta.content)
		if gotAnswer != "" {
			answer.WriteString(gotAnswer)
			content.WriteString(gotAnswer)
		}
		working.WriteString(gotWorking)
		if delta.reasoning != "" {
			split.reasoning(delta.reasoning)
			working.WriteString(delta.reasoning)
		}
	}
	heldAnswer, heldWorking := split.flush()
	if heldAnswer != "" {
		answer.WriteString(heldAnswer)
		content.WriteString(heldAnswer)
	}
	working.WriteString(heldWorking)
	wantsPromotion := len(prose) == 0 || prose[0]
	if promoted, ok := split.promote(wantsPromotion); ok {
		answer.WriteString(promoted)
		content.WriteString(promoted)
	}
	return answer.String(), working.String(), content.String()
}

// THE ORDINARY WIRE IS UNTOUCHED. An endpoint that splits the channels itself
// gets exactly the two streams it sent, which is the case nearly every call is.
func TestAnEndpointThatSplitsItsOwnChannelsIsLeftAlone(t *testing.T) {
	drawn, working, kept := splitWire([]wireDelta{
		{reasoning: "the user wants "},
		{reasoning: "a definition."},
		{content: "A mutex "},
		{content: "guards one thing."},
	}, false)
	if drawn != "A mutex guards one thing." {
		t.Fatalf("the answer drawn was %q", drawn)
	}
	if working != "the user wants a definition." {
		t.Fatalf("the working drawn was %q", working)
	}
	if kept != drawn {
		t.Fatalf("the transcript kept %q and the surface drew %q", kept, drawn)
	}
}

// A REPLY THAT ARRIVED ONLY ON THE REASONING CHANNEL IS THE ANSWER. It is the
// shape measured against the real router — a response with no content deltas at
// all and thousands of characters of reasoning — and reading it as an empty
// reply is a turn that costs money and tells the person nothing.
func TestAReplyThatArrivedOnlyOnTheReasoningChannelIsTheAnswer(t *testing.T) {
	drawn, _, kept := splitWire([]wireDelta{
		{reasoning: "A mutex guards "},
		{reasoning: "one thing at a time."},
	}, false)
	if drawn != "A mutex guards one thing at a time." {
		t.Fatalf("the working was never promoted to an answer: %q", drawn)
	}
	if kept != drawn {
		t.Fatalf("the transcript kept %q and the surface drew %q", kept, drawn)
	}
}

// AND A TURN THAT CALLED SOMETHING IS NOT A TURN THAT LOST ITS ANSWER. Silence
// beside a tool call is a model behaving, and promoting its working would type
// private reasoning into the transcript as though it had been said out loud.
func TestAToolCallWithNoWordsBesideItPromotesNothing(t *testing.T) {
	drawn, working, kept := splitWire([]wireDelta{
		{reasoning: "I should read the file first."},
	}, true)
	if drawn != "" || kept != "" {
		t.Fatalf("a tool-calling turn was given an answer out of its working: %q / %q", drawn, kept)
	}
	if working != "I should read the file first." {
		t.Fatalf("the working drawn was %q", working)
	}
}

// A `<think>` FENCE IN THE ANSWER CHANNEL IS WORKING, and neither the tag nor
// what it holds reaches the answer. It is what every gateway in front of a raw
// open model produces, and it arrives split across deltas at arbitrary points.
func TestWorkingFencedInTheAnswerChannelIsNeverTheAnswer(t *testing.T) {
	drawn, working, kept := splitWire([]wireDelta{
		{content: "<thi"},
		{content: "nk>let me work "},
		{content: "this out</thi"},
		{content: "nk>\n\nA mutex guards one thing."},
	}, false)
	if drawn != "A mutex guards one thing." {
		t.Fatalf("the answer drawn was %q", drawn)
	}
	if working != "let me work this out" {
		t.Fatalf("the working drawn was %q", working)
	}
	if kept != drawn {
		t.Fatalf("the transcript kept %q and the surface drew %q", kept, drawn)
	}
	if strings.Contains(kept, "<think") {
		t.Fatalf("the tag reached the transcript: %q", kept)
	}
}

// A FENCE THAT NEVER CLOSED IS THE SAME SHAPE AS A REASONING-ONLY REPLY, and it
// is answered by the same rule rather than by a second one: the response said
// nothing and thought at length, so the thinking was the reply.
func TestAFenceThatNeverClosedStillLeavesAnAnswer(t *testing.T) {
	drawn, _, kept := splitWire([]wireDelta{
		{content: "<think>A mutex guards one thing at a time."},
	}, false)
	if drawn != "A mutex guards one thing at a time." {
		t.Fatalf("an unclosed fence swallowed the whole reply: %q", drawn)
	}
	if kept != drawn {
		t.Fatalf("the transcript kept %q and the surface drew %q", kept, drawn)
	}
}

// AND AN ANSWER THAT TALKS ABOUT THE TAG KEEPS ITS OWN WORDS. The fence may
// only open while nothing has been said; a `<think>` in the fourth paragraph is
// a model answering a question about it, and an answer eaten for quoting a tag
// would be a worse defect than the one this closes.
func TestAnAnswerThatMentionsTheTagIsNotEatenByIt(t *testing.T) {
	drawn, working, _ := splitWire([]wireDelta{
		{content: "Some gateways emit "},
		{content: "<think> and never strip it."},
	}, false)
	if drawn != "Some gateways emit <think> and never strip it." {
		t.Fatalf("the answer lost its own words: %q", drawn)
	}
	if working != "" {
		t.Fatalf("a mentioned tag was read as working: %q", working)
	}
}

// A HALF-SPELLED TAG AT THE END OF A STREAM IS TEXT THE MODEL WROTE. Nothing
// withheld against a fence may be lost to a fence that never arrived.
func TestAHalfSpelledTagAtTheEndOfTheStreamIsGivenBack(t *testing.T) {
	drawn, _, kept := splitWire([]wireDelta{{content: "<thi"}}, false)
	if drawn != "<thi" {
		t.Fatalf("the withheld bytes were dropped: %q", drawn)
	}
	if kept != drawn {
		t.Fatalf("the transcript kept %q and the surface drew %q", kept, drawn)
	}
}

// A RESPONSE THAT SAID NOTHING AND THOUGHT NOTHING IS STILL EMPTY. The
// promotion invents nothing: an endpoint that answered with silence is a failed
// call, and the taxonomy that reads it as one must go on being able to.
func TestASilentResponseIsStillSilent(t *testing.T) {
	drawn, working, kept := splitWire(nil, false)
	if drawn != "" || working != "" || kept != "" {
		t.Fatalf("silence became %q / %q / %q", drawn, working, kept)
	}
}

// AND A CALL THAT ASKED FOR A VALUE GETS NO PROMOTION. A gate asks for
// `{"work": false}` and its answer is read by a parser: a model that
// deliberated in its working and never wrote the object did not answer, and
// handing the deliberation over would be a task started off a draft.
func TestACallThatAsksForAValueIsNeverGivenTheWorkingAsItsAnswer(t *testing.T) {
	drawn, _, kept := splitWire([]wireDelta{
		{reasoning: `this looks like work. maybe {"work": true}? no, it is a question.`},
	}, false, false)
	if drawn != "" || kept != "" {
		t.Fatalf("a value slot was filled with the model's working: %q / %q", drawn, kept)
	}
}
