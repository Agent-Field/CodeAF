package remote

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestIdleRelease_R10_RemoteKeepsRetryableOwnershipRefusal(t *testing.T) {
	want := &executor.WorkRefusal{Line: "couldn't confirm taking this chat back from the sync service; try again", Retryable: true}
	far := &viewerAdmissionAgent{fakeAgent: &fakeAgent{}, refusal: want}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	err = loop.Client.Agent().CheckWork()
	var got *executor.WorkRefusal
	if !errors.As(err, &got) || *got != *want {
		t.Fatalf("lost retryable refusal over RPC: %+v, %v", got, err)
	}
}
