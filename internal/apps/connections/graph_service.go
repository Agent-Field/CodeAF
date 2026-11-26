package connections

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func (s Service) graph(ctx context.Context, c registration) (graphSource, string, error) {
	grant, transcripts, err := s.graphGrant(ctx, c)
	if err != nil {
		return graphSource{}, "", err
	}
	if c.GraphFixture {
		return graphSource{client: graphFixtureClient(), transcripts: true}, grant, nil
	}
	manager, err := s.Microsoft()
	if err != nil {
		return graphSource{}, "", ErrPersonCredential
	}
	client, err := manager.Client(ctx, "microsoft-365")
	if err != nil {
		return graphSource{}, "", ErrPersonCredential
	}
	// The manager reads the grant again to build its client. Recheck before
	// dispatch so a concurrently changed sign-in cannot use an old query fence.
	current, _, err := manager.MicrosoftGrant()
	if err != nil || current != grant {
		return graphSource{}, "", ErrSourcePermission
	}
	return graphSource{client: client, transcripts: transcripts}, grant, nil
}

func (s Service) graphGrant(ctx context.Context, c registration) (string, bool, error) {
	if s.Store == nil {
		return "", false, ErrPersonCredential
	}
	store, err := s.Store()
	if err != nil {
		return "", false, err
	}
	if person := s.person(ctx).ID; person == "" || person != store.Person() {
		return "", false, ErrSourcePermission
	}
	if c.GraphFixture {
		return "fixture/" + c.Binding, true, nil
	}
	if s.Microsoft == nil {
		return "", false, ErrPersonCredential
	}
	manager, err := s.Microsoft()
	if err != nil {
		return "", false, ErrPersonCredential
	}
	grant, transcripts, err := manager.MicrosoftGrant()
	if err != nil {
		return "", false, fmt.Errorf("%w: %s", ErrPersonCredential, err)
	}
	return grant, transcripts, nil
}

func (s Service) checkGraphGrant(ctx context.Context, c registration) error {
	grant, _, err := s.graphGrant(ctx, c)
	if err != nil || grant != c.graphGrant {
		return ErrSourcePermission
	}
	return nil
}

// A tenant-wide denial differs from an individual meeting being inaccessible.
// Remember only the provider's exact tenant-wide denial for the current grant.
var blockedGraphTranscripts sync.Map

func graphTranscriptBlock(c registration) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(c.graphGrant)))
}

func (s Service) graphAvailable(c registration, summary *Summary) {
	grant, available, err := s.graphGrant(context.Background(), c)
	c.graphGrant = grant
	_, blocked := blockedGraphTranscripts.Load(graphTranscriptBlock(c))
	for _, name := range slices.Clone(summary.Reads) {
		if c.Reads[name].Table != "transcripts" || available && !blocked {
			continue
		}
		reason := errTeamsGrant.Error()
		if err != nil {
			reason = err.Error()
		} else if blocked {
			reason = errTeamsDisabled.Error()
		}
		if summary.Unavailable == nil {
			summary.Unavailable = map[string]string{}
		}
		summary.Unavailable[name] = reason
		summary.Reads = slices.DeleteFunc(summary.Reads, func(read string) bool { return read == name })
	}
}

