package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// wallWork is one session sitting idle behind a task that runs until its
// context is cut. The runner lands the stopped node through the real graph
// transition, so these tests observe the same row and wake road as a task in a
// working copy.
type wallWork struct {
	agent     *Agent
	completer *scriptedCompleter
	journal   string
	node      *TaskNode
	settled   <-chan struct{}
	worktree  string
	branch    string
}

func newWallWork(t *testing.T, wall time.Duration) wallWork {
	t.Helper()
	journal := writeableJournal(t)
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Unattended = true
		config.Budget = Budget{Wall: wall}
		config.SessionFile = journal
	})
	worktree := filepath.Join(t.TempDir(), "task-worktree")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("make the task working copy: %v", err)
	}
	marker := filepath.Join(worktree, "kept.txt")
	if err := os.WriteFile(marker, []byte("work already done\n"), 0o644); err != nil {
		t.Fatalf("write the task's kept work: %v", err)
	}
	branch := "task/wall-work"
	started := make(chan struct{})
	settled := make(chan struct{})
	graph := stubbedGraph(agent, func(node *TaskNode) {
		if !node.claimRun() {
			return
		}
		ctx, _ := node.runContext()
		node.graph.mu.Lock()
		node.worktree = worktree
		node.branch = branch
		node.graph.mu.Unlock()
		close(started)
		<-ctx.Done()
		node.finish("stopped before it finished", []string{marker}, branch, mergeAborted)
		node.graph.complete(node, TaskFailed)
		close(settled)
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: "wall work", brief: "keep working", acceptance: "the work is done", named: true,
	})
	waitSignal(t, started, "the work under the wall to start")
	return wallWork{
		agent: agent, completer: completer, journal: journal, node: graph.node(id),
		settled: settled, worktree: worktree, branch: branch,
	}
}

func spendWall(t *testing.T, agent *Agent) string {
	t.Helper()
	steward := agent.steward()
	if steward == nil {
		t.Fatal("the unattended session has no Steward")
	}
	steward.mu.Lock()
	started, wall := steward.started, steward.wall
	steward.now = func() time.Time { return started.Add(wall) }
	steward.mu.Unlock()
	spent, why := steward.Budget().Exhausted()
	if !spent {
		t.Fatal("the moved wall clock did not exhaust the budget")
	}
	return why
}

func wallDecisions(t *testing.T, path string) []journalPrincipal {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the session journal: %v", err)
	}
	var decisions []journalPrincipal
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Principal != nil && entry.Principal.Who == "steward" && entry.Principal.Event == "decided" {
			decisions = append(decisions, *entry.Principal)
		}
	}
	return decisions
}

func awaitWallDecision(t *testing.T, path string, within time.Duration) journalPrincipal {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if rows := wallDecisions(t, path); len(rows) > 0 {
			return rows[len(rows)-1]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the wall wrote no ending within %s", within)
	return journalPrincipal{}
}

func awaitWallStream(t *testing.T, lane <-chan (<-chan Event), within time.Duration) <-chan Event {
	t.Helper()
	select {
	case stream, open := <-lane:
		if !open {
			t.Fatal("the standing lane closed before the wall said its ending")
		}
		return stream
	case <-time.After(within):
		t.Fatalf("the wall put no line on the standing lane within %s", within)
		return nil
	}
}

func TestAWallWithWorkOutEndsTheRun(t *testing.T) {
	work := newWallWork(t, 2*time.Second)
	row := awaitWallDecision(t, work.journal, 4*time.Second)
	_, budgetWords := (Budget{Wall: 2 * time.Second, SpentWall: 2 * time.Second}).Exhausted()
	if row.Decision != string(DecideStop) {
		t.Fatalf("the wall's decision is %q, want stop", row.Decision)
	}
	if !strings.Contains(row.Reason, budgetWords) {
		t.Fatalf("the wall's reason is %q, want the budget's own %q", row.Reason, budgetWords)
	}
	waitSignal(t, work.settled, "the work under the wall to settle")
}

func TestTheWallStopsTheWorkAndKeepsWhatItDid(t *testing.T) {
	work := newWallWork(t, time.Hour)
	spendWall(t, work.agent)
	if !work.agent.endRunOnTheWall() {
		t.Fatal("the spent wall did not take its ending")
	}
	waitSignal(t, work.settled, "the stopped work to settle")
	if state := work.node.stateNow(); state == TaskRunning || state == TaskQueued {
		t.Fatalf("the stopped work still reads %q", state)
	}
	report, changed, branch, merge := work.node.leavings()
	if branch != work.branch || merge != mergeAborted {
		t.Fatalf("the work landed on branch %q with merge %q", branch, merge)
	}
	if !strings.Contains(report, "stopped") || len(changed) != 1 {
		t.Fatalf("the stopped row lost what the worker landed: report %q, changed %v", report, changed)
	}
	work.node.graph.mu.Lock()
	worktree := work.node.worktree
	work.node.graph.mu.Unlock()
	if worktree != work.worktree {
		t.Fatalf("the working copy moved from %q to %q", work.worktree, worktree)
	}
	if _, err := os.Stat(filepath.Join(work.worktree, "kept.txt")); err != nil {
		t.Fatalf("the work in the task's working copy was not kept: %v", err)
	}
}

func TestTheWallSaysOneLineOnTheStandingLane(t *testing.T) {
	work := newWallWork(t, time.Hour)
	lane, stop := work.agent.WatchWakes()
	defer stop()
	spendWall(t, work.agent)
	if !work.agent.endRunOnTheWall() {
		t.Fatal("the spent wall did not take its ending")
	}
	events := collect(t, awaitWallStream(t, lane, time.Second))
	if len(events) != 1 || events[0].Kind != EventNotice {
		t.Fatalf("the wall stream carried %v, want one notice", kinds(events))
	}
	if !strings.HasPrefix(events[0].Text, checkpointStoppedNote) ||
		!strings.Contains(events[0].Text, stopStoppedMovingTail) {
		t.Fatalf("the wall's line reads %q", events[0].Text)
	}
	select {
	case extra := <-lane:
		t.Fatalf("the wall put a second stream on the standing lane: %v", extra)
	case <-time.After(20 * time.Millisecond):
	}
	if calls := work.completer.requests(); calls != 0 {
		t.Fatalf("the wall started %d model calls", calls)
	}
	waitSignal(t, work.settled, "the stopped work to settle")
}

func TestASpentRunStartsNoTurnNobodyAskedFor(t *testing.T) {
	work := newWallWork(t, time.Hour)
	spendWall(t, work.agent)
	work.agent.mu.Lock()
	started := work.agent.wakeLocked()
	work.agent.mu.Unlock()
	if started {
		t.Fatal("an exhausted wall started a turn nobody asked for")
	}
	if calls := work.completer.requests(); calls != 0 {
		t.Fatalf("the exhausted wall made %d model calls", calls)
	}
	work.agent.endRunOnTheWall()
	waitSignal(t, work.settled, "the stopped work to settle")
}

func TestAWallWithNothingOutWritesNothing(t *testing.T) {
	journal := writeableJournal(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Unattended = true
		config.Budget = Budget{Wall: 20 * time.Millisecond}
		config.SessionFile = journal
	})
	lane, stop := agent.WatchWakes()
	defer stop()
	time.Sleep(wallSettleTick + 100*time.Millisecond)
	if rows := wallDecisions(t, journal); len(rows) != 0 {
		t.Fatalf("a wall with nothing moving wrote decisions: %+v", rows)
	}
	if strings.Contains(transcriptText(agent), checkpointStoppedNote) {
		t.Fatal("a wall with nothing moving wrote a stopping line")
	}
	select {
	case stream := <-lane:
		t.Fatalf("a wall with nothing moving announced a stream: %v", stream)
	default:
	}
}

