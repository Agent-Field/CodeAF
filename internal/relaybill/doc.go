// Package relaybill drives the shape of a heavy hosted-relay user through the
// real client, so that what the relay bills for it can be read back from the
// Cloudflare analytics and scaled to a month.
//
// The shape is one identity with its home screen open, a chat held on one
// machine with its lease kept alive, and a number of moves of that chat: warm
// moves between two machines that already hold its objects and cold moves to a
// machine that has never seen it. Every request is made by the code the program
// ships (syncsetup's Drive and Continuer, the list read of the home screen), so
// a change in the client changes the bill this measures; nothing here speaks
// the wire by hand.
//
// The home screen's schedule is the one thing the caller supplies, because it
// lives in the terminal surface: [Schedule] is the pace the screen asks the
// directory at, and the surface's own test hands over its real one.
package relaybill
