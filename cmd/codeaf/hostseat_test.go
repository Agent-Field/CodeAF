package main

import "github.com/Agent-Field/codeaf/internal/executor/executortest"

// These tests call tools with no session around them, so those calls run on
// the host seat by name; production still refuses a call that lost its session.
func init() { executortest.Host() }
