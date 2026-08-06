package main

import (
	"os"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const groundRecallLimit = 5

// openDefaultHistory is read-optional: a one-shot command with no resident
// store neither creates one nor changes a prompt. That keeps the benchmarked
// headless path byte-identical until folded history actually exists.
func openDefaultHistory() *store.Store {
	path := defaultChatDB()
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return nil
	}
	history, err := store.Open(path)
	if err != nil {
		return nil
	}
	hasFolds, err := history.HasFolds()
	if err != nil || !hasFolds {
		_ = history.Close()
		return nil
	}
	return history
}

func recallHits(history *store.Store, terms string, limit int) []store.RecallHit {
	if history == nil {
		return nil
	}
	hits, err := history.Recall(terms, resident.ExtractCues(terms), limit)
	if err != nil {
		return nil
	}
	return hits
}
