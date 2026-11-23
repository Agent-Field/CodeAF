//go:build windows

package processgroup

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const helperEnv = "CODEAF_PROCESSGROUP_HELPER"

type helperReceipt struct {
	PID     int
	Address string
}

func TestMain(m *testing.M) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		os.Exit(m.Run())
	}
	if err := processHelper(mode); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func processHelper(mode string) error {
	switch mode {
	case "listen":
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		defer listener.Close()
		data, _ := json.Marshal(helperReceipt{os.Getpid(), listener.Addr().String()})
		if err := os.WriteFile(os.Getenv("CODEAF_PROCESSGROUP_RECEIPT"), data, 0o600); err != nil {
			return err
		}
		// A console helper has no GUI window for taskkill's non-force stop.
		for {
			connection, err := listener.Accept()
			if err != nil {
				return err
			}
			connection.Close()
		}
	case "spawn":
		self, _ := os.Executable()
		child := exec.Command(self)
		child.Env = append(os.Environ(), helperEnv+"=listen")
		if err := child.Start(); err != nil {
			return err
		}
		return child.Process.Release()
	case "tree":
		// The .cmd shim and this parent both exit before the listener is stopped.
		child := exec.Command("cmd.exe")
		child.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c ""` + os.Getenv("CODEAF_PROCESSGROUP_SHIM") + `""`}
		child.Env = append(os.Environ(), helperEnv+"=spawn")
		return child.Run()
	case "detach", "owned", "managed":
		self, _ := os.Executable()
		child := exec.Command(self)
		child.Env = append(os.Environ(), helperEnv+"=listen")
		if mode == "detach" {
			ConfigureDetached(child)
		} else {
			Configure(child)
		}
		if err := Start(child); err != nil {
			return err
		}
		// Wait until the listener has published its receipt, then emulate engine exit.
		if _, err := readHelperReceipt(os.Getenv("CODEAF_PROCESSGROUP_RECEIPT")); err != nil {
			return err
		}
		if mode == "managed" {
			_ = Terminate(child.Process.Pid)
			time.Sleep(100 * time.Millisecond)
			if err := Kill(child.Process.Pid); err != nil {
				return err
			}
			if err := WaitEmpty(child.Process.Pid, 5*time.Second); err != nil {
				return err
			}
			_ = child.Wait()
			return nil
		}
		return child.Process.Release()
	}
	return fmt.Errorf("unknown helper mode %q", mode)
}

func readHelperReceipt(path string) (helperReceipt, error) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var receipt helperReceipt
		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &receipt) == nil {
			return receipt, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return helperReceipt{}, fmt.Errorf("helper did not publish a listener")
}

func helperCommand(t *testing.T, mode string) (*exec.Cmd, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "receipt.json")
	shim := filepath.Join(dir, "launch.cmd")
	if err := os.WriteFile(shim, []byte("@\""+self+"\"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), helperEnv+"="+mode, "CODEAF_PROCESSGROUP_RECEIPT="+path, "CODEAF_PROCESSGROUP_SHIM="+shim)
	return cmd, path
}

func assertListenerGone(t *testing.T, receipt helperReceipt) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for ProcessAlive(receipt.PID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if ProcessAlive(receipt.PID) {
		t.Fatalf("listener process %d survived stop", receipt.PID)
	}
	// Int-fix contract B: Windows can signal process death just before its
	// sockets finish closing. Allow that teardown to complete after the PID check.
	deadline = time.Now().Add(2 * time.Second)
	for {
		listener, err := net.Listen("tcp", receipt.Address)
		if err == nil {
			listener.Close()
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("stopped process still holds its port: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Contracts 1, 3, 4: the real API owns an orphan through a .cmd shim, retains
// it after leader reaping, escalates, and verifies the whole tree is stopped.
func TestContractWindowsOrphanedTreeStop(t *testing.T) {
	cmd, path := helperCommand(t, "tree")
	Configure(cmd)
	if err := Start(cmd); err != nil {
		t.Fatal(err)
	}
	group := CaptureGroup(cmd.Process.Pid)
	t.Cleanup(func() { _ = group.Kill() })
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	receipt, err := readHelperReceipt(path)
	if err != nil {
		t.Fatal(err)
	}
	if !group.Alive() {
		t.Fatal("leader exited, but its listening grandchild was lost")
	}
	_ = group.Terminate()
	// Contract 4: keep the soft round separate; this console helper needs force.
	time.Sleep(100 * time.Millisecond)
	if !group.Alive() {
		t.Fatal("helper did not exercise hard-kill escalation")
	}
	if err := group.Kill(); err != nil {
		t.Fatal(err)
	}
	if group.Alive() {
		t.Fatal("successful hard kill did not verify tree completion")
	}
	if err := WaitEmpty(cmd.Process.Pid, 5*time.Second); err != nil {
		t.Fatalf("stop was not verified: %v", err)
	}
	if group.Alive() {
		t.Fatal("stop result said stopped with a live job member")
	}
	assertListenerGone(t, receipt)
}

// Contracts 1 and 2: explicit engine teardown stops managed launches; ordinary
// children and deliberate detaches survive a parent exit, as on POSIX.
func TestContractWindowsEngineLifetime(t *testing.T) {
	for _, mode := range []string{"managed", "owned", "detach"} {
		t.Run(mode, func(t *testing.T) {
			engine, path := helperCommand(t, mode)
			if err := engine.Run(); err != nil {
				t.Fatal(err)
			}
			receipt, err := readHelperReceipt(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if p, err := os.FindProcess(receipt.PID); err == nil {
					_ = p.Kill()
					_ = p.Release()
				}
			})
			if mode == "managed" {
				assertListenerGone(t, receipt)
				return
			}
			if !ProcessAlive(receipt.PID) {
				t.Fatal("deliberate detach died with its engine")
			}
			// Probe the listening socket without creating a TIME_WAIT connection
			// that could independently obstruct the later port-release check.
			if listener, err := net.Listen("tcp", receipt.Address); err == nil {
				listener.Close()
				t.Fatal("detached listener no longer holds its port")
			}
			if mode == "owned" {
				return
			}
			// A later engine can reopen the kept launch and verify its stop.
			group := CaptureGroup(receipt.PID)
			if !group.Alive() {
				t.Fatal("detached launch could not be re-adopted")
			}
			_ = group.Terminate()
			if err := group.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := WaitEmpty(receipt.PID, 5*time.Second); err != nil {
				t.Fatal(err)
			}
			assertListenerGone(t, receipt)
		})
	}
}

// Contracts 1 and 2: the engine may itself already belong to a Job Object.
func TestContractWindowsNestedJob(t *testing.T) {
	engine, path := helperCommand(t, "managed")
	Configure(engine)
	if err := Start(engine); err != nil {
		t.Fatal(err)
	}
	group := CaptureGroup(engine.Process.Pid)
	t.Cleanup(func() { _ = group.Kill() })
	if err := engine.Wait(); err != nil {
		t.Fatal(err)
	}
	receipt, err := readHelperReceipt(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WaitEmpty(engine.Process.Pid, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	assertListenerGone(t, receipt)
}

// Contract 1: inherited-console commands also own their launch for cancellation.
func TestContractWindowsInheritedLaunch(t *testing.T) {
	cmd, path := helperCommand(t, "tree")
	if err := Start(cmd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Kill(cmd.Process.Pid) })
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	receipt, err := readHelperReceipt(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := KillChild(cmd.Process); err != nil {
		t.Fatal(err)
	}
	if err := WaitEmpty(cmd.Process.Pid, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	assertListenerGone(t, receipt)
}

// Contracts 1 and 3: cancellation during suspended setup must not mistake an
// empty, not-yet-assigned job for a completed stop and then resume the child.
func TestContractWindowsCancelDuringLaunch(t *testing.T) {
	base, _ := helperCommand(t, "listen")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, base.Path, base.Args[1:]...)
	cmd.Env = base.Env
	Configure(cmd)
	entered := make(chan struct{})
	cmd.Cancel = func() error {
		close(entered)
		return Kill(cmd.Process.Pid)
	}
	original := beforeJobAssignment
	t.Cleanup(func() { beforeJobAssignment = original })
	beforeJobAssignment = func() {
		cancel()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("cancellation never reached the launch")
		}
		// Give the cancellation a chance to query the still-empty job. Startup
		// must keep that query behind assignment, not let it report success.
		time.Sleep(50 * time.Millisecond)
	}
	if err := Start(cmd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Kill(cmd.Process.Pid) })
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = Kill(cmd.Process.Pid)
		<-done
		t.Fatal("cancelled suspended launch was resumed and left running")
	}
	if Alive(cmd.Process.Pid) {
		t.Fatal("cancelled launch still owns live processes")
	}
}
