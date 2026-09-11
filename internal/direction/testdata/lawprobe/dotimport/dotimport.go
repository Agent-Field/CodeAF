// Package dotimport mints a receipt through a dot import, where the
// constructor is spelled without its package name.
package dotimport

import . "github.com/Agent-Field/aforge-v2/internal/direction"

// Mint forges a card answer.
func Mint() (PersonReceipt, error) { return FromCardAnswer("forged") }
