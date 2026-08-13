package head

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// scribeClientRecorder is a fake provider that answers naming calls and
// remembers what it was asked and how it was capped. The cap is asserted
// because the whole economy of this pass is "a clerk, not a judge": a call with
// no ceiling on a room that opened with a long deliverable is the one way four
// words become expensive.
type scribeClientRecorder struct {
	mutex   sync.Mutex
	answers []string
	// raw is scripted ahead of answers, for a test that needs a reply shape
	// textResponse cannot make — an empty completion wearing a finish reason.
	raw       []*ai.Response
	calls     int
	prompts   []string
	maxTokens int
	ceilings  []int
	model     string
}

func (client *scribeClientRecorder) CompleteWithMessages(_ context.Context, messages []ai.Message,
	options ...ai.Option) (*ai.Response, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.calls++
	request := ai.Request{Messages: messages}
	for _, option := range options {
		_ = option(&request)
	}
	if request.MaxTokens != nil {
		client.maxTokens = *request.MaxTokens
		client.ceilings = append(client.ceilings, *request.MaxTokens)
	}
	if len(client.raw) > 0 {
		response := client.raw[0]
		client.raw = client.raw[1:]
		return response, nil
	}
	var seen strings.Builder
	for _, message := range messages {
		for _, part := range message.Content {
			seen.WriteString(part.Text)
			seen.WriteString("\n")
		}
	}
	client.prompts = append(client.prompts, seen.String())
	if len(client.answers) == 0 {
		return textResponse("Untitled"), nil
	}
	answer := client.answers[0]
	client.answers = client.answers[1:]
	response := textResponse(answer)
	response.Model = client.model
	return response, nil
}

func (client *scribeClientRecorder) count() int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.calls
}

// seedExchange puts one real exchange in a room: the person's words and the
// answer that followed them.
func seedExchange(t *testing.T, graph *store.Store, sessionID, ask, answer string) {
	t.Helper()
	if _, err := graph.PostMessage(store.Message{
		SessionID: sessionID, Role: store.RoleUser, Body: ask,
	}); err != nil {
		t.Fatal(err)
	}
	if answer == "" {
		return
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: sessionID, Role: store.RoleAgent, Body: answer,
	}); err != nil {
		t.Fatal(err)
	}
}

func roomTitleOf(t *testing.T, graph *store.Store, sessionID string) string {
	t.Helper()
	session, found, err := graph.Session(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		return ""
	}
	return session.Title
}

// The room gets its name from the exchange that made it a conversation, and
// the call that buys it is one cheap call with a ceiling on it.
func TestTheScribeNamesARoomFromItsFirstExchange(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "can you audit last quarter's billing code",
		"Yes — I'll go through the billing package and report what I find.")

	scribe := &scribeClientRecorder{answers: []string{"Billing code audit"}}
	New(scribe, graph).nameRoom(context.Background(), "room")

	if title := roomTitleOf(t, graph, "room"); title != "Billing code audit" {
		t.Fatalf("the room is called %q", title)
	}
	if scribe.count() != 1 {
		t.Fatalf("naming cost %d calls, want exactly one", scribe.count())
	}
	if scribe.maxTokens != scribeMaxTokens {
		t.Fatalf("the naming call was capped at %d tokens, want %d", scribe.maxTokens, scribeMaxTokens)
	}
	prompt := scribe.prompts[0]
	for _, required := range []string{"2 to 5 words", "audit last quarter's billing code", "billing package"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("the naming call did not carry %q:\n%s", required, prompt)
		}
	}
}

// Once per room, and the room's own title is the record that it happened.
// There is no second flag to keep in step with the thing it describes.
func TestTheScribeNamesARoomOnlyOnce(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "audit the billing code", "On it.")

	scribe := &scribeClientRecorder{answers: []string{"Billing code audit", "Something else entirely"}}
	conversationalHead := New(scribe, graph)
	conversationalHead.nameRoom(context.Background(), "room")
	conversationalHead.nameRoom(context.Background(), "room")

	if scribe.count() != 1 {
		t.Fatalf("the scribe ran %d times, want once per room", scribe.count())
	}
	if title := roomTitleOf(t, graph, "room"); title != "Billing code audit" {
		t.Fatalf("the room was renamed to %q", title)
	}

	// And a room somebody named by hand is a room with a name: the scribe never
	// speaks over it, in this process or the next one.
	if _, err := graph.RenameSession("room", "quarterly numbers"); err != nil {
		t.Fatal(err)
	}
	New(scribe, graph).nameRoom(context.Background(), "room")
	if scribe.count() != 1 {
		t.Fatal("the scribe re-titled a room that already had a name")
	}
	if title := roomTitleOf(t, graph, "room"); title != "quarterly numbers" {
		t.Fatalf("the hand-typed name became %q", title)
	}
}

