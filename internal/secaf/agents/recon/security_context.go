package recon

// Ports src/sec_af/agents/recon/security_context.py.

import (
	"context"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/secaf/appx"
	"github.com/Agent-Field/codeaf/internal/secaf/harnessx"
	"github.com/Agent-Field/codeaf/internal/secaf/prompts"
	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

const (
	securityContextPromptPath  = "recon/security_context.txt"
	securityContextAgentName   = "recon-security-context"
	securityContextExtractName = "Security context profiler"
)

// securityContextPrompt builds the exact prompt run_security_context_profiler
// sends. Same two-step shape as the data-flow prompt: placeholder substitution
// first, exploration CONTEXT suffix second.
func securityContextPrompt(repoPath string, architecture schemas.ArchitectureMap) string {
	template := prompts.MustLoad(securityContextPromptPath)
	return strings.ReplaceAll(template, architectureMapPlaceholder, ArchitectureContextBlock(architecture)) +
		explorationContextSuffix(repoPath)
}

// RunSecurityContextProfiler ports security_context.py
// run_security_context_profiler: substitute the architecture map, harness for a
// SecurityContextRaw, then parse_security_context_raw (which is where the flat
// `security_signals` list gets bucketed into headers / deployment / framework).
func RunSecurityContextProfiler(ctx context.Context, app appx.Harnesser, repoPath string, architecture schemas.ArchitectureMap) (schemas.SecurityContext, error) {
	prompt := securityContextPrompt(repoPath, architecture)

	harnessCwd, err := os.MkdirTemp("", "secaf-"+securityContextAgentName+"-")
	if err != nil {
		return schemas.SecurityContext{}, err
	}
	defer os.RemoveAll(harnessCwd)

	raw, err := harnessx.RunExtract[schemas.SecurityContextRaw](
		ctx, app, prompt,
		appx.HarnessOptions{Cwd: harnessCwd, ProjectDir: repoPath},
		securityContextExtractName,
	)
	if err != nil {
		return schemas.SecurityContext{}, err
	}
	return ParseSecurityContextRaw(raw), nil
}
