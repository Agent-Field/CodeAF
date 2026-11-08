package serve

// PullLooks is how many looks pulls made, each decoding every job.
func PullLooks() int64 { return pullLooks.Load() }

// Settles is how many settles ran, each decoding every job.
func Settles() int64 { return settles.Load() }

// WorkerSettleEvery is how often a runner's requests settle the floor.
const WorkerSettleEvery = workerSettleEvery
