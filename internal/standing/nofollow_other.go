//go:build !unix

package standing

// noFollow is nothing where the platform has no such flag; the link check
// before the open is the whole of the border there ([excerpt]).
const noFollow = 0
