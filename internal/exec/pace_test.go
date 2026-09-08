package exec

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestTheBriefNamesTheLeafsWall(t *testing.T) {
	brief := func(wall time.Duration) string {
		t.Helper()
		client := &scriptedCompleter{}
		linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, wall)
		if _, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"}); err != nil {
			t.Fatal(err)
		}
		if len(client.seen) == 0 || len(client.seen[0]) < 2 {
			t.Fatalf("the leaf saw no brief: %+v", client.seen)
		}
		return client.seen[0][1].Content[0].Text
	}

	fortyFive := brief(45 * time.Minute)
	fifteen := brief(15 * time.Minute)
	if !strings.Contains(fortyFive, "45m of wall-clock time") {
		t.Fatalf("45-minute brief does not name its wall: %q", fortyFive)
	}
	if !strings.Contains(fifteen, "15m of wall-clock time") {
		t.Fatalf("15-minute brief does not name its wall: %q", fifteen)
	}
	if fortyFive == fifteen {
		t.Fatal("leaves with different walls received the same brief")
	}
}

func TestWallDurationTextUsesTimeoutSpelling(t *testing.T) {
	cases := map[time.Duration]string{
		45 * time.Minute:                   "45m",
		90 * time.Minute:                   "1h30m",
		2*time.Minute + 30*time.Second:     "2m30s",
		30 * time.Second:                   "30s",
		2*time.Hour + 400*time.Millisecond: "2h",
		2*time.Hour + 600*time.Millisecond: "2h0m1s",
		400 * time.Millisecond:             "",
		0:                                  "",
		-time.Second:                       "",
	}
	for duration, want := range cases {
		if got := wallDurationText(duration); got != want {
			t.Errorf("wallDurationText(%s) = %q, want %q", duration, got, want)
		}
	}
}

func TestTheLeafIsPacedAgainstItsWallBeforeTheLandingReserve(t *testing.T) {
	space := workspace(t)
	client := &scriptedCompleter{
		turns: [][]ai.ToolCall{
			{call("first", "sh", `{"cmd":"printf first"}`)},
			{call("second", "sh", `{"cmd":"printf second"}`)},
			{call("third", "sh", `{"cmd":"printf third"}`)},
		},
		delays: []time.Duration{5100 * time.Millisecond, 1800 * time.Millisecond, 2600 * time.Millisecond},
	}
	linear := NewLinear(client, space, nil, 10, 1_000_000, 10*time.Second)
	if _, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"}); err != nil {
		t.Fatal(err)
	}

	paceCall, landingCall := -1, -1
	for callIndex, messages := range client.seen {
		for _, message := range messages {
			if message.Role != "user" || len(message.Content) == 0 {
				continue
			}
			body := message.Content[0].Text
			if paceCall < 0 && strings.Contains(body, "clock for this task now reads") {
				paceCall = callIndex
				if !strings.Contains(body, " gone and ") || !strings.Contains(body, " left") {
					t.Fatalf("clock reading does not name time gone and left: %q", body)
				}
			}
			if landingCall < 0 && strings.Contains(body, "wall-clock deadline for this task is close") {
				landingCall = callIndex
			}
		}
	}
	if paceCall < 0 {
		t.Fatalf("the leaf never received its clock reading across %d calls", len(client.seen))
	}
	if landingCall < 0 {
		t.Fatalf("the leaf never received its deadline landing instruction across %d calls", len(client.seen))
	}
	if paceCall >= landingCall {
		t.Fatalf("clock reading arrived at call %d, deadline landing at %d", paceCall, landingCall)
	}

	final := client.seen[len(client.seen)-1]
	paceMessages := 0
	landingMessage := -1
	for index, message := range final {
		if message.Role != "user" || len(message.Content) == 0 {
			continue
		}
		body := message.Content[0].Text
		if strings.Contains(body, "clock for this task now reads") {
			paceMessages++
			if landingMessage >= 0 {
				t.Fatalf("clock reading appeared after landing instruction in the transcript: %+v", final)
			}
		}
		if strings.Contains(body, "wall-clock deadline for this task is close") {
			landingMessage = index
		}
	}
	if paceMessages != 1 {
		t.Fatalf("clock reading messages = %d, want exactly one", paceMessages)
	}
}

