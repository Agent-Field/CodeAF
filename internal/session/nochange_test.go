package session

// A COMPLETION CHECK THAT WAS WRONG IS TOLD SO IN ONE TOKEN, AND THE ANSWER STANDS.
//
// THE MEASURED FAILURE (#1065): a person asked for a per-seat model table, the
// model wrote it, and the end-of-turn reader — shown the first six hundred bytes
// of it — said three times running that the table was cut off. The model
// reprinted it twice and then argued, and a finished turn folds to its last
// words, so the argument was the only thing the person was left reading.
//
// Three contracts close it and each is pinned here: the reader is shown a real
// answer whole and told in words when it is not; a carry-on the model answers
// with [NoChangeReply] ends carrying on for the ask; and that reply is never
// the answer a person reads back.

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// perSeatTable is an answer well past the six hundred bytes the reader used to
// be shown, in the shape the measured turn ended on.
func perSeatTable() string {
	var table strings.Builder
	table.WriteString("| seat | model | why |\n| --- | --- | --- |\n")
	for seat := 1; seat <= 30; seat++ {
		fmt.Fprintf(&table, "| seat %02d | deepseek/deepseek-v4.1-flash | cheap and fast for this seat's work |\n", seat)
	}
	return strings.TrimSpace(table.String())
}

// THE READER SEES A TABLE WHOLE, AND A CUT IT CANNOT AVOID SAYS WHOSE CUT IT IS.
func TestTheReaderSeesALongAnswerWholeAndACutNamesItself(t *testing.T) {
	table := perSeatTable()
	if len(table) <= 600 {
		t.Fatalf("the fixture is %d bytes, which the old bound already showed whole", len(table))
	}
	digest := checkpointDigest("a per-seat model table", []ai.Message{textMessage("assistant", table)})
	if !strings.Contains(digest, table) {
		t.Fatalf("a %d-byte answer did not reach the reader whole:\n%s", len(table), digest)
	}
	if strings.Contains(digest, "[clipped by codeaf:") {
		t.Fatalf("an answer that fit was marked clipped:\n%s", digest)
	}

	huge := strings.Repeat("é long report line that keeps going\n", 2*checkpointSaidBytes/36)
	said := checkpointClipSaid(huge)
	if len(said) > checkpointSaidBytes {
		t.Errorf("clipped last words are %d bytes, over the %d bound", len(said), checkpointSaidBytes)
	}
	kept := strings.LastIndex(said, "\n[clipped by codeaf: ")
	if kept < 0 {
		t.Fatalf("a cut was not named in words: %q", said[len(said)-80:])
	}
	if want := fmt.Sprintf(checkpointClippedMark, kept, len(huge)); !strings.HasSuffix(said, want) {
		t.Errorf("the mark miscounts the cut: got %q, want suffix %q", said[kept:], want)
	}
	if !strings.HasPrefix(huge, said[:kept]) {
		t.Error("the clipped words are not the head of what was said")
	}
	// AND THE READER IS TOLD WHAT THE MARK MEANS, in the ask it is read with.
	if !strings.Contains(checkpointRemainsAsk, "never evidence that the person saw a cut-off answer") {
		t.Error("the remains ask no longer tells the reader a clipped summary is not a cut-off answer")
	}
}

// A CARRY-ON ANSWERED [no change] ENDS CARRYING ON, AND THE ANSWER BEFORE IT STANDS.
//
// The reader here says the same false thing every time it is asked, in words
// that change each time so the standstill rule is not what ends the turn.
func TestACarryOnAnsweredNoChangeEndsItAndKeepsTheAnswer(t *testing.T) {
	table := perSeatTable()
	var remainsAsks, carriedOn, rounds atomic.Int64
	steps := make([]step, checkpointMarkAt(1)+40)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForRemains(messages) {
				return textResponse(fmt.Sprintf("the table was cut off mid-row (look %d)", remainsAsks.Add(1))), nil
			}
			if last := messages[len(messages)-1]; last.Role == "user" && strings.HasPrefix(partsText(last), checkpointCarryOnLead) {
				carriedOn.Add(1)
				return textResponse(NoChangeReply), nil
			}
			if call := rounds.Add(1); call <= int64(checkpointMarkAt(1)) {
				return toolResponseWithText(fmt.Sprintf("call-%d", call), "ls",
					fmt.Sprintf(`{"path":"./%d"}`, call), ""), nil
			}
			return textResponse(table), nil
		}
	}
	agent := checkpointAgent(t, &scriptedCompleter{steps: steps})
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "compare the models per seat in a table; do not change any files")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	notices := noticeTexts(collect(t, events))

	if got := carriedOn.Load(); got != 1 {
		t.Errorf("the model was carried on %d times, want once; notices were %q", got, notices)
	}
	if got := remainsAsks.Load(); got != 1 {
		t.Errorf("the reader was spent %d times, want once: a rejected note is not read for again", got)
	}
	if got := strings.Count(transcriptText(agent), checkpointCarryOnLead); got != 1 {
		t.Errorf("%d continuations were written into the turn, want one", got)
	}
	if saidSomething(notices, checkpointCarriedOnNote(nil)[:20]) {
		t.Errorf("the turn reached the carry-on cap instead of ending on [no change]: %q", notices)
	}

	// THE MODEL KEEPS ITS TOKEN; THE PERSON'S HISTORY DOES NOT.
	if !strings.Contains(transcriptText(agent), NoChangeReply) {
		t.Error("the model's own record lost the reply it made")
	}
	entries, _, stop := agent.AttachReplay()
	stop()
	last := ""
	for _, entry := range entries {
		if IsNoChangeReply(entry.Text) {
			t.Errorf("a %q row reached the person's history", NoChangeReply)
		}
		if entry.Role == "assistant" && strings.TrimSpace(entry.Text) != "" {
			last = entry.Text
		}
	}
	if last != table {
		t.Errorf("the answer left standing is not the table:\n%s", last)
	}
}

// THE TOKEN IS AN ANSWER TO A CARRY-ON AND NOTHING ELSE. A turn that says it
// without having been carried on is read for what remains like any other, so
// the token cannot become a way to end a turn unread.
func TestNoChangeWithoutACarryOnDoesNotEndTheReading(t *testing.T) {
	var remainsAsks, rounds atomic.Int64
	steps := make([]step, checkpointMarkAt(1)+10)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForRemains(messages) {
				remainsAsks.Add(1)
				return textResponse(checkpointNothingLeft), nil
			}
			if call := rounds.Add(1); call <= int64(checkpointMarkAt(1)) {
				return toolResponseWithText(fmt.Sprintf("call-%d", call), "ls",
					fmt.Sprintf(`{"path":"./%d"}`, call), ""), nil
			}
			return textResponse(NoChangeReply), nil
		}
	}
	agent := checkpointAgent(t, &scriptedCompleter{steps: steps})
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "look through these paths")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	if got := remainsAsks.Load(); got != 1 {
		t.Errorf("the reader was spent %d times on a turn never carried on, want once", got)
	}
}
