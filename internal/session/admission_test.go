package session

// What the admission context must be true about, at the boundaries that broke
// it elsewhere: a correction later than the budget, a quote that has to be
// checkable, an outcome nobody recorded, inheritance that must not run away, a
// record that has to survive JSON and a session that has to survive a restart.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the fixture ─────────────────────────────────────────────────────────────

// saidTurns builds the person's remembered turns, oldest first.
func saidTurns(texts ...string) []personTurn {
	turns := make([]personTurn, 0, len(texts))
	for index, text := range texts {
		turns = append(turns, personTurn{
			seq:  uint64(index + 1),
			key:  chatRefKey(textMessage("user", text)),
			text: elide(text, admissionQuoteLimit),
		})
	}
	return turns
}

// transcriptOf lays the same turns out as a transcript, so the compiler can
// place them in the order they were said.
func transcriptOf(turns []personTurn, extra ...ai.Message) []ai.Message {
	messages := make([]ai.Message, 0, len(turns)+len(extra))
	for _, turn := range turns {
		messages = append(messages, textMessage("user", turn.text))
	}
	return append(messages, extra...)
}

func quotedTexts(context AdmissionContext) []string {
	out := make([]string, 0, len(context.Quotes))
	for _, quote := range context.Quotes {
		out = append(out, quote.Text)
	}
	return out
}

func hasQuote(context AdmissionContext, want string) bool {
	for _, quote := range context.Quotes {
		if strings.Contains(quote.Text, want) {
			return true
		}
	}
	return false
}

// ── the correction after the budget ─────────────────────────────────────────

// THE TWENTY-FIRST CORRECTION IS THE ONE THAT MATTERS. A walker that credits
// entries from the oldest end spends its budget on settled history and drops
// the sentence that changed the job — the one thing a worker cannot recover by
// reading harder.
func TestAdmissionKeepsTheNewestCorrectionPastTwentyExchanges(t *testing.T) {
	texts := make([]string, 0, 24)
	for index := 0; index < 23; index++ {
		texts = append(texts, fmt.Sprintf("earlier point number %d about the importer", index))
	}
	texts = append(texts, "actually use tabs, not spaces, in the generated file")
	turns := saidTurns(texts...)

	compiled := compileAdmission(admissionSource{
		said: turns, messages: transcriptOf(turns), scope: "chat/s1",
	})

	if !hasQuote(compiled, "actually use tabs") {
		t.Fatalf("the newest correction did not survive the budget: %q", quotedTexts(compiled))
	}
	if hasQuote(compiled, "number 0 about") {
		t.Fatalf("an exchange from the far end was preferred over the newest: %q", quotedTexts(compiled))
	}
	if len(compiled.Quotes) > admissionQuotesKept {
		t.Fatalf("the selection carried %d quotes, past the bound of %d", len(compiled.Quotes), admissionQuotesKept)
	}
	// AND THEY ARE READ IN THE ORDER THEY WERE SAID. A correction printed above
	// the thing it corrects is the conversation backwards.
	last := compiled.Quotes[len(compiled.Quotes)-1]
	if !strings.Contains(last.Text, "actually use tabs") {
		t.Fatalf("the newest line is not last:\n%q", quotedTexts(compiled))
	}
}

// A CONSTRAINT IN THE LAST SENTENCE OF A LONG MESSAGE SURVIVES THE CLIP. A
// head-only bound drops exactly the words a worker most needs, which is why the
// middle is what goes.
func TestAdmissionElidesTheMiddleAndKeepsBothEnds(t *testing.T) {
	long := "rewrite the importer so it streams. " + strings.Repeat("background about the old one. ", 80) +
		"and whatever you do, do not touch the tests."
	turns := saidTurns(long)

	compiled := compileAdmission(admissionSource{
		said: turns, messages: transcriptOf(turns), scope: "chat/s1",
	})

	if len(compiled.Quotes) != 1 {
		t.Fatalf("want one quote, got %d", len(compiled.Quotes))
	}
	text := compiled.Quotes[0].Text
	if len(text) > admissionQuoteLimit {
		t.Fatalf("the quote is %d bytes, past the %d bound", len(text), admissionQuoteLimit)
	}
	for _, want := range []string{"rewrite the importer", "do not touch the tests", elisionMark} {
		if !strings.Contains(text, want) {
			t.Fatalf("the elided quote lost %q:\n%s", want, text)
		}
	}
}

