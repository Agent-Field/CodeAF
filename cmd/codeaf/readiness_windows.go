package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/shell"
)

func platformReadiness() []appwire.ReadinessCheck {
	state, message := "ok", "Git Bash found"
	if _, err := shell.Bash(); err != nil {
		state, message = "error", err.Error()
	}
	return []appwire.ReadinessCheck{{Name: "Git Bash", State: state, Message: message, Fix: "install Git for Windows or set CODEAF_BASH to its bash.exe"}}
}
