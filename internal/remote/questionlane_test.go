package remote

// The bug these pin, and it is the questions wave's headline: since the session
// host landed, a plain `aforge` in a project is a SURFACE talking to this
// machine's engine over a unix socket — and this wire carried ResolveQuestion
// and neither WatchQuestions nor OpenQuestions. internal/tui3 asserts the three
// as one seam, so the assertion failed, no question was ever drawn, and every
// `ask` stopped the turn with nothing on any screen. They follow
// standinglane_test.go, which pinned the same fault in the harness lane.

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// askingAgent is a [fakeAgent] carrying the questions half of a real session
// agent: the standing lane, the replay of what is still open onto every new
// subscription, and the one door an answer of any lane goes through.
type askingAgent struct {
	*fakeAgent

	mu       sync.Mutex
	lanes    []chan session.Event
	opened   int
	open     []session.Question
	answered []session.Answer
}

func newAskingAgent(open ...session.Question) *askingAgent {
	return &askingAgent{fakeAgent: &fakeAgent{}, open: open}
}

// WatchQuestions replays what is already open before its first live event,
// exactly as [session.Agent.WatchQuestions] does — that replay is what makes a
// question survive an attach, and half these tests are about it.
func (a *askingAgent) WatchQuestions() (<-chan session.Event, func()) {
	lane := make(chan session.Event, 32)
	a.mu.Lock()
	a.opened++
	a.lanes = append(a.lanes, lane)
	for _, open := range a.open {
		open := open
		lane <- session.Event{Kind: session.EventQuestion, ID: open.ID, Question: &open}
	}
	a.mu.Unlock()
	var once sync.Once
	return lane, func() {
		once.Do(func() {
			a.mu.Lock()
			for at, one := range a.lanes {
				if one == lane {
					a.lanes = append(a.lanes[:at], a.lanes[at+1:]...)
					break
				}
			}
			a.mu.Unlock()
			close(lane)
		})
	}
}

func (a *askingAgent) ResolveQuestion(answer session.Answer) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.answered = append(a.answered, answer)
	return nil
}

// raise puts one event on every open lane, with NO TURN RUNNING — which is the
// shape most questions really have.
func (a *askingAgent) raise(event session.Event) {
	a.mu.Lock()
	lanes := append([]chan session.Event(nil), a.lanes...)
	a.mu.Unlock()
	for _, lane := range lanes {
		lane <- event
	}
}

func (a *askingAgent) subscriptions() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opened
}

func (a *askingAgent) heard() []session.Answer {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]session.Answer(nil), a.answered...)
}

// askedAt is a fixed clock, rounded so a time that has crossed as text compares
// equal to the one that went in — the monotonic reading a wire cannot carry is
// not part of what is being asserted here.
var askedAt = time.Date(2026, 9, 9, 14, 2, 0, 0, time.UTC)

// wholeQuestion is a question with something in every field a surface draws
// from, so a field this wire quietly drops fails a test rather than a screen.
func wholeQuestion() session.Question {
	confidence := session.Pick{Key: "2", Reason: "the record already leans this way", Confidence: "medium", WouldChange: "a benchmark under 40ms"}
	return session.Question{
		ID:     7,
		Kind:   session.QuestionAsk,
		Ask:    session.AskChoice,
		Form:   session.FormCard,
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "which storage shape should this use?",
		Reason: "two shapes are viable and the record does not choose between them",
		Subject: session.SubjectRef{
			Kind: session.SubjectTask, ID: 4, Name: "port the parser",
		},
		Options: []session.AnswerOption{
			{Key: "1", Label: "sqlite", Body: "one file, a query language", Consequence: "a dependency", Dimensions: map[string]string{"speed": "fast"}},
			{Key: "2", Label: "jsonl", Body: "append only", Safe: true},
		},
		Input:    session.InputShape{Kind: session.InputText, Prompt: "or say what you would rather"},
		Pick:     &confidence,
		Stakes:   session.StakesCostly,
		Policy:   session.Policy{Kind: session.PolicyAsk},
		Blocking: session.Blocking{Turn: true, Tasks: []string{"port the parser"}},
		Scope:    []session.AnswerScope{session.ScopeOnce, session.ScopeProject},
		Attach:   []session.Block{{Kind: session.BlockTable, Title: "what each costs", Rows: [][]string{{"sqlite", "1 dep"}, {"jsonl", "none"}}}},
		Asked:    askedAt,
	}
}

