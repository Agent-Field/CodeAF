//go:build !windows

// Npm package helper. Installation is behind the narrow Reifier interface;
// the default implementation invokes npm with save/ignore-scripts settings.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// InstallFailedError is the tagged npm installation failure.
type InstallFailedError struct {
	Add   []string
	Dir   string
	Cause error
}

func (e *InstallFailedError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("NpmInstallFailedError: dir=%s add=%v", e.Dir, e.Add)
	}
	return fmt.Sprintf("NpmInstallFailedError: dir=%s add=%v: %v", e.Dir, e.Add, e.Cause)
}

func (e *InstallFailedError) Unwrap() error { return e.Cause }

// EntryPoint is the installed package directory and optional import entry.
type EntryPoint struct {
	Directory  string  `json:"directory"`
	Entrypoint *string `json:"entrypoint"`
}

// PackageRequest is an extra dependency requested by Install.
type PackageRequest struct {
	Name    string
	Version string
}

// ReifiedNode is the package a reify installed.
type ReifiedNode struct {
	Name string
	Path string
}

// Reifier performs one npm reify operation.
type Reifier interface {
	Reify(ctx context.Context, dir string, add []string) (*ReifiedNode, error)
}

// Npm is an npm package-cache service.
type Npm struct {
	CacheDir string
	FS       *AppFileSystem
	Reifier  Reifier

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewNpm constructs an npm helper rooted at cacheDir.
func NewNpm(cacheDir string, reifier Reifier) *Npm {
	fs := NewFileSystem()
	if reifier == nil {
		reifier = &commandReifier{spawner: NewSpawner()}
	}
	return &Npm{CacheDir: cacheDir, FS: fs, Reifier: reifier, locks: make(map[string]*sync.Mutex)}
}

func (n *Npm) directory(pkg string) string {
	return filepath.Join(n.CacheDir, "packages", Sanitize(pkg))
}

func (n *Npm) lock(dir string) func() {
	n.mu.Lock()
	lock := n.locks[dir]
	if lock == nil {
		lock = &sync.Mutex{}
		n.locks[dir] = lock
	}
	n.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (n *Npm) reify(ctx context.Context, dir string, add []string) (*ReifiedNode, error) {
	unlock := n.lock(dir)
	defer unlock()
	node, err := n.Reifier.Reify(ctx, dir, add)
	if err != nil {
		return nil, &InstallFailedError{Add: append([]string(nil), add...), Dir: dir, Cause: err}
	}
	return node, nil
}

// Add installs pkg in its isolated package cache.
func (n *Npm) Add(ctx context.Context, pkg string) (EntryPoint, error) {
	dir := n.directory(pkg)
	name := packageName(pkg)
	installed := filepath.Join(dir, "node_modules", filepath.FromSlash(name))
	if n.FS.ExistsSafe(installed) {
		return resolveEntryPoint(name, installed), nil
	}
	first, err := n.reify(ctx, dir, []string{pkg})
	if err != nil {
		return EntryPoint{}, err
	}
	if first == nil {
		result := resolveEntryPoint(name, installed)
		if result.Entrypoint != nil {
			return result, nil
		}
		return EntryPoint{}, &InstallFailedError{Add: []string{pkg}, Dir: dir}
	}
	return resolveEntryPoint(first.Name, first.Path), nil
}

// Install reifies dir when node_modules is absent or the root lockfile omits a
// declared dependency. An unwritable directory is silently skipped.
func (n *Npm) Install(ctx context.Context, dir string, input ...[]PackageRequest) error {
	if !writable(dir) {
		return nil
	}
	requests := []PackageRequest{}
	if len(input) > 0 {
		requests = input[0]
	}
	add := make([]string, 0, len(requests))
	for _, pkg := range requests {
		if pkg.Version == "" {
			add = append(add, pkg.Name)
		} else {
			add = append(add, pkg.Name+"@"+pkg.Version)
		}
	}
	if !n.FS.ExistsSafe(filepath.Join(dir, "node_modules")) {
		_, err := n.reify(ctx, dir, add)
		return err
	}

	pkg := readJSONObject(filepath.Join(dir, "package.json"))
	lock := readJSONObject(filepath.Join(dir, "package-lock.json"))
	declared := map[string]bool{}
	for _, key := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
		for name := range objectMap(pkg[key]) {
			declared[name] = true
		}
	}
	for _, request := range requests {
		declared[request.Name] = true
	}
	root := objectMap(objectMap(lock["packages"])[""])
	locked := map[string]bool{}
	for _, key := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
		for name := range objectMap(root[key]) {
			locked[name] = true
		}
	}
	for name := range declared {
		if !locked[name] {
			_, err := n.reify(ctx, dir, add)
			return err
		}
	}
	return nil
}

