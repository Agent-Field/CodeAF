package raftabi

import "github.com/Agent-Field/codeaf/internal/clusterstate"

func v1Command(d *decoder) ([]byte, error) {
	cluster, now, raw := d.str(), d.u64(), d.blob()
	if d.end() != nil || now > clusterstate.MaxSafeInteger {
		return nil, errFrame
	}
	var proof clusterstate.V1Request
	if err := clusterstate.Decode(raw, &proof); err != nil {
		return nil, err
	}
	c, err := clusterstate.V1Command(cluster, int64(now), proof)
	if err != nil {
		return nil, err
	}
	command, err := clusterstate.EncodeCommand(c)
	if err != nil {
		return nil, err
	}
	if len(command) > clusterstate.MaxCommandBytes {
		return nil, errFrame
	}
	e := encoder{}
	e.blob(command)
	return e.b, nil
}
