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

// Large legacy proofs use bounded proposals without changing their signed body.
func v1Commands(d *decoder) ([]byte, error) {
	cluster, now, raw, body := d.str(), d.u64(), d.blob(), d.blob()
	if d.end() != nil || now > clusterstate.MaxSafeInteger {
		return nil, errFrame
	}
	var proof clusterstate.V1Request
	if err := clusterstate.Decode(raw, &proof); err != nil {
		return nil, err
	}
	if proof.Body != nil || len(body) > clusterstate.MaxV1BodyBytes {
		return nil, errFrame
	}
	proof.Body = body
	commands, err := clusterstate.V1RequestCommands(cluster, int64(now), proof)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.u32(uint32(len(commands)))
	for _, c := range commands {
		b, err := clusterstate.EncodeCommand(c)
		if err != nil {
			return nil, err
		}
		e.blob(b)
	}
	return e.b, nil
}
