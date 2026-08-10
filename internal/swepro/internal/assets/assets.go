// Package assets embeds the prompt and tool-description text imported by
// swe-pro/src/agent/agent.ts:9-14, src/session/{prompt,system}.ts:5-23,
// src/tool/**/*.ts, and src/review/tool/*.ts at commit 3b25a1a.
package assets

import "embed"

// files mirrors paths below the TypeScript repository's root, so callers use
// the stable resolved import path (for example "src/session/prompt/gpt.txt").
//
//go:embed src
var files embed.FS

// Get returns the byte-identical embedded text for a resolved TypeScript
// import path. It returns false for paths outside the embedded asset set.
func Get(tsImportPath string) (string, bool) {
	data, err := files.ReadFile(tsImportPath)
	if err != nil {
		return "", false
	}
	return string(data), true
}
