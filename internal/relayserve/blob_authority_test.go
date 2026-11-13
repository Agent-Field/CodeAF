package relayserve_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// R16/R21 require a delayed blob completion to recheck committed authority.
func TestR16R21RevocationOrFreezeDuringBlobIOCannotReturnUploadSuccess(t *testing.T) {
	for _, refusal := range []error{wireauth.ErrRevoked, wireauth.ErrRotated} {
		t.Run(refusal.Error(), func(t *testing.T) {
			store := blobstore.NewMemory()
			checks := 0
			guarded := relayserve.CommittedBlobStore{Store: store, Authorize: func(context.Context) error {
				checks++
				if checks == 2 {
					return refusal
				}
				return nil
			}}
			frame, e := blobstore.Encode(strings.Repeat("0", 32), []blobstore.Object{{RID: strings.Repeat("1", 64), Bytes: []byte("AGEO\x01sealed")}})
			if e != nil {
				t.Fatal(e)
			}
			if id, e := guarded.PutFrame(context.Background(), frame); id != "" || !errors.Is(e, refusal) {
				t.Fatal(id, e)
			}
			if _, e := store.GetFrame(context.Background(), blobstore.IDOf(frame)); e != nil {
				t.Fatal("uploaded frame was deleted", e)
			}
		})
	}
}
