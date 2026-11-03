package serve

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ledgerJobNames uses the whole loaded floor, before visibility or queue filters.
// A missing origin cannot make a copy's reference identify its own dollars.
func ledgerJobNames(items []factory.Item) map[int]bool {
	counts := make(map[string]int, len(items))
	for _, it := range items {
		counts[factory.LedgerName(it)]++
	}
	unique := make(map[int]bool, len(items))
	for _, it := range items {
		if it.FromJob != 0 || counts[factory.LedgerName(it)] != 1 {
			continue
		}
		if source, err := url.Parse(it.SourceIssue); err == nil && it.SourceIssue != "" && it.Num > 0 {
			origin := strings.Trim(source.Path, "/")
			want := factory.RepoOf(it) + "/issues/" + strconv.Itoa(it.Num)
			if strings.EqualFold(origin, want) {
				continue
			}
		}
		unique[it.ID] = true
	}
	return unique
}
