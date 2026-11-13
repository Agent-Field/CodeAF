//go:build !windows

package main

import "github.com/Agent-Field/codeaf/internal/appwire"

func platformReadiness() []appwire.ReadinessCheck { return nil }
