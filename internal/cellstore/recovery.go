package cellstore

import (
	"log"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// openRecovery joins the local WAL to committed receipts. A process can die
// after a seal writes its turn and before the Recorder removes its WAL rows.
// Only a receipt named by a committed turn proves that completion was sealed.
func openRecovery(c cell.Cell, path string) (*WAL, Recovery, error) {
	wal, recovery, err := OpenWAL(path)
	if err != nil || len(recovery.Completed) == 0 {
		return wal, recovery, err
	}
	// Started and SealedAtMs come from independent wall clocks, which can move
	// backward. A turn sealed before the oldest pending Started can still contain
	// that call, so a time cutoff is unsafe. The chain orders commits, not starts;
	// without a durable call-to-turn index, every committed receipt is needed.
	entries, err := Log(c)
	if err != nil {
		log.Print("recovery could not check saved receipts; keeping WAL replay unchanged")
		return wal, recovery, nil
	}
	sealed := make(map[string]bool)
	for _, entry := range entries {
		for _, call := range entry.Receipt.Calls {
			sealed[callKey(call.ID, call.ArgsHash, call.Started)] = true
		}
	}
	var pending, booked []Executed
	for _, call := range recovery.Completed {
		if sealed[callKey(call.Call.ID, call.Call.ArgsHash, call.Call.Started)] {
			booked = append(booked, call)
		} else {
			pending = append(pending, call)
		}
	}
	if len(booked) > 0 {
		if err := wal.Sealed(booked); err != nil {
			return nil, Recovery{}, err
		}
	}
	recovery.Completed = pending
	return wal, recovery, nil
}
