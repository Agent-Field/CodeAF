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
// It also remembers which models a turn's end should ask the app to list again
// ([appBackend.modelLoadedBy]): one the server said it was not running, which
// the next turn on it loads, so the server can then say the window it gave;
// and one whose row says its window is small, which the next turn on it may
// reload with a larger one after the person raised Ollama's setting and
// restarted it. A model this list ran out of time to ask about keeps what the
// last list remembered of it, so a slow answer neither hides a warning for one
// list nor loses that request.
//
// A SMALL WINDOW THAT STAYS THE SAME IS ASKED ABOUT ONCE. The first list after
// a turn's notice has read the window that turn left the model with, and each
// list costs a question per model to the server and a redraw in the app: so a
// small window is marked again only when it differs from the one that list
// read. A person who raised the setting and restarted Ollama finds the model
// unloaded, or loaded with another window, and either is marked again.
//
// A MODEL THE SERVER NEVER KEEPS LOADED IS ASKED ABOUT ONCE. When the list a
// turn asked for still finds the model unloaded (a server that unloads it at
// once, keep_alive 0), another turn would find the same, and every list costs
// questions to the server: so no turn asks again for it while it stays
// unloaded, until a list sees it running.
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
	readings := appOllamaLimits(ctx, models, sources, nil, seen)
	b.locked(func() {
		relist := map[string]bool{}
		for name, reading := range readings {
			if reading.Stale {
				if b.ollamaRelist[name] {
					relist[name] = true
				}
				continue
			}
			small := appSmallWindow(reading.Limits)
			switch {
			case small == 0:
				delete(b.ollamaToldWindow, name)
			case b.ollamaRelisted[name]:
				if b.ollamaToldWindow == nil {
					b.ollamaToldWindow = map[string]int{}
				}
				b.ollamaToldWindow[name] = small
			}
			switch {
			case reading.Loaded:
				delete(b.ollamaRelisted, name)
				delete(b.ollamaQuiet, name)
			case reading.Unloaded && b.ollamaRelisted[name]:
				delete(b.ollamaRelisted, name)
				if b.ollamaQuiet == nil {
					b.ollamaQuiet = map[string]bool{}
				}
				b.ollamaQuiet[name] = true
			}
			if (reading.Unloaded && !b.ollamaQuiet[name]) || (small > 0 && small != b.ollamaToldWindow[name]) {
				relist[name] = true
			}
		}
		b.ollamaRelist = relist
	})
}

// appSmallWindow is the small window limits carry, or 0 when they carry none.
func appSmallWindow(limits []appwire.ModelLimit) int {
	for _, limit := range limits {
		if limit.Kind == appwire.LimitSmallWindow {
			return limit.Window
		}
	}
	return 0
}

// appOllamaLimits fills the limits of the rows an Ollama service serves, and
// answers what the server said of each of its models, by the name the server
// knows them by: a model it ran out of time to ask about has the reading seen
// kept from the last list that could, marked stale. client is the test's door;
// nil is the probe's own.
//
// ONLY A CONNECTED OLLAMA IS ASKED, AND ONLY ABOUT ITS OWN MODELS. The
// questions are that server's own API: another provider's address would answer
// them with an error at best, and a model another service serves is not this
// server's to describe even when both happen to list the same name.
func appOllamaLimits(ctx context.Context, models []appwire.Model, sources modelsource.Set, client *http.Client, seen *ollamalimits.Memory) map[string]ollamalimits.Reading {
	service, connected := sources.ByID(ollamalimits.SourceID)
	if !connected || !service.HasCredentials() {
		return nil
	}
	probe, ok := ollamalimits.At(service.Address, service.Key)
	if !ok {
		return nil
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
	readings := probe.Read(ctx, names, session.LeanWindow())
	for name, reading := range readings {
		for _, i := range rows[name] {
			models[i].Limits = reading.Limits
		}
	}
	return readings
}

// modelLoadedBy is called when a turn on model has ended. A turn on an Ollama
// model the server was not running at the last list has loaded it, and one on a
// model listed with a small window it was not known to have after an earlier
// notice may have reloaded it with a larger one, so the server can now say what
// it gave the model: the app is told the models changed, once, and its next list
// reads the window the model really has.
func (b *appBackend) modelLoadedBy(model string) {
	if proc := openedEngineProcess(); proc != nil {
		_, sources := proc.currentAccount()
		b.ollamaLoaded(sources, model)
	}
}

// ollamaLoaded is [appBackend.modelLoadedBy] over the services given. A turn
// says it at most once, and only for a model the last list marked: the list
// the notice asks for marks the model again only if it is still worth asking
// about ([appBackend.addModelLimits]).
func (b *appBackend) ollamaLoaded(sources modelsource.Set, model string) {
	model, _ = roles.SplitEffort(model)
	owner, bare := sources.For(model)
	if !strings.EqualFold(owner.Source.ID, ollamalimits.SourceID) {
		return
	}
	var tell func()
	b.locked(func() {
		if b.ollamaRelist[bare] {
			delete(b.ollamaRelist, bare)
			if b.ollamaRelisted == nil {
				b.ollamaRelisted = map[string]bool{}
			}
			b.ollamaRelisted[bare] = true
			tell = b.tellModels
		}
	})
	if tell != nil {
		tell()
	}
}
