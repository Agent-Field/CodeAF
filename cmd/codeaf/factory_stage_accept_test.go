package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

func unresolvedIn(notes []string) []string {
	var out []string
	for _, n := range notes {
		if rest, ok := strings.CutPrefix(n, verification.FailurePrefix); ok {
			out = append(out, strings.TrimSpace(rest))
		}
	}
	return out
}

// What the owner accepted on an earlier step is not held against a later one:
// the same check failing in the same words. Any other failure still is.
func TestAnAcceptedFailureDoesNotBlockALaterStep(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	door := &receiptStageDoor{requireFresh: true}
	c := stageConversation{file: file, dir: root, verification: door}
	run("CHECK_EXIT=1 cargo test --test cli regression")
	notes, _, key := c.VerificationEvidence()
	blocked := unresolvedIn(notes)
	if len(blocked) != 1 || key == "" {
		t.Fatalf("fixture: unresolved %q, key %q", blocked, key)
	}

	door.accepted = blocked
	notes, _, key = c.VerificationEvidence()
	if left := unresolvedIn(notes); len(left) != 0 || key != "" {
		t.Fatalf("the accepted failure still blocks: %q, key %q", left, key)
	}
	if !strings.Contains(strings.Join(notes, "\n"), `"accepted":true`) {
		t.Fatalf("the receipt does not say accepted:\n%s", strings.Join(notes, "\n"))
	}

	// A different failing check is new, and blocks.
	run("CHECK_EXIT=1 cargo test --test cli other")
	notes, _, key = c.VerificationEvidence()
	if left := unresolvedIn(notes); len(left) != 1 || left[0] == blocked[0] || key == "" {
		t.Fatalf("a new failure was waived with the accepted one: %q", left)
	}
}

// The factory's own comparison clears a failing Go test the base already
// fails, and the step's report says so; a measurement of another checkout
// clears nothing.
func TestAFailureTheFactoryFoundOnTheBaseDoesNotBlock(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\necho 'FAIL example.com/p'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSpace(gitT(t, root, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(root, "code.txt"), []byte("the change"), 0600); err != nil {
		t.Fatal(err)
	}
	run("export PATH=" + repairShellQuote(bin) + ":\"$PATH\"; go test ./... -count=1")
	c := stageConversation{file: file, dir: root, verification: &receiptStageDoor{requireFresh: true, baselineCommit: base}}
	notes, _, _ := c.VerificationEvidence()
	if left := unresolvedIn(notes); len(left) == 0 {
		t.Fatalf("fixture: the failing go test does not block:\n%s", strings.Join(notes, "\n"))
	}

	state, err := verification.SnapshotAt(root)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := verification.ReadIn(file, root)
	if err != nil {
		t.Fatal(err)
	}
	latest, _ := verification.RequiredChecks(checks, state, true)
	stream := func(action, test, output string) string {
		raw, _ := json.Marshal(struct{ Action, Package, Test, Output string }{action, "example.com/p", test, output})
		return string(raw)
	}
	failing := strings.Join([]string{
		stream("start", "", ""), stream("run", "TestOld", ""), stream("output", "TestOld", "    p_test.go:3: wrong\n"),
		stream("fail", "TestOld", ""), stream("output", "", "FAIL\n"), stream("fail", "", ""),
	}, "\n")
	fake := func(context.Context, string, []string, []string) (string, int, error) { return failing, 1, nil }
	if n, err := verification.MeasureBase(context.Background(), file, root, latest, state, base, fake); err != nil || n != 1 {
		t.Fatalf("cleared %d: %v", n, err)
	}
	notes, _, key := c.VerificationEvidence()
	if left := unresolvedIn(notes); len(left) != 0 || key != "" {
		t.Fatalf("a failure found on the base still blocks: %q", left)
	}
	if !strings.Contains(strings.Join(notes, "\n"), verification.BaselineNotePrefix+base) {
		t.Fatalf("the report does not say the failure is on the base:\n%s", strings.Join(notes, "\n"))
	}

	// The change moves on: that measurement is of another checkout.
	if err := os.WriteFile(filepath.Join(root, "code.txt"), []byte("a later change"), 0600); err != nil {
		t.Fatal(err)
	}
	notes, _, _ = c.VerificationEvidence()
	if left := unresolvedIn(notes); len(left) == 0 {
		t.Fatal("a measurement of an earlier checkout cleared this one")
	}
}
