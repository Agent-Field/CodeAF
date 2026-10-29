//go:build linux || darwin

package executor

import "github.com/Agent-Field/codeaf/internal/processgroup"

func membersOf(pgid int) []int { return processgroup.Members(pgid) }
