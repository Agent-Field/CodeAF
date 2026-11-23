package main

import (
	"os"
	"runtime"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/env"
)

// Matches ui2_core::engine::LOCAL_GUI_ENV. This non-secret arrangement marker
// is set only for a trusted direct bundled launch; the desktop also guards its
// copy commands with its own provenance. A stdio pipe alone proves no locality.
const appLocalGUIEnv = "CODEAF_APP_LOCAL_GUI"

func appLocalGUI(raw string, parent int, platform string) bool {
	return parent > 0 && (platform == "darwin" || platform == "linux") && raw == "v1:"+strconv.Itoa(parent)
}

// Consume before any tool can inherit the marker. No legacy alias is accepted.
func consumeAppLocalGUI() bool {
	raw := env.ConsumeExact(appLocalGUIEnv)
	return appLocalGUI(raw, os.Getppid(), runtime.GOOS)
}
