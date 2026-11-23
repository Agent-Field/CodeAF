//go:build !linux && !darwin && !freebsd && !windows

package diskspace

import "errors"

func of(string) (Usage, error) { return Usage{}, errors.New("diskspace: not known on this system") }
