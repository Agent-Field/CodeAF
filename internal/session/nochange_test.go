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
	"unicode/utf8"

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

	// AN ASK THAT FILLS MOST OF THE PAGE LEAVES THE LAST WORDS LESS ROOM, AND THEY
	// ARE STILL CUT BY NAME. The digest's backstop cuts from the end with a bare
	// `…`, which is exactly the marker a reader took for a broken answer.
	long := strings.Repeat("keep every seat's constraints in mind. ", 14_000/39)
	report := strings.Repeat("| seat | model | why this seat |\n", 8_000/32)
	page := checkpointCompletionPage(long, []ai.Message{textMessage("assistant", report)})
	if len(page) > checkpointDigestBytes {
		t.Errorf("the page is %d bytes, over the %d digest bound", len(page), checkpointDigestBytes)
	}
	if !strings.Contains(page, strings.TrimSpace(long)) {
		t.Error("the ask did not reach the reader whole")
	}
	if strings.HasSuffix(page, "…") || !strings.Contains(page, "[clipped by codeaf:") {
		t.Errorf("last words squeezed by the ask were not cut by name; the page ends %q", page[len(page)-80:])
	}

	// EVERY BYTE OFFSET OF A TWO-BYTE RUN, so whichever parity the mark's width
	// leaves the cut on, one of these lands it inside a rune.
	for _, lead := range []string{"", "a"} {
		huge := lead + strings.Repeat("é", checkpointSaidBytes)
		said := checkpointClipSaid(huge, checkpointSaidBytes)
		if len(said) > checkpointSaidBytes {
			t.Errorf("clipped last words are %d bytes, over the %d bound", len(said), checkpointSaidBytes)
		}
		if !utf8.ValidString(said) {
			t.Errorf("the cut split a character: %q", said[len(said)-60:])
		}
		kept := strings.LastIndex(said, "\n[clipped by codeaf: ")
		if kept < 0 {
			t.Fatalf("a cut was not named in words: %q", said[len(said)-80:])
		}
		if want := "\n" + fmt.Sprintf(checkpointClippedMark, kept, len(huge)); said[kept:] != want {
			t.Errorf("the mark miscounts the cut: got %q, want %q", said[kept:], want)
		}
		if !strings.HasPrefix(huge, said[:kept]) {
			t.Error("the clipped words are not the head of what was said")
		}
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
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointAgent(t, completer, func(config *Config) { config.Memory = openTestBrain(t) })
	learned := observeMemoryExtraction(completer)
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
	if got := saidHowOften(notices, checkpointCarryOnNote); got != 1 || len(notices) != 1 {
		t.Errorf("want exactly the one carry-on notice and nothing after it; notices were %q", notices)
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
	if err := agent.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	exchange := learnedExchange(t, learned)
	if !strings.Contains(exchange, "ASSISTANT:\n| seat | model | why |") ||
		!strings.Contains(exchange, "| seat 01 |") || strings.Contains(exchange, "ASSISTANT:\n"+NoChangeReply) {
		t.Errorf("the memory reflex did not receive the answer before the withdrawn token:\n%s", exchange)
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

// A TOKEN AFTER WORK IS NOT AN ANSWER TO THE NOTE. The model was carried on,
// made calls, and then said [no change]: those calls may have changed what the
// answer before the note says, so the turn is read again rather than ended on
// it unread.
func TestNoChangeAfterCallsMadeForTheNoteIsReadAgain(t *testing.T) {
	var remainsAsks, rounds, afterNote atomic.Int64
	steps := make([]step, checkpointMarkAt(1)+40)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForRemains(messages) {
				if remainsAsks.Add(1) == 1 {
					return textResponse("seat 3 has no model named"), nil
				}
				return textResponse(checkpointNothingLeft), nil
			}
			if last := messages[len(messages)-1]; last.Role == "user" && strings.HasPrefix(partsText(last), checkpointCarryOnLead) {
				afterNote.Add(1)
				return toolResponseWithText("call-note", "ls", `{"path":"./seat-3"}`, ""), nil
			}
			if afterNote.Load() > 0 {
				return textResponse(NoChangeReply), nil
			}
			if call := rounds.Add(1); call <= int64(checkpointMarkAt(1)) {
				return toolResponseWithText(fmt.Sprintf("call-%d", call), "ls",
					fmt.Sprintf(`{"path":"./%d"}`, call), ""), nil
			}
			return textResponse(perSeatTable()), nil
		}
	}
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointAgent(t, completer, func(config *Config) { config.Memory = openTestBrain(t) })
	learned := observeMemoryExtraction(completer)
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "compare the models per seat in a table")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	if got := remainsAsks.Load(); got != 2 {
		t.Errorf("the reader was spent %d times, want twice: a token after calls must not end the turn unread", got)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if exchange := learnedExchange(t, learned); !strings.Contains(exchange, "ASSISTANT:\n"+NoChangeReply) {
		t.Errorf("the memory reflex was not handed the non-withdrawn token reply:\n%s", exchange)
	}
}

// observeMemoryExtraction reads the existing reflex seam without adding a
// production hook. The checkpoint fixture already routes concurrent readers
// through scriptedCompleter.aside; this wraps that route so the post-turn
// extractor can answer without consuming one of the conversation's steps.
func observeMemoryExtraction(completer *scriptedCompleter) <-chan string {
	learned := make(chan string, 1)
	prior := completer.aside
	completer.aside = func(messages []ai.Message) (*ai.Response, bool) {
		if len(messages) > 1 && strings.Contains(messageText(messages[0]), "worth remembering after this session ends") {
			learned <- messageText(messages[1])
			return textResponse(`{"mem":0}`), true
		}
		if prior != nil {
			return prior(messages)
		}
		return nil, false
	}
	return learned
}

func learnedExchange(t *testing.T, learned <-chan string) string {
	t.Helper()
	select {
	case exchange := <-learned:
		return exchange
	default:
		t.Fatal("the memory reflex never read the exchange")
		return ""
	}
}

// THE TOKEN ANSWERS THE READER'S NOTE AND NOTHING ELSE THAT OPENS A TURN BACK UP.
// A load or ask nudge also wears [carry on], and a person can steer in between;
// neither is the note, so neither is ended by the token.
func TestNoChangeAnswersOnlyTheReadersNote(t *testing.T) {
	note := textMessage("user", checkpointCarryOnLead+"seat 3 has no model named")
	token := textMessage("assistant", NoChangeReply)
	for _, shape := range []struct {
		name     string
		messages []ai.Message
		want     bool
	}{
		{"the note, then the token", []ai.Message{note, token}, true},
		{"the note, a volatile note, then the token", []ai.Message{note, textMessage("user", volatileNoteOpening+" the clock moved"), token}, true},
		{"a load nudge, then the token", []ai.Message{textMessage("user", checkpointLoadNudgeLead([]string{"ask"})), token}, false},
		{"the note, the person steering, then the token", []ai.Message{note, textMessage("user", "leave seat 3 blank"), token}, false},
		{"the note, then the token with more words", []ai.Message{note, textMessage("assistant", NoChangeReply+" the table is whole")}, false},
		{"the token with nothing before it", []ai.Message{token}, false},
	} {
		if got := answeredNoteUnchanged(shape.messages); got != shape.want {
			t.Errorf("%s: honoured %v, want %v", shape.name, got, shape.want)
		}
	}
}
