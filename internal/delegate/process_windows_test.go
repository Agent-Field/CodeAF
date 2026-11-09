//go:build windows

package delegate

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

type processReceipt struct {
	PID     int
	Address string
}

func TestMain(m *testing.M) {
	mode := os.Getenv("CODEAF_DELEGATE_PROCESS_HELPER")
	if mode == "" {
		os.Exit(m.Run())
	}
	path := os.Getenv("CODEAF_DELEGATE_PROCESS_RECEIPT")
	if mode == "listener" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(1)
		}
		data, _ := json.Marshal(processReceipt{os.Getpid(), listener.Addr().String()})
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
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), "CODEAF_DELEGATE_PROCESS_HELPER=listener")
	if child.Start() != nil {
		os.Exit(1)
	}
	_ = child.Process.Release()
	for {
		if _, err := os.Stat(path + ".exit"); err == nil {
			os.Exit(0)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Contracts 1, 3, 4: a delegate leader can exit during its soft-stop grace while
// its grandchild survives. The launch must escalate and verify before saying stopped.
func TestContractWindowsDelegateStopsOrphanAfterLeaderExit(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "listener.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type answer struct {
		result Result
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		result, err := Run(ctx, Launch{Name: "fixture", Bin: self, Dir: t.TempDir(), Grace: 150 * time.Millisecond,
			Env: append(os.Environ(), "CODEAF_DELEGATE_PROCESS_HELPER=parent", "CODEAF_DELEGATE_PROCESS_RECEIPT="+path)}, nil)
		done <- answer{result, err}
	}()
	var receipt processReceipt
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &receipt) == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if receipt.PID == 0 {
		t.Fatal("delegate never started its listener")
	}
	child, err := os.FindProcess(receipt.PID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Kill(); _ = child.Release() })
	cancel()
	if err := os.WriteFile(path+".exit", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if !got.result.Stopped || !got.result.Killed || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("stop = %+v, %v; want verified escalation", got.result, got.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("delegate stop did not finish")
	}
	if processgroup.ProcessAlive(receipt.PID) {
		t.Fatal("delegate reported stopped with a live grandchild")
	}
	listener, err := net.Listen("tcp", receipt.Address)
	if err != nil {
		t.Fatalf("stopped delegate still holds its port: %v", err)
	}
	listener.Close()
}
