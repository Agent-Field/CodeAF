package store

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAppRunClaimRollsBackWhenProcessDiesBeforeRun(t *testing.T) {
	const key = "crm/stage"
	if path := os.Getenv("CODEAF_TEST_APP_RUN_CRASH_DB"); path != "" {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.withAppRunClaim(42, key, func(_ *sql.Tx) error {
			fmt.Fprintln(os.Stdout, "receipt inserted")
			_, err := bufio.NewReader(os.Stdin).ReadByte()
			return err
		})
		t.Fatalf("the parent should kill the process inside the transaction: %v", err)
	}

	path := filepath.Join(t.TempDir(), "runs.db")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestAppRunClaimRollsBackWhenProcessDiesBeforeRun$")
	cmd.Env = append(os.Environ(), "CODEAF_TEST_APP_RUN_CRASH_DB="+path)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "receipt inserted\n" {
		t.Fatalf("child did not reach the gap: %q, %v", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("the interrupted child unexpectedly finished normally")
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i, id := range []string{"run/recovered", "run/duplicate"} {
		claimed, err := s.ClaimAppRun(42, key, id, json.RawMessage(`{"state":"running"}`))
		if err != nil || claimed != (i == 0) {
			t.Fatalf("claim %d = %v, %v", i, claimed, err)
		}
	}
	got, err := s.AppRunCheckpoints()
	if err != nil || len(got) != 1 || got["run/recovered"] == nil {
		t.Fatalf("checkpoints = %s, %v", got, err)
	}
}
