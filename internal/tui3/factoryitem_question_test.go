package tui3

// The floor item's change card, drawn and answered on the terminal surface.
//
// `factory_item` raises a [session.QuestionItem] and waits on it. Until this
// block drew that kind a hosted terminal showed `running factory_item` until the
// person pressed esc, because nothing on the surface could put the card on the
// screen or take a key for it. These tests run the REAL tool on a real session
// behind a real loopback wire, over a real factory store, so what is asserted is
// what the engine raised, what the block drew, and what the store holds after a
// key — not strings the test wrote for itself.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// countingItemDoor is the cmd door's own shape over a real store: it previews and
// applies through [session.ApplyItemChange] and counts the writes, so "applied
// once" is a fact about the door and not about a string.
type countingItemDoor struct {
	st      *store.Store
	recipe  factory.Recipe
	applied atomic.Int32
}

func (d *countingItemDoor) Preview(_ context.Context, id int, change session.ItemChange) (factory.Item, factory.Item, error) {
	it, err := d.st.Get(id)
	if err != nil {
		return factory.Item{}, factory.Item{}, err
	}
	after, err := session.ApplyItemChange(it, change, d.recipe)
	if err != nil {
		return factory.Item{}, factory.Item{}, err
	}
	return session.ItemStaged(it, d.recipe), after, nil
}

func (d *countingItemDoor) Apply(_ context.Context, id int, change session.ItemChange) (factory.Item, error) {
	err := d.st.Update(id, func(it *factory.Item) error {
		next, err := session.ApplyItemChange(*it, change, d.recipe)
		if err != nil {
			return err
		}
		*it = next
		return nil
	})
	if err != nil {
		return factory.Item{}, err
	}
	d.applied.Add(1)
	return d.st.Get(id)
}

