package enginehost

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type unsavedHostAgent struct {
	stubAgent
	failure error
}

func (a unsavedHostAgent) Close() error { return a.failure }

// Contract D2 returns the host's own close and final publish failures before departure.
func TestHostReturnsUnsavedTranscriptOnCloseAndDetach(t *testing.T) {
	for _, finalPublish := range []bool{false, true} {
		for _, door := range []string{"close", "detach", "detach and wait"} {
			t.Run(door+map[bool]string{false: "/seal", true: "/publish"}[finalPublish], func(t *testing.T) {
				shortHome(t)
				workspace := t.TempDir()
				transcript := filepath.Join(workspace, "transcript.jsonl")
				failure := session.SavingError(transcript, errors.New("seal: snapshot: dial unix missing.sock: connection refused"))
				far := unsavedHostAgent{failure: failure}
				if finalPublish {
					far.failure = nil
				}
				stopped := make(chan error, 1)
				go func() {
					stopped <- Run(workspace, Options{Boot: func(remote.Hello) (*remote.Engine, error) {
						engine := &remote.Engine{Agent: far, Workspace: workspace, ReleaseOnLeave: func(remote.WrappedAgent) bool { return true }}
						if finalPublish {
							engine.FinishSaving = func(remote.WrappedAgent) error { return failure }
						}
						return engine, nil
					}})
				}()
				waitForHostQuietly(t, workspace)
				t.Cleanup(func() { _ = Retire(workspace, true); <-stopped })
				client := dialHost(t, workspace)
				defer client.Close()
				var err error
				switch door {
				case "close":
					err = client.Agent().Close()
				case "detach":
					err = client.Agent().Detach()
				default:
					err = client.Agent().DetachAndWait()
				}
				var unsaved *session.UnsavedChatError
				if !errors.As(err, &unsaved) || unsaved.Transcript != transcript {
					t.Fatalf("departure lost the unsaved transcript: %v", err)
				}
				if door == "close" {
					if again := client.Agent().Close(); !errors.As(again, &unsaved) || unsaved.Transcript != transcript {
						t.Fatalf("repeated close hid the save failure: %v", again)
					}
				}
			})
		}
	}
}
