// Diff fingerprint — port of src/util/diff-fingerprint.ts:1-84
// (swe-pro 3b25a1a).
package util

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type FingerprintResult struct {
	SHA256     string `json:"sha256"`
	DiffLines  int    `json:"diffLines"`
	HasChanges bool   `json:"hasChanges"`
}

const diffFingerprintCommandTimeout = 5 * time.Minute

func FingerprintDiff(ctx context.Context, workspace string, baseRef string, headRef ...string) FingerprintResult {
	if baseRef == "" {
		baseRef = "HEAD"
	}
	commandCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), diffFingerprintCommandTimeout)
	defer cancel()
	_, _ = RunProcess(commandCtx, []string{"git", "-C", workspace, "add", "-A"}, RunOptions{NoThrow: true})
	args := []string{"git", "-C", workspace, "diff", "--no-color"}
	if len(headRef) > 0 {
		args = append(args, baseRef, headRef[0])
	} else {
		args = append(args, "--cached", baseRef)
	}
	result, err := RunProcess(commandCtx, args, RunOptions{NoThrow: true})
	if err != nil || result.Code != 0 {
		return FingerprintResult{SHA256: hashText(""), DiffLines: 0, HasChanges: false}
	}
	lines := []string{}
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		if strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "Date: ") {
			continue
		}
		lines = append(lines, line)
	}
	normalized := jscompat.Trim(strings.Join(lines, "\n"))
	count := 0
	if normalized != "" {
		count = len(strings.Split(normalized, "\n"))
	}
	return FingerprintResult{
		SHA256: hashText(normalized), DiffLines: count, HasChanges: normalized != "",
	}
}

func IsSameFingerprint(a, b FingerprintResult) bool { return a.SHA256 == b.SHA256 }

func hashText(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
