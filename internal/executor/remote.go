package executor

import (
	"context"
	"errors"
)

// ErrRemoteNotImplemented is what Remote answers until transport exists.
var ErrRemoteNotImplemented = errors.New("executor: remote execution is not implemented")

// Remote will run a request on another device.
type Remote struct{}

func (Remote) Exec(context.Context, ExecRequest, func(Chunk)) (ExecResult, error) {
	return ExecResult{}, ErrRemoteNotImplemented
}
