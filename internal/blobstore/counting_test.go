package blobstore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/blobstore/blobstoretest"
)

func TestCountingConforms(t *testing.T) {
	blobstoretest.Run(t, func(*testing.T) blobstore.Store {
		return blobstore.Counting{Inner: blobstore.NewMemory(), C: &blobstore.Counters{}}
	})
}

func TestCountingCountsEveryRequest(t *testing.T) {
	ctx := context.Background()
	c := blobstore.Counting{Inner: blobstore.NewMemory(), C: &blobstore.Counters{}}
	obj := blobstore.Object{RID: blobstoreRID('a'), Bytes: []byte("AGEO\x01sealed")}
	frame, err := blobstore.Encode("00000000000000000000000000000000", []blobstore.Object{obj, {RID: blobstoreRID('b'), Bytes: []byte("AGEO\x01more")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutFrame(ctx, frame); err != nil {
		t.Fatal(err)
	}
	got, err := c.Get(ctx, obj.RID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, blobstoreRID('c')); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("missing get = %v", err)
	}
	if _, err := c.PutFrame(ctx, []byte("not a frame")); err == nil {
		t.Fatal("a bad frame was stored")
	}
	if _, err := c.Has(ctx, []string{obj.RID, blobstoreRID('c')}); err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"puts": 2, "gets": 2, "has": 1, "up": int64(len(frame)) + 11, "down": int64(len(got)), "objects": 2}
	have := map[string]int64{"puts": c.C.Puts.Load(), "gets": c.C.Gets.Load(), "has": c.C.Has.Load(),
		"up": c.C.BytesUp.Load(), "down": c.C.BytesDown.Load(), "objects": c.C.ObjectsUp.Load()}
	for k, w := range want {
		if have[k] != w {
			t.Errorf("%s = %d, want %d", k, have[k], w)
		}
	}
}

func blobstoreRID(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
