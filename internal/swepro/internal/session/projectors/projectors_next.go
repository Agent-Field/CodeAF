// Next-generation working-memory projectors — port of
// src/session/projectors-next.ts:1-203 (swe-pro 3b25a1a). The raw JSON reducer
// below embeds the narrow SessionMessageUpdater behavior used by that module;
// no unrelated v2 session service is pulled into this package.
package projectors

import (
	"context"
	"database/sql"
	"errors"
)

const (
	EventNextAgentSwitched     = "session.next.agent.switched"
	EventNextModelSwitched     = "session.next.model.switched"
	EventNextPrompted          = "session.next.prompted"
	EventNextSynthetic         = "session.next.synthetic"
	EventNextShellStarted      = "session.next.shell.started"
	EventNextShellEnded        = "session.next.shell.ended"
	EventNextStepStarted       = "session.next.step.started"
	EventNextStepEnded         = "session.next.step.ended"
	EventNextStepFailed        = "session.next.step.failed"
	EventNextTextStarted       = "session.next.text.started"
	EventNextTextDelta         = "session.next.text.delta"
	EventNextTextEnded         = "session.next.text.ended"
	EventNextToolInputStarted  = "session.next.tool.input.started"
	EventNextToolInputDelta    = "session.next.tool.input.delta"
	EventNextToolInputEnded    = "session.next.tool.input.ended"
	EventNextToolCalled        = "session.next.tool.called"
	EventNextToolSuccess       = "session.next.tool.success"
	EventNextToolFailed        = "session.next.tool.failed"
	EventNextReasoningStarted  = "session.next.reasoning.started"
	EventNextReasoningDelta    = "session.next.reasoning.delta"
	EventNextReasoningEnded    = "session.next.reasoning.ended"
	EventNextRetried           = "session.next.retried"
	EventNextCompactionStarted = "session.next.compaction.started"
	EventNextCompactionDelta   = "session.next.compaction.delta"
	EventNextCompactionEnded   = "session.next.compaction.ended"
)

func nextProjectorTypes() []string {
	return []string{
		EventNextAgentSwitched,
		EventNextModelSwitched,
		EventNextPrompted,
		EventNextSynthetic,
		EventNextShellStarted,
		EventNextShellEnded,
		EventNextStepStarted,
		EventNextStepEnded,
		EventNextStepFailed,
		EventNextTextStarted,
		EventNextTextDelta,
		EventNextTextEnded,
		EventNextToolInputStarted,
		EventNextToolInputDelta,
		EventNextToolInputEnded,
		EventNextToolCalled,
		EventNextToolSuccess,
		EventNextToolFailed,
		EventNextReasoningStarted,
		EventNextReasoningDelta,
		EventNextReasoningEnded,
		EventNextRetried,
		EventNextCompactionStarted,
		EventNextCompactionDelta,
		EventNextCompactionEnded,
	}
}

func isNextProjector(eventType string) bool {
	for _, candidate := range nextProjectorTypes() {
		if candidate == eventType {
			return true
		}
	}
	return false
}

func (s *Store) projectNext(
	ctx context.Context,
	tx *sql.Tx,
	event Event,
	data jsonValue,
) error {
	switch event.Type {
	case EventNextAgentSwitched:
		if err := s.updateSessionSwitch(ctx, tx, data, "agent"); err != nil {
			return err
		}
	case EventNextModelSwitched:
		if err := s.updateSessionSwitch(ctx, tx, data, "model"); err != nil {
			return err
		}
	case EventNextTextDelta, EventNextToolInputDelta, EventNextReasoningDelta, EventNextCompactionDelta:
		return nil
	}
	return s.updateNextMessage(ctx, tx, event, data)
}

func (s *Store) updateSessionSwitch(
	ctx context.Context,
	tx *sql.Tx,
	data jsonValue,
	field string,
) error {
	sessionID, _ := stringField(data, "sessionID")
	timestamp, _ := objectField(data, "timestamp")
	value, _ := objectField(data, field)
	driverValue, err := sqlValue(value, field == "model")
	if err != nil {
		return err
	}
	timeValue, err := sqlValue(timestamp, false)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		"UPDATE session SET "+field+" = ?, time_updated = ? WHERE id = ?",
		driverValue, timeValue, sessionID,
	)
	return err
}

