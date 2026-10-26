//go:build !windows

package taskstorage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/taskstorage"
)

// This is a real subprocess archive adapter. It deliberately keeps writing
// after its manager dies, like a native archive command outside Go's lifetime.
func TestReceiptLeaseProcessHelper(t *testing.T) {
	role := os.Getenv("TASKSTORAGE_LEASE_ROLE")
	if role == "" {
		t.Skip("subprocess helper")
	}
	root := os.Getenv("TASKSTORAGE_LEASE_ROOT")
	if role == "manager" || role == "ensure" {
		m := taskstorage.Manager{Root: filepath.Join(root, "storage"), Trees: filepath.Join(root, "trees"), Busy: func(string) bool { return false }, Vault: taskstorage.CLIBackend{Binary: filepath.Join(root, "engine"), Store: filepath.Join(root, "vault")}}
		var err error
		if role == "ensure" {
			err = m.Ensure(context.Background(), filepath.Join(root, "trees", "task"))
		} else {
			err = m.Park(context.Background(), filepath.Join(root, "trees", "task"))
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 4 {
		os.Exit(3)
	}
	args = args[1:]
	if args[1] == "restore" {
		var destination string
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--destination" {
				destination = args[i+1]
			}
		}
		temporary := filepath.Join(filepath.Dir(destination), ".furrow-archive-active")
		if e := os.MkdirAll(temporary, 0700); e != nil {
			os.Exit(4)
		}
		_ = os.WriteFile(filepath.Join(temporary, "partial"), []byte("writer active"), 0600)
		_ = os.WriteFile(filepath.Join(root, "ready"), []byte(strconv.Itoa(os.Getpid())), 0600)
		deadline := time.Now().Add(20 * time.Second)
		for {
			if _, e := os.Stat(filepath.Join(root, "release")); e == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(5)
			}
			time.Sleep(10 * time.Millisecond)
		}
		e := os.MkdirAll(destination, 0700)
		if e == nil {
			e = os.WriteFile(filepath.Join(destination, "work.txt"), []byte("original"), 0600)
		}
		_ = os.WriteFile(filepath.Join(root, "writer-done"), []byte("done"), 0600)
		if e != nil {
			os.Exit(6)
		}
	}
	fmt.Printf(`{"version":1,"snapshot":"%s"}`, strings.Repeat("a", 64))
	os.Exit(0)
}

func TestKilledManagerKeepsReceiptLockedUntilArchiveWriterExits(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "trees", "task")
	if e := os.MkdirAll(path, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(path, "work.txt"), []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	engine := filepath.Join(root, "engine")
	script := "#!/bin/sh\nTASKSTORAGE_LEASE_ROLE=archive exec \"$TASKSTORAGE_TEST_BINARY\" -test.run=^TestReceiptLeaseProcessHelper$ -- \"$@\"\n"
	if e := os.WriteFile(engine, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestReceiptLeaseProcessHelper$")
	command.Env = append(os.Environ(), "TASKSTORAGE_LEASE_ROLE=manager", "TASKSTORAGE_LEASE_ROOT="+root, "TASKSTORAGE_TEST_BINARY="+os.Args[0])
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if e := command.Start(); e != nil {
		t.Fatal(e)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(root, "release"), []byte("release"), 0600)
		_ = command.Process.Kill()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, e := os.Stat(filepath.Join(root, "writer-done")); e == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		if raw, e := os.ReadFile(filepath.Join(root, "ready")); e == nil {
			if id, e := strconv.Atoi(string(raw)); e == nil {
				if process, e := os.FindProcess(id); e == nil {
					_ = process.Kill()
				}
			}
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, e := os.Stat(filepath.Join(root, "ready")); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("archive writer did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	receipt, state := recoveryReceipt(t, filepath.Join(root, "storage"))
	check, ok := state["check"].(string)
	if !ok || check == "" {
		t.Fatal("writer started without durable verification journal")
	}
	container := filepath.Join(filepath.Dir(receipt), check)
	if e := command.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-waited:
		if e == nil {
			t.Fatal("manager unexpectedly completed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("manager did not exit")
	}
	m := taskstorage.Manager{Root: filepath.Join(root, "storage"), Trees: filepath.Join(root, "trees"), Vault: taskstorage.CLIBackend{Binary: engine, Store: filepath.Join(root, "vault")}}
	canceled, cancel := context.WithCancel(context.Background())
	canceledResult := make(chan error, 1)
	go func() { canceledResult <- m.Ensure(canceled, path) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-canceledResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled recovery: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		_ = os.WriteFile(filepath.Join(root, "release"), []byte("release"), 0600)
		select {
		case <-canceledResult:
		case <-time.After(2 * time.Second):
		}
		t.Fatal("canceled recovery waited for the orphan writer")
	}
	if _, err := os.Stat(container); err != nil {
		t.Fatalf("canceled recovery changed active container: %v", err)
	}
	result := make(chan error, 1)
	go func() { result <- m.Ensure(context.Background(), path) }()
	select {
	case e := <-result:
		t.Fatalf("recovery ran while orphan archive writer was alive: %v", e)
	case <-time.After(150 * time.Millisecond):
	}
	if _, e := os.Stat(filepath.Join(container, ".furrow-archive-active", "partial")); e != nil {
		t.Fatalf("active writer's directory changed: %v", e)
	}
	if e := os.WriteFile(filepath.Join(root, "release"), []byte("release"), 0600); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-result:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery remained blocked after writer exit")
	}
	if _, e := os.Stat(container); !os.IsNotExist(e) {
		t.Fatalf("journaled scratch remains: %v", e)
	}
	raw, e := os.ReadFile(receipt)
	if e != nil {
		t.Fatal(e)
	}
	var after map[string]any
	if e = json.Unmarshal(raw, &after); e != nil {
		t.Fatal(e)
	}
	if after["check"] != nil && after["check"] != "" {
		t.Fatal("journal not cleared after writer exit")
	}
	data, e := os.ReadFile(filepath.Join(path, "work.txt"))
	if e != nil || string(data) != "original" {
		t.Fatalf("resident task changed: %q %v", data, e)
	}
}
