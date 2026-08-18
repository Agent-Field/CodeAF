package reflex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the fake ────────────────────────────────────────────────────────────────

// fake is a Completer that answers from a script and keeps every request it was
// handed. One entry per call: the second entry is what the repair retry gets.
type fake struct {
	replies []string
	err     error
	calls   []ai.Request
}

func (f *fake) CompleteWithMessages(_ context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	request := ai.Request{Messages: messages}
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	f.calls = append(f.calls, request)
	if f.err != nil {
		return nil, f.err
	}
	index := len(f.calls) - 1
	if index >= len(f.replies) {
		return nil, errors.New("fake: the script ran out of replies")
	}
	return reply(f.replies[index]), nil
}

func reply(text string) *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
	}}}
}

// prompt is the text of one request, every message run together, which is what
// a test asking "was the index in there" actually wants to search.
func prompt(request ai.Request) string {
	var builder strings.Builder
	for _, message := range request.Messages {
		for _, part := range message.Content {
			builder.WriteString(part.Text)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

var index = []Stub{
	{ID: "m3", Title: "standup is at 9:15", Type: "fact", Scope: "project"},
	{ID: "m7", Title: "prefers dark themes", Type: "preference", Scope: "user"},
}

// ── route ───────────────────────────────────────────────────────────────────

// A REPLY IS A REPLY WHATEVER IT IS WRAPPED IN. A small model fences its JSON,
// prefaces it, or apologises after it, and every one of those is the right
// answer with something around it — none of them may cost a turn its routing.
func TestRouteReadsTheAnswerThroughWhateverTheModelWrappedItIn(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply string
	}{
		{"clean json", `{"inject":["m3"],"cmd":null}`},
		{"fenced json", "```json\n{\"inject\":[\"m3\"],\"cmd\":null}\n```"},
		{"prose either side", "Sure! Here is the answer:\n{\"inject\":[\"m3\"],\"cmd\":null}\nHope that helps."},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fake{replies: []string{test.reply}}
			result, err := Route(context.Background(), client, "when is standup", index)
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if len(result.Inject) != 1 || result.Inject[0] != "m3" {
				t.Fatalf("Route injected %v, want [m3]", result.Inject)
			}
			if result.Cmd != nil {
				t.Fatalf("Route found a command in an ordinary question: %+v", result.Cmd)
			}
			if len(client.calls) != 1 {
				t.Fatalf("Route made %d calls for a readable answer, want 1", len(client.calls))
			}
		})
	}
}

func TestRouteRepairsOneUnreadableAnswerAndThenGivesUp(t *testing.T) {
	client := &fake{replies: []string{"I'm not sure what you mean.", `{"inject":["m7"],"cmd":null}`}}
	result, err := Route(context.Background(), client, "what themes do I like", index)
	if err != nil {
		t.Fatalf("Route after one repair: %v", err)
	}
	if len(result.Inject) != 1 || result.Inject[0] != "m7" {
		t.Fatalf("the repaired answer injected %v, want [m7]", result.Inject)
	}
	if len(client.calls) != 2 {
		t.Fatalf("Route made %d calls, want the first and one repair", len(client.calls))
	}
	if repaired := prompt(client.calls[1]); !strings.Contains(repaired, repairInstruction) {
		t.Fatalf("the repair call never asked for only the JSON object:\n%s", repaired)
	}

	// Twice unreadable is a no-op, reported as one typed error, with nothing
	// half-filled in the result: a turn cannot be partly routed.
	client = &fake{replies: []string{"no idea", "still no idea"}}
	result, err = Route(context.Background(), client, "when is standup", index)
	if !errors.Is(err, ErrReflexFailed) {
		t.Fatalf("Route error = %v, want ErrReflexFailed", err)
	}
	if len(result.Inject) != 0 || result.Cmd != nil {
		t.Fatalf("a failed Route returned %+v, want the zero result", result)
	}
	if len(client.calls) != 2 {
		t.Fatalf("Route made %d calls before giving up, want 2", len(client.calls))
	}
}

