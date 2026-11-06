package main

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

var _ appwire.FactoryIntaker = (*appBackend)(nil)

// FactoryIntakeAdd files outside work on the named factory through its
// POST /api/intake, the one door every sender uses; this layer only carries
// the entry there and the factory's answer back.
func (b *appBackend) FactoryIntakeAdd(ctx context.Context, p appwire.FactoryIntakeParams) (inbox.Result, error) {
	data, err := b.factoryHub().post(ctx, p.Factory, serve.IntakePath, p.Entry)
	if err != nil {
		return inbox.Result{}, err
	}
	var out inbox.Result
	if json.Unmarshal(data, &out) != nil || out.ID < 1 {
		// The factory said yes and named no job: one may exist that this
		// layer cannot name, so it is uncertain rather than a success.
		return inbox.Result{}, appwire.Errorf(appwire.CodeUncertain, "the factory filed the work but did not say which job; read the queue before trying again")
	}
	return out, nil
}
