package remote

// browse.go is the client half of the two read-only questions a surface may
// ask about the engine's disk: what is in this directory (MethodListDir), and
// which of these words are real files (MethodStatPaths). The server half and
// its law live in file.go beside handOver, because they are the same boundary:
// nothing outside the workspace and the session's own folder crosses.

import "encoding/json"

// ListDir asks the engine for one directory, by a path on the ENGINE's disk.
// The path is never resolved here, for the reason [Client.FetchFile] gives.
// The error is the engine's sentence and nothing softens it.
func (c *Client) ListDir(path string) (DirListing, error) {
	payload, err := c.call(nil, MethodListDir, ListDirArgs{Path: path})
	if err != nil {
		return DirListing{}, err
	}
	var listing DirListing
	if err := json.Unmarshal(payload, &listing); err != nil {
		return DirListing{}, err
	}
	return listing, nil
}

// StatPaths asks the engine which of these candidate paths exist under its
// two-roots law. One call per burst of new rows — the batching is the whole
// reason this is not a per-word round trip.
func (c *Client) StatPaths(paths []string) ([]PathFact, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	payload, err := c.call(nil, MethodStatPaths, StatPathsArgs{Paths: paths})
	if err != nil {
		return nil, err
	}
	var facts []PathFact
	if err := json.Unmarshal(payload, &facts); err != nil {
		return nil, err
	}
	return facts, nil
}
