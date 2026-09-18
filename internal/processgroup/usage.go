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
// THE READING IS /proc, WHICH IS LINUX'S. A platform without it answers "cannot
// say" rather than a guess, and a bound that cannot say never cuts — the same
// silence rule the admission governor keeps (session's task_pressure.go): a
// person on a machine this cannot measure gets exactly the scheduler they had
// before the bound existed.

// Usage is what one job subtree is using at one instant.
type Usage struct {
	// Processes is how many processes are alive in the subtree.
	Processes int
	// CPUSeconds is the subtree's cumulative CPU time in seconds, summed over
	// every process in it. Subtract two readings and divide by the wall between
	// them to get the cores the subtree held.
	CPUSeconds float64
}
