package blobstore_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

func TestMemoryLogRecordsEveryCallInOrder(t *testing.T) {
	ctx := context.Background()
	m := blobstore.NewMemory()
	a, b := obj("a", "one"), obj("b", "two")
	frame, _ := blobstore.Encode(testKey, []blobstore.Object{a, b})

	id, err := m.PutFrame(ctx, frame)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(ctx, a.RID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Has(ctx, []string{b.RID, obj("c", "").RID}); err != nil {
		t.Fatal(err)
	}

	want := []blobstore.Op{
		{Kind: "put", Frame: id, RIDs: []string{a.RID, b.RID}, Bytes: int64(len(frame))},
		{Kind: "get", RIDs: []string{a.RID}, Bytes: int64(len(a.Bytes))},
		{Kind: "has", RIDs: []string{b.RID, obj("c", "").RID}},
	}
	if got := m.Log(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Log() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestMemoryLogIsACopy(t *testing.T) {
	m := blobstore.NewMemory()
	_, _ = m.Has(context.Background(), nil)
	m.Log()[0].Kind = "changed"
	if m.Log()[0].Kind != "has" {
		t.Fatal("changing the returned log changed the store's log")
	}
}

func TestMemoryStoresACopyOfTheFrame(t *testing.T) {
	ctx := context.Background()
	m := blobstore.NewMemory()
	a := obj("a", "one")
	frame, _ := blobstore.Encode(testKey, []blobstore.Object{a})
	if _, err := m.PutFrame(ctx, frame); err != nil {
		t.Fatal(err)
	}
	for i := range frame {
		frame[i] = 0
	}
	if got, err := m.Get(ctx, a.RID); err != nil || string(got) != string(a.Bytes) {
		t.Fatalf("Get after the caller reused its buffer = %q, %v", got, err)
	}
}

func TestMemoryFailAfterFailsOnceAtTheChosenCall(t *testing.T) {
	ctx := context.Background()
	m := blobstore.NewMemory()
	m.FailAfter(1, blobstore.ErrUnreachable)
	if _, err := m.Has(ctx, nil); err != nil {
		t.Fatalf("first call = %v, want success", err)
	}
	if _, err := m.Has(ctx, nil); !errors.Is(err, blobstore.ErrUnreachable) {
		t.Fatalf("second call = %v, want ErrUnreachable", err)
	}
	if _, err := m.Has(ctx, nil); err != nil {
		t.Fatalf("third call = %v, want success", err)
	}
}
