package processgroup

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Contract 2: neither POSIX group setup nor session detachment gives a child a
// parent-death signal. Returning from an ordinary command must preserve that
// lifetime on Windows, too; managed shutdown is an explicit stop operation.
func TestContract2ProcessLifetime(t *testing.T) {
	mode := os.Getenv("CODEAF_LIFETIME_HELPER")
	path := os.Getenv("CODEAF_LIFETIME_RECEIPT")
	if mode == "listener" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(lifetimeReceipt{os.Getpid(), listener.Addr().String()})
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			connection.Close()
		}
	}
	if mode != "" {
		child := lifetimeCommand(mode, path)
		child.Env = append(child.Env, "CODEAF_LIFETIME_HELPER=listener")
		if mode == "detached" {
			ConfigureDetached(child)
		} else {
			Configure(child)
		}
		if err := Start(child); err != nil {
			t.Fatal(err)
		}
		if _, err := lifetimeReady(path); err != nil {
			_ = child.Process.Kill()
			t.Fatal(err)
		}
		_ = child.Process.Release()
		return
	}
	for _, mode := range []string{"ordinary", "detached"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "listener.json")
			parent := lifetimeCommand(mode, path)
			if err := parent.Run(); err != nil {
				t.Fatal(err)
			}
			receipt, err := lifetimeReady(path)
			if err != nil {
				t.Fatal(err)
			}
			process, err := os.FindProcess(receipt.PID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = process.Kill(); _ = process.Release() })
			connection, err := net.DialTimeout("tcp", receipt.Address, time.Second)
			if err != nil {
				t.Fatalf("%s child died with its parent: %v", mode, err)
			}
			connection.Close()
		})
	}
}

type lifetimeReceipt struct {
	PID     int
	Address string
}

func lifetimeCommand(mode, path string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestContract2ProcessLifetime$")
	cmd.Env = append(os.Environ(), "CODEAF_LIFETIME_HELPER="+mode, "CODEAF_LIFETIME_RECEIPT="+path)
	return cmd
}

func lifetimeReady(path string) (lifetimeReceipt, error) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var receipt lifetimeReceipt
		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &receipt) == nil {
			return receipt, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return lifetimeReceipt{}, fmt.Errorf("listener did not start")
}