// THE SAME WORDS TYPED TWICE ARE TWO INSTRUCTIONS. A person who repeats a
// sentence after a correction has said something new, and an identity made of
// the words alone would collapse the two into one event.
func TestAdmissionIdentityIsTheEventAndNotTheWords(t *testing.T) {
	turns := saidTurns("use the streaming parser", "no, buffer it after all", "use the streaming parser")

	compiled := compileAdmission(admissionSource{
		said: turns, messages: transcriptOf(turns), scope: "chat/s1",
	})

	if len(compiled.Quotes) != 3 {
		t.Fatalf("want three separate turns, got %d: %q", len(compiled.Quotes), quotedTexts(compiled))
	}
	ids := map[string]bool{}
	for _, quote := range compiled.Quotes {
		if ids[quote.ID] {
			t.Fatalf("two turns share the id %q", quote.ID)
		}
		ids[quote.ID] = true
	}
	// AND THE SCOPE IS PART OF IT: the same sentence in another session or
	// another task is not the same event.
	elsewhere := compileAdmission(admissionSource{
		said: turns[:1], messages: transcriptOf(turns[:1]), scope: "task 7/s2",
	})
	if elsewhere.Quotes[0].ID == compiled.Quotes[0].ID {
		t.Fatalf("two scopes minted one id: %q", elsewhere.Quotes[0].ID)
	}
}

// ── text and calls are not alternatives ─────────────────────────────────────

// AN ASSISTANT MESSAGE THAT CONCLUDES AND CALLS IN ONE BREATH IS THE ORDINARY
// SHAPE OF WORK. A walker that treats "has text" and "has calls" as exclusive
// drops precisely those.
func TestAdmissionKeepsAssistantTextThatAccompaniesACall(t *testing.T) {
	said := ai.Message{
		Role:      "assistant",
		Content:   []ai.ContentPart{{Type: "text", Text: "the parser is StreamCSV; reading its callers next"}},
		ToolCalls: []ai.ToolCall{{ID: "c1", Function: ai.ToolCallFunction{Name: "grep", Arguments: `{"pattern":"StreamCSV"}`}}},
	}
	compiled := compileAdmission(admissionSource{
		messages: []ai.Message{said, {Role: "tool", ToolCallID: "c1", Content: []ai.ContentPart{{Type: "text", Text: "3 hits"}}}},
		outcomes: map[string]callOutcome{"c1": {tool: "grep"}},
		scope:    "chat/s1",
	})

	if len(compiled.Quotes) != 1 {
		t.Fatalf("the text beside a call was dropped: %+v", compiled.Quotes)
	}
	quote := compiled.Quotes[0]
	if quote.Speaker != admissionAssistant || len(quote.Calls) != 1 || quote.Calls[0] != "grep" {
		t.Fatalf("the call it was said alongside is not on the quote: %+v", quote)
	}
	// AND IT IS NOT A FINDING. The rendered line says who said it, and the rule
	// over the section says what that is worth.
	if !strings.Contains(quote.line(), "the assistant") {
		t.Fatalf("the quote does not name its speaker: %s", quote.line())
	}
	if len(compiled.Evidence) != 1 || compiled.Evidence[0].Input != `{"pattern":"StreamCSV"}` {
		t.Fatalf("the exact input reference was lost: %+v", compiled.Evidence)
	}
}

// ── nothing missing is reported as something good ───────────────────────────

