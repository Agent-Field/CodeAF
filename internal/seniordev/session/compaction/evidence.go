//go:build !windows

package compaction

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/seniordev/session/evidenceharvest"
)

// FallbackEvidenceSelector exposes the deterministic evidence harvest through
// the compaction service's evidence seam.
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
