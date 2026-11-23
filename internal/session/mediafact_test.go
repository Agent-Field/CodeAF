package session

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func usageCost(v float64) *ai.Usage { return &ai.Usage{Cost: &v} }

func TestChargeFromUsageNeverFabricatesAFigure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		usage  *ai.Usage
		charge string
		cost   bool
	}{
		{"absent usage", nil, MediaChargeUnknown, false},
		{"usage without a cost", &ai.Usage{PromptTokens: 5}, MediaChargeUnknown, false},
		{"explicit zero", usageCost(0), MediaNotCharged, false},
		{"positive", usageCost(0.16), MediaCharged, true},
		{"positive infinity", usageCost(math.Inf(1)), MediaChargeUnknown, false},
		{"negative infinity", usageCost(math.Inf(-1)), MediaChargeUnknown, false},
		{"not a number", usageCost(math.NaN()), MediaChargeUnknown, false},
		{"negative", usageCost(-1), MediaChargeUnknown, false},
	} {
		charge, cost := ChargeFromUsage(tc.usage)
		if charge != tc.charge || (cost != nil) != tc.cost {
			t.Errorf("%s: got %s %v, want %s (cost %v)", tc.name, charge, cost, tc.charge, tc.cost)
		}
		if cost != nil {
			if _, err := json.Marshal(cost); err != nil {
				t.Errorf("%s: a reported cost does not marshal: %v", tc.name, err)
			}
		}
	}
}

func TestGeneratedMediaReadsOnlyActualFacts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.png")
	data := pngOfSize(t, 40, 30)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	fact := GeneratedMedia(path, data, " paint/model ").WithUsage(usageCost(0.5)).WithReferences([]string{"r1.png", " ", "r2.png"}).WithSaid("hello")
	if fact.Width != 40 || fact.Height != 30 || fact.MIME != "image/png" || fact.Bytes != int64(len(data)) {
		t.Fatalf("fact = %+v", fact)
	}
	if len(fact.SHA256) != 64 || fact.Model != "paint/model" || fact.Charge != MediaCharged || fact.Said != "hello" {
		t.Fatalf("fact = %+v", fact)
	}
	if strings.Join(fact.References, ",") != "r1.png,r2.png" {
		t.Fatalf("references = %v", fact.References)
	}
	if fact.ModNS == 0 || fact.Canonical == "" {
		t.Fatalf("identity not stamped: %+v", fact)
	}
	// Audio: no dimensions, no duration — nothing here knows them.
	audio := GeneratedMedia(path, []byte("ID3\x04 not really"), "m")
	if audio.Width != 0 || audio.DurationMS != 0 {
		t.Fatalf("audio fact invents numbers: %+v", audio)
	}
	if long := (MediaFact{}).WithSaid(strings.Repeat("x", mediaSaidMax+1)); long.Said != "" {
		t.Fatal("an over-long text was cut instead of dropped")
	}
}

// appendLegacyRow appends one raw row to an index.
func appendLegacyRow(t *testing.T, index string, row Artifact) {
	t.Helper()
	if !RecordArtifact(index, row) {
		t.Fatalf("row %+v was not written", row)
	}
}

func mediaFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pngOfSize(t, 4, 3), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSameCanonicalPathRecordedTwiceIsTwoSources(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	path := filepath.Join(dir, "draft.png")
	var ids []string
	for i := 1; i <= 2; i++ {
		data := pngOfSize(t, 10*i, 10)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		id := RecordMedia(index, Artifact{Path: path, Session: "s1", Title: "draft", Kind: "image", Created: time.Now()},
			GeneratedMedia(path, data, "m"))
		ids = append(ids, id)
	}
	if ids[0] == "" || ids[0] == ids[1] {
		t.Fatalf("ids = %v: an overwrite shares its predecessor's id", ids)
	}
	scan, err := ListMedia(index, "s1", "", 10, time.Time{})
	if err != nil || len(scan.Rows) != 2 {
		t.Fatalf("list = %+v %v", scan, err)
	}
	if scan.Rows[0].Fact.ID != ids[1] || scan.Rows[1].Fact.ID != ids[0] {
		t.Fatalf("order/ids = %s %s", scan.Rows[0].Fact.ID, scan.Rows[1].Fact.ID)
	}
	if scan.Rows[0].Version() == scan.Rows[1].Version() || !strings.HasPrefix(scan.Rows[0].Version(), "sha256:") {
		t.Fatalf("versions %q %q", scan.Rows[0].Version(), scan.Rows[1].Version())
	}
	// Reopen: a second read of the same file answers the same ids.
	again, _ := ListMedia(index, "s1", "", 10, time.Time{})
	if again.Rows[0].Fact.ID != ids[1] {
		t.Fatal("ids did not persist")
	}
}

