package remote

// browse.go is the client half of what the browse page asks of the engine's
// disk. Two of the three are read-only questions — what is in this directory
// (MethodListDir), and which of these words are real files (MethodStatPaths) —
// and the third is the page's one write (MethodDepositFile), which lands a file
// in the session's attachments and nowhere else. The server halves and their
// law live in file.go beside handOver, because they are the same boundary:
// nothing outside the workspace and the session's own folder crosses, and
// nothing this side sends chooses a directory on that machine.

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

// DepositFile puts one file in the far session's attachments folder WITHOUT
// saying anything: the browse page's drag-drop lane. It answers with the path
// the bytes landed at, on the ENGINE's disk.
//
// THE NAME CROSSES AS THE PAGE GAVE IT AND IS JUDGED OVER THERE. That is the
// one place this parts from [Agent.SubmitFiles], which reduces a path to a name
// on this side because the person typed it here on a machine that knows what
// its own separator is. This name came off a browser upload, so there is no
// local knowledge to apply to it and nothing to gain by pre-empting the
// boundary: the engine refuses a name that is a path ([attachmentName]), and
// the refusal is the engine's sentence with nothing softening it, exactly as
// [Client.FetchFile]'s is.
func (c *Client) DepositFile(name, mime string, data []byte) (string, error) {
	payload, err := c.call(nil, MethodDepositFile, WireFile{Name: name, MIME: mime, Bytes: data})
	if err != nil {
		return "", err
	}
	var landed DepositedFile
	if err := json.Unmarshal(payload, &landed); err != nil {
		return "", err
	}
	return landed.Path, nil
}
