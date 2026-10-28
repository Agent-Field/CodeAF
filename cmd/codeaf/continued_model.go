package main

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// continuedModel identifies a copy before seating can seal local bookkeeping.
// Ordinary local launches retain their model rules.
func continuedModel(cfg session.Config) (string, string, error) {
	dev, err := identity.LoadDevice(home.Dir())
	if err != nil {
		return "", "", nil
	}
	c, err := cell.OpenAt(cfg.Place.Dir, cfg.Place.ID())
	if err != nil {
		return "", "", nil
	}
	head, err := cellstore.Head(c)
	from, adopted := cellstore.Adopted(c)
	if err != nil || head == nil || !adopted && (head.Turn.Device == "" || head.Turn.Device == dev.Cert.Device) {
		return "", "", nil
	}
	model, providerID := cfg.Model, ""
	if meta, err := session.LoadMeta(c.Root); err == nil {
		if state := meta.ModelIdentity; state != nil {
			if state.Models != nil {
				model = state.Models.Main
			}
			if state.Requested == model {
				providerID = state.Provider
			}
		}
		if meta.Model == model && meta.ModelProvider != "" {
			providerID = meta.ModelProvider
		}
	}
	sources := cfg.Sources.OrDefault(cfg.APIKey, cfg.BaseURL)
	return continuedModelChoice(sources, model, providerID, from)
}

// continuedModelUnavailable carries the original provider through local saves
// while the person sets up a usable default.
type continuedModelUnavailable struct{ model, provider, from string }

func (e *continuedModelUnavailable) Error() string {
	return fmt.Sprintf("This chat used %s, which isn't set up on this computer. Set up a model in Settings, then send again.", continuedModelLabel(e.model))
}

func (e *continuedModelUnavailable) prepare(model string, sources modelsource.Set) (string, string, error) {
	provider := e.provider
	if model != e.model {
		provider = ""
	}
	return continuedModelChoice(sources, model, provider, e.from)
}

func continuedModelChoice(sources modelsource.Set, model, providerID, from string) (string, string, error) {
	if config.ModelUsable(sources, model, providerID) {
		return "", "", nil
	}
	settings, err := config.LoadKeyless()
	if err != nil {
		return "", "", err
	}
	fallback := v3TalkModel("", settings)
	if !config.ModelUsable(sources, fallback, "") {
		return "", "", &continuedModelUnavailable{model: model, provider: providerID, from: from}
	}
	where := ""
	if from != "" {
		where = " on " + from
	}
	return fallback, fmt.Sprintf("This chat used %s%s. It isn't set up here, so it continues with %s.", model, where, fallback), nil
}

func continuedModelLabel(model string) string {
	for _, source := range modelsource.Vendored() {
		if bare, ok := strings.CutPrefix(model, source.Written+"/"); ok {
			return strings.TrimSuffix(bare, ":latest") + " (" + source.Name + ")"
		}
	}
	return model
}
