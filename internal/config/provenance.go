package config

// Provenance: the one question the registry could answer and did not.
//
// A surface that shows a value owes the reader where the value came from —
// 5.20's honesty duty applied to a settings sheet. Two thirds of that answer
// already exist here: [Setting.PinnedBy] names an environment pin, and every
// row that is neither pinned nor written down is showing a built-in default.
// The missing third is "is this one written down?", and only this package can
// answer it, because only this package knows which file the row reads out of
// and how a malformed entry degrades. A render surface that opened
// config.json for itself would be a second reader of a format this package
// owns — the mirror of the second-writer mistake, and just as certain to
// drift.

// PersistedKeys lists the rows whose current value is written down in the
// profile's config.json rather than resolved from a built-in default. The
// order is the registry's own row order, so a caller rendering in that order
// can walk both lists together.
//
// A row fronting a store this registry does not own — anything with a
// [Setting.PrefsField], meaning the model slots and the chat divider, which
// live beside the graph — is never listed, even if a hand-edited config.json
// happens to carry a key by that name. Absence from THIS file is not evidence
// about another one, and a provenance chip that guessed would be exactly the
// estimate 10.2.8 bans.
//
// An unreadable or absent config.json returns no keys: a file that is not
// there yet is a profile with nothing written down, which is the truth.
func (s *Settings) PersistedKeys() []string {
	values, err := readProfileConfig(s.options.ProfileDir)
	if err != nil || len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.rows))
	for _, row := range s.rows {
		if row.PrefsField != "" {
			continue
		}
		if _, ok := values[row.Key]; ok {
			keys = append(keys, row.Key)
		}
	}
	return keys
}