func (s Service) graphRows(ctx context.Context, graph graphSource, c registration, read Read, store *data.Store, sc data.Scope,
	q Query, args map[string]any) (graphPage, error) {
	key, _ := args[read.Key].(string)
	graphBinding, _, err := store.LiveBinding(sc, read.Type, false)
	if err != nil {
		return graphPage{}, err
	}
	var cursorKey string
	if q.Cursor != "" || key == "" {
		descriptor, _ := json.Marshal([]any{c.Binding, q.Read, graphBinding, args})
		cursorKey = string(descriptor)
	}
	cursor, err := openGraphCursor(c.graphGrant, cursorKey, q.Cursor)
	if err != nil {
		return graphPage{}, err
	}
	var meetingID string
	var eventBinding data.TypeBinding
	var eventRead Read
	if read.Table == "transcripts" {
		eventRead = c.Reads[read.GraphMeetingRead]
		var eventType *data.Type
		_, eventBinding, eventType, err = s.bind(c.Config, eventRead, false)
		if err != nil || eventBinding.Common != data.CommonEvent || eventType.HasRowPolicy() {
			return graphPage{}, ErrSourcePermission
		}
		meetingID, _ = args["meeting"].(string)
		if meetingID != "" {
			value, err := effectKey(store, c, eventRead, eventBinding, meetingID)
			if err != nil {
				return graphPage{}, err
			}
			args["meeting"] = value
		}
	}
	p, err := graph.rows(ctx, read, cursor, key, args, q.Limit, false)
	if errors.Is(err, errTeamsDisabled) {
		blockedGraphTranscripts.Store(graphTranscriptBlock(c), true)
	}
	if err != nil {
		return graphPage{}, err
	}
	if read.Table == "transcripts" && meetingID != "" {
		for _, row := range p.rows {
			row["meeting"] = meetingID
		}
	}
	if len(p.rows)+len(p.removed) > q.Limit {
		return graphPage{}, ErrTooLarge
	}
	p.next = sealGraphCursor(c.graphGrant, cursorKey, p.next)
	p.checkpoint = sealGraphCursor(c.graphGrant, cursorKey, p.checkpoint)
	return p, nil
}

func sealGraphCursor(grant, scope, address string) string {
	if address == "" {
		return ""
	}
	signature := hmac.New(sha256.New, []byte(grant))
	_, _ = signature.Write([]byte(scope + "\x00" + address))
	return base64.RawURLEncoding.EncodeToString(append(signature.Sum(nil), address...))
}

