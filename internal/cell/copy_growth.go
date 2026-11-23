package cell

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/upgradenotice"
)

type copySource struct {
	Original string `json:"original"`
	Bytes    int64  `json:"bytes"`
	Head     string `json:"head"`
	Turns    int    `json:"turns"`
	Known    bool   `json:"known"`
}

// readCopySource counts person turns, rather than replies or tool messages.
// Only complete, readable lines can establish a baseline for a later warning.
func readCopySource(path string, length int64) (copySource, error) {
	f, err := os.Open(path)
	if err != nil {
		return copySource{}, err
	}
	defer f.Close()
	if length < 0 {
		info, err := f.Stat()
		if err != nil {
			return copySource{}, err
		}
		length = info.Size()
	}
	sum := sha256.New()
	reader := bufio.NewReader(io.TeeReader(io.LimitReader(f, length), sum))
	stamp := copySource{Bytes: length, Known: true}
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF && len(line) == 0 {
			break
		}
		if err != nil {
			stamp.Known = false
			break
		}
		var entry struct {
			Type string `json:"type"`
			Role string `json:"role"`
		}
		if json.Unmarshal(line, &entry) != nil {
			stamp.Known = false
			continue
		}
		if entry.Type == "message" && entry.Role == "user" {
			stamp.Turns++
		}
	}
	stamp.Head = hex.EncodeToString(sum.Sum(nil))
	return stamp, nil
}

// OlderTurnsNotice uses only the source recorded at copy time. An old empty
// marker has no trustworthy imported count, so it cannot justify a warning.
func OlderTurnsNotice(root string, interactive bool) (string, error) {
	if !interactive || root == "" {
		return "", nil
	}
	raw, err := os.ReadFile(filepath.Join(root, CopyReadyPath))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var baseline copySource
	if json.Unmarshal(raw, &baseline) != nil || !baseline.Known || baseline.Original == "" || baseline.Bytes <= 0 {
		return "", nil
	}
	source := filepath.Join(baseline.Original, legacyTranscript)
	info, err := os.Stat(source)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if info.Size() <= baseline.Bytes {
		return "", nil
	}
	prefix, err := readCopySource(source, baseline.Bytes)
	if err != nil {
		return "", err
	}
	if !prefix.Known || prefix.Head != baseline.Head {
		return "", nil
	}
	current, err := readCopySource(source, info.Size())
	if err != nil {
		return "", err
	}
	count := current.Turns - baseline.Turns
	if !current.Known || count <= 0 {
		return "", nil
	}
	noun, verb := "turns", "are"
	if count == 1 {
		noun, verb = "turn", "is"
	}
	line := fmt.Sprintf("%d %s typed in an older codeaf %s kept in %s and %s not shown here.", count, noun, verb, upgradenotice.Path(source), verb)
	report := fmt.Sprintf("%d turns typed in an older codeaf after this copy was made.\nOriginal transcript: %s\nLive copy: %s\nThese turns have not been merged into the copy. The original is unchanged.\n", count, source, root)
	return upgradenotice.Once(filepath.Join(root, StateDir, "older-turns-noticed-v1.json"), fmt.Sprint(current.Turns), filepath.Join(root, StateDir, "older-turns-report.txt"), []byte(report), line)
}
