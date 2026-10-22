package main

import (
	"log"
	"strings"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// v3MediaModel retains the ordinary media ladder. During --one-model an
// explicit media choice is also a boundary: an incapable pinned model makes
// that operation unavailable instead of silently spending on a replacement.
// Unpinned media still follows the existing capability-qualified defaults.
func v3MediaModel(models *catalog.Catalog, profileDir string, source roles.Source, oneModel ...bool) func(string) string {
	resolve := v3AutomaticMediaModel(models, profileDir, source)
	if len(oneModel) == 0 || !oneModel[0] {
		return resolve
	}
	return func(modality string) string {
		modality = strings.ToLower(strings.TrimSpace(modality))
		if pinned := v3ExplicitMediaPin(profileDir, source, modality); pinned != "" {
			if models == nil || !v3MediaCapable(models.SnapshotNow(), modality, pinned) {
				log.Printf("media: %s is unavailable: the explicit %s pin cannot %s", modality, pinned, v3MediaVerb[modality])
				return ""
			}
			return pinned
		}
		return resolve(modality)
	}
}
func v3ExplicitMediaPin(profileDir string, source roles.Source, modality string) string {
	if slot, ok := v3MediaSlot[modality]; ok {
		if pinned := config.MediaSlotModelAt(profileDir, slot); pinned != "" {
			return pinned
		}
	}
	if modality == "vision" {
		if pinned := config.VisionModelAt(profileDir); pinned != "" {
			return pinned
		}
	}
	if role, ok := v3MediaPin[modality]; ok {
		if pinned, set := roles.Pinned(source, role); set {
			return pinned
		}
	}
	return ""
}
