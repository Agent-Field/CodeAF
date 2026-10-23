package run

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestManagerStatusQuestionsNeverBecomeWorkerInstructions(t *testing.T) {
	talk := newFakeTalk()
	started := make(chan Job, 1)
	writeNotes := make(chan []string, 1)
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	exec := ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		if job.Stage.Name == "plan" {
			started <- job
			select {
			case <-release:
			case <-ctx.Done():
				return factory.StageResult{}, ctx.Err()
			}
		} else {
			writeNotes <- append([]string(nil), job.Notes...)
		}
		return done("finished"), nil
	})
	g, _ := managed(t, map[factory.StageKind]Executor{factory.StageChat: exec}, talk, time.Hour)
	id := g.add("fix the ledger", chat("plan"), chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	var job Job
	select {
	case job = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Plan never started")
	}
	lp := g.r.loop()
	ctl := lp.ctl(id)
	for _, question := range []string{"Everything ok here?", "Any updates?", "How is the plan coming along?"} {
		talk.typed(job.Item.Talk, question)
		lp.hear(ctl)
		// The explicit steering door must respect the same distinction too.
		if err := g.r.Steer(id, question); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case words := <-job.Steer:
		t.Fatalf("status question reached the active worker: %q", words)
	default:
	}
	const instruction = "Use the existing helper"
	talk.typed(job.Item.Talk, instruction)
	lp.hear(ctl)
	lp.hear(ctl)
	select {
	case words := <-job.Steer:
		if words != instruction {
			t.Fatalf("worker received %q", words)
		}
	default:
		t.Fatal("real steering never reached the active worker")
	}
	select {
	case words := <-job.Steer:
		t.Fatalf("the same instruction reached the worker twice: %q", words)
	default:
	}
	finish()
	it := g.waitState(id, factory.StateLanded)
	if notes := <-writeNotes; len(notes) != 1 || notes[0] != instruction {
		t.Fatalf("future stage instructions contain status questions: %q", notes)
	}
	if it.Heard.IsZero() || !logHas(it, "steer: "+instruction) {
		t.Fatal("real steering lost its consumption mark or internal audit")
	}
	for _, line := range talk.lines(it.Talk) {
		if strings.HasPrefix(line, "steer: ") {
			t.Fatal("runner repeated steering in the conversation")
		}
	}
}

func TestStatusQuestionsBeforeLaunchNeverBecomeTheStageBrief(t *testing.T) {
	talk := newFakeTalk()
	var notes []string
	g, _ := managed(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
		notes = append([]string(nil), job.Notes...)
		return done("done"), nil
	})}, talk, time.Hour)
	id := g.add("fix it", chat("plan"))
	transcript := "manager-" + strconv.Itoa(id)
	talk.typed(transcript, "Everything ok here?")
	talk.typed(transcript, "Keep the API compatible")
	talk.typed(transcript, "Any updates?")
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if len(it.Notes) != 1 || it.Notes[0] != "Keep the API compatible" || len(notes) != 1 || notes[0] != it.Notes[0] {
		t.Fatalf("read-only check-ins became the launch brief: item=%q stage=%q", it.Notes, notes)
	}
	if it.Heard.IsZero() {
		t.Fatal("ignored status questions were left pending for a later poll")
	}
}
