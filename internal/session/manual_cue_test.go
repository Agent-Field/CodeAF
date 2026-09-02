package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/manual/asked"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A QUESTION ABOUT AFORGE ARRIVES WITH THE MANUAL'S OWN TITLES BESIDE IT.
//
// This is the whole mechanism, tested where it is true: in the messages the
// turn actually reasons from. The model is told nothing — it is shown that the
// pages exist, which is the one thing about this corpus it cannot know.
func TestAQuestionAboutAforgeReachesTheModelWithTheManualsTitles(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{finalText("nobody but you")}}
	agent, _ := newTestAgent(t, completer, nil)

	events, err := agent.Submit(context.Background(), "who can see my files in aforge")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	opening := firstUserText(t, completer)
	if !strings.HasPrefix(opening, "who can see my files in aforge") {
		t.Fatalf("the person's own sentence is no longer the head of the message:\n%s", opening)
	}
	if !strings.Contains(opening, manualCueOpening) {
		t.Fatalf("the manual's titles never reached the model:\n%s", opening)
	}
	if !strings.Contains(opening, "[permissions · ") {
		t.Fatalf("the page that answers this question was not among the titles:\n%s", opening)
	}
}

// AND AN ORDINARY TURN IS BYTE-IDENTICAL TO WHAT IT WAS. The gate is the
// corpus's own vocabulary, and a sentence that reaches for none of it pays
// nothing at all — which is the bargain that lets this ride on every turn.
func TestATurnThatIsNotAboutAforgeCarriesNoCue(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{finalText("Paris")}}
	agent, _ := newTestAgent(t, completer, nil)

	events, err := agent.Submit(context.Background(), "what is the capital of France")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if opening := firstUserText(t, completer); opening != "what is the capital of France" {
		t.Fatalf("an uncued turn carried something the person did not type:\n%s", opening)
	}
}

// THE PERSON NEVER SEES IT, AND NEITHER DOES THE CONVERSATION. The transcript
// and the journal — what a resume replays, what an export writes, what the
// screen is drawn from — hold their sentence and nothing else, because the
// block is attached to one turn's copy of the messages and never to the
// messages themselves.
func TestTheCueNeverReachesTheJournalOrTheTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{finalText("noted")}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })

	events, err := agent.Submit(context.Background(), "how do I set a spending limit")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	live := append([]ai.Message(nil), agent.messages...)
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the journal was never written: %v", err)
	}
	written := string(raw)
	if !strings.Contains(written, "how do I set a spending limit") {
		t.Fatalf("the journal lost the person's sentence:\n%s", written)
	}
	if strings.Contains(written, manualCueMark) {
		t.Fatalf("the journal kept the block the person never typed:\n%s", written)
	}
	for _, message := range live {
		if strings.Contains(messageContentText(message), manualCueMark) {
			t.Fatalf("the transcript kept the block:\n%s", messageContentText(message))
		}
	}
}

// AND IT DIES WITH THE TURN THAT BOUGHT IT. A conversation that kept every cue
// would pay for yesterday's hints on every request it ever makes again, and a
// hint about a question already answered is the worst prompt bytes there are:
// still there, no longer about anything.
func TestTheCueDoesNotOutliveItsTurn(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		finalText("nobody but you"),
		finalText("you are welcome"),
	}}
	agent, _ := newTestAgent(t, completer, nil)

	for _, said := range []string{"who can see my files in aforge", "thanks"} {
		events, err := agent.Submit(context.Background(), said)
		if err != nil {
			t.Fatalf("Submit(%q): %v", said, err)
		}
		collect(t, events)
	}

	second := completer.request(1)
	if len(second) == 0 {
		t.Fatal("the second turn never reached the model")
	}
	for _, message := range second {
		if strings.Contains(messageContentText(message), manualCueMark) {
			t.Fatalf("the first turn's cue was still being paid for on the second:\n%s",
				messageContentText(message))
		}
	}
}

// AND NEITHER DOES THE PERSON'S ASK. [Agent.taskRequest] is what work handed out
// this turn is briefed with, and what the manual's own lookup reads beside the
// model's query (#307) — so a cue folded into it would put the corpus's headings
// into a worker's brief and search the manual with its own index.
func TestTheCueDoesNotBecomeWhatThePersonAskedFor(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{finalText("noted")}}
	agent, _ := newTestAgent(t, completer, nil)

	events, err := agent.Submit(context.Background(), "how do I split a job into parts")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if asked := agent.taskRequest(); asked != "how do I split a job into parts" {
		t.Fatalf("the person's ask became something they did not say: %q", asked)
	}
}