// A room with words in only one direction is not a conversation yet. Naming it
// from the ask alone would name the question rather than the room.
func TestTheScribeWaitsForTheReply(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "audit the billing code", "")

	scribe := &scribeClientRecorder{answers: []string{"Billing code audit"}}
	conversationalHead := New(scribe, graph)
	conversationalHead.nameRoom(context.Background(), "room")
	if scribe.count() != 0 {
		t.Fatalf("the scribe named a room with no reply in it (%d calls)", scribe.count())
	}
	if title := roomTitleOf(t, graph, "room"); title != "" {
		t.Fatalf("an unanswered room was called %q", title)
	}

	// A machine notice is not somebody talking, so it does not make the room a
	// conversation either.
	if _, err := graph.PostMessage(store.Message{
		SessionID: "room", Role: store.RoleSystem, Body: "Taking over as resident.",
	}); err != nil {
		t.Fatal(err)
	}
	conversationalHead.nameRoom(context.Background(), "room")
	if scribe.count() != 0 {
		t.Fatalf("a system notice was taken for a reply (%d calls)", scribe.count())
	}

	// The reply lands, and now there is a room to name.
	if _, err := graph.PostMessage(store.Message{
		SessionID: "room", Role: store.RoleAgent, Body: "On it — reading the billing package now.",
	}); err != nil {
		t.Fatal(err)
	}
	conversationalHead.nameRoom(context.Background(), "room")
	if title := roomTitleOf(t, graph, "room"); title != "Billing code audit" {
		t.Fatalf("the room is called %q once it had an exchange", title)
	}
}

// Whatever the model says goes through the one normalizer, so a clerk that
// answers with quotes, a label, a full stop, a paragraph or a sentence all land
// on the same kind of row.
func TestTheScribeTitleGoesThroughTheSanitizer(t *testing.T) {
	for name, pair := range map[string][2]string{
		"quoted":        {`"Billing code audit"`, "Billing code audit"},
		"trailing stop": {"Billing code audit.", "Billing code audit"},
		"a label":       {"Title: Billing code audit", "Billing code audit"},
		"a paragraph":   {"Billing code audit\n\nBecause they asked about billing.", "Billing code audit"},
		"a whole sentence": {"They want an audit of last quarter's billing code and tests",
			"They want an audit of"},
		"nothing at all": {"   ", ""},
	} {
		if got := roomTitle(pair[0]); got != pair[1] {
			t.Errorf("%s: %q sanitized to %q, want %q", name, pair[0], got, pair[1])
		}
	}

	// And the sanitized answer is what reaches the row.
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "audit the billing code", "On it.")
	scribe := &scribeClientRecorder{answers: []string{"\"Billing code audit.\""}}
	New(scribe, graph).nameRoom(context.Background(), "room")
	if title := roomTitleOf(t, graph, "room"); title != "Billing code audit" {
		t.Fatalf("the row reads %q, want the sanitized name", title)
	}
}

// thoughtOnly is what a mandatory-reasoning model hands back when the whole
// ceiling went on thinking: a real, successful, paid-for response with a
// length-shaped finish and not one visible character in it. This is the exact
// shape measured against the owner's own endpoint, and it is the shape that
// made every room in the rail untitled.
func thoughtOnly(finish string) *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant"},
		FinishReason: finish,
	}}}
}

