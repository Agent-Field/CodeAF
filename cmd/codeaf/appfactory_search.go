package main

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// appSearchJobsWait bounds all factories together because search runs on keystrokes.
const appSearchJobsWait = 1500 * time.Millisecond

// appFactoryPassiveRead keeps a search read from resuming a local factory.
type appFactoryPassiveRead struct{}

type appJobMatch struct {
	hit  appwire.Hit
	rank int
	born time.Time
}

// SearchJobs reads each connected factory's own caller-filtered queue in parallel.
func (b *appBackend) SearchJobs(query string, perGroup int) []appwire.Hit {
	if perGroup <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), appFactoryPassiveRead{}, true), appSearchJobsWait)
	defer cancel()
	h := b.factoryHub()
	rows, err := h.snapshot()
	if err != nil {
		return nil
	}
	words := strings.Fields(strings.ToLower(query))
	now := h.now()
	results := make(chan []appJobMatch, len(rows))
	pending := 0
	for _, row := range rows {
		// The hub forgets the local service's credential when it stops. A saved
		// connection or resume choice alone must never start it for a search.
		if row.Local && h.tokenFor(row.ID) == "" {
			continue
		}
		pending++
		guard.Go("app/search jobs", func() {
			q, err := b.FactoryQueue(ctx, row.ID)
			var matches []appJobMatch
			if err == nil {
				for _, job := range q.Jobs {
					if match, ok := appSearchJob(row.ID, job, words, now); ok {
						matches = append(matches, match)
					}
				}
			}
			results <- matches
		})
	}
	var matches []appJobMatch
	for pending > 0 {
		select {
		case found := <-results:
			matches = append(matches, found...)
			pending--
		case <-ctx.Done():
			pending = 0
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if !a.born.Equal(b.born) {
			return a.born.After(b.born)
		}
		return a.hit.ID < b.hit.ID
	})
	if len(matches) > perGroup {
		matches = matches[:perGroup]
	}
	hits := make([]appwire.Hit, 0, len(matches))
	for _, m := range matches {
		hits = append(hits, m.hit)
	}
	return hits
}

func appSearchJob(connection string, job api.Job, words []string, now time.Time) (appJobMatch, bool) {
	if job.Dismissed || job.State == api.StateDropped || strings.TrimSpace(job.DoneHow) == "dropped" {
		return appJobMatch{}, false
	}
	// The default jobs list keeps closed jobs only for the week they closed.
	if job.State == api.StateDone && (job.DoneAt.IsZero() || now.Sub(job.DoneAt) >= 7*24*time.Hour || job.DoneAt.After(now.Add(time.Minute))) {
		return appJobMatch{}, false
	}
	rank := appJobSearchRank(job, words)
	if rank < 0 {
		return appJobMatch{}, false
	}
	return appJobMatch{hit: appwire.Hit{Kind: appwire.HitJob, ID: connection + "#" + strconv.Itoa(job.ID), Factory: connection, DocumentID: job.ID, Repo: job.Repo, Number: job.Num, Title: job.Title, State: job.State}, rank: rank, born: job.CreatedAt}, true
}

func appJobSearchRank(job api.Job, words []string) int {
	title, repo := strings.ToLower(job.Title), strings.ToLower(job.Repo)
	detail := ""
	if job.Ask != nil {
		detail = strings.ToLower(job.Ask.Text)
	}
	rank := 0
	for _, word := range words {
		switch {
		case strings.Contains(title, word):
		case appJobNumberMatches(job.Num, repo, word):
		case detail != "" && strings.Contains(detail, word):
			rank = max(rank, 1)
		case strings.Contains(repo, word):
			rank = max(rank, 2)
		default:
			return -1
		}
	}
	if len(words) == 0 {
		return -1
	}
	return rank
}

func appJobNumberMatches(number int, repo, word string) bool {
	if number <= 0 {
		return false
	}
	n := strconv.Itoa(number)
	_, short, _ := strings.Cut(repo, "/")
	return word == n || word == "#"+n || word == repo+"#"+n || (short != "" && word == short+"#"+n)
}
