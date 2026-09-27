package session

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// A finished status must follow file close without waiting for retention, while
// done and a completion note still wait for maintenance to finish.
func TestJobStatusSettlesBeforeRetentionButDoneWaits(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kill      bool
		wantState jobState
		wantCode  int
	}{
		{name: "natural exit", wantState: jobExited, wantCode: 7},
		{name: "requested kill", kill: true, wantState: jobKilled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notes := make(chan string, 1)
			registry := newJobRegistry(t.TempDir(), Place{}, func(note string) { notes <- note })
			one, err := registry.newJob("fixture", jobKindBash)
			if err != nil {
				t.Fatal(err)
			}
			if err := registry.add(one); err != nil {
				t.Fatal(err)
			}
			if _, err := one.sink.Write([]byte("complete line\n")); err != nil {
				t.Fatal(err)
			}
			if tc.kill && !one.requestKill() {
				t.Fatal("could not request kill")
			}

			entered := make(chan struct{})
			release := make(chan struct{})
			finished := make(chan struct{})
			one.sink.finishRetention = func() {
				close(entered)
				<-release
				one.sink.mu.Lock()
				one.sink.retentionText = "job log retention deferred: synthetic failure"
				one.sink.mu.Unlock()
			}
			go func() {
				registry.settleExit(one, 7)
				close(finished)
			}()
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
				select {
				case <-finished:
				case <-time.After(10 * time.Second):
					t.Error("job settlement did not finish")
				}
			}()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("retention maintenance did not start")
			}

			if one.running() {
				t.Fatal("job still reads as running while retention maintenance is blocked")
			}
			info := one.info()
			if info.state != tc.wantState || info.code != tc.wantCode {
				t.Fatalf("unfinished status during retention: state=%v code=%d", info.state, info.code)
			}
			if _, err := one.sink.file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("finished job still has an open log: %v", err)
			}
			data, err := os.ReadFile(one.logPath)
			if err != nil || string(data) != "complete line\n" {
				t.Fatalf("finished job log = %q, %v", data, err)
			}
			select {
			case <-one.done:
				t.Fatal("done closed before retention maintenance completed")
			default:
			}
			select {
			case note := <-notes:
				t.Fatalf("completion note arrived before retention maintenance completed: %q", note)
			default:
			}

			close(release)
			select {
			case <-one.done:
			case <-time.After(10 * time.Second):
				t.Fatal("done did not close after retention maintenance completed")
			}
			<-finished
			if tc.kill {
				select {
				case note := <-notes:
					t.Fatalf("requested kill reported its own death: %q", note)
				default:
				}
				return
			}
			select {
			case note := <-notes:
				if !strings.Contains(note, "job log retention deferred: synthetic failure") {
					t.Fatalf("completion note lost the retention failure: %q", note)
				}
			default:
				t.Fatal("completion note was not delivered after retention maintenance")
			}
		})
	}
}
