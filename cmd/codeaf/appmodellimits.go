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
// It also remembers which models were reported on the server's default window,
// the one answer the server could not put a figure to. Such a model's real
// window is known once a turn has loaded it, so the turn's end asks the app to
// list again ([appBackend.modelLoadedBy]).
func (b *appBackend) addModelLimits(until time.Time, models []appwire.Model, sources modelsource.Set) {
	ctx, cancel := context.WithDeadline(context.Background(), until)
	defer cancel()
	unloaded := appOllamaLimits(ctx, models, sources, nil)
	b.locked(func() { b.ollamaUnloaded = unloaded })
}

// appOllamaLimits fills the limits of the rows an Ollama service serves and
// answers the models, by the name the server knows them by, that it reported
// on the server's default window. client is the test's door; nil is the
// probe's own.
//
// ONLY A CONNECTED OLLAMA IS ASKED, AND ONLY ABOUT ITS OWN MODELS. The
// questions are that server's own API: another provider's address would answer
// them with an error at best, and a model another service serves is not this
// server's to describe even when both happen to list the same name.
func appOllamaLimits(ctx context.Context, models []appwire.Model, sources modelsource.Set, client *http.Client) map[string]bool {
	service, connected := sources.ByID(ollamalimits.SourceID)
	if !connected || !service.HasCredentials() {
		return nil
	}
	probe, ok := ollamalimits.At(service.Address, service.Key)
	if !ok {
		return nil
	}
	probe.Client = client
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
	var unloaded map[string]bool
	for name, limits := range probe.Limits(ctx, names, session.LeanWindow()) {
		for _, i := range rows[name] {
			models[i].Limits = limits
		}
		if limits[0].Kind == appwire.LimitSmallWindow && limits[0].Source == appwire.WindowDefault {
			if unloaded == nil {
				unloaded = map[string]bool{}
			}
			unloaded[name] = true
		}
	}
	return unloaded
}

// modelLoadedBy is called when a turn on model has ended. A turn on an Ollama
// model the last list could put no window to has loaded it, so the server can
// now say what it gave the model: the app is told the models changed, once,
// and its next list reads the real figure.
func (b *appBackend) modelLoadedBy(model string) {
	if proc := openedEngineProcess(); proc != nil {
		_, sources := proc.currentAccount()
		b.ollamaLoaded(sources, model)
	}
}

// ollamaLoaded is [appBackend.modelLoadedBy] over the services given. It is
// said once for a model and not at every turn's end: the list the notice asks
// for forgets the model unless the server still has no figure for it.
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
