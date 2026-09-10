package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The episodic half of memory is tested the way the memory tests are: against
// what a person could observe. Which verb the model was handed, what one search
// actually answers with, and what it says when there is nothing there.

// beltHas reports whether a name is on an agent's belt.
func beltHas(agent *Agent, name string) bool {
	for _, tool := range agent.belt() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// searchConversations calls the tool the way the wire does.
func searchConversations(t *testing.T, agent *Agent, args string) string {
	t.Helper()
	out, failed, err := agent.searchConversationsTool(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("search_conversations: %v", err)
	}
	if failed {
		t.Fatalf("search_conversations refused %s: %s", args, out)
	}
	return out
}

// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. The index this tool reads
// is in the store, so a session opened with memory off must not be told it can
// search anything: a model handed the verb plans a whole answer around it, and
// one that fails every time it is called is worse than one that was never there.
func TestSearchingConversationsIsOnTheBeltOnlyWithAStoreBehindIt(t *testing.T) {
	with, _ := brainAgent(t, &scriptedCompleter{}, nil)
	if !beltHas(with, "search_conversations") {
		t.Error("a session with memory on was not given search_conversations")
	}
	without, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if beltHas(without, "search_conversations") {
		t.Error("a session with memory off was handed a verb with no index behind it")
	}
	// The other belt a person never sees is a task node's, and it is handed no
	// store at all — the same wall that stops a node writing memories, read
	// from the other side.
	node, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.InTask = true })
	if beltHas(node, "search_conversations") {
		t.Error("a task node was given a verb over a store it does not have")
	}
}

// One search, and everything a hit is supposed to carry: how long ago, which
// conversation, who said it, the words themselves, and the transcript that can
// be read for the rest.
func TestSearchingConversationsAnswersWithTheWordsTheirAgeAndTheirTranscript(t *testing.T) {
	root := t.TempDir()
	here := filepath.Join(root, "1111111111111111")
	there := filepath.Join(root, "2222222222222222")
	for _, dir := range []string{here, there} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	// The other conversation's journal has to be ON DISK for its URI to be
	// printed: a pointer to a file that is not there is the one thing this
	// must never write.
	transcript := filepath.Join(there, placeTranscript)
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	agent, brain := brainAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = Place{Dir: here, Workspace: config.Workspace}
	})
	if _, err := brain.OpenSession("2222222222222222", "the pricing thread", "chat"); err != nil {
		t.Fatalf("open session: %v", err)
	}
	post(t, brain, "2222222222222222", store.RoleUser, "we decided the retry limit stays at three")
	post(t, brain, "2222222222222222", store.RoleAgent, "noted — three retries and no backoff change")

	out := searchConversations(t, agent, `{"query":"retry limit"}`)
	if !strings.Contains(out, "we decided the retry limit stays at three") {
		t.Fatalf("the words that were said are not in the answer:\n%s", out)
	}
	if !strings.Contains(out, "'the pricing thread'") {
		t.Fatalf("the conversation is not named:\n%s", out)
	}
	if !strings.Contains(out, "them: ") {
		t.Fatalf("nobody is said to have spoken:\n%s", out)
	}
	if !strings.Contains(out, "just now") && !strings.Contains(out, "ago") {
		t.Fatalf("the hit carries no age:\n%s", out)
	}
	if !strings.Contains(out, "transcript file://"+transcript) {
		t.Fatalf("the transcript to read is not named:\n%s", out)
	}
}

// A hit whose conversation has no journal on this disk is still a hit, and it
// simply carries no transcript clause — the emptiness law, which forbids a row
// that reads `transcript ` with nothing after it as much as it forbids `$0.00`.
func TestAConversationWithNoJournalOnDiskNamesNoTranscript(t *testing.T) {
	root := t.TempDir()
	here := filepath.Join(root, "1111111111111111")
	if err := os.MkdirAll(here, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	agent, brain := brainAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = Place{Dir: here, Workspace: config.Workspace}
	})
	post(t, brain, "3333333333333333", store.RoleUser, "the flag is called quiet-mode")

	out := searchConversations(t, agent, `{"query":"quiet-mode"}`)
	if !strings.Contains(out, "the flag is called quiet-mode") {
		t.Fatalf("the words are missing:\n%s", out)
	}
	if strings.Contains(out, "transcript file://") {
		t.Fatalf("a transcript was named for a conversation with no journal:\n%s", out)
	}
	// The id stands in for a name nobody gave the conversation, so the model
	// still has something to point at.
	if !strings.Contains(out, "3333333333333333") {
		t.Fatalf("an unnamed conversation is not identified at all:\n%s", out)
	}
}

