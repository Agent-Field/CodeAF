// Package blobstore is the content-addressed seam between a device and the
// place its encrypted objects rest. A store is told only two things: put this
// frame, and give me the object with this remote id. It never learns a path,
// a name or a plaintext byte, so every implementation, from the in-memory fake
// to the disk and the network, can be swapped without the caller noticing.
//
// The package imports nothing but the standard library (contract law L0), so
// the relay can link it without pulling in a device.
package blobstore
