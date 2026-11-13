package clusterstate

// Retry headers include a hashed key, digest, counters and a small result.
// Larger response payloads still consume the identity's content allowance.
const sessionBookkeepingBytes = 512

// CoordinationReserveBytes bounds retry headers for all retained sessions and
// their count row, plus one bounded membership-initialization transaction.
// The live-session and member limits remain independent admission checks.
const CoordinationReserveBytes = (MaxRetainedSessions+1)*sessionBookkeepingBytes + MaxEvaluationBytes

func bookkeepingCost(k RecordKey, b []byte) uint64 {
	if b == nil {
		return 0
	}
	return min(RecordBytes(k, b), sessionBookkeepingBytes)
}

// ApplicationBookkeeping reads the same bounded accounting for native apply,
// hosted storage and streamed snapshots. It never grants content capacity.
func ApplicationBookkeeping(raw []byte) (uint64, error) {
	if raw == nil {
		return 0, nil
	}
	var counts sessionCounts
	if Decode(raw, &counts) != nil || counts.Live > MaxLiveSessions || counts.Live > counts.Retained || counts.Retained > MaxRetainedSessions || counts.Bookkeeping > uint64(counts.Retained+1)*sessionBookkeepingBytes || counts.Initialization > MaxEvaluationBytes {
		return 0, ErrDependency
	}
	return counts.Bookkeeping + counts.Initialization, nil
}
