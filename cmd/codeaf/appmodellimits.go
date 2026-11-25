package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/ollamalimits"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// addModelLimits writes onto each row what its own server says it falls short
// in ([appwire.ModelLimit]), asking no later than until.
//
// It also remembers which models the server said it was not running. Such a
// model is loaded by the next turn on it, and the server can then say the
// window it gave, so the turn's end asks the app to list again
// ([appBackend.modelLoadedBy]). A model this list ran out of time to ask about
// keeps what the last list remembered of it, so a slow answer neither hides a
// warning for one list nor loses that request.
func (b *appBackend) addModelLimits(until time.Time, models []appwire.Model, sources modelsource.Set) {
	ctx, cancel := context.WithDeadline(context.Background(), until)
	defer cancel()
	var seen *ollamalimits.Memory
	b.locked(func() {
		if b.ollamaSeen == nil {
			b.ollamaSeen = &ollamalimits.Memory{}
		}
		seen = b.ollamaSeen
	})
	unloaded, unasked := appOllamaLimits(ctx, models, sources, nil, seen)
	b.locked(func() {
		for _, name := range unasked {
			if b.ollamaUnloaded[name] {
				if unloaded == nil {
					unloaded = map[string]bool{}
				}
				unloaded[name] = true
			}
		}
		b.ollamaUnloaded = unloaded
	})
}

// appOllamaLimits fills the limits of the rows an Ollama service serves. It
// answers the models, by the name the server knows them by, that the server
// said it is not running, and the models it ran out of time to ask about,
// whose rows carry what seen kept from the last list that could. client is
// the test's door; nil is the probe's own.
//
// ONLY A CONNECTED OLLAMA IS ASKED, AND ONLY ABOUT ITS OWN MODELS. The
// questions are that server's own API: another provider's address would answer
// them with an error at best, and a model another service serves is not this
// server's to describe even when both happen to list the same name.
func appOllamaLimits(ctx context.Context, models []appwire.Model, sources modelsource.Set, client *http.Client, seen *ollamalimits.Memory) (unloaded map[string]bool, unasked []string) {
	service, connected := sources.ByID(ollamalimits.SourceID)
	if !connected || !service.HasCredentials() {
		return nil, nil
	}
	probe, ok := ollamalimits.At(service.Address, service.Key)
	if !ok {
		return nil, nil
	}
	probe.Client, probe.Memory = client, seen
	rows := map[string][]int{}
	var names []string
	for i, model := range models {
		owner, bare := sources.For(model.ID)
		if !strings.EqualFold(owner.Source.ID, ollamalimits.SourceID) || bare == "" {
			continue
		}
		if rows[bare] == nil {
			names = append(names, bare)
		}
		rows[bare] = append(rows[bare], i)
	}
	for name, reading := range probe.Read(ctx, names, session.LeanWindow()) {
		for _, i := range rows[name] {
			models[i].Limits = reading.Limits
		}
		switch {
		case reading.Stale:
			unasked = append(unasked, name)
		case reading.Unloaded:
			if unloaded == nil {
				unloaded = map[string]bool{}
			}
			unloaded[name] = true
		}
	}
	return unloaded, unasked
}

// modelLoadedBy is called when a turn on model has ended. A turn on an Ollama
// model the server was not running at the last list has loaded it, so the
// server can now say what it gave the model: the app is told the models
// changed, once, and its next list reads the window the model really has.
func (b *appBackend) modelLoadedBy(model string) {
	if proc := openedEngineProcess(); proc != nil {
		_, sources := proc.currentAccount()
		b.ollamaLoaded(sources, model)
	}
}

// ollamaLoaded is [appBackend.modelLoadedBy] over the services given. It is
// said once for a model and not at every turn's end: the list the notice asks
// for forgets the model unless the server is still not running it.
func (b *appBackend) ollamaLoaded(sources modelsource.Set, model string) {
	model, _ = roles.SplitEffort(model)
	owner, bare := sources.For(model)
	if !strings.EqualFold(owner.Source.ID, ollamalimits.SourceID) {
		return
	}
	var tell func()
	b.locked(func() {
		if b.ollamaUnloaded[bare] {
			delete(b.ollamaUnloaded, bare)
			tell = b.tellModels
		}
	})
	if tell != nil {
		tell()
	}
}