func TestLegacyRowsListWithStableDistinctIDs(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	path := mediaFile(t, dir, "old.png")
	at := time.Unix(1700000000, 5)
	appendLegacyRow(t, index, Artifact{Path: path, Session: "s1", Title: "old", Kind: "image", Created: at})
	appendLegacyRow(t, index, Artifact{Path: path, Session: "s1", Title: "old", Kind: "image", Created: at.Add(time.Second)})
	appendLegacyRow(t, index, Artifact{Path: path, Session: "s1", Title: "export", Kind: "export", Created: at})
	first, _ := ListMedia(index, "s1", "", 10, time.Time{})
	if len(first.Rows) != 2 {
		t.Fatalf("rows = %d, want the export left out", len(first.Rows))
	}
	a, b := first.Rows[0], first.Rows[1]
	if !strings.HasPrefix(a.Fact.ID, "srl_") || a.Fact.ID == b.Fact.ID || a.Recorded || a.Version() != "" {
		t.Fatalf("legacy rows = %+v %+v", a, b)
	}
	// Stable through appends.
	appendLegacyRow(t, index, Artifact{Path: path, Session: "s1", Title: "new", Kind: "image", Created: at.Add(time.Hour)})
	second, _ := ListMedia(index, "s1", "", 10, time.Time{})
	if second.Rows[1].Fact.ID != a.Fact.ID || second.Rows[2].Fact.ID != b.Fact.ID {
		t.Fatal("a legacy id moved when a row was appended")
	}
	got, err := FindMedia(index, "s1", b.Fact.ID, time.Time{})
	if err != nil || got.Fact.ID != b.Fact.ID {
		t.Fatalf("find legacy = %v %v", got.Fact.ID, err)
	}
}

// sparseIndex writes `old` rows for s1, `filler` rows of other sessions, then
// `recent` rows for s1, returning the ids oldest-first.
func sparseIndex(t *testing.T, index, dir string, old, filler, recent int) []string {
	t.Helper()
	path := mediaFile(t, dir, "p.png")
	var ids []string
	add := func(session string, n int) {
		id := RecordMedia(index, Artifact{Path: path, Session: session, Title: fmt.Sprintf("t%d", n), Kind: "image", Created: time.Now()},
			GeneratedMedia(path, []byte(fmt.Sprint(n)), "m"))
		if session == "s1" {
			ids = append(ids, id)
		}
	}
	for i := 0; i < old; i++ {
		add("s1", i)
	}
	for i := 0; i < filler; i++ {
		add("other", i)
	}
	for i := 0; i < recent; i++ {
		add("s1", 1000+i)
	}
	return ids
}

func withSmallScan(t *testing.T, chunk, budget int64) {
	t.Helper()
	c, b := mediaChunk, mediaScanBudget
	mediaChunk, mediaScanBudget = chunk, budget
	t.Cleanup(func() { mediaChunk, mediaScanBudget = c, b })
}

