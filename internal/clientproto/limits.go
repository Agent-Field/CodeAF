// Package clientproto defines the shared bounds of the scoped-client wire.
// Executors and the Go relay use the same limits so a locally accepted call
// or snapshot cannot promise bytes the receiving relay will refuse.
package clientproto

import "time"

const (
	MaxSlotBytes     = 64 << 10
	MaxClientSlots   = 64
	MaxQueueBytes    = 4 << 10
	MaxClientQueue   = 128
	MaxFabricBody    = 96 << 10
	MaxClientMethods = 64
	MaxAge           = 24 * time.Hour
)
