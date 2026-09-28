//go:build !windows

package orclient

import "github.com/Agent-Field/codeaf/internal/modelsource"

// Service is the identity of the model service whose wire this client speaks:
// OpenRouter's, which is the shape codeaf's model API answers in. It is the
// provider every model senior-dev asks for is filed under, the key a request's
// service options are kept under, and the namespace the service's reasoning
// details and finish metadata come back in.
//
// CODEAF SPELLS THAT IDENTITY ONCE, as modelsource.DefaultID, and a law holds
// the whole module to it (internal/modelsource/purity_law_test.go). Every use
// in senior-dev reads it from here, so the word is written in one place in
// codeaf and in none in this program.
const Service = modelsource.DefaultID
