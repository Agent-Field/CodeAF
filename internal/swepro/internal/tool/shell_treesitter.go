//go:build treesitter

// This opt-in dependency marker pins the native Bash grammar used by the
// shell permission scanner. Production builds that provide the module cache
// use the "treesitter" tag when grammar-backed parse diagnostics are desired;
// the deterministic scanner in shell_port.go remains the public projection.
package tool

import _ "github.com/smacker/go-tree-sitter/bash"
