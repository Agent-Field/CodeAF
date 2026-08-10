package main

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// runVersion is deliberately provider-free. Installers and harness doctors
// need to identify the binary before an API key or a workspace exists.
func runVersion() error {
	fmt.Println(versionString())
	return nil
}

func versionString() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "aforge dev"
	}
	version := strings.TrimSpace(info.Main.Version)
	if version == "" || version == "(devel)" || version == "devel" {
		return "aforge dev"
	}
	return "aforge " + version
}
