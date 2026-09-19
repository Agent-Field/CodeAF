package wsapi

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func actionEvidence(plan ActionPlan, action Action) []EvidenceRef {
	if len(action.Evidence) > 0 {
		return action.Evidence
	}
	return plan.Evidence
}

// evidenceHash is hex SHA-256 of the canonical, sorted passage hashes.
func evidenceHash(refs []EvidenceRef) string {
	hashes := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.PassageHash == "" {
			continue
		}
		hashes = append(hashes, ref.PassageHash)
	}
	sort.Strings(hashes)
	sum := sha256.Sum256([]byte(strings.Join(hashes, "\n")))
	return hex.EncodeToString(sum[:])
}

func hasPassageEvidence(refs []EvidenceRef) bool {
	for _, ref := range refs {
		if ref.PassageHash != "" {
			return true
		}
	}
	return false
}

func normalizeRef(ref workspace.Ref) workspace.Ref {
	if ref.Kind == "" {
		ref.Kind = workspace.ConversationKind
	}
	return ref
}
