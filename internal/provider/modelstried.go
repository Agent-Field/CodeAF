package provider

import (
	"context"
	"strings"
	"sync"
)

// ── WHICH MODELS THIS CALL HAS ACTUALLY BEEN ON ─────────────────────────────
//
// There used to be two mechanisms that changed a request's model, drawing from
// one list and neither aware of the other: the adapter's own walk at the foot of
// the endpoint ladder (endpoints.go, deleted 2026-09-10) and internal/session's
// turn loop. The session's is the one that survives — it owns the turn and it is
// the only layer that knows what the turn has spent — and it used to pick the
// next model by COUNTING: `options[len(hopped)]`, where `hopped` was a list it
// kept itself. A chain the adapter had already walked was walked again from the
// top, and the person paid twice for the same refusal.
//
// So the fact is recorded where it happens. Every model this adapter puts on the
// wire is written to a small record on the call's own context, and the session
// reads it: the next model is the first one on the chain that is NOT on this
// list, which is a fact rather than an index.
//
// IT IS PER CALL AND NOT PER PROCESS. A conversation asks the same model of the
// same chain over and over, and a record that outlived one question would make
// the second question believe its own model had already failed. The session opens
// one per turn ([WithModelsTried]); a context without one is the legal empty
// state — every write is dropped and [ModelsTried] answers nothing, which is
// exactly what a caller with no chain needs.

type modelsTriedKey struct{}

// modelsTried is the record itself. It is a pointer on the context rather than a
// value, because the writes happen several frames below whoever opened it.
type modelsTried struct {
	mu     sync.Mutex
	models []string
}

// WithModelsTried opens the record for one question. Opening it twice on one
// chain of contexts keeps the OUTER one, so a turn's record survives every child
// context its calls and errands derive.
func WithModelsTried(ctx context.Context) context.Context {
	if modelsTriedFrom(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, modelsTriedKey{}, &modelsTried{})
}

func modelsTriedFrom(ctx context.Context) *modelsTried {
	if ctx == nil {
		return nil
	}
	record, _ := ctx.Value(modelsTriedKey{}).(*modelsTried)
	return record
}

// noteModelTried records that a request naming this model left for the wire. It
// is called from the one door every send passes through ([Client.sendShaped]),
// so a model reaches this list exactly when it has actually been asked — never
// when it was merely considered.
func noteModelTried(ctx context.Context, model string) {
	record := modelsTriedFrom(ctx)
	model = strings.TrimSpace(model)
	if record == nil || model == "" {
		return
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	for _, seen := range record.models {
		if strings.EqualFold(seen, model) {
			return
		}
	}
	record.models = append(record.models, model)
}

// ModelsTried names every model this question has been put to, in the order it
// was put to them. It is the FACT a caller choosing the next model reads instead
// of counting its own hops.
//
// A context with no record answers nothing, which a caller must read as "no
// model has been ruled out" rather than as an error: a build with no chain, a
// call made outside a turn, and `--one-model` all arrive here, and each of them
// wants the move to be ABSENT rather than broken.
func ModelsTried(ctx context.Context) []string {
	record := modelsTriedFrom(ctx)
	if record == nil {
		return nil
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	return append([]string(nil), record.models...)
}
