package cellstore

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/guard"
)

func TestConcurrentFirstCaptureOrdersStoreHandles(t *testing.T) {
	for round := 0; round < 8; round++ {
		e := realEngine(t)
		// Both cold and warmed stores must order standalone readers with seals.
		d := e.Transport.(Spawn)
		reader := Spawn{Binary: e.Binary, Socket: d.Socket}
		c := newCell(t)
		if err := os.WriteFile(filepath.Join(c.Root, "note"), []byte("hello"), 0600); err != nil {
			t.Fatal(err)
		}
		target := e.target(c)
		if round%2 == 0 {
			if _, err := d.Do(t.Context(), target, capturePlanOp{}); err != nil {
				t.Fatal(err)
			}
		}
		start := make(chan struct{})
		errs := make(chan error, 4)
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			guard.Go("concurrent first capture", func() {
				defer wg.Done()
				<-start
				_, err := reader.Do(t.Context(), target, capturePlanOp{})
				errs <- err
			})
		}
		close(start)
		_, err := d.Do(t.Context(), target, sealOp{Turn: fmt.Sprint(round)})
		if err != nil {
			t.Errorf("round %d seal: %v", round, err)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Errorf("round %d plan: %v", round, err)
			}
		}
		if t.Failed() {
			return
		}
	}
}
