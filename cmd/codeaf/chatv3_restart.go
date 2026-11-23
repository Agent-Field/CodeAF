package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// heldRestartWithin is how long an open stands behind the start of a restart,
// for the terminal and the app alike. The files are fetched BEHIND the open, so
// this bounds only the part in front of it (reading the chat's record from the
// sync service and putting the arrival in place), which is well under a second.
// A sync service that cannot be reached must not hold up opening a chat that is
// saved on this computer, and a restart that did not start leaves the failure
// exactly as it was.
const heldRestartWithin = 10 * time.Second

// restartHeldFilesBeforeOpen is the terminal's half of #2781: OPENING A CHAT THIS
// COMPUTER ALREADY HOLDS RESTARTS THE FILES THAT FAILED TO ARRIVE. It is called
// by [openV3Agent], the one function every terminal open of a saved chat ends in
// (the launch, Home's local open, the resume picker, /resume), so no door has its
// own copy; the app's open asks the same [syncsetup.Continuer.RestartFiles]
// ([appBackend.restartHeldFiles]).
//
// It runs BEFORE the conversation opens, as a take does, because the restart asks
// any window holding the chat's journal to let go, and a conversation opened
// first would be that window. Most opens end at its first line: a chat whose
// files arrived has no failure to find, and nothing is read from the sync service
// for it.
//
// NOTHING HERE CAN REFUSE THE OPEN. A restart that could not start is said in the
// log, and the open goes on with the failure as it was and its refusal accurate.
func restartHeldFilesBeforeOpen(root string) {
	if root == "" || cell.AbandonedArrival(root) == nil {
		return
	}
	s, err := syncOfFirst()
	if err != nil {
		return
	}
	id := filepath.Base(root)
	// ONLY A CHAT THIS HOME KEEPS IS ONE ITS SYNC SIDE CAN RESTART: the take finds
	// a chat by its id, so a folder somebody pointed the terminal at that is not
	// where this home keeps that id is left alone.
	if !sameChatFolder(takeRootForAt(s.Home, id), root) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), heldRestartWithin)
	defer cancel()
	terminalMoving.start(id)
	restarted, err := chatContinuerWith(s, surfaceNotices.Say, terminalMoving.report, true, continueSeams{arrived: terminalArrived(surfaceNotices)}).RestartFiles(ctx, id)
	if err != nil {
		log.Printf("open %s: could not restart the files that failed to arrive: %v", id, err)
	}
	if err != nil || !restarted {
		terminalMoving.stop(id)
	}
}

// sameChatFolder says a and b are one folder, whatever links lead to it.
func sameChatFolder(a, b string) bool {
	left, err := os.Stat(a)
	if err != nil {
		return false
	}
	right, err := os.Stat(b)
	return err == nil && os.SameFile(left, right)
}
