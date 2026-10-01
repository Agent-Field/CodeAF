// Package dirwatch follows the directory's change socket, so a screen that
// shows the directory learns of another device's change when it happens rather
// than by asking on a timer.
//
// The socket carries one fact: the directory's version. It never carries
// records, so a frame is only ever a reason to read the list once. This package
// owns the connection (dialling, keeping it alive, noticing it died, coming
// back with jittered waits) and nothing else; what to do with a new version,
// and what to do while the socket is down, stay with the surface that draws
// the list.
//
// One connection is kept per identity per process, however many surfaces
// follow it, and it exists only while somebody follows.
//
// # Presence and joined devices
//
// A socket opened with events (directory.HTTP.Watch does; WatchHolding does
// not) also hears event frames (docs/ux-pairing-contract.md, section 5), and
// the follower's State shows them to a surface:
//
//   - State.Online lists, sorted, the other devices that hold a watch socket
//     now. A device is online while it holds at least one socket; the relay
//     tells that it is offline 15 s after its last socket closed, and says
//     nothing when it reconnects inside that gap. The set is empty while
//     State.Up is false, because the relay names who is online only when a
//     socket opens. A surface draws "● spark" for a name in Online and
//     "○ dumb" otherwise, and reads the last time a device was seen from the
//     device record's last_seen.
//   - State.Joined lists the newest MaxJoined device-joined events, oldest
//     first, each with a Seq that only rises. A surface remembers the newest
//     Seq it showed and shows every higher one once ("<name> joined your
//     fleet"); Name is sealed under the metadata key and is opened by the
//     surface. Joined survives a reconnect.
//
// A surface waits on Follower.Changes as before and reads State; an Online or
// Joined change is a change like a new version. A revoked event removes the
// device from Online; the list read the version bump calls for says why.
// Frames of a kind this package does not know are ignored.
package dirwatch
