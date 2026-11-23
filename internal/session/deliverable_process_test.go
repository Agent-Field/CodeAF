package session

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

type deliverableProcessFixture struct {
	Index    string
	Artifact Artifact
}

// The desktop keeps locally while a separate host records the same index.
// Pipes and hooks put the host's overwrite after Keep's lookup, with no sleeps.
func TestKeepPreservesOverwriteAcrossProcesses(t *testing.T) {
	agent, workspace, index := deliverableAgent(t, true)
	original := wavFile(1, 8, 4, 4)
	writeNewFile(t, agent, "voice.wav", string(original))
	base := listDeliverables(t, index, DeliverableQuery{}, 10)[0]
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestDeliverableOverwriteProcess$", "-test.count=1", "-test.timeout=30s")
	child.Env = append(os.Environ(), "CODEAF_DELIVERABLE_PROCESS_TEST=1")
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	child.Stderr = &stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	if err := json.NewEncoder(input).Encode(deliverableProcessFixture{index, base.Artifact}); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(output)
	expect := func(want string) {
		t.Helper()
		if !scanner.Scan() || scanner.Text() != want {
			var rest []string
			rest = append(rest, scanner.Text())
			for scanner.Scan() {
				rest = append(rest, scanner.Text())
			}
			t.Fatalf("host protocol: want %q, got %q (%v)", want, rest, scanner.Err())
		}
	}
	expect("ready")
	old := deliverableAfterLookup
	deliverableAfterLookup = func() {
		if _, err := fmt.Fprintln(input, "overwrite"); err != nil {
			t.Fatal(err)
		}
		if !scanner.Scan() {
			t.Fatalf("host stopped before overwrite: %v", scanner.Err())
		}
		switch scanner.Text() {
		case "blocked": // The fixed host must wait until Keep appends.
		case "unlocked":
			expect("recorded") // Before the fix, B appends before Keep's stale A.
		default:
			t.Fatalf("unexpected host state: %q", scanner.Text())
		}
	}
	t.Cleanup(func() { deliverableAfterLookup = old })
	kept, path, err := KeepDeliverable(t.Context(), index, base, []string{workspace}, t.TempDir(), KeepRequest{Dir: t.TempDir()})
	deliverableAfterLookup = old
	if err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	for scanner.Scan() { // Drain the child's completion and test-runner output.
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("host overwrite: %v\n%s", err, stderr.String())
	}
	latest, err := FindDeliverable(index, agent.journalID(), base.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	current := wavFile(1, 8, 8, 8)
	wantHash := fmt.Sprintf("%x", sha256.Sum256(current))
	if latest.Fact.File.SHA256 != wantHash || latest.Fact.File.Bytes != int64(len(current)) || latest.Fact.Version != "sha256:"+wantHash || latest.Fact.Run == base.Fact.Run {
		t.Fatalf("Keep restored stale source metadata: got %+v, want hash %s and fresh attribution", latest.Fact, wantHash)
	}
	if latest.Artifact.Media == nil || latest.Artifact.Media.SHA256 != wantHash || latest.Fact.File.MIME != base.Fact.File.MIME {
		t.Fatalf("Keep restored stale preview or MIME: media %+v, file %+v", latest.Artifact.Media, latest.Fact.File)
	}
	if live := ResolveDeliverable(latest, []string{workspace}); live.State != MediaAvailable {
		t.Fatalf("Keep restored stale descriptor: %s", live.State)
	}
	wantCopy := fmt.Sprintf("%x", sha256.Sum256(original))
	if latest.Fact.State != DeliverKept || latest.Fact.Kept == nil || latest.Fact.Kept.SHA256 != wantCopy || latest.Fact.Kept.Path != path || kept.Fact.Kept.SHA256 != wantCopy {
		t.Fatalf("overwrite lost Keep's copy metadata: %+v", latest.Fact)
	}
	if copied, err := os.ReadFile(path); err != nil || !bytes.Equal(copied, original) {
		t.Fatalf("kept copy changed: %x, %v", copied, err)
	}
	rows := listDeliverables(t, index, DeliverableQuery{}, 10)
	if len(rows) != 1 || rows[0].Fact.File.SHA256 != wantHash || rows[0].Fact.Kept.SHA256 != wantCopy {
		t.Fatalf("listing lost the overwrite or kept copy: %+v", rows)
	}
}

func TestDeliverableOverwriteProcess(t *testing.T) {
	if os.Getenv("CODEAF_DELIVERABLE_PROCESS_TEST") != "1" {
		t.Skip("independent deliverable store helper")
	}
	var fixture deliverableProcessFixture
	input := bufio.NewReader(os.Stdin)
	line, err := input.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(line, &fixture); err != nil {
		t.Fatal(err)
	}
	// This host has its own path cache and mutex. The recorder receives the
	// owning session's identity, just as the write observer supplies it.
	if _, err := FindDeliverable(fixture.Index, fixture.Artifact.Session, fixture.Artifact.Deliver.ID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	fmt.Println("ready")
	var command string
	if _, err := fmt.Fscan(input, &command); err != nil || command != "overwrite" {
		t.Fatalf("overwrite command: %q, %v", command, err)
	}
	writtenBeforeRecord = func() {
		file, err := os.OpenFile(fixture.Index, os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		err = filelock.Lock(file, true, true)
		switch {
		case filelock.IsBusy(err):
			fmt.Println("blocked")
		case err == nil:
			_ = filelock.Unlock(file)
			fmt.Println("unlocked")
		default:
			t.Fatal(err)
		}
	}
	current := wavFile(1, 8, 8, 8)
	artifact := fixture.Artifact
	if err := os.WriteFile(artifact.Path, current, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	file := identityOf(artifact.Path, info)
	file.SHA256 = fmt.Sprintf("%x", sha256.Sum256(current))
	ctx := withRunIdentity(t.Context())
	artifact.Deliver = &DeliverableFact{ID: NewDeliverableID(), At: time.Now().UTC(), Run: runIdentity(ctx), Authored: true, Origin: DeliverOriginAgent, State: DeliverDraft, File: file, Version: "sha256:" + file.SHA256}
	attachWrittenMedia(ctx, &artifact, file, current)
	if _, ok := recordWrittenArtifact(fixture.Index, artifact); !ok {
		t.Fatal("host overwrite was not recorded")
	}
	fmt.Println("recorded")
}
