package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Labels read only the requested identities. A chart needing four labels must
// not enumerate/parse all conversation journals or retain their Agent views.
const labelBucketLimit = 512
const labelReadBudget = 200 * time.Millisecond
const labelMetaBytes = 128 << 10

func (b *appBackend) AgentLabels(ids []string) (appwire.AgentLabels, error) {
	if err := appwire.CheckAgentLabelIDs(ids); err != nil {
		return appwire.AgentLabels{}, err
	}
	known := make(map[string]string, len(ids))
	var missing []string
	b.locked(func() {
		for _, id := range ids {
			if a := b.agents[id]; a != nil && strings.TrimSpace(a.view.Title) != "" && len(a.view.Title) <= 4096 {
				known[id] = strings.Clone(a.view.Title)
			}
		}
	})
	for _, id := range ids {
		if _, ok := known[id]; !ok {
			missing = append(missing, id)
		}
	}
	found, partial := readAgentLabelBatch(session.PlacesRoot(), missing, time.Now().Add(labelReadBudget), labelBucketLimit, loadAgentLabelMeta)
	for id, title := range found {
		known[id] = title
	}
	out := appwire.AgentLabels{Labels: []appwire.AgentLabel{}, Incomplete: partial}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if title, ok := known[id]; ok && !seen[id] {
			out.Labels = append(out.Labels, appwire.AgentLabel{ID: id, Title: title})
			seen[id] = true
		}
	}
	return out, nil
}

// The sole directory stream contains project buckets, never session folders.
// Each bucket probes only wanted ids. Bounds stop with incomplete, not false absence.
func readAgentLabelBatch(root string, ids []string, deadline time.Time, bucketLimit int, load func(string, string) (string, bool)) (map[string]string, bool) {
	found := make(map[string]string)
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	if len(wanted) == 0 {
		return found, false
	}
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return found, true
	}
	dir, err := os.Open(root)
	if err != nil {
		return found, !os.IsNotExist(err)
	}
	defer dir.Close()
	visited := 0
	for len(wanted) > 0 {
		if visited >= bucketLimit || (!deadline.IsZero() && !time.Now().Before(deadline)) {
			return found, true
		}
		entries, readErr := dir.ReadDir(min(64, bucketLimit-visited))
		for _, entry := range entries {
			visited++
			if !entry.IsDir() {
				continue
			}
			for id := range wanted {
				if !deadline.IsZero() && !time.Now().Before(deadline) {
					return found, true
				}
				if title, ok := load(filepath.Join(root, entry.Name(), id, "meta.json"), id); ok {
					delete(wanted, id)
					if title != "" {
						found[id] = title
					}
				}
			}
		}
		if readErr == io.EOF {
			return found, false
		}
		if readErr != nil {
			return found, true
		}
	}
	return found, false
}

func loadAgentLabelMeta(path, id string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > labelMetaBytes {
		return "", false
	}
	raw, err := io.ReadAll(io.LimitReader(file, labelMetaBytes+1))
	if err != nil || len(raw) > labelMetaBytes {
		return "", false
	}
	var meta struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if json.Unmarshal(raw, &meta) != nil || meta.ID != id || len(meta.Title) > 4096 {
		return "", false
	}
	return strings.TrimSpace(meta.Title), true
}
