package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// Observer turns executed calls into inventory facts. It implements
// executor.Observer and never fails a call: an observation that cannot be
// stored is dropped, because the inventory is a record, not a gate.
type Observer struct {
	store  *Store
	probes Probes
	probe  Prober

	mu     sync.Mutex
	hashes map[fileKey]string // binary hash by path and mtime
	probed map[string]string  // version by binary hash, present once probed
}

type fileKey struct {
	path  string
	mtime time.Time
}

// NewObserver watches calls on behalf of store. A nil probes uses
// DefaultProbes; a nil probe runs the binary.
func NewObserver(store *Store, probes Probes, probe Prober) *Observer {
	if probes == nil {
		probes = DefaultProbes
	}
	if probe == nil {
		probe = execProbe
	}
	o := &Observer{store: store, probes: probes, probe: probe,
		hashes: map[fileKey]string{}, probed: map[string]string{}}
	for _, t := range store.Snapshot().Tools {
		o.probed[t.BinaryHash] = t.VersionString
	}
	return o
}

var _ executor.Observer = (*Observer)(nil)

// Observe records the tool the call ran, the services it left alive and the
// names of the variables it was given.
func (o *Observer) Observe(req executor.ExecRequest, res executor.ExecResult) {
	tool, ok := o.tool(req)
	_ = o.store.Update(func(inv *Inventory) {
		if ok {
			putTool(inv, tool)
		}
		for _, s := range res.Services {
			putService(inv, serviceOf(req, tool, s))
		}
		inv.EnvVarNames = union(inv.EnvVarNames, envNames(req.Env))
	})
}

// tool resolves argv[0] through PATH, hashes it and finds its version once.
func (o *Observer) tool(req executor.ExecRequest) (Tool, bool) {
	return o.toolOn(req.Argv[0], pathOf(req.Env))
}

// toolOn is [Observer.tool] for a name and the PATH it is looked up on.
func (o *Observer) toolOn(name, pathList string) (Tool, bool) {
	path, ok := resolveTool(name, pathList)
	if !ok {
		return Tool{}, false
	}
	hash, ok := o.hash(path)
	if !ok {
		return Tool{}, false
	}
	name = filepath.Base(path)
	return Tool{Name: name, BinaryHash: hash, VersionString: o.version(name, path, hash)}, true
}

func (o *Observer) hash(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	key := fileKey{path, info.ModTime()}
	o.mu.Lock()
	defer o.mu.Unlock()
	if h, ok := o.hashes[key]; ok {
		return h, true
	}
	h, err := hashFile(path)
	if err != nil {
		return "", false
	}
	o.hashes[key] = h
	return h, true
}

// version probes each binary hash at most once; a tool with no probe entry
// keeps an empty version.
func (o *Observer) version(name, path, hash string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if v, done := o.probed[hash]; done {
		return v
	}
	v := ""
	if args, ok := o.probes[name]; ok {
		v = o.probe(path, args)
	}
	o.probed[hash] = v
	return v
}

// hashFile is the identity of a binary's bytes. SHA-256 stands in until the
// store's BLAKE3 lands; only this function changes then.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// resolveTool finds the executable a name means. A relative path with a slash
// is a script inside the cell, not a tool, and is not recorded.
func resolveTool(argv0, pathList string) (string, bool) {
	if filepath.IsAbs(argv0) {
		return argv0, true
	}
	if strings.ContainsRune(argv0, filepath.Separator) {
		return "", false
	}
	for _, dir := range filepath.SplitList(pathList) {
		p := filepath.Join(dir, argv0)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// pathOf is the PATH the call ran with: its own environment, else ours.
func pathOf(env []string) string {
	if env == nil {
		return os.Getenv("PATH")
	}
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			return v
		}
	}
	return ""
}

// envNames keeps the name of each KEY=VALUE pair; values are dropped here.
func envNames(env []string) []string {
	var names []string
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok && name != "" {
			names = append(names, name)
		}
	}
	return names
}

// serviceOf names a service after the executable that started it and takes
// its version from that tool. A data directory that is not cell-relative is
// dropped: it would not travel (L1).
func serviceOf(req executor.ExecRequest, tool Tool, s executor.Service) Service {
	out := Service{Name: nameOf(req, s), Version: tool.VersionString, Ports: s.Ports}
	if dir := filepath.ToSlash(filepath.Clean(s.DataDir)); s.DataDir != "" && relative(dir) {
		out.DataDir = dir
	}
	return out
}

// nameOf prefers the name the executor read from the surviving process.
func nameOf(req executor.ExecRequest, s executor.Service) string {
	if s.Name != "" {
		return s.Name
	}
	return filepath.Base(req.Argv[0])
}

func relative(p string) bool {
	return !filepath.IsAbs(p) && !strings.HasPrefix(p, "/") && p != ".." && !strings.HasPrefix(p, "../")
}

func putTool(inv *Inventory, t Tool) {
	for i := range inv.Tools {
		if inv.Tools[i].Name == t.Name {
			inv.Tools[i] = t
			return
		}
	}
	inv.Tools = append(inv.Tools, t)
	sort.Slice(inv.Tools, func(i, j int) bool { return inv.Tools[i].Name < inv.Tools[j].Name })
}

func putService(inv *Inventory, s Service) {
	for i := range inv.Services {
		if inv.Services[i].Name == s.Name {
			inv.Services[i] = mergeService(inv.Services[i], s)
			return
		}
	}
	inv.Services = append(inv.Services, mergeService(Service{}, s))
	sort.Slice(inv.Services, func(i, j int) bool { return inv.Services[i].Name < inv.Services[j].Name })
}

// mergeService lets a later sighting add ports and facts but never erase them.
func mergeService(old, next Service) Service {
	next.Ports = unionInts(old.Ports, next.Ports)
	if next.Version == "" {
		next.Version = old.Version
	}
	if next.DataDir == "" {
		next.DataDir = old.DataDir
	}
	return next
}

func union(a, b []string) []string {
	set := map[string]bool{}
	for _, s := range append(append([]string{}, a...), b...) {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func unionInts(a, b []int) []int {
	set := map[int]bool{}
	for _, n := range append(append([]int{}, a...), b...) {
		set[n] = true
	}
	out := make([]int, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// Sight records the tool a name resolves to on pathList, as if a call had just
// run it. The harness uses it to look at what a setup turn installed: what the
// inventory learns is what is really there, never what the agent said.
func (o *Observer) Sight(name, pathList string) {
	if tool, ok := o.toolOn(name, pathList); ok {
		_ = o.store.Update(func(inv *Inventory) { putTool(inv, tool) })
	}
}

// Resolve finds the executable a name means on pathList.
func Resolve(name, pathList string) (string, bool) { return resolveTool(name, pathList) }