// A PROVIDER THAT NEVER ANSWERED IS THE SAME NO-OP, and it costs one call, not
// a retry: there is nothing to repair.
func TestRouteTreatsAProviderFailureAsANoOp(t *testing.T) {
	client := &fake{err: errors.New("provider: 502")}
	result, err := Route(context.Background(), client, "when is standup", index)
	if !errors.Is(err, ErrReflexFailed) {
		t.Fatalf("Route error = %v, want ErrReflexFailed", err)
	}
	if len(result.Inject) != 0 {
		t.Fatalf("a failed Route returned %+v, want the zero result", result)
	}
	if len(client.calls) != 1 {
		t.Fatalf("Route made %d calls against a broken provider, want 1", len(client.calls))
	}
}

func TestRouteKeepsOnlyIdsTheIndexActuallyHeldAndOnlyRealCommands(t *testing.T) {
	client := &fake{replies: []string{`{"inject":["m3","m99",""],"cmd":{"name":"remember","arg":"deploys on Fridays"}}`}}
	result, err := Route(context.Background(), client, "remember that I deploy on Fridays", index)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if len(result.Inject) != 1 || result.Inject[0] != "m3" {
		t.Fatalf("Route injected %v, want only the id the index carried", result.Inject)
	}
	if result.Cmd == nil || result.Cmd.Name != "remember" || result.Cmd.Arg != "deploys on Fridays" {
		t.Fatalf("Route read the command as %+v", result.Cmd)
	}

	// A command word this package does not know is an unreadable answer, which
	// means the repair path — not a Cmd nothing downstream can act on.
	client = &fake{replies: []string{`{"inject":[],"cmd":{"name":"memorise","arg":"x"}}`, `{"inject":[],"cmd":null}`}}
	result, err = Route(context.Background(), client, "remember x", index)
	if err != nil {
		t.Fatalf("Route after repair: %v", err)
	}
	if result.Cmd != nil {
		t.Fatalf("Route kept an invented command: %+v", result.Cmd)
	}
	if len(client.calls) != 2 {
		t.Fatalf("an invented command took %d calls, want a repair", len(client.calls))
	}
}

