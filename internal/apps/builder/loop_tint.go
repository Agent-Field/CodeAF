package builder

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// chooseTint gives the build the first palette tint no other app uses, or the
// least used one when all are taken, so a new app's frame differs from its
// neighbours'. The worker may still set its own with `codeaf app tint`.
func (s *Service) chooseTint(b *build) {
	if b.tintChoice() != "" {
		return
	}
	used := s.tintsInUse(b.app)
	b.setTint(leastUsed(spec.Tints, used), usedNames(used))
}

// tintsInUse counts the tints the other apps carry in their app.json; an app
// with no tint counts for nothing.
func (s *Service) tintsInUse(self string) map[string]int {
	counts := map[string]int{}
	apps, err := s.all.Store.List(s.ctx, svc.Caller{Member: svc.Owner})
	if err != nil {
		return counts
	}
	for _, a := range apps {
		if a.ID == self {
			continue
		}
		if detail, err := s.all.Store.Get(s.ctx, a.ID); err == nil {
			if tint := tintOf(detail.Files[spec.AppFile]); tint != "" {
				counts[tint]++
			}
		}
	}
	return counts
}

func tintOf(appJSON string) string {
	var app struct {
		Tint string `json:"tint"`
	}
	_ = json.Unmarshal([]byte(appJSON), &app)
	return app.Tint
}

// leastUsed is the first palette entry with the lowest count.
func leastUsed(palette []string, counts map[string]int) string {
	best := ""
	for _, tint := range palette {
		if best == "" || counts[tint] < counts[best] {
			best = tint
		}
	}
	return best
}

func usedNames(counts map[string]int) []string {
	names := make([]string, 0, len(counts))
	for tint := range counts {
		names = append(names, tint)
	}
	sort.Strings(names)
	return names
}

// tintLine is the plan task's brief line about the tint.
func tintLine(tint string, used []string) string {
	others := "none"
	if len(used) > 0 {
		others = strings.Join(used, ", ")
	}
	return "Tint hint: " + tint + " (other apps use: " + others + ")"
}

func (b *build) tintChoice() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tint
}

func (b *build) setTint(tint string, used []string) {
	b.mu.Lock()
	b.tint, b.tintUsed = tint, used
	b.mu.Unlock()
}

func (b *build) tintUsedBy() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tintUsed
}
