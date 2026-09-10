package session

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

const contextTracePageSize = 40
const contextTraceScanLimit = 16 * 1024 * 1024

// A receipt describes the inputs actually selected for one execution. It is
// evidence of exposure, never a claim that the model followed an instruction.
// Standing documents replace earlier revisions, so the effective words belong
// here; shared context already has immutable revision history in its owner.
type contextExposure struct {
	ExecutionID          string             `json:"execution_id"`
	Phase                string             `json:"phase"`
	Owner                workspace.Ref      `json:"owner,omitempty"`
	ParentCause          string             `json:"parent_cause,omitempty"`
	Standing             []standingExposure `json:"standing,omitempty"`
	Shared               []sharedExposure   `json:"shared,omitempty"`
	ReadError            string             `json:"read_error,omitempty"`
	GoverningCollections map[string]int     `json:"governing_collections,omitempty"`
}

type standingExposure struct {
	ID         string               `json:"id"`
	Revision   uint64               `json:"revision"`
	Origin     standing.Origin      `json:"origin"`
	Prompt     string               `json:"prompt"`
	Grant      string               `json:"grant,omitempty"`
	Workspace  string               `json:"workspace,omitempty"`
	Altitude   standing.Altitude    `json:"altitude,omitempty"`
	Exceptions []standing.Exception `json:"exceptions,omitempty"`
	Scope      *standing.Scope      `json:"scope,omitempty"`
	Adoption   *standing.Adoption   `json:"adoption,omitempty"`
}

type sharedExposure struct {
	ID        string        `json:"id"`
	Revision  int           `json:"revision"`
	Source    workspace.Ref `json:"source"`
	Truncated bool          `json:"truncated,omitempty"`
}

// recordContextExposureLocked consumes the same selections the prompt renderer
// used. Re-querying either store here would claim exposure to a different
// revision if a person edited it between selection and the journal write.
// The caller holds a.mu; receipt identifiers are independent of process-local
// turn counters and therefore cannot be reused after reopening a conversation.
func (a *Agent) recordContextExposureLocked(owner workspace.Ref, records []workspace.ContextRecord, holds []standing.Item, readError string, placements ...map[string]int) string {
	if a.file == nil {
		return ""
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ""
	}
	receipt := contextExposure{ExecutionID: hex.EncodeToString(random[:]), Phase: "selected", Owner: owner, ParentCause: "not_recorded", ReadError: readError}
	if len(placements) > 0 {
		receipt.GoverningCollections = placements[0]
	}
	for _, item := range holds {
		altitude := item.Level()
		if item.Scope != nil {
			altitude = ""
		}
		receipt.Standing = append(receipt.Standing, standingExposure{ID: item.ID, Revision: item.Revision, Origin: item.Origin, Prompt: item.Prompt(), Grant: item.Grant, Workspace: item.Workspace, Altitude: altitude, Exceptions: item.Exceptions, Scope: item.Scope, Adoption: item.Adoption})
	}
	for _, record := range records {
		receipt.Shared = append(receipt.Shared, sharedExposure{ID: record.ID, Revision: record.Revision, Source: record.Source, Truncated: len([]rune(record.Text)) > organizationTextLimit})
	}
	if !a.file.appendContextExposure(receipt) {
		return ""
	}
	return receipt.ExecutionID
}

// A returned loop is not a successful work outcome. Completion, errors and
// tool replies retain their existing records and are linked by the reader.
func (a *Agent) finishContextExposure(executionID string) {
	if executionID == "" {
		return
	}
	a.mu.Lock()
	file := a.file
	a.mu.Unlock()
	if file != nil {
		file.appendContextExposure(contextExposure{ExecutionID: executionID, Phase: "loop_returned"})
	}
}

func (s *sessionFile) appendContextExposure(receipt contextExposure) bool {
	if !s.writeLine(sessionEntry{Type: "context_exposure", Exposure: &receipt, Timestamp: stamp()}) {
		return false
	}
	// Receipt boundaries are durable before an execution is described as recorded.
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.file.Sync() == nil
}

// Each trace row points to original journal evidence. It intentionally carries
// no tool arguments, response bodies or model reasoning. Provider call IDs are
// not globally unique, so the original physical line is always part of a link.
type contextTraceRow struct {
	Line          int                `json:"line"`
	Kind          string             `json:"kind"`
	Exposure      *contextExposure   `json:"exposure,omitempty"`
	Calls         []contextTraceCall `json:"calls,omitempty"`
	ResultFor     string             `json:"result_for,omitempty"`
	CallLine      int                `json:"call_line,omitempty"`
	ExecutionID   string             `json:"execution_id,omitempty"`
	Association   string             `json:"association,omitempty"`
	DetailOmitted bool               `json:"detail_omitted,omitempty"`
}
type contextTraceCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type contextTracePage struct {
	Journal     string            `json:"journal"`
	Rows        []contextTraceRow `json:"rows"`
	NextLine    int               `json:"next_line,omitempty"`
	Unreadable  string            `json:"unreadable,omitempty"`
	Limitations string            `json:"limitations"`
}

func (a *Agent) contextTraceTools() []bare.Tool {
	if strings.TrimSpace(a.config.SessionFile) == "" {
		return nil
	}
	return []bare.Tool{{Name: "context_trace", Description: "Read a bounded page of this conversation's recorded context selections and links to original tool calls, replies and outcomes. Exposure does not prove compliance; a successful tool reply does not prove an external effect. Parent causes may be unknown. Use next_line for the next page, and read the original journal line for its evidence.", Schema: json.RawMessage(`{"type":"object","properties":{"from_line":{"type":"integer","minimum":1}},"additionalProperties":false}`), Execute: a.contextTraceTool}}
}