func (s *Store) updateNextMessage(
	ctx context.Context,
	tx *sql.Tx,
	event Event,
	data jsonValue,
) error {
	sessionID, _ := stringField(data, "sessionID")
	currentAssistant, assistantOK, err := currentAssistant(ctx, tx, sessionID)
	if err != nil {
		return err
	}

	switch event.Type {
	case EventNextAgentSwitched:
		message := newJSONObject()
		copyField(message, "agent", data, "agent")
		message.set("time", createdTime(data))
		return s.appendSessionMessage(ctx, tx, event.ID, sessionID, "agent-switched", message)

	case EventNextModelSwitched:
		message := newJSONObject()
		copyField(message, "model", data, "model")
		message.set("time", createdTime(data))
		return s.appendSessionMessage(ctx, tx, event.ID, sessionID, "model-switched", message)

	case EventNextPrompted:
		prompt, _ := objectField(data, "prompt")
		message := newJSONObject()
		copyField(message, "text", prompt, "text")
		copyField(message, "files", prompt, "files")
		copyField(message, "agents", prompt, "agents")
		message.set("time", createdTime(data))
		return s.appendSessionMessage(ctx, tx, event.ID, sessionID, "user", message)

	case EventNextSynthetic:
		message := newJSONObject()
		copyField(message, "sessionID", data, "sessionID")
		copyField(message, "text", data, "text")
		message.set("time", createdTime(data))
		return s.appendSessionMessage(ctx, tx, event.ID, sessionID, "synthetic", message)

	case EventNextShellStarted:
		message := newJSONObject()
		copyField(message, "callID", data, "callID")
		copyField(message, "command", data, "command")
		message.set("output", stringJSON(""))
		message.set("time", createdTime(data))
		return s.appendSessionMessage(ctx, tx, event.ID, sessionID, "shell", message)

	case EventNextShellEnded:
		callID, _ := stringField(data, "callID")
		shell, ok, err := currentShell(ctx, tx, sessionID, callID)
		if err != nil || !ok {
			return err
		}
		copyField(shell.data, "output", data, "output")
		setNested(shell.data, "time", "completed", eventTimestamp(data))
		return s.updateSessionMessage(ctx, tx, sessionID, shell)

	case EventNextStepStarted:
		if assistantOK {
			setNested(currentAssistant.data, "time", "completed", eventTimestamp(data))
			if err := s.updateSessionMessage(ctx, tx, sessionID, currentAssistant); err != nil {
				return err
			}
		}
		message := newJSONObject()
		copyField(message, "agent", data, "agent")
		copyField(message, "model", data, "model")
		message.set("time", createdTime(data))
		message.set("content", arrayJSON([]jsonValue{}))
		if snapshot, ok := objectField(data, "snapshot"); ok && jsonTruthy(snapshot) {
			value := newJSONObject()
			value.set("start", snapshot)
			message.set("snapshot", objectJSON(value))
		}
		return s.appendSessionMessage(ctx, tx, event.ID, sessionID, "assistant", message)

	case EventNextStepEnded:
		if !assistantOK {
			return nil
		}
		setNested(currentAssistant.data, "time", "completed", eventTimestamp(data))
		copyField(currentAssistant.data, "finish", data, "finish")
		copyField(currentAssistant.data, "cost", data, "cost")
		copyField(currentAssistant.data, "tokens", data, "tokens")
		if snapshot, ok := objectField(data, "snapshot"); ok && jsonTruthy(snapshot) {
			current, exists := currentAssistant.data.get("snapshot")
			var value *jsonObjectValue
			if exists && current.kind == jsonObject {
				value = current.o.clone()
			} else {
				value = newJSONObject()
			}
			value.set("end", snapshot)
			currentAssistant.data.set("snapshot", objectJSON(value))
		}
		return s.updateSessionMessage(ctx, tx, sessionID, currentAssistant)

	case EventNextStepFailed:
		if !assistantOK {
			return nil
		}
		setNested(currentAssistant.data, "time", "completed", eventTimestamp(data))
		currentAssistant.data.set("finish", stringJSON("error"))
		copyField(currentAssistant.data, "error", data, "error")
		return s.updateSessionMessage(ctx, tx, sessionID, currentAssistant)

	case EventNextTextStarted:
		if !assistantOK {
			return nil
		}
		item := newJSONObject()
		item.set("type", stringJSON("text"))
		item.set("text", stringJSON(""))
		appendContent(currentAssistant.data, objectJSON(item))
		return s.updateSessionMessage(ctx, tx, sessionID, currentAssistant)

	case EventNextTextEnded:
		if !assistantOK {
			return nil
		}
		if item, ok := latestContent(currentAssistant.data, "text", ""); ok {
			copyField(item, "text", data, "text")
		}
		return s.updateSessionMessage(ctx, tx, sessionID, currentAssistant)

	case EventNextToolInputStarted:
		if !assistantOK {
			return nil
		}
		item := newJSONObject()
		item.set("type", stringJSON("tool"))
		copyField(item, "id", data, "callID")
		copyField(item, "name", data, "name")
		item.set("time", createdTime(data))
		state := newJSONObject()
		state.set("status", stringJSON("pending"))
		state.set("input", stringJSON(""))
		item.set("state", objectJSON(state))
		appendContent(currentAssistant.data, objectJSON(item))
		return s.updateSessionMessage(ctx, tx, sessionID, currentAssistant)

	case EventNextToolInputEnded, EventNextRetried:
		return nil

	case EventNextToolCalled:
		if !assistantOK {
			return nil
		}
		callID, _ := stringField(data, "callID")
		if item, ok := latestContent(currentAssistant.data, "tool", callID); ok {
			copyField(item, "provider", data, "provider")
			setNested(item, "time", "ran", eventTimestamp(data))
			state := newJSONObject()
			state.set("status", stringJSON("running"))
			copyField(state, "input", data, "input")
			state.set("structured", objectJSON(newJSONObject()))
			state.set("content", arrayJSON([]jsonValue{}))
			item.set("state", objectJSON(state))
		}
		return s.updateSessionMessage(ctx, tx, sessionID, currentAssistant)

	case EventNextToolSuccess:
		if !assistantOK {
			return nil
		}
		callID, _ := stringField(data, "callID")
		if item, ok := latestContent(currentAssistant.data, "tool", callID); ok {
			state, stateOK := item.get("state")
			status, _ := stringField(state, "status")
			if stateOK && status == "running" {
				copyField(item, "provider", data, "provider")
				setNested(item, "time", "completed", eventTimestamp(data))
				next := newJSONObject()
				next.set("status", stringJSON("completed"))
				if input, exists := objectField(state, "input"); exists {
					next.set("input", input)
				}
				copyField(next, "structured", data, "structured")
				copyField(next, "content", data, "content")
				item.set("state", objectJSON(next))
			}
		}
		return s.updateSessionMessage(ctx, tx, sessionID, currentAssistant)

	case EventNextToolFailed:
		if !assistantOK {
			return nil
		}
		callID, _ := stringField(data, "callID")
		if item, ok := latestContent(currentAssistant.data, "tool", callID); ok {
			state, stateOK := item.get("state")
			status, _ := stringField(state, "status")
			if stateOK && status == "running" {
				copyField(item, "provider", data, "provider")
				setNested(item, "time", "completed", eventTimestamp(data))
				next := newJSONObject()
				next.set("status", stringJSON("error"))
				copyField(next, "error", data, "error")
				if input, exists := objectField(state, "input"); exists {
					next.set("input", input)
				}
				if structured, exists := objectField(state, "structured"); exists {
					next.set("structured", structured)
				}
				if content, exists := objectField(state, "content"); exists {
					next.set("content", content)
				}
				item.set("state", objectJSON(next))
			}
		}
		return s.updateSessionMessage(ctx, tx, sessionID, currentAssistant)

	case EventNextReasoningStarted:
		if !assistantOK {
			return nil
		}
		item := newJSONObject()
		item.set("type", stringJSON("reasoning"))
		copyField(item, "id", data, "reasoningID")
		item.set("text", stringJSON(""))
		appendContent(currentAssistant.data, objectJSON(item))
		return s.updateSessionMessage(ctx, tx, sessionID, currentAssistant)

	case EventNextReasoningEnded:
		if !assistantOK {
			return nil
		}
		reasoningID, _ := stringField(data, "reasoningID")
		if item, ok := latestContent(currentAssistant.data, "reasoning", reasoningID); ok {
			copyField(item, "text", data, "text")
		}
		return s.updateSessionMessage(ctx, tx, sessionID, currentAssistant)

	case EventNextCompactionStarted:
		message := newJSONObject()
		copyField(message, "reason", data, "reason")
		message.set("summary", stringJSON(""))
		message.set("time", createdTime(data))
		return s.appendSessionMessage(ctx, tx, event.ID, sessionID, "compaction", message)

	case EventNextCompactionEnded:
		compaction, ok, err := currentCompaction(ctx, tx, sessionID)
		if err != nil || !ok {
			return err
		}
		copyField(compaction.data, "summary", data, "text")
		copyField(compaction.data, "include", data, "include")
		return s.updateSessionMessage(ctx, tx, sessionID, compaction)
	}
	return nil
}

