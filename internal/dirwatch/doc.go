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
package dirwatch
