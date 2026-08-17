package main

import (
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The consent card's door back to disk: where "always" is written down.
//
// It is a PAIR — one seam for a tool, one for a shell command — handed to the
// surface together, because they answer the same question against the two rows
// that can answer it: tools.approval names a tool, tools.bashPatterns names a
// command line, and the card knows which of the two it is looking at.
//
// It lives in its own file beside chatv3.go for chatv2_rail.go's stated reason:
// the seam that lets the v3 surface reach a registry row is a different thing
// from the window's assembly, and keeping them apart means a wave editing one
// does not collide with a wave editing the other.
//
// THE PROFILE IS THE PLACE. A repository's .aforge-v3/config.json is never written
// from here — it is a file a team commits, and a keystroke on a consent card
// must not commit to somebody's repository. internal/config's approvalmemory.go
// states the honest consequence: while a repository answers one of these rows,
// its answer replaces the person's WHOLE at launch, so what is written here
// takes effect everywhere except inside that repository.

// saveToolApproval remembers one tool's allow in the person's own profile.
//
// The error is returned rather than dropped here, which is the ONE difference
// from saveRail's shape (chatv2_rail.go). The rail's caller has nothing left to
// say once the sidebar has moved; this one's caller is about to print a receipt
// claiming the answer was saved, so it has to be told whether that is true. The
// failure is still dropped — the surface drops it, silently, and keeps the
// session-scoped always it always had.
func saveToolApproval(profileDir, tool string) error {
	return config.RememberToolApproval(profileDir, tool, "allow")
}

// saveBashApproval remembers one whole command line as an allow rule.
//
// It is never a deny, and neither is the seam above: the card only ever persists
// a yes (internal/tui3's consent.go says why), and a standing never is a line a
// person writes in the settings sheet on purpose.
func saveBashApproval(profileDir, command string) error {
	return config.RememberBashApproval(profileDir, command)
}

// bankToolApproval and bankBashApproval are the two seams the surface actually
// holds: write the rule down, then hand the running session the gate that rule
// is part of.
//
// THE SECOND HALF IS WHAT MAKES THE FIRST HALF MEAN ANYTHING TODAY. Without it,
// pressing always saved a preference that the running gate could not see, and
// the only thing stopping the next question was internal/session's
// session-scoped memo — which covers this agent's remaining life and nothing
// beyond it. The rebuild is the same v3Policy the launch ran, over the same
// three rows through the same project layer, so a banked rule reaches the gate
// by exactly the road it would have taken on the next launch, minus the launch.
//
// THE WRITE'S ERROR IS THE ONE THAT TRAVELS. The surface is about to say
// "saved" and it must be told whether that is true (internal/tui3's Options
// says why), and what is true is whether the row reached the disk. A rebuild
// that then fails has not made the write a lie: the rule IS saved and the next
// session will read it, so the error is dropped and the gate already standing
// stays standing. That case needs a settings file to have stopped parsing
// between the write and the read a moment later, which the launch's own read of
// the same rows would already have refused to start on.
func bankToolApproval(agent *session.Agent, workspace, profileDir string, yolo bool) func(string) error {
	return func(tool string) error {
		if err := saveToolApproval(profileDir, tool); err != nil {
			return err
		}
		refreshV3Policy(agent, workspace, profileDir, yolo)
		return nil
	}
}

func bankBashApproval(agent *session.Agent, workspace, profileDir string, yolo bool) func(string) error {
	return func(command string) error {
		if err := saveBashApproval(profileDir, command); err != nil {
			return err
		}
		refreshV3Policy(agent, workspace, profileDir, yolo)
		return nil
	}
}

// applyV3Approvals is the permissions panel's live seam: a line taken back
// there is already off the disk by the time this runs, and this is what makes
// the gate this conversation is running on agree.
//
// THE REBUILD'S ERROR IS THE ONE THAT TRAVELS HERE, which is the opposite of
// the banking pair above and for the same reason stated the other way round.
// There the write was the claim and the rebuild was a bonus; here the drop has
// already been written and the only open question is whether the running gate
// heard about it. A rebuild that fails means the rule is gone from the file and
// still standing in this session, and the panel says exactly that — the receipt
// names the next session rather than claiming the line is already gone.
func applyV3Approvals(agent *session.Agent, workspace, profileDir string, yolo bool) func() error {
	return func() error {
		policy, err := v3Policy(workspace, profileDir, yolo)
		if err != nil {
			return err
		}
		agent.SetApprovalPolicy(policy)
		return nil
	}
}

// v3CurrentGate copies a launch config and swaps in the gate as it stands right
// now. It is what /new and /resume open a second conversation on.
//
// THE SAME COMPLAINT, ONE DOOR OVER. cfg is the config this window launched on
// and it carries the policy that launch built, so an agent opened after somebody
// banked a rule would start behind the gate that rule was written to change —
// /new would quietly resurrect exactly the asking the keystroke had just
// stopped, and the person would have no way to connect the two.
//
// THE ROWS ARE RE-READ HERE RATHER THAN PUSHED, which is the whole reason this
// needs no lock: the read happens inside one closure on one keystroke and writes
// nothing that anything else can see. A pushed policy belongs to a session that
// already exists; this one is a config being assembled for a session that does
// not exist yet, and assembling it is the launch's own job done again.
//
// A REBUILD THAT FAILS KEEPS THE LAUNCH'S GATE, for the reason
// [session.Agent.SetApprovalPolicy] refuses a nil: the answer to "I could not
// read the rules" is never a session with no rules. The new conversation then
// opens exactly as it did before this function existed.
func v3CurrentGate(cfg session.Config, workspace, profileDir string, yolo bool) session.Config {
	if policy, err := v3Policy(workspace, profileDir, yolo); err == nil {
		cfg.ApprovalPolicy = policy
	}
	return cfg
}

// refreshV3Policy rebuilds the gate from the rows as they stand and pushes it
// into the running session. A rebuild that fails pushes nothing, and
// [session.Agent.SetApprovalPolicy] refuses a nil for the same reason: the safe
// answer to "I could not read the rules" is the gate that is already there.
func refreshV3Policy(agent *session.Agent, workspace, profileDir string, yolo bool) {
	if agent == nil {
		return
	}
	policy, err := v3Policy(workspace, profileDir, yolo)
	if err != nil {
		return
	}
	agent.SetApprovalPolicy(policy)
}