func eventTimestamp(data jsonValue) jsonValue {
	value, _ := objectField(data, "timestamp")
	return value
}

func createdTime(data jsonValue) jsonValue {
	time := newJSONObject()
	time.set("created", eventTimestamp(data))
	return objectJSON(time)
}

func jsonTruthy(value jsonValue) bool {
	switch value.kind {
	case jsonNull:
		return false
	case jsonBool:
		return value.b
	case jsonNumber:
		return value.n != 0
	case jsonString:
		return value.s != ""
	}
	return true
}

func setNested(object *jsonObjectValue, parent, field string, value jsonValue) {
	current, ok := object.get(parent)
	if !ok || current.kind != jsonObject {
		child := newJSONObject()
		child.set(field, value)
		object.set(parent, objectJSON(child))
		return
	}
	current.o.set(field, value)
}

func appendContent(assistant *jsonObjectValue, item jsonValue) {
	content, ok := assistant.get("content")
	if !ok || content.kind != jsonArray {
		content = arrayJSON([]jsonValue{})
	}
	content.a = append(content.a, item)
	assistant.set("content", content)
}

func latestContent(assistant *jsonObjectValue, itemType, id string) (*jsonObjectValue, bool) {
	content, ok := assistant.get("content")
	if !ok || content.kind != jsonArray {
		return nil, false
	}
	for index := len(content.a) - 1; index >= 0; index-- {
		item := &content.a[index]
		if item.kind != jsonObject {
			continue
		}
		currentType, _ := stringField(*item, "type")
		currentID, _ := stringField(*item, "id")
		if currentType == itemType && (id == "" || currentID == id) {
			return item.o, true
		}
	}
	return nil, false
}

