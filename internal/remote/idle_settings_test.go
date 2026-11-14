package remote

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
)

type refusedSettingsAgent struct {
	*rungAgent
	refusal *executor.WorkRefusal
}

func (a *refusedSettingsAgent) SetModel(string)                   {}
func (a *refusedSettingsAgent) SetConversationEffort(string) bool { return false }
func (a *refusedSettingsAgent) SettingError() error               { return a.refusal }

func (a *refusedSettingsAgent) SetModelChecked(string) error { return a.refusal }
func (a *refusedSettingsAgent) SetConversationEffortChecked(string) (bool, error) {
	return false, a.refusal
}

func TestIdleRelease_R5_1_RemoteSettingsKeepRefusalAndReplica(t *testing.T) {
	want := &executor.WorkRefusal{Line: "laptop continued this chat", HolderID: "B", HolderName: "laptop"}
	far := &refusedSettingsAgent{rungAgent: &rungAgent{fakeAgent: &fakeAgent{model: "old/model"}, stored: "low"}, refusal: want}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	a := loop.Client.Agent()
	for _, call := range []func() error{
		func() error { return a.SetModelChecked("new/model") },
		func() error { _, err := a.SetConversationEffortChecked("high"); return err },
		func() error { a.SetModel("new/model"); return a.SettingError() },
		func() error {
			if a.SetConversationEffort("high") {
				t.Error("refused effort acknowledged")
			}
			return a.SettingError()
		},
	} {
		var got *executor.WorkRefusal
		if err := call(); !errors.As(err, &got) || *got != *want {
			t.Fatalf("setting refusal=%+v, %v", got, err)
		}
	}
	if a.Model() != "old/model" || a.ResolvedEffort() != "low" || far.Model() != "old/model" || far.stored != "low" {
		t.Fatal("refusal changed the model or effort")
	}
}

// This pause models preemption after a legacy setter records its error. Two
// windows share the agent but have independent ordered request lanes.
type idleR8ResultAgent struct {
	*rungAgent
	chosen  atomic.Value
	result  atomic.Pointer[executor.WorkRefusal]
	refusal *executor.WorkRefusal
	entered chan struct{}
	release chan struct{}
}

func (a *idleR8ResultAgent) Model() string { return a.chosen.Load().(string) }
func (a *idleR8ResultAgent) Close() error  { return nil }
func (a *idleR8ResultAgent) SetModel(model string) {
	if model == "denied/model" {
		a.result.Store(a.refusal)
		close(a.entered)
		<-a.release
		return
	}
	a.chosen.Store(model)
	a.result.Store(nil)
}
func (a *idleR8ResultAgent) SettingError() error {
	if result := a.result.Load(); result != nil {
		return result
	}
	return nil
}
func (a *idleR8ResultAgent) SetModelChecked(model string) error {
	if model == "denied/model" {
		close(a.entered)
		<-a.release
		return a.refusal
	}
	a.chosen.Store(model)
	return nil
}

func TestIdleRelease_R8_ConcurrentModelRPCsKeepTheirOwnResult(t *testing.T) {
	want := &executor.WorkRefusal{Line: "another computer continued this chat", HolderID: "B"}
	far := &idleR8ResultAgent{rungAgent: &rungAgent{fakeAgent: &fakeAgent{model: "old/model"}, stored: "low"}, refusal: want, entered: make(chan struct{}), release: make(chan struct{})}
	far.chosen.Store("old/model")
	open := func() *Agent {
		loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = loop.Close() })
		return loop.Client.Agent()
	}
	first, second := open(), open()
	finish := sync.OnceFunc(func() { close(far.release) })
	defer finish()
	done := make(chan error, 1)
	go func() { done <- first.SetModelChecked("denied/model") }()
	select {
	case <-far.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first window did not enter the setter")
	}
	if err := second.SetModelChecked("accepted/model"); err != nil {
		t.Fatal(err)
	}
	finish()
	select {
	case err := <-done:
		var got *executor.WorkRefusal
		if !errors.As(err, &got) || *got != *want {
			t.Fatalf("first window lost its own refusal: %+v, %v", got, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first window did not finish")
	}
	if first.Model() == "denied/model" || far.Model() != "accepted/model" {
		t.Fatalf("refused model acknowledged: first=%q owner=%q", first.Model(), far.Model())
	}
}
