package run

import (
	"context"
	"sync/atomic"
)

type storageProofKey struct{}

// storageProof collects facts only from workers after they have closed. The
// supervisor joins every worker before the engine reads this final proof.
type storageProof struct {
	observed atomic.Int64
	unsafe   atomic.Bool
}

func withStorageProof(ctx context.Context) (context.Context, *storageProof) {
	proof := &storageProof{}
	return context.WithValue(ctx, storageProofKey{}, proof), proof
}
func observeStorageProof(ctx context.Context, safe bool) {
	proof, _ := ctx.Value(storageProofKey{}).(*storageProof)
	if proof == nil {
		return
	}
	if !safe {
		proof.unsafe.Store(true)
	}
	proof.observed.Add(1)
}
func (p *storageProof) safe() bool { return p != nil && p.observed.Load() > 0 && !p.unsafe.Load() }
