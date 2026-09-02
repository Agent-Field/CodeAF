package config

import "strings"

// Credentials is every credential this profile is configured with, in the exact
// values a call would carry — the key the models are talked to with, the search
// keys, an application secret somebody pasted for their own Google registration.
//
// IT EXISTS SO THAT A RECORD OF A RUN CAN PROMISE A PERSON THEIR KEY IS NOT IN
// IT. The debug record (internal/trace) redacts credentials by shape —
// `Bearer …`, an `sk-…` token, a field spelled `authorization` — and a shape is
// only ever the keys somebody thought of: a Google `AIza…`, a Groq `gsk_…` or a
// self-hosted endpoint's plain token would land verbatim in a folder a person
// is about to attach to a bug report. The exact values are the only thing that
// catches a key whose shape nobody has seen, and this is where a process asks
// for them.
//
// THE SETTINGS REGISTRY IS THE SOURCE, and this walks it rather than keeping a
// second list: a row that marks itself `Secret` is a credential, and a
// credential row added later is covered here the day it is added, with no
// second place to remember. Values are read the way every one of those rows
// reads them — the row's own environment variable first, then the profile file
// — and the empty ones are left out, so an unconfigured machine registers
// nothing and every record stays exactly as it was written.
func Credentials(profileDir string) []string {
	registry := NewSettings(SettingsOptions{ProfileDir: profileDir})
	var found []string
	add := func(value string) {
		if value = strings.TrimSpace(value); value == "" {
			return
		}
		for _, known := range found {
			if known == value {
				return
			}
		}
		found = append(found, value)
	}
	for _, row := range registry.Rows() {
		if !row.Secret {
			continue
		}
		add(credentialAt(profileDir, row.Env, row.Key))
	}
	// AND THE KEY'S SECOND SPELLING. The model key resolves from two variables
	// and not one ([APIKeyAt]) — the row names only the first — so a person
	// running with OPENAI_API_KEY set would have registered nothing at all.
	add(APIKeyAt(profileDir))
	return found
}