// A question raised with no turn running reaches a hosted surface WHOLE. Before
// this lane it reached nothing at all: the event was emitted onto a subscription
// this wire did not carry, so the road a plain `aforge` takes drew no block, no
// chip and no row, and the turn simply stopped.
func TestAQuestionReachesAHostedSurfaceWhole(t *testing.T) {
	far := newAskingAgent()
	loop := laneLoop(t, far)

	lane, stop := loop.Client.Agent().WatchQuestions()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the questions lane", func() bool { return far.subscriptions() == 1 })

	asked := wholeQuestion()
	far.raise(session.Event{Kind: session.EventQuestion, ID: asked.ID, Question: &asked})

	event := nextLane(t, lane)
	if event.Kind != session.EventQuestion || event.Question == nil {
		t.Fatalf("the questions lane carried %v with question %v", event.Kind, event.Question)
	}
	if !reflect.DeepEqual(*event.Question, asked) {
		t.Fatalf("the question arrived changed:\n got %+v\nwant %+v", *event.Question, asked)
	}
}

// A question withdrawn and a question answered cross too, and the answer crosses
// WHOLE — the record a second window reads is the whole of what somebody meant,
// not the key they pressed.
func TestAWithdrawalAndAnAnsweredRecordReachAHostedSurface(t *testing.T) {
	far := newAskingAgent()
	loop := laneLoop(t, far)

	lane, stop := loop.Client.Agent().WatchQuestions()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the questions lane", func() bool { return far.subscriptions() == 1 })

	gone := wholeQuestion()
	gone.Withdrawn = &session.Withdrawal{Reason: "the file it was about was deleted", By: "the model", At: askedAt}
	far.raise(session.Event{Kind: session.EventQuestionWithdrawn, ID: gone.ID, Question: &gone})
	event := nextLane(t, lane)
	if event.Kind != session.EventQuestionWithdrawn || event.Question == nil || event.Question.Withdrawn == nil {
		t.Fatalf("the withdrawal arrived as %v/%+v", event.Kind, event.Question)
	}
	if event.Question.Withdrawn.Reason != "the file it was about was deleted" {
		t.Fatalf("the withdrawal lost its reason: %+v", event.Question.Withdrawn)
	}

	settled := wholeQuestion()
	given := wholeAnswer()
	far.raise(session.Event{Kind: session.EventQuestionAnswered, ID: settled.ID, Question: &settled, Answer: &given})
	event = nextLane(t, lane)
	if event.Kind != session.EventQuestionAnswered || event.Answer == nil {
		t.Fatalf("the answered record arrived as %v/%+v", event.Kind, event.Answer)
	}
	if !reflect.DeepEqual(*event.Answer, given) {
		t.Fatalf("the record arrived changed:\n got %+v\nwant %+v", *event.Answer, given)
	}
}

// wholeAnswer is an answer with something in every field a person's intent can
// reach: the picks, the words beside them, the notes on parts, the exchange, the
// blanks, the dial, the reframe, the scope, the soft reason and who decided.
func wholeAnswer() session.Answer {
	dial := 0.75
	return session.Answer{
		At:        askedAt,
		Kind:      session.QuestionAsk,
		ID:        7,
		Key:       "2",
		From:      "home",
		Ask:       session.AskChoice,
		Picked:    []string{"2", "1"},
		Change:    "2, but keep the sqlite file as the source of truth",
		Comments:  map[string]string{"1": "too much machinery for this"},
		AskedBack: []session.Exchange{{Option: "2", Asked: "does it hold order?", Replied: "yes, by append", At: askedAt}},
		Blanks:    map[string]string{"table": "events"},
		Dial:      &dial,
		Reframe:   "the real question is where the truth lives",
		DecidedBy: session.DecidedByPerson,
		Scope:     session.ScopeProject,
		Why:       "the migration is easier to unwind",
	}
}

// THE ANSWER GOES UP WHOLE. A wire that carried only the key would be the wire
// deciding that the notes, the exchange and the words beside the pick are not
// part of what somebody said.
func TestAnAnswerCrossesTheWireWhole(t *testing.T) {
	far := newAskingAgent()
	loop := laneLoop(t, far)

	given := wholeAnswer()
	if err := loop.Client.Agent().ResolveQuestion(given); err != nil {
		t.Fatalf("answering over the wire: %v", err)
	}
	heard := far.heard()
	if len(heard) != 1 {
		t.Fatalf("the engine heard %d answers, want one", len(heard))
	}
	if !reflect.DeepEqual(heard[0], given) {
		t.Fatalf("the answer arrived changed:\n got %+v\nwant %+v", heard[0], given)
	}
}

