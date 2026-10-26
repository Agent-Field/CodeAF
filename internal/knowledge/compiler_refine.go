package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
)

// refineLearning spends bounded local-evidence work only after inputs change.
// It never introduces a new source fact or launches external research.
func refineLearning(vault, learning, previous document, model compilerModel) (document, error) {
	wakeups := document{}
	processed := map[string]map[string]bool{}
	for _, s := range items(learning["subjects"]) {
		for _, q := range items(s["questions"]) {
			id := questionID(s, q)
			processed[id] = map[string]bool{}
			for _, source := range stringsValue(q["processed_wakes"]) {
				processed[id][source] = true
			}
		}
	}
	rows, err := model.db.Query("SELECT id,source,reason FROM wakes WHERE partition='profile' ORDER BY id,source")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, source, reason string
		if err = rows.Scan(&id, &source, &reason); err != nil {
			rows.Close()
			return nil, err
		}
		if !processed[id][source] {
			wakeups[id] = append(items(wakeups[id]), document{"source": source, "reason": reason})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if !hasPendingRefinement(learning) && len(wakeups) == 0 {
		return learning, nil
	}
	digest := identity([]any{vault["sources"], vault["claims"]})
	result := clone(learning)
	guides := object(result["guides"])
	type refinementJob struct {
		subject, previous, guide document
		err                      error
	}
	jobs := []refinementJob{}
	for _, subject := range items(result["subjects"]) {
		if !flag(subject["needs_editorial_update"]) || subject["refinement_attempt_hash"] == refinementSubjectHash(subject) {
			continue
		}
		jobs = append(jobs, refinementJob{subject: clone(subject), previous: clone(object(guides[textValue(subject["id"])]))})
		if len(jobs) == subjectConcurrency {
			break
		}
	}
	ctx, cancel := context.WithCancel(model.ctx)
	defer cancel()
	var workers sync.WaitGroup
	for i := range jobs {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			workerModel := model
			workerModel.ctx = ctx
			job := &jobs[i]
			job.guide, job.err = learnSubject(vault, job.subject, job.previous, workerModel, "Refresh this subject using the changed local evidence.", 4, 18000)
			if job.err != nil {
				cancel()
			}
		}(i)
	}
	workers.Wait()
	for _, job := range jobs {
		if job.err != nil && !errors.Is(job.err, context.Canceled) {
			return nil, job.err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Workers own isolated drafts; only the coordinator changes learning state.
	subjects := index(items(result["subjects"]))
	for _, job := range jobs {
		id := textValue(job.subject["id"])
		subject := subjects[id]
		guide := job.guide
		guides[id] = guide
		if len(items(guide["sections"])) > 0 {
			subject["sections"] = items(guide["sections"])
			subject["state"] = "explained"
			represented := map[string]bool{}
			for _, section := range items(subject["sections"]) {
				for _, p := range items(section["paragraphs"]) {
					for _, cid := range stringsValue(p["claims"]) {
						represented[cid] = true
					}
				}
			}
			pending := []string{}
			for _, cid := range stringsValue(subject["editorial_pending_claims"]) {
				if !represented[cid] {
					pending = append(pending, cid)
				}
			}
			subject["editorial_pending_claims"] = pending
			subject["needs_editorial_update"] = len(pending) > 0
		}
		subject["refinement_attempt_hash"] = refinementSubjectHash(subject)
	}
	result["guides"] = guides
	if result["refinement_evidence_hash"] != digest || len(wakeups) > 0 {
		result, err = curateQuestions(vault, result, previous, model, "", 2, 8)
		if err != nil {
			return nil, err
		}
		result = wakeQuestions(result, wakeups, vault)
		result, err = maintainKnowledge(vault, result, previous, model, 4, "", false)
		if err != nil {
			return nil, err
		}
		result["refinement_evidence_hash"] = digest
	}
	return result, nil
}
func storeQuestionWatches(tx *sql.Tx, learning document) error {
	if _, err := tx.Exec("DELETE FROM watch_terms"); err != nil {
		return err
	}
	stop := words("the and what how does when which with from that this are for")
	for _, subject := range items(learning["subjects"]) {
		for _, question := range items(subject["questions"]) {
			id := questionID(subject, question)
			text := strings.Join(stringsValue(question["search_terms"]), " ") + " " + textValue(question["question"]) + " " + textValue(subject["scope"])
			for _, term := range questionTerms(text) {
				if !stop[term] {
					if _, err := tx.Exec("INSERT OR IGNORE INTO watch_terms(term,question) VALUES(?,?)", term, id); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
func wakeFromSource(tx *sql.Tx, source document) error {
	terms := questionTerms(textValue(source["text"]))
	matches := map[string][]string{}
	for start := 0; start < len(terms); start += 200 {
		batch := terms[start:min(len(terms), start+200)]
		marks := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, term := range batch {
			marks[i] = "?"
			args[i] = term
		}
		rows, err := tx.Query("SELECT question,term FROM watch_terms WHERE term IN ("+strings.Join(marks, ",")+")", args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, term string
			if err = rows.Scan(&id, &term); err != nil {
				rows.Close()
				return err
			}
			matches[id] = append(matches[id], term)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	for id, matched := range matches {
		if len(matched) >= 2 {
			if _, err := tx.Exec("INSERT OR IGNORE INTO wakes(partition,id,source,reason) VALUES('profile',?,?,?)", id, textValue(source["id"]), mustJSON(matched)); err != nil {
				return err
			}
		}
	}
	return nil
}

func refinementSubjectHash(subject document) string {
	questions := []document{}
	for _, q := range items(subject["questions"]) {
		questions = append(questions, choose(q, "question", "claims"))
	}
	return identity([]any{subject["id"], subject["title"], subject["scope"], subject["claims"], subject["editorial_pending_claims"], subject["conflicts"], questions})
}
func hasPendingRefinement(learning document) bool {
	for _, subject := range items(learning["subjects"]) {
		if flag(subject["needs_editorial_update"]) && subject["refinement_attempt_hash"] != refinementSubjectHash(subject) {
			return true
		}
	}
	return false
}
