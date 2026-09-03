package main

import (
	"fmt"
	"runtime"

	"github.com/Agent-Field/aforge-v2/internal/buildinfo"
)

// runVersion answers the one question an installer, a doctor, or a packaging
// script asks before it trusts the binary on the PATH: is aforge here, and
// which one is it. It reads nothing, writes nothing, and needs no API key —
// probing for the binary must never be a configuration problem.
//
// AND IT ANSWERS THE SECOND QUESTION TOO, which is the one a person filing a
// defect is actually asked: WHICH BUILD, ON WHAT. `aforge dev` names nothing —
// not the commit, not the day, not the machine — and a bug report carrying it
// costs somebody a round trip before the investigation can start. `make build`
// stamps the revision and the moment ([buildinfo]); the toolchain and the
// platform come from the runtime and are always there.
func runVersion() error {
	fmt.Println(versionString())
	return nil
}

// versionString is ONE LINE, and stays one line: an external harness reads this
// output to decide whether aforge is installed at all, and a second line would
// be a second thing for it to get wrong.
func versionString() string {
	return "aforge " + resolvedVersion() + " · " + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH
}

// resolvedVersion has one source with presence and session metadata, so the
// command can never name a different build from the one those files name.
//
// A BINARY THAT CANNOT NAME ITS SOURCE SAYS SO. Built with a bare `go build`
// rather than `make build` there is no revision stamped and nothing to fall
// back on, and the bare word `dev` reads like a release name — so it is written
// out as the absence it is, and the sentence names the target that fixes it.
func resolvedVersion() string {
	stamped := buildinfo.String()
	if buildinfo.Revision() == "" {
		return stamped + " (no revision stamped — built without `make build`)"
	}
	return stamped
}
