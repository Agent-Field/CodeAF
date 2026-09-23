package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/probe"
)

// actObservation is the act data payload when the act carries an observation.
type actObservation struct {
	probe.ActData
	Observation probe.ObserveData `json:"observation"`
}

// waitData is the data payload of wait: truthful settled/timeout reason.
type waitData struct {
	Settled  bool   `json:"settled"`
	Reason   string `json:"reason"`
	Revision int    `json:"revision"`
}

func obsPath(m *probe.Manager, id string) string {
	return filepath.Join(m.Root, "obs", id+".last")
}

func loadPrev(m *probe.Manager, id string) string {
	b, err := os.ReadFile(obsPath(m, id))
	if err != nil {
		return ""
	}
	return string(b)
}

func savePrev(m *probe.Manager, id, snap string) {
	_ = os.MkdirAll(filepath.Join(m.Root, "obs"), 0o700)
	_ = os.WriteFile(obsPath(m, id), []byte(snap), 0o600)
}

func clearPrev(m *probe.Manager, id string) { _ = os.Remove(obsPath(m, id)) }

// buildIdentity computes the immutable build identity of an existing binary.
func buildIdentity(binPath, source string) (probe.BuildIdentity, error) {
	var b probe.BuildIdentity
	abs, err := filepath.Abs(binPath)
	if err != nil {
		return b, err
	}
	b.Binary = abs
	f, err := os.Open(abs)
	if err != nil {
		return b, fmt.Errorf("open binary: %v", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return b, err
	}
	b.SHA = hex.EncodeToString(h.Sum(nil))
	if out, err := exec.Command("go", "version").Output(); err == nil {
		b.GoVersion = strings.TrimSpace(string(out))
	}
	if source != "" {
		b.Dirty = gitDirty(source)
	}
	return b, nil
}

func gitDirty(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return false // not a git checkout: identity still covers the sha
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// buildFromSource compiles the source dir into the probe root and returns its identity.
func buildFromSource(m *probe.Manager, source string) (probe.BuildIdentity, error) {
	src, err := filepath.Abs(source)
	if err != nil {
		return probe.BuildIdentity{}, err
	}
	out := filepath.Join(m.Root, "bin", "codeaf")
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return probe.BuildIdentity{}, err
	}
	if o, err := exec.Command("go", "build", "-o", out, src).CombinedOutput(); err != nil {
		return probe.BuildIdentity{}, fmt.Errorf("go build: %s", strings.TrimSpace(string(o)))
	}
	return buildIdentity(out, src)
}

// parseDims parses WxH; empty means the Manager default.
func parseDims(s string) (int, int, error) {
	if s == "" {
		return 0, 0, nil
	}
	i := strings.IndexByte(s, 'x')
	if i <= 0 || i == len(s)-1 {
		return 0, 0, fmt.Errorf("resize must be WxH")
	}
	w, err1 := strconv.Atoi(s[:i])
	hh, err2 := strconv.Atoi(s[i+1:])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("resize must be WxH of positive ints")
	}
	return w, hh, nil
}

// diffSnapshots is a line-oriented +/- diff between two snapshots.
func diffSnapshots(old, cur string) string {
	var b strings.Builder
	oldL, curL := splitLines(old), splitLines(cur)
	oi, ci := 0, 0
	for oi < len(oldL) || ci < len(curL) {
		switch {
		case oi < len(oldL) && ci < len(curL) && oldL[oi] == curL[ci]:
			oi++
			ci++
		case oi < len(oldL) && !hasLine(curL, oldL[oi], ci):
			fmt.Fprintf(&b, "-%s\n", oldL[oi])
			oi++
		default:
			fmt.Fprintf(&b, "+%s\n", curL[ci])
			ci++
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func hasLine(lines []string, s string, from int) bool {
	for i := from; i < len(lines) && i < from+40; i++ {
		if lines[i] == s {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
