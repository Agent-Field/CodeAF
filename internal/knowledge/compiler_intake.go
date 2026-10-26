package knowledge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SourceWindow struct{ Start, Length int }
type ProposalReceipt struct{ New, Duplicates, Rejected int }

// Revisit reopens explicitly named immutable sources, optionally supplying exact spans.
func (v *Vault) Revisit(ids []string, windows map[string]SourceWindow) error {
	if !v.Enabled() {
		return ErrDisabled
	}
	ids = unique(ids)
	if len(ids) == 0 || len(ids) > 64 {
		return errors.New("revisit requires one to 64 source IDs")
	}
	for id := range windows {
		if !contains(ids, id) {
			return errors.New("window source must be explicitly revisited")
		}
	}
	db, err := v.database()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var payload string
		if err = tx.QueryRow("SELECT payload FROM events WHERE partition='profile' AND id=?", id).Scan(&payload); err == sql.ErrNoRows {
			return errors.New("cannot revisit a source outside this host profile")
		} else if err != nil {
			return err
		}
		var source document
		if err = json.Unmarshal([]byte(payload), &source); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM source_windows WHERE partition='profile' AND id=?", id); err != nil {
			return err
		}
		if w, ok := windows[id]; ok {
			if w.Start < 0 || w.Length < 1 || w.Length > 24000 || w.Start+w.Length > runeLength(textValue(source["text"])) {
				return errors.New("invalid exact source window")
			}
			if _, err = tx.Exec("INSERT INTO source_windows VALUES('profile',?,?,?)", id, w.Start, w.Length); err != nil {
				return err
			}
		}
		if _, err = tx.Exec("UPDATE events SET status='revisit',arrived=? WHERE partition='profile' AND id=? AND status='done'", float64(time.Now().UnixNano())/1e9, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// StageProposals accepts an optional sidecar only after its assistant response commits.
// The host supplies the proven owner/project partition; evidence stays in that project.
func (v *Vault) StageProposals(committedID string, partition map[string]string, envelope json.RawMessage) (ProposalReceipt, error) {
	var receipt ProposalReceipt
	if !v.Enabled() {
		return receipt, ErrDisabled
	}
	if len(partition) != 2 || partition["owner"] == "" || partition["project"] == "" {
		return receipt, errors.New("host must supply proven owner/project partition keys")
	}
	var input document
	if err := json.Unmarshal(envelope, &input); err != nil {
		return receipt, errors.New("proposal envelope must be a JSON object")
	}
	db, err := v.database()
	if err != nil {
		return receipt, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	readSource := func(id string) (document, error) {
		var raw string
		err := tx.QueryRow("SELECT payload FROM events WHERE partition='profile' AND id=?", id).Scan(&raw)
		if err != nil {
			return nil, err
		}
		var s document
		err = json.Unmarshal([]byte(raw), &s)
		return s, err
	}
	committed, err := readSource(committedID)
	if err != nil {
		return receipt, err
	}
	project := func(s document) string { return textValue(object(object(s["provenance"])["locator"])["project"]) }
	if committed["role"] != "assistant" || project(committed) != partition["project"] {
		return receipt, errors.New("committed assistant response does not belong to the host project")
	}
	sources := document{}
	prior := []document{}
	targets := []string{}
	raws := items(input["knowledge_delta"])
	for _, raw := range raws[:min(4, len(raws))] {
		for _, e := range items(raw["evidence"]) {
			id := textValue(e["source"])
			if id == "" || id == "self" {
				continue
			}
			s, readErr := readSource(id)
			if readErr == sql.ErrNoRows {
				continue
			}
			if readErr != nil {
				return receipt, readErr
			}
			if project(s) == partition["project"] {
				sources[id] = s
			}
		}
		target := any(nil)
		if raw["op"] == "correct" {
			target = raw["target"]
			id := textValue(target)
			if id != "" {
				var known string
				readErr := tx.QueryRow("SELECT id FROM claim_keys WHERE partition='profile' AND id=?", id).Scan(&known)
				if readErr == nil {
					var claimRaw string
					claimErr := tx.QueryRow("SELECT c.value FROM publications p,json_each(p.payload,'$.claims') c WHERE p.partition='profile' AND json_extract(c.value,'$.id')=? LIMIT 1", id).Scan(&claimRaw)
					if claimErr != nil && claimErr != sql.ErrNoRows {
						return receipt, claimErr
					}
					if claimErr == nil {
						var claim document
						if err = json.Unmarshal([]byte(claimRaw), &claim); err != nil {
							return receipt, err
						}
						permitted := len(items(claim["evidence"])) > 0
						for _, ev := range items(claim["evidence"]) {
							source, sourceErr := readSource(textValue(ev["source"]))
							if sourceErr != nil && sourceErr != sql.ErrNoRows {
								return receipt, sourceErr
							}
							if sourceErr == sql.ErrNoRows || project(source) != partition["project"] {
								permitted = false
							}
						}
						if permitted {
							targets = append(targets, id)
						}
					}
				} else if readErr != sql.ErrNoRows {
					return receipt, readErr
				}
			}
		}
		text := foldText(strings.Join(strings.Fields(textValue(raw["text"])), " "))
		id := identity([]any{partition, raw["op"], text, target})
		var payload string
		readErr := tx.QueryRow("SELECT payload FROM proposals WHERE partition='profile' AND id=?", id).Scan(&payload)
		if readErr == nil {
			var p document
			if err = json.Unmarshal([]byte(payload), &p); err != nil {
				return receipt, err
			}
			if index(prior)[textValue(p["id"])] == nil {
				prior = append(prior, p)
			}
		} else if readErr != sql.ErrNoRows {
			return receipt, readErr
		}
	}
	staged, err := collectProposals(input, committed, sources, document{"owner": partition["owner"], "project": partition["project"]}, document{"proposals": prior}, targets)
	if err != nil {
		return receipt, err
	}
	r := object(staged["receipt"])
	receipt = ProposalReceipt{New: int(number(r["new"])), Duplicates: int(number(r["duplicates"])), Rejected: int(number(r["rejected"]))}
	for _, p := range items(staged["proposals"]) {
		id := textValue(p["id"])
		ev := items(p["evidence"])
		p["evidence"] = ev[:min(8, len(ev))]
		if _, err = tx.Exec("INSERT OR REPLACE INTO proposals VALUES('profile',?,?)", id, mustJSON(p)); err != nil {
			return receipt, err
		}
		if _, err = tx.Exec("DELETE FROM proposal_sources WHERE partition='profile' AND id=?", id); err != nil {
			return receipt, err
		}
		for _, e := range items(p["evidence"]) {
			if _, err = tx.Exec("INSERT OR IGNORE INTO proposal_sources VALUES('profile',?,?)", id, textValue(e["source"])); err != nil {
				return receipt, err
			}
		}
	}
	if receipt.New > 0 {
		if _, err = tx.Exec("UPDATE events SET status='revisit' WHERE partition='profile' AND id=? AND status='done'", committedID); err != nil {
			return receipt, err
		}
	}
	if err = tx.Commit(); err != nil {
		return receipt, fmt.Errorf("stage knowledge proposals: %w", err)
	}
	return receipt, nil
}
