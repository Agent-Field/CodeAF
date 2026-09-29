// Package vaultsync carries the key vault with the chat: Push uploads the
// sealed vault as one store object and moves the directory's vault pointer by
// compare-and-swap, Pull fetches, verifies and merges it, and Inject writes a
// cell's secrets into its workspace as .env when the cell is taken over.
//
// The .env rule. A .env the person already has is theirs. Inject never
// rewrites or removes a line of it. It appends only the names the file does not
// set, each on its own line after the person's text, in the vault's stable name
// order. A name the file already sets to the vault's value is in sync and left
// alone silently; a name it sets to another value is skipped and reported (by
// name only) through Syncer.Notify. When nothing is missing the file is not
// touched. A new file is mode 0600.
//
// The carried-state rule. Two kinds of profile state ride in the vault beside
// the secrets, sealed with the cell key, pushed when they change, deleted by a
// tombstone and merged by the newer stamp exactly like a secret. credentials.json
// is one whole-file slot, stamped with the file's save time. The provider keys of
// the profile config (syncsetup) are one entry per key, like .env names: a key is
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
