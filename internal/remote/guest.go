package remote

import "github.com/Agent-Field/codeaf/internal/chatlist"

// HostName returns this machine's own short name for guest refusal sentences.
// It defaults to [MachineName] so the holder and every refusal sentence name
// the computer the same way the window bar does.
var HostName = MachineName

// guestWatchMay is the complete allow-list of wire methods a watch-only remote
// viewer may call. Everything not on this list is refused by default so a
// method added later cannot leak host files or mutate a conversation unless it
// is explicitly granted here.
var guestWatchMay = map[string]bool{
	MethodTaskRoom:          true,
	MethodTaskWatch:         true,
	MethodQuestionWatch:     true,
	MethodPlanTaskPage:      true,
	MethodAppTasks:          true,
	MethodAppTask:           true,
	MethodAppQuickTasks:     true,
	MethodAppProgramActions: true,
	MethodRenderList:        true,
	MethodRenderGet:         true,
	MethodDetach:            true,
	MethodPing:              true,
	MethodCheckWork:         true,
	MethodTranscriptPage:    true,
	MethodModel:             true,
	MethodUsage:             true,
	MethodTitle:             true,
	MethodContextTokens:     true,
	MethodHeldQuestions:     true,
	MethodObserve:           true,
	MethodUnobserve:         true,
	MethodAutonomy:          true,
	MethodSpendRail:         true,
	MethodResolvedEffort:    true,
	MethodEffort:            true,
	MethodResolvedApproval:  true,
	MethodHarness:           true,
	MethodAttachedSkills:    true,
}

// guestDriveMay is the complete allow-list of wire methods a drive-permitted
// remote viewer may call. It extends [guestWatchMay] with the conversation
// input, stop, keyboard, and one-turn approval doors while keeping host shell,
// file, posture, and session lifecycle doors closed.
var guestDriveMay = func() map[string]bool {
	allowed := make(map[string]bool, len(guestWatchMay)+10)
	for method := range guestWatchMay {
		allowed[method] = true
	}
	for _, method := range []string{
		MethodSubmit,
		MethodFollowUp,
		MethodSteer,
		MethodUnqueueFollowUp,
		MethodInterrupt,
		MethodStopWork,
		MethodTake,
		MethodTyping,
		MethodConsent,
		MethodQuestionResolve,
	} {
		allowed[method] = true
	}
	return allowed
}()

// GuestMay reports whether a remote viewer with the given drive grant is
// permitted to invoke method on the holder.
func GuestMay(method string, drive bool) bool {
	if drive {
		return guestDriveMay[method]
	}
	return guestWatchMay[method]
}

func guestMay(method string, drive bool) bool {
	return GuestMay(method, drive)
}

// GuestRefusal returns the person-facing sentence explaining why method is
// refused to a remote viewer on host, or the empty string when the allow-list
// permits the call.
func GuestRefusal(host, method string, drive bool) string {
	if GuestMay(method, drive) {
		return ""
	}
	if !drive && guestDriveMay[method] {
		return chatlist.LiveWatchOnly(host)
	}
	return chatlist.LiveNotAllowedHere(host)
}
