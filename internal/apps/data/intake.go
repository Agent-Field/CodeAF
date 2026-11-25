package data

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
)

const intakeMigration = `
CREATE TABLE intake_deliveries(
	app TEXT NOT NULL,
	channel TEXT NOT NULL,
	key TEXT NOT NULL,
	digest TEXT NOT NULL,
	receipt TEXT NOT NULL,
	PRIMARY KEY(app, channel, key)
);
CREATE TABLE intake_blobs(
	id TEXT PRIMARY KEY,
	content BLOB NOT NULL
);
CREATE TABLE intake_attachments(
	record TEXT NOT NULL,
	field TEXT NOT NULL,
	blob TEXT NOT NULL,
	PRIMARY KEY(record, field, blob)
);
`

// IntakeBytesMax bounds each delivery, including the combined attachment bytes.
const IntakeBytesMax = 4 << 20

type IntakeDelivery struct {
	App, Channel, Key, Type, Run string
	Version                      int
	Set                          map[string]any
	CSV                          []byte
	Mapping                      map[string]string
	FileField                    string
	Files                        []IntakeFile
	Work                         *WorkItem
	Form                         *appschema.ProcessForm
}

type IntakeFile struct {
	Name, MIME string
	Bytes      []byte
}

type IntakeReceipt struct {
	Batch    string          `json:"batch,omitempty"`
	Rows     []string        `json:"rows"`
	Created  int             `json:"created"`
	Failed   int             `json:"failed,omitempty"`
	Problems []ImportProblem `json:"problems,omitempty"`
	WorkItem string          `json:"work_item,omitempty"`
	Replayed bool            `json:"replayed,omitempty"`
}

// IntakeTrigger is the immutable routing information on a record-created event.
type IntakeTrigger struct {
	Channel string `json:"channel"`
	Run     string `json:"run"`
	Type    string `json:"record_type"`
}

