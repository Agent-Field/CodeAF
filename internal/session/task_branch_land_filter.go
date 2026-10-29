package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const landAttributeReadMax = 8 << 20

// landFilters binds the attributes git will use before status or merge-tree
// can run a filter. Status compares the tracked checkout too, so those paths
// join the branch's additions, modifications, deletions and rename endpoints.
func landFilters(ctx context.Context, plan *TaskBranchLandPlan) error {
	if len(landFilterDrivers(plan.settings)) == 0 {
		plan.attributes = ""
		return nil
	}
	paths, err := landFilterPaths(ctx, plan.Root, plan.Base, plan.BranchHead)
	if err != nil {
		return err
	}
	plan.attributes, err = landFilterAttributes(ctx, plan.Root, plan.settings, paths, "", plan.Head, plan.BranchHead, plan.Base)
	return err
}

func landFilterPaths(ctx context.Context, root, base, branch string) ([]string, error) {
	paths := map[string]bool{}
	for _, args := range [][]string{
		{"ls-files", "--cached", "-z"},
		{"diff", "--no-ext-diff", "--no-textconv", "--name-only", "--no-renames", "-z", base, branch, "--"},
	} {
		out, code, err := landRead(ctx, root, landAttributeReadMax, args...)
		if err != nil || code != 0 || len(out) > 0 && out[len(out)-1] != 0 {
			return nil, landReadFailed(err, refuseLand(LandFilter, "this land cannot determine which files run a filter program"))
		}
		for _, path := range strings.Split(string(out), "\x00") {
			if path != "" {
				paths[path] = true
			}
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	return ordered, nil
}

// landFilterAttributes asks git itself, preserving attribute precedence,
// macros, nested files, info/attributes and core.attributesFile. The checkout
// and each tree are separate readings: either side may introduce a filter or
// remove an attribute that still governs a path compared against the base.
func landFilterAttributes(ctx context.Context, root string, config landConfigFacts, paths []string, sources ...string) (digest string, result error) {
	drivers := landFilterDrivers(config)
	if len(drivers) == 0 {
		return "", nil
	}
	caps, err := landGitAttributeCapabilities(ctx, root)
	if err != nil {
		return "", err
	}
	scratch := ""
	defer func() {
		if scratch != "" {
			if err := os.RemoveAll(scratch); err != nil && result == nil {
				digest, result = "", landUnknownAttributes()
			}
		}
	}()
	input := []byte(strings.Join(paths, "\x00"))
	if len(paths) > 0 {
		input = append(input, 0)
	}
	sum := sha256.New()
	if err := landAttributeFiles(ctx, root, paths, sum, caps); err != nil {
		return "", err
	}
	filtered := map[string]bool{}
	for _, source := range sources {
		out, diagnostics, code, err := landTreeAttributes(ctx, root, source, input, caps, &scratch)
		unknown := refuseLand(LandFilter, "this land cannot determine which files run a filter program")
		if err != nil || code != 0 || len(diagnostics) != 0 {
			return "", landReadFailed(err, unknown)
		}
		records := bytes.Split(out, []byte{0})
		if len(records) != len(paths)*3+1 || len(records[len(records)-1]) != 0 {
			return "", unknown
		}
		for i, path := range paths {
			if string(records[3*i]) != path || string(records[3*i+1]) != "filter" {
				return "", unknown
			}
			if drivers[string(records[3*i+2])] {
				filtered[path] = true
			}
		}
		sum.Write(out)
	}
	var refused []string
	for _, path := range paths {
		if filtered[path] {
			refused = append(refused, path)
		}
	}
	if len(refused) > 0 {
		return "", refuseLand(LandFilter, "this repository runs a filter program on its files, and a land cannot vouch for it", refused...)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// landAttributeFiles binds readable attribute files outside the committed
// trees. Git sometimes silently ignores a read failure (a directory used as
// core.attributesFile, for example), so a successful check-attr is not enough.
func landAttributeFiles(ctx context.Context, root string, paths []string, sum io.Writer, caps *landAttributeCapabilities) error {
	unknown := refuseLand(LandFilter, "this land cannot determine which files run a filter program")
	files := map[string]bool{}
	locations, disabled, err := landAttributeLocations(ctx, root, caps)
	if err != nil {
		return err
	}
	for _, name := range disabled {
		fmt.Fprintf(sum, "%s:disabled;", name)
	}
	for _, path := range locations {
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		files[path] = true
	}
	for _, path := range paths {
		for dir := filepath.Dir(filepath.FromSlash(path)); ; dir = filepath.Dir(dir) {
			file := filepath.Join(root, dir, ".gitattributes")
			if files[file] {
				break
			}
			files[file] = true
			if dir == "." {
				break
			}
		}
	}
	ordered := make([]string, 0, len(files))
	for file := range files {
		ordered = append(ordered, file)
	}
	sort.Strings(ordered)
	left := int64(landAttributeReadMax)
	for _, path := range ordered {
		fmt.Fprintf(sum, "%d:%s;", len(path), path)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			fmt.Fprint(sum, "missing;")
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return unknown
		}
		if info.Size() > left {
			return refuseLand(LandTooLarge, "this land's attribute files are larger than it reads")
		}
		file, err := os.Open(path)
		if err != nil {
			return unknown
		}
		data, err := io.ReadAll(io.LimitReader(file, left+1))
		file.Close()
		if err != nil {
			return unknown
		}
		left -= int64(len(data))
		if left < 0 {
			return refuseLand(LandTooLarge, "this land's attribute files are larger than it reads")
		}
		fmt.Fprintf(sum, "%d:", len(data))
		sum.Write(data)
	}
	return nil
}

// landFilterDrivers names the filter drivers that have a program to run. With
// none, no attribute can make git run a filter, so a land asks git nothing about
// attributes: an older git without those queries still lands, and a driver added
// after the plan changes the configuration digest the plan already binds.
func landFilterDrivers(config landConfigFacts) map[string]bool {
	drivers := map[string]bool{}
	for key, values := range config.values {
		if !strings.HasPrefix(key, "filter.") {
			continue
		}
		dot := strings.LastIndexByte(key, '.')
		if dot < len("filter.") {
			continue
		}
		switch key[dot+1:] {
		case "clean", "smudge", "process":
			// Subsection names (the driver) are case-sensitive in git config.
			if len(values) > 0 && values[len(values)-1] != "" {
				drivers[key[len("filter."):dot]] = true
			}
		}
	}
	return drivers
}

// Capability entries bind executable identity and the current version line,
// because a macOS shim can select another Git without changing its own file.
// Repository facts must still be read again under the land lock.
type landAttributeCapabilities struct {
	mu                                sync.Mutex
	path                              string
	info                              os.FileInfo
	version                           string
	major, minor                      int
	parsed, apple                     bool
	treeUnsupported, filesUnsupported bool
}

var landAttributeCapabilityCache struct {
	sync.Mutex
	entries []*landAttributeCapabilities
}

var landAppleVersion = regexp.MustCompile(`^git version \S+ \(Apple Git-[0-9]+(\.[0-9]+)*\)$`)
var landVersionNumbers = regexp.MustCompile(`^git version ([0-9]+)\.([0-9]+)([. ]|$)`)

func resetLandAttributeCapabilities() {
	landAttributeCapabilityCache.Lock()
	defer landAttributeCapabilityCache.Unlock()
	landAttributeCapabilityCache.entries = nil
}

func landUnknownAttributes() error {
	return refuseLand(LandFilter, "this land cannot determine which files run a filter program")
}

func landGitAttributeCapabilities(ctx context.Context, root string) (*landAttributeCapabilities, error) {
	command := landCommand(ctx, root, "version")
	info, statErr := os.Stat(command.Path)
	cacheable := command.Err == nil && statErr == nil
	out, diagnostics, code, err := landReadInput(ctx, root, 64<<10, nil, "version")
	if err != nil || code != 0 || len(diagnostics) != 0 || !bytes.HasSuffix(out, []byte{'\n'}) || bytes.Count(out, []byte{'\n'}) != 1 || !bytes.HasPrefix(out, []byte("git version ")) {
		return nil, landReadFailed(err, landUnknownAttributes())
	}
	version := string(out[:len(out)-1])
	landAttributeCapabilityCache.Lock()
	defer landAttributeCapabilityCache.Unlock()
	if cacheable {
		for _, entry := range landAttributeCapabilityCache.entries {
			if entry.path == command.Path && entry.version == version && os.SameFile(entry.info, info) && entry.info.Size() == info.Size() && entry.info.ModTime().Equal(info.ModTime()) {
				return entry, nil
			}
		}
	}
	entry := &landAttributeCapabilities{path: command.Path, info: info, version: version}
	entry.apple = landAppleVersion.MatchString(entry.version)
	if parts := landVersionNumbers.FindStringSubmatch(entry.version); parts != nil {
		major, e1 := strconv.Atoi(parts[1])
		minor, e2 := strconv.Atoi(parts[2])
		if e1 == nil && e2 == nil {
			entry.major, entry.minor, entry.parsed = major, minor, true
		}
	}
	if cacheable {
		// A new executable identity or version at the same path retires its old
		// entry, so upgrades and shim switches keep one entry per path.
		kept := landAttributeCapabilityCache.entries[:0]
		for _, old := range landAttributeCapabilityCache.entries {
			if old.path != entry.path {
				kept = append(kept, old)
			}
		}
		landAttributeCapabilityCache.entries = append(kept, entry)
	}
	return entry, nil
}

func (c *landAttributeCapabilities) older(major, minor int) bool {
	return c.parsed && (c.major < major || c.major == major && c.minor < minor)
}

// landUnsupportedTreeAttributes accepts only the complete usage reply for this
// source, so warnings and truncated errors cannot become cached capabilities.
func landUnsupportedTreeAttributes(diagnostics []byte, source string) bool {
	if !bytes.HasSuffix(diagnostics, []byte{'\n'}) {
		return false
	}
	lines := strings.Split(string(diagnostics), "\n")
	if len(lines) < 3 || lines[0] != "error: unknown option `source="+source+"'" || !strings.HasPrefix(lines[1], "usage: git check-attr ") {
		return false
	}
	for _, line := range lines[2:] {
		if line != "" && strings.TrimLeft(line, " \t\r\v\f") == line {
			return false
		}
	}
	return true
}

func landTreeAttributes(ctx context.Context, root, source string, input []byte, caps *landAttributeCapabilities, scratch *string) ([]byte, []byte, int, error) {
	caps.mu.Lock()
	unsupported := caps.treeUnsupported
	caps.mu.Unlock()
	if source == "" || !unsupported {
		args := []string{"check-attr", "-z", "--stdin"}
		if source != "" {
			args = append(args, "--source="+source)
		}
		args = append(args, "filter")
		out, diagnostics, code, err := landReadInput(ctx, root, landAttributeReadMax, input, args...)
		if source == "" || err != nil || code != 129 || len(out) != 0 || !landUnsupportedTreeAttributes(diagnostics, source) || !caps.older(2, 40) {
			return out, diagnostics, code, err
		}
		caps.mu.Lock()
		caps.treeUnsupported = true
		caps.mu.Unlock()
	}
	if *scratch == "" {
		dir, err := os.MkdirTemp("", "codeaf-land-attributes-")
		if err != nil {
			return nil, nil, -1, landUnknownAttributes()
		}
		*scratch = dir
		absolute, err := filepath.Abs(dir)
		if err != nil {
			return nil, nil, -1, landUnknownAttributes()
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, nil, -1, landUnknownAttributes()
		}
		canonicalRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, nil, -1, landUnknownAttributes()
		}
		canonicalRoot, err = filepath.Abs(canonicalRoot)
		if err != nil {
			return nil, nil, -1, landUnknownAttributes()
		}
		// TMPDIR may point into the checkout, including through a symlink. The
		// caller removes scratch on refusal before any private indexes are made.
		// A linked worktree's Git directory outside this root is out of scope.
		relative, err := filepath.Rel(canonicalRoot, canonical)
		if err != nil || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, nil, -1, landUnknownAttributes()
		}
		*scratch = canonical
		if err := os.Mkdir(filepath.Join(canonical, "hooks"), 0o700); err != nil {
			return nil, nil, -1, landUnknownAttributes()
		}
	}
	// Every tree starts with a nonexistent index; split indexes and index-change
	// hooks would otherwise write in the repository while preparing this reading.
	slot, err := os.MkdirTemp(*scratch, "tree-")
	if err != nil {
		return nil, nil, -1, landUnknownAttributes()
	}
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(slot, "index")}
	quiet := []string{"-c", "core.splitIndex=false", "-c", "index.sparse=false", "-c", "core.hooksPath=" + filepath.Join(*scratch, "hooks")}
	args := append(append([]string{}, quiet...), "read-tree", "--no-sparse-checkout", "--no-recurse-submodules", source)
	out, diagnostics, code, err := landReadInputEnv(ctx, root, landAttributeReadMax, nil, env, args...)
	if err != nil || code != 0 || len(out) != 0 || len(diagnostics) != 0 {
		return nil, nil, -1, landReadFailed(err, landUnknownAttributes())
	}
	args = append(append([]string{}, quiet...), "check-attr", "--cached", "-z", "--stdin", "filter")
	return landReadInputEnv(ctx, root, landAttributeReadMax, input, env, args...)
}

