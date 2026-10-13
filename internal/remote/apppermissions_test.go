package remote

import (
	"sync/atomic"
	"testing"
)

type appPermissionEngine struct {
	*fakeAgent
	enabled atomic.Bool
	running atomic.Bool
}

func (a *appPermissionEngine) EnableAppPermissions() { a.enabled.Store(true) }
func TestAppPermissionsEnableTheJoinedEngine(t *testing.T) {
	far := &appPermissionEngine{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if err := loop.Client.Agent().EnableAppPermissions(); err != nil {
		t.Fatal(err)
	}
	if !far.enabled.Load() {
		t.Fatal("app permission policy was not enabled on the shared owner")
	}
}
func TestAppPermissionsRefuseAnEngineWithoutTheDoor(t *testing.T) {
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: &fakeAgent{}}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if err := loop.Client.Agent().EnableAppPermissions(); err == nil {
		t.Fatal("a missing app permission gate was silently accepted")
	}
}

func (a *appPermissionEngine) ReplyRunning() bool { return a.running.Load() }
func TestAppReplyRunningReadsTheJoinedOwnersInlineReply(t *testing.T) {
	far := &appPermissionEngine{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if loop.Client.Agent().ReplyRunning() {
		t.Fatal("idle reply reported running")
	}
	far.running.Store(true)
	if !loop.Client.Agent().ReplyRunning() {
		t.Fatal("the joined owner's reply was hidden")
	}
}
