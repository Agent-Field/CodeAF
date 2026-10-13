package lifecycle

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestWrapIsPassthrough(t *testing.T) {
	original := store.New(svc.Config{}, &svc.Services{})
	if Wrap(original) != original {
		t.Fatal("stub changed the store")
	}
	if Wrap(nil) != nil {
		t.Fatal("stub changed nil store")
	}
}
