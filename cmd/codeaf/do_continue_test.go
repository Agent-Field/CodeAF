package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAssignmentOfPeelsExactlyOneWrapperAtATime(t *testing.T) {
	first := continuationBrief(priorRun{assignment: "add a flag", ending: "it was stopped"}, "")
	second := continuationBrief(priorRun{assignment: assignmentOf(first), ending: "it finished"}, "also a test")
	if got := assignmentOf(second); got != "add a flag" {
		t.Fatalf("assignment after two rounds = %q", got)
	}
	if strings.Count(second, continuePreamble) != 1 {
		t.Fatalf("a continuation of a continuation nested:\n%s", second)
	}
}

func TestAssignmentOfLeavesAPreambleInsideAnAssignmentAlone(t *testing.T) {
	own := "explain this sentence: " + continuePreamble + "then stop"
	if got := assignmentOf(own); got != own {
		t.Fatalf("a preamble inside an assignment was peeled: %q", got)
	}
}

func TestContinuationBriefWritesOnlyTheBlocksItHas(t *testing.T) {
	bare := continuationBrief(priorRun{assignment: "do x", ending: "it finished"}, "  ")
	for _, header := range []string{continueReachedHeader, continueAskedHeader} {
		if strings.Contains(bare, header) {
			t.Fatalf("empty block %q was written:\n%s", header, bare)
		}
	}
	full := continuationBrief(priorRun{assignment: "do x", ending: "it finished", reached: []string{"- a [done]"}}, "also y")
	for _, want := range []string{"do x", continueEndingHeader + "it finished.", "- a [done]", continueAskedHeader + "\nalso y"} {
		if !strings.Contains(full, want) {
			t.Fatalf("brief lacks %q:\n%s", want, full)
		}
	}
}

func TestPersonsWordsNeverEnterTheAssignment(t *testing.T) {
	brief := continuationBrief(priorRun{assignment: "do x", ending: "it finished"}, "also y")
	if got := assignmentOf(brief); got != "do x" {
		t.Fatalf("assignment = %q", got)
	}
}

func makeRecord(t *testing.T, home, name, brief string) string {
	t.Helper()
	folder := filepath.Join(home, "runs", recordPrefix+name)
	store, err := session.OpenRunPlanAt(filepath.Join(folder, "plandb.db"), "title", brief)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EndRoot("limit"); err != nil {
		t.Fatal(err)
	}
	return folder
}

func TestResolveRecordByIdPrefixAndPath(t *testing.T) {
	home := beltRunEnv(t)
	one := makeRecord(t, home, "1234567", "a")
	two := makeRecord(t, home, "1299999", "b")
	for id, want := range map[string]string{"1234567": one, recordPrefix + "1234567": one, "1234": one, "1299": two, one: one} {
		if got, err := resolveRecord(id); err != nil || got != want {
			t.Fatalf("resolveRecord(%q) = %q, %v; want %q", id, got, err, want)
		}
	}
	for _, id := range []string{"12", "77", ""} {
		if got, err := resolveRecord(id); err == nil {
			t.Fatalf("resolveRecord(%q) = %q, want a refusal", id, got)
		}
	}
}

func TestReadPriorRunReadsAssignmentAndEnding(t *testing.T) {
	home := beltRunEnv(t)
	folder := makeRecord(t, home, "42", "rename the logger")
	prior, err := readPriorRun(folder)
	if err != nil {
		t.Fatal(err)
	}
	if prior.assignment != "rename the logger" || prior.ending != endingWords["failed"] {
		t.Fatalf("prior = %+v", prior)
	}
	if _, err := readPriorRun(t.TempDir()); err == nil {
		t.Fatal("a folder with no record was read")
	}
}

// seenBriefs records every prompt a worker was handed, and answers as inner.
type seenBriefs struct {
	inner session.Completer
	mu    sync.Mutex
	docs  []string
}

func (s *seenBriefs) CompleteWithMessages(ctx context.Context, msgs []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	s.mu.Lock()
	s.docs = append(s.docs, beltDocument(msgs))
	s.mu.Unlock()
	return s.inner.CompleteWithMessages(ctx, msgs, opts...)
}

func (s *seenBriefs) sawAll(words ...string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, doc := range s.docs {
		found := true
		for _, word := range words {
			found = found && strings.Contains(doc, word)
		}
		if found {
			return true
		}
	}
	return false
}

func TestContinueCarriesAFailedRunToDone(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)
	failing := &beltSeat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("scripted worker failure")
	}}
	var stdout, stderr strings.Builder
	_ = doErrand(doRequest{task: "write out.txt", workspace: workspace, asJSON: true, timeout: 5 * time.Second,
		stdout: &stdout, stderr: &stderr, newBeltCompleter: func(string) session.Completer { return failing }})
	folder := keptRunFolder(t, stderr.String())
	if !strings.Contains(stderr.String(), continueHint(folder)) {
		t.Fatalf("stderr does not say how to continue:\n%s", stderr.String())
	}

	brief, assignment, err := continuedBrief(recordID(folder), "and say so")
	if err != nil || assignment != "write out.txt" {
		t.Fatalf("continuedBrief = %q, %q, %v", brief, assignment, err)
	}
	seat := &seenBriefs{inner: finishingSeat(0)}
	stdout.Reset()
	stderr.Reset()
	err = doErrand(doRequest{task: brief, assignment: assignment, workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat }})
	if err != nil {
		t.Fatalf("continued run: %v\n%s\n%s", err, stderr.String(), stdout.String())
	}
	if stop := doEnvelopeFields(t, stdout.String())["stop"]; stop != "done" {
		t.Fatalf("continued run stop = %v", stop)
	}
	if !seat.sawAll("write out.txt", continueEndingHeader, "and say so") {
		t.Fatalf("no worker was handed the assignment, the ending and the finding:\n%v", seat.docs)
	}
	if _, err := os.Stat(filepath.Join(workspace, "out.txt")); err != nil {
		t.Fatalf("continued run left no file: %v", err)
	}
}

func TestContinueRefusesAnUnknownRun(t *testing.T) {
	beltRunEnv(t)
	if _, _, err := continuedBrief("nope", ""); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("unknown id error = %v", err)
	}
}
