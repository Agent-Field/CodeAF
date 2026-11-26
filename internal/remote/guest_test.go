package remote

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

type countingGuestAgent struct {
	*fakeAgent
	mu        sync.Mutex
	stopWorks int
	questions []session.Answer
}

func (c *countingGuestAgent) StopWork() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopWorks++
	return nil
}

func (c *countingGuestAgent) ResolveQuestion(answer session.Answer) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.questions = append(c.questions, answer)
	return nil
}

// TestAWatchOnlyViewerCannotSendSteerOrAnswer verifies V11: a remote watch-only
// connection gets exactly chatlist.LiveWatchOnly(host) for Submit, FollowUp,
// Steer, Interrupt, StopWork, Consent, QuestionResolve, and Take (which
// answers and moves nothing), and the agent sees none of them.
func TestAWatchOnlyViewerCannotSendSteerOrAnswer(t *testing.T) {
	base := &fakeAgent{}
	agent := &countingGuestAgent{fakeAgent: base}
	engine := engineOn(base)
	engine.Agent = agent
	sess := NewSession(engine, true)

	// A local window holds the keyboard so we can also prove Take moves nothing.
	owner := dialSession(t, sess)
	owner.hello(Hello{Version: Version, Surface: "macbook"})

	viewer := dialSession(t, sess)
	welcomeFrame := viewer.hello(Hello{
		Version: Version,
		Surface: "viewer",
		Session: sess.engine.SessionFile,
		Join:    true,
		Remote: &RemoteViewer{
			Device: "dev-watch",
			Label:  "tablet",
			Watch:  true,
			Drive:  false,
			Road:   "relay",
		},
	})
	if welcomeFrame.Kind != "welcome" {
		t.Fatalf("watch-only handshake answered %q: %s", welcomeFrame.Kind, welcomeFrame.Error)
	}

	want := chatlist.LiveWatchOnly(HostName())
	for id, call := range []struct {
		method  string
		payload any
	}{
		{MethodSubmit, SubmitArgs{Text: "hello"}},
		{MethodFollowUp, SubmitArgs{Text: "more"}},
		{MethodSteer, SubmitArgs{Text: "change direction"}},
		{MethodInterrupt, InterruptArgs{}},
		{MethodStopWork, nil},
		{MethodConsent, ConsentArgs{ID: 1, Allow: true}},
		{MethodQuestionResolve, QuestionArgs{Answer: session.Answer{Key: "k"}}},
		{MethodTake, nil},
	} {
		result := viewer.call(uint64(id+1), call.method, call.payload)
		if result.Error != want {
			t.Fatalf("%s on watch-only viewer answered error %q, want %q", call.method, result.Error, want)
		}
	}

	if len(base.sent) != 0 {
		t.Fatalf("agent saw Submit: %v", base.sent)
	}
	if len(base.follows) != 0 {
		t.Fatalf("agent saw FollowUp: %v", base.follows)
	}
	if len(base.steered) != 0 {
		t.Fatalf("agent saw Steer: %v", base.steered)
	}
	if base.interrupts != 0 {
		t.Fatalf("agent saw %d Interrupt calls", base.interrupts)
	}
	if len(base.consents) != 0 {
		t.Fatalf("agent saw Consent: %v", base.consents)
	}
	agent.mu.Lock()
	stopWorks := agent.stopWorks
	questions := len(agent.questions)
	agent.mu.Unlock()
	if stopWorks != 0 {
		t.Fatalf("agent saw %d StopWork calls", stopWorks)
	}
	if questions != 0 {
		t.Fatalf("agent saw %d ResolveQuestion calls", questions)
	}

	// Take moved nothing: the owner still holds the keyboard and can submit.
	owner.ok(99, MethodSubmit, SubmitArgs{Text: "owner still drives"})
}

