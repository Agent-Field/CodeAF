//go:build linux

package cellstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

// A stopped syscall exposes the first-pack race without deleting a pack or
// falsifying an engine answer. Planning must join the retained seal writer.
func TestCapturePlanCannotObserveHalfOfTheFirstSeal(t *testing.T) {
	for _, door := range []string{"spawn", "second-daemon"} {
		t.Run(door, func(t *testing.T) { testFirstSealInterleaving(t, door) })
	}
}

func testFirstSealInterleaving(t *testing.T, door string) {
	if _, err := exec.LookPath("strace"); err != nil {
		t.Skip("strace is needed to stop the real engine at its missing-pack observation")
	}
	e := realEngine(t)
	d := e.Transport.(Spawn)
	c := newCell(t)
	target := e.target(c)
	if err := os.WriteFile(filepath.Join(c.Root, "note"), []byte("first shared chat"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Daemon{Binary: e.Binary, Socket: d.Socket}).Do(t.Context(), target, capturePlanOp{}); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "reader.trace")
	// Discover the syscall ordinal from this binary instead of pinning libc's
	// startup work or the host architecture into the regression.
	_, err := spawn(t.Context(), target.Tree, append(os.Environ(), dataDirEnv+"="+target.DataDir), "strace", "-o", trace, "-e", "trace=statx", e.Binary, "--json", "--timeline", target.Timeline, "capture-plan")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	ordinal := 0
	found := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "statx(") {
			ordinal++
		}
		if strings.Contains(line, "/packs/pack-000001.agp") && strings.Contains(line, "ENOENT") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("engine did not probe the initially absent pack")
	}
	if err := os.Remove(trace); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), filepath.Base(e.Binary))
	t.Setenv("CODEAF_PACK_REPRO_BINARY", e.Binary)
	t.Setenv("CODEAF_PACK_REPRO_TRACE", trace)
	t.Setenv("CODEAF_PACK_REPRO_WHEN", strconv.Itoa(ordinal))
	t.Setenv("CODEAF_PACK_REPRO_DOOR", door)
	script := `#!/bin/sh
case "$CODEAF_PACK_REPRO_DOOR:$1" in
 spawn:--json|second-daemon:serve)
  exec strace -f -o "$CODEAF_PACK_REPRO_TRACE" -e trace=statx -e "inject=statx:signal=SIGSTOP:when=$CODEAF_PACK_REPRO_WHEN" "$CODEAF_PACK_REPRO_BINARY" "$@"
  ;;
 *) exec "$CODEAF_PACK_REPRO_BINARY" "$@" ;;
esac
`
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var reader Transport = Spawn{Binary: wrapper, Socket: d.Socket}
	if door == "second-daemon" {
		reader = daemonFor(t, wrapper)
	}
	done := make(chan error, 1)
	guard.Go("capture-plan first seal regression", func() {
		raw, err := reader.Do(ctx, target, capturePlanOp{})
		if err == nil {
			var plan capturePlan
			err = json.Unmarshal(raw, &plan)
			if err == nil && plan.Token == "" {
				err = errors.New("planning returned no token")
			}
		}
		done <- err
	})
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	var sealedWhileStopped bool
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("concurrent first seal broke capture-plan: %v", err)
			}
			if sealedWhileStopped {
				t.Fatal("another writer passed the stopped planning operation")
			}
			if _, err := d.Do(ctx, target, sealOp{Turn: "after-plan", Durable: true}); err != nil {
				t.Fatal(err)
			}
			return
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-tick.C:
			traceBytes, _ := os.ReadFile(trace)
			if !strings.Contains(string(traceBytes), "stopped by SIGSTOP") {
				continue
			}
			// The old spawn path has already observed ENOENT. Finish the first seal
			// before allowing it to read the newer, nonempty durability frontier.
			if !strings.Contains(string(traceBytes), "/packs/pack-000001.agp") {
				t.Fatal("stopped at a different syscall")
			}
			lines := strings.Split(string(traceBytes), "\n")
			var stoppedLine string
			for _, line := range lines {
				if strings.Contains(line, "stopped by SIGSTOP") {
					stoppedLine = line
					break
				}
			}
			pid, err := strconv.Atoi(strings.Fields(stoppedLine)[0])
			if err != nil {
				t.Fatal(err)
			}
			process, err := os.FindProcess(pid)
			if err != nil {
				t.Fatal(err)
			}
			stopped := true
			t.Cleanup(func() {
				if stopped {
					_ = process.Kill()
				}
			})
			sealCtx, stop := context.WithTimeout(ctx, time.Second)
			_, sealErr := d.Do(sealCtx, target, sealOp{Turn: "during-plan", Durable: true})
			sealedWhileStopped = !errors.Is(sealErr, context.DeadlineExceeded)
			stop()
			if err := process.Signal(syscall.SIGCONT); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Fatalf("resume reader: %v", err)
			}
			stopped = false
			// One resume is enough; do not interpret the old stop marker again.
			tick.Stop()
		}
	}
}
