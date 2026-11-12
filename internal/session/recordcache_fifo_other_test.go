//go:build !linux && !darwin

package session

import "errors"

func mkfifo(string) error { return errors.New("no named pipes on this platform") }
