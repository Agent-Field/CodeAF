package cellstore

import (
	"sync"

	"github.com/Agent-Field/codeaf/internal/cell"
)

var publication = struct {
	sync.Mutex
	roots map[string]map[*publicationRule]bool
}{roots: map[string]map[*publicationRule]bool{}}

type publicationRule struct{ owes func(int64) bool }

// RequirePublication keeps recovery notices behind the chat's publish boundary,
// including startup turns sealed before the drive had its first fence.
func RequirePublication(c cell.Cell, owes ...func(int64) bool) func() {
	rule := &publicationRule{}
	if len(owes) > 0 {
		rule.owes = owes[0]
	}
	publication.Lock()
	if publication.roots[c.Root] == nil {
		publication.roots[c.Root] = map[*publicationRule]bool{}
	}
	publication.roots[c.Root][rule] = true
	publication.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			publication.Lock()
			defer publication.Unlock()
			delete(publication.roots[c.Root], rule)
			if len(publication.roots[c.Root]) == 0 {
				delete(publication.roots, c.Root)
			}
		})
	}
}

func requiresPublication(c cell.Cell, end int64) bool {
	publication.Lock()
	rules := make([]*publicationRule, 0, len(publication.roots[c.Root]))
	for rule := range publication.roots[c.Root] {
		rules = append(rules, rule)
	}
	publication.Unlock()
	for _, rule := range rules {
		if rule.owes == nil || rule.owes(end) {
			return true
		}
	}
	return false
}
