// Package vaultsync carries the key vault with the chat: Push uploads the
// sealed vault as one store object and moves the directory's vault pointer by
// compare-and-swap, and Pull fetches, verifies and merges it, then writes the
// carried state (withheld files, credentials, provider keys) back to this machine.
//
// The withheld-file rule. Every file a chat's seals withhold because it holds a
// secret (.env and the other dotenv files anywhere in the folder, keys,
// credentials) travels as one whole-file slot per path relative to the folder:
// the exact bytes and the mode, sealed with the cell key. Merge keeps the newest
// whole file per path, a file deleted on one machine is deleted on the others by
// a tombstone, and two machines that edited different files keep both. The file
// is written back at the same path with the same bytes and mode (files.go).
//
// The carried-state rule. Two kinds of profile state ride in the vault beside
// the secrets, sealed with the cell key, pushed when they change, deleted by a
// tombstone and merged by the newer stamp exactly like a secret. credentials.json
// is one whole-file slot, stamped with the file's save time. The provider keys of
// the profile config (syncsetup) are one entry per key, like a secret's name: a key is
// stamped only when its value changed since the last capture (a local ledger of
// digests remembers), so an edit to anything else in config.json never ages a
// key; a machine with no place for a key has no opinion on it, and only a key
// this machine once shared can be tombstoned by it. Everything is captured
// before every push and pull and restored after every merge. A damaged copy is
// never captured, so it cannot bury a good one. The rest of config.json
// (budgets, rails, model picks) is never carried: those belong to the machine
// they were set on.
//
// Nothing here ever logs, returns or notifies a secret value; only names.
package vaultsync
