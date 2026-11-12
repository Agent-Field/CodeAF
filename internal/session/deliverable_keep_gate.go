package session

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// Keep admission bounds retained keys AND all holders/waiters. A canceled
// waiter gives back only its reservation; a holder owns its gate until the
// actual copy/record/rollback operation returns. No goroutine waits for callers.
const keepAdmissionMax = 64
const keepIdentityMax = 128

type keepKey struct{ index, session, id string }
type keepGate struct {
	key   keepKey
	token chan struct{}
	refs  int
}
type keepAdmission struct {
	mu    sync.Mutex
	gates map[keepKey]*keepGate
	refs  int
}

var deliverKeepAdmission keepAdmission

func (r *keepAdmission) reserve(key keepKey) (*keepGate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refs >= keepAdmissionMax {
		return nil, ErrMediaBusy
	}
	if r.gates == nil {
		r.gates = make(map[keepKey]*keepGate)
	}
	gate := r.gates[key]
	if gate == nil {
		// Both map and release closures use these detached bounded strings.
		owned := keepKey{strings.Clone(key.index), strings.Clone(key.session), strings.Clone(key.id)}
		gate = &keepGate{key: owned, token: make(chan struct{}, 1)}
		gate.token <- struct{}{}
		r.gates[owned] = gate
	}
	gate.refs++
	r.refs++
	return gate, nil
}

func (r *keepAdmission) unreserve(key keepKey, gate *keepGate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	gate.refs--
	r.refs--
	if gate.refs == 0 {
		delete(r.gates, key)
	}
}

func (r *keepAdmission) acquire(ctx context.Context, key keepKey) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrMediaBusy, err)
	}
	if len(key.index) > deliverIndexKeyMax {
		return nil, ErrDeliverIndexPath
	}
	if len(key.session) > keepIdentityMax || len(key.id) > keepIdentityMax {
		return nil, ErrKeepSource
	}
	gate, err := r.reserve(key)
	if err != nil {
		return nil, err
	}
	key = gate.key // waiters and releasers must not retain caller backing strings
	select {
	case <-ctx.Done():
		r.unreserve(key, gate)
		return nil, errors.Join(ErrMediaBusy, ctx.Err())
	case <-gate.token:
		// A simultaneous cancellation must not adopt a gate and start work.
		if err := ctx.Err(); err != nil {
			gate.token <- struct{}{}
			r.unreserve(key, gate)
			return nil, errors.Join(ErrMediaBusy, err)
		}
		var once sync.Once
		return func() {
			once.Do(func() {
				gate.token <- struct{}{}
				r.unreserve(key, gate)
			})
		}, nil
	}
}
