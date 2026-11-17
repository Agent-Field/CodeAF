package remote

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

type contextAdmissionAgent struct {
	*fakeAgent
	err error
}

func (a *contextAdmissionAgent) CheckWork() error { return a.err }

func TestRemoteContextErrorsMatchOnlyWholeText(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		for _, message := range []string{cause.Error(), "wrapped: " + cause.Error(), " " + cause.Error(), cause.Error() + " "} {
			t.Run(message, func(t *testing.T) {
				far := &contextAdmissionAgent{fakeAgent: &fakeAgent{}, err: errors.New(message)}
				loop := workspaceLoop(t, far, false)
				err := loop.Client.Agent().CheckWork()
				if err == nil || err.Error() != message {
					t.Fatalf("error text changed: %v, want %q", err, message)
				}
				if want := message == cause.Error(); errors.Is(err, cause) != want || (want && err != cause) {
					t.Fatalf("sentinel match for %q = %v, want %v", message, err, want)
				}
			})
		}
		t.Run("refusal/"+cause.Error(), func(t *testing.T) {
			want := &executor.WorkRefusal{Line: cause.Error(), HolderID: "device", HolderName: "laptop"}
			far := &contextAdmissionAgent{fakeAgent: &fakeAgent{}, err: want}
			loop := workspaceLoop(t, far, false)
			err := loop.Client.Agent().CheckWork()
			var got *executor.WorkRefusal
			if !errors.As(err, &got) || *got != *want || errors.Is(err, cause) {
				t.Fatalf("typed refusal changed: %+v, %v", got, err)
			}
		})
	}
}
