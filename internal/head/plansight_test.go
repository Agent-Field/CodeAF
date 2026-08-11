package head

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The head has never read a graph edge, and every shallow answer it has ever
// given about live work followed from that. These are the four halves of the
// fix: the board carries structure, the belt can open a plan, the belt can
// remember the sentence before this one, and a status question opens the belt
// at all.

// needing wires a spec to the work it waits for, which is what puts an edge in
// the store at splice time.
func needing(node store.NodeSpec, ids ...string) store.NodeSpec {
	for _, id := range ids {
		node.Needs = append(node.Needs, store.Need{NodeID: id, Kind: store.FeedsInto})
	}
	return node
}

// seedLedgerAudit is one job with a real dependency in it: a read, a write-up
// that waits for the read, and the read already under way.
func seedLedgerAudit(t *testing.T, graph *store.Store) {
	t.Helper()
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("audit", "", "Ledger audit", "audit the ledger"),
		spec("audit-n1", "audit", "Read the ledger", "read the ledger"),
		needing(spec("audit-n2", "audit", "Write the report", "write the report"), "audit-n1"))
	startNode(t, graph, "audit-n1")
}

func TestTheBoardSaysWhatWaitsOnWhatAndHowLongItHasRun(t *testing.T) {
	graph := openHeadStore(t)
	seedLedgerAudit(t, graph)
	board := New(nil, graph).boardFor("surgery", "", nil, time.Now().Add(20*time.Minute))

	rows := make(map[string]string)
	for _, line := range strings.Split(board, "\n") {
		if fields := strings.SplitN(strings.TrimPrefix(line, "- "), " | ", 2); len(fields) == 2 {
			rows[fields[0]] = line
		}
	}
	if !strings.Contains(rows["audit-n2"], "waits on Read the ledger") {
		t.Fatalf("the queued step never said what it is queued behind:\n%s", board)
	}
	if strings.Contains(rows["audit-n1"], "waits on") {
		t.Fatalf("the step nothing waits for was given a wait:\n%s", board)
	}
	if !strings.Contains(rows["audit-n1"], "running 20m") {
		t.Fatalf("the running step never said how long it has been going:\n%s", board)
	}
	// The job root does none of the work, so its elapsed is its longest-running
	// part — which is exactly what "how long has that been going?" means.
	if !strings.Contains(rows["audit"], "running 20m") {
		t.Fatalf("the job never rolled up how long its parts have been running:\n%s", board)
	}

	// A landed upstream is provenance, not a wait: a job whose input arrived is
	// moving, and saying it waits on that input reads as stuck.
	landed := openHeadStore(t)
	spliceJobTree(t, landed, store.OriginUser, "",
		spec("audit", "", "Ledger audit", "audit the ledger"),
		spec("audit-n1", "audit", "Read the ledger", "read the ledger"),
		needing(spec("audit-n2", "audit", "Write the report", "write the report"), "audit-n1"))
	completeNodeWith(t, landed, "audit-n1", "The ledger balances.")
	after := New(nil, landed).boardFor("surgery", "", nil, time.Now())
	if strings.Contains(after, "waits on Read the ledger") {
		t.Fatalf("a settled upstream still reads as something to wait for:\n%s", after)
	}
	if strings.Contains(after, "running ") {
		t.Fatalf("work that stopped is still being clocked:\n%s", after)
	}
}

// seedPlannedMarket is a job with a journaled plan: three steps, the write-up
// waiting on the other two, and the store rows the plan was spliced as.
func seedPlannedMarket(t *testing.T, graph *store.Store) {
	t.Helper()
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("market", "", "Northern market", "work out whether the northern market is worth entering"),
		spec("market-n1", "market", "Read the filings", "read the filings"),
		spec("market-n2", "market", "Interview the vendors", "interview the vendors"),
		needing(spec("market-n3", "market", "Write it up", "write it up"), "market-n1", "market-n2"))
	completeNodeWith(t, graph, "market-n1", "Six filings read.")
	startNode(t, graph, "market-n2")

	document := &plan.Graph{
		Goal: "work out whether the northern market is worth entering",
		Nodes: []plan.Node{
			{ID: 1, Title: "Read the filings", Summary: "read the filings",
				State: plan.StatePending, Kind: plan.KindWork, Turns: 3},
			{ID: 2, Title: "Interview the vendors", Summary: "interview the vendors",
				State: plan.StatePending, Kind: plan.KindWork},
			{ID: 3, Title: "Write it up", Summary: "write it up", Needs: []int{1, 2},
				State: plan.StatePending, Kind: plan.KindSynthesis,
				Contract: "one page, every claim cited"},
		},
	}
	encoded, err := document.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordPlanGraph("market", store.PlanGraph{Root: "market", Graph: encoded}); err != nil {
		t.Fatalf("journal the plan: %v", err)
	}
}

