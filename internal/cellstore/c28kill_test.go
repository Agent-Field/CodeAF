//go:build !windows

package cellstore

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// TestC28KillAtEveryRecordBoundary proves C28/L9/L12 with a real SIGKILL,
// including death before the group timer: intent, process group, completion,
// seal publication and WAL compaction each reopen without executing anything.
func TestC28KillAtEveryRecordBoundary(t *testing.T) {
	for _, boundary := range []string{"intent", "group", "done", "turn", "compact", "resolved"} {
		t.Run(boundary, func(t *testing.T) {
			c := newCell(t)
			wal := filepath.Join(t.TempDir(), "calls")
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			// The child shares the parent's test envelope. A private short ceiling
			// could mistake loaded-disk startup for a C28 recovery failure.
			var args = []string{"-test.run=^TestC28KilledWriter$"}
			if deadline, ok := t.Deadline(); ok {
				args = append(args, "-test.timeout="+time.Until(deadline).String())
			}
			cmd := exec.CommandContext(t.Context(), binary, args...)
			cmd.Env = append(os.Environ(), "C28_CHILD="+boundary, "C28_CELL="+c.Root, "C28_WAL="+wal)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			scanner := bufio.NewScanner(stdout)
			ready := false
			for scanner.Scan() {
				if scanner.Text() == "C28 ready" {
					ready = true
					break
				}
			}
			if !ready {
				_ = cmd.Wait()
				t.Fatal("writer never reached record boundary")
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err = cmd.Wait(); err == nil {
				t.Fatal("writer was not killed")
			}
			inner := &stubExec{}
			r, _ := newRecorder(t, c, inner, wal)
			if inner.runs != 0 {
				t.Fatal("reopen ran an external call")
			}
			_, rec, err := OpenWAL(wal)
			if err != nil {
				t.Fatal(err)
			}
			switch boundary {
			case "intent", "group":
				if len(r.Incomplete()) != 1 || len(rec.Completed) != 0 {
					t.Fatalf("lost incomplete: %+v", rec)
				}
				if r.Incomplete()[0].MayRerun() {
					t.Fatal("external intent permits rerun")
				}
				if boundary == "group" && len(rec.Incomplete[0].Groups) != 1 {
					t.Fatal("lost process-group record")
				}
			case "done", "turn":
				if len(rec.Completed) != 1 || string(rec.Completed[0].Stdout) != "saved once" {
					t.Fatalf("lost external result: %+v", rec)
				}
			case "compact", "resolved":
				if len(rec.Completed) != 0 || len(rec.Incomplete) != 0 {
					t.Fatalf("closed record came back: %+v", rec)
				}
			}
			if boundary == "turn" || boundary == "compact" {
				head, err := Head(c)
				if err != nil || head == nil || len(head.Receipt.Calls) != 1 {
					t.Fatalf("lost sealed turn: %+v %v", head, err)
				}
			}
		})
	}
}

// TestC28KilledWriter is the subprocess side of C28's record-boundary proof.
// It blocks on its input after acknowledging the write, so only SIGKILL ends it.
func TestC28KilledWriter(t *testing.T) {
	boundary := os.Getenv("C28_CHILD")
	if boundary == "" {
		return
	}
	commits.after = func(time.Duration, func()) {}
	c, err := cell.OpenAt(os.Getenv("C28_CELL"), filepath.Base(os.Getenv("C28_CELL")))
	if err != nil {
		t.Fatal(err)
	}
	w, _, err := OpenWAL(os.Getenv("C28_WAL"))
	if err != nil {
		t.Fatal(err)
	}
	w.owner = c.Root
	// Keep the group clock manual here: every record is tested before any fsync.
	i := Intent{V: walV, Tool: "bash", ArgsHash: "once", Started: 1, SideEffect: "external"}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(w.Begin(i))
	if boundary != "intent" {
		must(w.Started(i, GroupRec{PGID: 123, Start: 0, Owner: os.Getpid()}))
	}
	done := Executed{Call: Call{Tool: i.Tool, ArgsHash: i.ArgsHash, Started: i.Started, SideEffect: i.SideEffect}, Stdout: []byte("saved once")}
	if strings.Contains(" done turn compact ", " "+boundary+" ") {
		must(w.Finish(i, done))
	}
	if boundary == "turn" || boundary == "compact" {
		fake := &fakeEngine{}
		_, err := fake.engine(t).Seal(t.Context(), c, TurnInfo{Calls: []Executed{done}})
		must(err)
	}
	if boundary == "compact" {
		must(w.Sealed([]Executed{done}))
	}
	if boundary == "resolved" {
		must(w.Resolve(i))
	}
	fmt.Println("C28 ready")
	// A parent-owned pipe stays open until its kill, without real-time sleeps.
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
	select {}
}
