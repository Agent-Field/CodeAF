package main

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// FactoryForemanOpen is factory.foreman_open: the foreman conversation of the
// factory this engine serves, made the first time ([foremanMaker]) and the same
// one every time after, held open so the app draws it at once. Its floor is
// this host's own store, the factory home it belongs to and no other; a factory
// this engine does not serve is refused, as its job chats are.
func (b *appBackend) FactoryForemanOpen(ctx context.Context, p appwire.FactoryRef) (appwire.FactoryForeman, error) {
	host, err := b.localHostOf(p.Factory)
	if err != nil {
		return appwire.FactoryForeman{}, err
	}
	load := host.seam.Load
	if load == nil {
		load = factory.LocalSeam(host.floor, time.Now(), factoryPipelineOption(host.floor)).Load
	}
	transcript, err := foremanMaker(host.floor, host.workspace, host.profile, load)(ctx)
	if err != nil {
		return appwire.FactoryForeman{}, appwire.Errorf(appwire.CodeConflict, "the factory's manager could not be opened: %s", err)
	}
	id := appAgentID(transcript)
	if _, err := b.holdJobChat(id); err != nil {
		return appwire.FactoryForeman{}, err
	}
	return appwire.FactoryForeman{Factory: p.Factory, Agent: id}, nil
}
