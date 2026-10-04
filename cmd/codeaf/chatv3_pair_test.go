package main

// The chat's pairing door (chatv3_pair.go). The plain refusal is said the
// chat's way: the terminal sentence names its --replace flag, and here the
// same fact points at the /pair keystroke the person is already sitting at.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pair"
)

func TestChatJoinRefusalNamesTheInChatReplace(t *testing.T) {
	err := chatJoinRefusal(pair.ErrDifferentChats)
	for _, want := range []string{"/pair <code> --replace", "already has chats of its own"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal lacks %q: %s", want, err)
		}
	}
	// Any other failure is carried as it is.
	other := errors.New("the relay is down")
	if chatJoinRefusal(other) != other {
		t.Fatal("an unrelated error was rewritten")
	}
	if chatJoinRefusal(nil) != nil {
		t.Fatal("a nil error was rewritten")
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
