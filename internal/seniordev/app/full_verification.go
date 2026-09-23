//go:build !windows

package app

import (
	"context"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/session/fullverification"
)

const fullVerificationTimeoutMS = 600_000

// The registry may be configured with another shell. POSIX-like shells with
// pipefail honor this strict mode; shells without it reject the preamble and
// therefore fail verification closed instead of trusting a masked pipeline.
const strictVerificationPreamble = "set -euo pipefail\n"

type projectVerificationResult struct {
	Commands []any
	Prompt   string
	Failed   *fullverification.Entrypoint
	Failure  string
	// TimedOut is set when at least one entrypoint was killed at the
	// fullVerificationTimeoutMS ceiling without ever producing an exit status.
	// A hung suite is an INCOMPLETE observation, not a red one.
	TimedOut bool
	// NewFailures counts failures not excused as pre-existing (missing
	// entrypoints included).
	NewFailures int
}

// timedOutEntrypoint records an entrypoint that exhausted the verification ceiling,
// together with the worktree fingerprint it hung against.
type timedOutEntrypoint struct {
	Tail        string
	Fingerprint string
	HaveFinger  bool
}

func verificationMemoKey(entrypoint fullverification.Entrypoint) string {
	return entrypoint.Workdir + "\x00" + entrypoint.Command
}

// runProjectVerification executes the discovered project-wide entrypoints
// through the live Bash registry. It deliberately disables the test memo
// while retaining the registry's process-derived exitCode metadata.
func (runner *pipeline) runProjectVerification(
	ctx context.Context,
) projectVerificationResult {
	return newProjectVerificationRun(runner, ctx).run()
}

func planHasKind(plan fullverification.Plan, kind fullverification.EntrypointKind) bool {
	for _, entrypoint := range plan.Entrypoints {
		if entrypoint.Kind == kind {
			return true
		}
	}
	return false
}

func verificationTailSuffix(tail string) string {
	if tail == "" {
		return ""
	}
	return " — " + strings.ReplaceAll(tail, "\n", " ")
}

func verificationOutputTail(output string, limit int) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return suffixUTF16(output, limit)
}
