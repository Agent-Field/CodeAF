package store

import "github.com/Agent-Field/codeaf/internal/apps/custom"

// customDefinitionPath keeps draft writes and version reads on the same path
// rule as the compiler without changing the wave-1 definition formats.
func customDefinitionPath(name string) bool { return custom.IsFile(name) }
