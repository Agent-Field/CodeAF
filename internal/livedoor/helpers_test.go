package livedoor

import (
	"io"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// eventually waits, in real time bounded at three seconds and without sleeping,
// for cond to become true. THE LINK'S STATE CHANGES UNDER ITS OWN LOCK BUT ITS
// TUNNEL CLOSE AND ITS CHANNEL-END CALLBACKS RUN ON GOROUTINES, so a test that
// advances the fake clock may read them a scheduler tick before they happen,
// and the race detector makes that tick long. Whether the thing happens is the
// contract; that it has happened by the instant of the next line is not.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		runtime.Gosched()
	}
}

// countBeforeRWC counts each write BEFORE handing it to the transport, so a
// test that sees a frame arrive at the far end can rely on the count already
// including it.
type countBeforeRWC struct {
	io.ReadWriteCloser
	n *atomic.Int64
}

func (c *countBeforeRWC) Write(p []byte) (int, error) {
	c.n.Add(1)
	return c.ReadWriteCloser.Write(p)
}
