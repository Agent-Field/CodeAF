// Package cas stores immutable blobs by their SHA-256 digest.
//
// The store intentionally has no garbage collector in this milestone.
// Reachability-based garbage collection arrives with fold integration, when
// the graph can authoritatively identify which blobs are still live.
package cas
