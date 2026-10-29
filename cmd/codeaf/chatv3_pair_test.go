package main

// The chat's pairing door shares the same merge and explicit replacement
// behavior as the terminal command.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// M8: ordinary refusals never steer a person into the destructive opt-in door.
func TestM8ChatPairRefusalDoesNotAdviseReplace(t *testing.T) {
	if strings.Contains(pair.ErrDifferentChats.Error(), "--replace") || !strings.Contains(pair.ErrDifferentChats.Error(), "nothing was changed") {
		t.Fatal(pair.ErrDifferentChats)
	}
}

func TestM1M8ChatPairDoorUsedSoloHome(t *testing.T) {
	rig := newPairRig(t)
	t.Setenv("CODEAF_HOME", rig.homeB)
	t.Setenv("CODEAF_PROFILE_DIR", rig.homeB)
	t.Setenv("CODEAF_SYNC_URL", rig.url)
	if _, err := identity.EnsureSolo(rig.homeB); err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(rig.homeB, "own-chat")
	if err := os.WriteFile(chat, []byte("history kept\n"), 0600); err != nil {
		t.Fatal(err)
	}
	shown := rig.show(t)
	code := shown.code(t)
	done := make(chan error, 1)
	go func() {
		_, err := (chatPairDoor{}).Join(t.Context(), code, joinScreen{newTerminal(strings.NewReader(""), io.Discard)})
		done <- err
	}()
	shown.out.waitFor(t, `wants your chats\. Same three words`)
	if _, err := io.WriteString(shown.typed, "y\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := shown.end(t); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(chat); err != nil || string(got) != "history kept\n" {
		t.Fatalf("M1 /pair history: %q, %v", got, err)
	}
}

// The replacing errand reaches pair.Join with replace set: the interface keeps
// the two doors from disagreeing about what a replace is.
func TestChatPairDoorImplementsBothJoins(t *testing.T) {
	var _ interface {
		Join(ctx context.Context, typed string, ui pair.JoinUI) (pair.Joined, error)
		JoinReplacing(ctx context.Context, typed string, ui pair.JoinUI) (pair.Joined, error)
		HasOwnChats() bool
	} = chatPairDoor{}
}
