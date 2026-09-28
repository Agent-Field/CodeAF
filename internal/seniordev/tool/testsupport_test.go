//go:build !windows

package tool

import (
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

// Shared helpers for the tests in this package.

func definitionNames(definitions []steploop.ToolDefinition) []string {
	out := make([]string, 0, len(definitions))
	for _, item := range definitions {
		out = append(out, item.Provider.Name)
	}
	return out
}
func containsName(names []string, name string) bool {
	for _, item := range names {
		if item == name {
			return true
		}
	}
	return false
}
