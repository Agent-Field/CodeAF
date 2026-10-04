// Package rotate replaces a person's identity: the signing seed, the cell key
// and the dedup secret together. Every chat and the vault are sealed again under
// the new keys and published under the new identity, this machine switches to
// it, and the relay retires the old identity after a grace period.
//
// The work is a chain of states kept in a journal (rotation.json) that holds the
// new root before anything is sent, so a crash at any point is finished by
// running the command again and never mints a second root. Until the switch the
// old identity is the one on disk; after it the new one is; between the freeze
// and the switch the old identity cannot be written to, so there is never a
// moment when two roots are silently live.
package rotate
