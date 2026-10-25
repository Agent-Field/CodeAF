package clusterctl

import (
	"github.com/Agent-Field/codeaf/internal/clusterwire"
	"github.com/Agent-Field/codeaf/internal/raftcore"
)

const ForwardPath = clusterwire.ForwardPath
const ForwardTimeout = clusterwire.ForwardTimeout

type Forward = clusterwire.Forward
type ForwardStatus = clusterwire.ForwardStatus

func PeerStatus(s raftcore.Status) ForwardStatus { return clusterwire.PeerStatus(s) }
