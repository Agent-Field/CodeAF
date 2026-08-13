package store

import (
	"io"
	"os"
	"strings"
)

// The edge between two nodes carries the work, not a report about the work.
//
// It did not. A settled node's row holds its final message, and a worker that
// wrote a file says so in one sentence — "The evaluation is complete. The file
// is at /w/job/07-vendors.md" — because that IS the honest final message for a
// leaf whose deliverable is a document. The dependency edge then handed the
// consumer that sentence and told it, in the consumer's own brief, that it held
// "results from earlier work, which you already have and must not gather
// again". The consumer was told it held the material, discovered it held a
// path, and went and got the material: measured over one twelve-node report
// job, nineteen turns of filesystem archaeology across four nodes, tool reads
// of 4.7–6.3× the material that actually mattered, and one node that wandered
// into a previous run's artifacts and admitted a seventh vendor into a
// six-vendor evaluation.
//
// So the digest carries the product. Where a producer left files, their text is
// read back and travels on the edge under exactly the budget that was already
// there — the pot, the per-dependency share, the clip note, the spill to the
// content-addressed store — which is machinery that was correct and, until
// this, never once exercised, because nothing large enough to bite it ever
// reached it.
//
// Where a producer left no files, its summary is still the product: a leaf that
// answered in prose answered in its final message, and there is nothing else of
// it to carry. That is the whole test, and it is structural — did this node
// record artifacts — never a reading of what the summary says.

// productReadFloor is the least one artifact may be read with, in bytes. Below
// roughly this, inlining a file's opening lines is worse than naming it: the
// consumer gets a fragment it cannot use and a path it now distrusts.
const productReadFloor = 1 << 10

// artifactBlockOverhead is what one inlined file costs beyond its own bytes:
// the rule above it, the path, the newlines. It is charged against the read
// budget so a producer with eight files cannot spend the whole allowance on
// headers.
const artifactBlockOverhead = 64

// readProduct inlines the files a settled node left behind, bounded to limit
// bytes across all of them.
//
// The bound is the caller's pot, because the pot is what this text is going to
// be spent against: reading more than the whole fan-in budget could ever carry
// is work done to be thrown away. Whatever does not fit stays exactly where it
// was — the paths travel beside the digest as they always have, and the
// consumer that needs a byte past the bound opens the file.
//
// Three things are refused, and all three are structural facts about the file
// rather than judgments about its contents: a path that is not a regular file
// (a directory, a device, a path that never existed — summaryPaths recovers
// absolute-looking words from prose, so some of them are not files at all), a
// file that reads as binary, and a file already inlined for this same consumer.
func readProduct(files []string, limit int, seen map[string]bool) string {
	if limit <= 0 || len(files) == 0 {
		return ""
	}
	var block strings.Builder
	remaining := limit
	for _, path := range files {
		if remaining < productReadFloor {
			break
		}
		if seen[path] {
			continue
		}
		body, ok := readArtifact(path, remaining-artifactBlockOverhead)
		if !ok || strings.TrimSpace(body) == "" {
			continue
		}
		seen[path] = true
		header := "\n\n--- what it wrote, the file " + path + " ---\n"
		block.WriteString(header)
		block.WriteString(body)
		remaining -= len(header) + len(body)
	}
	return block.String()
}

// readArtifact reads one file, bounded, and reports whether what came back is
// text a consumer can be handed.
//
// It reads one byte past the bound so a clipped read can say it was clipped.
// The note matters more here than it does in the pot's own clipping: the
// consumer is being told this is what the producer wrote, and a file that
// stops mid-sentence with nothing saying why is the one shape that reads as a
// finished thought and is not one.
func readArtifact(path string, limit int) (string, bool) {
	if limit <= 0 {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return "", false
	}
	if strings.IndexByte(string(raw), 0) >= 0 {
		// Not text. A consumer handed the first kilobyte of a PNG has been
		// handed noise it will spend a turn making sense of.
		return "", false
	}
	if len(raw) > limit {
		clipped := bounded(string(raw), limit)
		return clipped + "\n[the rest of this file is past the budget for it — open " + path + " for all of it]", true
	}
	return string(raw), true
}

// productBytes is how many bytes a settled node's files hold, without reading
// them. It is the measurement half of the same fact readProduct carries, and it
// exists because every budget downstream of the fan-in — the completion reserve
// an assembly is sized with, the turn and token grant a gathering leaf is given
// — is arithmetic over what actually landed. Counting the summary alone counted
// the press release and sized the consumer for it.
func productBytes(files []string, seen map[string]bool) int {
	total := 0
	for _, path := range files {
		if seen[path] {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		seen[path] = true
		total += int(info.Size())
	}
	return total
}
