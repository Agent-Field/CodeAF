package main

import (
	"bufio"
	"bytes"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// olderFacts is the pair a conversation's identity reports; a host from before
// the pair has none to report, which is what sends the app to set one.
var olderFacts = regexp.MustCompile(`"models":\{[^{}]*\},?`)

// olderMethodHost stands on the workspace's socket in front of a real host and
// renames one method on its way in, so the real host's own dispatch answers it
// the way a host built before that method does: `no such method`. What it says
// back loses the pair, as such a host never knew of one.
func olderMethodHost(t *testing.T, workspace, method string) {
	t.Helper()
	socket, err := enginehost.SocketPath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	real := socket + ".real"
	if err := os.Rename(socket, real); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	asked := []byte(`"method":"` + method + `"`)
	unknown := []byte(`"method":"` + method + `FromLater"`)
	var mu sync.Mutex
	var open []net.Conn
	keep := func(c net.Conn) {
		mu.Lock()
		defer mu.Unlock()
		open = append(open, c)
	}
	go func() {
		for {
			window, err := listener.Accept()
			if err != nil {
				return
			}
			host, err := net.Dial("unix", real)
			if err != nil {
				_ = window.Close()
				return
			}
			keep(window)
			keep(host)
			go func() {
				defer host.Close()
				lines := bufio.NewReader(window)
				for {
					line, err := lines.ReadBytes('\n')
					if len(line) > 0 {
						if _, werr := host.Write(bytes.ReplaceAll(line, asked, unknown)); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
			go func() {
				defer window.Close()
				lines := bufio.NewReader(host)
				for {
					line, err := lines.ReadBytes('\n')
					if len(line) > 0 {
						if _, werr := window.Write(olderFacts.ReplaceAll(line, nil)); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range open {
			_ = c.Close()
		}
		_ = os.Rename(real, socket)
	})
}

// A CONVERSATION AN OLDER WINDOW HOLDS STILL OPENS. A host from before the
// main/fallback pair cannot take the pair, and that is all it cannot do: the
// conversation stays readable here, and the app says what is out of reach.
func TestAConversationOnAnOlderHostOpensAndSaysItsModelsCannotChange(t *testing.T) {
	b, workspace := organizeProductionBackend(t)
	agents, err := b.ListAgents("")
	if err != nil || len(agents) == 0 {
		t.Fatalf("agents %v (%v)", agents, err)
	}
	id := agents[0].ID
	file := b.agents[id].transcript
	stopped := make(chan error, 1)
	go func() {
		stopped <- enginehost.Run(workspace, enginehost.Options{Boot: func(hello remote.Hello) (*remote.Engine, error) {
			proc, err := openAppEngineProcess()
			if err != nil {
				return nil, err
			}
			launch, err := openV3Launch(proc, v3Options{Workspace: workspace, Model: "test/model", Session: file, OneModel: true})
			if err != nil {
				return nil, err
			}
			agent, err := session.New(launch.Config)
			if err != nil {
				return nil, err
			}
			proc.track(agent)
			return &remote.Engine{Agent: agent, Workspace: workspace, SessionFile: file}, nil
		}})
	}()
	t.Cleanup(func() {
		_, _ = enginehost.Stop(workspace)
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("host did not stop")
		}
	})
	waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.PID == os.Getpid() })
	client := environmentWindow(t, workspace, file)
	defer client.Close()
	olderMethodHost(t, workspace, remote.MethodSetConversationModels)

	if _, err := b.OpenAgent(id); err != nil {
		t.Fatalf("an older host's conversation would not open: %v", err)
	}
	var joined bool
	b.locked(func() {
		joined = b.agents[id].live != nil && !b.agents[id].live.conn.Assembled && !b.agents[id].live.conn.Lent
	})
	if !joined {
		t.Fatal("the conversation was not joined on its host")
	}
	entries, _, err := b.Entries(id, "", 50)
	if err != nil || len(entries) == 0 {
		t.Fatalf("the conversation is not readable: %d entries, %v", len(entries), err)
	}
	b.emit.(*appInboxTap).next.(*appRecorder).waitFor(t, 5*time.Second, func(_ string, data any) bool {
		e, ok := data.(appwire.EntryAdded)
		return ok && e.Entry.Agent == id && e.Entry.Kind == appwire.EntryNotice && e.Entry.Text == appOlderHostModels
	})
	_, err = b.SetConversationModels(id, session.ConversationModels{Main: "test/other"})
	if wireCode(err) != appwire.CodeUnknownMethod || err.Error() == "" || strings.Contains(err.Error(), "no such method") {
		t.Fatalf("a pick on an older host answered %v, want its plain sentence", err)
	}
	if !strings.Contains(err.Error(), appOlderHostModels) {
		t.Fatalf("a pick on an older host answered %q", err)
	}
}
