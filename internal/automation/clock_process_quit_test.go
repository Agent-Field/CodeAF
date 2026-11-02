package automation

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestAppQuitHelperLifetime(t *testing.T) {
	if root := os.Getenv("CODEAF_TEST_CLOCK_ROOT"); root != "" {
		if os.Getenv("CODEAF_TEST_CLOCK_ROLE") == "window" {
			release, err := NewPresence(root).Hold("app")
			if err != nil {
				t.Fatal(err)
			}
			fmt.Println("ready")
			_, _ = bufio.NewReader(os.Stdin).ReadByte()
			release()
			return
		}
		fast(t)
		store, err := Open(root)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		clock := Clock{Store: store, Presence: NewPresence(root), Runner: &fakeRunner{}}
		if err := clock.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, killed := range []bool{false, true} {
		t.Run(fmt.Sprintf("killed=%v", killed), func(t *testing.T) {
			root := t.TempDir()
			window := exec.Command(os.Args[0], "-test.run=^TestAppQuitHelperLifetime$")
			window.Env = append(os.Environ(), "CODEAF_TEST_CLOCK_ROOT="+root, "CODEAF_TEST_CLOCK_ROLE=window")
			in, err := window.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			out, err := window.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := window.Start(); err != nil {
				t.Fatal(err)
			}
			defer window.Process.Kill()
			if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "ready\n" {
				t.Fatalf("presence: %q %v", line, err)
			}
			clock := exec.Command(os.Args[0], "-test.run=^TestAppQuitHelperLifetime$")
			clock.Env = append(os.Environ(), "CODEAF_TEST_CLOCK_ROOT="+root, "CODEAF_TEST_CLOCK_ROLE=clock")
			clock.Stderr = os.Stderr
			if err := clock.Start(); err != nil {
				t.Fatal(err)
			}
			defer clock.Process.Kill()
			done := make(chan error, 1)
			go func() { done <- clock.Wait() }()
			deadline := time.Now().Add(5 * time.Second)
			for !Held(root) {
				if time.Now().After(deadline) {
					t.Fatal("clock never held its lock")
				}
				time.Sleep(5 * time.Millisecond)
			}
			started := time.Now()
			if killed {
				if err := window.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			} else {
				in.Close()
			}
			err = window.Wait()
			if !killed && err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("clock outlived app presence")
			}
			if Held(root) {
				t.Fatal("exited clock kept its lock")
			}
			if n, err := NewPresence(root).Count(); err != nil || n != 0 {
				t.Fatalf("presence after exit: %d %v", n, err)
			}
			t.Logf("clock quit to gone (60ms test grace, killed=%v): %.3fs", killed, time.Since(started).Seconds())
		})
	}
}