// THE ESCALATION. A ceiling spent entirely on reasoning is the one failure a
// bigger ceiling can fix, so the clerk asks once more with room to spare — and
// the room gets its name from the second answer.
func TestTheScribeRetriesOnceWhenTheCeilingWentOnThinking(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "can you audit last quarter's billing code", "On it.")

	scribe := &scribeClientRecorder{
		raw:     []*ai.Response{thoughtOnly("length")},
		answers: []string{"Billing code audit"},
	}
	New(scribe, graph).nameRoom(context.Background(), "room")

	if title := roomTitleOf(t, graph, "room"); title != "Billing code audit" {
		t.Fatalf("the room is called %q after the escalation", title)
	}
	if scribe.count() != 2 {
		t.Fatalf("naming cost %d calls, want the try and one escalation", scribe.count())
	}
	if len(scribe.ceilings) != 2 {
		t.Fatalf("the calls carried %d ceilings, want two", len(scribe.ceilings))
	}
	if scribe.ceilings[0] != scribeMaxTokens {
		t.Fatalf("the first call was capped at %d, want %d", scribe.ceilings[0], scribeMaxTokens)
	}
	// SUBSTANTIALLY larger, and pinned as a ratio rather than as a number: the
	// escalation is only a mechanism if the second ceiling is big enough to
	// hold a reasoning budget the first one could not.
	if scribe.ceilings[1] != scribeReliefTokens || scribe.ceilings[1] < 4*scribe.ceilings[0] {
		t.Fatalf("the escalation was capped at %d, want %d and several times the first",
			scribe.ceilings[1], scribeReliefTokens)
	}
}

// A provider that reports no finish reason at all has told us nothing, and an
// empty answer on a successful call is then the only signal there is.
func TestTheScribeEscalatesWhenNoFinishReasonIsReported(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "audit the billing code", "On it.")

	scribe := &scribeClientRecorder{
		raw:     []*ai.Response{thoughtOnly("")},
		answers: []string{"Billing code audit"},
	}
	New(scribe, graph).nameRoom(context.Background(), "room")

	if title := roomTitleOf(t, graph, "room"); title != "Billing code audit" {
		t.Fatalf("the room is called %q", title)
	}
	if scribe.count() != 2 {
		t.Fatalf("naming cost %d calls, want the try and one escalation", scribe.count())
	}
}

// ONCE, AND THEN SILENCE. A model that thinks its way through the escalated
// ceiling too leaves the room exactly as it was, and does not buy a third call.
// The room is untitled, which is the true answer, and the next turn is another
// chance at the same two calls — never a loop inside one pass.
func TestAScribeThatOnlyEverThinksNamesNothing(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "audit the billing code", "On it.")

	scribe := &scribeClientRecorder{raw: []*ai.Response{thoughtOnly("length"), thoughtOnly("length")}}
	New(scribe, graph).nameRoom(context.Background(), "room")

	if title := roomTitleOf(t, graph, "room"); title != "" {
		t.Fatalf("a reply with nothing in it became the title %q", title)
	}
	if scribe.count() != 2 {
		t.Fatalf("the scribe made %d calls, want one escalation and no more", scribe.count())
	}
}

// A model that CHOSE to say nothing will choose it again with sixteen times the
// room. That is a different failure from running out of budget, the finish
// reason says which, and the clerk pays for one call rather than two.
func TestAnEmptyAnswerThatFinishedProperlyIsNotEscalated(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "audit the billing code", "On it.")

	scribe := &scribeClientRecorder{raw: []*ai.Response{thoughtOnly("stop")}}
	New(scribe, graph).nameRoom(context.Background(), "room")

	if title := roomTitleOf(t, graph, "room"); title != "" {
		t.Fatalf("an empty answer became the title %q", title)
	}
	if scribe.count() != 1 {
		t.Fatalf("the scribe made %d calls, want the one it was owed", scribe.count())
	}
}

// An empty answer is not a name. The room keeps saying "untitled", which is the
// true answer, and the next turn is another chance.
func TestAnUnusableAnswerLeavesTheRoomUntitled(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "audit the billing code", "On it.")
	scribe := &scribeClientRecorder{answers: []string{"  ...  "}}
	conversationalHead := New(scribe, graph)
	conversationalHead.nameRoom(context.Background(), "room")
	if title := roomTitleOf(t, graph, "room"); title != "" {
		t.Fatalf("an unusable answer became the title %q", title)
	}
	scribe.answers = []string{"Billing code audit"}
	conversationalHead.nameRoom(context.Background(), "room")
	if title := roomTitleOf(t, graph, "room"); title != "Billing code audit" {
		t.Fatalf("the second attempt produced %q", title)
	}
}