func TestAPersonsSessionHasNoWallReader(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Budget = Budget{Wall: time.Hour}
	})
	agent.mu.Lock()
	armed := agent.wallStop != nil
	agent.mu.Unlock()
	if armed {
		t.Fatal("a person's session armed a wall reader")
	}
}

func TestACeilingWithNoWallArmsNothing(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Unattended = true
		config.Budget = Budget{USD: 10}
	})
	agent.mu.Lock()
	armed := agent.wallStop != nil
	agent.mu.Unlock()
	if armed {
		t.Fatal("a money-only ceiling armed a wall reader")
	}
}

func TestTheWallDoesNotSealATurnThatIsSpeaking(t *testing.T) {
	work := newWallWork(t, 200*time.Millisecond)
	lane, stop := work.agent.WatchWakes()
	defer stop()
	work.agent.mu.Lock()
	work.agent.running = true
	work.agent.mu.Unlock()
	time.Sleep(wallSettleTick + 300*time.Millisecond)
	if rows := wallDecisions(t, work.journal); len(rows) != 0 {
		t.Fatalf("the wall wrote an ending over a speaking turn: %+v", rows)
	}
	select {
	case stream := <-lane:
		t.Fatalf("the wall spoke over the running turn: %v", stream)
	default:
	}
	work.agent.mu.Lock()
	work.agent.running = false
	work.agent.mu.Unlock()
	events := collect(t, awaitWallStream(t, lane, 2*wallSettleTick))
	if len(events) != 1 || events[0].Kind != EventNotice {
		t.Fatalf("the ending after the turn carried %v", kinds(events))
	}
	waitSignal(t, work.settled, "the work to stop after the speaking turn")
}

func TestTheWallEndsARunOnce(t *testing.T) {
	work := newWallWork(t, time.Hour)
	spendWall(t, work.agent)
	for range 3 {
		if !work.agent.endRunOnTheWall() {
			t.Fatal("a taken wall ending asked to be polled again")
		}
	}
	waitSignal(t, work.settled, "the stopped work to settle")
	if rows := wallDecisions(t, work.journal); len(rows) != 1 {
		t.Fatalf("the wall wrote %d endings, want one: %+v", len(rows), rows)
	}
}

func TestATaskUnderAWallInheritsWhatIsLeftOfIt(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Unattended = true
		config.Budget = Budget{Wall: 2 * time.Hour}
	})
	node := &TaskNode{graph: newTaskGraph()}
	steward := agent.steward()
	steward.mu.Lock()
	started := steward.started
	steward.now = func() time.Time { return started.Add(90 * time.Minute) }
	steward.mu.Unlock()
	if got := agent.taskLimits(node).deadline; got != 30*time.Minute {
		t.Fatalf("the task got %s, want the 30 minutes left of the wall", got)
	}
	steward.mu.Lock()
	steward.now = func() time.Time { return started.Add(2*time.Hour - taskAllowance/2) }
	steward.mu.Unlock()
	if got := agent.taskLimits(node).deadline; got != taskAllowance {
		t.Fatalf("the task got %s inside the wall margin, want the %s allowance", got, taskAllowance)
	}

	noWall, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Unattended = true
		config.Budget = Budget{USD: 10}
	})
	if got := noWall.taskLimits(node).deadline; got != taskDeadline {
		t.Fatalf("a task with no wall got %s, want %s", got, taskDeadline)
	}
}
