//go:build windows

package clipboard

import "context"

func goos() string { return "windows" }

// run is never reached: pick refuses every platform it has no helper for.
func run(context.Context, Command) error { return ErrUnavailable }
