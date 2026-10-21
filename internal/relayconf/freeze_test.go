//go:build relayurl

package relayconf

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// The holder of a frozen-computer check is this very test binary run again, so
// the real client code answers pings in a process the test can stop by pid.
const holderHomeEnv = "CAF_FREEZE_HOLDER_HOME"

// freezeBound is what the owner was promised: a device that stops answering is
// shown offline within 30 s (STAGE-1-CONTRACTS.md section 21.12.2).
const freezeBound = 30 * time.Second

// holderMain is the child: it follows the watch feed as a device of the
// identity and keeps answering pings until it is stopped.
func holderMain(t *testing.T, home, base string) {
	id, _ := identity.Ensure(home)
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	h := directory.NewHTTP(base, reqsign.SignFor(signer{id, dev}, wall), httpClient)
	f := dirwatch.Follow("freeze-holder", func(ctx context.Context) (dirwatch.Stream, error) { return h.Watch(ctx) })
	defer f.Close()
	time.Sleep(10 * time.Minute)
}

// TestFrozenDeviceShowsOffWithinBound runs two homes against one relay, stops
// the holder's process with SIGSTOP and measures how long the viewer takes to
// show it offline, then continues it and measures the way back.
func TestFrozenDeviceShowsOffWithinBound(t *testing.T) {
	base := baseURL(t)
	if home := os.Getenv(holderHomeEnv); home != "" {
		holderMain(t, home, base)
		return
	}
	acct := newAccount(t)
	holder, viewer := acct.device("holder"), acct.device("viewer")
	for _, name := range []string{"holder", "viewer"} {
		d := directory.NewHTTP(base, acct.sign(name, wall), httpClient)
		if err := d.PutDevice(context.Background(), acct.deviceID(name), directory.Device{V: 1, Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	v := directory.NewHTTP(base, reqsign.SignFor(viewer, wall), httpClient)
	f := dirwatch.Follow("freeze-viewer", func(ctx context.Context) (dirwatch.Stream, error) { return v.Watch(ctx) })
	defer f.Close()

	// The child holds the same identity under the holder's own device home.
	home := t.TempDir()
	if err := identity.Save(home, acct.id); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestFrozenDeviceShowsOffWithinBound$", "-relay-url="+base)
	child.Env = append(os.Environ(), holderHomeEnv+"="+home)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL); _ = child.Wait() })
	_ = holder // the child makes its own device under the saved identity

	waitFor := func(want bool, within time.Duration) time.Duration {
		start := time.Now()
		for time.Since(start) < within {
			if len(f.State().Online) > 0 == want {
				return time.Since(start)
			}
			select {
			case <-f.Changes():
			case <-time.After(250 * time.Millisecond):
			}
		}
		t.Fatalf("online=%v not reached within %s (online %v)", want, within, f.State().Online)
		return 0
	}
	t.Logf("holder online after %s", waitFor(true, 20*time.Second))
	time.Sleep(12 * time.Second) // a few pings land, so the last sign of life is recent

	_ = syscall.Kill(pid, syscall.SIGSTOP)
	off := waitFor(false, freezeBound+10*time.Second)
	t.Logf("FREEZE: shown off %s after SIGSTOP", off)
	if off > freezeBound {
		t.Errorf("offline took %s, bound %s", off, freezeBound)
	}

	_ = syscall.Kill(pid, syscall.SIGCONT)
	t.Logf("RESUME: back online %s after SIGCONT", waitFor(true, 30*time.Second))
}
