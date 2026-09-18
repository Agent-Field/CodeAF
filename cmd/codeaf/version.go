package main

import (
	"fmt"
	"runtime"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
)

// runVersion answers the one question an installer, a doctor, or a packaging
// script asks before it trusts the binary on the PATH: is codeaf here, and
// which one is it. It reads nothing, writes nothing, and needs no API key —
// probing for the binary must never be a configuration problem.
//
// AND IT ANSWERS THE SECOND QUESTION TOO, which is the one a person filing a
// defect is actually asked: WHICH BUILD, ON WHAT. `codeaf dev` names nothing —
// not the commit, not the day, not the machine — and a bug report carrying it
// costs somebody a round trip before the investigation can start. `make build`
// stamps the revision and the moment ([buildinfo]); the toolchain and the
// platform come from the runtime and are always there.
func runVersion() error {
	fmt.Println(versionString())
	return nil
}

// versionString is ONE LINE, and stays one line: an external harness reads this
// output to decide whether codeaf is installed at all, and a second line would
// be a second thing for it to get wrong.
func versionString() string {
	return "codeaf " + resolvedVersion() + " · " + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH
}

// resolvedVersion has one source with presence and session metadata, so the
// command can never name a different build from the one those files name.
//
// A BINARY THAT CANNOT NAME ITS SOURCE SAYS SO, and what it says has to be the
// real reason. A bare `go build` is NOT that reason: inside a git checkout the
// toolchain stamps `vcs.revision` itself and [buildinfo] falls back to it, so a
// plain build prints a full pseudo-version — measured on go1.26.5,
// `codeaf v0.2.2-0.20260918035413-2ab365d6cb3e`. What leaves a binary with
// nothing to name is having no VCS to read: a build from a tree that is not a
// git checkout, an archive, a vendored copy, or `-buildvcs=false`. The bare word
// `dev` would read like a release name, so the absence is written out — and it
// names the condition rather than a Makefile target that is not required.
func resolvedVersion() string {
	stamped := buildinfo.String()
	if buildinfo.Revision() == "" {
		return stamped + " (no revision stamped — built outside a git checkout)"
	}
	return stamped
}
