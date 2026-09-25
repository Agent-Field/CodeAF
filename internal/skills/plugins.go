package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RootClaudePlugins is the Root every skill read out of a Claude Code plugin
// carries. It is where Claude Code keeps its plugin registry, not a folder
// this scan walks: the skills themselves live wherever each installation's
// own record says it was unpacked.
const RootClaudePlugins = ".claude/plugins"

// MOST OF THE CLAUDE CODE SKILLS A PERSON HAS ARRIVE INSIDE A PLUGIN, and a
// plugin's skills never sit in ~/.claude/skills: Claude Code unpacks each
// installed plugin into a versioned folder of its own and reads the skills out
// of that folder's skills/ directory, or out of the folders the plugin names.
// So this file reads them the way Claude
// Code itself decides which ones are live, and in no other way.
//
// A PLUGIN SKILL IS LIVE ONLY WHEN ITS PLUGIN IS BOTH INSTALLED AND ENABLED.
//
//   - INSTALLED means named in ~/.claude/plugins/installed_plugins.json, whose
//     `plugins` map keys each plugin as `name@marketplace` and lists its
//     installations. Each installation carries the absolute `installPath` it
//     was unpacked into and a `scope`: `user` is live in every project, while
//     `project` and `local` are live only in the one `projectPath` they name.
//     An older registry held one object per plugin instead of a list, with no
//     scope, and that shape reads as one user installation.
//   - ENABLED means the `enabledPlugins` map says true for that key, read the
//     way Claude Code layers its settings: ~/.claude/settings.json first, then
//     the project's .claude/settings.json, then its .claude/settings.local.json,
//     each later file overriding the one before. A plugin the map does not name,
//     or names with anything but true, is off.
//
// THE PLUGINS FOLDER IS NEVER WALKED. It holds marketplace clones listing
// plugins nobody installed, and cached older versions of plugins that were
// updated since, and a scan that walked it would offer a person skills their
// own Claude Code does not load. Only the paths the registry names are read.
//
// THE NAME IS THE SKILL'S OWN, AND A PLUGIN SKILL NEVER OUTRANKS A HAND-KEPT
// ONE. Claude Code spells a plugin skill `plugin:skill` so two plugins cannot
// collide, but codeaf's shelf knows a skill by its folder's name — that is the
// one identity every reader keys on (internal/store's Fact.SkillName), and
// the agentskills.io name has no colon in its alphabet. So a plugin skill
// keeps its bare name and settles a collision by rank instead: every skill in
// the six hand-kept folders of a scope owns the name over a plugin's, because
// a skill somebody placed by hand is the one they meant, and between two
// plugins the one whose key sorts first owns it. The loser stays in the result
// marked Shadowed, like every other loser.

// installedPluginsFile and the settings files are named relative to the two
// directories [Discover] is handed.
var (
	installedPluginsFile = filepath.Join(".claude", "plugins", "installed_plugins.json")
	claudeSettingsFiles  = []string{
		filepath.Join(".claude", "settings.json"),
		filepath.Join(".claude", "settings.local.json"),
	}
	pluginManifestFile     = filepath.Join(".claude-plugin", "plugin.json")
	marketplaceCatalogFile = filepath.Join(".claude-plugin", "marketplace.json")
	knownMarketplacesFile  = filepath.Join(".claude", "plugins", "known_marketplaces.json")
)

// claudePlugin is one installed and enabled plugin: its registry key and the
// folders its skills are read from, in the order they are read.
type claudePlugin struct {
	id           string
	skillFolders []string
}

// pluginInstall is one installation record in installed_plugins.json.
type pluginInstall struct {
	Scope       string `json:"scope"`
	InstallPath string `json:"installPath"`
	ProjectPath string `json:"projectPath"`
}

