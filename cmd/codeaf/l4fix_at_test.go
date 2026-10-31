package main

import (
	"context"
	"encoding/base64"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// L4-G6: The actual --at door carries the welcome's chosen name into the window and its other conversations.
func TestL4FixAtWindowUsesDeviceName(t *testing.T) {
	h := t.TempDir()
	t.Setenv("CODEAF_HOME", h)
	t.Setenv("CODEAF_PROFILE_DIR", h)
	t.Cleanup(cell.UseAvailabilityInTests(false))
	if _, err := devname.Set(h, "desk-b"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(&relay.Server{})
	defer server.Close()
	t.Setenv("CODEAF_RELAY", server.URL)
	machine, err := pair.ThisDevice(pair.FileKeeper{Path: filepath.Join(t.TempDir(), "key")})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := pair.ThisDevice(pair.OpenKeeper())
	if err != nil {
		t.Fatal(err)
	}
	devices := pair.BookAt(filepath.Join(t.TempDir(), "devices.json"))
	if err := devices.Admit(pair.Paired{Label: "surface", Key: base64.RawURLEncoding.EncodeToString(surface.Public())}); err != nil {
		t.Fatal(err)
	}
	if err := pair.MachineBook().Remember(pair.Known{Name: machine.Name(), Key: base64.RawURLEncoding.EncodeToString(machine.Public()), Service: server.URL}); err != nil {
		t.Fatal(err)
	}
	pipes := make(chan io.ReadWriteCloser, 1)
	ready := make(chan struct{})
	var once sync.Once
	host := &pair.Host{Service: server.URL, Device: machine, DisplayName: "desk-b", Devices: devices, Desk: &pair.Desk{}, Say: func(line string) {
		if strings.Contains(line, "reachable as") {
			once.Do(func() { close(ready) })
		}
	}, Open: func(pipe io.ReadWriteCloser) {
		pipes <- pipe
		defer pipe.Close()
		_ = remote.Serve(pipe, pipe, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: &quietAgent{}, Workspace: "/work", SessionFile: "/work/chat.jsonl"}, nil
		}})
	}}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- host.Run(ctx) }()
	defer func() { cancel(); <-done }()
	<-ready
	old := runSurfaceProgram
	defer func() { runSurfaceProgram = old }()
	visited := false
	runSurfaceProgram = func(_ context.Context, options tui3.Options) error {
		visited = true
		if options.Host != "desk-b" || strings.Contains(options.Notice, machine.Name()) {
			t.Errorf("host=%q notice=%q", options.Host, options.Notice)
		}
		_ = (<-pipes).Close()
		compact, ok := options.Agent.(interface{ Compact(context.Context) error })
		if !ok {
			t.Fatal("remote agent has no compact door")
		}
		if err := compact.Compact(t.Context()); err == nil || !strings.Contains(err.Error(), "desk-b") || strings.Contains(err.Error(), machine.Name()) {
			t.Errorf("window error uses a different name: %v", err)
		}
		return nil
	}
	if err := openChatV3At(atLaunch{target: machine.Name()}); err != nil {
		t.Fatal(err)
	}
	stopFinalSaveProgress()
	takeFinalSaveFailure()
	if !visited {
		t.Fatal("surface never opened")
	}
}
