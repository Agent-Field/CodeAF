// Package cas stores immutable blobs by their SHA-256 digest.
//
// Fold pointers make graph reachability explicit. Garbage collection remains
// intentionally out of scope: immutable overflow is kept until a later pass
// can prove that no journal event refers to it.
package cas
