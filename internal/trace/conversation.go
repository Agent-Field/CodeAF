package trace

import "context"

// KeepConversation enables one run and returns the terminal's receipt and folder.
func KeepConversation(ctx context.Context) (folder, message string) {
	folder = Dir(RunFrom(ctx))
	if folder == "" {
		return "", "this conversation has no run to record."
	}
	if Enabled() {
		return folder, "the record is already on for every conversation this codeaf holds · this one goes to " + folder
	}
	if EnabledRun(ctx) {
		return folder, "the record is already on · it goes to " + folder
	}
	if EnableRun(ctx) == "" {
		return "", "this conversation has no run to record."
	}
	return folder, "recording this conversation · it goes to " + folder
}

// ConversationRun adopts an explicit run or mints one without the process fallback.
func ConversationRun(ctx context.Context) string {
	if run := runOnContext(ctx); run != "" {
		return run
	}
	return NewRunID()
}
