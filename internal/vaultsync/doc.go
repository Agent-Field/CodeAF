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
// The credentials rule. credentials.json rides in the vault as one reserved
// slot, so it is sealed with the cell key, pushed when it changes, deleted by a
// tombstone and merged by the newer slot stamp exactly like a secret: the last
// machine to save it wins whole, and a delete beats an older copy. The file is
// captured into the slot before every push and pull, stamped with the file's
// modification time so a late look never beats a newer edit sent by another
// machine, and restored from the slot
// after every merge. A file that is not valid JSON is never captured, so a
// damaged copy cannot bury a good one, and it is set aside, not overwritten,
// when a good copy arrives. config.json (budgets, rails) is never carried: those
// limits belong to the machine they were set on.
//
// Nothing here ever logs, returns or notifies a secret value; only names.
package vaultsync
