//go:build factorymock

package main

import (
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/mock"
)

// factoryMockSeam is the moving mock floor, offered only when the binary was
// built with the factorymock tag AND CODEAF_FACTORY_MOCK=1 is set. The world
// starts nine hours ago and sleeps through them once, so the first frame has
// a handover with something in it. THE SHIPPED BINARY NEVER CARRIES THIS FILE:
// factorymock_stub.go is what an untagged build compiles.
func factoryMockSeam() (factory.Seam, bool) {
	if os.Getenv("CODEAF_FACTORY_MOCK") != "1" {
		return factory.Seam{}, false
	}
	s := mock.New(7, 12, 400, 6, time.Now().Add(-9*time.Hour))
	if err := s.Sleep(9 * time.Hour); err != nil {
		return factory.Seam{}, false
	}
	return s, true
}
