package cellstore

// sealOp is the turn-end seal: attach the folder if the store has not seen it
// and snapshot it, labelled with the receipt id.
type sealOp struct {
	Turn string `json:"turn"`
	// Changed lists the paths to visit; nil walks the tree.
	Changed []string `json:"changed"`
}

func (sealOp) Verb() string { return "seal" }

func (o sealOp) Args() []string {
	args := []string{"--json", "hook", "turn-end", "--turn", o.Turn}
	for _, p := range o.Changed {
		args = append(args, "--changed", p)
	}
	return args
}

// snapOp is a labelled snapshot that is not a turn.
type snapOp struct {
	Message string `json:"message"`
}

func (snapOp) Verb() string { return "snap" }

func (o snapOp) Args() []string { return []string{"--json", "snap", "-m", o.Message} }

// restoreOp writes a snapshot back, byte for byte.
type restoreOp struct {
	Snapshot string `json:"snapshot"`
	// Paths limits the restore to these top-level paths; empty is the tree.
	Paths []string `json:"paths,omitempty"`
}

func (restoreOp) Verb() string { return "restore" }

func (o restoreOp) Args() []string {
	args := []string{"--json", "rewind", o.Snapshot, "--yes"}
	for _, p := range o.Paths {
		args = append(args, "--paths="+p)
	}
	return args
}
