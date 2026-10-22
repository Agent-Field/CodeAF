package main

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagerOrdinaryTurnAnswersPersistedPlanThroughItsOwnDoor(t *testing.T) {
	st, web, _ := talkLab(t)
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	id, err := st.Add(context.Background(), factory.Item{Repo: "web", Title: "merge both sources", Talk: file})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Update(id, func(it *factory.Item) error {
		it.State = factory.StateNeedsYou
		it.Question, it.QKind = "Merge both or restrict the form?", "step"
		it.Asking = &factory.Asked{Stage: "plan", Question: it.Question, At: time.Now(), With: factory.AskedYou}
		it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseRunning, Round: 1, Chat: "existing-plan"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	door, ok := runDoor(st, web, "").(session.ManagerStatusDoor)
	if !ok {
		t.Fatal("ordinary manager has no durable status/answer door")
	}
	before, _ := st.Revision(id)
	status, err := door.ManagerStatus(context.Background(), file)
	after, _ := st.Revision(id)
	if err != nil || status.QuestionID == "" || !strings.Contains(status.Text, "No step work is running") || before != after {
		t.Fatalf("status mutated the job or lost its question: %s %v", status.Text, err)
	}
	if _, err = door.AnswerPending(context.Background(), filepath.Join(t.TempDir(), "other.jsonl"), status.QuestionID, "Merge both"); err == nil {
		t.Fatal("another conversation answered the job")
	}
	if _, err = door.AnswerPending(context.Background(), file, status.QuestionID, "Merge both; the person said to choose the best option."); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(id)
	if got.State != factory.StateQueued || got.Question != "" || got.Stream.Phases[0].Chat != "existing-plan" || len(got.Stream.Phases[0].Carry) != 1 {
		t.Fatal("saved decision did not preserve the drafted plan")
	}
}