type selectedMessage struct {
	id      string
	msgType string
	data    *jsonObjectValue
}

func currentAssistant(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
) (selectedMessage, bool, error) {
	messages, err := selectMessages(ctx, tx, sessionID, "assistant")
	if err != nil {
		return selectedMessage{}, false, err
	}
	for _, message := range messages {
		timeValue, _ := message.data.get("time")
		_, exists := objectField(timeValue, "completed")
		// decodeMessage has already transformed the epoch number into a
		// DateTime object, so epoch 0 is truthy here just like every other
		// completed timestamp.
		if !exists {
			return message, true, nil
		}
	}
	return selectedMessage{}, false, nil
}

func currentCompaction(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
) (selectedMessage, bool, error) {
	messages, err := selectMessages(ctx, tx, sessionID, "compaction")
	if err != nil || len(messages) == 0 {
		return selectedMessage{}, false, err
	}
	return messages[0], true, nil
}

func currentShell(
	ctx context.Context,
	tx *sql.Tx,
	sessionID, callID string,
) (selectedMessage, bool, error) {
	messages, err := selectMessages(ctx, tx, sessionID, "shell")
	if err != nil {
		return selectedMessage{}, false, err
	}
	for _, message := range messages {
		current, _ := stringField(objectJSON(message.data), "callID")
		if current == callID {
			return message, true, nil
		}
	}
	return selectedMessage{}, false, nil
}

