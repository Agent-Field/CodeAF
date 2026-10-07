package enginehost

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
)

func TestConversationDeleteMintedSessionIsRetiredAndCannotJoinAgain(t *testing.T) {
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	var engine *remote.Engine
	deleted := false
	host := &Host{sessions: map[string]*remote.Session{}, opts: Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		if deleted {
			return nil, errors.New("permanently deleted")
		}
		engine = &remote.Engine{Agent: stubAgent{}, SessionFile: file, DeleteConversation: func(string, map[string]string, map[string][]string) error { deleted = true; return nil }}
		return engine, nil
	}}}
	sess, err := host.open(remote.Hello{New: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.DeleteConversation(file, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !sess.Ended() || len(host.sessions) != 0 {
		t.Fatal("minted deletion retained a live session")
	}
	if _, err = host.open(remote.Hello{Session: file, Join: true}); err == nil {
		t.Fatal("deleted session joined again")
	}
}

func TestConversationDeleteTombstoneBlocksForeignHostCachedViewAndResolvedLatest(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "transcript.jsonl")
	host := stubHost(t, dir)
	host.opts.Boot = func(remote.Hello) (*remote.Engine, error) { return nil, errors.New("deleted") }
	host.sessions[file] = remote.NewSession(&remote.Engine{Agent: stubAgent{}, SessionFile: file}, true)
	host.latest = file
	if err := os.WriteFile(filepath.Join(dir, ".conversation-deleted"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.open(remote.Hello{Session: file, Join: true}); err == nil {
		t.Fatal("foreign cached view joined after deletion")
	}
	host.deleting = map[string]bool{file: true}
	if _, err := host.open(remote.Hello{}); err == nil {
		t.Fatal("latest bypassed deletion reservation")
	}
}
