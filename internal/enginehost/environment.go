package enginehost

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
)

// environmentName stays beside the lock and socket because this answer belongs
// to the host, never to a conversation or a credential sent across its wire.
const environmentName = "environment"

// canonicalEnvironment keeps only the environment codeaf actually reads for
// providers and settings. THE HOST'S ADDRESS IS EXCLUDED: two processes finding
// this socket already agreed on the state root, and shell bookkeeping must not
// make an idle host restart. Compatibility spellings collapse to the owned name.
func canonicalEnvironment(workspace string) string {
	profileDir := config.ProfilePath(config.ProfileDir(), "")
	// PROFILE REFERENCES RESOLVE WHERE THE HOST RUNS. The launcher can stand
	// elsewhere, but a daemon chdir must not turn an equal environment into a
	// different custom-key list on every launch.
	if profileDir != "" && !filepath.IsAbs(profileDir) {
		profileDir = filepath.Join(workspace, profileDir)
	}
	// The variable list is read from the profile at each side's moment.
	names := config.ProviderKeyEnvNamesAt(profileDir)
	names = append(names, "CODEAF_BASE_URL")
	for _, row := range config.NewSettings(config.SettingsOptions{ProfileDir: profileDir}).Rows() {
		if row.Env != "" {
			names = append(names, row.Env)
		}
	}
	seen := make(map[string]bool, len(names))
	var lines []string
	for _, name := range names {
		if strings.HasPrefix(name, env.Legacy("CODEAF_")) {
			name = "CODEAF_" + strings.TrimPrefix(name, env.Legacy("CODEAF_"))
		}
		if name == "CODEAF_HOME" || name == config.ProfileDirEnv || seen[name] {
			continue
		}
		seen[name] = true
		// Value is the dynamic env door: provider names are foreign, while
		// owned settings read with exactly Get's compatibility fallback.
		if value := env.Value(name); value != "" {
			lines = append(lines, name+"="+value)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func environmentMAC(salt []byte, workspace string) []byte {
	mac := hmac.New(sha256.New, salt)
	_, _ = mac.Write([]byte(canonicalEnvironment(workspace)))
	return mac.Sum(nil)
}

// writeEnvironment writes a fresh salt and digest atomically, so a launcher
// never compares a partly written answer. No environment value is saved, even
// when the environment contains a provider key with an unfamiliar shape.
func writeEnvironment(dir, workspace string) error {
	salt := make([]byte, sha256.Size)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".environment-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(append(salt, environmentMAC(salt, workspace)...)); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(dir, environmentName))
}

// EnvironmentDiffers answers only a complete fingerprint. Missing, unreadable
// or malformed files mean the host has not said, which must never be a reason
// to restart it. Comparison stays local and takes constant time for the digest.
func EnvironmentDiffers(workspace string) bool {
	dir, err := where(workspace)
	if err != nil {
		return false
	}
	body, err := os.ReadFile(filepath.Join(dir, environmentName))
	if err != nil || len(body) != 2*sha256.Size {
		return false
	}
	return !hmac.Equal(body[sha256.Size:], environmentMAC(body[:sha256.Size], workspace))
}
