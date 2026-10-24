package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftabi"
)

// rowProof keeps the oversized-row memory fixture in a bounded native driver.
// The Worker still evaluates every row and its actual heap is sampled without
// forcing collection. Keeping HTTP/JSON copies out of the Node coordinator is
// necessary for the combined RSS budget, not an alternative application rule.
func rowProof(origin string, result io.Writer) error {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "" {
		return errors.New("row proof requires the local workerd origin")
	}
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 1, Proxy: nil}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	request := func(method, path string, b []byte) ([]byte, error) {
		req, err := http.NewRequest(method, origin+path, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			return nil, errors.New("row proof HTTP refusal")
		}
		out, err := io.ReadAll(io.LimitReader(resp.Body, raftabi.MaxBuffer+1))
		if len(out) > raftabi.MaxBuffer {
			return nil, errors.New("row proof oversized response")
		}
		return out, err
	}
	native := raftabi.New()
	check := func(op uint32, blobs ...[]byte) ([]byte, error) {
		var b bytes.Buffer
		binary.Write(&b, binary.LittleEndian, raftabi.Version)
		binary.Write(&b, binary.LittleEndian, op)
		for _, blob := range blobs {
			binary.Write(&b, binary.LittleEndian, uint32(len(blob)))
			b.Write(blob)
		}
		expected := native.Call(b.Bytes())
		actual, err := request("POST", "/call", b.Bytes())
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(actual, expected) {
			return nil, errors.New("R14 row ABI differs between native and wasm")
		}
		r := bytes.NewReader(actual)
		var version, n uint32
		if binary.Read(r, binary.LittleEndian, &version) != nil || version != raftabi.Version || binary.Read(r, binary.LittleEndian, &n) != nil || n != 0 {
			return nil, errors.New("R14 row ABI refused")
		}
		if binary.Read(r, binary.LittleEndian, &n) != nil || uint64(n) != uint64(r.Len()) {
			return nil, errors.New("R14 row ABI invalid response")
		}
		return io.ReadAll(r)
	}
	const rowCount = 129
	var hashes [rowCount][32]byte
	original, restored := sha256.New(), sha256.New()
	total, maxRow := 0, 0
	for i := 0; i < rowCount; i++ {
		size := 64 << 10
		if i == 0 {
			size = 768 << 10
		}
		cell := clusterstate.Created(clusterstate.CellInit{Head: "opaque-head", Class: "work", Size: 1, Title: strings.Repeat("A", size), Keys: map[string]map[string]string{"key": {"recipient": "opaque-wrap"}}}, "device-a", 1000)
		row, err := clusterstate.EncodeRow(clusterstate.Row{Schema: clusterstate.SchemaVersion, Key: fmt.Sprintf("legacy-%d", i), Cell: &cell})
		if err != nil {
			return err
		}
		if len(row) > 1<<20 {
			return errors.New("R27 row exceeds legacy envelope")
		}
		total += len(row)
		maxRow = max(maxRow, len(row))
		hashes[i] = sha256.Sum256(row)
		original.Write(row)
		got, err := check(5, row)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, row) {
			return errors.New("R28 opaque row changed")
		}
		if _, err := request("PUT", fmt.Sprintf("/store/legacy/row-%d", i), got); err != nil {
			return err
		}
	}
	if total <= 8<<20 {
		return errors.New("R27 fixture must exceed default admission ceiling")
	}
	for i := 0; i < rowCount; i++ {
		row, err := request("GET", fmt.Sprintf("/store/legacy/row-%d", i), nil)
		if err != nil {
			return err
		}
		if sha256.Sum256(row) != hashes[i] {
			return errors.New("R28 retained row differs")
		}
		restored.Write(row)
	}
	digest := original.Sum(nil)
	if !bytes.Equal(digest, restored.Sum(nil)) {
		return errors.New("R28 logical fixture content differs")
	}
	row, err := request("GET", "/store/legacy/row-0", nil)
	if err != nil {
		return err
	}
	command, err := clusterstate.EncodeCommand(clusterstate.Command{Schema: 1, Cluster: "p03-cluster", Actor: "device-a", Session: "proof", Sequence: 2, RequestDigest: "opaque-digest", Now: 2000, Signature: []byte{1, 2, 3}, Change: clusterstate.Mutation{Release: &clusterstate.ReleaseCell{Cell: "legacy-0", Fence: 1}}})
	if err != nil {
		return err
	}
	next, err := check(4, command, row)
	if err != nil {
		return err
	}
	decoded, err := clusterstate.DecodeRow(next)
	if err != nil {
		return err
	}
	if decoded.Cell.Lease.Expires != 0 || decoded.Cell.Title != strings.Repeat("A", 768<<10) {
		return errors.New("R28 large row release lost state")
	}
	return json.NewEncoder(result).Encode(map[string]any{"rows": rowCount, "bytes": total, "ceiling": max(8<<20, total), "maxRowBytes": maxRow, "digest": hex.EncodeToString(digest), "representation": "paged immutable single rows; host inventory; grandfathered ceiling; streamed snapshots remain P05"})
}
