package contextrefs

import "testing"

func TestKeptShortHashCollision(t *testing.T) {
	refs := CreateContextRefs("conversation")
	first := refs.Register("diff", "collision-8515")
	second := refs.Register("diff", "collision-11163")
	if first.RefID != second.RefID || !first.FirstMention || !second.FirstMention {
		t.Fatalf("collision behavior changed: first=%+v second=%+v", first, second)
	}
	if got := refs.Stats(); got.Registered != 2 {
		t.Fatalf("colliding contents should both register: %+v", got)
	}
}

func TestStoreReturnsStableConversationRegistry(t *testing.T) {
	store := CreateContextRefsStore()
	a := store.ForConversation("a")
	if again := store.ForConversation("a"); again != a {
		t.Fatal("store returned a different registry")
	}
	if b := store.ForConversation("b"); b == a {
		t.Fatal("different conversations share a registry")
	}
}
