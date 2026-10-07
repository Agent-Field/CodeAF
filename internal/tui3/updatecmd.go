package tui3

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

type updateCheckMsg struct {
	available codeupdate.Available
	show      bool
}

type updateResolveMsg struct {
	release codeupdate.Release
	choice  codeupdate.Choice
	err     error
	// auto is true when the offer's own road raised this, in the background and
	// for a release the person did not have to ask for.
	auto bool
}

type updateInstallMsg struct {
	result codeupdate.InstallResult
	err    error
	auto   bool
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

// tookUpdateCheck folds the launch check. OFF still names the release, because
// knowing is not acting; ON raises the offer and its countdown.
func (a *app) tookUpdateCheck(message updateCheckMsg) tea.Cmd {
	if !message.show {
		return nil
	}
	if a.updateAuto == nil {
		a.note(message.available.Notice())
		return nil
	}
	if refusal := a.updateRefusal(); refusal != "" {
		a.note(quietUpdateNotice(message.available))
		if a.updateAuto.Enabled {
			a.note(refusal)
		}
		return nil
	}
	if !a.updateAuto.Enabled {
		a.note(quietUpdateNotice(message.available))
		return nil
	}
	tag := strings.TrimSpace(message.available.Latest)
	state := codeupdate.AutoState{}
	if a.updateAuto.State != nil {
		state = a.updateAuto.State()
	}
	if !codeupdate.ShouldOffer(state, tag, a.now()) {
		return nil
	}
	a.offer.raise(tag, a.updateRunning, a.now())
	a.note(a.updateOfferNote())
	return a.offerTick()
}

// runUpdateCommand is the typed road, on the same coordinator as the offer. It
// never interrupts anything: an install begun here replaces the file in the
// background, and a turn or a task may keep running throughout.
func (a *app) runUpdateCommand(argument string) tea.Cmd {
	argument = strings.TrimSpace(argument)
	switch argument {
	case "skip":
		switch {
		case a.offer.offering():
			a.skipUpdateOffer()
		case a.offer.active():
			// AN INSTALL ALREADY RUNNING IS NOT OURS TO CANCEL, and saying it
			// was skipped would be a lie the download immediately contradicts.
			a.note("an install is already running · it finishes in the background · /update never stops the next one")
		default:
			a.note("there is no update offer to skip · /update installs one for the next launch when you want it")
		}
		return nil
	case "never":
		a.declineUpdateOffer()
		return nil
	}
	if a.updateInFlight {
		a.note("an update is already running")
		return nil
	}
	if a.resolveUpdate == nil || a.installUpdate == nil || a.restart == nil {
		a.note("this window cannot update codeaf here · install a release with: " + a.updateCurlLine())
		return nil
	}
	if codeupdate.Kind(a.updateRunning) == "other" {
		a.note("this codeaf was built from source · rebuild with make build, or install a release: " + codeupdate.CurlCommand)
		return nil
	}
	if refusal := a.updateRefusal(); refusal != "" {
		a.note(refusal)
		return nil
	}
	// BARE `/update` WHILE THE OFFER IS UP is the offer's own "now".
	if argument == "" && a.offer.offering() {
		return a.updateNowFromOffer()
	}
	a.offer.clear()
	return a.beginUpdate(updateChoice(argument, a.updateRunning), false)
}

// beginUpdate captures the resolver and leaves the loop. Every road installs in
// the background; `auto` only changes what the completion says.
func (a *app) beginUpdate(choice codeupdate.Choice, auto bool) tea.Cmd {
	resolve := a.resolveUpdate
	a.updateInFlight = true
	return func() tea.Msg {
		release, err := resolve(context.Background(), choice)
		return updateResolveMsg{release: release, choice: choice, err: err, auto: auto}
	}
}

func updateChoice(argument, running string) codeupdate.Choice {
	argument = strings.TrimSpace(argument)
	switch argument {
	case "":
		return codeupdate.Choice{Channel: codeupdate.FollowedChannel(running), Running: running}
	case "stable":
		return codeupdate.Choice{Channel: "stable", Running: running}
	case "rc", "dev", "staging":
		return codeupdate.Choice{Channel: argument, Running: running}
	default:
		return codeupdate.Choice{Version: argument, Running: running}
	}
}

func (a *app) tookUpdateResolve(message updateResolveMsg) tea.Cmd {
	if message.err != nil {
		return a.updateStopped(message.auto, message.choice.Version, message.err)
	}
	if message.release.Tag == a.updateRunning {
		a.updateInFlight = false
		a.offer.clear()
		a.note("you are on the newest codeaf, " + message.release.Tag)
		return nil
	}
	if message.choice.Version == "" {
		available := codeupdate.Available{
			Latest: message.release.Tag, Running: a.updateRunning,
			LatestPublished: message.release.PublishedAt, RunningPublished: message.release.RunningPublishedAt,
		}
		if available.Ahead() {
			channel := message.choice.Channel
			if channel == "" {
				channel = "stable"
			}
			a.updateInFlight = false
			a.offer.clear()
			a.note("this codeaf is " + a.updateRunning + ", ahead of the newest " + channel + " " + message.release.Tag + " — /update " + message.release.Tag + " installs it anyway")
			return nil
		}
	}
	// The lock, the on-disk re-read and the duplicate/downgrade refusal all live
	// inside [codeupdate.Install], so the chat surface has no second acquirer to
	// keep in step with the command line.
	a.offer.downloading(message.auto)
	a.offer.tag = strings.TrimSpace(message.release.Tag)
	a.note(a.updateOfferNote())
	install := a.installUpdate
	release := message.release
	// ONLY A NAMED TAG IS A DELIBERATE ROLLBACK. A channel or a bare /update
	// follows the release line forward, so a file another window advanced is
	// never stepped back by it.
	allowDowngrade := !message.auto && strings.TrimSpace(message.choice.Version) != ""
	return func() tea.Msg {
		result, err := install(context.Background(), codeupdate.InstallOptions{
			Release: release, AllowDowngrade: allowDowngrade,
		})
		return updateInstallMsg{result: result, err: err, auto: message.auto}
	}
}

// tookUpdateInstall finishes one install. Both roads say the same thing: the
// file is replaced, this process keeps the build it started on, and a later
// launch opens the new one.
func (a *app) tookUpdateInstall(message updateInstallMsg) tea.Cmd {
	a.updateInFlight = false
	if message.err != nil {
		return a.updateStopped(message.auto, message.result.Release.Tag, message.err)
	}
	if message.result.Already {
		// ALREADY INSTALLED IS SUCCESS, SAID QUIETLY: the build on disk is the
		// one this window was about to write, so there is nothing to alarm.
		tag := strings.TrimSpace(message.result.Release.Tag)
		a.offer.clear()
		a.note("codeaf " + tag + " is already the build on disk · this session is untouched")
		return nil
	}
	tag := strings.TrimSpace(message.result.Release.Tag)
	if tag == "" {
		tag = a.offer.tag
	}
	a.offer.ready(a.now())
	a.offer.tag = tag
	if a.updateAuto != nil && a.updateAuto.Success != nil {
		a.updateAuto.Success()
	}
	if message.auto {
		// THE QUIET ROAD: one line, no checksum, no path, no curl script. Those
		// belong to a person who asked for diagnostics.
		a.note(a.updateOfferNote())
		return a.offerSettleTick()
	}
	a.note("checksum matched · installed at " + message.result.Path)
	a.note(a.updateOfferNote())
	return a.offerSettleTick()
}

// updateStopped is the one completion a failed install has, on either road. A
// background failure names the error and the manual road in ONE line and keeps
// the old version running; a hand-run one may carry the curl road, because the
// person asked for the details.
func (a *app) updateStopped(auto bool, tag string, err error) tea.Cmd {
	a.updateInFlight = false
	if reason, refused := refusalReason(err); refused {
		// A REFUSAL IS NOT A FAILURE: nothing was downloaded, nothing is
		// retried, and the release's automatic attempts are untouched.
		a.offer.clear()
		a.note(reason)
		return nil
	}
	tag = strings.TrimSpace(tag)
	if auto {
		if a.updateAuto != nil && a.updateAuto.Failure != nil && tag != "" {
			a.updateAuto.Failure(tag, err)
		}
		a.offer.failed(a.now())
		a.offer.tag = tag
		a.note("codeaf " + tag + " could not be installed · this version keeps running · " + err.Error() + " · /update retries")
		return a.offerSettleTick()
	}
	a.offer.clear()
	a.note("could not update codeaf: " + err.Error())
	a.note("install a release with: " + a.updateCurlLine())
	return nil
}

func (a *app) updateFailed(err error) {
	a.note("could not update codeaf: " + err.Error())
	a.note("install a release with: " + a.updateCurlLine())
}

func (a *app) updateCurlLine() string {
	if strings.TrimSpace(a.updateCurl) == "" {
		return codeupdate.CurlCommand
	}
	return a.updateCurl
}
