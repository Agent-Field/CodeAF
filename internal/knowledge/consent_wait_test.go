package knowledge

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestCaptureConsentFreshAnswerDoesNotWaitForConfirmation(t *testing.T) {
	now := time.Now()
	previous := consentNow
	consentNow = func() time.Time { return now }
	defer func() { consentNow = previous }()
	v := Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	enabled, since := v.CaptureConsent()
	if !enabled || since.IsZero() {
		t.Fatalf("initial consent = (%v, %v)", enabled, since)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	confirmed := make(chan struct{})
	read := make(chan struct{})
	var held atomic.Bool
	path := filepath.Join(v.root, "knowledge.sqlite")
	restore := SetDatabaseHookForTest(func(opened string) {
		if opened == path && held.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	})
	defer restore()
	defer func() { close(release); <-confirmed; <-read }()
	go func() { v.ConfirmConsent(); close(confirmed) }()
	<-entered
	go func() {
		defer close(read)
		if got, at := Open(v.profile).CaptureConsentWithin(time.Second); got != enabled || !at.Equal(since) {
			t.Errorf("fresh shared consent = (%v, %v); want (%v, %v)", got, at, enabled, since)
		}
	}()
	select {
	case <-read:
	case <-time.After(time.Second):
		t.Error("fresh consent waited for another chat's database refresh")
	}
}

func TestCaptureConsentSharesPendingReadInsideBubble(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		v := Open(t.TempDir())
		if err := v.StartBuild(); err != nil {
			t.Fatal(err)
		}
		entered, release := make(chan struct{}), make(chan struct{})
		var opens atomic.Int64
		path := filepath.Join(v.root, "knowledge.sqlite")
		defer SetDatabaseHookForTest(func(opened string) {
			if opened == path {
				opens.Add(1)
				close(entered)
				<-release
			}
		})()
		defer close(release)
		read := func(done chan<- time.Time) {
			enabled, since := Open(v.profile).CaptureConsent()
			if !enabled || since.IsZero() {
				t.Errorf("shared consent = (%v, %v)", enabled, since)
			}
			done <- since
		}
		first, second := make(chan time.Time, 1), make(chan time.Time, 1)
		go read(first)
		<-entered
		go read(second)
		// Both readers must be able to wait while the store is held, so the
		// bubble can run the timer that releases a SQLite busy wait.
		synctest.Wait()
		time.Sleep(time.Second)
		if got := opens.Load(); got != 1 {
			t.Fatalf("pending consent opened the database %d times; want 1", got)
		}
		// Return releases the store, then synctest joins both readers.
		t.Cleanup(func() {
			if a, b := <-first, <-second; !a.Equal(b) {
				t.Errorf("coalesced readers got different consent times: %v and %v", a, b)
			}
		})
	})
}
