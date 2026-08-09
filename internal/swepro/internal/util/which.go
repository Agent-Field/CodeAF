// Executable lookup — port of src/util/which.ts:1-14
// (swe-pro 3b25a1a).
package util

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func Which(command string, env map[string]string, binDir string) (string, bool) {
	base := firstEnvironment(env, "PATH", "Path")
	if base == "" {
		base = firstEnvironmentMap(os.Environ(), "PATH", "Path")
	}
	full := binDir
	if base != "" {
		full = base + string(os.PathListSeparator) + binDir
	}
	extensions := firstEnvironment(env, "PATHEXT", "PathExt")
	if extensions == "" {
		extensions = firstEnvironmentMap(os.Environ(), "PATHEXT", "PathExt")
	}
	for _, dir := range filepath.SplitList(full) {
		candidates := []string{command}
		if runtime.GOOS == "windows" && filepath.Ext(command) == "" {
			for _, extension := range strings.Split(extensions, ";") {
				if extension != "" {
					candidates = append(candidates, command+strings.ToLower(extension), command+strings.ToUpper(extension))
				}
			}
		}
		for _, candidate := range candidates {
			path := filepath.Join(dir, candidate)
			info, err := os.Stat(path)
			if err == nil && !info.IsDir() && (runtime.GOOS == "windows" || info.Mode()&0o111 != 0) {
				absolute, _ := filepath.Abs(path)
				return absolute, true
			}
		}
	}
	return "", false
}

func firstEnvironment(env map[string]string, keys ...string) string {
	for _, key := range keys {
		if value, ok := env[key]; ok {
			return value
		}
	}
	return ""
}

func firstEnvironmentMap(values []string, keys ...string) string {
	env := map[string]string{}
	for _, value := range values {
		key, item, _ := strings.Cut(value, "=")
		env[key] = item
	}
	return firstEnvironment(env, keys...)
}