// claudePlugins answers which plugins are live for this home and project, split
// by the scope their skills belong to. Anything that cannot be read — no
// registry, a registry that does not parse, a settings file that does not —
// is absence, never an error: a broken plugin folder must not cost a person
// the skills in their other folders.
func claudePlugins(homeDir, projectDir string) (user, project []claudePlugin) {
	if homeDir == "" {
		return nil, nil
	}
	data, err := readPluginJSON(filepath.Join(absolute(homeDir), installedPluginsFile))
	if err != nil {
		return nil, nil
	}
	var registry struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if json.Unmarshal(data, &registry) != nil {
		return nil, nil
	}
	enabled := enabledPlugins(homeDir, projectDir)
	ids := make([]string, 0, len(registry.Plugins))
	for id := range registry.Plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if !enabled[id] {
			continue
		}
		var userFolders, projectFolders []string
		for _, install := range pluginInstalls(registry.Plugins[id]) {
			root := strings.TrimSpace(install.InstallPath)
			if root == "" || !filepath.IsAbs(root) {
				continue
			}
			switch strings.TrimSpace(install.Scope) {
			case "", "user":
				userFolders = appendNew(userFolders, pluginSkillFolders(homeDir, id, root)...)
			case "project", "local":
				if projectDir != "" && samePlace(install.ProjectPath, projectDir) {
					projectFolders = appendNew(projectFolders, pluginSkillFolders(homeDir, id, root)...)
				}
			}
		}
		if len(userFolders) > 0 {
			user = append(user, claudePlugin{id: id, skillFolders: userFolders})
		}
		if len(projectFolders) > 0 {
			project = append(project, claudePlugin{id: id, skillFolders: projectFolders})
		}
	}
	return user, project
}

// pluginInstalls reads one registry entry in either of its two shapes: the
// list of installations the current registry keeps, or the single object an
// older one kept.
func pluginInstalls(raw json.RawMessage) []pluginInstall {
	var many []pluginInstall
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	var one pluginInstall
	if json.Unmarshal(raw, &one) == nil {
		return []pluginInstall{one}
	}
	return nil
}

// enabledPlugins layers the `enabledPlugins` maps of the settings files in
// Claude Code's order, the later file overriding the earlier, and keeps the
// keys whose final value is exactly true.
func enabledPlugins(homeDir, projectDir string) map[string]bool {
	files := []string{filepath.Join(absolute(homeDir), claudeSettingsFiles[0])}
	if projectDir != "" {
		for _, name := range claudeSettingsFiles {
			files = append(files, filepath.Join(absolute(projectDir), name))
		}
	}
	merged := make(map[string]bool)
	for _, file := range files {
		data, err := readPluginJSON(file)
		if err != nil {
			continue
		}
		var settings struct {
			EnabledPlugins map[string]json.RawMessage `json:"enabledPlugins"`
		}
		if json.Unmarshal(data, &settings) != nil {
			continue
		}
		for id, raw := range settings.EnabledPlugins {
			var on bool
			merged[id] = json.Unmarshal(raw, &on) == nil && on
		}
	}
	return merged
}

// pluginSkillFolders is where one installed plugin keeps its skills.
//
// A PLUGIN THAT NAMES ITS SKILLS IS READ FOR THOSE AND NO OTHERS. The names can
// come from two places: the `skills` field of the plugin's own manifest, and
// the `skills` field of the plugin's entry in its marketplace's catalog, which
// is how one repository is split into several plugins that each load a part of
// it. Either spells a path or a list of paths relative to the plugin, and each
// path is a skill folder itself or a folder of skill folders. When either names
// anything, the default skills/ folder is not read unless it is named too:
// Claude Code's own inventory of such a plugin lists only the named folders,
// and reading the rest would offer skills the person installed a different
// plugin for, or none. A plugin that names nothing is read from skills/.
//
// A path that climbs out of the plugin is ignored: the registry vouches for the
// plugin's folder and for nothing outside it.
func pluginSkillFolders(homeDir, id, root string) []string {
	paths := declaredSkillPaths(filepath.Join(root, pluginManifestFile), "")
	if name, market, ok := strings.Cut(id, "@"); ok && name != "" && market != "" {
		paths = append(paths, marketplaceSkillPaths(homeDir, market, name, root)...)
	}
	if len(paths) == 0 {
		return []string{filepath.Join(root, "skills")}
	}
	var folders []string
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || filepath.IsAbs(path) {
			continue
		}
		folder := filepath.Join(root, path)
		relative, err := filepath.Rel(root, folder)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			continue
		}
		folders = appendNew(folders, folder)
	}
	return folders
}