// itemModel is a scripted model endpoint: its first reply calls `factory_item`
// with args, every later one is plain text, and it keeps what it was sent so a
// test can read what the model was told the card came to.
type itemModel struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newItemModel(t *testing.T, args string) *itemModel {
	t.Helper()
	m := &itemModel{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.bodies = append(m.bodies, string(raw))
		// THE CALL IS MADE TO THE REQUEST THAT CARRIES THE TOOL AND HAS NOT BEEN
		// ANSWERED YET: other requests (a title, a reflex) get plain text, and
		// the one after the result carries `call_item` back.
		first := strings.Contains(string(raw), `"factory_item"`) && !strings.Contains(string(raw), "call_item")
		m.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if first {
			call := `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_item","type":"function","function":{"name":"factory_item","arguments":` + strconv.Quote(args) + `}}]}}]}`
			_, _ = io.WriteString(w, "data: "+call+"\n\n")
			_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Noted."}}]}`+"\n\n")
			_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(m.Close)
	return m
}

// told is everything the model was sent after its first reply, which is where
// the tool's result lands.
func (m *itemModel) told() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, body := range m.bodies {
		if strings.Contains(body, "call_item") {
			out = append(out, body)
		}
	}
	return strings.Join(out, "\n")
}

// itemLab is one hosted conversation with a floor item in it: a real session on
// the far side of a real loopback wire, the surface on the near side, and a
// question lab drawing what the lane delivers.
type itemLab struct {
	*questionLab
	st     *store.Store
	door   *countingItemDoor
	model  *itemModel
	far    *session.Agent
	id     int
	lane   <-chan session.Event
	events <-chan session.Event
	cancel context.CancelFunc
	turn   chan struct{}
}

// itemShape builds the item under test from the default recipe's stages, so a
// case can say what is fixed, off or legacy in one place.
type itemShape func(*factory.Item)

func itemLabFor(t *testing.T, shape itemShape, args string) *itemLab {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recipe := factory.DefaultRecipe()
	created, err := st.Create(factory.Item{Repo: "factory-demo", Kind: factory.KindIssue, Title: "Total double-counts an entry added twice", State: factory.StateNew, Cap: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(created.ID, func(it *factory.Item) error {
		*it = session.ItemStaged(*it, recipe)
		it.Stages = factory.CopyStages(it.Stages)
		if shape != nil {
			shape(it)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	door := &countingItemDoor{st: st, recipe: recipe}
	model := newItemModel(t, strings.ReplaceAll(args, "ITEM", strconv.Itoa(created.ID)))
	dir := t.TempDir()
	far, err := session.New(session.Config{
		Workspace: dir, Model: "vendor/m", APIKey: "test", BaseURL: model.URL, System: "SYSTEM",
		FactoryItem: door,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = far.Close() })
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) { return &remote.Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	hosted := loop.Client.Agent()
	if !DrawsQuestions(hosted) {
		t.Fatal("the hosted agent does not carry the questions seam")
	}
	lab := &itemLab{questionLab: newQuestionLab(t), st: st, door: door, model: model, far: far, id: created.ID, turn: make(chan struct{})}
	lab.a = newTestApp(hosted)
	lab.a.width, lab.a.height = 100, 30
	lab.a.clock = func() time.Time { return lab.at }
	doors, _ := lab.a.questionDoors()
	var stop func()
	lab.lane, stop = doors.WatchQuestions()
	t.Cleanup(stop)
	return lab
}

// ask has the model call the tool and waits for the card to reach the block,
// the way the lane delivers it.
func (l *itemLab) ask() session.Question {
	l.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.t.Cleanup(cancel)
	events, err := l.far.Submit(ctx, "change the item")
	if err != nil {
		l.t.Fatal(err)
	}
	l.events = events
	go func() {
		for range events {
		}
		close(l.turn)
	}()
	for {
		ev := l.next()
		if ev.Kind == session.EventQuestion && ev.Question != nil && ev.Question.Kind == session.QuestionItem {
			l.at = ev.Question.Asked
			l.fold(ev)
			return *ev.Question
		}
	}
}

// next is the next event on the lane, or a failure: nothing here may wait forever.
func (l *itemLab) next() session.Event {
	l.t.Helper()
	select {
	case ev, ok := <-l.lane:
		if !ok {
			l.t.Fatal("the question lane closed")
		}
		return ev
	case <-time.After(10 * time.Second):
		l.t.Fatal("the lane delivered nothing")
	}
	return session.Event{}
}

func (l *itemLab) fold(ev session.Event) {
	l.t.Helper()
	drive(l.t, l.a, runCmd(l.a.questionFold(ev))...)
	l.rows()
}

// settled waits for the turn to end.
func (l *itemLab) settled() {
	l.t.Helper()
	select {
	case <-l.turn:
	case <-time.After(10 * time.Second):
		l.t.Fatal("the turn never ended: the tool is still waiting on its card")
	}
}

func (l *itemLab) item() factory.Item {
	l.t.Helper()
	it, err := l.st.Get(l.id)
	if err != nil {
		l.t.Fatal(err)
	}
	return it
}

// flat is the block as one line of words: the box's edges taken off and every
// run of spaces made one, so a row the renderer wrapped is still a row.
func cardFlat(block string) string {
	var words []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.Trim(strings.TrimSpace(line), "│╭╮╰╯─")
		words = append(words, strings.Fields(line)...)
	}
	return strings.Join(words, " ")
}

func cardWords(s string) string { return strings.Join(strings.Fields(s), " ") }

// The three shapes of item the thinking card has to be truthful about. Each
// one is a way "think strong" must NOT reach every step.
var itemRenderCases = []struct {
	name  string
	shape itemShape
	// moved and kept are the facts a person reads on the card and in the store.
	moved, kept map[string]string
	off         string
}{
	{
		// A LEGACY STAGE keeps the capital its document was written with.
		name: "legacy",
		shape: func(it *factory.Item) {
			at := factory.StageIndex(it.Stages, "proof")
			tidy := factory.Stage{Name: "Tidy", Kind: factory.StageChat, Ask: "tidy up", On: true, Until: factory.UntilDone}
			it.Stages = append(it.Stages[:at], append([]factory.Stage{tidy}, it.Stages[at:]...)...)
		},
		moved: map[string]string{"Tidy": "strong", "plan": "strong"},
	},
	{
		// A FIXED STEP stays where the recipe pinned it.
		name: "fixed",
		shape: func(it *factory.Item) {
			plan := factory.StageIndex(it.Stages, "plan")
			it.Stages[plan].Fixed, it.Stages[plan].Effort = true, "cheap"
		},
		moved: map[string]string{"write": "strong", "review": "strong"},
		kept:  map[string]string{"plan": "cheap"},
	},
	{
		// AN OFF STEP is edited and stays off.
		name: "off",
		shape: func(it *factory.Item) {
			// The shipped default has no security step since 2026-10-20, so
			// one is laid on after review, off.
			sec := factory.Stage{Name: "security", Kind: factory.StageChat, Ask: "secrets", Until: factory.UntilClean, On: false, Effort: "cheap"}
			it.Stages = factory.PlaceStage(it.Stages, sec, "after review")
		},
		moved: map[string]string{"security": "strong", "write": "strong"},
		off:   "security",
	},
}

const itemThinkArgs = `{"item":ITEM,"effort":"strong","why":"the work is subtle"}`

// itemRowsFor is the card's body as the SHARED facts say it, computed from the
// item in the store and the change the tool previews: the same two functions
// the engine calls, from different inputs than the question carries.
func itemRowsFor(t *testing.T, lab *itemLab, change session.ItemChange, why string) []string {
	t.Helper()
	it := lab.item()
	after, err := session.ApplyItemChange(it, change, lab.door.recipe)
	if err != nil {
		t.Fatal(err)
	}
	staged := session.ItemStaged(it, lab.door.recipe)
	return session.ItemRows(session.ItemNotice{
		Before: session.ItemFactsOf(staged), After: session.ItemFactsOf(after),
		Effort: change.Effort, Why: why,
	})
}

// A HOSTED `factory_item` CALL IS DRAWN, ANSWERED ONCE, AND TRUTHFUL PER STEP.
func TestFactoryItemCardOnTheTerminalYesAppliesOneChange(t *testing.T) {
	for _, tc := range itemRenderCases {
		t.Run(tc.name, func(t *testing.T) {
			lab := itemLabFor(t, tc.shape, itemThinkArgs)
			before := lab.item()
			rows := itemRowsFor(t, lab, session.ItemChange{Effort: "strong"}, "the work is subtle")
			q := lab.ask()

			if len(lab.a.questions) != 1 || lab.a.questions[0].question.Kind != session.QuestionItem {
				t.Fatalf("the card is not on the block: %d questions", len(lab.a.questions))
			}
			card := cardFlat(lab.plain())
			for _, want := range append([]string{q.Head}, rows...) {
				if !strings.Contains(card, cardWords(want)) {
					t.Errorf("the card does not say %q:\n%s", want, lab.plain())
				}
			}
			for _, option := range session.AnswerOptions(session.QuestionItem) {
				if !strings.Contains(card, option.Key+" "+option.Label) {
					t.Errorf("the card offers no %q row:\n%s", option.Label, lab.plain())
				}
			}
			for name := range tc.kept {
				if strings.Contains(card, name+" —") || strings.Contains(card, name+" cheap →") {
					t.Errorf("the card moves the fixed step %s:\n%s", name, lab.plain())
				}
			}
			if tc.off != "" && !strings.Contains(card, tc.off+"(off)") {
				t.Errorf("the card does not say %s is off:\n%s", tc.off, lab.plain())
			}
			if !reflect.DeepEqual(lab.item(), before) || lab.door.applied.Load() != 0 {
				t.Fatal("the item moved before anybody answered")
			}

			// AN ENTER OVER THE EMPTY BOX IS NOT A YES, wherever the pointer stands.
			lab.tick(questionSettle * 2)
			lab.press("enter")
			lab.pressMsg(tea.KeyPressMsg{Code: tea.KeyDown})
			lab.pressMsg(tea.KeyPressMsg{Code: tea.KeyUp})
			lab.press("enter")
			if lab.door.applied.Load() != 0 || len(lab.a.questions) != 1 {
				t.Fatalf("enter answered the card: applied %d, open %d", lab.door.applied.Load(), len(lab.a.questions))
			}

			lab.press("1")
			// REPEATS ARE SAFE: the card is gone, so a second digit, an enter and
			// a late answer straight at the engine all land on nothing.
			lab.press("1")
			lab.press("enter")
			_ = lab.a.agent.(questionResolver).ResolveQuestion(session.Answer{Kind: session.QuestionItem, Ref: q.Ref, Key: session.ItemYesKey})
			lab.settled()

			if got := lab.door.applied.Load(); got != 1 {
				t.Fatalf("a yes applied %d changes, want 1", got)
			}
			if len(lab.a.questions) != 0 {
				t.Fatal("the answered card is still on the block")
			}
			now := session.ItemFactsOf(lab.item())
			for name, want := range tc.moved {
				if now.Thinking[name] != want {
					t.Errorf("%s thinks %q after the yes, want %q", name, now.Thinking[name], want)
				}
			}
			for name, want := range tc.kept {
				if now.Thinking[name] != want {
					t.Errorf("fixed %s moved to %q, want %q", name, now.Thinking[name], want)
				}
			}
			if tc.off != "" {
				stages := lab.item().Stages
				if sec := stages[factory.StageIndex(stages, tc.off)]; sec.On {
					t.Errorf("%s was switched on by a thinking change", tc.off)
				}
			}
			if lab.item().Cap != before.Cap {
				t.Errorf("the budget moved from %v to %v", before.Cap, lab.item().Cap)
			}
			if !strings.Contains(lab.model.told(), "said yes") {
				t.Errorf("the model was not told the card was answered yes: %q", lab.model.told())
			}
		})
	}
}

// A NO CHANGES NOTHING, and so does the esc that puts the card off.
func TestFactoryItemCardOnTheTerminalDeclineAndEscapeChangeNothing(t *testing.T) {
	for _, tc := range itemRenderCases {
		t.Run(tc.name, func(t *testing.T) {
			lab := itemLabFor(t, tc.shape, itemThinkArgs)
			before := lab.item()
			lab.ask()
			lab.tick(questionSettle * 2)

			// ESC IS "LATER", not a no: the card is put aside, nothing is sent, and
			// the item is as it was.
			lab.press("esc")
			if lab.door.applied.Load() != 0 || !reflect.DeepEqual(lab.item(), before) {
				t.Fatal("esc changed the item")
			}
			select {
			case <-lab.turn:
				t.Fatal("esc answered the card and ended the tool")
			case <-time.After(100 * time.Millisecond):
			}

			// THE NO IS THE SAFE ROW. It may be put back from the side first.
			lab.pressMsg(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			lab.tick(questionSettle * 2)
			lab.rows()
			lab.press("2")
			lab.press("2")
			lab.settled()
			if lab.door.applied.Load() != 0 || !reflect.DeepEqual(lab.item(), before) {
				t.Fatalf("a no changed the item: applied %d", lab.door.applied.Load())
			}
			if !strings.Contains(lab.model.told(), "the person said no") {
				t.Errorf("the model was not told the card was declined: %q", lab.model.told())
			}
		})
	}
}

// WORDS ARE A CHANGE AND NEVER A YES: the item stays, the model is handed the
// sentence, and a sentence beside a yes still changes nothing.
func TestFactoryItemCardOnTheTerminalTypedRevisionIsRefused(t *testing.T) {
	for _, tc := range itemRenderCases {
		t.Run(tc.name, func(t *testing.T) {
			lab := itemLabFor(t, tc.shape, itemThinkArgs)
			before := lab.item()
			lab.ask()
			lab.tick(questionSettle * 2)

			const said = "make it cheap on the plan and leave review alone"
			lab.a.input.setText(said)
			lab.press("enter")
			lab.settled()

			if lab.door.applied.Load() != 0 || !reflect.DeepEqual(lab.item(), before) {
				t.Fatalf("typed words changed the item: applied %d", lab.door.applied.Load())
			}
			if len(lab.a.questions) != 0 {
				t.Fatal("the answered card is still on the block")
			}
			told := lab.model.told()
			if !strings.Contains(told, "the person changed it") || !strings.Contains(told, said) || strings.Contains(told, "said yes") {
				t.Errorf("the model was not handed the correction as a correction: %q", told)
			}
			if lab.a.input.String() != "" {
				t.Errorf("the sentence is still in the box: %q", lab.a.input.String())
			}
		})
	}
}

// AN INTERRUPT COMES THE CARD DOWN: the tool ends, the block empties, and an
// answer that arrives afterwards finds nothing to apply.
func TestFactoryItemCardOnTheTerminalInterruptWithdrawsTheCard(t *testing.T) {
	lab := itemLabFor(t, nil, itemThinkArgs)
	before := lab.item()
	q := lab.ask()
	lab.tick(questionSettle * 2)
	// THE CARD MUST BE DRAWN BEFORE IT IS TAKEN DOWN: with the whitelist case
	// reverted nothing is ever drawn, and "no questions after the interrupt"
	// would pass without proving a withdrawal.
	if len(lab.a.questions) != 1 {
		t.Fatalf("the card is not on the block before the interrupt: %d questions", len(lab.a.questions))
	}

	lab.far.Interrupt()
	for {
		ev := lab.next()
		lab.fold(ev)
		if ev.Kind == session.EventQuestionWithdrawn {
			break
		}
	}
	lab.settled()
	if len(lab.a.questions) != 0 {
		t.Fatalf("the card is still on the block after the interrupt: %d", len(lab.a.questions))
	}
	if lab.press("1") {
		t.Fatal("a key was taken for a card that is gone")
	}
	_ = lab.a.agent.(questionResolver).ResolveQuestion(session.Answer{Kind: session.QuestionItem, Ref: q.Ref, Key: session.ItemYesKey})
	if lab.door.applied.Load() != 0 || !reflect.DeepEqual(lab.item(), before) {
		t.Fatal("a yes after the interrupt changed the item")
	}
}