func landAttributeLocations(ctx context.Context, root string, caps *landAttributeCapabilities) ([]string, []string, error) {
	caps.mu.Lock()
	legacy := caps.filesUnsupported
	caps.mu.Unlock()
	var files, disabled []string
	if !legacy {
		for _, name := range []string{"GIT_ATTR_GLOBAL", "GIT_ATTR_SYSTEM"} {
			out, diagnostics, code, err := landReadInput(ctx, root, 64<<10, nil, "var", name)
			if err == nil && code == 129 && len(out) == 0 && bytes.Equal(diagnostics, []byte("usage: git var (-l | <variable>)\n")) && caps.older(2, 42) {
				caps.mu.Lock()
				caps.filesUnsupported = true
				caps.mu.Unlock()
				legacy = true
				break
			}
			if err != nil || code != 0 || len(diagnostics) != 0 || !bytes.HasSuffix(out, []byte{'\n'}) {
				return nil, nil, landReadFailed(err, landUnknownAttributes())
			}
			if path := string(out[:len(out)-1]); path == "" {
				disabled = append(disabled, name)
			} else {
				files = append(files, path)
			}
		}
	}
	if legacy {
		var err error
		files, disabled, err = landLegacyAttributeLocations(ctx, root, caps)
		if err != nil {
			return nil, nil, err
		}
	}
	out, diagnostics, code, err := landReadInput(ctx, root, 64<<10, nil, "rev-parse", "--git-path", "info/attributes")
	if err != nil || code != 0 || len(diagnostics) != 0 || !bytes.HasSuffix(out, []byte{'\n'}) || len(out) == 1 {
		return nil, nil, landReadFailed(err, landUnknownAttributes())
	}
	return append(files, string(out[:len(out)-1])), disabled, nil
}

