package appwire

import (
	"slices"
	"testing"
)

// sendingEngine is an engine that can send a message to a chat another
// computer holds, and says whether it is on offer now.
type sendingEngine struct {
	*fakeEngine
	offered bool
}

func (*sendingEngine) SendToChat(ChatSendParams) (ChatNote, error) { return ChatNote{}, nil }
func (e *sendingEngine) ChatsSendOffered() bool                    { return e.offered }

// plainSendingEngine sends messages and says nothing about whether they are on
// offer, as every engine written before the question existed.
type plainSendingEngine struct{ *fakeEngine }

func (plainSendingEngine) SendToChat(ChatSendParams) (ChatNote, error) { return ChatNote{}, nil }

// A capability that cannot work is absent, not broken: hello advertises
// chats.send only while the engine says a message to another computer can be
// sent, so a client never draws a composer that every send refuses. An engine
// that says nothing keeps advertising it, as before.
func TestHelloAdvertisesChatsSendOnlyWhileItIsOnOffer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		engine Engine
		want   bool
	}{
		{"on offer", &sendingEngine{fakeEngine: &fakeEngine{}, offered: true}, true},
		{"not on offer", &sendingEngine{fakeEngine: &fakeEngine{}, offered: false}, false},
		{"an engine that does not say", plainSendingEngine{&fakeEngine{}}, true},
		{"an engine that cannot send", &fakeEngine{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Contains(withChatsFeatures(Hello{}, tc.engine).Features, FeatureChatsSend)
			if got != tc.want {
				t.Fatalf("hello advertises %s = %v; want %v", FeatureChatsSend, got, tc.want)
			}
		})
	}
}
