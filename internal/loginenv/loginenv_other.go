//go:build !darwin

package loginenv

import "context"

func loginPath(context.Context) (string, error) {
	return "", nil
}
