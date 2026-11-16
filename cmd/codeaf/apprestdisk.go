package main

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	appRestDiskTotalBytes = 96 << 20
	appRestDiskEntries    = 256
	appRestDiskScanEvery  = time.Minute
)

// The inventory is rebuilt at first use and once a minute by the idle sweep.
// Writes update it directly, so enforcing the bounds never scans per request.
var appRestDisk appRestDiskCache

type appRestDiskEntry struct {
	name    string
	size    int64
	written time.Time
}

type appRestDiskCache struct {
	mu       sync.Mutex
	root     string
	nextScan time.Time
	entries  map[string]appRestDiskEntry
	bytes    int64
}

func (c *appRestDiskCache) maintain(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked(now)
	c.trimLocked()
}

func (c *appRestDiskCache) loadLocked(now time.Time) {
	root := filepath.Dir(appRestPath("entry"))
	if c.root == root && now.Before(c.nextScan) {
		return
	}
	files, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	c.root, c.nextScan = root, now.Add(appRestDiskScanEvery)
	c.entries, c.bytes = map[string]appRestDiskEntry{}, 0
	agents, known := appRestAgentFolders()
	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			continue
		}
		agent := strings.TrimSuffix(file.Name(), ".json")
		if !info.Mode().IsRegular() || strings.HasSuffix(file.Name(), ".json") && known && !agents[agent] {
			_ = os.Remove(filepath.Join(root, file.Name()))
			continue
		}
		c.rememberLocked(file.Name(), info)
	}
}

// Only folder names and deletion markers are needed, never journal or task
// contents. A failed listing cannot establish that an agent was deleted.
func appRestAgentFolders() (map[string]bool, bool) {
	projects, err := os.ReadDir(session.PlacesRoot())
	if os.IsNotExist(err) {
		return nil, true
	}
	if err != nil {
		return nil, false
	}
	agents := map[string]bool{}
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		root := filepath.Join(session.PlacesRoot(), project.Name())
		folders, err := os.ReadDir(root)
		if err != nil {
			return nil, false
		}
		for _, folder := range folders {
			if folder.IsDir() && appCloseConversationExists(filepath.Join(root, folder.Name())) {
				agents[folder.Name()] = true
			}
		}
	}
	return agents, true
}

func (c *appRestDiskCache) rememberLocked(name string, info os.FileInfo) {
	c.bytes -= c.entries[name].size
	c.entries[name] = appRestDiskEntry{name: name, size: info.Size(), written: info.ModTime()}
	c.bytes += info.Size()
}

func (c *appRestDiskCache) trimLocked() {
	if c.bytes <= appRestDiskTotalBytes && len(c.entries) <= appRestDiskEntries {
		return
	}
	entries := make([]appRestDiskEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b appRestDiskEntry) int {
		if order := a.written.Compare(b.written); order != 0 {
			return order
		}
		return cmp.Compare(a.name, b.name)
	})
	for _, entry := range entries {
		if c.bytes <= appRestDiskTotalBytes && len(c.entries) <= appRestDiskEntries {
			break
		}
		c.dropLocked(entry.name)
	}
}

func (c *appRestDiskCache) dropLocked(name string) {
	if err := os.Remove(filepath.Join(c.root, name)); err != nil && !os.IsNotExist(err) {
		return
	}
	c.bytes -= c.entries[name].size
	delete(c.entries, name)
}

func (c *appRestDiskCache) forget(agent string) {
	path := appRestPath(agent)
	if path == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked(time.Now())
	if c.root != filepath.Dir(path) {
		_ = os.Remove(path)
		return
	}
	c.dropLocked(filepath.Base(path))
}
