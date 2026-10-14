package space

import "github.com/Agent-Field/codeaf/internal/apps/bridge"

func conflict(message string) *bridge.Error {
	return bridge.Errorf(bridge.CodeConflict, message)
}
func denied(message string) *bridge.Error { return bridge.Errorf(bridge.CodeDenied, message) }
func invalid(message string) *bridge.Error {
	return bridge.Errorf(bridge.CodeInvalid, message)
}
func notFound(message string) *bridge.Error {
	return bridge.Errorf(bridge.CodeNotFound, message)
}

func inviteExpired(by string) *bridge.Error {
	if by == "" {
		by = "the person who invited you"
	}
	return conflict("This invite has expired · Ask @" + by + " for a new one")
}
