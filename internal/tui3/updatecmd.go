package tui3

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

type updateCheckMsg struct {
	available codeupdate.Available
	show      bool
}

type updateResolveMsg struct {
	release codeupdate.Release
	err     error
}

type updateInstallMsg struct {
	result codeupdate.InstallResult
	err    error
}

type updateRestartMsg struct{}

const updateTurnRefusal = "finish the update before starting a turn"

// updateStopsTurn keeps every turn-starting door behind the update that owns
// the next process start. Home also receives the same words on its visible
// message line because its full-page frame hides conversation notes.
func (a *app) updateStopsTurn() bool {
	if !a.updateActive {
		return false
	}
	a.note(updateTurnRefusal)
	if a.at(pageHome) {
		a.home.say(updateTurnRefusal, "")
	}
	return true
}

// checkForUpdate starts after the app is ready to draw, so a slow or absent
// network can never become a slow first frame.
func (a *app) checkForUpdate() tea.Cmd {
	if a.updateCheck == nil {
		return nil
	}
	return func() tea.Msg {
		available, show := a.updateCheck(a.ctx)
		return updateCheckMsg{available: available, show: show}
	}
}

func (a *app) tookUpdateCheck(message updateCheckMsg) tea.Cmd {
	if message.show {
		a.note(message.available.Notice())
	}
	return nil
}

func (a *app) runUpdateCommand(argument string) tea.Cmd {
	if a.state == stateWorking || len(a.runningNodes()) != 0 {
		a.note("finish the running turn or task before updating codeaf")
		return nil
	}
	if a.updateActive {
		a.note("an update is already running")
		return nil
	}
	if a.resolveUpdate == nil || a.installUpdate == nil || a.restart == nil {
		a.note("this window cannot update codeaf here · install a release with: " + codeupdate.CurlCommand)
		return nil
	}
	if codeupdate.Kind(a.updateRunning) == "other" {
		a.note("this codeaf was built from source · rebuild with make build, or install a release: " + codeupdate.CurlCommand)
		return nil
	}
	choice := updateChoice(argument)
	resolve := a.resolveUpdate
	// The mark is set before the command leaves the loop. A second /update or a
	// new turn can therefore never enter while release selection is off-frame.
	a.updateActive = true
	return func() tea.Msg {
		release, err := resolve(context.Background(), choice)
		return updateResolveMsg{release: release, err: err}
	}
}

func updateChoice(argument string) codeupdate.Choice {
	argument = strings.TrimSpace(argument)
	switch argument {
	case "", "stable":
		return codeupdate.Choice{Channel: "stable"}
	case "rc", "dev", "staging":
		return codeupdate.Choice{Channel: argument}
	default:
		return codeupdate.Choice{Version: argument}
	}
}

func (a *app) tookUpdateResolve(message updateResolveMsg) tea.Cmd {
	if message.err != nil {
		a.updateFailed(message.err)
		return nil
	}
	if message.release.Tag == a.updateRunning {
		a.updateActive = false
		a.note("you are on the newest codeaf, " + message.release.Tag)
		return nil
	}
	a.note("downloading codeaf " + message.release.Tag + " for " + runtimePlatform() + "…")
	install := a.installUpdate
	return func() tea.Msg {
		result, err := install(context.Background(), message.release)
		return updateInstallMsg{result: result, err: err}
	}
}

func runtimePlatform() string {
	return runtimeGOOS + "/" + runtimeGOARCH
}

func (a *app) tookUpdateInstall(message updateInstallMsg) tea.Cmd {
	if message.err != nil {
		a.updateFailed(message.err)
		return nil
	}
	a.note("checksum matched · installed at " + message.result.Path)
	a.note("restarting on " + message.result.Release.Tag + "…")
	*a.restart = codeupdate.Plan{
		Path: message.result.Path,
		Args: codeupdate.RestartArgs(a.updateArgs, a.file),
	}
	// The final two notes get one frame of their own before the program quits.
	// Quitting in this same update would restore the terminal before Bubble Tea
	// had painted the checksum and restart lines at all.
	return surfaceTick(frameInterval, func(time.Time) tea.Msg { return updateRestartMsg{} })
}

func (a *app) updateFailed(err error) {
	a.updateActive = false
	a.note("could not update codeaf: " + err.Error())
	a.note("install a release with: " + codeupdate.CurlCommand)
}
