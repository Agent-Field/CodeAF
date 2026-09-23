package session

// holder_test.go pins the move protocol between BUILDS (holder.go's
// compatibility rule): the bytes another build writes are read here, and the
// bytes this build writes are the ones another build reads. A change that makes
// any of these fail is a change to a protocol a running older window still
// speaks.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// A REQUEST AS ANOTHER BUILD WRITES IT is answered: one field, `at`, in
// takeover.json beside the journal. These are the literal bytes of the first
// build that spoke it.
func TestAMoveRequestFromAnotherBuildIsHonoured(t *testing.T) {
	dir := t.TempDir()
	at := time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	if err := os.WriteFile(filepath.Join(dir, "takeover.json"), []byte(`{"at":"`+at+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, asked := takeoverAsked(dir, time.Now()); !asked {
		t.Fatal("a request in the frozen shape was not seen")
	}
}

// AND THIS BUILD WRITES EXACTLY THAT SHAPE, so an older holder reads it.
func TestThisBuildsMoveRequestIsTheFrozenShape(t *testing.T) {
	dir := t.TempDir()
	if err := AskTakeover(dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "takeover.json"))
	if err != nil {
		t.Fatalf("the request is not at the frozen name: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	if !slices.Equal(keys, []string{"at"}) {
		t.Fatalf("the request carries %v, want exactly [at]", keys)
	}
	if _, err := time.Parse(time.RFC3339Nano, fields["at"].(string)); err != nil {
		t.Fatalf("`at` is not an RFC 3339 time: %v", err)
	}
}

// THE HOLDER'S RECORD, as an older build writes it, names the window: the four
// frozen fields and nothing newer.
func TestAHolderRecordFromAnOlderBuildNamesTheWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	raw := `{"schema":1,"sessionId":"aaaa000000000002","workspace":"/tmp/alpha","build":"a1b2c3d4 built 2026-09-21 09:00","pid":424242,"updatedAt":"` +
		now.Add(-time.Minute).UTC().Format(time.RFC3339Nano) + `","state":"working"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "presence.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	holder, ok := ReadHolder(dir, now)
	if !ok {
		t.Fatal("an older build's record was not read")
	}
	if holder.PID != 424242 || holder.Build != "a1b2c3d4 built 2026-09-21 09:00" || holder.State != PresenceWorking {
		t.Fatalf("the record read as %+v", holder)
	}
	// A RECORD A MINUTE OLD IS STILL THE HOLDER: a wedged window stops beating
	// before it lets go of its lock, and that is the window this has to name.
	// One older than a request's own life is not trusted with a pid.
	if _, ok := ReadHolder(dir, now.Add(TakeoverStale+time.Minute)); ok {
		t.Fatal("a record older than a request's life was trusted with a pid")
	}
}

// AND THIS BUILD WRITES THE FOUR FROZEN FIELDS under schema 1.
func TestThisBuildsHolderRecordCarriesTheFrozenFields(t *testing.T) {
	raw, err := json.Marshal(SessionPresence{Schema: presenceSchema, SessionID: "x", PID: 7, Build: "b", State: PresenceIdle, UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if presenceSchema != 1 || fields["schema"] != float64(1) {
		t.Fatalf("the presence schema moved: %v", fields["schema"])
	}
	for _, key := range []string{"pid", "build", "state", "updatedAt"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("the holder record lost %q: %s", key, raw)
		}
	}
}

// STOP REACHES THE PROCESS THE RECORD NAMES, and only while that process is
// still holding the lock.
func TestStopHolderSignalsTheWindowHoldingTheLockAndNoOther(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(transcript, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "120")
	if err := child.Start(); err != nil {
		t.Skipf("no process to stand in for the holder: %v", err)
	}
	pid := child.Process.Pid
	exited := make(chan struct{})
	go func() { _ = child.Wait(); close(exited) }()
	t.Cleanup(func() {
		select {
		case <-exited:
		default:
			_ = child.Process.Kill()
			<-exited
		}
	})
	raw, _ := json.Marshal(map[string]any{"schema": 1, "sessionId": "s", "pid": pid, "build": "b", "state": "idle", "updatedAt": time.Now().Format(time.RFC3339Nano)})
	if err := os.WriteFile(filepath.Join(dir, "presence.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// A FREE LOCK IS A HOLDER THAT ALREADY WENT, and nothing is signalled.
	if _, err := StopHolder(dir, transcript, time.Now()); err == nil {
		t.Fatal("a stop was sent for a conversation nobody is holding")
	}
	select {
	case <-exited:
		t.Fatal("a process was signalled for a lock it did not hold")
	default:
	}

	file, err := os.Open(transcript)
	if err != nil {
		t.Fatal(err)
	}
	if err := filelock.Lock(file, true, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = filelock.Unlock(file); _ = file.Close() })

	got, err := StopHolder(dir, transcript, time.Now())
	if err != nil || got != pid {
		t.Fatalf("stop answered (%d, %v), want pid %d", got, err, pid)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the holder was not stopped")
	}
}
