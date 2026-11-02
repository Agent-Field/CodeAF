package main

import "github.com/Agent-Field/codeaf/internal/delegate/builtin"

// startupMaintenanceNeeded leaves informational requests free of home adoption
// and scheduler changes. Real starts retain the compatibility cleanup.
func startupMaintenanceNeeded(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "version", "--version", "-v", "help", "--help", "-h", "manual":
		return false
	}
	if _, carried := builtin.Find(args[0]); carried && len(args) > 1 && args[1] == "help" {
		return false
	}
	for _, arg := range args[1:] {
		if arg == "--" {
			break
		}
		if askedForHelp([]string{arg}) {
			return false
		}
	}
	return true
}
