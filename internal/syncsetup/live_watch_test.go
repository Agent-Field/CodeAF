package syncsetup

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// liveRelayVar names a real relay to smoke the watch feed against. Unset, the
// test is skipped: it spends a few requests on a real network.
const liveRelayVar = "CODEAF_LIVE_WATCH_RELAY"

// TestLiveWatchSmoke is two devices of one fresh identity on a real relay:
// one creates cells, the other's feed hears the version and does one list read.
// It logs how long a change takes to reach the other device's list.
func TestLiveWatchSmoke(t *testing.T) {
	base := os.Getenv(liveRelayVar)
	if base == "" {
		t.Skipf("%s is not set", liveRelayVar)
	}
	ctx := context.Background()
	id, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	client := func() *directory.HTTP {
		dev, err := identity.NewDevice(id)
		if err != nil {
			t.Fatal(err)
		}
		sign := reqsign.SignFor(deviceSigner{id, dev}, time.Now)
		c := directory.NewHTTP(base, sign, &http.Client{Timeout: 30 * time.Second})
		if err := c.PutDevice(ctx, dev.ID(), directory.Device{V: 1, AddedBy: id.ID()}); err != nil {
			t.Fatalf("PutDevice: %v", err)
		}
		return c
	}
	a, b := client(), client()

	feed := dirwatch.Follow(id.ID(), b.Watch)
	defer feed.Close()
	wait := func(what string) {
		select {
		case <-feed.Changes():
		case <-time.After(15 * time.Second):
			t.Fatalf("no frame for %s", what)
		}
	}
	wait("the first frame on accept")
	held, err := b.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var toAck, toFrame, toList []time.Duration
	for i := 0; i < 5; i++ {
		start := time.Now()
		cell := fmt.Sprintf("01J00000000000000000000%03d", i)
		if _, err := a.Create(ctx, cell, directory.CellInit{Head: head, Class: "chat"}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		acked := time.Since(start)
		wait("a created cell")
		frame := time.Since(start)
		toAck = append(toAck, acked)
		if feed.State().Version <= held.Version {
			t.Fatalf("frame version %d is not past the list's %d", feed.State().Version, held.Version)
		}
		if held, err = b.List(ctx); err != nil {
			t.Fatal(err)
		}
		toFrame, toList = append(toFrame, frame), append(toList, time.Since(start))
		if !feed.State().Up || held.Version != feed.State().Version {
			t.Fatalf("after one read the list is at %d and the feed at %+v", held.Version, feed.State())
		}
	}
	sort.Slice(toAck, func(i, j int) bool { return toAck[i] < toAck[j] })
	sort.Slice(toFrame, func(i, j int) bool { return toFrame[i] < toFrame[j] })
	sort.Slice(toList, func(i, j int) bool { return toList[i] < toList[j] })
	t.Logf("the writing device's own request: median %v, worst %v", toAck[2], toAck[4])
	t.Logf("publish-call-start to frame: median %v, worst %v (includes the publish request itself)", toFrame[2], toFrame[4])
	t.Logf("publish-call-start to the other device's list read done: median %v, worst %v", toList[2], toList[4])
}
