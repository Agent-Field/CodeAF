package gitidentity

// A memory's project owner is keyed on WHOSE repository the workspace is, and
// only falls back to WHERE it is. The key must survive what a workspace
// survives — a folder move, a checkout on another machine — so it is hashed
// from the git remote URL when one exists, and from the canonical path only
// when there is no remote to name.
//
// THIS IS AN IDENTITY, NOT A PATH. `project:<key>` rows outlive renames,
// clones and moves; a path hash would have turned every `mv` of a repository
// into amnesia. What it does NOT survive yet: renaming a repository that has
// no remote, whose only identity is its path — a limitation, written into the
// architecture document as open decision 4, and the honest answer for a store
// whose rows have no alias table yet.

import (
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"path/filepath"
	"strings"
)

// ProjectKey answers the stable project key of a workspace: the normalized
// origin remote's hash when the workspace is a git repository with an origin,
// and the canonical path's hash otherwise. The second answer is also the
// error-free answer for a workspace that is not a repository at all.
//
// The key is opaque by design and the same length either way — 16 bytes of
// SHA-256 — so nothing that reads an owner string can tell a remote-keyed
// project from a path-keyed one and start treating them differently.
func ProjectKey(workspace string) (string, error) {
	root := strings.TrimSpace(workspace)
	if root == "" {
		return "", nil
	}
	if remote := originRemote(root); remote != "" {
		if normalized := NormalizeRemoteURL(remote); normalized != "" {
			return hashKey(normalized), nil
		}
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", nil
	}
	return hashKey(absolute), nil
}

// hashKey is the one way a project identity is minted.
func hashKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

// originRemote answers the workspace's origin URL, or "". Git's own answer is
// the authority; a failed read is "no remote", never a failure — the path
// fallback is the designed answer for a workspace without one.
func originRemote(root string) string {
	out, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// NormalizeRemoteURL folds the spellings of one repository down to one name.
// These are the same repository and must key the same owner:
//
//	git@github.com:owner/repo.git
//	ssh://git@github.com/owner/repo
//	https://github.com/owner/repo
//	http://github.com/owner/repo/
//	github.com/owner/repo
//
// The scheme and the credentials are dropped, a trailing .git is dropped, the
// host is lower-cased (GitHub's own rule, and the only host this
// normalization claims to speak for), and the result is scheme-less. Two
// checkouts — one cloned over SSH on one machine, one over HTTPS on another —
// are one project to the memory, which is the whole point.
func NormalizeRemoteURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	// scp-like spelling first, before anything eats the colon: git@host:owner/repo
	if at := strings.Index(value, "@"); at >= 0 && strings.Contains(value[at:], ":") && !strings.Contains(value[:at], "://") {
		host := value[at+1 : strings.Index(value[at:], ":")+at]
		value = host + "/" + value[strings.Index(value[at:], ":")+at+1:]
	}
	value = strings.TrimPrefix(value, "ssh://")
	value = strings.TrimPrefix(value, "git://")
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "git+ssh://")
	value = strings.TrimPrefix(value, "git+https://")
	// A port on a host spelling: github.com:22/owner/repo
	if colon := strings.Index(value, ":"); colon > 0 {
		port := value[colon+1:]
		digits := 0
		for digits < len(port) && port[digits] >= '0' && port[digits] <= '9' {
			digits++
		}
		if digits > 0 && digits < len(port) && port[digits] == '/' {
			value = value[:colon] + port[digits:]
		}
	}
	value = strings.TrimSuffix(value, ".git")
	// A userinfo on a URL spelling: https://git@github.com/owner/repo
	if at := strings.Index(value, "@"); at >= 0 && !strings.Contains(value[:at], "/") {
		value = value[at+1:]
	}
	value = strings.TrimSuffix(value, ".git")
	value = strings.TrimSuffix(value, "/")
	// The host is lower-cased; the path is not — case-sensitive hosts exist,
	// and a normalization that folds their case would collide two projects.
	if slash := strings.Index(value, "/"); slash > 0 {
		value = strings.ToLower(value[:slash]) + value[slash:]
	} else {
		value = strings.ToLower(value)
	}
	return value
}
