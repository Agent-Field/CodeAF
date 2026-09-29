// Package relayserve is the relay program: the blind pipe every relay has
// always been, and, when given a store directory, the two wires that keep a
// person's chats reachable from any of their machines (contract §13).
//
// It is the ONE place the real request-signing package is bound into the wires.
// The blobstore and directory packages know only the wireauth seam, so each
// builds and tests without signing; this package hands both the same
// reqsign.AuthenticateAt and so decides, once, who a request came from.
package relayserve