func TestAReadOnlyLeafIsAskedForItsResultAndAskedAgain(t *testing.T) {
	turns := distinctReadTurns(2 * noProgressReconTurns)
	turns = append(turns, nil)
	client := &scriptedCompleter{turns: turns}
	linear := NewLinear(client, workspace(t), nil, 100, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "research"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %s, want %s", outcome.Stop, StopDone)
	}

	final := client.seen[len(client.seen)-1]
	notices := messagesContaining(final, "consecutive tool-calling turns without changing the workspace")
	if len(notices) != 2 {
		t.Fatalf("read-only notices = %d, want two: %+v", len(notices), notices)
	}
	if !strings.Contains(notices[0], itoa(noProgressReconTurns)) {
		t.Fatalf("first notice does not carry its turn count: %q", notices[0])
	}
	if !strings.Contains(notices[1], itoa(2*noProgressReconTurns)) {
		t.Fatalf("second notice does not carry its larger turn count: %q", notices[1])
	}
	for _, notice := range notices {
		if !strings.Contains(notice, "Produce the result now") {
			t.Fatalf("notice does not ask for the result: %q", notice)
		}
	}
	if transcriptContains(client, noProgressConcludeDirective) {
		t.Fatalf("read-only leaf received a conclude directive: %+v", final)
	}
}

func TestAProducingLeafIsNeverPaced(t *testing.T) {
	run := func(t *testing.T, turns [][]ai.ToolCall, brief string) (*scriptedCompleter, *Outcome) {
		t.Helper()
		client := &scriptedCompleter{turns: turns}
		linear := NewLinear(client, workspace(t), nil, 100, 1_000_000, time.Minute)
		outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: brief})
		if err != nil {
			t.Fatal(err)
		}
		return client, outcome
	}

	turnCount := 2*noProgressReconTurns + 2
	producingTurns := make([][]ai.ToolCall, turnCount)
	for index := range producingTurns {
		producingTurns[index] = []ai.ToolCall{call(
			fmt.Sprintf("write-%d", index), "sh",
			fmt.Sprintf(`{"cmd":"printf 'step %d' > result-%d.txt"}`, index, index),
		)}
	}
	client, outcome := run(t, producingTurns, "produce")
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %s, want %s", outcome.Stop, StopDone)
	}
	if transcriptContains(client, "consecutive tool-calling turns without changing the workspace") {
		t.Fatalf("a producing leaf received a recon notice: %+v", client.seen[len(client.seen)-1])
	}

	// The read-only control binds the absence above to the production signal:
	// without the notice in production, this half fails instead of passing vacuously.
	readOnly, readOutcome := run(t, distinctReadTurns(turnCount), "research")
	if readOutcome.Stop != StopDone {
		t.Fatalf("read-only control stop = %s, want %s", readOutcome.Stop, StopDone)
	}
	if !transcriptContains(readOnly, "You have made "+itoa(noProgressReconTurns)+
		" consecutive tool-calling turns without changing the workspace") {
		t.Fatalf("read-only control did not receive the recon notice: %+v", readOnly.seen[len(readOnly.seen)-1])
	}
}

