// Package parity runs the organiser against the owner's saved embeddings and
// writes the comparison. The corpus itself is not in this repository: tests
// read PLACEORG_REF and skip when it is unset. A small synthetic fixture under
// testdata/ exercises the same scoring without that corpus.
//
// The comparison is structure, not names. Cluster ids are arbitrary labels, so
// the score is the adjusted Rand index at each level of the tree the audit
// left, which is what the reference's root, mid and leaf columns describe.
// The kNN graph is the one the reference built. Chat text is not in the
// reference directory, so the neighbour stage cannot rebuild the anchor
// boosts that graph carries; its own package checks the edge set.
package parity