// The limit is the ANSWER's bound and it is clamped at both ends: nothing asked
// for is honoured above the ceiling, and nothing asked for below one falls back
// to the default rather than returning an empty answer.
func TestSearchingConversationsClampsWhatItWillAnswerWith(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	for index := 0; index < conversationLimitMax+12; index++ {
		post(t, brain, "4444444444444444", store.RoleUser, "the deploy runs on friday")
	}
	lines := func(out string) int {
		count := 0
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if strings.Contains(line, "the deploy runs on friday") && strings.HasPrefix(line, "  message ") {
				count++
			}
		}
		return count
	}
	if got := lines(searchConversations(t, agent, `{"query":"deploy","limit":99}`)); got != conversationLimitMax {
		t.Errorf("asked for 99 excerpts and got %d, want the ceiling of %d", got, conversationLimitMax)
	}
	if got := lines(searchConversations(t, agent, `{"query":"deploy"}`)); got != conversationLimitDefault {
		t.Errorf("asked for no number and got %d excerpts, want the default of %d", got, conversationLimitDefault)
	}
	if got := lines(searchConversations(t, agent, `{"query":"deploy","limit":-4}`)); got != conversationLimitDefault {
		t.Errorf("asked for -4 excerpts and got %d, want the default of %d", got, conversationLimitDefault)
	}
}

// A query nobody can parse is a MISS AND IT IS SAID. The store already answers
// hostile FTS syntax with nothing rather than an error (thread_search.go); what
// is pinned here is that the tool reports it as an honest empty answer instead
// of a failure the model will try to work around.
func TestAQueryTheIndexCannotParseIsAnHonestMiss(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	post(t, brain, "5555555555555555", store.RoleUser, "the deploy runs on friday")

	for _, query := range []string{`"unclosed`, `NEAR(`, `*`, `nothing was ever said about this`} {
		out, failed, err := agent.searchConversationsTool(context.Background(),
			json.RawMessage(`{"query":`+quoteJSON(query)+`}`))
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		if failed {
			t.Errorf("search %q was reported as a failure: %s", query, out)
		}
		if !strings.Contains(out, "Nothing said in any earlier conversation matches") {
			t.Errorf("search %q did not say plainly that it found nothing: %s", query, out)
		}
	}
}

// An empty query is the one thing the tool refuses, because a search with no
// words in it is a call the model can make again correctly.
func TestSearchingConversationsWithNoWordsIsRefused(t *testing.T) {
	agent, _ := brainAgent(t, &scriptedCompleter{}, nil)
	out, failed, err := agent.searchConversationsTool(context.Background(), json.RawMessage(`{"query":"  "}`))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !failed || !strings.Contains(out, "query is required") {
		t.Errorf("an empty query was not refused: failed=%v %s", failed, out)
	}
}

func post(t *testing.T, brain *store.Store, session string, role store.Role, body string) {
	t.Helper()
	if _, err := brain.PostMessage(store.Message{SessionID: session, Role: role, Body: body}); err != nil {
		t.Fatalf("post message: %v", err)
	}
}

// A correction can share no query words with the decision it replaces. One
// lookup must retain that exchange and a second lookup must work across places
// even when the old conversation has no sibling transcript file.
func TestConversationSearchFindsThePassageAndOpensItsCorrectionByID(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	post(t, brain, "elsewhere", store.RoleAgent, strings.Repeat("background detail ", 100)+"The amber launch code is CEDAR-81.")
	post(t, brain, "unrelated", store.RoleUser, "private unrelated neighbour must not leak")
	post(t, brain, "elsewhere", store.RoleUser, "Correction: use MAPLE-92 instead.")
	out := searchConversations(t, agent, `{"query":"amber","limit":1}`)
	for _, want := range []string{"CEDAR-81", "MAPLE-92", "Conversation elsewhere", "message "} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "private unrelated") {
		t.Fatal("context crossed conversations")
	}
	hits, err := brain.FindConversationMessages(context.Background(), "amber", "elsewhere", "", 1)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits: %v %v", hits, err)
	}
	opened := searchConversations(t, agent, fmt.Sprintf(`{"session_id":"elsewhere","message_id":%d}`, hits[0].Seq))
	if !strings.Contains(opened, "MAPLE-92") {
		t.Fatalf("cannot open correction: %s", opened)
	}
	scoped := searchConversations(t, agent, `{"query":"amber","session_id":"unrelated"}`)
	if strings.Contains(scoped, "CEDAR-81") {
		t.Fatalf("scope ignored: %s", scoped)
	}
}