// The clerk answers with a name and the subjects under it, and the parse is
// defensive IN ONE DIRECTION: everything that can go wrong with the second line
// costs the room its tags and never its title.
func TestTheScribeReadsATitleAndItsTags(t *testing.T) {
	for name, want := range map[string]struct {
		raw   string
		title string
		tags  []string
	}{
		"both lines": {"Billing code audit\nbilling, invoices, proration",
			"Billing code audit", []string{"billing", "invoices", "proration"}},
		"title only":         {"Billing code audit", "Billing code audit", nil},
		"an empty tags line": {"Billing code audit\n   \n", "Billing code audit", nil},
		"a labelled tags line": {"Billing code audit\nTags: billing, invoices",
			"Billing code audit", []string{"billing", "invoices"}},
		"a bulleted tags line": {"Billing code audit\n- billing, - invoices",
			"Billing code audit", []string{"billing", "invoices"}},
		// Prose is not a tag: an entry of several words matches everything a
		// subsequence filter is asked, so it is dropped rather than clipped.
		"a sentence where the tags were": {
			"Billing code audit\nThis conversation is about the billing package and its tests",
			"Billing code audit", nil},
		"prose with commas in it": {
			"Billing code audit\nthey asked about billing, and I said I would look at it",
			"Billing code audit", nil},
		// The title survives whatever the second line is, which is the whole
		// point: a room without tags is findable, a room without a name is not.
		"garbage after the name": {"Billing code audit\n{\"tags\": [\"billing\"]}",
			"Billing code audit", nil},
		"a paragraph": {"Billing code audit\n\nBecause they asked about billing.",
			"Billing code audit", nil},
		"nothing at all": {"   ", "", nil},
	} {
		title, tags := roomLabel(want.raw)
		if title != want.title {
			t.Errorf("%s: %q named the room %q, want %q", name, want.raw, title, want.title)
		}
		if !slices.Equal(tags, want.tags) {
			t.Errorf("%s: %q filed the room under %v, want %v", name, want.raw, tags, want.tags)
		}
	}
}

// The tags reach the row, through the same store door the title does, and a
// person who renames the room afterwards restates its NAME and nothing about
// what it is about.
func TestTheScribeFilesTheRoomAndAManualRenameLeavesTheFilingAlone(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "audit the billing code", "On it.")

	scribe := &scribeClientRecorder{answers: []string{"Billing code audit\nbilling, proration"}}
	New(scribe, graph).nameRoom(context.Background(), "room")

	session, found, err := graph.Session("room")
	if err != nil || !found {
		t.Fatalf("read room: %v (found %v)", err, found)
	}
	if session.Title != "Billing code audit" {
		t.Fatalf("the room is called %q", session.Title)
	}
	if want := []string{"billing", "proration"}; !slices.Equal(session.Tags, want) {
		t.Fatalf("the room is filed under %v, want %v", session.Tags, want)
	}

	if _, err := graph.RenameSession("room", "quarterly numbers"); err != nil {
		t.Fatal(err)
	}
	renamed, _, err := graph.Session("room")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Title != "quarterly numbers" {
		t.Fatalf("the hand-typed name became %q", renamed.Title)
	}
	if want := []string{"billing", "proration"}; !slices.Equal(renamed.Tags, want) {
		t.Fatalf("a rename rewrote the filing to %v, want %v left alone", renamed.Tags, want)
	}
}

// The brief asks for the second line, and the clerk that never sees the ask
// cannot answer it.
func TestTheScribeBriefAsksForTheSubjectsToo(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "audit the billing code", "On it.")
	scribe := &scribeClientRecorder{answers: []string{"Billing code audit\nbilling"}}
	New(scribe, graph).nameRoom(context.Background(), "room")

	if len(scribe.prompts) == 0 {
		t.Fatal("the scribe made no call")
	}
	for _, required := range []string{"two lines", "up to 3 topic words", "lowercase"} {
		if !strings.Contains(scribe.prompts[0], required) {
			t.Fatalf("the naming brief did not carry %q:\n%s", required, scribe.prompts[0])
		}
	}
}

