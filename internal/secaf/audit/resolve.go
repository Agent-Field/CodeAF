package audit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// remotePrefixes are the spellings _resolve_repo treated as something to clone
// (`repo_url.startswith(("https://", "http://", "git@"))`), plus any other
// scheme-qualified address, so a URL is named as a URL in the refusal rather
// than reported as a directory that does not exist.
var remotePrefixes = []string{"https://", "http://", "git@", "ssh://", "git://", "file://"}

// ErrRemoteRepository is the refusal for a repository given as a URL. Inside
// codeaf the audit reads a checkout that is already on disk; it never clones.
var ErrRemoteRepository = errors.New("the security audit reads a local directory and does not clone repositories")

// ResolveRepo turns the request's repo_url into the absolute, symlink-free
// directory the audit reads.
//
// It replaces `_resolve_repo(repo_url)` (src/sec_af/app.py:75), and is
// deliberately narrower. The node cloned a URL into a workspaces directory
// (SEC_AF_WORKSPACES_DIR, falling back to ~/.sec-af/workspaces), pulled an
// existing clone, and silently audited SEC_AF_REPO_PATH or its own working
// directory when repo_url was neither a URL nor a directory. Inside codeaf all
// three would be surprises — a network fetch nobody asked for, a directory
// written outside the project, or an audit of the wrong tree — so only an
// existing local directory is accepted, and everything else is refused with a
// sentence that says why.
func ResolveRepo(repoURL string) (string, error) {
	trimmed := strings.TrimSpace(repoURL)
	if trimmed == "" {
		return "", errors.New("no repository to audit: repo_url is empty")
	}
	for _, prefix := range remotePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return "", fmt.Errorf("%w: %q is a URL; clone it and audit the directory", ErrRemoteRepository, trimmed)
		}
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("repository %q: %w", trimmed, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("repository %q does not exist", trimmed)
		}
		return "", fmt.Errorf("repository %q: %w", trimmed, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("repository %q: %w", trimmed, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository %q is not a directory", trimmed)
	}
	return resolved, nil
}
