package clusterstate

// Peer quotas bound expendable routing independently of retained application
// records. Both relay runtimes derive their admission and wire limits here.
const (
	MaxPeerMessages           = 64
	MaxPeerQueuedBytes        = 1 << 20
	PeerRetentionMillis int64 = 30_000
)
