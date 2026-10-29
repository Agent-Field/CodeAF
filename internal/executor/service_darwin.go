//go:build darwin

package executor

import (
	"slices"

	"github.com/Agent-Field/codeaf/internal/darwinproc"
)

func cmdline(pid int) []string { return darwinproc.Argv(pid) }

func pidPorts(pid int) []int { return darwinproc.ListenPorts(pid) }

func listenPorts(pids []int) []int {
	ports := make([]int, 0)
	for _, pid := range pids {
		ports = append(ports, pidPorts(pid)...)
	}
	slices.Sort(ports)
	return slices.Compact(ports)
}