func TestAdmissionNeverReadsAMissingOutcomeAsSuccess(t *testing.T) {
	calls := ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{
		{ID: "ok", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"a.go"}`}},
		{ID: "bad", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"go build ./..."}`}},
		{ID: "quiet", Function: ai.ToolCallFunction{Name: "grep", Arguments: `{"pattern":"x"}`}},
		{ID: "never", Function: ai.ToolCallFunction{Name: "ls", Arguments: `{}`}},
	}}
	messages := []ai.Message{calls}
	for _, id := range []string{"ok", "bad", "quiet"} {
		messages = append(messages, ai.Message{Role: "tool", ToolCallID: id,
			Content: []ai.ContentPart{{Type: "text", Text: "body of " + id}}})
	}
	compiled := compileAdmission(admissionSource{
		messages: messages,
		outcomes: map[string]callOutcome{
			"ok":  {tool: "read"},
			"bad": {tool: "bash", failed: true, detail: "undefined: streamCSV"},
		},
		scope: "chat/s1",
	})

	byCall := map[string]AdmissionHandle{}
	for _, handle := range compiled.Evidence {
		byCall[handle.Call] = handle
	}
	if got := byCall["ok"].Outcome; got != AdmissionOK {
		t.Errorf("a recorded success reads %q", got)
	}
	if got := byCall["bad"]; got.Outcome != AdmissionFailed || !strings.Contains(got.line(), "undefined: streamCSV") {
		t.Errorf("the failure lost its own words: %+v / %s", got, got.line())
	}
	// THE TWO SHAPES OF NOT KNOWING, and neither of them is success.
	if got := byCall["quiet"]; got.Outcome != AdmissionUnknown || !strings.Contains(got.line(), "outcome unknown") {
		t.Errorf("an unrecorded outcome was not said to be unknown: %s", got.line())
	}
	if got := byCall["never"]; got.Outcome != AdmissionUnanswered || strings.Contains(got.line(), "came back") {
		t.Errorf("a call with no result was reported as returning: %s", got.line())
	}
	// AND A FAILURE OUTRANKS A SUCCESS OF THE SAME AGE when the room runs out.
	if compiled.Evidence[0].Call != "bad" && len(compiled.Evidence) == admissionHandlesKept {
		t.Errorf("the failure was not preferred: %+v", compiled.Evidence)
	}
}

// ── inheritance is bounded ──────────────────────────────────────────────────

// A GRANDCHILD CARRIES ITS FAMILY'S HISTORY, A GREAT-GREAT-GRANDCHILD DOES NOT.
// Without the generation bound every level re-carries every level above it and
// the document grows with the depth of the tree.
func TestAdmissionInheritanceIsBoundedByGeneration(t *testing.T) {
	root := compileAdmission(admissionSource{
		said:     saidTurns("build the importer", "and keep the CSV column order"),
		messages: transcriptOf(saidTurns("build the importer", "and keep the CSV column order")),
		scope:    "chat/s1",
	})
	if len(root.Quotes) != 2 {
		t.Fatalf("the root context is %d quotes, want two", len(root.Quotes))
	}

	generation := root
	for depth := 1; depth <= admissionDepthLimit+1; depth++ {
		generation = compileAdmission(admissionSource{
			scope: "task " + strconv.Itoa(depth) + "/s1", from: "task " + strconv.Itoa(depth),
			inherited: generation,
		})
		if len(generation.Quotes) > admissionInherited {
			t.Fatalf("generation %d carries %d quotes, past the inherited bound of %d",
				depth, len(generation.Quotes), admissionInherited)
		}
		for _, quote := range generation.Quotes {
			if quote.Depth > admissionDepthLimit {
				t.Fatalf("generation %d carries an entry %d generations old", depth, quote.Depth)
			}
		}
	}
	if len(generation.Quotes) != 0 {
		t.Fatalf("past the generation bound the family's conversation is still being carried: %q",
			quotedTexts(generation))
	}
}

// A CHILD'S OWN CONTEXT IS NOT DUPLICATED BY ITS INHERITANCE. The same entry
// arriving twice — once locally, once from the parent — is one entry.
func TestAdmissionInheritanceDoesNotDuplicateWhatIsAlreadyLocal(t *testing.T) {
	turns := saidTurns("build the importer")
	source := admissionSource{said: turns, messages: transcriptOf(turns), scope: "chat/s1"}
	root := compileAdmission(source)

	source.inherited = root
	twice := compileAdmission(source)

	if len(twice.Quotes) != 1 {
		t.Fatalf("one sentence was carried %d times: %q", len(twice.Quotes), quotedTexts(twice))
	}
}