// The whole point of the post-turn lane: the reply is journaled first and the
// name arrives behind it. This runs the real loop — the belt answers the turn,
// the scribe names the room — and the assertion is that the room ends up named
// without the answer ever having waited on it.
func TestAFinishedTurnNamesTheRoomBehindTheReply(t *testing.T) {
	graph := openHeadStore(t)
	client := &beltClient{
		turns: []beltTurn{{text: "Yes — the billing package looks clean apart from the proration."}},
		plain: []string{"Billing code audit"},
	}
	conversationalHead := New(client, graph).WithRoomNaming(true)
	stop := startServing(t, conversationalHead)
	defer stop()

	user, err := graph.PostMessage(store.Message{
		SessionID: "room", Role: store.RoleUser, Body: "have a look at the billing code",
	})
	if err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "room", user.Seq)
	if !strings.Contains(reply.Body, "billing package") {
		t.Fatalf("the reply is %q", reply.Body)
	}
	title := waitForRoomTitle(t, graph, "room")
	if title != "Billing code audit" {
		t.Fatalf("the room is called %q after its first exchange", title)
	}
	if _, tooled := client.counts(); tooled == 0 {
		t.Fatal("the turn never reached the belt")
	}
}

func waitForRoomTitle(t *testing.T, graph *store.Store, sessionID string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		session, found, err := graph.Session(sessionID)
		if err != nil {
			t.Fatalf("read room: %v", err)
		}
		if found && strings.TrimSpace(session.Title) != "" {
			return session.Title
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to be named", sessionID)
	return ""
}

// watchfulClient answers every call and remembers whether the surface's
// typewriter was pointed at it. The turn's calls are streamed — that is how a
// reply appears a word at a time — and the naming call must not be, or the
// room's label would be typed into the room as though somebody were saying it.
type watchfulClient struct {
	mutex   sync.Mutex
	replies []string
	streams []bool
}

func (client *watchfulClient) CompleteWithMessages(ctx context.Context, _ []ai.Message,
	_ ...ai.Option) (*ai.Response, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.streams = append(client.streams, provider.Streaming(ctx))
	if len(client.replies) == 0 {
		return textResponse("nothing scripted"), nil
	}
	reply := client.replies[0]
	client.replies = client.replies[1:]
	return textResponse(reply), nil
}

func (client *watchfulClient) observed() []bool {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return append([]bool(nil), client.streams...)
}

// The label is not a reply and may not look like one.
func TestTheNamingCallIsNotStreamedIntoTheRoom(t *testing.T) {
	graph := openHeadStore(t)
	client := &watchfulClient{replies: []string{"The billing package looks clean.", "Billing code audit"}}
	conversationalHead := New(client, graph).WithRoomNaming(true)

	ctx, cancel := context.WithCancel(provider.WithStreamObserver(
		context.Background(), func(provider.StreamEvent) {}))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- conversationalHead.Serve(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	if _, err := graph.PostMessage(store.Message{
		SessionID: "room", Role: store.RoleUser, Body: "have a look at the billing code",
	}); err != nil {
		t.Fatal(err)
	}
	if title := waitForRoomTitle(t, graph, "room"); title != "Billing code audit" {
		t.Fatalf("the room is called %q", title)
	}
	observed := client.observed()
	if len(observed) < 2 {
		t.Fatalf("the head made %d calls, want the turn and the naming call", len(observed))
	}
	if !observed[0] {
		t.Fatal("the turn's own call was not streamed — the reply would arrive whole")
	}
	if observed[len(observed)-1] {
		t.Fatal("the naming call was streamed into the room")
	}
}

// The clerk is a declared capability, not a thing every head does. A window
// that never asked for names pays for none, and its turns cost exactly what a
// turn costs.
func TestAHeadThatWasNotAskedToNameRoomsNamesNone(t *testing.T) {
	graph := openHeadStore(t)
	client := &watchfulClient{replies: []string{"The billing package looks clean.", "Billing code audit"}}
	conversationalHead := New(client, graph)
	stop := startServing(t, conversationalHead)

	user, err := graph.PostMessage(store.Message{
		SessionID: "room", Role: store.RoleUser, Body: "have a look at the billing code",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForAgentReply(t, graph, "room", user.Seq)
	stop()

	if calls := len(client.observed()); calls != 1 {
		t.Fatalf("the turn cost %d calls, want the one the turn asked for", calls)
	}
	if title := roomTitleOf(t, graph, "room"); title != "" {
		t.Fatalf("a head with no naming clerk called the room %q", title)
	}
}
