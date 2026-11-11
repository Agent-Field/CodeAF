package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func keepReservations(r *keepAdmission) (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refs, len(r.gates)
}

func TestKeepAdmissionBoundsAllHoldersAndWaitersAndForgetsKeys(t *testing.T) {
	var r keepAdmission
	key := keepKey{"index", "session", "id"}
	hold, err := r.acquire(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, keepAdmissionMax-1)
	for i := 1; i < keepAdmissionMax; i++ {
		go func() {
			release, err := r.acquire(ctx, key)
			if release != nil {
				release()
			}
			done <- err
		}()
	}
	until := time.Now().Add(time.Second)
	for {
		refs, _ := keepReservations(&r)
		if refs == keepAdmissionMax {
			break
		}
		if time.Now().After(until) {
			t.Fatal("waiters not admitted")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := r.acquire(context.Background(), keepKey{"other", "other", "other"}); !errors.Is(err, ErrMediaBusy) {
		t.Fatalf("full: %v", err)
	}
	cancel()
	for i := 1; i < keepAdmissionMax; i++ {
		if err := <-done; !errors.Is(err, context.Canceled) || !errors.Is(err, ErrMediaBusy) {
			t.Fatalf("cancel %v", err)
		}
	}
	if refs, keys := keepReservations(&r); refs != 1 || keys != 1 {
		t.Fatalf("holder lost: %d/%d", refs, keys)
	}
	hold()
	hold()
	if refs, keys := keepReservations(&r); refs != 0 || keys != 0 {
		t.Fatalf("retained: %d/%d", refs, keys)
	}
	for i := 0; i < 1000; i++ {
		release, err := r.acquire(context.Background(), keepKey{"index", "session", fmt.Sprint(i)})
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	if refs, keys := keepReservations(&r); refs != 0 || keys != 0 {
		t.Fatalf("historical keys: %d/%d", refs, keys)
	}
}

func TestKeepAdmissionSourceIdentityAndKeyBounds(t *testing.T) {
	var r keepAdmission
	for _, key := range []keepKey{{"i", "s", "id"}, {"other", "s", "id"}, {"i", "other", "id"}} {
		release, err := r.acquire(context.Background(), key)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
	}
	if refs, keys := keepReservations(&r); refs != 3 || keys != 3 {
		t.Fatalf("aliased owners: %d/%d", refs, keys)
	}
	for _, key := range []keepKey{{strings.Repeat("i", 4097), "s", "id"}, {"i", strings.Repeat("s", 129), "id"}, {"i", "s", strings.Repeat("d", 129)}} {
		if release, err := r.acquire(context.Background(), key); err == nil {
			release()
			t.Fatal("oversize admitted")
		}
	}
	if refs, keys := keepReservations(&r); refs != 3 || keys != 3 {
		t.Fatalf("bad keys retained %d/%d", refs, keys)
	}
}

func TestKeepCanceledWaiterDoesNotReleaseRunningCopyAndDuplicateSeesKept(t *testing.T) {
	index, row, roots, _ := keepFixture(t)
	dest, home := t.TempDir(), t.TempDir()
	entered, settle := make(chan struct{}), make(chan struct{})
	old := keepBeforeOpen
	keepBeforeOpen = func(string) { close(entered); <-settle }
	defer func() { keepBeforeOpen = old }()
	done := make(chan error, 1)
	go func() {
		_, _, err := KeepDeliverable(context.Background(), index, row, roots, home, KeepRequest{Dir: dest})
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, _, err := KeepDeliverable(ctx, index, row, roots, home, KeepRequest{Dir: dest}); !errors.Is(err, ErrMediaBusy) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter %v", err)
	}
	if refs, keys := keepReservations(&deliverKeepAdmission); refs != 1 || keys != 1 {
		t.Fatalf("holder prematurely freed %d/%d", refs, keys)
	}
	duplicate := make(chan error, 1)
	go func() {
		_, _, err := KeepDeliverable(context.Background(), index, row, roots, home, KeepRequest{Dir: dest})
		duplicate <- err
	}()
	until := time.Now().Add(time.Second)
	for {
		refs, _ := keepReservations(&deliverKeepAdmission)
		if refs == 2 {
			break
		}
		if time.Now().After(until) {
			t.Fatal("duplicate never reserved while first copy was held")
		}
		time.Sleep(time.Millisecond)
	}
	close(settle)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-duplicate; !errors.Is(err, ErrKeepAlready) {
		t.Fatalf("duplicate %v", err)
	}
	if refs, keys := keepReservations(&deliverKeepAdmission); refs != 0 || keys != 0 {
		t.Fatalf("leak %d/%d", refs, keys)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 1 {
		t.Fatalf("copies %d", len(entries))
	}
}

func TestKeepUndecidedSourceIsBusyAndCopiesNothing(t *testing.T) {
	index, row, roots, _ := keepFixture(t)
	row.Fact.Branch = "task/pending-aaaaaa"
	row.Fact.Rel = "plan.md"
	row.Fact.File = nil
	if !AppendDeliverableUpdate(index, row, func(f *DeliverableFact) { f.Branch = row.Fact.Branch; f.Rel = row.Fact.Rel; f.File = nil }) {
		t.Fatal("record")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec /bin/sleep 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	dest := t.TempDir()
	_, _, err := KeepDeliverable(ctx, index, row, roots, t.TempDir(), KeepRequest{Dir: dest, Workspace: roots[0]})
	if !errors.Is(err, ErrMediaBusy) || errors.Is(err, ErrKeepSource) {
		t.Fatalf("undecided: %v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("copied: %v", entries)
	}
}

func TestKeepAdmissionHolderCancellationRetainsOwnershipUntilSettlement(t *testing.T) {
	var r keepAdmission
	ctx, cancel := context.WithCancel(context.Background())
	release, err := r.acquire(ctx, keepKey{"index", "session", "id"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if refs, keys := keepReservations(&r); refs != 1 || keys != 1 {
		t.Fatalf("canceled holder released before settlement %d/%d", refs, keys)
	}
	release()
	if refs, keys := keepReservations(&r); refs != 0 || keys != 0 {
		t.Fatalf("settled holder retained %d/%d", refs, keys)
	}
}
