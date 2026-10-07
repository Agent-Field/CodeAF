//go:build !factorymock

package main

import "github.com/Agent-Field/codeaf/internal/factory"

// factoryMockSeam is the absent door: an untagged build has no mock floor, so
// the shipped binary spends none of its size budget on one. The tagged twin
// lives in factorymock.go.
func factoryMockSeam() (factory.Seam, bool) { return factory.Seam{}, false }
