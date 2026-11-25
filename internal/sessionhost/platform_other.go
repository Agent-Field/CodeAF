//go:build !linux

package sessionhost

import (
	"context"
	"errors"
	"fmt"
	"os"
)

const unsupported = "Hosted chats require a Linux server."

func Probe(cfg Config) Report {
	return Report{
		Mode: modeRefused, Reason: unsupported, Require: cfg.Require,
		EUID: os.Geteuid(), FenceMode: "none", CapNamesEff: []string{},
		UIDSwitch: SwitchReport{Error: unsupported},
	}
}

func ListenAndServe(context.Context, Config, Report) error {
	return errors.New(unsupported)
}

func RunHelper([]string) int {
	fmt.Fprintln(os.Stderr, unsupported)
	return 126
}

// RestrictSupervisor has nothing to restrict where hosted chats do not run.
func RestrictSupervisor() error { return nil }
