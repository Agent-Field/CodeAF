package workspacestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGetKeepsCommittedJournalAcrossConcurrentRecovery(t *testing.T) {
	dir := t.TempDir()
	writer := open(t, dir)
	request, _, _ := moveSetup(t, writer, "read-race")
	crashRun(t, writer, "after-destination", request)
	writer.pairFault = nil
	reader := open(t, dir)
	called := false
	reader.afterGetRead = func() {
		called = true
		result, err := writer.PutPair(request)
		if err != nil || !result.Already {
			t.Fatal(result, err)
		}
		if _, err := os.Stat(writer.journalPath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("recovery did not remove journal", err)
		}
	}
	record, err := reader.Get(keyA)
	if !called {
		t.Fatal("read/recovery seam never ran")
	}
	if err != nil || record.Revision != 2 || record.MovedTo["mv"] != keyB {
		t.Fatal("postcommit read returned stale half", record.Revision, record.MovedTo, err)
	}
}

func TestPublishedJournalCannotRollbackWhenDirectorySyncFails(t *testing.T) {
	dir := t.TempDir()
	writer := open(t, dir)
	request, _, _ := moveSetup(t, writer, "sync-race")
	reader := open(t, dir)
	observed := false
	writer.syncDir = func(string) error {
		a, errA := reader.Get(keyA)
		b, errB := reader.Get(keyB)
		if errA != nil || errB != nil || a.Revision != 2 || b.Revision != 2 {
			t.Fatal("published journal was not visible", a, b, errA, errB)
		}
		observed = true
		return errors.New("directory sync failed")
	}
	result, err := writer.PutPair(request)
	if !observed {
		t.Fatal("read did not run after public commit")
	}
	if err != nil || result.Source.Revision != 2 || result.Destination.Revision != 2 {
		t.Fatal("a published commit was reported undone", result, err)
	}
	a, _ := reader.Get(keyA)
	b, _ := reader.Get(keyB)
	if a.Revision != 2 || b.Revision != 2 {
		t.Fatal("visible commit rolled back", a.Revision, b.Revision)
	}
}

func TestWorkspaceSignatureIncludesPendingLogicalPublication(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	request, _, _ := moveSetup(t, s, "signature-pair")
	before, err := s.Signature(keyA)
	if err != nil {
		t.Fatal(err)
	}
	crashRun(t, s, "after-journal", request)
	after, err := s.Signature(keyA)
	if err != nil || after == before {
		t.Fatal("logical journal publication did not invalidate signature", before, after, err)
	}
	if err := os.Remove(s.path(keyB)); err != nil {
		t.Fatal(err)
	}
	missing, err := s.Signature(keyB)
	if err != nil || missing == (FileSignature{}) {
		t.Fatal("missing physical workspace hid committed overlay", missing, err)
	}
	if record, err := s.Get(keyB); err != nil || record.Revision != 2 {
		t.Fatal(record, err)
	}
}

func TestTransferHintsCoverAllValidatedSplitIdentities(t *testing.T) {
	s := open(t, t.TempDir())
	moving := []tabSpec{}
	for i := 0; i < 200; i++ {
		moving = append(moving, tabSpec{id: fmt.Sprintf("tab-%03d", i), panes: []string{fmt.Sprintf("left-%03d", i), fmt.Sprintf("right-%03d", i)}})
	}
	source := pdoc(append([]tabSpec{{id: "keep"}}, moving...))
	destination := pdoc(tabs("home"))
	if _, err := s.Put(keyA, 0, "writer", source); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(keyB, 0, "writer", destination); err != nil {
		t.Fatal(err)
	}
	next := pdoc(append([]tabSpec{{id: "home"}}, moving...))
	result, err := s.PutPair(PairRequest{Intent: "many-splits", Writer: "writer", Source: keyA, Destination: keyB, SourceRevision: 1, DestinationRevision: 1, SourceWorkspace: pdoc(tabs("keep")), DestinationWorkspace: next})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Source.MovedTo) != 600 {
		t.Fatal("valid split identities were silently omitted", len(result.Source.MovedTo))
	}
	for _, tab := range moving {
		for _, id := range append([]string{tab.id}, tab.panes...) {
			if result.Source.MovedTo[id] != keyB {
				t.Fatal("missing relocation", id)
			}
		}
	}
}

