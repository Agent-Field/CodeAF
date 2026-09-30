//go:build !darwin && !linux

package session

import "errors"

// cloneTree cannot clone on this system: a folder that would be cloned is
// left out, or linked when the project listed it ([carryOne]).
func cloneTree(string, string) error {
	return errors.New("this system clones no folders copy-on-write")
}