func selectMessages(
	ctx context.Context,
	tx *sql.Tx,
	sessionID, messageType string,
) ([]selectedMessage, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, data FROM session_message
		WHERE session_id = ? AND type = ? ORDER BY id DESC`, sessionID, messageType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []selectedMessage{}
	for rows.Next() {
		var id, encoded string
		if err := rows.Scan(&id, &encoded); err != nil {
			return nil, err
		}
		value, err := parseOrderedJSON([]byte(encoded))
		if err != nil {
			return nil, err
		}
		if value.kind != jsonObject {
			return nil, errors.New("projectors: session_message data must be an object")
		}
		normalized := normalizeMessageData(messageType, value.o)
		out = append(out, selectedMessage{id: id, msgType: messageType, data: normalized})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeMessageData(messageType string, input *jsonObjectValue) *jsonObjectValue {
	switch messageType {
	case "shell":
		return orderedSubset(input, []string{"time", "callID", "command", "output"})
	case "assistant":
		out := orderedSubset(input, []string{
			"time", "agent", "model", "content", "snapshot", "finish", "cost", "tokens", "error",
		})
		if content, ok := out.get("content"); ok && content.kind == jsonArray {
			for index := range content.a {
				content.a[index] = normalizeAssistantContent(content.a[index])
			}
			out.set("content", content)
		}
		return out
	case "compaction":
		return orderedSubset(input, []string{"reason", "summary", "include", "time"})
	default:
		return input.clone()
	}
}

func normalizeAssistantContent(value jsonValue) jsonValue {
	if value.kind != jsonObject {
		return value
	}
	itemType, _ := stringField(value, "type")
	switch itemType {
	case "text":
		return objectJSON(orderedSubset(value.o, []string{"type", "text"}))
	case "reasoning":
		return objectJSON(orderedSubset(value.o, []string{"type", "id", "text"}))
	case "tool":
		out := orderedSubset(value.o, []string{"type", "id", "name", "provider", "state", "time"})
		if state, ok := out.get("state"); ok && state.kind == jsonObject {
			status, _ := stringField(state, "status")
			var fields []string
			switch status {
			case "pending":
				fields = []string{"status", "input"}
			case "running":
				fields = []string{"status", "input", "structured", "content"}
			case "completed":
				fields = []string{"status", "input", "attachments", "content", "structured"}
			case "error":
				fields = []string{"status", "input", "content", "structured", "error"}
			}
			if fields != nil {
				out.set("state", objectJSON(orderedSubset(state.o, fields)))
			}
		}
		if timing, ok := out.get("time"); ok && timing.kind == jsonObject {
			out.set("time", objectJSON(orderedSubset(
				timing.o,
				[]string{"created", "ran", "completed", "pruned"},
			)))
		}
		return objectJSON(out)
	default:
		return value
	}
}

func orderedSubset(input *jsonObjectValue, fields []string) *jsonObjectValue {
	out := newJSONObject()
	for _, field := range fields {
		if value, ok := input.get(field); ok {
			out.set(field, value)
		}
	}
	return out
}

func (s *Store) appendSessionMessage(
	ctx context.Context,
	tx *sql.Tx,
	id, sessionID, messageType string,
	data *jsonObjectValue,
) error {
	timeValue, _ := data.get("time")
	created, _ := objectField(timeValue, "created")
	createdValue, err := sqlValue(created, false)
	if err != nil {
		return err
	}
	encoded, err := objectJSON(data).compactString()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO session_message
		(id, session_id, type, time_created, time_updated, data)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, sessionID, messageType, createdValue, s.now(), encoded,
	)
	return err
}

func (s *Store) updateSessionMessage(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
	message selectedMessage,
) error {
	encoded, err := objectJSON(message.data).compactString()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE session_message SET data = ?, time_updated = ?
		WHERE id = ? AND session_id = ? AND type = ?`,
		encoded, s.now(), message.id, sessionID, message.msgType,
	)
	return err
}
