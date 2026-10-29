package processgroup

// A job subtree is a process group, and a process group is addressable — #1155
// recorded every group at launch with its leader's identity precisely so it
// could be named later. That same handle is what a bound on a subtree's
// resource use needs: the leader's pid is the root of the tree, and the tree is
// exactly what the job started.
//
// WHAT THIS MEASURES, AND IN WHAT UNIT. Two things, because a bound in this
// code is stated in cores and processes and both have to be readable together:
//
//   - Processes is how many processes are alive in the subtree at this instant.
//   - CPUSeconds is the subtree's CUMULATIVE processor time (user plus system)
//     over every process in it. A cumulative figure is what a RATE is taken
//     from: the difference of two readings over the wall between them is the
//     cores the subtree held, which is the reading this product bounds. One
//     reading on its own is not a rate and must not be read as one.
//
// Linux reads /proc and macOS reads the kernel's process-info interface. Other
// platforms answer "cannot say" rather than guess, and a bound that cannot say
// never cuts, preserving the scheduler the person had before the bound existed.

// Usage is what one job subtree is using at one instant.
type Usage struct {
	// Processes is how many processes are alive in the subtree.
	Processes int
	// CPUSeconds is the subtree's cumulative CPU time in seconds, summed over
	// every process in it. Subtract two readings and divide by the wall between
	// them to get the cores the subtree held.
	CPUSeconds float64
	// CPUPartial says CPUSeconds left out part of the tree: the walk stopped at
	// its limit, a listing was truncated, or a process could not be read. Such
	// a sum is not comparable with another reading, because a process missing
	// from one and present in the next would read as a burst of its whole
	// lifetime's CPU, so no rate may be taken from it. Processes is still a
	// lower bound.
	CPUPartial bool
}
