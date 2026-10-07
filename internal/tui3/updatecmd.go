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
	// named is true when a PERSON named what to install — `/update v0.9.0`, or
	// a channel by hand. It is the only thing that makes replacing the file with
	// an older release deliberate, and the only road that reaches the installer
	// when its tag is the build already running. A tag THIS WINDOW pinned — the
	// release the offer was about, used for a bare `/update` during the
	// countdown — is not a person naming anything, which is why the intent is
	// carried here and never derived from the choice's own tag.
	named bool
}

// namedTag is whether the person named an exact release by hand.
func (m updateResolveMsg) namedTag() bool {
	return m.named && strings.TrimSpace(m.choice.Version) != ""
}

type updateInstallMsg struct {
	result codeupdate.InstallResult
	err    error
	auto   bool
	// tag is the release THIS WINDOW asked for. The installer answers an empty
	// [codeupdate.InstallResult] on every error ([codeupdate.Install]), so a
	// failure tag read from the result alone would be "", and the automatic
	// road would record no failure and name no release. It is carried from the
	// resolve, which is the last place both the answer and the offer's tag are
	// in hand ([app.tookUpdateInstall] falls back to the offer for a message
	// built before this field existed).
	tag string
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
	// THE MOMENT IS KEPT WITH THE TAG, because the automatic road resolves the
	// tag itself rather than the channel and the install record needs the fact
	// (see [updateOffer.published] and [app.pinnedChoice]).
	a.offer.published = message.available.LatestPublished
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
	// NO RESTART PLAN IS ASKED FOR. An install replaces the file and the next
	// launch opens it, so the ability to update here rests on the two doors
	// that do the work and on nothing else.
	if a.resolveUpdate == nil || a.installUpdate == nil {
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
	// A PERSON NAMED IT: the argument is a channel or an exact tag. That is the
	// intent [updateResolveMsg.named] carries, and a bare `/update` is the
	// default road that names nothing.
	return a.beginUpdate(updateChoice(argument, a.updateRunning), false, argument != "")
}

// beginUpdate captures the resolver and leaves the loop. Every road installs in
// the background; `auto` and `named` only change what the completion says and
// whether a rollback is deliberate.
func (a *app) beginUpdate(choice codeupdate.Choice, auto, named bool) tea.Cmd {
	resolve := a.resolveUpdate
	a.updateInFlight = true
	return func() tea.Msg {
		release, err := resolve(context.Background(), choice)
		return updateResolveMsg{release: release, choice: choice, err: err, auto: auto, named: named}
	}
}

// pinnedChoice is the request for ONE exact tag this window is holding, with the
// publish moment the launch check gave it ([updateOffer.published]). It is the
// automatic road's own answer and bare `/update` answered from an open offer:
// both pin a tag, both install with AllowDowngrade false, so both must hand the
// installer the fact that orders two same-day channel builds.
func (a *app) pinnedChoice(tag string) codeupdate.Choice {
	choice := updateChoice(tag, a.updateRunning)
	choice.Published = a.offer.published
	return choice
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
	// THE AUTOMATIC ROAD ASKS ONCE MORE, HERE. The resolver is off-frame, so a
	// row turned off or a tag another window dismissed can land between the
	// deadline and the bytes: the decision is asked again before anything is
	// downloaded. A command a person typed is theirs and is not re-checked.
	if message.auto {
		if withdrawn := a.updateAutoWithdrawn(message.release.Tag); withdrawn != "" {
			a.updateInFlight = false
			a.offer.clear()
			a.note(withdrawn)
			return nil
		}
	}
	// AN EXPLICIT TAG DOES NOT STOP HERE. `a.updateRunning` is the stamp of this
	// PROCESS; the file on disk can be something else entirely, because another
	// window installed a newer build while this one kept working. `/update <tag>`
	// is a person asking for that exact file, so it reaches the shared
	// installer, which is idempotent when the disk already holds it and refuses
	// a silent rollback otherwise. The automatic road and the default channel
	// keep the quiet no-op, where the stamp really is the answer.
	if message.release.Tag == a.updateRunning && !message.namedTag() {
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
	// THE TAG IS COMPUTED ONCE, AND THE OFFER KEEPS IT WHEN THE RESOLVE COULD
	// NOT. A resolver that answers a bare tag (the pinned road) always names
	// one; a resolver that errored may not, and the offer's own tag is the last
	// fact either road has.
	tag := strings.TrimSpace(message.release.Tag)
	if tag == "" {
		tag = strings.TrimSpace(a.offer.tag)
	}
	a.offer.downloading()
	a.offer.tag = tag
	a.note(a.updateOfferNote())
	install := a.installUpdate
	release := message.release
	// ONLY A PERSON NAMING AN EXACT TAG IS A DELIBERATE ROLLBACK. A channel, a
	// bare `/update`, the automatic road and the bare `/update` that answers an
	// open offer all follow the release line forward, so a file another window
	// advanced is never stepped back by them.
	allowDowngrade := message.namedTag()
	return func() tea.Msg {
		result, err := install(context.Background(), codeupdate.InstallOptions{
			Release: release, AllowDowngrade: allowDowngrade,
		})
		return updateInstallMsg{result: result, err: err, auto: message.auto, tag: tag}
	}
}

// tookUpdateInstall finishes one install. Both roads say the same thing: the
// file is replaced, this process keeps the build it started on, and a later
// launch opens the new one.
func (a *app) tookUpdateInstall(message updateInstallMsg) tea.Cmd {
	a.updateInFlight = false
	// THE TAG SURVIVES A FAILURE. Every error out of the installer carries an
	// empty result, so the tag this window asked for is read from the message,
	// then the result, and last from the offer that raised it — never from the
	// empty result alone. Without this the automatic road recorded no failure
	// and drew `codeaf  could not be installed`: no count, no backoff, and no
	// release named in the line a person reads.
	tag := strings.TrimSpace(message.tag)
	if tag == "" {
		tag = strings.TrimSpace(message.result.Release.Tag)
	}
	if tag == "" {
		tag = strings.TrimSpace(a.offer.tag)
	}
	if message.err != nil {
		return a.updateStopped(message.auto, tag, message.err)
	}
	if message.result.Already {
		// ALREADY INSTALLED IS SUCCESS, SAID QUIETLY: the build on disk is the
		// one this window was about to write, so there is nothing to alarm.
		a.offer.clear()
		a.note("codeaf " + tag + " is already the build on disk · this session is untouched")
		return nil
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
