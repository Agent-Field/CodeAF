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
// THE CARRIED-STATE LAW. Credentials, provider keys, profile preferences,
// shared memories and portable teams ride beside secrets. Whole files use
// their save time; Keyed gives individual fields independent clocks and
// deletion tombstones. Only a key this machine once shared can be removed by
// it. A guarded apply preserves edits made during a network round. A damaged
// copy is neither captured nor overwritten.
//
// ONE TRAFFIC VALUE PER WRITING DEVICE AND TEAM. DeviceWindows replaces a
// writer's bounded window without republishing received history. The source
// owns the limits (internal/teams): 32 lines and 16 KiB per team, text shortened
// past 2 KiB, 64 KiB across a device's teams, publication at most every 30
// seconds. Team records are never rate-limited. Removing a team's own value
// happens on the next round; no writer deletes another writer's value. A
// receiving source replaces its display mirror from locally known paired
// writers, or every other writer when no local roster is available. Optional
// team sharing uses BestEffort so its failures cannot block other carried state.
//
// Nothing here ever logs, returns or notifies a secret value; only names.
package vaultsync
