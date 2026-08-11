package head

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// foldClient records every prompt it is asked, so a test can say what the model
// was shown rather than only how many times it was called. hold runs before the
// answer and may block or write to the journal, which is how a message arriving
// mid-turn is staged.
//
// turns are scripted tool calls, served in order before the plain reply. Folding
// is about how many TURNS the head takes, and a turn is several provider calls
// now, so a fixture about work being commissioned has to be able to say so.
type foldClient struct {
	mutex   sync.Mutex
	prompts []string
	reply   string
	turns   []beltTurn
	hold    func(call int, ctx context.Context)
}

func (client *foldClient) CompleteWithMessages(ctx context.Context, messages []ai.Message,
	_ ...ai.Option) (*ai.Response, error) {
	client.mutex.Lock()
	client.prompts = append(client.prompts, promptText(messages))
	call := len(client.prompts)
	hold := client.hold
	var turn *beltTurn
	if len(client.turns) > 0 {
		turn = &client.turns[0]
		client.turns = client.turns[1:]
	}
	client.mutex.Unlock()
	if hold != nil {
		hold(call, ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if turn != nil {
		response := textResponse(turn.text)
		response.Choices[0].Message.ToolCalls = turn.calls
		return response, nil
	}
	return textResponse(client.reply), nil
}

func (client *foldClient) callCount() int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return len(client.prompts)
}

func (client *foldClient) prompt(index int) string {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if index < 0 || index >= len(client.prompts) {
		return ""
	}
	return client.prompts[index]
}

func promptText(messages []ai.Message) string {
	var rendered strings.Builder
	for _, message := range messages {
		for _, part := range message.Content {
			rendered.WriteString(part.Text)
		}
	}
	return rendered.String()
}

func postUserLine(t *testing.T, graphStore *store.Store, sessionID, body string) store.Message {
	t.Helper()
	message, err := graphStore.PostMessage(store.Message{
		SessionID: sessionID, Role: store.RoleUser, Body: body,
	})
	if err != nil {
		t.Fatalf("post %q: %v", body, err)
	}
	return message
}

// The complaint this whole wave exists for: a question and the correction typed
// straight after it were two turns, and the first one answered a question the
// person had already withdrawn. They are one turn now — one call, carrying both
// sentences, and one reply.
func TestTwoMessagesTypedInOneBreathAreOneTurn(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &foldClient{reply: "This quarter: 12,004."}
	first := postUserLine(t, graphStore, "one-breath", "how are the totals?")
	second := postUserLine(t, graphStore, "one-breath", "actually, this quarter only")

	conversationalHead := New(client, graphStore)
	cursors := newSessionCursors(0)
	if err := conversationalHead.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("provider calls = %d, want the two messages answered as one turn", calls)
	}
	prompt := client.prompt(0)
	if !strings.Contains(prompt, first.Body) || !strings.Contains(prompt, second.Body) {
		t.Fatalf("the turn did not carry both of the person's sentences:\n%s", prompt)
	}
	if strings.Index(prompt, first.Body) > strings.Index(prompt, second.Body) {
		t.Fatal("the folded turn reordered what the person said")
	}
	replies := agentReplies(t, graphStore, "one-breath")
	if len(replies) != 1 {
		t.Fatalf("thread has %d replies, want one answer to the folded turn", len(replies))
	}
	if replies[0].Answers != second.Seq {
		t.Fatalf("the reply answers %d, want the newest folded row %d", replies[0].Answers, second.Seq)
	}
	if cursor := cursors.answeredThrough("one-breath"); cursor < second.Seq {
		t.Fatalf("cursor = %d, want past every folded row (%d)", cursor, second.Seq)
	}

	// Restart: the head resumes past rows a fold already answered, and a poll
	// from the cursors it advanced re-answers nothing.
	if err := conversationalHead.poll(context.Background(), cursors); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("provider calls = %d after a second poll, want the folded rows left alone", calls)
	}
	restarted, err := conversationalHead.initialCursors()
	if err != nil {
		t.Fatal(err)
	}
	if resumed := restarted.answeredThrough("one-breath"); resumed < second.Seq {
		t.Fatalf("a restart resumes at %d, want past the folded rows (%d)", resumed, second.Seq)
	}
}

