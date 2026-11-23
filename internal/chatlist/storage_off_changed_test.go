package chatlist

import "testing"

// forgetStorageOffSaid makes the package as it is before any sentence was said.
func forgetStorageOffSaid() {
	storageOffSaid.Lock()
	storageOffSaid.line = ""
	storageOffSaid.Unlock()
}

// A STORAGE-OFF SENTENCE THAT WAS SAID CAN BE TAKEN BACK WORD FOR WORD: the
// words are chosen when they are said, and the answer they were chosen from can
// change afterwards (another computer is paired, or the last one was removed).
// StorageOffChanged names the sentence that now replaces the one that was said,
// and says nothing when none was said or nothing changed.
func TestStorageOffChangedNamesTheSentenceThatReplacesTheOneSaid(t *testing.T) {
	live := false
	LiveCarriesChats = func() bool { return live }
	forgetStorageOffSaid()
	t.Cleanup(func() { LiveCarriesChats = nil; forgetStorageOffSaid() })

	live = true
	if line, ok := StorageOffChanged(); ok {
		t.Fatalf("a sentence nobody said was replaced by %q", line)
	}

	live = false
	if got := StorageOffSentence(); got != RelayStorageOff {
		t.Fatalf("StorageOffSentence = %q", got)
	}
	if line, ok := StorageOffChanged(); ok {
		t.Fatalf("a sentence that is still true was replaced by %q", line)
	}

	live = true
	line, ok := StorageOffChanged()
	if !ok || line != RelayStorageOffLive {
		t.Fatalf("after pass-through began to carry chats: %q, %v; want the pass-through sentence", line, ok)
	}
	if line, ok := StorageOffChanged(); ok {
		t.Fatalf("the replacement was offered twice: %q", line)
	}

	live = false
	line, ok = StorageOffChanged()
	if !ok || line != RelayStorageOff {
		t.Fatalf("after the last other computer was removed: %q, %v; want the old sentence", line, ok)
	}
	if got := StorageOffSentence(); got != RelayStorageOff {
		t.Fatalf("StorageOffSentence = %q", got)
	}
	if line, ok := StorageOffChanged(); ok {
		t.Fatalf("a sentence that is still true was replaced by %q", line)
	}
}
