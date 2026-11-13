package tui3

const removedPairNotice = "removed by another of your devices — pair again to sync"

func (a *app) removedComputer() {
	a.machineRead.computerRemoved = true
	a.teamsShareGeneration++
	if a.teamsDisk.door.Sharing == nil {
		a.adoptTeamsSharing(false)
	} else {
		a.teamsSharingCmd = a.refreshTeamsSharing()
	}
	a.leftOff.row = nil
	a.continueGeneration++
	a.dropHomeAsk()
	a.homeAccountNotice = removedPairNotice
	a.addMachine.known, a.addMachine.size = true, 1
	a.home.say("", "")
	a.touch()
}