// Folding is about how many times the head speaks, never about how much work
// gets done. Two things said in one breath still run as two jobs — the fan-out
// reads them out of the folded words — and the person still gets one answer.
//
// One turn is several provider calls now rather than exactly one, so the count
// that says "one turn" is the number of times the turn was OPENED: the fold
// builds one prompt from both rows and every call after it carries that same
// prompt plus tool traffic.
func TestAFoldedTurnStillFansOutIntoSeparateWork(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &foldClient{
		reply: "On both — I'll report back as each lands.",
		turns: []beltTurn{{calls: []ai.ToolCall{beltCall("s1", beltToolSpawn, map[string]any{
			"orders": []string{"book the flights", "find somewhere to eat"}})}}},
	}
	first := postUserLine(t, graphStore, "fan-out", "book the flights")
	last := postUserLine(t, graphStore, "fan-out", "and find somewhere to eat")

	if err := New(client, graphStore).poll(context.Background(), newSessionCursors(0)); err != nil {
		t.Fatalf("poll: %v", err)
	}
	// The spawn and the sentence after it: one turn, two calls, one prompt built
	// once from both of the person's rows.
	if calls := client.callCount(); calls != 2 {
		t.Fatalf("provider calls = %d, want the spawn and the sentence of one turn", calls)
	}
	opening := client.prompt(0)
	if !strings.Contains(opening, first.Body) || !strings.Contains(opening, last.Body) {
		t.Fatalf("the turn did not open with both of the person's sentences:\n%s", opening)
	}
	commands, err := graphStore.PendingCommands(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 {
		t.Fatalf("queued %d pieces of work, want the folded turn to still fan out into two", len(commands))
	}
	replies := agentReplies(t, graphStore, "fan-out")
	if len(replies) != 1 || replies[0].Answers != last.Seq {
		t.Fatalf("replies = %d, answering %d; want one receipt covering both rows (%d)",
			len(replies), replies[0].Answers, last.Seq)
	}
}

// The freshness rule, stated where it is decided. A page can hold more than one
// turn, and everything that closes one is here: the quiet window, the row
// ceiling, and a message that belongs to a different protocol.
func TestFoldAheadReadsOneTurnOutOfThePage(t *testing.T) {
	at := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	user := func(seq int64, when time.Time, body string) store.Message {
		return store.Message{Seq: seq, Time: when, SessionID: "page", Role: store.RoleUser, Body: body}
	}
	fresh := []store.Message{
		user(1, at, "how are the totals?"),
		user(2, at.Add(4*time.Second), "actually, this quarter only"),
	}
	if fold := foldAhead(fresh); fold.rows != 2 || fold.last != 2 {
		t.Fatalf("a correction typed four seconds later did not fold: rows=%d last=%d", fold.rows, fold.last)
	}
	stale := []store.Message{
		user(1, at, "how are the totals?"),
		user(2, at.Add(time.Hour), "and the headcount?"),
	}
	fold := foldAhead(stale)
	if fold.rows != 1 || fold.last != 1 {
		t.Fatalf("a message an hour later was folded in: rows=%d last=%d", fold.rows, fold.last)
	}
	// The page therefore yields two turns, which is what the poll answers.
	if next := foldAhead(stale[1:]); next.rows != 1 || next.last != 2 {
		t.Fatalf("the stale message did not open its own turn: rows=%d last=%d", next.rows, next.last)
	}

	typing := make([]store.Message, 0, 6)
	for index := 0; index < 6; index++ {
		typing = append(typing, user(int64(index+1), at.Add(time.Duration(index)*time.Second), "and another thing"))
	}
	if fold := foldAhead(typing); fold.rows != foldLimit || fold.last != int64(foldLimit) {
		t.Fatalf("the row ceiling did not hold: rows=%d last=%d", fold.rows, fold.last)
	}

	narrated := []store.Message{
		user(1, at, "how are the totals?"),
		{Seq: 2, Time: at.Add(time.Second), Role: store.RoleAgent, NodeID: "job-1", Body: "reading the ledger"},
		user(3, at.Add(2*time.Second), "actually, this quarter only"),
	}
	if fold := foldAhead(narrated); fold.rows != 2 || fold.last != 3 {
		t.Fatalf("a job narrating between two sentences closed the turn: rows=%d last=%d", fold.rows, fold.last)
	}

	steering := []store.Message{
		user(1, at, "how are the totals?"),
		{Seq: 2, Time: at.Add(time.Second), SessionID: "page", Role: store.RoleUser,
			NodeID: "job-1", Body: "check the second sheet too"},
		user(3, at.Add(2*time.Second), "actually, this quarter only"),
	}
	if fold := foldAhead(steering); fold.rows != 2 || fold.last != 3 {
		t.Fatalf("steering meant for a worker changed the turn: rows=%d last=%d", fold.rows, fold.last)
	}

	elsewhere := []store.Message{
		user(1, at, "how are the totals?"),
		{Seq: 2, Time: at.Add(time.Second), SessionID: "other-window",
			Role: store.RoleUser, Body: "what is running?"},
		user(3, at.Add(2*time.Second), "actually, this quarter only"),
	}
	if fold := foldAhead(elsewhere); fold.rows != 1 || fold.last != 1 {
		t.Fatalf("the turn stepped over another window's words: rows=%d last=%d", fold.rows, fold.last)
	}

	answered := []store.Message{
		user(1, at, "how are the totals?"),
		{Seq: 2, Time: at.Add(time.Second), SessionID: "page", Role: store.RoleUser,
			QuestionSeq: 7, Body: "the second"},
	}
	if fold := foldAhead(answered); fold.rows != 1 || fold.last != 1 {
		t.Fatalf("an answer to a numbered question was folded away: rows=%d last=%d", fold.rows, fold.last)
	}
}

// A second window is a different conversation on one journal, and the cursor is
// journal-wide: a fold that stepped over a visitor's turn would mark it handled
// and it would never be answered at all.
func TestAFoldNeverStepsOverAnotherWindowsTurn(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &foldClient{reply: "Noted."}
	here := postUserLine(t, graphStore, "this-window", "how are the totals?")
	visitor := postUserLine(t, graphStore, "that-window", "what is running?")
	back := postUserLine(t, graphStore, "this-window", "actually, this quarter only")

	if err := New(client, graphStore).poll(context.Background(), newSessionCursors(0)); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if calls := client.callCount(); calls != 3 {
		t.Fatalf("provider calls = %d, want one per turn across the two windows", calls)
	}
	if replies := agentReplies(t, graphStore, "that-window"); len(replies) != 1 {
		t.Fatalf("the visitor window got %d replies, want one", len(replies))
	}
	if replies := agentReplies(t, graphStore, "this-window"); len(replies) != 2 {
		t.Fatalf("this window got %d replies for turns %d and %d, want two",
			len(replies), here.Seq, back.Seq)
	}
	if visitor.Seq <= here.Seq || back.Seq <= visitor.Seq {
		t.Fatal("the journal did not interleave the two windows")
	}
}

// The poll only looks at the journal between turns, so a correction typed while
// the routing call is out used to be answered afterwards — the same two replies
// by a slower route. The turn withdraws and asks again with both halves.
func TestAMessageArrivingMidTurnIsAbsorbedIntoIt(t *testing.T) {
	graphStore := openHeadStore(t)
	var second store.Message
	client := &foldClient{reply: "This quarter: 12,004."}
	client.hold = func(call int, ctx context.Context) {
		if call != 1 {
			return
		}
		second = postUserLine(t, graphStore, "mid-turn", "actually, this quarter only")
		<-ctx.Done()
	}
	first := postUserLine(t, graphStore, "mid-turn", "how are the totals?")

	conversationalHead := New(client, graphStore)
	cursors := newSessionCursors(0)
	if err := conversationalHead.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if calls := client.callCount(); calls != 2 {
		t.Fatalf("provider calls = %d, want the first turn withdrawn and asked once more", calls)
	}
	prompt := client.prompt(1)
	if !strings.Contains(prompt, first.Body) || !strings.Contains(prompt, second.Body) {
		t.Fatalf("the refolded turn did not carry both sentences:\n%s", prompt)
	}
	replies := agentReplies(t, graphStore, "mid-turn")
	if len(replies) != 1 {
		t.Fatalf("thread has %d replies, want one answer covering both sentences", len(replies))
	}
	if replies[0].Answers != second.Seq {
		t.Fatalf("the reply answers %d, want the row it absorbed (%d)", replies[0].Answers, second.Seq)
	}
	if cursor := cursors.answeredThrough("mid-turn"); cursor < second.Seq {
		t.Fatalf("cursor = %d, want past the absorbed row %d", cursor, second.Seq)
	}
}

// Somebody who keeps typing must still be answered. Every arrival refolds the
// turn until the row ceiling, and then the turn finishes carrying all of them.
func TestContinuousTypingStillEndsInAnAnswer(t *testing.T) {
	graphStore := openHeadStore(t)
	typed := make([]store.Message, 0, foldLimit)
	var typedMutex sync.Mutex
	client := &foldClient{reply: "Here is everything you asked."}
	client.hold = func(call int, ctx context.Context) {
		if call >= foldLimit {
			return
		}
		typedMutex.Lock()
		typed = append(typed, postUserLine(t, graphStore, "still-typing", "and another thing"))
		typedMutex.Unlock()
		<-ctx.Done()
	}
	opening := postUserLine(t, graphStore, "still-typing", "how are the totals?")

	cursors := newSessionCursors(0)
	if err := New(client, graphStore).poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if calls := client.callCount(); calls != foldLimit {
		t.Fatalf("provider calls = %d, want one per refold up to the ceiling (%d)", calls, foldLimit)
	}
	typedMutex.Lock()
	last := typed[len(typed)-1]
	typedMutex.Unlock()
	prompt := client.prompt(foldLimit - 1)
	if !strings.Contains(prompt, opening.Body) || strings.Count(prompt, last.Body) == 0 {
		t.Fatalf("the last turn did not carry what the person kept typing:\n%s", prompt)
	}
	replies := agentReplies(t, graphStore, "still-typing")
	if len(replies) != 1 {
		t.Fatalf("thread has %d replies, want one", len(replies))
	}
	if replies[0].Answers != last.Seq {
		t.Fatalf("the reply answers %d, want every row it carried (%d)", replies[0].Answers, last.Seq)
	}
	if cursor := cursors.answeredThrough("still-typing"); cursor < last.Seq {
		t.Fatalf("cursor = %d, want past every row the turn answered (%d)", cursor, last.Seq)
	}
}
