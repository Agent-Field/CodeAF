// Package cellsync moves a cell between a device and the shared store and
// directory: it exports the engine's new objects, uploads them, and only then
// moves the directory head. Nothing here waits on the network from a seal.
package cellsync
