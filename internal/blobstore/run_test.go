package blobstore_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/blobstore/blobstoretest"
)

func TestMemoryConforms(t *testing.T) {
	blobstoretest.Run(t, func(*testing.T) blobstore.Store { return blobstore.NewMemory() })
}

func TestDiskConforms(t *testing.T) {
	blobstoretest.Run(t, func(t *testing.T) blobstore.Store {
		d, err := blobstore.NewDisk(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return d
	})
}
