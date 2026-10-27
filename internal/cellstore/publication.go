package cellstore

import (
	"sync"

	"github.com/Agent-Field/codeaf/internal/cell"
)

var publication = struct {
	sync.Mutex
	roots map[string]int
}{roots: map[string]int{}}

// RequirePublication keeps recovery notices behind the chat's publish boundary,
// including startup turns sealed before the drive had its first fence.
func RequirePublication(c cell.Cell) func() {
	publication.Lock()
	publication.roots[c.Root]++
	publication.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			publication.Lock()
			defer publication.Unlock()
			publication.roots[c.Root]--
			if publication.roots[c.Root] == 0 {
				delete(publication.roots, c.Root)
			}
		})
	}
}

func requiresPublication(c cell.Cell) bool {
	publication.Lock()
	defer publication.Unlock()
	return publication.roots[c.Root] > 0
}
