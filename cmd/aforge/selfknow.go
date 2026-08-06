package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

const selfKnowledgeTTL = 5 * time.Minute

type cachedSelfKnowledge struct {
	profileDir string
	model      string
	text       string
	expires    time.Time
}

type selfKnowledgeBucket struct {
	tokens     []int
	turns      []int
	failures   int
	successes  int
	promotions int
	cost       float64
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
		case profile.BucketReflex, profile.BucketDirect, "atomic", "borderline", "oversized", "synthesis":
		default:
			continue
		}
		bucket := buckets[record.Size]
		bucket.tokens = append(bucket.tokens, record.Tokens)
		bucket.turns = append(bucket.turns, record.Turns)
		bucket.cost += record.Cost
		if record.Size == profile.BucketReflex {
			if record.Promoted {
				bucket.promotions++
			} else if record.Verdict == provider.VerdictVerifiedSuccess ||
				record.Verdict == provider.VerdictUnverifiedSuccess {
				bucket.successes++
			}
		}
		positive, graded := record.Verdict.Graded()
		if graded && !positive {
			bucket.failures++
		}
		buckets[record.Size] = bucket
	}

	lines := make([]string, 0, len(buckets))
	for _, size := range []string{profile.BucketReflex, profile.BucketDirect, "atomic", "borderline", "oversized", "synthesis"} {
		bucket, ok := buckets[size]
		if !ok {
			continue
		}
		if size == profile.BucketReflex {
			samples := float64(len(bucket.tokens))
			lines = append(lines, fmt.Sprintf("%s: median %d tokens, %d turns; n=%d; success=%.1f%%; promoted=%.1f%%; avg cost=$%.4f",
				size, selfKnowledgeMedian(bucket.tokens), selfKnowledgeMedian(bucket.turns), len(bucket.tokens),
				100*float64(bucket.successes)/samples, 100*float64(bucket.promotions)/samples, bucket.cost/samples))
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
