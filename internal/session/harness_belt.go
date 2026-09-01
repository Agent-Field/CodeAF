package session

// harness_belt.go is the ONE answer to "which tools may a saved harness reach".
//
// It had two, and they were a drift pair. The designer was told what it could
// whitelist by [Agent.harnessMachinery], the lint checked the whitelist it wrote
// against [Agent.harnessToolNames], and the RUN resolved those same names
// against a third list assembled in cmd/aforge's chatv3_harness.go — three
// independent calls to bare.AllTools that nothing held together. A verb added to
// one of them and not the others is a harness the designer may write and the
// runner cannot execute, or worse, a verb the runner would happily execute that
// the lint refuses to let anybody name.
//
// So all three read this.
//
// ── WHAT A HARNESS GETS, AND WHY IT IS NOT THE WHOLE BELT ──
//
// The wire tools (internal/exec/bare) — read, bash, edit, write, grep, find, ls
// — plus THE MEDIA FAMILY, and nothing else.
//
// The exclusions are the same ones they always were, and the reason still
// holds: notes, jobs, connected accounts, watches, settings and the task verbs
// are things a CONVERSATION reaches for. A saved procedure that inherited them
// would be a recipe that could rewrite the person's settings or start a task
// tree, months after anybody read it.
//
// THE MEDIA FAMILY IS NOT IN THAT CATEGORY AND WAS TREATED AS THOUGH IT WERE.
// "Image generation is something a conversation reaches for" was written when
// the only thing anybody made with a harness was a diff — and it was already
// false when it was written: the first harness design anybody wrote for a
// marketing image put generate_image and view_image in its steps, and the lint
// refused the whitelist. Producing a picture to a fixed recipe is exactly the
// kind of work a saved procedure is FOR, and there is nothing conversational
// about it: it takes a prompt, writes a file, and names the path.
//
// ── THE CONDITIONAL-PRESENCE LAW TRAVELS ──
//
// Each media verb is here only when this machine has a model for it, by the
// belt's own law (tools_media.go's [Agent.mediaHand]) and through the very same
// constructors the conversation's belt uses. A machine that cannot draw does not
// offer the designer generate_image, does not let the lint accept it on a
// whitelist, and does not resolve it at run time — three "no"s from one place,
// which is the whole point of this file.

import (
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// HarnessBeltSeams is everything the media half of the belt needs from the door
// that builds it. It is a struct rather than four arguments because the run
// door (cmd/aforge's chatv3_harness.go) fills it from a config it already holds,
// and a positional list of three interfaces and a function is a call nobody can
// read at the call site.
//
// ALL OF IT IS OPTIONAL. A zero HarnessBeltSeams yields exactly the seven wire
// tools, which is what a harness has always had and what a machine with no media
// models still gets.
type HarnessBeltSeams struct {
	// Media and MediaModel are [Config]'s own pair, with [Config]'s own meaning:
	// the client that carries a generation request, and the resolver that says
	// which model serves a modality. Either one absent takes every generation
	// verb off, exactly as it does on the conversation's belt.
	Media      MediaGenerator
	MediaModel func(modality string) string

	// MediaPick is [Config.MediaPick]: the just-in-time model choice on the
	// four making verbs. Nil keeps the `model` argument off their schemas,
	// exactly as it does on the conversation's belt.
	MediaPick func(modality, word string) (string, error)

	// Seer is the completer view_image asks to look. It is separate from Media
	// because looking is a CHAT call to a vision model and not a media-endpoint
	// call, which is the same split the conversation's belt makes
	// (tools_view.go). Nil leaves view_image off however well the resolver
	// answers, because a verb with nothing to send the picture to is a verb that
	// would refuse on every call.
	Seer Completer
}

// HarnessBelt is every tool a saved harness may name on its whitelist, may
// resolve at run time, and may be told about while it is being designed.
//
// The workspace is the one the tools act in. It is the DESIGNER's workspace when
// this is called for the guide and the RUN's when it is called for the bridges,
// and those are the same directory in every door that exists today — but the
// argument stays explicit rather than being read off a config, because the day
// they differ the tools must act in the run's and not in the one a page was
// written in.
func HarnessBelt(workspace string, seams HarnessBeltSeams) []bare.Tool {
	tools := bare.AllTools(workspace)

	// The media verbs are built by the CONVERSATION'S OWN CONSTRUCTORS through a
	// throwaway agent, and that is deliberate rather than lazy: a harness that
	// drew with a second implementation of generate_image would drift from the
	// one the person watched work in the chat — a different description, a
	// different destination, a different sentence back. This agent is never run,
	// never journals and never speaks; it exists so the four constructors have
	// the receiver they are written against.
	//
	// Its usage counters go nowhere, which is correct here and stated so nobody
	// "fixes" it: a harness run accounts its own spend through the trail, and a
	// picture billed twice would be worse than one billed in one place.
	agent := &Agent{
		config: Config{Workspace: workspace, Media: seams.Media, MediaModel: seams.MediaModel, MediaPick: seams.MediaPick},
		client: seams.Seer,
	}
	tools = append(tools, agent.imageTools()...)
	tools = append(tools, agent.speakTools()...)
	tools = append(tools, agent.musicTools()...)
	tools = append(tools, agent.videoTools()...)
	if seams.Seer != nil {
		tools = append(tools, agent.viewTools()...)
	}
	// edit_video needs NO SEAM AT ALL — no client, no resolver, no seer — because
	// it buys nothing: it is ffmpeg on this machine, and its gate is PATH. So a
	// saved procedure that assembles generated clips into one film can name it on
	// its whitelist wherever ffmpeg is, including on a machine that has no video
	// model to generate a clip with in the first place.
	return append(tools, agent.videoEditTools()...)
}

// HarnessBeltNames is the same belt as a set, for the lint that checks a
// whitelist and for anything else that only needs to ask "may this name be
// written down here".
func HarnessBeltNames(workspace string, seams HarnessBeltSeams) map[string]bool {
	names := map[string]bool{}
	for _, tool := range HarnessBelt(workspace, seams) {
		names[tool.Name] = true
	}
	return names
}

// harnessSeams is this agent's own media wiring, in the shape the belt builder
// takes. It is how the DESIGNER side gets the same answer the run side will:
// the guide is written from the conversation's seams, and the run is executed
// with the door's, and both are the person's one install.
func (a *Agent) harnessSeams() HarnessBeltSeams {
	return HarnessBeltSeams{
		Media:      a.config.Media,
		MediaModel: a.config.MediaModel,
		MediaPick:  a.config.MediaPick,
		Seer:       a.client,
	}
}