func TestConversationSearchCanBrowseAConversationAndRefusesAmbiguousIDs(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	post(t, brain, "room", store.RoleUser, "our current decision")
	if got := searchConversations(t, agent, `{"session_id":"room"}`); !strings.Contains(got, "our current decision") {
		t.Fatal(got)
	}
	for _, args := range []string{`{"message_id":1}`, `{"session_id":"room","message_id":-1}`, `{"session_id":"room","message_id":1,"query":"decision"}`} {
		_, failed, err := agent.searchConversationsTool(context.Background(), json.RawMessage(args))
		if !failed || err != nil {
			t.Fatalf("ambiguous input accepted: %s", args)
		}
	}
}

// The real worker constructor must carry search authority through every depth,
// without turning its inherited reader into writable long-term memory.
func TestTaskWorkersInheritConversationReadsWithoutMemoryWrites(t *testing.T) {
	parent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	post(t, brain, "other-project", store.RoleUser, "the launch receipt is SILVER-731")
	post(t, brain, parent.sessionID(), store.RoleUser, "Before handing off: the copper receipt is GOLD-842")
	owner := parent
	for depth := 0; depth < 3; depth++ {
		graph := owner.graph()
		graph.run = func(*TaskNode) {}
		id := graph.reserve()
		graph.admit(id, taskSpec{title: "look up earlier decision", brief: "find the launch receipt", acceptance: "quote the receipt"})
		worker, err := owner.newTaskAgent(context.Background(), t.TempDir(), graph.node(id), "")
		if err != nil {
			t.Fatal(err)
		}
		defer worker.Close()
		if !beltHas(worker, "search_conversations") || beltHas(worker, "remember") || worker.config.Memory != nil || worker.memory != nil {
			t.Fatalf("depth %d: worker must carry read-only history, with no memory writer", depth)
		}
		if got := searchConversations(t, worker, `{"query":"SILVER"}`); !strings.Contains(got, "SILVER-731") {
			t.Fatal(got)
		}
		if got := searchConversations(t, worker, `{"query":"GOLD"}`); !strings.Contains(got, "GOLD-842") {
			t.Fatalf("worker lost its parent history: %s", got)
		}
		owner = worker
	}
}

// Searching for another chat must not find the question that just asked for it.
// An explicit scope remains the way to inspect this conversation's older text.
func TestConversationSearchExcludesItsOwnQueryButAllowsExplicitScope(t *testing.T) {
	agent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	post(t, brain, agent.sessionID(), store.RoleUser, "find my sandbox browser conversation")
	post(t, brain, "elsewhere", store.RoleUser, "we chose the sandbox browser called cedar")
	out := searchConversations(t, agent, `{"query":"sandbox browser"}`)
	if strings.Contains(out, "find my sandbox") || !strings.Contains(out, "called cedar") {
		t.Fatal(out)
	}
	args, _ := json.Marshal(map[string]any{"query": "sandbox browser", "session_id": agent.sessionID()})
	if got := searchConversations(t, agent, string(args)); !strings.Contains(got, "find my sandbox") {
		t.Fatal(got)
	}
}

func TestTaskSearchMissNamesConversationSearchOnlyWhenItExists(t *testing.T) {
	with, _ := brainAgent(t, &scriptedCompleter{}, nil)
	without, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if got := with.taskSearchText("missing-specific-topic", 1, taskScopeProject); !strings.Contains(got, "search_conversations") {
		t.Fatal(got)
	}
	if got := without.taskSearchText("missing-specific-topic", 1, taskScopeProject); strings.Contains(got, "search_conversations") {
		t.Fatal(got)
	}
}

func TestForkedHandsInheritOnlyConversationReads(t *testing.T) {
	parent, brain := brainAgent(t, &scriptedCompleter{}, nil)
	post(t, brain, "elsewhere", store.RoleUser, "the shared receipt is FIR-555")
	seed, system := parent.forkSeed()
	hand, err := parent.newHandAgent(forkPart{Role: "read the sources", Scope: []string{}}, seed, system, &handLeash{limit: forkRounds})
	if err != nil {
		t.Fatal(err)
	}
	defer hand.Close()
	if !beltHas(hand, "search_conversations") || beltHas(hand, "remember") || hand.config.Memory != nil {
		t.Fatal("fork did not inherit only history reads")
	}
	if got := searchConversations(t, hand, `{"query":"FIR"}`); !strings.Contains(got, "FIR-555") {
		t.Fatal(got)
	}
}
