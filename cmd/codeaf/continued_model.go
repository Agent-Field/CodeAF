package main

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
)

// continuedModel checks a copy saved by another computer before seating it can
// seal new local bookkeeping. Ordinary local launches retain their model rules.
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
	if config.ModelUsable(sources, model, providerID) {
		return "", "", nil
	}
	settings, err := config.LoadKeyless()
	if err != nil {
		return "", "", err
	}
	fallback := v3TalkModel("", settings)
	if !config.ModelUsable(sources, fallback, "") {
		return "", "", fmt.Errorf("This chat used %s. It isn't set up here, and this computer has no usable default model. Set up a model in Settings.", model)
	}
	where := ""
	if from != "" {
		where = " on " + from
	}
	return fallback, fmt.Sprintf("This chat used %s%s. It isn't set up here, so it continues with %s.", model, where, fallback), nil
}
