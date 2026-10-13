package loginenv

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLoginPathUsesNonInteractiveLoginShell(t *testing.T) {
	root := t.TempDir()
	shell := filepath.Join(root, "login-shell")
	script := `#!/bin/sh
[ "$#" = 3 ] && [ "$1" = -l ] && [ "$2" = -c ] || exit 12
if read -r input; then exit 13; fi
printf 'profile banner\n'
PATH='/login/tool/bin:/usr/bin'
eval "$3"
printf 'profile footer\n'
`
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	got, err := loginPath(context.Background())
	if err != nil || got != "/login/tool/bin:/usr/bin" {
		t.Fatalf("loginPath = %q, %v", got, err)
	}
}

func TestLoginEnvProcessHelper(t *testing.T) {
	if os.Getenv("CODEAF_LOGIN_ENV_HELPER") != "1" {
		return
	}
	mode, address := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	if mode == "parent" {
		child := exec.Command(os.Args[0], "-test.run=^TestLoginEnvProcessHelper$", "--", "child", address)
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Run(); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	conn, err := net.Dial("tcp", address)
	if err != nil {
		os.Exit(3)
	}
	_, _ = io.Copy(io.Discard, conn)
	os.Exit(0)
}

func TestShellOutputCancellationKillsChildHoldingStdout(t *testing.T) {
	t.Setenv("CODEAF_LOGIN_ENV_HELPER", "1")
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(startupTimeout)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := shellOutput(ctx, os.Args[0], "-test.run=^TestLoginEnvProcessHelper$", "--", "parent", listener.Addr().String())
		done <- err
	}()
	// The child's connection proves it inherited stdout before cancellation;
	// no startup sleep or assumed scheduler timing decides when to kill it.
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("shellOutput error = %v; want context cancellation", err)
		}
	case <-time.After(startupTimeout):
		t.Fatal("shellOutput stayed blocked on inherited stdout")
	}
	if err := conn.SetReadDeadline(time.Now().Add(startupTimeout)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("child survived process-group cancellation: %v", err)
	}
}

func TestShellOutputAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if output, err := shellOutput(ctx, "/bin/zsh", "-l", "-c", pathCommand); err != context.Canceled || output != "" {
		t.Fatalf("cancelled shellOutput = %q, %v", output, err)
	}
}
