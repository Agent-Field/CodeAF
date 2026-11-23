package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/home"
)

// The refusal for closed storage is worded by the computer it is said on: the
// door must have told chatlist how to ask whether this home's chats move live,
// and a home that has nobody to send them to gets the ordinary sentence.
func TestStorageOffSentenceAsksTheHomeWhetherChatsMoveLive(t *testing.T) {
	if chatlist.LiveCarriesChats == nil {
		t.Fatal("the door never told chatlist how to ask whether chats move live")
	}
	t.Setenv(home.EnvVar, t.TempDir())
	if got := chatlist.StorageOffSentence(); got != chatlist.RelayStorageOff {
		t.Fatalf("a home that is paired with nothing says %q; want the ordinary refusal", got)
	}
}
