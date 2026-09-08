package config

import "strings"

// The conversation model, written down.
//
// Every other model slot on the sheet resolves from somewhere a later launch
// can read again — an environment variable, a row in this file — and the talk
// slot did not. It was a LIVE SEAM and only that: the picker swapped the model
// on the running agent, the sheet's row read it back off the same agent, and
// the next launch opened on AFORGE_MODEL or the built-in default as though
// nobody had chosen anything. A person who picked a model, worked in it for an
// hour and restarted found themselves back on a model they had deliberately
// left.
//
// So the choice lands here, and the v3 door reads it back before it opens
// (cmd/aforge's chatv3.go). The whole rule is three lines below and the
// interesting one is that a SAVED CHOICE BEATS AFORGE_MODEL — which is the
// registry's own law for this row rather than a new one. The talk slot carries
// AFORGE_MODEL as an [Setting.EnvDefault] and not an [Setting.Env]: the
// variable seeds a value nobody has chosen and never freezes the row, so the
// sheet lets you change it while the variable is set. A launch that let the
// variable win would make that edit revert on the next start, silently, which
// is the same complaint this file exists to answer.

// KeyChatModel is where that choice is written in the profile's config.json.
//
// It is the SETTINGS ROW'S OWN KEY — [ModelSettingKey] for the talk slot —
// rather than a name of this file's invention, so if the registry ever persists
// its model rows itself the two halves land on one key instead of two. It is
// spelled as a constant because a key is a constant; [TestChatModelKeyIsTheRow]
// pins the two spellings together.
const KeyChatModel = "model.talk"

// ChatModelAt is the conversation model this profile last settled on, or empty
// when nobody has chosen one. Empty is not a failure and never a model name:
// the caller falls through to its own default, which is what an unconfigured
// install has always opened on.
func ChatModelAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyChatModel); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// WriteChatModel records the choice. It goes through the one writer every
// persisted row goes through, so the rest of the file — the gate, the rail,
// somebody else's rows — survives untouched (budget.go's writeProfileValue).
func WriteChatModel(profileDir, slug string) error {
	return writeProfileValue(profileDir, KeyChatModel, strings.TrimSpace(slug))
}

// FirstPrompt reports whether this profile has never chosen a conversation
// model. A fresh install's first typed line is this: [ChatModelAt] is empty
// and the talk slot is still the build default. The hedge uses it so a stall
// there names `/model` instead of sitting silent until the ninety-second
// first-token cut (F42).
func FirstPrompt(profileDir string) bool {
	return ChatModelAt(profileDir) == ""
}