// ── the record survives the machinery ───────────────────────────────────────

// A CONTEXT OF UNEXPORTED FIELDS SERIALISES AS `{}` AND A RESUMED TASK READS AN
// EMPTY DOCUMENT UNDER A FULL HEADING. This is the test that fails first if
// anybody makes that change.
func TestAdmissionContextSurvivesJSON(t *testing.T) {
	original := AdmissionContext{
		Version: AdmissionContextVersion,
		Quotes: []AdmissionQuote{{
			ID: "chat/s1/p3", Speaker: admissionPerson, Text: "keep the CSV column order",
			Source: "/tmp/s.jsonl",
		}, {
			ID: "chat/s1/m8", Speaker: admissionAssistant, Text: "the parser is StreamCSV",
			Calls: []string{"grep"}, From: "task 4", Depth: 1,
		}},
		Evidence: []AdmissionHandle{{
			Call: "c1", Tool: "bash", Input: `{"command":"go build"}`,
			Outcome: AdmissionFailed, Detail: "undefined: streamCSV",
		}},
	}

	encoded, err := json.Marshal(taskRecord{ID: 4, Admission: recordedAdmission(original)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), `{}`) {
		t.Fatalf("an entry serialised as an empty object:\n%s", encoded)
	}
	var back taskRecord
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	restored := restoredAdmission(back.Admission)
	if len(restored.Quotes) != 2 || restored.Quotes[0].Text != "keep the CSV column order" {
		t.Fatalf("the quotes did not survive the round trip: %+v", restored.Quotes)
	}
	if restored.Quotes[0].Source != "/tmp/s.jsonl" {
		t.Fatalf("the source address did not survive: %+v", restored.Quotes[0])
	}
	if len(restored.Evidence) != 1 || restored.Evidence[0].Outcome != AdmissionFailed {
		t.Fatalf("the evidence did not survive: %+v", restored.Evidence)
	}
	// A RECORD FROM A VERSION THIS BUILD DOES NOT KNOW IS DROPPED, not guessed
	// at, and an absent one is ordinary.
	ahead := original
	ahead.Version = AdmissionContextVersion + 1
	if got := ahead.restored(); !got.empty() {
		t.Fatalf("a record from a later version was read anyway: %+v", got)
	}
	if got := restoredAdmission(nil); !got.empty() {
		t.Fatalf("an absent record invented one: %+v", got)
	}
}

// ── what the worker actually reads ──────────────────────────────────────────

// THE DOCUMENT SAYS WHAT THE QUOTES ARE WORTH. The whole safety of carrying
// somebody's sentences into a worker's prompt is that the worker is told they
// were SAID, that the selection is partial, and that the work above is still
// the work.
func TestTheWorkerIsToldQuotesAreSaidAndNotSettled(t *testing.T) {
	compiled := AdmissionContext{Version: AdmissionContextVersion, Quotes: []AdmissionQuote{{
		ID: "chat/s1/p2", Speaker: admissionPerson, Text: "keep the CSV column order",
		Source: "/tmp/s.jsonl",
	}}}
	opening := composeBrief("rewrite the importer", "edit importer.go", "importer.go", "it streams",
		"", compiled, taskOrigin{}, taskCopy{})

	for _, want := range []string{
		admissionQuotesHeading, "not the whole record", "what was SAID",
		"the person", "keep the CSV column order",
		"grep or read /tmp/s.jsonl",
	} {
		if !strings.Contains(opening, want) {
			t.Fatalf("the worker's document is missing %q:\n%s", want, opening)
		}
	}
	// AND THE CONTRACT COMES FIRST. A document that opened on the conversation
	// would read as instruction with a contract appended.
	if strings.Index(opening, briefDoneHeading) > strings.Index(opening, admissionQuotesHeading) {
		t.Fatalf("the quotes were printed above the contract:\n%s", opening)
	}
	// AND AN EMPTY CONTEXT DRAWS NOTHING — the emptiness law.
	plain := composeBrief("rewrite the importer", "edit importer.go", "", "", "",
		AdmissionContext{}, taskOrigin{}, taskCopy{})
	if strings.Contains(plain, admissionQuotesHeading) || strings.Contains(plain, admissionEvidenceHeading) {
		t.Fatalf("a task with no context got a heading over nothing:\n%s", plain)
	}
}