// An empty message routes to nothing WITHOUT PAYING FOR A CALL.
func TestRouteDoesNotCallTheModelForAnEmptyMessage(t *testing.T) {
	client := &fake{}
	result, err := Route(context.Background(), client, "   ", index)
	if err != nil || len(result.Inject) != 0 || result.Cmd != nil {
		t.Fatalf("Route(\"\") = %+v, %v", result, err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("Route called the model %d times for an empty message", len(client.calls))
	}
}

// ── what the model is shown ─────────────────────────────────────────────────

func TestTheRouterIsShownACappedIndexAndACappedMessage(t *testing.T) {
	long := make([]Stub, 0, 250)
	for i := range 250 {
		long = append(long, Stub{ID: stubID(i), Title: "line", Type: "fact", Scope: "user"})
	}
	client := &fake{replies: []string{`{"inject":[],"cmd":null}`}}
	if _, err := Route(context.Background(), client, strings.Repeat("x", 5000), long); err != nil {
		t.Fatalf("Route: %v", err)
	}
	// Counted off the rendering rather than off the whole prompt, which carries
	// worked examples with index lines of their own.
	if lines := strings.Count(renderIndex(long), "\n") + 1; lines != indexLimit {
		t.Fatalf("the index rendered %d lines for 250 stubs, want %d", lines, indexLimit)
	}
	shown := prompt(client.calls[0])
	if !strings.Contains(shown, stubID(indexLimit-1)) {
		t.Fatalf("the router was not shown the last stub inside the cap")
	}
	if strings.Contains(shown, stubID(indexLimit)) {
		t.Fatalf("the router was shown a stub past the cap")
	}
	if run := strings.Repeat("x", messageLimit+1); strings.Contains(shown, run) {
		t.Fatalf("the router was shown more than %d characters of the message", messageLimit)
	}
	if run := strings.Repeat("x", messageLimit); !strings.Contains(shown, run) {
		t.Fatalf("the router was shown less than %d characters of the message", messageLimit)
	}
}

func stubID(i int) string {
	return "m" + string(rune('a'+i/26)) + string(rune('a'+i%26))
}

func TestAnEmptyIndexReadsAsWordsRatherThanABlank(t *testing.T) {
	if got := renderIndex(nil); got != "(nothing remembered yet)" {
		t.Fatalf("an empty index renders %q", got)
	}
	if got := renderIndex([]Stub{{Title: "no id here"}}); got != "(nothing remembered yet)" {
		t.Fatalf("a stub with no id renders %q, want it dropped", got)
	}
	if got := renderIndex(index); !strings.Contains(got, "- m3: standup is at 9:15 [fact/project]") {
		t.Fatalf("the index renders %q", got)
	}
}

// EVERY REFLEX CALL IS SHAPED THE SAME: a small ceiling and no temperature, so
// the answer is the shape the examples showed and the bill is small.
func TestEveryCallIsSmallAndCold(t *testing.T) {
	client := &fake{replies: []string{`{"inject":[],"cmd":null}`}}
	if _, err := Route(context.Background(), client, "hello", index); err != nil {
		t.Fatalf("Route: %v", err)
	}
	request := client.calls[0]
	if request.MaxTokens == nil || *request.MaxTokens != answerTokens {
		t.Fatalf("the request's max tokens = %v, want %d", request.MaxTokens, answerTokens)
	}
	if request.Temperature == nil || *request.Temperature != 0 {
		t.Fatalf("the request's temperature = %v, want 0", request.Temperature)
	}
	if request.Model != "" {
		t.Fatalf("an unbound call named model %q; the client's own model answers", request.Model)
	}
}

// ── extract ─────────────────────────────────────────────────────────────────

// NOTHING TO REMEMBER IS THE COMMON ANSWER and it is taken at face value: no
// enum is checked, because there is nothing in the answer to check.
func TestExtractPassesNothingWorthKeepingStraightThrough(t *testing.T) {
	client := &fake{replies: []string{`{"mem":0,"type":"","scope":"nonsense"}`}}
	result, err := Extract(context.Background(), client, "what does this regex do", "it matches a date.")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Mem != 0 {
		t.Fatalf("Extract read mem = %d, want 0", result.Mem)
	}
	if result.State != nil {
		t.Fatalf("Extract invented a state delta: %+v", result.State)
	}
	if len(client.calls) != 1 {
		t.Fatalf("mem 0 took %d calls, want 1 — its fields are not validated", len(client.calls))
	}
}

func TestExtractRepairsAKeptMemoryWithAWordItDoesNotKnow(t *testing.T) {
	good := `{"mem":1,"type":"preference","scope":"user","title":"wants changes made not explained",` +
		`"text":"Prefers the change made directly.","tags":["style",""]}`
	client := &fake{replies: []string{`{"mem":1,"type":"vibe","scope":"user","title":"x","text":"y"}`, good}}
	result, err := Extract(context.Background(), client, "stop explaining", "understood.")
	if err != nil {
		t.Fatalf("Extract after repair: %v", err)
	}
	if result.Type != "preference" || result.Scope != "user" {
		t.Fatalf("Extract read %+v", result)
	}
	if len(result.Tags) != 1 || result.Tags[0] != "style" {
		t.Fatalf("Extract kept tags %v, want the blank dropped", result.Tags)
	}
	if len(client.calls) != 2 {
		t.Fatalf("an unknown type took %d calls, want a repair", len(client.calls))
	}

	// A scope it does not know is the same path, and twice is a no-op.
	client = &fake{replies: []string{
		`{"mem":1,"type":"fact","scope":"everywhere","title":"x","text":"y"}`,
		`{"mem":1,"type":"fact","scope":"everywhere","title":"x","text":"y"}`,
	}}
	result, err = Extract(context.Background(), client, "a", "b")
	if !errors.Is(err, ErrReflexFailed) {
		t.Fatalf("Extract error = %v, want ErrReflexFailed", err)
	}
	if result.Mem != 0 || result.Type != "" {
		t.Fatalf("a failed Extract returned %+v, want the zero result", result)
	}
}

func TestExtractReadsTheStateDeltaWhenTheExchangeMovedTheWork(t *testing.T) {
	client := &fake{replies: []string{`{"mem":1,"type":"project_state","scope":"project",` +
		`"title":"import done migration next","text":"The import script is finished.","tags":["import"],` +
		`"state":{"goal":"move the data across","done":["import script"],"inflight":[],` +
		`"next":["the migration"],"open":["which cutover window"],"refs":["scripts/import.go"]}}`}}
	result, err := Extract(context.Background(), client, "import is done, migration next", "good — starting.")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.State == nil {
		t.Fatal("Extract dropped the state delta")
	}
	if result.State.Goal != "move the data across" {
		t.Fatalf("the delta's goal = %q", result.State.Goal)
	}
	if len(result.State.Done) != 1 || result.State.Done[0] != "import script" {
		t.Fatalf("the delta's done = %v", result.State.Done)
	}
	if len(result.State.Inflight) != 0 {
		t.Fatalf("an empty list came back as %v, want nothing", result.State.Inflight)
	}
	if len(result.State.Next) != 1 || len(result.State.Open) != 1 || len(result.State.Refs) != 1 {
		t.Fatalf("the delta read %+v", result.State)
	}
}

func TestExtractDoesNotCallTheModelForAnEmptyExchange(t *testing.T) {
	client := &fake{}
	result, err := Extract(context.Background(), client, "", "  ")
	if err != nil || result.Mem != 0 {
		t.Fatalf("Extract(\"\", \"\") = %+v, %v", result, err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("Extract called the model %d times for an empty exchange", len(client.calls))
	}
}

func TestBothSidesOfTheExchangeAreClippedOnTheirOwn(t *testing.T) {
	client := &fake{replies: []string{`{"mem":0}`}}
	if _, err := Extract(context.Background(), client,
		strings.Repeat("u", 4000), strings.Repeat("a", 4000)); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	shown := prompt(client.calls[0])
	if !strings.Contains(shown, strings.Repeat("u", messageLimit)) ||
		!strings.Contains(shown, strings.Repeat("a", messageLimit)) {
		t.Fatal("one side of the exchange was clipped away entirely")
	}
	if strings.Contains(shown, strings.Repeat("u", messageLimit+1)) ||
		strings.Contains(shown, strings.Repeat("a", messageLimit+1)) {
		t.Fatalf("a side of the exchange was shown past %d characters", messageLimit)
	}
}

// ── decide ──────────────────────────────────────────────────────────────────

func TestDecideReadsAllFourAnswers(t *testing.T) {
	candidate := ExtractResult{Mem: 1, Type: "fact", Scope: "project",
		Title: "deploys on Tuesdays", Text: "Deploys go out on Tuesday mornings.", Tags: []string{"release"}}
	neighbors := []Neighbor{{ID: "m2", Title: "deploys on Fridays", Text: "Deploys go out on Friday afternoons."}}

	for _, test := range []struct {
		name   string
		reply  string
		op     string
		target string
	}{
		{"skip", `{"op":"skip","target_id":"","title":"","text":"","tags":[]}`, "skip", ""},
		{"update", `{"op":"update","target_id":"m2","title":"t","text":"x","tags":["release"]}`, "update", "m2"},
		{"supersede", `{"op":"SUPERSEDE","target_id":"m2","title":"t","text":"x"}`, "supersede", "m2"},
		{"add", `{"op":"add","target_id":"","title":"t","text":"x"}`, "add", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fake{replies: []string{test.reply}}
			result, err := Decide(context.Background(), client, candidate, neighbors)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if result.Op != test.op || result.TargetID != test.target {
				t.Fatalf("Decide read %+v, want op %q on %q", result, test.op, test.target)
			}
			if len(client.calls) != 1 {
				t.Fatalf("Decide made %d calls for a readable answer", len(client.calls))
			}
		})
	}
}

func TestDecideRepairsAnOperationItDoesNotKnow(t *testing.T) {
	client := &fake{replies: []string{`{"op":"merge","target_id":"m2"}`, `{"op":"update","target_id":"m2","title":"t","text":"x"}`}}
	result, err := Decide(context.Background(), client, ExtractResult{Mem: 1, Title: "t"}, nil)
	if err != nil {
		t.Fatalf("Decide after repair: %v", err)
	}
	if result.Op != "update" {
		t.Fatalf("Decide read %+v", result)
	}
	if len(client.calls) != 2 {
		t.Fatalf("an unknown op took %d calls, want a repair", len(client.calls))
	}

	client = &fake{replies: []string{`{"op":"merge"}`, `{"op":"merge"}`}}
	result, err = Decide(context.Background(), client, ExtractResult{Mem: 1, Title: "t"}, nil)
	if !errors.Is(err, ErrReflexFailed) {
		t.Fatalf("Decide error = %v, want ErrReflexFailed", err)
	}
	if result.Op != "" {
		t.Fatalf("a failed Decide returned %+v, want the zero result", result)
	}
}

func TestDecideIsShownAtMostThreeNeighboursAndSaysSoWhenThereAreNone(t *testing.T) {
	many := []Neighbor{
		{ID: "m1", Title: "one"}, {ID: "m2", Title: "two"},
		{ID: "m3", Title: "three"}, {ID: "m4", Title: "four"},
	}
	client := &fake{replies: []string{`{"op":"add","title":"t","text":"x"}`}}
	if _, err := Decide(context.Background(), client, ExtractResult{Mem: 1, Title: "t"}, many); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	shown := prompt(client.calls[0])
	// The prompt carries worked examples of its own, so the assertion is about
	// the neighbour that must NOT be there rather than a count of lines.
	if !strings.Contains(shown, "- m3: three") || strings.Contains(shown, "- m4: four") {
		t.Fatalf("Decide was shown neighbours past the cap of %d:\n%s", neighborLimit, shown)
	}
	if got := decideInput(ExtractResult{Title: "t"}, nil); !strings.Contains(got, "(nothing stored near it)") {
		t.Fatalf("an empty neighbour list renders %q", got)
	}
}

// ── the model the calls run on ──────────────────────────────────────────────

// THE TIER IS THE POINT. A reflex call takes the reflex tier's model, not the
// low tier's, and a person's pin still outranks both.
func TestModelWalksTheReflexRolesLadder(t *testing.T) {
	src := roles.Source(func(key string) (string, bool) {
		value, ok := map[string]string{
			"tiers.reflex": "vendor/tiny",
			"tiers.low":    "vendor/cheap",
			"tiers.high":   "vendor/careful",
		}[key]
		return value, ok
	})
	model, err := Model(src, "vendor/conversation")
	if err != nil {
		t.Fatalf("Model: %v", err)
	}
	if model != "vendor/tiny" {
		t.Fatalf("Model = %q, want the reflex tier's own model", model)
	}
	// Nothing configured is the conversation's own model, which is the ladder's
	// floor and the reason a fresh install still works.
	if model, err = Model(nil, "vendor/conversation"); err != nil || model != "vendor/conversation" {
		t.Fatalf("Model with nothing set = %q, %v", model, err)
	}
}

func TestBindSendsEveryReflexCallToOneModel(t *testing.T) {
	client := &fake{replies: []string{`{"inject":[],"cmd":null}`}}
	if _, err := Route(context.Background(), Bind(client, "vendor/tiny"), "hello", index); err != nil {
		t.Fatalf("Route: %v", err)
	}
	if got := client.calls[0].Model; got != "vendor/tiny" {
		t.Fatalf("a bound call named model %q", got)
	}
	if request := client.calls[0]; request.MaxTokens == nil || *request.MaxTokens != answerTokens {
		t.Fatal("binding a model dropped the token ceiling")
	}
	// Binding nothing is not binding the empty string.
	if _, ok := Bind(client, "  ").(bound); ok {
		t.Fatal("Bind(\"\") wrapped the client in a pin to nowhere")
	}
}

// ── the prompts ─────────────────────────────────────────────────────────────

// The prompts name the words the parser accepts, because they are rendered from
// the parser's own lists. A word missing here is a call that fails every time.
func TestThePromptsSpellTheEnumsTheParserAccepts(t *testing.T) {
	for _, word := range memoryTypes {
		if !strings.Contains(extractPrompt, word) {
			t.Fatalf("the extract prompt never names the type %q", word)
		}
	}
	for _, word := range memoryScopes {
		if !strings.Contains(extractPrompt, word) {
			t.Fatalf("the extract prompt never names the scope %q", word)
		}
	}
	for _, word := range decideOps {
		if !strings.Contains(decidePrompt, word) {
			t.Fatalf("the decide prompt never names the operation %q", word)
		}
	}
	for _, word := range commandNames {
		if !strings.Contains(routePrompt, word) {
			t.Fatalf("the route prompt never names the command %q", word)
		}
	}
	for name, text := range map[string]string{
		"route": routePrompt, "extract": extractPrompt, "decide": decidePrompt,
	} {
		if !strings.Contains(text, "ONE JSON object and nothing else") {
			t.Fatalf("the %s prompt never demands one JSON object", name)
		}
	}
}
