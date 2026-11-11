package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"
)

// SavedSpend reads meta.json's saved figure alone, without loading journal or
// workspace overlays. Missing or malformed metadata answers zero as LoadMeta
// does; only other I/O failures are errors.
func SavedSpend(dir string) (float64, error) {
	raw, err := os.ReadFile((Place{Dir: dir}).MetaPath())
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var saved struct {
		ID       string  `json:"id"`
		SpentUSD float64 `json:"spentUsd"`
	}
	if json.Unmarshal(raw, &saved) != nil || strings.TrimSpace(saved.ID) == "" {
		return 0, nil
	}
	return saved.SpentUSD, nil
}

// SpendSaved is the conversation's cumulative spend just written to meta.json.
// Dir is its Place directory, so a reader can find the conversation it holds
// without opening a journal. This is a saved figure, not the live meter.
type SpendSaved struct {
	Dir      string
	SpentUSD float64
}

var spendWatchers struct {
	sync.Mutex
	next int
	of   map[int]func(SpendSaved)
}

// OnSpendSaved calls watch after a spend snapshot reaches meta.json. It runs
// on the stamping goroutine, outside the agent and metadata locks; watch must
// not block or stamp spend again. The answer stops the watching.
func OnSpendSaved(watch func(SpendSaved)) (stop func()) {
	spendWatchers.Lock()
	defer spendWatchers.Unlock()
	if spendWatchers.of == nil {
		spendWatchers.of = map[int]func(SpendSaved){}
	}
	id := spendWatchers.next
	spendWatchers.next++
	spendWatchers.of[id] = watch
	return func() {
		spendWatchers.Lock()
		defer spendWatchers.Unlock()
		delete(spendWatchers.of, id)
	}
}

func announceSpendSaved(saved SpendSaved) {
	spendWatchers.Lock()
	readers := make([]func(SpendSaved), 0, len(spendWatchers.of))
	for _, watch := range spendWatchers.of {
		readers = append(readers, watch)
	}
	spendWatchers.Unlock()
	for _, watch := range readers {
		watch(saved)
	}
}