// THE CUE IS BOUNDED TWICE, and both bounds are checked against every question
// the free floors are measured on rather than against one example: a corpus
// grows, and the day a heading doubles in length is the day an unbounded block
// would double with it.
func TestTheCueStaysInsideItsCaps(t *testing.T) {
	for _, question := range everyPlainQuestion() {
		block := manualCueFor(question)
		if block == "" {
			continue
		}
		if len(block) > manualCueCap {
			t.Errorf("%q cued %d bytes, past the %d cap", question, len(block), manualCueCap)
		}
		lines := strings.Split(strings.TrimPrefix(block, manualCueOpening), "\n")
		if len(lines) > manualCueSections {
			t.Errorf("%q cued %d titles, past the %d it may show", question, len(lines), manualCueSections)
		}
		for _, line := range lines {
			_, clause, found := strings.Cut(line, "] ")
			if !found || !strings.HasPrefix(line, "[") {
				t.Errorf("%q cued a line that is not a titled one: %q", question, line)
				continue
			}
			if len(clause) > manualCueLineCap {
				t.Errorf("%q cued a clause of %d bytes, past the %d cap: %q",
					question, len(clause), manualCueLineCap, clause)
			}
		}
	}
}

// AND A MESSAGE THE CORPUS HAS NOTHING FOR IS A MESSAGE WITH NOTHING BESIDE IT.
// Empty, whitespace and a sentence in nobody's vocabulary all take the same
// road out, because a heading over nothing is the emptiness law broken in the
// model's house rather than in a person's.
func TestTheCueIsEmptyWhereTheCorpusHasNothing(t *testing.T) {
	for _, message := range []string{"", "   \n\t ", "what is the capital of France", "zzzzz qqqqq"} {
		if block := manualCueFor(message); block != "" {
			t.Errorf("%q cued something out of a corpus that has nothing for it:\n%s", message, block)
		}
	}
}

// A GATE WITH ONE ANSWER. The cue fires exactly where [manual.Corpus.Cued]
// fires and the corpus returns something, and this is that agreement written
// down: the day somebody adds a second reading of "is this about the product",
// the two will disagree and this is what says so.
func TestTheCueFiresWhereTheCorpusSaysTheMessageIsAboutIt(t *testing.T) {
	for _, question := range everyPlainQuestion() {
		cued, block := manual.Chat().Cued(question), manualCueFor(question)
		if cued != (block != "") {
			t.Errorf("%q: the corpus says cued=%v and the block is %d bytes", question, cued, len(block))
		}
	}
}

// AND IT IS ABSENT WHERE IT WOULD BE ANSWERING NOBODY. A task node's opening
// message is a brief rather than a question, and the switch is what lets the
// wire lane measure a pass without it on the same night and the same model.
func TestTheCueIsAbsentInsideATaskAndWhereItIsSwitchedOff(t *testing.T) {
	for _, one := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"switched off", func(config *Config) { config.ManualCueOff = true }},
		{"inside a task", func(config *Config) { config.InTask = true }},
	} {
		t.Run(one.name, func(t *testing.T) {
			completer := &scriptedCompleter{steps: []step{finalText("noted")}}
			agent, _ := newTestAgent(t, completer, one.mutate)
			events, err := agent.Submit(context.Background(), "who can see my files in aforge")
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			collect(t, events)
			if opening := firstUserText(t, completer); opening != "who can see my files in aforge" {
				t.Fatalf("%s still carried a cue:\n%s", one.name, opening)
			}
		})
	}
}

// withoutManualCue is what a person's message was before the manual's titles
// joined it for one request. Every assertion in this package about "what the
// model was sent" that is really about WHAT THE PERSON TYPED goes through it,
// for the reason the block exists at all: it is not a line of the conversation,
// and a test that read it as one would be asserting on a sentence nobody typed.
func withoutManualCue(text string) string {
	said, _, _ := strings.Cut(text, "\n\n"+manualCueMark)
	return said
}

// everyPlainQuestion is the forty-seven questions the free floors and the wire
// lane are both measured on, read out of [asked] rather than copied: a second
// copy of that list is exactly the drift that package exists to prevent.
func everyPlainQuestion() []string {
	questions := make([]string, 0, len(asked.Plain)+len(asked.HeldOut))
	for _, one := range append(append([]asked.Question{}, asked.Plain...), asked.HeldOut...) {
		questions = append(questions, one.Ask)
	}
	return questions
}
