package cellsync

import (
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/wireauth"
	"strings"
	"testing"
	"time"
)

func TestStorageOptInRefusalHaltsAndSpeaksOnce(t *testing.T) {
	r, ok := RefusalOf(wireauth.ErrStorageOff)
	if !ok || !r.Halts || !r.RetryStartup || r.Say(nil) != chatlist.RelayStorageOff {
		t.Fatalf("storage row: %+v", r)
	}
	if wait, retry := r.StartupWait(0, time.Second, wireauth.ErrStorageOff); !retry || wait > MaxBackoff {
		t.Fatalf("startup wait: %v %v", wait, retry)
	}
	b := &Batcher{}
	if !b.news(r) || b.news(r) {
		t.Fatal("refusal repeated within one episode")
	}
	b.recovered(true)
	if !b.news(r) {
		t.Fatal("new episode lost")
	}
}

// The refusal for closed storage says what is true on the computer it is said
// on: a computer whose chats move live is not told its chats stay put.
func TestStorageOffRefusalSpeaksForTheComputerItIsSaidOn(t *testing.T) {
	r, ok := RefusalOf(wireauth.ErrStorageOff)
	if !ok {
		t.Fatal("no refusal for closed storage")
	}
	t.Cleanup(func() { chatlist.LiveCarriesChats = nil })
	for _, c := range []struct {
		name  string
		carry func() bool
		want  string
	}{
		{"nothing says anything about live transfer", nil, chatlist.RelayStorageOff},
		{"live transfer is not carrying chats", func() bool { return false }, chatlist.RelayStorageOff},
		{"live transfer is carrying chats", func() bool { return true }, chatlist.RelayStorageOffLive},
	} {
		chatlist.LiveCarriesChats = c.carry
		if got := r.Say(wireauth.ErrStorageOff); got != c.want {
			t.Fatalf("%s: the refusal says %q; want %q", c.name, got, c.want)
		}
	}
	for _, word := range []string{"live", "keys", "memories", "teams", "preferences", "Settings under Devices"} {
		if !strings.Contains(chatlist.RelayStorageOffLive, word) {
			t.Fatalf("the live sentence does not mention %q: %s", word, chatlist.RelayStorageOffLive)
		}
	}
	if strings.Contains(chatlist.RelayStorageOffLive, "stay on this computer") {
		t.Fatal("the live sentence says chats stay on this computer")
	}
}

func TestStorageOptInMissedOffOnStillPublishesWithinCap(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	head := r.seal(map[string]string{"a": "pending"})
	note(b, head)
	r.store.set(wireauth.ErrStorageOff, nil)
	wait := b.flush(t.Context())
	if wait > MaxBackoff {
		t.Fatalf("storage-off wait %v exceeds %v", wait, MaxBackoff)
	}
	// Another process saves off and on between the waiter's samples. No edge
	// notification is delivered to this batcher; its ordinary retry must suffice.
	r.store.set(nil, nil)
	if b.flush(t.Context()) != 0 || b.Pending() != 0 || r.head(cellID).Head != head {
		t.Fatal("pending publication never recovered")
	}
}

func TestStorageOptInRetriesStayQuietAcrossSuccessfulHeartbeats(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	notices := 0
	b.OnError = func(error) { notices++ }
	note(b, r.seal(map[string]string{"a": "pending"}))
	r.store.set(wireauth.ErrStorageOff, nil)
	for i := 0; i < 5; i++ {
		if wait := b.flush(t.Context()); wait > MaxBackoff {
			t.Fatalf("wait %v", wait)
		}
		b.beat(t.Context())
	}
	if notices != 1 {
		t.Fatalf("refusal said %d times", notices)
	}
	row, _ := RefusalOf(wireauth.ErrStorageOff)
	if wait, _ := row.StartupWait(0, 2*time.Minute, wireauth.ErrStorageOff); wait > MaxBackoff {
		t.Fatalf("custom interval bypassed cap: %v", wait)
	}
}