func TestListWalksBackThroughTheWholeIndexInResumablePages(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	ids := sparseIndex(t, index, dir, 3, 400, 2)
	withSmallScan(t, 512, 4096) // the filler is many times a call's budget
	var got []string
	cursor, calls, incomplete := "", 0, 0
	for {
		scan, err := ListMedia(index, "s1", cursor, 100, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		calls++
		if scan.Incomplete {
			incomplete++
			if scan.Next == "" {
				t.Fatal("incomplete without a cursor to resume")
			}
		}
		for _, row := range scan.Rows {
			got = append(got, row.Fact.ID)
		}
		if scan.Next == "" {
			break
		}
		cursor = scan.Next
		if calls > 10000 {
			t.Fatal("listing does not terminate")
		}
	}
	if incomplete == 0 {
		t.Fatal("the budget never forced a resumable, incomplete page")
	}
	// Newest first, every row of the session, none from "other", none lost.
	if len(got) != len(ids) {
		t.Fatalf("got %d rows, want %d (oldest media must not be cut)", len(got), len(ids))
	}
	for i, id := range got {
		if id != ids[len(ids)-1-i] {
			t.Fatalf("row %d = %s, want %s", i, id, ids[len(ids)-1-i])
		}
	}
}

func TestListPagesByLimitAndSurvivesAppends(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	ids := sparseIndex(t, index, dir, 0, 0, 7)
	first, err := ListMedia(index, "s1", "", 3, time.Time{})
	if err != nil || len(first.Rows) != 3 || first.Next == "" || first.Incomplete {
		t.Fatalf("first = %+v %v", first, err)
	}
	// A row appended between pages is not seen and shifts nothing.
	sparseIndex(t, index, dir, 0, 0, 1)
	second, err := ListMedia(index, "s1", first.Next, 3, time.Time{})
	if err != nil || len(second.Rows) != 3 || second.Rows[0].Fact.ID != ids[3] {
		t.Fatalf("second = %+v %v", second, err)
	}
	third, _ := ListMedia(index, "s1", second.Next, 3, time.Time{})
	if len(third.Rows) != 1 || third.Rows[0].Fact.ID != ids[0] || third.Next != "" {
		t.Fatalf("third = %+v", third)
	}
}

func TestListCursorRefusesAReplacedOrForeignIndex(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	sparseIndex(t, index, dir, 0, 0, 5)
	page, _ := ListMedia(index, "s1", "", 2, time.Time{})
	for _, bad := range []string{"nonsense", "m2.!!", "m2." + base64.RawURLEncoding.EncodeToString([]byte(`{"e":-4}`))} {
		if _, err := ListMedia(index, "s1", bad, 2, time.Time{}); !errors.Is(err, ErrMediaCursor) {
			t.Fatalf("cursor %q answered %v", bad, err)
		}
	}
	// Replace the file (new inode) with a shorter one.
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	sparseIndex(t, index, dir, 0, 0, 1)
	if _, err := ListMedia(index, "s1", page.Next, 2, time.Time{}); !errors.Is(err, ErrMediaCursor) {
		t.Fatalf("a cursor into a replaced index answered %v", err)
	}
}

func TestScanSurvivesChunkBoundariesAndOverlongLines(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	ids := sparseIndex(t, index, dir, 2, 0, 0)
	// An oversize junk line between rows, then more rows.
	f, _ := os.OpenFile(index, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"session":"s1","junk":"` + strings.Repeat("z", mediaRowMax+10) + "\"}\n")
	_ = f.Close()
	ids = append(ids, sparseIndex(t, index, dir, 2, 0, 0)...)
	for _, chunk := range []int64{7, 64, 1000, 1 << 20} {
		withSmallScan(t, chunk, 1<<40)
		scan, err := ListMedia(index, "s1", "", 100, time.Time{})
		if err != nil || len(scan.Rows) != len(ids) || scan.Next != "" {
			t.Fatalf("chunk %d: rows=%d next=%q err=%v, want %d", chunk, len(scan.Rows), scan.Next, err, len(ids))
		}
		for i, row := range scan.Rows {
			if row.Fact.ID != ids[len(ids)-1-i] {
				t.Fatalf("chunk %d: row %d wrong", chunk, i)
			}
		}
	}
}

func TestFindReachesTheOldestSourceAndNeverMistakesOutOfReachForAbsent(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	ids := sparseIndex(t, index, dir, 1, 300, 1)
	withSmallScan(t, 256, 1024) // find is not subject to the listing's byte budget
	row, err := FindMedia(index, "s1", ids[0], time.Time{})
	if err != nil || row.Fact.ID != ids[0] {
		t.Fatalf("oldest = %v %v", row.Fact.ID, err)
	}
	// Another conversation's source is not this one's.
	if _, err := FindMedia(index, "other", ids[0], time.Time{}); !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("foreign source answered %v", err)
	}
	if _, err := FindMedia(index, "s1", NewMediaID(), time.Time{}); !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("unknown source answered %v", err)
	}
	if _, err := FindMedia(index, "s1", "../../etc/passwd", time.Time{}); !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("a path as an id answered %v", err)
	}
	// The first lookup warmed this source. An expired caller still refuses
	// before a cache validation or a new scan starts.
	before := MediaIndexScans()
	if _, err := FindMedia(index, "s1", ids[0], time.Now().Add(-time.Second)); !errors.Is(err, ErrMediaBusy) {
		t.Fatalf("expired deadline answered %v", err)
	}
	if after := MediaIndexScans(); after != before {
		t.Fatalf("expired lookup started a scan: before %d, after %d", before, after)
	}
	if _, err := FindMedia(filepath.Join(dir, "none.jsonl"), "s1", ids[0], time.Time{}); !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("no index answered %v", err)
	}
}