// WHAT IS ALREADY OPEN IS REPLAYED THE MOMENT A SURFACE ATTACHES, and it is what
// makes a question survive a window that was not there when it was asked — a
// conversation resumed, one switched back to behind home, a link repaired.
// [Agent.OpenQuestions] is answered from that replay rather than from a round
// trip, so a surface holding the lane knows what is open without asking.
func TestWhatIsOpenIsReplayedOnAttachAndReadable(t *testing.T) {
	waiting := wholeQuestion()
	far := newAskingAgent(waiting)
	loop := laneLoop(t, far)
	agent := loop.Client.Agent()

	if open := agent.OpenQuestions(); len(open) != 0 {
		t.Fatalf("a surface that never opened the lane reported %d open questions", len(open))
	}

	lane, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	if event := nextLane(t, lane); event.Kind != session.EventQuestion || event.Question == nil || event.Question.ID != 7 {
		t.Fatalf("the replay carried %v/%+v", event.Kind, event.Question)
	}
	waitFor(t, "the surface can read what is open", func() bool {
		open := agent.OpenQuestions()
		return len(open) == 1 && open[0].ID == 7 && open[0].Head == waiting.Head
	})

	// Answered, it stops being open — and a second copy of a question already
	// held replaces it where it stands rather than doubling the row.
	again := wholeQuestion()
	far.raise(session.Event{Kind: session.EventQuestion, ID: again.ID, Question: &again})
	nextLane(t, lane)
	if open := agent.OpenQuestions(); len(open) != 1 {
		t.Fatalf("a replayed question was drawn twice: %d open", len(open))
	}
	given := wholeAnswer()
	far.raise(session.Event{Kind: session.EventQuestionAnswered, ID: again.ID, Question: &again, Answer: &given})
	nextLane(t, lane)
	waitFor(t, "the answered question stopped being open", func() bool { return len(agent.OpenQuestions()) == 0 })
}

// A SURFACE TAKING UP A SECOND CONVERSATION DOES NOT CARRY THE FIRST ONE'S
// QUESTIONS INTO IT. The lane is replaced rather than added to, and what this
// window believed was open is forgotten before the new engine replays its own.
func TestWatchingQuestionsAgainForgetsWhatTheLastConversationHeld(t *testing.T) {
	far := newAskingAgent(wholeQuestion())
	loop := laneLoop(t, far)
	agent := loop.Client.Agent()

	first, _ := agent.WatchQuestions()
	nextLane(t, first)
	waitFor(t, "the surface read the replay", func() bool { return len(agent.OpenQuestions()) == 1 })

	far.mu.Lock()
	far.open = nil
	far.mu.Unlock()

	second, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	waitFor(t, "the engine reopened the questions lane", func() bool { return far.subscriptions() == 2 })
	waitFor(t, "the questions of the conversation this window left are gone", func() bool {
		return len(agent.OpenQuestions()) == 0
	})
	select {
	case event, ok := <-second:
		if ok {
			t.Fatalf("the second lane replayed %+v from the conversation this window left", event.Question)
		}
	case <-time.After(200 * time.Millisecond):
		// Nothing arriving is the answer: the lane is open and this conversation
		// is waiting on nobody.
	}
}

// An engine whose agent has no questions lane is ABSENT RATHER THAN BROKEN: the
// subscription is refused quietly, the lane never speaks, and answering is a
// sentence a person can read rather than a hang.
func TestAnEngineWithNoQuestionsLaneRefusesQuietly(t *testing.T) {
	loop := laneLoop(t, &fakeAgent{})
	agent := loop.Client.Agent()

	lane, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	select {
	case event, ok := <-lane:
		if ok {
			t.Fatalf("a lane nothing feeds delivered %v", event.Kind)
		}
	case <-time.After(200 * time.Millisecond):
	}
	err := agent.ResolveQuestion(wholeAnswer())
	if err == nil {
		t.Fatal("an engine that cannot answer questions accepted one")
	}
	if want := "this session cannot answer questions from here"; !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal reads %q, and a person has to be able to read it", err)
	}
}

// A READING SURFACE MAY WATCH A QUESTION AND MAY NOT ANSWER IT. The page that
// looks into work running in the conversation next door is owed the fact that
// the work has stopped and is waiting on somebody; the answer belongs to the
// window that owns it.
func TestAReadingSurfaceWatchesQuestionsAndNeverAnswersThem(t *testing.T) {
	if !watcherMay(MethodQuestionWatch) {
		t.Fatal("a reading surface cannot subscribe to questions, so a task room over somebody else's work cannot say the work is waiting on a person")
	}
	if watcherMay(MethodQuestionResolve) {
		t.Fatal("a reading surface may answer a question: a window that came to READ one task would be deciding for the window that owns it")
	}
}