// A leaf whose deliverable is its answer reads and mutates nothing by design,
// so the guard may ask for that answer but may never stop it for reading.
func TestALeafWhoseEveryReadIsFreshIsNeverConcluded(t *testing.T) {
	turns := distinctReadTurns(3 * noProgressReconTurns)
	turns = append(turns, nil)
	client := &scriptedCompleter{turns: turns}
	space := workspace(t)
	linear := NewLinear(client, space, nil, len(turns)+landingTurns+2, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "research"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %s, want %s", outcome.Stop, StopDone)
	}
	if outcome.Exhausted != "" {
		t.Fatalf("exhausted = %q, want empty", outcome.Exhausted)
	}
	if outcome.Turns <= 2*noProgressReconTurns {
		t.Fatalf("read-only leaf took %d turns, want more than %d", outcome.Turns, 2*noProgressReconTurns)
	}
	if transcriptContains(client, noProgressConcludeDirective) {
		t.Fatalf("a fresh-reading leaf received a conclude directive: %+v", client.seen[len(client.seen)-1])
	}

	record, err := os.ReadFile(TraceFile(space.Root(), "1"))
	if err != nil {
		t.Fatal(err)
	}
	recorded := string(record)
	if strings.Contains(recorded, "conclude directive injected") {
		t.Fatalf("the recorder contains a conclude directive: %q", record)
	}
	if strings.Contains(recorded, string(StopNoProgress)) {
		t.Fatalf("the recorder says a fresh-reading leaf stopped for no progress: %q", record)
	}

	notices := messagesContaining(client.seen[len(client.seen)-1],
		"consecutive tool-calling turns without changing the workspace")
	if len(notices) < 2 {
		t.Fatalf("read-only notices = %d, want at least two: %+v", len(notices), notices)
	}
	if !strings.Contains(notices[0], itoa(noProgressReconTurns)) {
		t.Fatalf("first notice does not carry its turn count: %q", notices[0])
	}
	if !strings.Contains(notices[1], itoa(2*noProgressReconTurns)) {
		t.Fatalf("second notice does not carry its larger turn count: %q", notices[1])
	}
}

func TestALeafAlreadyLandingIsNeverPaced(t *testing.T) {
	space := workspace(t)
	client := &scriptedCompleter{
		turns:  [][]ai.ToolCall{{call("read", "sh", `{"cmd":"printf result"}`)}},
		delays: []time.Duration{1100 * time.Millisecond},
	}
	linear := NewLinear(client, space, nil, 8, 12, 2*time.Second)
	if _, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "research"}); err != nil {
		t.Fatal(err)
	}

	final := client.seen[len(client.seen)-1]
	if len(messagesContaining(final, "The budget for this task is spent.")) == 0 {
		t.Fatalf("the budget landing was not active in the final transcript: %+v", final)
	}
	if transcriptContains(client, "clock for this task now reads") {
		t.Fatalf("a leaf already landing received a clock reading: %+v", final)
	}
	record, err := os.ReadFile(TraceFile(space.Root(), "1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), "budget exhausted — landing reserve granted") {
		t.Fatalf("the recorder does not show the budget landing: %q", record)
	}
	if strings.Contains(string(record), "wall pace —") {
		t.Fatalf("the recorder paced a leaf already landing: %q", record)
	}
}