// declaredSkillPaths reads the `skills` field of one JSON manifest: of the
// document itself when entry is empty, or of the plugin called entry in the
// document's `plugins` list, which is a marketplace catalog's shape. A file
// that is absent or does not parse names nothing.
func declaredSkillPaths(file, entry string) []string {
	data, err := readPluginJSON(file)
	if err != nil {
		return nil
	}
	var field json.RawMessage
	if entry == "" {
		var manifest struct {
			Skills json.RawMessage `json:"skills"`
		}
		if json.Unmarshal(data, &manifest) != nil {
			return nil
		}
		field = manifest.Skills
	} else {
		var catalog struct {
			Plugins []struct {
				Name   string          `json:"name"`
				Skills json.RawMessage `json:"skills"`
			} `json:"plugins"`
		}
		if json.Unmarshal(data, &catalog) != nil {
			return nil
		}
		for _, plugin := range catalog.Plugins {
			if plugin.Name == entry {
				field = plugin.Skills
				break
			}
		}
	}
	if len(field) == 0 {
		return nil
	}
	var one string
	if json.Unmarshal(field, &one) == nil {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(field, &many) == nil {
		return many
	}
	return nil
}

// marketplaceSkillPaths answers what a marketplace's catalog says one of its
// plugins' skills are. The catalog is looked for where Claude Code keeps it:
// the location its marketplace record names, then the conventional folder
// under the plugins directory, then the copy a plugin whose source is its
// whole marketplace carries inside its own install. The first catalog found is
// the one read.
func marketplaceSkillPaths(homeDir, market, plugin, installPath string) []string {
	var places []string
	if data, err := readPluginJSON(filepath.Join(absolute(homeDir), knownMarketplacesFile)); err == nil {
		var known map[string]struct {
			InstallLocation string `json:"installLocation"`
		}
		if json.Unmarshal(data, &known) == nil {
			if location := strings.TrimSpace(known[market].InstallLocation); filepath.IsAbs(location) {
				places = append(places, location)
			}
		}
	}
	places = append(places,
		filepath.Join(absolute(homeDir), ".claude", "plugins", "marketplaces", market),
		installPath)
	for _, place := range places {
		file := filepath.Join(place, marketplaceCatalogFile)
		if _, err := os.Stat(file); err != nil {
			continue
		}
		return declaredSkillPaths(file, plugin)
	}
	return nil
}

// appendNew appends the paths not already held, so a manifest that names the
// default skills/ folder, or a plugin installed twice at one path, is read
// once.
func appendNew(held []string, paths ...string) []string {
	for _, path := range paths {
		clean := filepath.Clean(path)
		seen := false
		for _, existing := range held {
			if existing == clean {
				seen = true
				break
			}
		}
		if !seen {
			held = append(held, clean)
		}
	}
	return held
}

// samePlace compares the project a registry entry names with the project being
// scanned, through links: the registry records the path Claude Code was
// opened at, and on a machine whose temporary or home folder is itself a link
// the two spellings of one directory differ.
func samePlace(recorded, projectDir string) bool {
	recorded = strings.TrimSpace(recorded)
	if recorded == "" {
		return false
	}
	resolve := func(path string) string {
		path = absolute(path)
		if real, err := filepath.EvalSymlinks(path); err == nil {
			return real
		}
		return filepath.Clean(path)
	}
	return resolve(recorded) == resolve(projectDir)
}