// AN IDENTIFIER NO TOOL CAN RESOLVE IS WORSE THAN NO POINTER. A quote the
// journal could not place says the file alone, and one with no journal at all
// says nothing rather than a hash.
func TestAQuoteNeverPointsAtSomethingUnreachable(t *testing.T) {
	placed := AdmissionQuote{Speaker: admissionPerson, Text: "x", Source: "/tmp/s.jsonl"}
	if got := placed.line(); !strings.Contains(got, "grep or read /tmp/s.jsonl") {
		t.Fatalf("a quote with a record did not point at it: %s", got)
	}
	nowhere := AdmissionQuote{ID: "chat/s1/p1", Speaker: admissionPerson, Text: "x"}
	if got := nowhere.line(); strings.Contains(got, "chat/s1/p1") || strings.Contains(got, "read") {
		t.Fatalf("a quote with no journal pointed somewhere anyway: %s", got)
	}
}

// ── the doors ───────────────────────────────────────────────────────────────

// admissionAgent is a conversation with a journal, which is what a reachable
// source pointer needs.
func admissionAgent(t *testing.T, completer Completer, path string) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = path
		config.TaskAudit = false
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	return agent
}

// EVERY QUOTE'S ADDRESS IS A FILE A WORKER CAN OPEN AT THAT LINE. This drives
// the real conversation, the real journal and the real typed-task door, and then
// reads the address out of the composed brief and opens it.
func TestAnAdmittedQuoteAddressesALineThatIsReallyThere(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{}
	agent := admissionAgent(t, completer, journal)

	collect(t, mustSubmit(t, agent, "the CSV column order has to survive the rewrite"))
	collect(t, mustSubmit(t, agent, "now start on the importer"))

	id, _, _, err := agent.StartTask(context.Background(), "rewrite the importer so it streams")
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	node := agent.graph().node(id)
	if node == nil {
		t.Fatal("the typed door admitted nothing")
	}
	opening := node.instruction()
	if !strings.Contains(opening, "CSV column order") {
		t.Fatalf("the earlier constraint did not reach the worker:\n%s", opening)
	}

	// The address, as the worker would read it, opened as the worker would open
	// it.
	place := admissionPlace(t, node.admission(), "CSV column order")
	if !strings.Contains(opening, "grep or read "+place.Source) {
		t.Fatalf("the document does not carry the address:\n%s", opening)
	}
	body, err := os.ReadFile(place.Source)
	if err != nil {
		t.Fatalf("the address a worker was given does not open: %v", err)
	}
	if !strings.Contains(string(body), "CSV column order") {
		t.Fatalf("the record a worker was sent to does not hold the quoted words")
	}
}

func admissionPlace(t *testing.T, context AdmissionContext, want string) AdmissionQuote {
	t.Helper()
	for _, quote := range context.Quotes {
		if strings.Contains(quote.Text, want) {
			return quote
		}
	}
	t.Fatalf("no quote carrying %q: %+v", want, context.Quotes)
	return AdmissionQuote{}
}

// THE MODEL'S OWN DOOR CARRIES THE SAME THING, through the real propose_task
// with the real arguments a model would write.
func TestProposeTaskCarriesTheWorkingContext(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{}
	agent := admissionAgent(t, completer, journal)
	stubbedGraph(agent, func(*TaskNode) {})
	collect(t, mustSubmit(t, agent, "keep the CSV column order whatever else changes"))
	collect(t, mustSubmit(t, agent, "rewrite the importer"))

	answer, isError, err := agent.proposeTask(context.Background(), json.RawMessage(
		`{"title":"importer","summary":"rewrite it","brief":"rewrite importer.go so it streams",`+
			`"deliverable":"importer.go","acceptance":"the importer streams"}`))
	if err != nil || isError {
		t.Fatalf("propose_task: %v / %q", err, answer)
	}
	id := admittedID(t, answer)
	opening := agent.graph().node(id).instruction()
	if !strings.Contains(opening, "CSV column order") {
		t.Fatalf("the proposal door dropped the earlier constraint:\n%s", opening)
	}
	if !strings.Contains(opening, admissionQuotesRule) {
		t.Fatalf("the quotes arrived without the rule that says what they are:\n%s", opening)
	}
}

