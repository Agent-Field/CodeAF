package compaction

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/evidenceharvest"
)

// FallbackEvidenceSelector exposes evidence-harvest.ts's deterministic
// fallback through the compaction service's evidence seam.
type FallbackEvidenceSelector struct {
	MaxChars float64
}

func (selector FallbackEvidenceSelector) SelectEvidence(
	_ context.Context, blocks []string,
) (*string, error) {
	if selector.MaxChars > 0 {
		return evidenceharvest.HarvestEvidence(blocks, selector.MaxChars), nil
	}
	return evidenceharvest.HarvestEvidence(blocks), nil
}
