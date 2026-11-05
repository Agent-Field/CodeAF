package serve

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// setJobContext uses the model edit's caller rule and the version read by the
// person. The store checks it again under its lock before replacing attachments.
func (h *Handler) setJobContext(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.JobContextRequest
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Expect == nil || body.Expect.Version == nil || *body.Expect.Version < 1 {
		h.fail(w, http.StatusUnprocessableEntity, "expect.version is required: send the version of the job the person read")
		return
	}
	if err := validateJobContext(body.Context); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if !h.fresh(w, r, it, "context", body.Expect) {
		return
	}
	if h.failed(w, contextEditable(it)) {
		return
	}
	err := h.recordAction(r, it, record.ContextSet, fmt.Sprintf("%d attachments", len(body.Context)), func() error {
		return h.config.Store.UpdateExpected(it.ID, *body.Expect.Version, func(cur *factory.Item) error {
			if err := contextEditable(*cur); err != nil {
				return err
			}
			cur.Context = slices.Clone(body.Context)
			return nil
		})
	})
	h.finish(w, err)
}

func contextEditable(it factory.Item) error {
	if it.Stream != nil {
		return factory.EditRefusal{Cause: fmt.Errorf("The job has started; its steps already read their context.")}
	}
	if it.State != factory.StateNew && it.State != factory.StateQueued {
		return factory.EditRefusal{Cause: fmt.Errorf("Context can only be set in Backlog or queued before the job's first run.")}
	}
	return nil
}

func validateJobContext(attachments []factory.JobContext) error {
	if attachments == nil {
		return fmt.Errorf("context is required: send [] to clear attachments")
	}
	if len(attachments) > factory.MaxJobContextItems {
		return fmt.Errorf("context takes at most %d items", factory.MaxJobContextItems)
	}
	total := 0
	for i := range attachments {
		c := &attachments[i]
		c.Title = strings.TrimSpace(c.Title)
		if n := utf8.RuneCountInString(c.Title); n < 1 || n > factory.MaxJobContextTitle {
			return fmt.Errorf("context title must be 1–%d characters after trimming", factory.MaxJobContextTitle)
		}
		if utf8.RuneCountInString(c.Source) > factory.MaxJobContextSource {
			return fmt.Errorf("context source must be at most %d characters", factory.MaxJobContextSource)
		}
		if strings.TrimSpace(c.Markdown) == "" {
			return fmt.Errorf("context markdown must be non-empty")
		}
		total += utf8.RuneCountInString(c.Markdown)
	}
	if total > factory.MaxJobContext {
		return fmt.Errorf("context markdown must total at most %d characters", factory.MaxJobContext)
	}
	return nil
}