// admittedID reads the id off the tool's own receipt, which is the only place a
// caller of propose_task learns it.
func admittedID(t *testing.T, answer string) uint64 {
	t.Helper()
	var id uint64
	if _, err := fmt.Sscanf(answer, "task %d", &id); err != nil || id == 0 {
		t.Fatalf("no task id in the receipt %q: %v", answer, err)
	}
	return id
}

// A REOPENED SESSION STILL HANDS OUT THE CONVERSATION IT HAD. The messages come
// back from the journal, but which of them the PERSON typed does not — that mark
// is on the record and has to be read back, or every task started after a
// restart silently loses its context.
func TestAdmissionSurvivesASessionReopen(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	first := admissionAgent(t, &scriptedCompleter{}, journal)
	collect(t, mustSubmit(t, first, "the CSV column order has to survive the rewrite"))
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := admissionAgent(t, &scriptedCompleter{}, journal)
	id, _, _, err := second.StartTask(context.Background(), "rewrite the importer so it streams")
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	opening := second.graph().node(id).instruction()
	if !strings.Contains(opening, "CSV column order") {
		t.Fatalf("work started after a reopen carries none of the conversation:\n%s", opening)
	}
	// AND THE SESSION'S OWN NOTES ARE NOT QUOTED AS THE PERSON. A task's landing
	// note is user-role too, and the journal's mark is the only thing that tells
	// them apart.
	for _, quote := range second.graph().node(id).admission().Quotes {
		if quote.Speaker == admissionPerson && strings.Contains(quote.Text, "[task ") {
			t.Fatalf("a note the session wrote was quoted as the person: %q", quote.Text)
		}
	}
}

// EVERY PART OF A DIVISION INHERITS THE FAMILY'S CONTEXT, through the real
// `divide_work` on a real graph. A part that lost it is a part that rediscovers
// what the conversation above it already settled — and the division road is
// where that used to happen, because it is the one door that builds its own
// spec for each child.
func TestDivisionPartsInheritTheAdmissionContext(t *testing.T) {
	parentContext := AdmissionContext{Version: AdmissionContextVersion, Quotes: []AdmissionQuote{{
		ID: "chat/s1/p1", Speaker: admissionPerson, Text: "keep the CSV column order",
		Source: "/tmp/s.jsonl",
	}}}
	family := newWipFamilyFrom(t, TaskModeWorktree, taskSpec{
		title: "the whole job", request: personSentence, brief: "do the whole job",
		acceptance: "it is done", depth: 1, admission: parentContext,
	}, nil)

	// What the dividing worker itself found out, before it decided to divide.
	family.worker.mu.Lock()
	family.worker.messages = append(family.worker.messages, ai.Message{
		Role:      "assistant",
		Content:   []ai.ContentPart{{Type: "text", Text: "the adapters live under internal/adapters"}},
		ToolCalls: []ai.ToolCall{{ID: "d1", Function: ai.ToolCallFunction{Name: "grep", Arguments: `{"pattern":"adapter"}`}}},
	})
	family.worker.mu.Unlock()

	family.divide(t)

	parts := family.graph.children(family.parent.id)
	if len(parts) != 2 {
		t.Fatalf("the division admitted %d parts, want two", len(parts))
	}
	for _, part := range parts {
		context := part.admission()
		if len(context.Quotes) == 0 {
			t.Fatalf("part %d was admitted with no working context at all", part.id)
		}
		inherited, local := false, false
		for _, quote := range context.Quotes {
			if strings.Contains(quote.Text, "CSV column order") {
				inherited = true
				if quote.Depth != 1 {
					t.Errorf("the family's own line is %d generations old on part %d", quote.Depth, part.id)
				}
				// AND ITS ADDRESS TRAVELS WITH IT: an inherited quote keeps the
				// journal its own admission resolved, not the child's.
				if quote.Source != "/tmp/s.jsonl" {
					t.Errorf("the inherited quote lost its address: %+v", quote)
				}
			}
			if strings.Contains(quote.Text, "internal/adapters") {
				local = true
				if len(quote.Calls) != 1 || quote.Calls[0] != "grep" {
					t.Errorf("the call the worker's own line was said alongside was lost: %+v", quote)
				}
			}
		}
		if !inherited {
			t.Errorf("part %d lost the conversation the family came out of: %q", part.id, quotedTexts(context))
		}
		if !local {
			t.Errorf("part %d lost what the dividing worker had just found: %q", part.id, quotedTexts(context))
		}
		// AND THE WORKER READS IT UNDER THE RULE THAT SAYS WHAT IT IS.
		if opening := part.instruction(); !strings.Contains(opening, admissionQuotesRule) {
			t.Errorf("part %d reads quotes with no rule over them:\n%s", part.id, opening)
		}
	}
}

