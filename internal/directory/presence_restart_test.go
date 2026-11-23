package directory

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkPresenceRoster(b *testing.B) {
	clock := &tick{t: time.UnixMilli(1_000_000)}
	dir := NewMemory(clock.Now)
	defer dir.Close()
	client := dir.For("reader")
	for i := range 16 {
		if err := client.PutDevice(b.Context(), fmt.Sprintf("dev_%d", i), Device{V: 1}); err != nil {
			b.Fatal(err)
		}
	}
	// Compare the same complete roster before and after startup recovery.
	clock.Advance(2 * time.Minute)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := client.Presence(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}

// P2: a fresh presence feed has no evidence about the stored roster yet.
// Both stores use this same boundary after their presence memory restarts.
func TestPresenceP2FreshMemoryIsUnknownUntilObservedOrWindowPasses(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			a := b.dir.For("dev_a")
			for _, id := range []string{"dev_a", "dev_b"} {
				if err := a.PutDevice(t.Context(), id, Device{V: 1}); err != nil {
					t.Fatal(err)
				}
			}
			view, err := a.Presence(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, known := view.Devices["dev_b"]; known {
				t.Fatalf("P2 unobserved computer was reported offline after restart: %+v", view)
			}
			sub, err := b.feed.SubscribeBeat("dev_b", nil, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			view, err = a.Presence(t.Context())
			if err != nil || !view.Devices["dev_b"].Online {
				t.Fatalf("P2 observed computer is not online: %+v, %v", view, err)
			}
			b.clock.Advance(Window(10*time.Second) + time.Millisecond)
			view, err = a.Presence(t.Context())
			if p, known := view.Devices["dev_b"]; err != nil || !known || p.Online {
				t.Fatalf("P2 actually lapsed computer is not offline: %+v, %v", view, err)
			}
			b.clock.Advance(Window(DefaultBeat))
			view, err = a.Presence(t.Context())
			if p, known := view.Devices["dev_a"]; err != nil || !known || p.Online {
				t.Fatalf("P2 never returning computer is not offline after a window: %+v, %v", view, err)
			}
		})
	}
}