// Which finds a package-provided executable, repairing the isolated install
// once when no bin exists.
func (n *Npm) Which(ctx context.Context, pkg string, bin ...string) (string, bool) {
	dir := n.directory(pkg)
	binDir := filepath.Join(dir, "node_modules", ".bin")
	hint := ""
	if len(bin) > 0 {
		hint = bin[0]
	}
	pick := func() (string, bool) {
		directory, err := os.Open(binDir)
		if err != nil {
			return "", false
		}
		files, err := directory.Readdirnames(-1)
		_ = directory.Close()
		if err != nil || len(files) == 0 {
			return "", false
		}
		if hint != "" {
			for _, file := range files {
				if file == hint {
					return file, true
				}
			}
			return "", false
		}
		if len(files) == 1 {
			return files[0], true
		}
		var manifest struct {
			Bin json.RawMessage `json:"bin"`
		}
		if data, err := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), "package.json")); err == nil &&
			json.Unmarshal(data, &manifest) == nil && len(manifest.Bin) > 0 && string(manifest.Bin) != "null" {
			var path string
			if json.Unmarshal(manifest.Bin, &path) == nil {
				return unscoped(pkg), true
			}
			order := orderedObjectKeys(manifest.Bin)
			if len(order) == 1 {
				return order[0], true
			}
			name := unscoped(pkg)
			for _, key := range order {
				if key == name {
					return name, true
				}
			}
			if len(order) > 0 {
				return order[0], true
			}
		}
		return files[0], true
	}
	if selected, ok := pick(); ok {
		return filepath.Join(binDir, selected), true
	}
	_ = os.Remove(filepath.Join(dir, "package-lock.json"))
	if _, err := n.Add(ctx, pkg); err != nil {
		return "", false
	}
	selected, ok := pick()
	if !ok {
		return "", false
	}
	return filepath.Join(binDir, selected), true
}

// Sanitize replaces Windows-illegal package path characters. It is a no-op
// on non-Windows platforms.
func Sanitize(pkg string) string {
	return sanitizeForPlatform(pkg, runtime.GOOS)
}

func sanitizeForPlatform(pkg, goos string) string {
	if goos != "windows" {
		return pkg
	}
	illegal := `<>:"|?*`
	var out strings.Builder
	for _, char := range pkg {
		if char < 32 || strings.ContainsRune(illegal, char) {
			out.WriteByte('_')
		} else {
			out.WriteRune(char)
		}
	}
	return out.String()
}

func packageName(pkg string) string {
	if strings.HasPrefix(pkg, "@") {
		slash := strings.IndexByte(pkg, '/')
		if slash < 0 {
			return pkg
		}
		if at := strings.IndexByte(pkg[slash:], '@'); at >= 0 {
			return pkg[:slash+at]
		}
		return pkg
	}
	if at := strings.IndexByte(pkg, '@'); at > 0 {
		return pkg[:at]
	}
	return pkg
}

func unscoped(pkg string) string {
	if strings.HasPrefix(pkg, "@") {
		parts := strings.Split(pkg, "/")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	return pkg
}

func resolveEntryPoint(_ string, dir string) EntryPoint {
	manifest := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		return EntryPoint{Directory: dir}
	}
	var pkg struct {
		Main   string `json:"main"`
		Module string `json:"module"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return EntryPoint{Directory: dir}
	}
	entry := pkg.Module
	if entry == "" {
		entry = pkg.Main
	}
	if entry == "" {
		entry = "index.js"
	}
	resolved := filepath.Join(dir, filepath.FromSlash(entry))
	if _, err := os.Stat(resolved); err != nil {
		return EntryPoint{Directory: dir}
	}
	return EntryPoint{Directory: dir, Entrypoint: &resolved}
}

func writable(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	file, err := os.CreateTemp(dir, ".senior-dev-write-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}

func readJSONObject(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(data, &out) != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func objectMap(value any) map[string]any {
	out, ok := value.(map[string]any)
	if !ok || out == nil {
		return map[string]any{}
	}
	return out
}

func orderedObjectKeys(raw []byte) []string {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil
	}
	keys := []string{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return keys
		}
		key, ok := token.(string)
		if !ok {
			return keys
		}
		keys = append(keys, key)
		var discard any
		if err := decoder.Decode(&discard); err != nil {
			return keys
		}
	}
	return keys
}

type commandReifier struct {
	spawner *Spawner
}

func (r *commandReifier) Reify(ctx context.Context, dir string, add []string) (*ReifiedNode, error) {
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return nil, err
	}
	args := []string{"install", "--ignore-scripts", "--save", "--save-prod", "--save-prefix="}
	args = append(args, add...)
	_, stderr, code, err := r.spawner.Run(ctx, MakeCommand("npm", args, CommandOptions{Cwd: dir}))
	if err != nil || code != 0 {
		if err == nil {
			err = errors.New(strings.TrimSpace(string(stderr)))
		}
		return nil, err
	}
	if len(add) == 0 {
		return nil, nil
	}
	name := packageName(add[0])
	return &ReifiedNode{Name: name, Path: filepath.Join(dir, "node_modules", filepath.FromSlash(name))}, nil
}