// ── the turns the window no longer holds ────────────────────────────────────

// A COMPACTED-AWAY CORRECTION STILL BEATS THE THING IT CORRECTS. Turns the
// working window has dropped are placed before it, and among themselves they
// keep the order they were heard in — so the newest of them is selected first
// and read last, exactly like every quote still in the window.
func TestCompactedTurnsKeepTheOrderTheyWereHeardIn(t *testing.T) {
	turns := saidTurns("use the streaming parser for the importer",
		"no — buffer the whole file, the parser cannot seek")
	// The window holds neither of them: a fold rewrote the transcript and the
	// only record of the two is what the session remembered.
	compiled := compileAdmission(admissionSource{
		said: turns, messages: []ai.Message{textMessage("assistant", "starting on it")},
		scope: "chat/s1",
	})

	if len(compiled.Quotes) < 2 {
		t.Fatalf("a compacted-away turn was dropped: %q", quotedTexts(compiled))
	}
	first, second := -1, -1
	for index, quote := range compiled.Quotes {
		if strings.Contains(quote.Text, "use the streaming parser") {
			first = index
		}
		if strings.Contains(quote.Text, "buffer the whole file") {
			second = index
		}
	}
	if first < 0 || second < 0 {
		t.Fatalf("one of the two conflicting turns is missing: %q", quotedTexts(compiled))
	}
	if second < first {
		t.Fatalf("the correction was printed above the thing it corrects: %q", quotedTexts(compiled))
	}

	// AND UNDER PRESSURE THE NEWER ONE IS THE ONE THAT SURVIVES. With room for a
	// single quote, selection must prefer the correction.
	long := strings.Repeat("x", admissionQuoteLimit)
	crowd := saidTurns(long+" first", long+" the correction")
	tight := compileAdmission(admissionSource{
		said: crowd, messages: []ai.Message{textMessage("assistant", "working")}, scope: "chat/s1",
	})
	newest := ""
	for _, quote := range tight.Quotes {
		if quote.Speaker == admissionPerson {
			newest = quote.Text
		}
	}
	if !strings.Contains(newest, "the correction") {
		t.Fatalf("under pressure the older compacted turn won: %q", quotedTexts(tight))
	}
}

// ── the budget bounds what is actually rendered ─────────────────────────────

