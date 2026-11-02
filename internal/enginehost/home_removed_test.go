package enginehost

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
)

type closingHomeAgent struct {
	stubAgent
	closing chan struct{}
	release chan struct{}
}

func (a *closingHomeAgent) Close() error {
	close(a.closing)
	<-a.release
	return nil
}

// C4.3: removal during a blocked close calls the command's exit hook promptly;
// an intact home keeps waiting for the same close, without calling the hook.
func TestShutdownExitsOnlyWhenHomeRemoved(t *testing.T) {
	for _, removed := range []bool{false, true} {
		t.Run(map[bool]string{false: "intact", true: "removed"}[removed], func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "home")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv(home.EnvVar, root)
			home.Witness(root)
			dir := filepath.Join(root, "host")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			agent := &closingHomeAgent{closing: make(chan struct{}), release: make(chan struct{})}
			defer close(agent.release)
			exit := make(chan struct{}, 1)
			h := &Host{dir: dir, listener: listener, opts: Options{HomeRemoved: func() { exit <- struct{}{} }},
				sessions: map[string]*remote.Session{"chat": remote.NewSession(&remote.Engine{Agent: agent}, true)}}
			finished := make(chan struct{})
			go func() { h.shutdown(); close(finished) }()
			select {
			case <-agent.closing:
			case <-time.After(time.Second):
				t.Fatal("close never started")
			}
			if removed {
				if err := os.RemoveAll(root); err != nil {
					t.Fatal(err)
				}
				select {
				case <-exit:
				case <-time.After(time.Second):
					t.Fatal("exit hook did not run after removal")
				}
				if _, err := os.Stat(root); !os.IsNotExist(err) {
					t.Fatalf("home recreated: %v", err)
				}
			} else {
				select {
				case <-exit:
					t.Fatal("exit hook called for intact home")
				case <-finished:
					t.Fatal("close returned before release")
				case <-time.After(200 * time.Millisecond):
				}
			}
			// Release and join before the next fixture changes the process environment.
			agent.release <- struct{}{}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("close did not finish")
			}
		})
	}
}
