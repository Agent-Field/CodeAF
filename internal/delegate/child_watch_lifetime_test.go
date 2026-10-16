package delegate

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"
)

func TestRunChildReturnsOnlyAfterItsHostWatchStops(t *testing.T) {
	t.Setenv(EnvModelAPI, "http://127.0.0.1:9/v1")
	t.Setenv(EnvModelToken, "token")
	previousPID, previousWatch := hostPID, hostWatch
	watching, releaseWatch, allowBody, bodyEnded, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var entered, released, bodyAllowed sync.Once
	unblockWatch := func() { released.Do(func() { close(releaseWatch) }) }
	finishBody := func() { bodyAllowed.Do(func() { close(allowBody) }) }
	hostPID = func() int {
		entered.Do(func() { close(watching) })
		<-releaseWatch
		return 4242
	}
	hostWatch = time.Hour
	t.Cleanup(func() {
		finishBody()
		unblockWatch()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("child did not leave after its watch was released")
		}
		hostPID, hostWatch = previousPID, previousWatch
	})
	inv, err := Parse(testProgram(func(context.Context, Host, []string) error {
		<-allowBody
		close(bodyEnded)
		return nil
	}), []string{"b"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		RunChild(context.Background(), inv, &bytes.Buffer{})
		close(done)
	}()
	select {
	case <-watching:
	case <-time.After(5 * time.Second):
		t.Fatal("host watch did not start")
	}
	finishBody()
	<-bodyEnded
	select {
	case <-done:
		t.Fatal("child returned while its host watch was still blocked")
	case <-time.After(50 * time.Millisecond):
	}
	unblockWatch()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not return after its watch stopped")
	}
}