func landAttributePath(out, diagnostics []byte, code int, err error) (string, error) {
	if err != nil || code != 0 || len(diagnostics) != 0 || len(out) == 0 || out[len(out)-1] != 0 || bytes.Count(out, []byte{0}) != 1 {
		return "", landReadFailed(err, landUnknownAttributes())
	}
	return string(out[:len(out)-1]), nil
}

func landLegacyAttributeLocations(ctx context.Context, root string, caps *landAttributeCapabilities) ([]string, []string, error) {
	// os/exec keeps the last occurrence of a variable, including an empty value.
	env := landCommand(ctx, root, "version").Env
	if env == nil {
		env = os.Environ()
	}
	values := map[string]string{}
	for _, kv := range env {
		if key, value, ok := strings.Cut(kv, "="); ok {
			values[key] = value
		}
	}
	var files, disabled []string
	out, diagnostics, code, err := landReadInput(ctx, root, 64<<10, nil, "config", "--null", "--path", "--get", "core.attributesFile")
	if err == nil && code == 1 && len(out) == 0 && len(diagnostics) == 0 {
		if xdg := values["XDG_CONFIG_HOME"]; xdg != "" {
			files = append(files, xdg+"/git/attributes")
		} else if home, ok := values["HOME"]; ok {
			files = append(files, home+"/.config/git/attributes")
		} else {
			return nil, nil, landUnknownAttributes()
		}
	} else {
		path, err := landAttributePath(out, diagnostics, code, err)
		if err != nil {
			return nil, nil, err
		}
		if path == "" {
			disabled = append(disabled, "GIT_ATTR_GLOBAL")
		} else {
			files = append(files, path)
		}
	}
	system, err := landLegacySystemAttributes(ctx, root, caps.apple, values["GIT_ATTR_NOSYSTEM"])
	if err != nil {
		return nil, nil, err
	}
	if len(system) == 0 {
		disabled = append(disabled, "GIT_ATTR_SYSTEM")
	}
	return append(files, system...), disabled, nil
}

func landLegacySystemAttributes(ctx context.Context, root string, apple bool, noSystem string) ([]string, error) {
	var disabled bool
	switch strings.ToLower(noSystem) {
	case "", "false", "no", "off":
	case "true", "yes", "on":
		disabled = true
	default:
		n, err := strconv.ParseInt(noSystem, 10, 32)
		if err != nil {
			return nil, landUnknownAttributes()
		}
		disabled = n != 0
	}
	if disabled {
		return nil, nil
	}
	if !apple {
		return nil, landUnknownAttributes()
	}
	// Apple's Git sources, Git-146 through Git-155, read both system_path of
	// share/git-core/gitattributes and system_path of ETC_GITATTRIBUTES, which
	// Apple's Makefile sets to /etc/gitattributes. Both obey GIT_ATTR_NOSYSTEM.
	// Git expands %(prefix)/ through that same system_path, preserving relocation.
	path, err := landAttributePath(landReadInput(ctx, root, 64<<10, nil, "-c", "codeaf.landattributes=%(prefix)/share/git-core/gitattributes", "config", "--null", "--path", "--get", "codeaf.landattributes"))
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) {
		return nil, landUnknownAttributes()
	}
	return []string{path, "/etc/gitattributes"}, nil
}