// TestEveryMethodIsRefusedToAGuestUnlessItsListSaysOtherwise verifies W3: every
// wire method constant in internal/remote is checked against the literal guest
// watch and drive allow-lists from the specification contract.
func TestEveryMethodIsRefusedToAGuestUnlessItsListSaysOtherwise(t *testing.T) {
	contractWatchMay := map[string]bool{
		// Every key of watcherReads:
		"Task.Room":          true,
		"Task.Watch":         true,
		"Question.Watch":     true,
		"PlanTaskPage":       true,
		"App.Tasks":          true,
		"App.Task":           true,
		"App.QuickTasks":     true,
		"App.ProgramActions": true,
		"Render.List":        true,
		"Render.Get":         true,
		"Detach":             true,
		"Ping":               true,
		"CheckWork":          true,
		// Plus the conversation read and observer doors:
		"Transcript.Page":  true,
		"Model":            true,
		"Usage":            true,
		"Title":            true,
		"ContextTokens":    true,
		"Held.Questions":   true,
		"Observe":          true,
		"Unobserve":        true,
		"Autonomy":         true,
		"SpendRail":        true,
		"ResolvedEffort":   true,
		"Effort":           true,
		"ResolvedApproval": true,
		"ResolveHarness":   true,
		"AttachedSkills":   true,
	}

	contractDriveMay := map[string]bool{}
	for method := range contractWatchMay {
		contractDriveMay[method] = true
	}
	for _, method := range []string{
		"Submit",
		"FollowUp",
		"Steer",
		"UnqueueFollowUp",
		"Interrupt",
		"StopWork",
		"Take",
		"Typing",
		"ResolveConsent",
		"ResolveQuestion",
	} {
		contractDriveMay[method] = true
	}

	allMethods := []struct {
		name   string
		method string
	}{
		{"MethodAnswerLaneOffer", MethodAnswerLaneOffer},
		{"MethodAppContinueInterrupted", MethodAppContinueInterrupted},
		{"MethodAppMoveToTask", MethodAppMoveToTask},
		{"MethodAppProgramActions", MethodAppProgramActions},
		{"MethodAppQuickTaskAction", MethodAppQuickTaskAction},
		{"MethodAppQuickTasks", MethodAppQuickTasks},
		{"MethodAppRename", MethodAppRename},
		{"MethodAppResumeAfterClose", MethodAppResumeAfterClose},
		{"MethodAppResumeAfterQuit", MethodAppResumeAfterQuit},
		{"MethodAppTask", MethodAppTask},
		{"MethodAppTaskAction", MethodAppTaskAction},
		{"MethodAppTaskRouting", MethodAppTaskRouting},
		{"MethodAppTaskRoutingAct", MethodAppTaskRoutingAct},
		{"MethodAppTaskRoutingOptions", MethodAppTaskRoutingOptions},
		{"MethodAppTasks", MethodAppTasks},
		{"MethodAttachSkills", MethodAttachSkills},
		{"MethodAttachedSkills", MethodAttachedSkills},
		{"MethodAutomationsActive", MethodAutomationsActive},
		{"MethodAutomationsChanges", MethodAutomationsChanges},
		{"MethodAutomationsClaim", MethodAutomationsClaim},
		{"MethodAutomationsCreate", MethodAutomationsCreate},
		{"MethodAutomationsCursor", MethodAutomationsCursor},
		{"MethodAutomationsDelete", MethodAutomationsDelete},
		{"MethodAutomationsList", MethodAutomationsList},
		{"MethodAutomationsRunNow", MethodAutomationsRunNow},
		{"MethodAutomationsRuns", MethodAutomationsRuns},
		{"MethodAutomationsSetStatus", MethodAutomationsSetStatus},
		{"MethodAutomationsStopRun", MethodAutomationsStopRun},
		{"MethodAutomationsUpdate", MethodAutomationsUpdate},
		{"MethodAutomationsWindows", MethodAutomationsWindows},
		{"MethodAutonomy", MethodAutonomy},
		{"MethodCheckWork", MethodCheckWork},
		{"MethodClaimChatStart", MethodClaimChatStart},
		{"MethodClearSkills", MethodClearSkills},
		{"MethodClientToolAnswer", MethodClientToolAnswer},
		{"MethodClientToolsSet", MethodClientToolsSet},
		{"MethodClose", MethodClose},
		{"MethodCompact", MethodCompact},
		{"MethodConnect", MethodConnect},
		{"MethodConnectKey", MethodConnectKey},
		{"MethodConsent", MethodConsent},
		{"MethodConsentRemember", MethodConsentRemember},
		{"MethodContextTokens", MethodContextTokens},
		{"MethodDelegateList", MethodDelegateList},
		{"MethodDelegateStart", MethodDelegateStart},
		{"MethodDepositFile", MethodDepositFile},
		{"MethodDesignWatch", MethodDesignWatch},
		{"MethodDetachSkill", MethodDetachSkill},
		{"MethodEarlier", MethodEarlier},
		{"MethodEffort", MethodEffort},
		{"MethodFetchFile", MethodFetchFile},
		{"MethodFollowUp", MethodFollowUp},
		{"MethodHarness", MethodHarness},
		{"MethodHeldQuestions", MethodHeldQuestions},
		{"MethodInterrupt", MethodInterrupt},
		{"MethodListDir", MethodListDir},
		{"MethodMemoryAlways", MethodMemoryAlways},
		{"MethodMemoryApplies", MethodMemoryApplies},
		{"MethodMemoryChanged", MethodMemoryChanged},
		{"MethodMemoryForget", MethodMemoryForget},
		{"MethodMemoryMemories", MethodMemoryMemories},
		{"MethodMemoryMove", MethodMemoryMove},
		{"MethodMemoryProject", MethodMemoryProject},
		{"MethodMemoryProvenance", MethodMemoryProvenance},
		{"MethodMemoryRemember", MethodMemoryRemember},
		{"MethodMemoryRememberAlways", MethodMemoryRememberAlways},
		{"MethodMemoryRestore", MethodMemoryRestore},
		{"MethodMemorySelection", MethodMemorySelection},
		{"MethodMemorySnapshot", MethodMemorySnapshot},
		{"MethodMemoryUpdate", MethodMemoryUpdate},
		{"MethodModel", MethodModel},
		{"MethodNoteConnected", MethodNoteConnected},
		{"MethodObserve", MethodObserve},
		{"MethodOpenPlanTaskWork", MethodOpenPlanTaskWork},
		{"MethodPing", MethodPing},
		{"MethodPlacesArchive", MethodPlacesArchive},
		{"MethodPlacesDelete", MethodPlacesDelete},
		{"MethodPlacesLedger", MethodPlacesLedger},
		{"MethodPlacesRefer", MethodPlacesRefer},
		{"MethodPlacesRemove", MethodPlacesRemove},
		{"MethodPlacesSearch", MethodPlacesSearch},
		{"MethodPlacesTask", MethodPlacesTask},
		{"MethodPlacesTaskDelete", MethodPlacesTaskDelete},
		{"MethodPlacesWorld", MethodPlacesWorld},
		{"MethodPlanAmend", MethodPlanAmend},
		{"MethodPlanCancel", MethodPlanCancel},
		{"MethodPlanNote", MethodPlanNote},
		{"MethodPlanPause", MethodPlanPause},
		{"MethodPlanPriority", MethodPlanPriority},
		{"MethodPlanResume", MethodPlanResume},
		{"MethodPlanRunSummary", MethodPlanRunSummary},
		{"MethodPlanSpend", MethodPlanSpend},
		{"MethodPlanTaskPage", MethodPlanTaskPage},
		{"MethodPlanTaskWork", MethodPlanTaskWork},
		{"MethodPlanTasks", MethodPlanTasks},
		{"MethodPlannerStart", MethodPlannerStart},
		{"MethodProcessActions", MethodProcessActions},
		{"MethodProcessList", MethodProcessList},
		{"MethodProcessPage", MethodProcessPage},
		{"MethodProcessRecord", MethodProcessRecord},
		{"MethodProcessRerun", MethodProcessRerun},
		{"MethodProcessRerunAt", MethodProcessRerunAt},
		{"MethodProcessStopAll", MethodProcessStopAll},
		{"MethodProcessStopAt", MethodProcessStopAt},
		{"MethodQuestionClock", MethodQuestionClock},
		{"MethodQuestionDismiss", MethodQuestionDismiss},
		{"MethodQuestionHold", MethodQuestionHold},
		{"MethodQuestionReplace", MethodQuestionReplace},
		{"MethodQuestionResolve", MethodQuestionResolve},
		{"MethodQuestionWatch", MethodQuestionWatch},
		{"MethodReasoningFor", MethodReasoningFor},
		{"MethodRefreshRunSummary", MethodRefreshRunSummary},
		{"MethodRenderCancel", MethodRenderCancel},
		{"MethodRenderGet", MethodRenderGet},
		{"MethodRenderList", MethodRenderList},
		{"MethodResolvedApproval", MethodResolvedApproval},
		{"MethodResolvedEffort", MethodResolvedEffort},
		{"MethodRewindAt", MethodRewindAt},
		{"MethodRewindPoints", MethodRewindPoints},
		{"MethodRunHarness", MethodRunHarness},
		{"MethodSessionNew", MethodSessionNew},
		{"MethodSessionOpen", MethodSessionOpen},
		{"MethodSessionsRecent", MethodSessionsRecent},
		{"MethodSetApproval", MethodSetApproval},
		{"MethodSetAutomaticModels", MethodSetAutomaticModels},
		{"MethodSetAutonomy", MethodSetAutonomy},
		{"MethodSetContext", MethodSetContext},
		{"MethodSetConversationModels", MethodSetConversationModels},
		{"MethodSetEffort", MethodSetEffort},
		{"MethodSetModel", MethodSetModel},
		{"MethodSetReasoningFor", MethodSetReasoningFor},
		{"MethodSetSpendRail", MethodSetSpendRail},
		{"MethodSetupPlan", MethodSetupPlan},
		{"MethodSideAsk", MethodSideAsk},
		{"MethodSideAskStream", MethodSideAskStream},
		{"MethodSkillFolders", MethodSkillFolders},
		{"MethodSkillShelf", MethodSkillShelf},
		{"MethodSpendRail", MethodSpendRail},
		{"MethodStatPaths", MethodStatPaths},
		{"MethodSteer", MethodSteer},
		{"MethodStopWork", MethodStopWork},
		{"MethodSubharnessResolve", MethodSubharnessResolve},
		{"MethodSubmit", MethodSubmit},
		{"MethodSubmitBash", MethodSubmitBash},
		{"MethodSubmitFiles", MethodSubmitFiles},
		{"MethodSubmitImage", MethodSubmitImage},
		{"MethodSubmitSetup", MethodSubmitSetup},
		{"MethodTake", MethodTake},
		{"MethodTaskEffort", MethodTaskEffort},
		{"MethodTaskHold", MethodTaskHold},
		{"MethodTaskModel", MethodTaskModel},
		{"MethodTaskPending", MethodTaskPending},
		{"MethodTaskRedoStronger", MethodTaskRedoStronger},
		{"MethodTaskResolve", MethodTaskResolve},
		{"MethodTaskRetry", MethodTaskRetry},
		{"MethodTaskRoom", MethodTaskRoom},
		{"MethodTaskRunHere", MethodTaskRunHere},
		{"MethodTaskSetEffort", MethodTaskSetEffort},
		{"MethodTaskSettle", MethodTaskSettle},
		{"MethodTaskStart", MethodTaskStart},
		{"MethodTaskStartOn", MethodTaskStartOn},
		{"MethodTaskSteer", MethodTaskSteer},
		{"MethodTaskStop", MethodTaskStop},
		{"MethodTaskWatch", MethodTaskWatch},
		{"MethodTaskWorkspaceApply", MethodTaskWorkspaceApply},
		{"MethodTaskWorkspaceCancel", MethodTaskWorkspaceCancel},
		{"MethodTaskWorkspaceReview", MethodTaskWorkspaceReview},
		{"MethodTeamsAcceptClosing", MethodTeamsAcceptClosing},
		{"MethodTeamsApplyDefault", MethodTeamsApplyDefault},
		{"MethodTeamsDecide", MethodTeamsDecide},
		{"MethodTeamsDefaults", MethodTeamsDefaults},
		{"MethodTeamsDelete", MethodTeamsDelete},
		{"MethodTeamsEscalate", MethodTeamsEscalate},
		{"MethodTeamsName", MethodTeamsName},
		{"MethodTeamsPackets", MethodTeamsPackets},
		{"MethodTeamsPropose", MethodTeamsPropose},
		{"MethodTeamsRaise", MethodTeamsRaise},
		{"MethodTeamsRead", MethodTeamsRead},
		{"MethodTeamsSpend", MethodTeamsSpend},
		{"MethodTeamsTraffic", MethodTeamsTraffic},
		{"MethodTeamsUpdate", MethodTeamsUpdate},
		{"MethodTeamsWrapUp", MethodTeamsWrapUp},
		{"MethodTitle", MethodTitle},
		{"MethodTitleWatch", MethodTitleWatch},
		{"MethodTranscript", MethodTranscript},
		{"MethodTranscriptPage", MethodTranscriptPage},
		{"MethodTyping", MethodTyping},
		{"MethodUnobserve", MethodUnobserve},
		{"MethodUnqueueFollowUp", MethodUnqueueFollowUp},
		{"MethodUsage", MethodUsage},
		{"MethodUseModelForNow", MethodUseModelForNow},
		// MethodDetach is placed last because it detaches the connection it runs on.
		{"MethodDetach", MethodDetach},
	}

	// Ensure every exported Method* constant in internal/remote is listed in allMethods.
	listed := make(map[string]bool, len(allMethods))
	for _, entry := range allMethods {
		listed[entry.name] = true
	}
	walkGo(t, filepath.Join(repoRoot(t), "internal", "remote"), func(_ string, file *ast.File) {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				val, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, ident := range val.Names {
					if strings.HasPrefix(ident.Name, "Method") && !listed[ident.Name] {
						t.Errorf("wire method constant %s is missing from allMethods table", ident.Name)
					}
				}
			}
		}
	})

	// Explicit contract assertions for forbidden methods.
	for _, forbidden := range []string{
		MethodTranscript,
		MethodSubmitBash,
		MethodSubmitSetup,
		MethodSubmitImage,
		MethodSubmitFiles,
		MethodSetApproval,
		MethodConsentRemember,
		MethodSessionsRecent,
		MethodSessionNew,
		MethodSessionOpen,
		MethodRewindPoints,
		MethodRewindAt,
		MethodDepositFile,
		MethodFetchFile,
		MethodListDir,
		MethodStatPaths,
		MethodClose,
		MethodSetupPlan,
		MethodPlanSpend,
		MethodPlanTasks,
		MethodOpenPlanTaskWork,
		MethodPlanTaskWork,
		MethodPlanNote,
		MethodPlanPause,
		MethodPlanResume,
		MethodPlanCancel,
		MethodPlanAmend,
		MethodPlanPriority,
		MethodPlanRunSummary,
		MethodRefreshRunSummary,
	} {
		if contractWatchMay[forbidden] || contractDriveMay[forbidden] {
			t.Fatalf("forbidden method %q is present in a guest allow-list", forbidden)
		}
	}

	agent := &fakeAgent{prefill: []session.Event{}}
	sess := heldSession(agent)
	t.Cleanup(func() { _ = agent.Close() })

	dialGuest := func(t *testing.T, label string, drive bool) *link {
		t.Helper()
		l := dialSession(t, sess)
		frame := l.hello(Hello{
			Version: Version,
			Surface: label,
			Session: sess.engine.SessionFile,
			Join:    true,
			Remote: &RemoteViewer{
				Device: "dev-" + label,
				Label:  label,
				Watch:  true,
				Drive:  drive,
				Road:   "relay",
			},
		})
		if frame.Kind != "welcome" {
			t.Fatalf("guest %s handshake answered %q: %s", label, frame.Kind, frame.Error)
		}
		return l
	}

	watchLink := dialGuest(t, "watch-guest", false)
	driveLink := dialGuest(t, "drive-guest", true)

	host := HostName()
	watchOnlySentence := chatlist.LiveWatchOnly(host)
	notAllowedSentence := chatlist.LiveNotAllowedHere(host)
	isGuestSentence := func(errText string) bool {
		return errText == watchOnlySentence || errText == notAllowedSentence
	}

	var callSeq uint64
	for _, entry := range allMethods {
		t.Run(entry.name, func(t *testing.T) {
			callSeq++
			wLink := watchLink
			dLink := driveLink
			if entry.method == MethodDetach {
				wLink = dialGuest(t, "watch-detach", false)
				dLink = dialGuest(t, "drive-detach", true)
			}

			watchRes := wLink.call(callSeq, entry.method, nil)
			if contractWatchMay[entry.method] {
				if isGuestSentence(watchRes.Error) {
					t.Fatalf("watch guest calling allowed %s (%q) was refused by guest guard: %q", entry.name, entry.method, watchRes.Error)
				}
			} else {
				want := notAllowedSentence
				if contractDriveMay[entry.method] {
					want = watchOnlySentence
				}
				if watchRes.Error != want {
					t.Fatalf("watch guest calling disallowed %s (%q) answered %q, want %q", entry.name, entry.method, watchRes.Error, want)
				}
			}

			driveRes := dLink.call(callSeq, entry.method, nil)
			if contractDriveMay[entry.method] {
				if isGuestSentence(driveRes.Error) {
					t.Fatalf("drive guest calling allowed %s (%q) was refused by guest guard: %q", entry.name, entry.method, driveRes.Error)
				}
			} else {
				if driveRes.Error != notAllowedSentence {
					t.Fatalf("drive guest calling disallowed %s (%q) answered %q, want %q", entry.name, entry.method, driveRes.Error, notAllowedSentence)
				}
			}
		})
	}
}
