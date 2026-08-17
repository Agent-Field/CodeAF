package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE APPROVAL MEMORY, as round trips.
//
// Every test here is about one sentence from approvalmemory.go's head: the write
// touches the person's row and nothing else, a row that does not parse is
// reported rather than rewritten, and an answer already given is not written a
// second time.

// profileWith writes one profile config.json and answers with its directory.
func profileWith(t *testing.T, rows map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	if len(rows) == 0 {
		return dir
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A profile that has never been told anything gets its first exception.
func TestRememberingAToolWritesTheFirstException(t *testing.T) {
	dir := profileWith(t, nil)
	if err := RememberToolApproval(dir, "edit", "allow"); err != nil {
		t.Fatalf("the first always did not land: %v", err)
	}
	if got := ToolApprovalsAt(dir); got != "edit:allow" {
		t.Fatalf("the row reads %q, want `edit:allow`", got)
	}
	tools, err := ParseToolApprovals(ToolApprovalsAt(dir))
	if err != nil || tools["edit"] != "allow" {
		t.Fatalf("the row does not parse back to the rule that was written: %v (%v)", tools, err)
	}
}

// A populated row KEEPS THE PERSON'S OWN TEXT and grows by one entry. The order
// they wrote and the spacing they used are still there afterwards, because the
// row is a line they read back.
func TestRememberingAToolMergesIntoTheRowThatIsThere(t *testing.T) {
	dir := profileWith(t, map[string]any{KeyToolApprovals: "read:allow, write:deny"})
	if err := RememberToolApproval(dir, "edit", "allow"); err != nil {
		t.Fatalf("the merge failed: %v", err)
	}
	if got := ToolApprovalsAt(dir); got != "read:allow, write:deny, edit:allow" {
		t.Fatalf("the row reads %q — the merge did not append to what was there", got)
	}
}

// Pressing always twice writes once. The second press finds the answer already
// in the row and touches no file at all.
func TestRememberingTheSameToolTwiceLeavesOneEntry(t *testing.T) {
	dir := profileWith(t, map[string]any{KeyToolApprovals: "read:allow"})
	for i := 0; i < 2; i++ {
		if err := RememberToolApproval(dir, "read", "allow"); err != nil {
			t.Fatalf("press %d: %v", i+1, err)
		}
	}
	if got := ToolApprovalsAt(dir); got != "read:allow" {
		t.Fatalf("the row reads %q, want the single entry it started with", got)
	}
}

// A tool that already has a DIFFERENT answer is rewritten in place, in the order
// the row was written.
func TestRememberingAToolReplacesItsOldAnswerInPlace(t *testing.T) {
	dir := profileWith(t, map[string]any{KeyToolApprovals: "read:allow, bash:deny, write:prompt"})
	if err := RememberToolApproval(dir, "bash", "allow"); err != nil {
		t.Fatalf("the replacement failed: %v", err)
	}
	if got := ToolApprovalsAt(dir); got != "read:allow, bash:allow, write:prompt" {
		t.Fatalf("the row reads %q — the entry moved or its neighbours changed", got)
	}
}

// A MALFORMED ROW IS REPORTED, NEVER CLOBBERED. The rules somebody wrote are
// still on disk, exactly as they wrote them, after the refusal.
func TestRememberingRefusesAMalformedRowAndLeavesItAlone(t *testing.T) {
	dir := profileWith(t, map[string]any{KeyToolApprovals: "read:allow, nonsense"})
	err := RememberToolApproval(dir, "edit", "allow")
	if err == nil {
		t.Fatal("a row that does not parse was written over")
	}
	if !strings.Contains(err.Error(), KeyToolApprovals) {
		t.Fatalf("the refusal does not name the row: %v", err)
	}
	if got := ToolApprovalsAt(dir); got != "read:allow, nonsense" {
		t.Fatalf("the row is now %q — the refusal edited it", got)
	}
}

// An action that is not one of the three is a mistake and never a silent write.
func TestRememberingRefusesAnActionThatIsNotOneOfTheThree(t *testing.T) {
	dir := profileWith(t, nil)
	if err := RememberToolApproval(dir, "edit", "always"); err == nil {
		t.Fatal("`always` was accepted as an action")
	}
	if got := ToolApprovalsAt(dir); got != "" {
		t.Fatalf("the row reads %q after a refusal", got)
	}
}

// ── the bash rules ──────────────────────────────────────────────────────────

// One command approved is one whole-line allow rule, and pressing always twice
// leaves exactly one.
func TestRememberingABashCommandAppendsItOnce(t *testing.T) {
	dir := profileWith(t, nil)
	for i := 0; i < 2; i++ {
		if err := RememberBashApproval(dir, "git status --short"); err != nil {
			t.Fatalf("press %d: %v", i+1, err)
		}
	}
	if got := BashApprovalsAt(dir); got != "allow git status --short" {
		t.Fatalf("the row reads %q, want one allow rule", got)
	}
	rules, err := ParseBashApprovals(BashApprovalsAt(dir))
	if err != nil || len(rules) != 1 {
		t.Fatalf("the row parses to %v (%v), want one rule", rules, err)
	}
	if rules[0].Match != "git status --short" || rules[0].Action != "allow" {
		t.Fatalf("the rule is %+v", rules[0])
	}
}

// A second, different command lands after the first: order is the author's
// priority statement and an appended rule is the newest, not the strongest.
func TestRememberingASecondBashCommandKeepsBothInOrder(t *testing.T) {
	dir := profileWith(t, map[string]any{KeyBashApprovals: "deny rm -rf *"})
	if err := RememberBashApproval(dir, "npm test"); err != nil {
		t.Fatalf("the append failed: %v", err)
	}
	if got := BashApprovalsAt(dir); got != "deny rm -rf *, allow npm test" {
		t.Fatalf("the row reads %q", got)
	}
}

// A command carrying the row's own separators survives the round trip, because
// a glob that could read back as two rules is quoted.
func TestABashCommandWithCommasAndQuotesRoundTrips(t *testing.T) {
	dir := profileWith(t, nil)
	command := `git commit -m "wave 9, the card"`
	if err := RememberBashApproval(dir, command); err != nil {
		t.Fatalf("the write failed: %v", err)
	}
	rules, err := ParseBashApprovals(BashApprovalsAt(dir))
	if err != nil {
		t.Fatalf("the row does not parse back: %v", err)
	}
	if len(rules) != 1 || rules[0].Match != command {
		t.Fatalf("the row read back as %v, want the one command it was written from", rules)
	}
}

// A COMPOUND LINE CANNOT BE REMEMBERED. An allow rule vouches for one whole
// command, so a rule written for a compound line would never fire, and a
// settings row that claims an approval which does nothing is worse than none.
func TestACompoundCommandIsRefusedRatherThanWrittenDead(t *testing.T) {
	dir := profileWith(t, nil)
	if err := RememberBashApproval(dir, "cd /tmp && rm -rf build"); err == nil {
		t.Fatal("a compound line was written as an allow rule")
	}
	if got := BashApprovalsAt(dir); got != "" {
		t.Fatalf("the row reads %q after the refusal", got)
	}
}

// A line a standing rule already ALLOWS — this one, or a broader glob somebody
// wrote by hand — is not written a second time.
func TestABashCommandABroaderRuleAlreadyAllowsIsNotWrittenAgain(t *testing.T) {
	dir := profileWith(t, map[string]any{KeyBashApprovals: "allow git *"})
	if err := RememberBashApproval(dir, "git status"); err != nil {
		t.Fatalf("the no-op reported an error: %v", err)
	}
	if got := BashApprovalsAt(dir); got != "allow git *" {
		t.Fatalf("the row reads %q — a rule was appended under one that already allows the line", got)
	}
}

// A line a standing rule DENIES is left alone and the caller is told. First
// match wins, so an allow under it would be a rule that never runs, and the
// standing rule is a decision somebody made on purpose.
func TestABashCommandAStandingDenyAnswersIsRefused(t *testing.T) {
	dir := profileWith(t, map[string]any{KeyBashApprovals: "deny curl *"})
	err := RememberBashApproval(dir, "curl example.com")
	if err == nil {
		t.Fatal("an allow was appended under a standing deny")
	}
	if !strings.Contains(err.Error(), "deny") {
		t.Fatalf("the refusal does not say what already answers the command: %v", err)
	}
	if got := BashApprovalsAt(dir); got != "deny curl *" {
		t.Fatalf("the row is now %q", got)
	}
}

// A bash rules row that does not parse is reported and kept, exactly as the
// tool row is.
func TestAMalformedBashRowIsReportedAndKept(t *testing.T) {
	dir := profileWith(t, map[string]any{KeyBashApprovals: `sometimes git status`})
	err := RememberBashApproval(dir, "ls")
	if err == nil {
		t.Fatal("a row that does not parse was written over")
	}
	if !strings.Contains(err.Error(), KeyBashApprovals) {
		t.Fatalf("the refusal does not name the row: %v", err)
	}
	if got := BashApprovalsAt(dir); got != "sometimes git status" {
		t.Fatalf("the row is now %q", got)
	}
}

// The row's own grammar: the action leads, the glob may be quoted or plain, and
// commas inside a quoted glob are part of the command rather than a separator.
func TestTheBashRowReadsBothSpellings(t *testing.T) {
	rules, err := ParseBashApprovals(`allow git status*, deny "rm -rf /, all of it"` + "\n" + `prompt npm *`)
	if err != nil {
		t.Fatalf("the row does not parse: %v", err)
	}
	want := []BashRule{
		{Match: "git status*", Action: "allow"},
		{Match: "rm -rf /, all of it", Action: "deny"},
		{Match: "npm *", Action: "prompt"},
	}
	if len(rules) != len(want) {
		t.Fatalf("the row parsed to %v", rules)
	}
	for i := range want {
		if rules[i] != want[i] {
			t.Fatalf("rule %d is %+v, want %+v", i, rules[i], want[i])
		}
	}
	// And the writer's spelling of the same rules parses back to the same list.
	again, err := ParseBashApprovals(FormatBashApprovals(rules))
	if err != nil {
		t.Fatalf("the formatted row does not parse: %v", err)
	}
	for i := range want {
		if again[i] != want[i] {
			t.Fatalf("round trip %d is %+v, want %+v", i, again[i], want[i])
		}
	}
}

// A rule missing its action, and a rule missing its command, are both mistakes
// somebody made in a file they can see — never a silently dropped rule.
func TestTheBashRowRefusesAnEntryItCannotRead(t *testing.T) {
	for _, raw := range []string{"git status", "allow", `allow "unclosed`, "maybe ls"} {
		if _, err := ParseBashApprovals(raw); err == nil {
			t.Fatalf("%q parsed as a rule", raw)
		}
	}
}

// The row is a settings row like any other: it renders, it edits, and a person
// un-remembers by emptying it.
func TestTheBashRowEditsThroughTheRegistry(t *testing.T) {
	dir := t.TempDir()
	rows := registry(t, dir)
	row, ok := rows.Row(KeyBashApprovals)
	if !ok {
		t.Fatal("the bash rules row is not in the registry")
	}
	if err := row.Apply("allow git status*"); err != nil {
		t.Fatalf("the row refused a rule it should take: %v", err)
	}
	if got := BashApprovalsAt(dir); got != "allow git status*" {
		t.Fatalf("the row holds %q", got)
	}
	if got := row.Value(); got != "allow git status*" {
		t.Fatalf("the row renders %q", got)
	}
	if err := row.Apply("sometimes git status"); err == nil {
		t.Fatal("the row took an action that is not one of the three")
	}
	if err := row.Apply(""); err != nil {
		t.Fatalf("the row refused to be emptied: %v", err)
	}
	if got := BashApprovalsAt(dir); got != "" {
		t.Fatalf("the row still holds %q after being emptied", got)
	}
	if row.EmptyLabel == "" {
		t.Fatal("an empty rules row would render as a blank")
	}
}
