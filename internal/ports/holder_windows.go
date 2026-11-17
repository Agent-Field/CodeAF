//go:build windows

package ports

import (
	"context"
	"errors"
	"syscall"
)

const wsaEADDRINUSE syscall.Errno = 10048

func addrInUse(err error) bool { return errors.Is(err, wsaEADDRINUSE) }

// lsofHolder is absent on Windows: a holder is simply not named.
func lsofHolder(context.Context, int) *Holder { return nil }
