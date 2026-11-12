package main

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The richer route is opt-in for connected generation. It preserves explicit
// choices and turns an unsafe absent default into an actual typed refusal.
func v3MediaAdmission(models *catalog.Catalog, profileDir string, source roles.Source) func(string, string) session.MediaAdmission {
	return func(modality, word string) session.MediaAdmission {
		if models == nil {
			return session.MediaAdmission{Reason: "unconfirmed", Message: "The media catalog is not confirmed. Choose a compatible model before generating."}
		}
		available := models.SnapshotNow()
		if strings.TrimSpace(word) != "" {
			model, err := config.ResolveMediaModel(available, modality, word)
			if err != nil {
				return session.MediaAdmission{Reason: "model_invalid", Message: err.Error()}
			}
			if model != "" {
				return session.MediaAdmission{Model: model}
			}
		}
		configured := ""
		if slot, ok := v3MediaSlot[modality]; ok {
			configured = strings.TrimSpace(config.MediaSlotModelAt(profileDir, slot))
		}
		if configured == "" {
			if role, ok := v3MediaPin[modality]; ok {
				if pin, set := roles.Pinned(source, role); set {
					configured = strings.TrimSpace(pin)
				}
			}
		}
		if configured != "" {
			// A known incompatible choice is an actionable refusal. A user explicitly
			// chosen route not in a listing remains theirs: the provider decides it.
			if _, known := available.Model(configured); known && !v3MediaCapable(available, modality, configured) {
				return session.MediaAdmission{Reason: "configured_model_invalid", Message: "The configured model " + configured + " does not advertise " + modality + " output. Choose a compatible model; no other model was substituted."}
			}
			return session.MediaAdmission{Model: configured}
		}
		var route config.MediaRoute
		switch modality {
		case "image":
			route = (config.Config{}).ResolveImageRoute(available)
		case "video":
			route = (config.Config{}).ResolveVideoRoute(available)
		default:
			return session.MediaAdmission{Reason: "model_required", Message: "Choose a compatible " + modality + " model before generating."}
		}
		if route.Model != "" {
			return session.MediaAdmission{Model: route.Model}
		}
		return session.MediaAdmission{Reason: string(route.Reason), Message: route.Err().Error(), Options: append([]string(nil), route.Options...)}
	}
}