func TestPairRefusesOversizedPersistedFormBeforePublication(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	request, _, _ := moveSetup(t, s, "encoded-size")
	// JSON clients send '<' literally; Go's default storage encoder expands it.
	raw := pdoc([]tabSpec{{id: "mv", draft: strings.Repeat("<", 45000)}})
	raw = bytes.ReplaceAll(raw, []byte(`\u003c`), []byte("<"))
	if err := Validate(raw); err != nil {
		t.Fatal("raw input should be valid", err)
	}
	request.DestinationWorkspace = raw
	before := snapshot(t, dir)
	if _, err := s.PutPair(request); !errors.Is(err, ErrTooLarge) {
		t.Fatal("unreadable persisted form was not refused before commit", err)
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatal("refused encoding published a journal or records")
	}
}

// Two OS processes exercise the advisory pair lock, not just distinct Store
// objects sharing one test process. Neither may overwrite the other's winner.
func TestPairAndOrdinaryWriterAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	request, _, _ := moveSetup(t, s, "process-pair")
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "request.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	commands := []*exec.Cmd{}
	outputs := []*bytes.Buffer{}
	for _, mode := range []string{"move", "write"} {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPairReviewSubprocessHelper$")
		cmd.Env = append(os.Environ(), "CODEAF_PAIR_REVIEW_DIR="+dir, "CODEAF_PAIR_REVIEW_MODE="+mode)
		out := &bytes.Buffer{}
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
		outputs = append(outputs, out)
	}
	for _, mode := range []string{"move", "write"} {
		for {
			if _, err := os.Stat(filepath.Join(dir, "ready-"+mode)); err == nil {
				break
			}
			if ctx.Err() != nil {
				t.Fatal("child did not reach lock race", ctx.Err())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err, outputs[i].String())
		}
	}
	pairWon := strings.Contains(outputs[0].String(), "PAIR_REVIEW_WIN")
	putWon := strings.Contains(outputs[1].String(), "PAIR_REVIEW_WIN")
	if pairWon == putWon {
		t.Fatal("exactly one process must win from revision1", outputs[0].String(), outputs[1].String())
	}
	a, errA := s.Get(keyA)
	b, errB := s.Get(keyB)
	if errA != nil || errB != nil {
		t.Fatal(errA, errB)
	}
	hasA := strings.Contains(string(a.Workspace), `"mv"`)
	hasB := strings.Contains(string(b.Workspace), `"mv"`)
	if pairWon && (hasA || !hasB) || putWon && (!hasA || hasB || !strings.Contains(string(a.Workspace), `"typed"`)) {
		t.Fatal("process winner was lost", pairWon, putWon, a, b)
	}
}

func TestPairReviewSubprocessHelper(t *testing.T) {
	dir, mode := os.Getenv("CODEAF_PAIR_REVIEW_DIR"), os.Getenv("CODEAF_PAIR_REVIEW_MODE")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	data, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request PairRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	s := open(t, dir)
	record, err := s.Get(keyA)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(record.Workspace, &doc); err != nil {
		t.Fatal(err)
	}
	doc["tabs"] = append(doc["tabs"].([]any), map[string]any{"id": "typed", "title": "Typed", "draft": "later words", "kind": "conversation", "pinned": false})
	typed, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ready-"+mode), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("race gate timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if mode == "move" {
		_, err = s.PutPair(request)
	} else if mode == "write" {
		_, err = s.Put(keyA, 1, "other-process", typed)
	} else {
		t.Fatal("unknown helper mode")
	}
	if err == nil {
		fmt.Println("PAIR_REVIEW_WIN")
		return
	}
	var pairConflict *PairConflictError
	var conflict *ConflictError
	if !errors.As(err, &pairConflict) && !errors.As(err, &conflict) {
		t.Fatal(err)
	}
	fmt.Println("PAIR_REVIEW_CONFLICT")
}

func TestMissingWorkspaceSignatureIgnoresCompletionOnlyLedger(t *testing.T) {
	s := open(t, t.TempDir())
	request, _, _ := moveSetup(t, s, "completed-signature")
	if _, err := s.PutPair(request); err != nil {
		t.Fatal(err)
	}
	if s.PairStamp() == "-/-" {
		t.Fatal("completed transfer must have ledger")
	}
	if signature, err := s.Signature(keyC); err != nil || signature != (FileSignature{}) {
		t.Fatal("ledger caused absent workspace reads", signature, err)
	}
}
