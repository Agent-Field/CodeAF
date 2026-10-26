package tui3

import "github.com/Agent-Field/codeaf/internal/chatlist"

func (a *app) removedComputer() {
	a.machineRead.computerRemoved = true
	a.leftOff.row = nil
	a.continueGeneration++
	a.dropHomeAsk()
	a.homeAccountNotice = chatlist.Removed
	a.home.say(chatlist.Removed, "")
	a.touch()
}
