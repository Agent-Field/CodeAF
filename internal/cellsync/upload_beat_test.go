package cellsync

import "testing"

// A beat is due again when the upload of pending turns has moved on, and it
// carries progress only while turns are pending.
func TestBatcherBeatsUploadProgress(t *testing.T) {
	b := &Batcher{}
	b.tell(1)
	if !b.toldPending(1) {
		t.Fatal("nothing changed, yet a beat is due")
	}
	b.Uploading(0, 1433)
	if b.toldPending(1) {
		t.Fatal("an upload started, yet no beat is due")
	}
	if got := b.beatUpload(1); got != (uploaded{0, 1433}) {
		t.Fatalf("beat carries %+v, want 0 of 1433", got)
	}
	if got := b.beatUpload(0); got != (uploaded{}) {
		t.Fatalf("with nothing pending the beat carries %+v", got)
	}
	b.tell(1)
	b.Uploading(412, 1433)
	if b.toldPending(1) {
		t.Fatal("the upload moved on, yet no beat is due")
	}
	b.Uploading(0, 0)
	b.tell(0)
	if !b.toldPending(0) {
		t.Fatal("after a publish nothing is due")
	}
}
