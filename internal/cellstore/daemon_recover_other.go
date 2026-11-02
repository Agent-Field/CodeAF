//go:build !linux && !darwin

package cellstore

import "context"

func recoverOwnedDaemon(context.Context, string) bool { return false }
