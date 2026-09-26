//go:build !windows

package tool

import _ "embed"

//go:embed apply_patch.txt
var applyPatchDescription string
