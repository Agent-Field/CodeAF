package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// The desktop app's backend (cmd/codeaf's app.go) draws the same facts this
// surface draws, and it reads them through these doors rather than restating
// the rules, so a tool row and a model menu say the same thing in both places.

// ToolTitle is the one line a tool row says for a call: the tool's name and
// what the call is about, exactly as [app.toolLineLayout] lays them out before
// any width is spent (`read src/auth/session.ts`, `bash pnpm test`).
func ToolTitle(tool, args, hint string) string {
	name, _ := toolWords(tool, hint)
	target := toolTarget(tool, args, hint)
	if target == "" {
		return name
	}
	if name == "" {
		return target
	}
	return name + " " + target
}

// ProviderCatalog is every model provider the terminal's first-run screen and
// /connect offer, in the order they offer them, so the app's setup lists the
// same providers in the same order.
func ProviderCatalog(catalog []modelsource.Source) []modelsource.Source {
	return providerCatalog(catalog)
}

// PickerModels is the chat model picker's selectable rows for a profile's
// services: the default service's rows from defaults (the process catalog, as
// the terminal's Options.Models), every other connected service's from
// forService, and the small model caches on disk where the terminal reads them.
// Rows that are a group's notice rather than a model are left out. It never
// reaches the network.
func PickerModels(profileDir string, sources modelsource.Set, defaults func() []Model, forService func(modelsource.Connected) []Model) []Model {
	a := &app{profileDir: profileDir, sources: sources, models: defaults, modelsForService: forService}
	a.modelLists = newLearned(readModelCacheName)
	// The names the terminal learns when it opens, read here at once.
	a.modelLists.learn(modelCacheNameFor("", modelcatalog.DefaultBaseURL))
	for _, service := range sources.All() {
		a.modelLists.learn(modelCacheNameFor(service.Source.ID, service.Address))
	}
	var rows []Model
	for _, model := range a.modelList() {
		if model.Unavailable || model.Notice != "" || strings.TrimSpace(model.ID) == "" {
			continue
		}
		rows = append(rows, model)
	}
	return rows
}

// TaskPhaseWords is what the terminal draws under a running task for the phase
// it is in — "checking what it left", "closing gaps · round 1 of 2", "sizing
// the work" — and "" for plain working, which says nothing.
func TaskPhaseWords(phase string, round, rounds int) string {
	return taskPhaseWords(phase, round, rounds)
}

// ToolTarget is what a call is about, as its row says it after the tool's
// name: the command, the path, the query. "" when the call names nothing.
func ToolTarget(tool, args, hint string) string {
	return toolTarget(tool, args, hint)
}

// ToolFileChange is the file a call writes and the lines it adds and removes,
// by the same counts the terminal's row draws (+12 −3), and false for a call
// that writes no file it can count.
func ToolFileChange(tool, args string) (path string, adds, dels int, ok bool) {
	fields := argsOf(args)
	path = argString(fields, "path")
	switch tool {
	case "edit":
		adds, dels, _ = editStat(args)
	case "write":
		content, _ := argBody(argString(fields, "content"))
		adds = lineCount(content)
	default:
		return "", 0, 0, false
	}
	return path, adds, dels, path != ""
}

// PathEntry is one name in the directory a typed path reaches into, as
// [CompletePath] offers it.
type PathEntry struct {
	// Name is the entry's own name, with no separator after it.
	Name string
	// Path is what the sentence says once the entry is chosen: what was typed
	// up to its last separator, then Name, then a separator for a directory.
	// `~/code/af` becomes `~/code/agentfield/` and never an absolute spelling
	// the person did not write, because the path stays a word in their sentence
	// and the agent's own tools resolve it.
	Path string
	Dir  bool
}

// PathListing is [CompletePath]'s answer: the directory the typed path names,
// and the names in it that the half-typed last segment matches.
type PathListing struct {
	// Dir is that directory, absolute and cleaned.
	Dir     string
	Entries []PathEntry
	// More says matches were left off, past the limit or past the browser's
	// own bound on one directory's rows.
	More bool
	// Missing says nothing is there, or what is there is not a directory;
	// Unreadable says it is there and could not be read. THE TWO ARE NEVER AN
	// EMPTY LIST, for [folderListing]'s reason: a folder nobody may open and a
	// folder with nothing in it are different facts about a person's disk.
	Missing    bool
	Unreadable bool
}

// CompletePath is the folder browser's typed-path walk (folderpick.go's
// [folderPick.browseSync]) answered as one list, for codeaf app's `@`:
// the directory a typed path names is listed, directories first and then
// files, and the half-typed name after the last separator narrows it — names
// that begin with it first, then names that hold it.
//
// IT IS THIS SURFACE'S RULES AND NOT A COPY OF THEM. The path is resolved by
// [resolvePathAt] (`~` is home, a relative path hangs off root, an absolute one
// is left alone), the directory is read by [folderEntries] (one readdir,
// bounded, dot names and `.git`/`node_modules`/`vendor` left out unless the
// half-typed name itself begins with a dot — the shell's rule and
// browseSync's), and `~`, `.` and `..` typed alone are the directories they
// name, as [folderPathish] reads them.
//
// IT READS ONE DIRECTORY AND NOTHING ELSE: no walk, no file contents, no
// write. root is where a relative path hangs, and home when it is empty; a
// limit above zero bounds the entries.
func CompletePath(typed, root string, limit int) PathListing {
	if root == "" {
		if home, err := os.UserHomeDir(); err == nil {
			root = home
		}
	}
	// head is the directory part as typed, leaf the half-typed name, and
	// spelled what every offered path begins with. A bare name has no
	// directory part and is looked for under root.
	head, leaf, spelled := "", typed, ""
	switch {
	case typed == "~" || typed == "." || typed == "..":
		head, leaf, spelled = typed, "", typed+"/"
	case strings.Contains(typed, "/"):
		head, leaf = pathHead(typed)
		spelled = typed[:len(typed)-len(leaf)]
	}
	dir := filepath.Clean(resolvePathAt(head, root, wslPaths{}))
	out := PathListing{Dir: dir}
	read := folderEntries(dir, strings.HasPrefix(leaf, "."))
	switch {
	case read.err == nil:
	case os.IsNotExist(read.err) || errors.Is(read.err, syscall.ENOTDIR):
		out.Missing = true
		return out
	default:
		out.Unreadable = true
		return out
	}
	needle := strings.ToLower(leaf)
	var starts, holds []PathEntry
	for at := 0; at < read.rows(); at++ {
		name := read.rowName(at)
		entry := PathEntry{Name: name, Path: spelled + name, Dir: read.isDir(at)}
		if entry.Dir {
			entry.Path += "/"
		}
		switch lower := strings.ToLower(name); {
		case strings.HasPrefix(lower, needle):
			starts = append(starts, entry)
		case strings.Contains(lower, needle):
			holds = append(holds, entry)
		}
	}
	out.Entries = append(starts, holds...)
	if limit > 0 && len(out.Entries) > limit {
		out.Entries, out.More = out.Entries[:limit], true
	}
	out.More = out.More || read.cut
	return out
}