func TestALeafRepeatingItselfIsStillConcluded(t *testing.T) {
	t.Run("the same call and the same result", func(t *testing.T) {
		space := workspace(t)
		turns := make([][]ai.ToolCall, noProgressRepeatCap+landingTurns+2)
		repeated := call("same", "sh", `{"cmd":"printf same"}`)
		for index := range turns {
			turns[index] = []ai.ToolCall{repeated}
		}
		client := &scriptedCompleter{turns: turns}
		linear := NewLinear(client, space, nil, len(turns), 1_000_000, time.Minute)
		outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Stop != StopNoProgress {
			t.Fatalf("stop = %s, want %s", outcome.Stop, StopNoProgress)
		}
		record, err := os.ReadFile(TraceFile(space.Root(), "1"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(record), "same tool call repeated") {
			t.Fatalf("the recorder does not name the repeat signal: %q", record)
		}
	})

	t.Run("content it has already seen", func(t *testing.T) {
		space := workspace(t)
		turns := make([][]ai.ToolCall, noProgressStagnantCap+landingTurns+2)
		alternating := []ai.ToolCall{
			call("one", "sh", `{"cmd":"printf one"}`),
			call("two", "sh", `{"cmd":"printf two"}`),
		}
		for index := range turns {
			turns[index] = []ai.ToolCall{alternating[index%len(alternating)]}
		}
		client := &scriptedCompleter{turns: turns}
		linear := NewLinear(client, space, nil, len(turns), 1_000_000, time.Minute)
		outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Stop != StopNoProgress {
			t.Fatalf("stop = %s, want %s", outcome.Stop, StopNoProgress)
		}
		record, err := os.ReadFile(TraceFile(space.Root(), "1"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(record), "consecutive turns with no filesystem mutations and no new information") {
			t.Fatalf("the recorder does not name the stagnant signal: %q", record)
		}
	})
}

func TestThePacingIsInTheRunsOwnRecord(t *testing.T) {
	t.Run("mutation-free recon", func(t *testing.T) {
		space := workspace(t)
		turns := append(distinctReadTurns(noProgressReconTurns), nil)
		client := &scriptedCompleter{turns: turns}
		linear := NewLinear(client, space, nil, len(turns), 1_000_000, time.Minute)
		if _, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "research"}); err != nil {
			t.Fatal(err)
		}

		record, err := os.ReadFile(TraceFile(space.Root(), "1"))
		if err != nil {
			t.Fatal(err)
		}
		want := "reading pace — " + itoa(noProgressReconTurns) +
			" consecutive tool-calling turns with no workspace mutation, result asked for"
		if !strings.Contains(string(record), want) {
			t.Fatalf("the recorder does not name the recon count: %q", record)
		}
		if strings.Contains(string(record), "conclude directive injected") {
			t.Fatalf("the recon notice was recorded as a conclusion: %q", record)
		}
	})

	t.Run("wall clock", func(t *testing.T) {
		space := workspace(t)
		client := &scriptedCompleter{
			turns:  [][]ai.ToolCall{{call("read", "sh", `{"cmd":"printf result"}`)}},
			delays: []time.Duration{1100 * time.Millisecond},
		}
		linear := NewLinear(client, space, nil, 10, 1_000_000, 2*time.Second)
		if _, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "research"}); err != nil {
			t.Fatal(err)
		}

		record, err := os.ReadFile(TraceFile(space.Root(), "1"))
		if err != nil {
			t.Fatal(err)
		}
		clockPrefix := "The clock for this task now reads "
		clockReading := ""
		for _, messages := range client.seen {
			for _, message := range messages {
				if message.Role == "user" && len(message.Content) > 0 &&
					strings.HasPrefix(message.Content[0].Text, clockPrefix) {
					clockReading = strings.TrimPrefix(message.Content[0].Text, clockPrefix)
				}
			}
		}
		goneAndLeft := strings.SplitN(clockReading, " gone and ", 2)
		if len(goneAndLeft) != 2 {
			t.Fatalf("the leaf did not receive a readable wall pace: %q", clockReading)
		}
		leftAndAdvice := strings.SplitN(goneAndLeft[1], " left.", 2)
		if len(leftAndAdvice) != 2 {
			t.Fatalf("the leaf did not receive time left in its wall pace: %q", clockReading)
		}
		want := "wall pace — " + goneAndLeft[0] + " gone, " + leftAndAdvice[0] + " left"
		if !strings.Contains(string(record), want) {
			t.Fatalf("the recorder does not name the wall reading: %q", record)
		}
	})
}

func distinctReadTurns(count int) [][]ai.ToolCall {
	turns := make([][]ai.ToolCall, count)
	for index := range turns {
		turns[index] = []ai.ToolCall{call(
			fmt.Sprintf("read-%d", index), "sh", fmt.Sprintf(`{"cmd":"printf 'result-%d'"}`, index),
		)}
	}
	return turns
}

func messagesContaining(messages []ai.Message, fragment string) []string {
	var matches []string
	for _, message := range messages {
		for _, part := range message.Content {
			if strings.Contains(part.Text, fragment) {
				matches = append(matches, part.Text)
			}
		}
	}
	return matches
}

func transcriptContains(client *scriptedCompleter, fragment string) bool {
	for _, messages := range client.seen {
		if len(messagesContaining(messages, fragment)) > 0 {
			return true
		}
	}
	return false
}

// The caller can leave less time than the leaf's nominal lease. The brief and
// pacing must describe that actual clock rather than invent elapsed time.
func TestTheLeafPacesAgainstAnEarlierCallerDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := &scriptedCompleter{}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Hour)
	if _, err := linear.Run(parent, Task{NodeID: 1, Brief: "work"}); err != nil {
		t.Fatal(err)
	}
	brief := client.seen[0][1].Content[0].Text
	if !strings.Contains(brief, "10s of wall-clock time") || strings.Contains(brief, "1h of wall-clock time") {
		t.Fatalf("the brief ignored its caller's shorter wall: %q", brief)
	}
	if transcriptContains(client, "clock for this task now reads") {
		t.Fatal("the new leaf claimed time had already elapsed before its first turn")
	}
}
