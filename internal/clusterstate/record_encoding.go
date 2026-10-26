package clusterstate

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
)

const maxJSONExpansion = 6

var compressedRecordPrefix = []byte{0, 'c', 'a', 'f', 'r', 1}

// Large opaque metadata stays lossless inside the physical row envelope.
// Compression changes neither the canonical logical record nor its fields.
func encodeRecord(v any) ([]byte, error) {
	tree, plainSize, err := canonicalTree(v)
	if err != nil {
		return nil, err
	}
	if plainSize < MaxEvaluationBytes/(2*maxJSONExpansion) {
		return json.Marshal(tree)
	}
	var out bytes.Buffer
	out.Write(compressedRecordPrefix)
	compressed := gzip.NewWriter(&out)
	counted := countingRecordWriter{writer: compressed}
	if err := encodeCanonicalTreeTo(&counted, tree, false); err != nil {
		compressed.Close()
		return nil, err
	}
	if err := compressed.Close(); err != nil {
		return nil, err
	}
	if out.Len() < counted.bytes {
		return out.Bytes(), nil
	}
	return json.Marshal(tree)
}

type countingRecordWriter struct {
	writer io.Writer
	bytes  int
}

func (w *countingRecordWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.bytes += n
	return n, err
}
func recordBytes(b []byte) ([]byte, error) {
	if !bytes.HasPrefix(b, compressedRecordPrefix) {
		return b, nil
	}
	r, err := gzip.NewReader(bytes.NewReader(b[len(compressedRecordPrefix):]))
	if err != nil {
		return nil, ErrCodec
	}
	// JSON escaping can expand each legacy byte to six ASCII bytes; record framing remains small.
	limit := maxJSONExpansion*MaxV1BodyBytes + MaxCommandBytes
	out, err := io.ReadAll(io.LimitReader(r, int64(limit+1)))
	closeErr := r.Close()
	if err != nil || closeErr != nil || len(out) > limit {
		return nil, ErrCodec
	}
	return out, nil
}

// EncodeApplicationResult bounds retained response bytes without changing the
// result's logical fields. Public projection always decodes the original type.
func EncodeApplicationResult(result Result) ([]byte, error) {
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return encodeRecord(result)
}