func openGraphCursor(grant, scope, sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if len(sealed) > 11000 {
		return "", ErrGraphCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil || len(raw) <= sha256.Size {
		return "", ErrGraphCursor
	}
	address := string(raw[sha256.Size:])
	if !hmac.Equal([]byte(sealed), []byte(sealGraphCursor(grant, scope, address))) {
		return "", ErrGraphCursor
	}
	return address, nil
}

func knownGraphRemovals(store *data.Store, c registration, read Read, binding data.TypeBinding, keys []string) ([]string, error) {
	var removed []string
	for _, key := range keys {
		encoded, err := encodeSourceKey(key)
		if err != nil {
			return nil, err
		}
		id, known, err := store.LookupBoundIdentity(binding, externalKey(c, read, encoded))
		if err != nil {
			return nil, err
		}
		if known {
			removed = append(removed, id)
		}
	}
	return removed, nil
}

func graphContentField(column string) bool { return column == "body" || column == "text" }

// SourceContent labels restricted provider text for callers operating at a
// trusted product door. Text is deliberately absent from row scans and triggers.
type SourceContent struct {
	Text  string           `json:"text"`
	Class schema.DataClass `json:"class"`
}

type ContentAudience uint8

const ContentPerson ContentAudience = 1

// ReadGraphContent loads the text only for an explicitly mapped, accessible
// record. Automated callers have no authority to obtain it through this door.
func (s Service) ReadGraphContent(ctx context.Context, q Query, id string, audience ContentAudience) (SourceContent, error) {
	if audience != ContentPerson {
		return SourceContent{}, fmt.Errorf("%w: Microsoft 365 content contains personal information and cannot leave through an outside harness", ErrSourcePermission)
	}
	c, err := s.scoped(q.App, q.Connection)
	if err != nil {
		return SourceContent{}, err
	}
	read, ok := c.Reads[q.Read]
	if c.Kind != Microsoft365 || !ok || !slices.Contains([]string{"outlook", "drive", "transcripts"}, read.Table) {
		return SourceContent{}, ErrRejected
	}
	store, binding, typ, err := s.bind(c.Config, read, false)
	if err != nil {
		return SourceContent{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	column := "text"
	if read.Table == "outlook" {
		column = "body"
	}
	var field *data.Field
	for name, mapped := range read.Fields {
		if mapped == column {
			field, err = typ.LiveField(name, false)
			break
		}
	}
	sc := data.Scope{App: q.App, Person: s.person(ctx)}
	if err != nil || field == nil || !typ.AllowsLiveSourceContent(field) || typ.HasRowPolicy() ||
		binding.Common != graphCommon(read.Table) {
		store.AuditRecordPolicy(sc, typ, "source_read_refused")
		return SourceContent{}, ErrSourcePermission
	}
	g, grant, err := s.graph(ctx, c)
	if err != nil {
		return SourceContent{}, err
	}
	c.graphGrant = grant
	raw, err := effectKey(store, c, read, binding, id)
	if err != nil {
		return SourceContent{}, err
	}
	key, ok := raw.(string)
	if !ok || key == "" {
		return SourceContent{}, ErrRejected
	}
	if read.Table == "transcripts" {
		meetingRead := c.Reads[read.GraphMeetingRead]
		if _, meetingBinding, meetingType, err := s.bind(c.Config, meetingRead, false); err != nil || meetingBinding.Common != data.CommonEvent || meetingType.HasRowPolicy() {
			return SourceContent{}, ErrSourcePermission
		}
	}
	page, err := g.point(ctx, read, key, true)
	if err != nil {
		return SourceContent{}, err
	}
	if len(page.rows) != 1 {
		return SourceContent{}, ErrSourcePermission
	}
	text, ok := page.rows[0][column].(string)
	if !ok {
		return SourceContent{}, ErrInvalidRows
	}
	if len(text) > maxCell {
		return SourceContent{}, ErrTooLarge
	}
	if err := s.checkGraphGrant(ctx, c); err != nil {
		return SourceContent{}, err
	}
	if _, current, currentType, err := s.bind(c.Config, read, false); err != nil || current != binding || currentType != typ {
		return SourceContent{}, ErrSourcePermission
	}
	if err := store.AuditPolicy(q.App, typ.ID, "source_content_review"); err != nil {
		return SourceContent{}, ErrSourcePermission
	}
	return SourceContent{Text: text, Class: schema.ClassPII}, nil
}

// PersonalSourceClasses protects inputs and previous step results regardless of
// whether an app author chose to classify their own record declarations.
func (s Service) PersonalSourceClasses(app string) ([]schema.DataClass, error) {
	defs, err := s.vault().Definitions()
	if err != nil {
		return nil, err
	}
	for _, definition := range defs {
		var c registration
		if json.Unmarshal(definition, &c) == nil && c.App == app && c.Kind == Microsoft365 {
			return []schema.DataClass{schema.ClassPII}, nil
		}
	}
	return nil, nil
}

func graphUserVersion(version string) bool {
	return (strings.HasPrefix(version, "W/\"") || strings.HasPrefix(version, "\"")) &&
		strings.HasSuffix(version, "\"") && !strings.ContainsAny(version, "\r\n")
}

func graphListSupportsType(read Read) bool {
	return read.Table == "outlook" || read.Table == "drive" ||
		read.Table == "calendar" && read.GraphStart != "" && read.GraphEnd != ""
}

func (s Service) graphScan(ctx context.Context, r residencyRoute, typ string) (data.SourceScan, error) {
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	q := Query{App: r.config.App, Connection: r.config.ID, Read: r.name, Limit: maxRows}
	scan := data.SourceScan{Type: typ}
	rows := map[string]data.Row{}
	var order []string
	for range 12 {
		p, err := s.Read(ctx, q)
		if err != nil {
			return data.SourceScan{}, err
		}
		for _, id := range p.Removed {
			rows[id] = nil
		}
		for _, row := range p.Rows {
			id := row.ID()
			if _, exists := rows[id]; !exists {
				order = append(order, id)
				if len(order) > maxRows {
					return data.SourceScan{}, ErrTooLarge
				}
			}
			rows[id] = row
		}
		if p.Next == "" {
			scan.Source = p.Source
			for _, id := range order {
				if row := rows[id]; row != nil {
					scan.Rows = append(scan.Rows, row)
				}
			}
			return scan, nil
		}
		q.Cursor = p.Next
	}
	return data.SourceScan{}, ErrTooLarge
}
