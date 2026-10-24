package main

import (
	"os"

	"github.com/Agent-Field/codeaf/internal/executor/executortest"
)

// These tests call tools with no session around them, so those calls run on
// the host seat by name; production still refuses a call that lost its session.
func init() {
	// The resident regression starts an isolated child without this fallback
	// so a missing production seat cannot be hidden by the test process.
	if os.Getenv("CODEAF_TEST_NO_HOST_SEAT") != "1" {
		executortest.Host()
	}
}