func TestAStatusQuestionOpensTheBeltAndThePlanReadAnswersIt(t *testing.T) {
	graph := openHeadStore(t)
	seedPlannedMarket(t, graph)

	session := "plan-question"
	user := postUser(t, graph, session, "hows it going what is the plan dag we have?")
	// The reading survives as evidence: a status question is marked as one in the
	// prompt, above the message, so a model that reads it knows plan is the tool
	// that answers "what's the plan" rather than a pair of counts.
	if prompt, err := New(&beltClient{}, graph).turnPrompt(user); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(prompt, "the shape of it, not just its temperature") {
		t.Fatalf("the status reading never reached the loop:\n%s", prompt)
	}

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolBoard, map[string]any{})}},
		{calls: []ai.ToolCall{beltCall("c2", beltToolPlan, map[string]any{"job": "market"})}},
		{text: "The filings are read, the vendor interviews are under way, and the write-up is waiting on both."},
	}}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if !strings.Contains(reply.Body, "write-up is waiting") {
		t.Fatalf("the belt never answered the question: %q", reply.Body)
	}

	read := ""
	for _, message := range client.seen {
		if message.Role == "tool" && len(message.Content) > 0 &&
			strings.HasPrefix(message.Content[0].Text, "plan for ") {
			read = message.Content[0].Text
		}
	}
	if read == "" {
		t.Fatalf("the plan read never reached the model: %+v", client.seen)
	}
	if !strings.Contains(read, "goal: work out whether the northern market is worth entering") {
		t.Fatalf("the plan read carried no goal:\n%s", read)
	}
	// Dependency order, in plain words, with the states read off the durable
	// rows rather than off a document journaled before any of it happened.
	if !strings.Contains(read, "- Read the filings | done") {
		t.Fatalf("a landed step reads as unstarted:\n%s", read)
	}
	if !strings.Contains(read, "- Interview the vendors | working") {
		t.Fatalf("a running step reads as unstarted:\n%s", read)
	}
	if !strings.Contains(read, "Write it up | waiting | after Read the filings, Interview the vendors") {
		t.Fatalf("the plan never said what the last step waits for:\n%s", read)
	}
	if !strings.Contains(read, "3 turns") || !strings.Contains(read, "method: one page, every claim cited") {
		t.Fatalf("the plan dropped the turns and the method it journaled:\n%s", read)
	}
	if !strings.Contains(read, "id market-n3") {
		t.Fatalf("the plan named no id, so nothing read here can be acted on:\n%s", read)
	}
	if strings.Index(read, "Read the filings") > strings.Index(read, "Write it up") {
		t.Fatalf("the steps are not in an order that could run:\n%s", read)
	}
	if len(read) > beltResultBytes {
		t.Fatalf("plan read = %d bytes, over its %d budget", len(read), beltResultBytes)
	}
}

// Two of the trigger's holes were one word wide each: an apostrophe nobody
// types, and a cue word that is also the name of the thing being asked about.
func TestStatusPhrasingSurvivesTheTrigger(t *testing.T) {
	if !selfQuestionCued("hows your day going") {
		t.Fatal(`"hows" without the apostrophe is not read as a question`)
	}
	if reference := redirectReference("what is the plan for the market research"); !strings.Contains(reference, "plan") {
		t.Fatalf("the subject of the question was stripped as cue vocabulary: %q", reference)
	}
	if !controlStatusCued("how far along is any of this") {
		t.Fatal("a question about progress carries no cue the trigger can see")
	}
	if !controlStatusCued("hows it going") {
		t.Fatal("the most ordinary status question in the language is not one")
	}
	if controlStatusCued("write me a poem about the sea") {
		t.Fatal("ordinary work opens the belt on a status cue that is not there")
	}
	// The weak half is a status word only inside a question. A statement that
	// happens to use one is not a question about the board.
	if controlStatusCued("im going out for an hour") {
		t.Fatal("a sentence about the user's own evening opened the belt")
	}
}

