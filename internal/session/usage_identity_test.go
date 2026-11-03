package session

import (
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLedgerIdentityProcessWriter(t *testing.T) {
	path := os.Getenv("CODEAF_TEST_LEDGER_IDENTITY")
	if path == "" {
		t.Skip("subprocess helper")
	}
	if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	for i := range 10 {
		RecordUsage(path, UsageLine{ID: string(rune('a' + i)), Model: "m", Input: 100, Output: 10, USD: .001})
	}
	if !StopUsageWriter(path) {
		t.Fatal("writer did not stop")
	}
}

func TestLedgerIdentityAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	const writers = 4
	cmds := make([]*exec.Cmd, writers)
	gates := make([]io.WriteCloser, writers)
	for i := range writers {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLedgerIdentityProcessWriter$", "-test.v")
		cmd.Env = append(os.Environ(), "CODEAF_TEST_LEDGER_IDENTITY="+path)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		gate, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i], gates[i] = cmd, gate
		t.Cleanup(func() {
			_ = gate.Close()
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})
	}
	for _, gate := range gates {
		if _, err := gate.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		_ = gate.Close()
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	for _, row := range rows {
		total += row.USD
	}
	t.Logf("expected=$0.010000 recorded=$%.6f rows=%d want_rows=10", total, len(rows))
	if math.Abs(total-.010) > 1e-9 || len(rows) != 10 {
		t.Fatalf("expected=$0.010000 recorded=$%.6f rows=%d want_rows=10", total, len(rows))
	}
}

func TestLedgerIdentityKeepsAccountingAfterUnfinishedTail(t *testing.T) {
	for _, tc := range []struct {
		name, tail string
		want       float64
		rows       int
	}{
		{"torn", `{"id":"lost","usd":`, .003, 2},
		{"complete_without_newline", `{"at":"2026-09-29T10:00:01Z","id":"old","input":1,"usd":0.004}`, .007, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), UsageLedgerName)
			if err := os.WriteFile(path, []byte(tc.tail), 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { StopUsageWriter(path) })
			// A complete old object acquires its missing newline even when the
			// first queued row is a replay of that same identity.
			if tc.name == "complete_without_newline" {
				RecordUsage(path, UsageLine{ID: "old", Input: 1, USD: .004})
			}
			RecordUsage(path, UsageLine{ID: "one", Input: 1, USD: .001})
			RecordUsage(path, UsageLine{ID: "two", Input: 1, USD: .002})
			if !FlushUsage() {
				t.Fatal("flush")
			}
			rows, err := ReadUsage(path, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			var got float64
			for _, row := range rows {
				got += row.USD
			}
			t.Logf("expected=$%.6f recorded=$%.6f rows=%d want_rows=%d", tc.want, got, len(rows), tc.rows)
			if math.Abs(got-tc.want) > 1e-9 || len(rows) != tc.rows {
				t.Fatalf("expected=$%.6f recorded=$%.6f rows=%d want_rows=%d", tc.want, got, len(rows), tc.rows)
			}
		})
	}
}
