package remote

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
	"github.com/Agent-Field/aforge-v2/internal/workspaceview"
)

// ── THE PERSON'S FOLDERS, READ BY THE MACHINE THAT KEEPS THEM ───────────────
//
// A collection — a folder a person files chats, work and files in — lives in
// one database under the ENGINE machine's state root (`v3/collections.db`), and
// every record it points at is on that machine too: the conversations, the
// project's record of work, the standing orders, the files. So a surface reads a
// folder the way it reads the world ([MethodPlacesWorld]): it asks the engine,
// and the engine answers out of its own disk through [workspaceview], which
// resolves every row through the owner that already keeps it.
//
// THESE ARE NOT THE FOLDERS A CONVERSATION IS ABOUT. Those are directories on
// disk a person attaches ([MethodPlacesRefer]); these are logical folders, and
// the two share a word and nothing else. Nothing here moves a file, attaches a
// directory, starts work or grants anything — every method is a reading.
//
// ADDITIVE, AS THE LATE PLACES ARE. An engine that predates these answers
// `engine: no such method`, and the surface draws that as `could not read this
// folder · engine: no such method "Collections.Page"` — never as a machine with none (wire_places.go states the
// bargain). And a surface over a connection NEVER opens its own database instead:
// the folders of the laptop are not the folders of the machine running the work.
const (
	// MethodCollectionsPage is one folder's contents, or the top level for an
	// empty id: filed and placed rows joined without duplicates, each resolved
	// through its owner ([workspaceview.Resolver.Folder]).
	MethodCollectionsPage = "Collections.Page" // CollectionPageArgs → workspaceview.FolderPage
	// MethodCollectionsItem is what one selected row says when looked at closely:
	// the folders filing it, the folders whose rules reach it, the shared context
	// and rules that reach it, and the ongoing work itself for a standing row.
	MethodCollectionsItem = "Collections.Item" // workspace.Ref → workspaceview.FolderItem
	// MethodCollectionsFile is the opening of one file a folder refers to, read
	// on the machine that holds it. It refuses any path no folder names, so it is
	// not a way to read the engine's disk.
	MethodCollectionsFile = "Collections.File" // string (absolute path) → workspaceview.ArtifactPreview
)

// CollectionPageArgs asks for one folder, bounded.
type CollectionPageArgs struct {
	ID    string `json:"id,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// EngineCollections is this machine's folders, as three readings. Any nil field
// is that reading absent here, answered as a refusal.
type EngineCollections struct {
	Page func(ctx context.Context, id string, limit int) (workspaceview.FolderPage, error)
	Item func(ctx context.Context, ref workspace.Ref) (workspaceview.FolderItem, error)
	File func(ctx context.Context, path string) (workspaceview.ArtifactPreview, error)
}

// collectionsOffWord is the refusal for an engine built without the readings.
const collectionsOffWord = "this engine cannot read its folders"

// collectionsCallTimeout bounds one reading on the engine's side of the wire.
const collectionsCallTimeout = 15 * time.Second

// collectionsCall answers the three readings, and says whether the method was
// one of them.
func (s *server) collectionsCall(call Frame) (json.RawMessage, bool, error) {
	sess := s.session
	sess.mu.Lock()
	doors := sess.engine.Collections
	sess.mu.Unlock()
	// A SERVER-SIDE BOUND AS WELL AS THE SURFACE'S: calls are answered one at a
	// time on this connection, so a reading that could not finish must not hold
	// every later call behind it. The database honours it; the file preview
	// refuses anything that could block before it opens (workspaceview.Artifact).
	ctx, cancel := context.WithTimeout(context.Background(), collectionsCallTimeout)
	defer cancel()

	switch call.Method {
	case MethodCollectionsPage:
		args, err := arg[CollectionPageArgs](call)
		if err != nil {
			return nil, true, err
		}
		if doors.Page == nil {
			return nil, true, errors.New(engineOffWord + collectionsOffWord)
		}
		page, err := doors.Page(ctx, args.ID, args.Limit)
		if err != nil {
			return nil, true, err
		}
		payload, err := json.Marshal(page)
		return payload, true, err

	case MethodCollectionsItem:
		ref, err := arg[workspace.Ref](call)
		if err != nil {
			return nil, true, err
		}
		if doors.Item == nil {
			return nil, true, errors.New(engineOffWord + collectionsOffWord)
		}
		item, err := doors.Item(ctx, ref)
		if err != nil {
			return nil, true, err
		}
		payload, err := json.Marshal(item)
		return payload, true, err

	case MethodCollectionsFile:
		path, err := arg[string](call)
		if err != nil {
			return nil, true, err
		}
		if doors.File == nil {
			return nil, true, errors.New(engineOffWord + collectionsOffWord)
		}
		preview, err := doors.File(ctx, path)
		if err != nil {
			return nil, true, err
		}
		payload, err := json.Marshal(preview)
		return payload, true, err
	}
	return nil, false, nil
}

// CollectionPage asks the engine for one folder's page. The error is answered
// and never swallowed, for [Client.World]'s reason: "that machine cannot say"
// and "that folder is empty" are two different sentences.
func (c *Client) CollectionPage(ctx context.Context, id string, limit int) (workspaceview.FolderPage, error) {
	var page workspaceview.FolderPage
	payload, err := c.call(ctx, MethodCollectionsPage, CollectionPageArgs{ID: id, Limit: limit})
	if err == nil {
		err = json.Unmarshal(payload, &page)
	}
	return page, err
}

// CollectionItem asks the engine what one selected row says in detail.
func (c *Client) CollectionItem(ctx context.Context, ref workspace.Ref) (workspaceview.FolderItem, error) {
	var item workspaceview.FolderItem
	payload, err := c.call(ctx, MethodCollectionsItem, ref)
	if err == nil {
		err = json.Unmarshal(payload, &item)
	}
	return item, err
}

// CollectionFile asks the engine for the opening of a file a folder refers to.
func (c *Client) CollectionFile(ctx context.Context, path string) (workspaceview.ArtifactPreview, error) {
	var preview workspaceview.ArtifactPreview
	payload, err := c.call(ctx, MethodCollectionsFile, path)
	if err == nil {
		err = json.Unmarshal(payload, &preview)
	}
	return preview, err
}