// With nothing live these words are ordinary conversation. The reading is still
// computed — it costs a map lookup — but it says nothing about work, because
// there is no work for it to be about, and the board says so in one line.
func TestAStatusQuestionOverAnEmptyBoardHasNothingToBeAbout(t *testing.T) {
	graph := openHeadStore(t)
	user := postUser(t, graph, "quiet", "hows it going what is the plan dag we have?")
	prompt, err := New(nil, graph).turnPrompt(user)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, emptyBoardLine) {
		t.Fatalf("an empty board did not say so:\n%s", prompt)
	}
	if strings.Contains(prompt, "the words rank against") {
		t.Fatalf("a quiet graph still produced a lexical reading:\n%s", prompt)
	}
}

// A job with no journaled plan is a job that was taken on whole. Saying so is
// an answer; erroring is a dead end the model would spend a call recovering
// from.
func TestAJobWithNoPlanSaysSoRatherThanFailing(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "errand", "One errand", "run the errand")
	node, found, err := graph.Node("errand")
	if err != nil || !found {
		t.Fatalf("fixture: found=%t err=%v", found, err)
	}
	rendered, failed := New(nil, graph).renderPlan(node)
	if failed {
		t.Fatalf("an unplanned job came back as a tool error: %q", rendered)
	}
	if !strings.Contains(rendered, "single step") {
		t.Fatalf("an unplanned job did not say it was taken on whole: %q", rendered)
	}
}

// The parts of a job are the parts, however deep the plan put them. A subtree
// two layers down read as a list of containers that concluded nothing.
func TestResultPartsReachThroughTheWholeSubtree(t *testing.T) {
	graph := openHeadStore(t)
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("survey", "", "Site survey", "survey the sites"),
		spec("survey-n1", "survey", "Northern sites", "the northern sites"),
		spec("survey-n2", "survey-n1", "Measure the north yard", "measure the north yard"))
	completeNodeWith(t, graph, "survey-n2", "The north yard is 40 metres.")
	parts := New(nil, graph).resultChildren("survey")
	joined := strings.Join(parts, "\n")
	if !strings.Contains(joined, "The north yard is 40 metres.") {
		t.Fatalf("the part that concluded something was two layers down and invisible:\n%s", joined)
	}
	if !strings.Contains(joined, "  - survey-n2") {
		t.Fatalf("the grandchild carried no depth marker, so the shape is a lie:\n%s", joined)
	}
}

func TestTheBeltPromptCarriesTheConversation(t *testing.T) {
	graph := openHeadStore(t)
	seedPlannedMarket(t, graph)
	session := "belt-memory"
	postUser(t, graph, session, "start the northern market work please")
	if _, err := graph.PostMessage(store.Message{SessionID: session, Role: store.RoleAgent,
		Body: "Started it — the filings first, then the vendor interviews."}); err != nil {
		t.Fatal(err)
	}

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolBoard, map[string]any{})}},
		{text: "The vendor interviews are the slow part."},
	}}
	user := postUser(t, graph, session, "which part of that is slowest?")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	waitForAgentReply(t, graph, session, user.Seq)

	opening := client.openingPrompt()
	if !strings.Contains(opening, "Recent thread before this message:") {
		t.Fatalf("the belt was given no conversation at all:\n%s", opening)
	}
	// The referent "that" lives in the reply before it, and nothing else in the
	// prompt says what it points at.
	if !strings.Contains(opening, "the filings first, then the vendor interviews") {
		t.Fatalf("the belt could not see the reply the follow-up answers:\n%s", opening)
	}
	if !strings.Contains(opening, "start the northern market work please") {
		t.Fatalf("the belt could not see what the user asked for:\n%s", opening)
	}
	// The board is still the floor, and it is still first.
	if board := strings.Index(opening, "Board (the user's live work):"); board != 0 {
		t.Fatalf("the conversation displaced the board from the top of the prompt: board at %d", board)
	}
	if strings.Index(opening, "\n\nRecent thread before this message:") <
		strings.Index(opening, "\n\nNotebook (durable memory") {
		t.Fatalf("the thread crowded the notebook down the prompt:\n%s", opening)
	}
}
