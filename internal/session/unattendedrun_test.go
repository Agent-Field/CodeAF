package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestAnUnattendedRunIsStillGoingWhileTheWorkItHandedOverIsMoving(t *testing.T) {
	var landed atomic.Bool
	completer := &scriptedCompleter{steps: unattendedWritingSteps(24, &landed)}
	agent, _ := writeSeamAgent(t, completer, func(config *Config) {
		config.Unattended = true
		config.Budget = Budget{Wall: time.Hour}
	})
	wakes, stopWakes := agent.WatchWakes()
	t.Cleanup(stopWakes)

	ran := make(ranNodes, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		<-release
		landed.Store(true)
		node.finish("the parser rename is complete", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	collect(t, mustSubmit(t, agent, "rename the parser and fix everything that calls it"))
	node := ran.await(t)
	if state := node.stateNow(); state != TaskRunning {
		t.Fatalf("the handed-over work is %q, want %q", state, TaskRunning)
	}
	if !agent.StillGoing() {
		t.Fatal("the unattended run said it was over while its handed-over work was moving")
	}

	releaseOnce.Do(func() { close(release) })
	var woken <-chan Event
	select {
	case woken = <-wakes:
	case <-time.After(5 * time.Second):
		t.Fatal("the landing did not wake a turn")
	}
	collect(t, woken)
	graph.mu.Lock()
	flight := graph.flightLocked()
	graph.mu.Unlock()
	if len(flight.moving) != 0 {
		t.Fatalf("the woken turn ended with work still moving: %v", flight.moving)
	}
	if agent.StillGoing() {
		t.Fatal("the unattended run still said it was going after the landing was read and the woken turn ended")
	}
}

func TestOnlyAnUnattendedRunWithABudgetCanStillBeGoing(t *testing.T) {
	person, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	person.graph()
	if person.StillGoing() {
		t.Fatal("a person's session said it had an unattended run to wait for")
	}

	withoutBudget, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Unattended = true
	})
	withoutBudget.graph()
	if withoutBudget.StillGoing() {
		t.Fatal("a --yolo run with no budget said it had an unattended run to wait for")
	}
}

// A landing's note is queued before the turn it wakes exists, so it counts as a
// run still going — until the wall has passed, when no turn will ever be started
// to answer it and the door would otherwise wait for a reply that is not coming.
func TestALandingNoteIsARunStillGoingUntilTheWallHasPassed(t *testing.T) {
	waiting, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Unattended = true
		config.Budget = Budget{Wall: time.Hour}
	})
	waiting.graph()
	waiting.mu.Lock()
	waiting.steering = append(waiting.steering, jobNote("task 1 has landed"))
	waiting.mu.Unlock()
	if !waiting.StillGoing() {
		t.Fatal("a landing waiting for the reply it wakes read as a run that was over")
	}

	spent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Unattended = true
		config.Budget = Budget{Wall: time.Millisecond}
	})
	spent.graph()
	spent.mu.Lock()
	spent.steering = append(spent.steering, jobNote("task 1 has landed"))
	spent.mu.Unlock()
	// The wall is read off the Steward's own clock, so this is the wall passing
	// and not a timer this test owns.
	time.Sleep(20 * time.Millisecond)
	if spent.StillGoing() {
		t.Fatal("a landing note nothing will ever answer kept the run going past its wall")
	}
}

// unattendedWritingSteps is [writingSteps] with work left at the first ending,
// so the goal owner actually moves the turn instead of finding it done. Once
// the task lands, the woken turn reads that landing and finds nothing left.
func unattendedWritingSteps(count int, landed *atomic.Bool) []step {
	steps := make([]step, count)
	for index := range steps {
		round := index
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			switch {
			case askedForSketch(messages):
				return textResponse(checkpointChainSketch), nil
			case askedForHandoff(messages):
				return textResponse("Finish the parser rename and every caller."), nil
			case askedToWriteHandoff(messages):
				return toolResponse("no-writer", "ls", `{"path":"."}`), nil
			case askedForRemains(messages):
				if landed.Load() {
					return textResponse(checkpointNothingLeft), nil
				}
				return textResponse("The parser rename and its callers are still left."), nil
			case landed.Load():
				return textResponse("The handed-over parser rename has landed."), nil
			default:
				arguments, _ := json.Marshal(struct {
					Path string `json:"path"`
					Text string `json:"content"`
				}{Path: fmt.Sprintf("file%d.txt", round), Text: fmt.Sprintf("round %d\n", round)})
				return toolResponseWithText(fmt.Sprintf("write-%d", round), "write", string(arguments),
					"Writing the next file."), nil
			}
		}
	}
	return steps
}
