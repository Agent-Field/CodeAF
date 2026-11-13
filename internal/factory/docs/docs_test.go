package docs

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), File))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

var at = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// THE LOG IS STAMPED IN ORDER, A SNAPSHOT REPLACES THE PREFIX IT COVERS, and a
// reader gets exactly what it lacks: the updates after its seq, or the
// snapshot and what came after when it is behind the snapshot or past the
// head.
func TestTheLogIsStampedAndASnapshotReplacesItsPrefix(t *testing.T) {
	s := openStore(t)
	var published []int64
	for i := 1; i <= 4; i++ {
		u, compact, err := s.Append("ws/ws_1/page/p", "abir", at, []byte{byte(i)}, int64(i-1), false, func(u Update) { published = append(published, u.Seq) })
		if err != nil || u.Seq != int64(i) || u.By != "abir" || compact {
			t.Fatalf("append %d = %+v %v %v", i, u, compact, err)
		}
	}
	if len(published) != 4 || published[3] != 4 {
		t.Fatalf("published %v", published)
	}
	st, err := s.Since("ws/ws_1/page/p", 2)
	if err != nil || st.Seq != 4 || st.Snapshot != nil || len(st.Updates) != 2 || st.Updates[0].Seq != 3 {
		t.Fatalf("since 2 = %+v %v", st, err)
	}
	if kept, err := s.PutSnapshot("ws/ws_1/page/p", "santosh", at, 3, []byte("S3")); err != nil || !kept {
		t.Fatalf("snapshot = %v %v", kept, err)
	}
	// A second desk compacting at the same seq is not an error, and keeps
	// nothing.
	if kept, err := s.PutSnapshot("ws/ws_1/page/p", "abir", at, 2, []byte("S2")); err != nil || kept {
		t.Fatalf("older snapshot = %v %v", kept, err)
	}
	if _, err := s.PutSnapshot("ws/ws_1/page/p", "abir", at, 9, []byte("S9")); !errors.Is(err, ErrAhead) {
		t.Fatalf("snapshot past the head = %v", err)
	}
	for _, since := range []int64{0, 1, 99} {
		st, err := s.Since("ws/ws_1/page/p", since)
		if err != nil || st.Snapshot == nil || st.Snapshot.Seq != 3 || !bytes.Equal(st.Snapshot.State, []byte("S3")) || st.Snapshot.By != "santosh" || len(st.Updates) != 1 || st.Updates[0].Seq != 4 || st.Pending != 1 {
			t.Fatalf("since %d after the snapshot = %+v %v", since, st, err)
		}
	}
	if st, _ := s.Since("ws/ws_1/page/p", 3); st.Snapshot != nil || len(st.Updates) != 1 {
		t.Fatalf("since 3 = %+v", st)
	}
	if _, _, err := s.Append("ws/ws_1/page/p", "abir", at, []byte{9}, 7, false, nil); !errors.Is(err, ErrAhead) {
		t.Fatalf("a base past the head = %v", err)
	}
}

// A SEED GOES ONLY INTO AN EMPTY DOCUMENT, so two desks that open a page for
// the first time cannot both put its text in.
func TestASeedGoesOnlyIntoAnEmptyDocument(t *testing.T) {
	s := openStore(t)
	if _, _, err := s.Append("d/x/page/y", "abir", at, []byte("A"), 0, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Append("d/x/page/y", "santosh", at, []byte("B"), 0, true, nil); !errors.Is(err, ErrSeeded) {
		t.Fatalf("second seed = %v", err)
	}
}

// THE COMPACTION RULE: past the soft line every update asks for a snapshot
// and is still kept; at the hard line it is refused until one arrives; and a
// dropped document starts again at 1.
func TestTheLogAsksForASnapshotThenRefusesWithoutOne(t *testing.T) {
	s := openStore(t)
	big := make([]byte, 64<<10)
	doc := "ws/ws_1/page/big"
	var seq int64
	compacted := false
	for {
		u, compact, err := s.Append(doc, "abir", at, big, seq, false, nil)
		if errors.Is(err, ErrSnapshotNeeded) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		seq = u.Seq
		if u.Seq*int64(len(big)) >= SoftBytes != compact {
			t.Fatalf("update %d: compact = %v", u.Seq, compact)
		}
		compacted = compacted || compact
	}
	if !compacted || seq != HardBytes/int64(len(big)) {
		t.Fatalf("refused after %d updates", seq)
	}
	if _, err := s.PutSnapshot(doc, "abir", at, seq, []byte("state")); err != nil {
		t.Fatal(err)
	}
	if u, compact, err := s.Append(doc, "abir", at, big, seq, false, nil); err != nil || u.Seq != seq+1 || compact {
		t.Fatalf("after the snapshot = %+v %v %v", u, compact, err)
	}
	if err := s.Drop(doc); err != nil {
		t.Fatal(err)
	}
	if u, _, err := s.Append(doc, "abir", at, big, 0, true, nil); err != nil || u.Seq != 1 {
		t.Fatalf("after drop = %+v %v", u, err)
	}
}

// A READER THAT FALLS BEHIND IS ENDED, never skipped; a caret lapses after
// the TTL and its lapse is heard; a bucket refills with time.
func TestTheHubEndsALaggardAndLapsesCarets(t *testing.T) {
	now := at
	h := NewHub(func() time.Time { return now })
	slow, leave := h.Subscribe("d")
	defer leave()
	for i := 0; i <= subscriberQueue; i++ {
		h.Publish(Event{Kind: KindUpdate, Doc: "d", Update: &Update{Seq: int64(i + 1)}})
	}
	n := 0
	for range slow {
		n++
	}
	if n != subscriberQueue {
		t.Fatalf("a laggard read %d before its feed closed", n)
	}
	leave()

	feed, leave2 := h.Subscribe("d")
	defer leave2()
	h.SetAwareness("d", "abir", 7, []byte("caret"))
	if e := <-feed; e.Kind != KindAwareness || e.Aware.By != "abir" || e.Aware.State == nil {
		t.Fatalf("caret = %+v", e)
	}
	now = now.Add(AwarenessTTL - time.Second)
	if got := h.Awareness("d"); len(got) != 1 {
		t.Fatalf("before the TTL: %+v", got)
	}
	now = now.Add(time.Second)
	h.Sweep()
	if e := <-feed; e.Kind != KindAwareness || e.Aware.State != nil || e.Aware.Client != 7 {
		t.Fatalf("lapse = %+v", e)
	}
	if got := h.Awareness("d"); len(got) != 0 {
		t.Fatalf("after the TTL: %+v", got)
	}

	for i := 0; i < 3; i++ {
		if !h.Allow("d", "abir", "update", 1, 3) {
			t.Fatalf("token %d refused", i)
		}
	}
	if h.Allow("d", "abir", "update", 1, 3) {
		t.Fatal("an empty bucket allowed")
	}
	if !h.Allow("d", "santosh", "update", 1, 3) {
		t.Fatal("one person's bucket spent another's")
	}
	now = now.Add(time.Second)
	if !h.Allow("d", "abir", "update", 1, 3) {
		t.Fatal("the bucket did not refill")
	}

	h.Remove("d")
	if e := <-feed; e.Kind != KindRemoved {
		t.Fatalf("remove = %+v", e)
	}
	if _, open := <-feed; open {
		t.Fatal("feed still open after remove")
	}
}
