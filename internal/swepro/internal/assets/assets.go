// Package assets embeds the prompt and tool-description text imported by
// swe-pro/src/agent/agent.ts:9-14, src/session/{prompt,system}.ts:5-23,
// src/tool/**/*.ts, and src/review/tool/*.ts at commit 3b25a1a.
package assets

import (
	_ "embed"

	"github.com/Agent-Field/aforge-v2/internal/packed"
)

// aforge-embed: D12 — the assets ship packed rather than raw. The src/ folder
// is still the source of truth and the names below are unchanged — a packed
// folder keeps the names go:embed gave it — so a caller's resolved import path
// resolves exactly as before. See internal/packed.
//
//go:generate go run github.com/Agent-Field/aforge-v2/internal/packed/cmd/pack -o src.pack.gz src
//go:embed src.pack.gz
var srcArchive []byte

// files mirrors paths below the TypeScript repository's root, so callers use
// the stable resolved import path (for example "src/session/prompt/gpt.txt").
var files = packed.New(srcArchive)

// Get returns the byte-identical embedded text for a resolved TypeScript
// import path. It returns false for paths outside the embedded asset set.
func Get(tsImportPath string) (string, bool) {
	data, err := files.ReadFile(tsImportPath)
	if err != nil {
		return "", false
	}
	return string(data), true
}
