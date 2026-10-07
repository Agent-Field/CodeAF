package tui3

import "runtime"

var (
	runtimeGOOS   = runtime.GOOS
	runtimeGOARCH = runtime.GOARCH
)

// runtimePlatform names the build a release download would fetch, as the
// installer spells it (`linux/amd64`). It is one function because the offer's
// line and its note must spell the platform identically.
func runtimePlatform() string {
	return runtimeGOOS + "/" + runtimeGOARCH
}