func TestResolveMediaStatesAndScope(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	record := func(origin, path string) MediaRow {
		fact := DerivedMedia(path)
		fact.Origin = origin
		RecordMedia(index, Artifact{Path: path, Session: "s", Title: filepath.Base(path), Kind: "image", Created: time.Now()}, fact)
		scan, _ := ListMedia(index, "s", "", 1, time.Time{})
		return scan.Rows[0]
	}
	roots := []string{workspace}

	inside := mediaFile(t, workspace, "in.png")
	row := record(MediaGenerated, inside)
	if live := ResolveMedia(row, roots); live.State != MediaAvailable || live.Path == "" || !live.HasBytes {
		t.Fatalf("inside = %+v", live)
	}
	// Changed: same path, new bytes. It may not be opened.
	if err := os.WriteFile(inside, append(pngOfSize(t, 4, 3), 0), 0o644); err != nil {
		t.Fatal(err)
	}
	if live := ResolveMedia(row, roots); live.State != MediaChanged {
		t.Fatalf("rewritten = %+v", live)
	}
	// Missing keeps the record: the row still says what was recorded.
	if err := os.Remove(inside); err != nil {
		t.Fatal(err)
	}
	live := ResolveMedia(row, roots)
	if live.State != MediaMissing || live.HasBytes || live.Path != "" || row.Fact.Bytes == 0 {
		t.Fatalf("missing = %+v, recorded bytes %d", live, row.Fact.Bytes)
	}

	// A generated file that is really a link out of the workspace is outside.
	target := mediaFile(t, outside, "secret.png")
	link := filepath.Join(workspace, "link.png")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	escaped := record(MediaGenerated, link)
	if got := ResolveMedia(escaped, roots); got.State != MediaOutside || got.Path != "" {
		t.Fatalf("symlink escape = %+v", got)
	}
	// The exact file a person attached is allowed wherever it is.
	if got := ResolveMedia(record(MediaAttachment, target), roots); got.State != MediaAvailable {
		t.Fatalf("attachment = %+v", got)
	}
	// ...until a directory on its path becomes a link elsewhere.
	sub := filepath.Join(outside, "d")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	attached := record(MediaAttachment, mediaFile(t, sub, "x.png"))
	elsewhere := t.TempDir()
	mediaFile(t, elsewhere, "x.png")
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, sub); err != nil {
		t.Fatal(err)
	}
	if got := ResolveMedia(attached, roots); got.State != MediaOutside {
		t.Fatalf("retargeted = %+v", got)
	}
	// Not a regular file.
	if got := ResolveMedia(record(MediaAttachment, outside), roots); got.State != MediaUnreadable {
		t.Fatalf("directory = %+v", got)
	}
}

func TestRecordAttachmentOnlyNotesMediaAndReadsOnlyAHead(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	text := filepath.Join(dir, "notes.txt")
	_ = os.WriteFile(text, []byte("hello"), 0o644)
	RecordAttachment(index, "s1", text)
	RecordAttachment(index, "s1", filepath.Join(dir, "gone.png"))
	RecordAttachment(index, "s1", dir)
	RecordAttachment(index, "", mediaFile(t, dir, "x.png"))
	if scan, _ := ListMedia(index, "s1", "", 10, time.Time{}); len(scan.Rows) != 0 {
		t.Fatalf("non-media recorded: %+v", scan.Rows)
	}
	RecordAttachment(index, "s1", mediaFile(t, dir, "pic.png"))
	scan, _ := ListMedia(index, "s1", "", 10, time.Time{})
	if len(scan.Rows) != 1 {
		t.Fatalf("rows = %d", len(scan.Rows))
	}
	r := scan.Rows[0]
	if r.Fact.Origin != MediaAttachment || r.Kind != "image" || r.Fact.Width != 4 || r.Fact.Height != 3 || r.Fact.SHA256 != "" {
		t.Fatalf("attachment = %+v", r.Fact)
	}
	if !strings.HasPrefix(r.Version(), "file:") {
		t.Fatalf("version %q: an attachment is never hashed", r.Version())
	}
}

