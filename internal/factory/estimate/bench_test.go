package estimate_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/estimate"
)

func BenchmarkOpenAndForAFullFloor(b *testing.B) {
	var items []factory.Item
	for i := range 1000 {
		items = append(items, finished(i+1, []string{"a/a", "a/b", "a/c"}[i%3], rubric(i%3, i%2, (i/3)%3, 0, i%2), float64(i%7)+0.1, t0.Add(time.Duration(i)*time.Hour), 10+i%50))
	}
	for i := range 800 {
		it := open("a/a", rubric(i%3, 0, 1, i%2, 0))
		it.ID = 5000 + i
		items = append(items, it)
	}
	b.ResetTimer()
	for range b.N {
		book := estimate.Open(items)
		for _, it := range items[1000:] {
			book.For(it)
		}
	}
}
