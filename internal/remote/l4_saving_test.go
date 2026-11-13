package remote

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// L4-H4b: the remote engine distinguishes a failed upload from failed local storage.
// L4-M1a: the welcome carries the device name used by the --at window bar.
func TestL4RemoteUploadReceiptAndDeviceName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	if _, err := devname.Set(home, "desk-b"); err != nil {
		t.Fatal(err)
	}
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{}, FinishSaving: func(WrappedAgent) error {
			return session.SavingError("/temporary/chat/transcript.jsonl", syncwords.UploadFailure(context.DeadlineExceeded))
		}, SessionFile: "/temporary/chat/transcript.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if got := loop.Client.Welcome().Machine; got != "desk-b" {
		t.Fatal(got)
	}
	err = loop.Client.Agent().Close()
	var unsaved *session.UnsavedChatError
	if !errors.As(err, &unsaved) || !syncwords.UploadPending(err) {
		t.Fatalf("upload fact was lost: %v", err)
	}
}

type l4LocalDeadline struct{ *fakeAgent }

func (l4LocalDeadline) Close() error {
	return session.SavingError("/temporary/chat/transcript.jsonl", context.DeadlineExceeded)
}

// L4-H4c: a local capture deadline cannot acquire an upload receipt across the wire.
func TestL4RemoteLocalDeadlineStaysFailure(t *testing.T) {
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: l4LocalDeadline{&fakeAgent{}}, SessionFile: "/temporary/chat/transcript.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	err = loop.Client.Agent().Close()
	if err == nil || syncwords.UploadPending(err) || loop.Client.Agent().LocalDurable() {
		t.Fatalf("local failure became a receipt: %v", err)
	}
}

// L4-H4b, L4-H4e: the engine acknowledges local durability before its upload can finish.
func TestL4RemoteLocalReceiptPrecedesUpload(t *testing.T) {
	upload := make(chan struct{})
	entered := make(chan struct{})
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{}, FinishSaving: func(WrappedAgent) error { close(entered); <-upload; return nil }}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	closed := make(chan error, 1)
	go func() { closed <- loop.Client.Agent().Close() }()
	<-entered
	deadline := time.After(time.Second)
	for !loop.Client.Agent().LocalDurable() {
		select {
		case <-deadline:
			close(upload)
			t.Fatal("no local receipt during the upload wait")
		default:
			runtime.Gosched()
		}
	}
	close(upload)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}

// L4-H4c: local failure remains a failure even if the later upload failed too.
func TestL4RemoteMixedSavingFailure(t *testing.T) {
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: l4LocalDeadline{&fakeAgent{}}, SessionFile: "/temporary/chat/transcript.jsonl", FinishSaving: func(WrappedAgent) error { return syncwords.UploadFailure(context.DeadlineExceeded) }}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	err = loop.Client.Agent().Close()
	if err == nil || syncwords.UploadPending(err) {
		t.Fatalf("mixed failure acquired a receipt: %v", err)
	}
}