// Deliver commits the dedupe receipt, record writes, work and outbox together.
// The version check closes the gap between interpreting a publication and writing it.
func (s *Store) Deliver(in IntakeDelivery) (IntakeReceipt, error) {
	var receipt IntakeReceipt
	if s.draft != "" || in.App == "" || in.Channel == "" || in.Key == "" || len(in.Key) > 256 {
		return receipt, errorf(CodeInvalid, "a delivery needs an app, channel and dedupe key of at most 256 bytes")
	}
	size := len(in.CSV)
	for _, file := range in.Files {
		size += len(file.Bytes)
	}
	if size > IntakeBytesMax {
		return receipt, errorf(CodeInvalid, "the delivery exceeds %d bytes", IntakeBytesMax)
	}
	payload, err := json.Marshal(struct {
		Set   map[string]any
		CSV   []byte
		Files []IntakeFile
	}{in.Set, in.CSV, in.Files})
	if err != nil {
		return receipt, errorf(CodeInvalid, "the delivery is not valid JSON")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	var header []string
	var lines [][]string
	if in.CSV != nil {
		header, lines, err = parseCSV(in.CSV)
		if err != nil {
			return receipt, err
		}
	}
	cause := "channel:" + in.Channel
	sc := Scope{App: in.App, Actor: cause, Cause: cause, CauseChain: []string{in.App + "/" + cause},
		sourceEvent: in.Key, intake: &IntakeTrigger{Channel: in.Channel, Run: in.Run, Type: in.Type}}
	err = s.write(func(w *writeTx) error {
		receipt = IntakeReceipt{Rows: []string{}}
		if in.Version != 0 {
			p, err := readPublication(w.tx, in.App)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				return errorf(CodeStale, "the app publication changed. Open the channel again")
			case err != nil:
				return err
			case p.Archived || p.Deleted || p.Version != in.Version:
				return errorf(CodeStale, "the app publication changed. Open the channel again")
			}
		}
		t, err := w.schema.allow(sc, in.Type, true)
		if err != nil {
			return err
		}
		var original, saved string
		err = w.tx.QueryRow(`SELECT digest, receipt FROM intake_deliveries WHERE app = ? AND channel = ? AND key = ?`,
			in.App, in.Channel, in.Key).Scan(&original, &saved)
		switch {
		case err == nil:
			if digest != original {
				return errorf(CodeDuplicate, "that dedupe key was already used for a different delivery")
			}
			receipt.Replayed = true
			return json.Unmarshal([]byte(saved), &receipt)
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		op := OpCreate
		if in.CSV != nil {
			op = OpImport
		}
		receipt.Batch, err = w.newBatch(sc, op, t)
		if err != nil {
			return err
		}
		if in.CSV != nil {
			mapping, err := importMapping(t, header, in.Mapping)
			if err != nil {
				return err
			}
			imported := &ImportResult{Mapping: mapping}
			if err := w.importRows(t, sc, header, lines, imported, func(id string) {
				receipt.Rows = append(receipt.Rows, id)
			}); err != nil {
				return err
			}
			receipt.Failed, receipt.Problems = imported.Failed, imported.Problems
			if imported.Created == 0 {
				if len(imported.Problems) > 0 {
					problem := imported.Problems[0]
					return fieldErr(problem.Field, "CSV line %d: %s", problem.Line, problem.Message)
				}
				return errorf(CodeInvalid, "the CSV has no rows to import")
			}
		} else if err := w.deliverRecord(t, sc, in, &receipt); err != nil {
			return err
		}
		receipt.Created = len(receipt.Rows)
		_, err = w.tx.Exec(`INSERT INTO intake_deliveries VALUES(?, ?, ?, ?, ?)`,
			in.App, in.Channel, in.Key, digest, canonicalJSON(receipt))
		return err
	})
	return receipt, err
}

func (w *writeTx) deliverRecord(t *Type, sc Scope, in IntakeDelivery, receipt *IntakeReceipt) error {
	set := make(map[string]any, len(in.Set)+1)
	maps.Copy(set, in.Set)
	if in.Form != nil {
		if err := w.checkIntakeForm(t, *in.Form, set); err != nil {
			return err
		}
	}
	var blobs []string
	if len(in.Files) > 0 {
		field := t.field(in.FileField)
		if field == nil || (field.Type != TypeFile && field.Type != TypeFiles) || (field.Type == TypeFile && len(in.Files) != 1) {
			return fieldErr(in.FileField, "the attachment field cannot accept these files")
		}
		values := make([]any, 0, len(in.Files))
		for _, file := range in.Files {
			blob := fmt.Sprintf("sha256:%x", sha256.Sum256(file.Bytes))
			if _, err := w.tx.Exec(`INSERT OR IGNORE INTO intake_blobs VALUES(?, ?)`, blob, file.Bytes); err != nil {
				return err
			}
			values = append(values, map[string]any{"blob": blob, "name": file.Name, "mime": file.MIME})
			blobs = append(blobs, blob)
		}
		set[in.FileField] = values
		if field.Type == TypeFile {
			set[in.FileField] = values[0]
		}
	}
	r, err := w.createOne(t, sc, set, OpCreate)
	if err != nil {
		return err
	}
	for _, blob := range blobs {
		if _, err := w.tx.Exec(`INSERT OR IGNORE INTO intake_attachments VALUES(?, ?, ?)`,
			r.ID, t.field(in.FileField).ID, blob); err != nil {
			return err
		}
	}
	receipt.Rows = append(receipt.Rows, r.ID)
	if in.Work != nil {
		work := *in.Work
		work.Type, work.Record = in.Type, r.ID
		item, err := w.createWorkItem(sc, work)
		if err != nil {
			return err
		}
		receipt.WorkItem = item.ID
	}
	return nil
}

// IntakeFiles loads only attachments that still appear in the caller's scoped
// view. A file id copied into an unrelated record grants no access to bytes.
func (s *Store) IntakeFiles(sc Scope, typ, id string) ([]IntakeFile, error) {
	s.visibility.RLock()
	defer s.visibility.RUnlock()
	t, err := s.Type(sc, typ)
	if err != nil {
		return nil, err
	}
	row, err := s.Get(sc, typ, id, false)
	if err != nil {
		return nil, err
	}
	attachments, err := s.rdb.Query(`SELECT field, blob FROM intake_attachments WHERE record = ? ORDER BY field, blob`, id)
	if err != nil {
		return nil, err
	}
	type ref struct{ field, blob string }
	var refs []ref
	for attachments.Next() {
		var r ref
		if err := attachments.Scan(&r.field, &r.blob); err != nil {
			attachments.Close()
			return nil, err
		}
		refs = append(refs, r)
	}
	err = attachments.Err()
	attachments.Close()
	if err != nil {
		return nil, err
	}
	var out []IntakeFile
	for _, r := range refs {
		field := t.byID[r.field]
		if field == nil || field.Hidden || field.Masked {
			continue
		}
		values, ok := row[field.Name].([]any)
		if !ok {
			values = []any{row[field.Name]}
		}
		for _, value := range values {
			v, _ := value.(map[string]any)
			if v["blob"] != r.blob || v["masks"] != nil {
				continue
			}
			file := IntakeFile{}
			file.Name, _ = v["name"].(string)
			file.MIME, _ = v["mime"].(string)
			if err := s.rdb.QueryRow(`SELECT content FROM intake_blobs WHERE id = ?`, r.blob).Scan(&file.Bytes); err != nil {
				return nil, err
			}
			out = append(out, file)
			break
		}
	}
	return out, nil
}

// checkIntakeForm evaluates declared field rules with the same coercion and
// condition language the writer uses for the record itself.
func (w *writeTx) checkIntakeForm(t *Type, form appschema.ProcessForm, set map[string]any) error {
	values, err := w.prepareSet(t, set, prepared{lenient: true})
	if err != nil {
		return err
	}
	active := map[string]bool{}
	matches := func(expr string) (bool, error) {
		if expr == "" {
			return true, nil
		}
		env, err := exprEnv(t, w.s.now, w.schema.person)
		if err != nil {
			return false, err
		}
		p, err := compile(env, expr)
		if err != nil {
			return false, err
		}
		return p.evalBool(t, values, nil), nil
	}
	for _, step := range form.Steps {
		on, err := matches(step.When)
		if err != nil {
			return err
		}
		if !on {
			continue
		}
		for _, field := range step.Fields {
			on, err := matches(field.When)
			if err != nil {
				return err
			}
			if !on {
				continue
			}
			f := t.field(field.Field)
			if f == nil || f.Type == TypeFile || f.Type == TypeFiles {
				continue
			}
			active[field.Field] = true
			if field.Required && values[f.Name] == nil {
				return fieldErr(field.Field, "%s is required", field.Field)
			}
		}
	}
	for _, name := range sortedKeys(set) {
		if !active[name] {
			return fieldErr(name, "%s is not available on this form", name)
		}
	}
	// Pass normalized input to the create so browser and JSON callers agree.
	for name := range set {
		set[name] = values[t.field(name).Name]
	}
	return nil
}