func (a *Agent) contextTraceTool(ctx context.Context, raw json.RawMessage) (string, bool, error) {
	var args struct {
		FromLine int `json:"from_line"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return err.Error(), true, nil
	}
	if args.FromLine < 0 {
		return "from_line must be positive", true, nil
	}
	a.mu.Lock()
	path := a.config.SessionFile
	if a.file != nil && a.file.file != nil {
		path = a.file.file.Name()
	}
	a.mu.Unlock()
	page, err := readContextTrace(ctx, path, max(1, args.FromLine))
	if err != nil {
		return err.Error(), true, nil
	}
	result, err := json.Marshal(page)
	if err != nil {
		return err.Error(), true, nil
	}
	return string(result), false, nil
}

// The journal remains the source of truth. Range association is conservative:
// overlapping executions and duplicate unmatched provider IDs are ambiguous,
// never guessed into a causal link. Metadata is not replayed as model input.
func readContextTrace(ctx context.Context, path string, from int) (contextTracePage, error) {
	page := contextTracePage{Journal: path, Rows: []contextTraceRow{}, Limitations: "Selection records describe exposure, not compliance. Tool rows are observed calls/replies, not proof of external effects. Execution links use journal windows; overlapping executions are ambiguous. Parent causes are not recorded. Original lines remain authoritative; read them for outcomes and content."}
	file, err := os.Open(path)
	if err != nil {
		return page, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), contextTraceScanLimit)
	active := map[string]bool{}
	interleaved := map[string]bool{}
	pending := map[string][]int{}
	line := 0
	replayedMessages := 0
	pageBytes := 0
	scannedBytes := 0
	for scanner.Scan() {
		line++
		scannedBytes += len(scanner.Bytes()) + 1
		if scannedBytes > contextTraceScanLimit {
			page.Unreadable = fmt.Sprintf("Trace scan stopped at line %d after its %d-byte budget; inspect the original journal for later evidence.", line, contextTraceScanLimit)
			break
		}
		if err := ctx.Err(); err != nil {
			return page, err
		}
		var entry sessionEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			page.Unreadable = fmt.Sprintf("Could not read journal line %d; later evidence was not inspected.", line)
			break
		}
		if entry.Type == "session" {
			var header sessionHeader
			if err := json.Unmarshal(scanner.Bytes(), &header); err == nil && header.Version > sessionFileVersion {
				return page, fmt.Errorf("journal format %d is newer than this reader", header.Version)
			}
		}
		if entry.Type == "compaction" {
			if entry.Window <= 0 {
				page.Unreadable = fmt.Sprintf("Journal line %d has an unknown compaction window; later copied messages cannot be distinguished from original actions.", line)
				break
			}
			pending = map[string][]int{}
			replayedMessages = entry.Window
			continue
		}
		if entry.Type == "message" && replayedMessages > 0 {
			replayedMessages--
			continue
		}
		var row contextTraceRow
		row.Line = line
		if entry.Exposure != nil && entry.Type == "context_exposure" {
			row.Kind, row.Exposure = entry.Type, entry.Exposure
			if entry.Exposure.Phase == "selected" || entry.Exposure.Phase == "loop_returned" {
				pending = map[string][]int{}
			}
			if entry.Exposure.Phase == "selected" {
				active[entry.Exposure.ExecutionID] = true
				if len(active) > 1 {
					for id := range active {
						interleaved[id] = true
					}
				}
			}
			if entry.Exposure.Phase == "loop_returned" {
				delete(active, entry.Exposure.ExecutionID)
			}
		} else {
			switch entry.Type {
			case "message":
				if entry.Role == "assistant" {
					pending = map[string][]int{}
				}
				if len(entry.ToolCalls) > 0 {
					row.Kind = "tool_calls"
					for _, call := range entry.ToolCalls {
						row.Calls = append(row.Calls, contextTraceCall{ID: call.ID, Name: call.Function.Name})
						if len(pending[call.ID]) > 0 {
							for i := range pending[call.ID] {
								pending[call.ID][i] = 0
							}
							pending[call.ID] = append(pending[call.ID], 0)
						} else {
							pending[call.ID] = []int{line}
						}
					}
				} else if entry.Role == "tool" {
					row.Kind, row.ResultFor = "tool_reply", entry.ToolCallID
					candidates := pending[entry.ToolCallID]
					if len(candidates) == 1 {
						row.CallLine = candidates[0]
					}
					if len(candidates) > 0 {
						pending[entry.ToolCallID] = candidates[1:]
					}
				}
			case "principal", "error", "failure", "abandoned", "created":
				row.Kind = entry.Type
			}
			if row.Kind != "" {
				row.Association = "unassigned"
				if len(active) == 1 {
					for id := range active {
						if interleaved[id] {
							row.Association = "overlapping_executions"
						} else {
							row.ExecutionID = id
						}
					}
					if row.ExecutionID != "" {
						row.Association = "journal_window"
					}
				}
				if len(active) > 1 {
					row.Association = "overlapping_executions"
				}
			}
		}
		if row.Kind == "" || line < from {
			continue
		}
		encoded, _ := json.Marshal(row)
		if len(encoded) > 12000 {
			row = contextTraceRow{Line: row.Line, Kind: row.Kind, DetailOmitted: true}
			encoded, _ = json.Marshal(row)
		}
		if len(page.Rows) == contextTracePageSize || (len(page.Rows) > 0 && pageBytes+len(encoded) > 24000) {
			page.NextLine = line
			break
		}
		pageBytes += len(encoded)
		page.Rows = append(page.Rows, row)
	}
	if err := scanner.Err(); err != nil {
		page.Unreadable = fmt.Sprintf("Journal reading stopped after line %d: %v", line, err)
	}
	return page, nil
}
