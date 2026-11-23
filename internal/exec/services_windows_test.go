//go:build windows

package exec

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

func TestMain(m *testing.M) {
	if path := os.Getenv("CODEAF_SERVICE_STOP_HELPER"); path != "" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(1)
		}
		data, _ := json.Marshal(listener.Addr().String())
		if os.WriteFile(path, data, 0o600) != nil {
			os.Exit(1)
		}
		for {
			connection, err := listener.Accept()
			if err != nil {
				os.Exit(1)
			}
			connection.Close()
		}
	}
	os.Exit(m.Run())
}

// Contracts 1, 3, 4: a native service that refuses the soft round is hard-killed
// and verified; an identity mismatch cannot be reported as a successful stop.
func TestContractWindowsServiceStop(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "listener.json")
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), "CODEAF_SERVICE_STOP_HELPER="+path)
	processgroup.ConfigureDetached(cmd)
	if err := processgroup.Start(cmd); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = processgroup.Kill(pid); _ = cmd.Wait() })
	var address string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &address) == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if address == "" {
		t.Fatal("service did not start listening")
	}
	started, err := ProcessStartTime(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := StopServiceProcess(pid, started.Add(-time.Hour)); err == nil {
		t.Fatal("unverified live service was reported stopped")
	}
	if !processgroup.Alive(pid) {
		t.Fatal("identity mismatch killed the service")
	}
	if err := StopServiceProcess(pid, started); err != nil {
		t.Fatal(err)
	}
	if processgroup.Alive(pid) {
		t.Fatal("stop succeeded while its job was alive")
	}
	assertServicePortFree(t, address)
}

func assertServicePortFree(t *testing.T, address string) {
	t.Helper()
	// Int-fix contract B: Windows can signal process death before its sockets
	// finish closing. Poll only after the service has been confirmed dead.
	deadline := time.Now().Add(2 * time.Second)
	for {
		listener, err := net.Listen("tcp", address)
		if err == nil {
			listener.Close()
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("stopped service still holds its port: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Contract 3: a job waiter that never verifies death must return failure after
// the hard-kill grace, so jobs kill cannot announce a completed stop.
func TestContractWindowsUnverifiedJobWait(t *testing.T) {
	if err := waitProcessGroupStop(make(chan struct{})); err == nil {
		t.Fatal("unverified job stop succeeded")
	}
}
