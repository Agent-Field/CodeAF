package skills

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// InputPaths names the roots and control files that discovery depends on. It
// includes absent paths so a later installation invalidates a remembered scan,
// and names marketplace catalogs without walking marketplace repositories.
func InputPaths(opts Options) []string {
	var paths []string
	for _, base := range []string{opts.ProjectDir, opts.HomeDir} {
		if strings.TrimSpace(base) == "" {
			continue
		}
		for _, root := range append(append([]string(nil), skillRoots...), RootCodexSystem) {
			paths = appendNew(paths, filepath.Join(absolute(base), root))
		}
		for _, file := range claudeSettingsFiles {
			paths = appendNew(paths, filepath.Join(absolute(base), file))
		}
	}
	if strings.TrimSpace(opts.HomeDir) == "" {
		return paths
	}
	home := absolute(opts.HomeDir)
	for _, root := range homeSkillRoots {
		paths = appendNew(paths, filepath.Join(home, root))
	}
	paths = appendNew(paths, filepath.Join(home, installedPluginsFile), filepath.Join(home, knownMarketplacesFile))
	var registry struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	data, _ := readPluginJSON(filepath.Join(home, installedPluginsFile))
	_ = json.Unmarshal(data, &registry)
	var known map[string]struct {
		InstallLocation string `json:"installLocation"`
	}
	data, _ = readPluginJSON(filepath.Join(home, knownMarketplacesFile))
	_ = json.Unmarshal(data, &known)
	for id, raw := range registry.Plugins {
		for _, install := range pluginInstalls(raw) {
			if !filepath.IsAbs(install.InstallPath) {
				continue
			}
			paths = appendNew(paths, filepath.Join(install.InstallPath, pluginManifestFile), filepath.Join(install.InstallPath, marketplaceCatalogFile))
		}
		if _, market, ok := strings.Cut(id, "@"); ok {
			paths = appendNew(paths, filepath.Join(home, ".claude", "plugins", "marketplaces", market, marketplaceCatalogFile))
			if location := known[market].InstallLocation; filepath.IsAbs(location) {
				paths = appendNew(paths, filepath.Join(location, marketplaceCatalogFile))
			}
		}
	}
	user, project := claudePlugins(opts.HomeDir, opts.ProjectDir)
	for _, plugin := range append(user, project...) {
		paths = appendNew(paths, plugin.skillFolders...)
	}
	return paths
}
