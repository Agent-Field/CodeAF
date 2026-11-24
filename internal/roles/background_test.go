package roles

import "testing"

// TestEveryRoleIsCrewWorkBackgroundOrMedia is the classification, written out
// once, of every role this build has a word for. A role added to the
// vocabulary without a line here fails, so whoever adds it decides whether its
// calls are a task's own work (the crew router may seat them) or a background
// seat (they follow the model the person chose).
func TestEveryRoleIsCrewWorkBackgroundOrMedia(t *testing.T) {
	want := map[Role]string{
		RolePlanner: "crew", RoleDesigner: "crew", RoleWorker: "crew",
		RoleAuditor: "crew", RoleRepair: "crew", RoleCareful: "crew",
		RoleDivision: "crew", RoleShaper: "crew", RoleMarkReader: "crew",
		RoleHandoff: "crew",

		RoleVision: "media", RoleImageGen: "media", RoleSpeech: "media", RoleVideo: "media",

		RoleTitle: "background", RoleCaption: "background", RoleTaskName: "background",
		RoleJobName: "background", RoleReflex: "background", RoleRouter: "background",
		RoleRouterConfirm: "background", RoleGuardian: "background", RoleSentinel: "background",
		RoleSpellOut: "background", RoleIntake: "background", RoleBrief: "background",
		RoleConsolidate: "background", RoleInsight: "background", RoleInsightNotes: "background",
		RoleInsightAsk: "background", RoleInsightThemes: "background",
		RoleInsightPipeline: "background", RoleInsightViews: "background",
		RoleInsightFactoryRead: "background", RoleInsightFactoryScout: "background",
	}
	for _, role := range Vocabulary() {
		class, ok := want[role]
		if !ok {
			t.Errorf("role %q has no classification: add it to crewWork (a task's own work) or to this table as background", role)
			continue
		}
		got := "background"
		switch {
		case CrewWork(role):
			got = "crew"
		case IsMedia(role):
			got = "media"
		}
		if got != class {
			t.Errorf("role %q is %s, want %s", role, got, class)
		}
	}
}

// TestABackgroundSeatNeverReadsTheCrewsRows is THE OWNER'S RULE as a test: a
// background seat answers on the model the person chose. Whatever crew tier a
// background role is registered on, and whatever the crew router left on that
// row, the ladder skips it and lands on the floor — the conversation's model.
// Only the role's own pin, or a reflex or small-work row the person wrote,
// outranks the floor.
func TestABackgroundSeatNeverReadsTheCrewsRows(t *testing.T) {
	restore(t)
	routed := settings(map[string]string{
		TierKey(TierWorker):     "router-worker-pick",
		TierKey(TierHigh):       "router-checker-pick",
		TierKey(TierMastermind): "router-planner-pick",
	})
	for _, role := range Vocabulary() {
		if !Background(role) {
			continue
		}
		for _, tier := range []Tier{TierWorker, TierHigh, TierMastermind} {
			Register(role, tier)
			got, err := Resolve(routed, role, "the-chosen-model")
			if err != nil {
				t.Fatalf("Resolve(%q on %s) error = %v", role, tier, err)
			}
			if got != "the-chosen-model" {
				t.Errorf("background role %q on the %s tier resolved %q, want the chosen model", role, tier, got)
			}
		}
	}
	// A crew role on the same rows still reads them: the router's pick is the
	// crew's business.
	Register(RolePlanner, TierMastermind)
	if got, _ := Resolve(routed, RolePlanner, "the-chosen-model"); got != "router-planner-pick" {
		t.Fatalf("planner resolved %q, want the mastermind row", got)
	}
}

// TestABackgroundSeatHonoursWhatThePersonWrote: a background role's own pin,
// and a small-work or reflex row the person wrote, are the person's choice and
// win over the floor.
func TestABackgroundSeatHonoursWhatThePersonWrote(t *testing.T) {
	restore(t)
	Register(RoleRouterConfirm, TierMastermind)
	pinned := settings(map[string]string{PinKey(RoleRouterConfirm): "pinned", TierKey(TierMastermind): "router-pick"})
	if got, _ := Resolve(pinned, RoleRouterConfirm, "chosen"); got != "pinned" {
		t.Fatalf("pinned routerconfirm = %q, want pinned", got)
	}
	written := settings(map[string]string{TierKey(TierLow): "their-small-model"})
	if got, _ := Resolve(written, RoleTitle, "chosen"); got != "their-small-model" {
		t.Fatalf("title with a written small-work row = %q, want it", got)
	}
}
