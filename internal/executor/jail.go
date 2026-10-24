package executor

import (
	"errors"
	"fmt"
)

// ErrNoIsolation is the refusal of a call that must have no network on a
// system that cannot isolate it. It reads as a sentence to the person: the
// call did not run.
var ErrNoIsolation = errors.New("this command was not run: the workspace is sandboxed (no network) but this system " +
	"cannot block the network")

// refuseWithout fails closed, saying why and what to do, when the network must
// be denied; a call that may use the network runs unconfined (degraded).
func refuseWithout(req ExecRequest, remedy string) error {
	if req.Net.Denies() {
		return fmt.Errorf("%w: %s", ErrNoIsolation, remedy)
	}
	return nil
}
