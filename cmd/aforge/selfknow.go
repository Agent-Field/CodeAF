package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/profile"
)

const selfKnowledgeTTL = 5 * time.Minute

type cachedSelfKnowledge struct {
	profileDir string
	model      string
	text       string
	expires    time.Time
}

type selfKnowledgeBucket struct {
	tokens   []int
	turns    []int
	failures int
}

var (
	selfKnowledgeMu     sync.Mutex
	selfKnowledgeCached cachedSelfKnowledge
)

func selfKnowledge(settings config.Config, model string) string {
	selfKnowledgeMu.Lock()
	defer selfKnowledgeMu.Unlock()

	now := time.Now()
	if selfKnowledgeCached.profileDir == settings.ProfileDir &&
		selfKnowledgeCached.model == model &&
		now.Before(selfKnowledgeCached.expires) {
		return selfKnowledgeCached.text
	}

	text := measureSelfKnowledge(settings, model)
	selfKnowledgeCached = cachedSelfKnowledge{
		profileDir: settings.ProfileDir,
		model:      model,
		text:       text,
		expires:    now.Add(selfKnowledgeTTL),
	}
	return text
}

func measureSelfKnowledge(settings config.Config, model string) string {
	measured, err := profile.Load(settings.ProfileDir, model, "linear")
	if err != nil || len(measured.Records) < 5 {
		return ""
	}

	buckets := make(map[string]selfKnowledgeBucket)
	for _, record := range measured.Records {
		switch record.Size {
		case profile.BucketDirect, "atomic", "borderline", "oversized", "synthesis":
		default:
			continue
		}
		bucket := buckets[record.Size]
		bucket.tokens = append(bucket.tokens, record.Tokens)
		bucket.turns = append(bucket.turns, record.Turns)
		positive, graded := record.Verdict.Graded()
		if graded && !positive {
			bucket.failures++
		}
		buckets[record.Size] = bucket
	}

	lines := make([]string, 0, len(buckets))
	for _, size := range []string{profile.BucketDirect, "atomic", "borderline", "oversized", "synthesis"} {
		bucket, ok := buckets[size]
		if !ok {
			continue
		}
		failureShare := 100 * float64(bucket.failures) / float64(len(bucket.tokens))
		lines = append(lines, fmt.Sprintf("%s: median %d tokens, %d turns; n=%d; failures=%.1f%%",
			size, selfKnowledgeMedian(bucket.tokens), selfKnowledgeMedian(bucket.turns), len(bucket.tokens), failureShare))
	}
	return strings.Join(lines, "\n")
}

func selfKnowledgeMedian(values []int) int {
	sort.Ints(values)
	return values[len(values)/2]
}