func TestRecordAndListRaceCleanly(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	path := mediaFile(t, dir, "p.png")
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				RecordMedia(index, Artifact{Path: path, Session: "s1", Title: "t", Kind: "image", Created: time.Now()}, DerivedMedia(path))
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				if _, err := ListMedia(index, "s1", "", 10, time.Time{}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	total, cursor := 0, ""
	for {
		scan, err := ListMedia(index, "s1", cursor, 100, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		total += len(scan.Rows)
		if scan.Next == "" {
			break
		}
		cursor = scan.Next
	}
	if total != 100 {
		t.Fatalf("listed %d rows, want 100", total)
	}
}

// A file whose size and modification time match the record is still not the
// recorded file when its change time or inode differ: times can be set back by
// a caller, the change time and the inode cannot.
func TestResolveMediaDetectsSameSizeReplacementWithRestoredTimes(t *testing.T) {
	workspace := t.TempDir()
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	record := func(name string) (MediaRow, string, os.FileInfo) {
		path := filepath.Join(workspace, name)
		if err := os.WriteFile(path, []byte("AAAAAAAAAAAAAAAA"), 0o644); err != nil {
			t.Fatal(err)
		}
		fact := DerivedMedia(path)
		fact.MIME = "image/png"
		RecordMedia(index, Artifact{Path: path, Session: "s", Title: name, Kind: "image", Created: time.Now()}, fact)
		scan, _ := ListMedia(index, "s", "", 1, time.Time{})
		info, _ := os.Stat(path)
		return scan.Rows[0], path, info
	}
	roots := []string{workspace}
	if _, _, ino := fileIdentity(mustStat(t, workspace)); ino == 0 {
		t.Skip("this platform reports no inode or change time")
	}

	// In place, same size, mtime put back: only the change time gives it away.
	row, path, info := record("inplace.png")
	if got := ResolveMedia(row, roots); got.State != MediaAvailable {
		t.Fatalf("untouched = %+v", got)
	}
	time.Sleep(30 * time.Millisecond) // the kernel's timestamps are coarse
	if err := os.WriteFile(path, []byte("BBBBBBBBBBBBBBBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got := ResolveMedia(row, roots); got.State != MediaChanged {
		t.Fatalf("same-size rewrite with the mtime restored = %+v", got)
	}

	// Atomic replace, same size, same mtime: a new inode.
	row, path, info = record("atomic.png")
	time.Sleep(30 * time.Millisecond)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, []byte("CCCCCCCCCCCCCCCC"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(temp, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temp, path); err != nil {
		t.Fatal(err)
	}
	if got := ResolveMedia(row, roots); got.State != MediaChanged {
		t.Fatalf("atomic replace with the mtime restored = %+v", got)
	}

	// A record with no identity (an older row) is never called changed on a guess.
	legacy := MediaRow{Artifact: Artifact{Path: path}, Fact: MediaFact{Origin: MediaGenerated, Canonical: canonicalFile(path)}, Kind: "image"}
	if got := ResolveMedia(legacy, roots); got.State != MediaAvailable {
		t.Fatalf("legacy = %+v", got)
	}
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// A page ends BEFORE the row that would pass its reply budget, with a cursor
// that resumes at that row; a row that alone does not fit is an error.
func TestListMediaFitEndsBeforeTheRowThatDoesNotFit(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	ids := sparseIndex(t, index, dir, 0, 0, 10)
	cost := func(MediaRow) (int, error) { return 100, nil }
	var got []string
	cursor := ""
	for pages := 0; ; pages++ {
		scan, err := ListMediaFit(index, "s1", cursor, 100, time.Time{}, 250, cost)
		if err != nil {
			t.Fatal(err)
		}
		if len(scan.Rows) > 2 {
			t.Fatalf("page of %d rows passes the budget", len(scan.Rows))
		}
		for _, row := range scan.Rows {
			got = append(got, row.Fact.ID)
		}
		if scan.Next == "" {
			break
		}
		if pages > 20 {
			t.Fatal("does not terminate")
		}
		cursor = scan.Next
	}
	if len(got) != len(ids) {
		t.Fatalf("got %d of %d rows: a row was dropped or repeated", len(got), len(ids))
	}
	for i, id := range got {
		if id != ids[len(ids)-1-i] {
			t.Fatalf("row %d out of order", i)
		}
	}
	if _, err := ListMediaFit(index, "s1", "", 100, time.Time{}, 50, cost); !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("a row alone over budget answered %v", err)
	}
}