// THE BOUND IS ON THE DOCUMENT, NOT ON A SUM OF FIELDS. Long paths, long tool
// names and the tools a line was said alongside are all rendered, so a cost
// model that counted only text and speaker would promise a bound it does not
// keep.
func TestTheRenderedContextStaysInsideItsBudget(t *testing.T) {
	record := "/very/long/path/to/a/workspace/that/somebody/really/has/.aforge/v3/sessions/" +
		strings.Repeat("deep/", 12) + "session.jsonl"
	turns := saidTurns(func() []string {
		texts := make([]string, 0, admissionTurnsRemembered)
		for index := 0; index < admissionTurnsRemembered; index++ {
			texts = append(texts, strings.Repeat("a constraint that goes on. ", 40)+strconv.Itoa(index))
		}
		return texts
	}()...)
	messages := transcriptOf(turns)
	outcomes := map[string]callOutcome{}
	for index := 0; index < 12; index++ {
		id := "call-with-a-long-identifier-" + strconv.Itoa(index)
		name := "a_tool_with_an_unusually_long_name_" + strconv.Itoa(index)
		messages = append(messages, ai.Message{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: strings.Repeat("thinking out loud. ", 40)}},
			ToolCalls: []ai.ToolCall{{ID: id, Function: ai.ToolCallFunction{
				Name: name, Arguments: `{"path":"` + strings.Repeat("x", 400) + `"}`}}},
		}, ai.Message{Role: "tool", ToolCallID: id, Content: []ai.ContentPart{{Type: "text", Text: "body"}}})
		outcomes[id] = callOutcome{tool: name, failed: index%2 == 0,
			detail: strings.Repeat("why it failed. ", 40)}
	}

	compiled := compileAdmission(admissionSource{
		said: turns, messages: messages, outcomes: outcomes, record: record,
		from: "task 12", scope: "task 12/s1",
	})

	rendered := admissionQuotesHeading + "\n" + admissionQuotesRule + "\n\n" +
		admissionQuotesSection(compiled) + "\n\n" +
		admissionEvidenceHeading + "\n" + admissionEvidenceRule + "\n\n" +
		admissionEvidenceSection(compiled)
	if len(rendered) > admissionBudget {
		t.Fatalf("the rendered context is %d bytes, past the %d it promises:\n%s",
			len(rendered), admissionBudget, rendered)
	}
	if len(compiled.Quotes) == 0 || len(compiled.Evidence) == 0 {
		t.Fatalf("the budget squeezed the context to nothing: %d quotes, %d handles",
			len(compiled.Quotes), len(compiled.Evidence))
	}
}

// ── the pointer a worker is given actually resolves ─────────────────────────

// A HANDLE'S POINTER IS FETCHED THE WAY THE LINE SAYS TO FETCH IT. The rendered
// line tells the worker to grep the call id in the record; this drives a real
// conversation that really ran a tool, then does exactly that.
func TestAHandlePointsAtAResultThatCanBeFetched(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponseWithText("call-1", "read", `{"path":"notes.md"}`,
				"opening the notes before anything else"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the notes say the column order matters"), nil
		},
	}}
	agent := admissionAgent(t, completer, journal)
	stubbedGraph(agent, func(*TaskNode) {})
	writeFile(t, filepath.Join(agent.config.Workspace, "notes.md"), "keep the CSV column order\n")

	collect(t, mustSubmit(t, agent, "look at the notes and then rewrite the importer"))

	id, _, _, err := agent.StartTask(context.Background(), "rewrite the importer so it streams")
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	admitted := agent.graph().node(id).admission()
	if len(admitted.Evidence) == 0 {
		t.Fatalf("the call this turn made is not in the context: %+v", admitted)
	}
	handle := admitted.Evidence[0]
	if handle.Source == "" {
		t.Fatalf("the handle points nowhere: %+v", handle)
	}
	if !strings.Contains(handle.line(), "grep "+handle.Call+" in "+handle.Source) {
		t.Fatalf("the line does not say how to fetch it: %s", handle.line())
	}
	// The fetch, exactly as the line describes it.
	body, err := os.ReadFile(handle.Source)
	if err != nil {
		t.Fatalf("the record a worker was sent to does not open: %v", err)
	}
	found := ""
	for _, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, handle.Call) && strings.Contains(line, `"role":"tool"`) {
			found = line
		}
	}
	if found == "" {
		t.Fatalf("grepping %s in the record finds no result line", handle.Call)
	}
	if !strings.Contains(found, "keep the CSV column order") {
		t.Fatalf("the line the call id leads to is not the result:\n%s", found)
	}
}
